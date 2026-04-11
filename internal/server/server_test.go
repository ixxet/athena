package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ixxet/athena/internal/adapter"
	"github.com/ixxet/athena/internal/domain"
	"github.com/ixxet/athena/internal/edge"
	edgeingress "github.com/ixxet/athena/internal/edge"
	"github.com/ixxet/athena/internal/edgehistory"
	"github.com/ixxet/athena/internal/facility"
	"github.com/ixxet/athena/internal/metrics"
	"github.com/ixxet/athena/internal/presence"
)

type serverStubPublisher struct {
	subjects []string
	payloads [][]byte
}

type serverStubAnalyticsReader struct {
	report edgehistory.AnalyticsReport
	err    error
}

func (s *serverStubPublisher) Publish(_ context.Context, subject string, payload []byte) error {
	s.subjects = append(s.subjects, subject)
	s.payloads = append(s.payloads, payload)
	return nil
}

func (s *serverStubAnalyticsReader) ReadAnalytics(_ context.Context, _ edgehistory.AnalyticsFilter) (edgehistory.AnalyticsReport, error) {
	return s.report, s.err
}

func testHandler(t *testing.T) http.Handler {
	t.Helper()

	baseTime := time.Date(2026, 4, 1, 8, 30, 0, 0, time.UTC)
	service := presence.NewService(
		adapter.NewMockAdapter(adapter.MockConfig{
			FacilityID: "ashtonbee",
			Entries:    3,
			Exits:      1,
			BaseTime:   baseTime,
		}),
		presence.WithClock(func() time.Time { return baseTime }),
	)
	readPath := presence.NewReadPath(service, domain.OccupancyFilter{
		FacilityID: "ashtonbee",
	})

	return NewHandler(readPath, metrics.New(readPath), "mock")
}

func TestHealthEndpoint(t *testing.T) {
	handler := testHandler(t)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	body := recorder.Body.String()
	if !strings.Contains(body, "\"service\":\"athena\"") {
		t.Fatalf("body = %q, want service field", body)
	}
	if !strings.Contains(body, "\"adapter\":\"mock\"") {
		t.Fatalf("body = %q, want adapter field", body)
	}
}

func TestPresenceCountEndpointUsesDefaultFacility(t *testing.T) {
	handler := testHandler(t)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/presence/count", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	body := recorder.Body.String()
	if !strings.Contains(body, "\"facility_id\":\"ashtonbee\"") {
		t.Fatalf("body = %q, want facility_id ashtonbee", body)
	}
	if !strings.Contains(body, "\"current_count\":2") {
		t.Fatalf("body = %q, want current_count 2", body)
	}
}

func TestPresenceCountEndpointReturnsZeroForUnknownFacility(t *testing.T) {
	handler := testHandler(t)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/presence/count?facility=missing", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	body := recorder.Body.String()
	if !strings.Contains(body, "\"facility_id\":\"missing\"") {
		t.Fatalf("body = %q, want facility_id missing", body)
	}
	if !strings.Contains(body, "\"current_count\":0") {
		t.Fatalf("body = %q, want current_count 0", body)
	}
}

func TestMetricsEndpointScrapesCanonicalReadPath(t *testing.T) {
	handler := testHandler(t)

	countRequest := httptest.NewRequest(http.MethodGet, "/api/v1/presence/count?facility=missing", nil)
	countRecorder := httptest.NewRecorder()
	handler.ServeHTTP(countRecorder, countRequest)

	metricsRequest := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(metricsRecorder, metricsRequest)

	if metricsRecorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", metricsRecorder.Code, http.StatusOK)
	}

	body := metricsRecorder.Body.String()
	if !strings.Contains(body, "athena_current_occupancy 2") {
		t.Fatalf("metrics body = %q, want athena_current_occupancy 2", body)
	}
}

func TestPresenceHistoryEndpointRequiresConfiguredHistory(t *testing.T) {
	handler := testHandler(t)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/presence/history?facility=ashtonbee&since=2026-04-01T08:00:00Z&until=2026-04-01T09:00:00Z", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	if !strings.Contains(recorder.Body.String(), "history is not configured") {
		t.Fatalf("body = %q, want missing history error", recorder.Body.String())
	}
}

func TestPresenceHistoryEndpointReturnsSanitizedFacilityHistory(t *testing.T) {
	projector := presence.NewProjectorWithClock(func() time.Time {
		return time.Date(2026, 4, 9, 13, 0, 0, 0, time.UTC)
	})
	readPath := presence.NewReadPath(projector, domain.OccupancyFilter{FacilityID: "ashtonbee"})
	publisher := &serverStubPublisher{}
	historyPath := t.TempDir() + "/edge-history.jsonl"
	historyStore, err := edgehistory.NewFileStore(historyPath)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}

	edgeService, err := edgeingress.NewService(
		publisher,
		"salt",
		map[string]string{"entry-node": "entry-token"},
		edgeingress.WithProjection(projector),
		edgeingress.WithObservationRecorder(historyStore),
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	handler := NewHandler(
		readPath,
		metrics.New(readPath),
		"edge-projection",
		WithEdgeTapHandler(edgeingress.NewHandler(edgeService)),
		WithHistoryReader(historyStore),
	)

	pass := httptest.NewRequest(http.MethodPost, "/api/v1/edge/tap", strings.NewReader(`{
		"event_id":"edge-accepted-001",
		"account_raw":"301536642",
		"direction":"in",
		"facility_id":"ashtonbee",
		"node_id":"entry-node",
		"observed_at":"2026-04-09T12:00:00Z",
		"result":"pass",
		"account_type":"Standard",
		"name":"Fixture Student"
	}`))
	pass.Header.Set("Content-Type", "application/json")
	pass.Header.Set("X-Ashton-Edge-Token", "entry-token")
	passRecorder := httptest.NewRecorder()
	handler.ServeHTTP(passRecorder, pass)
	if passRecorder.Code != http.StatusAccepted {
		t.Fatalf("pass status = %d, want %d", passRecorder.Code, http.StatusAccepted)
	}

	fail := httptest.NewRequest(http.MethodPost, "/api/v1/edge/tap", strings.NewReader(`{
		"event_id":"edge-fail-001",
		"account_raw":"301478835",
		"direction":"out",
		"facility_id":"ashtonbee",
		"node_id":"entry-node",
		"observed_at":"2026-04-09T12:30:00Z",
		"result":"fail",
		"account_type":"Standard",
		"name":"Fixture Student"
	}`))
	fail.Header.Set("Content-Type", "application/json")
	fail.Header.Set("X-Ashton-Edge-Token", "entry-token")
	failRecorder := httptest.NewRecorder()
	handler.ServeHTTP(failRecorder, fail)
	if failRecorder.Code != http.StatusAccepted {
		t.Fatalf("fail status = %d, want %d", failRecorder.Code, http.StatusAccepted)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/presence/history?facility=ashtonbee&since=2026-04-09T11:00:00Z&until=2026-04-09T13:00:00Z", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "\"facility_id\":\"ashtonbee\"") {
		t.Fatalf("body = %q, want facility_id", body)
	}
	if !strings.Contains(body, "\"committed\":true") {
		t.Fatalf("body = %q, want committed observation", body)
	}
	if !strings.Contains(body, "\"result\":\"fail\"") {
		t.Fatalf("body = %q, want fail observation", body)
	}
	if strings.Contains(body, "external_identity_hash") {
		t.Fatalf("body leaked external identity hash: %q", body)
	}
	if strings.Contains(body, "Fixture Student") {
		t.Fatalf("body leaked resolved name: %q", body)
	}
}

func TestPresenceHistoryEndpointRejectsInvalidQueries(t *testing.T) {
	historyPath := t.TempDir() + "/edge-history.jsonl"
	historyStore, err := edgehistory.NewFileStore(historyPath)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	record := edge.ObservationRecord{
		EventID:              "edge-accepted-001",
		FacilityID:           "ashtonbee",
		NodeID:               "entry-node",
		Direction:            domain.DirectionIn,
		Result:               "pass",
		Source:               domain.SourceRFID,
		ExternalIdentityHash: "hash-001",
		ObservedAt:           time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC),
		StoredAt:             time.Date(2026, 4, 9, 12, 0, 1, 0, time.UTC),
	}
	record.ObservationID = record.Identity()
	if err := historyStore.RecordObservation(context.Background(), record); err != nil {
		t.Fatalf("RecordObservation() error = %v", err)
	}

	handler := NewHandler(
		presence.NewReadPath(presence.NewProjector(), domain.OccupancyFilter{FacilityID: "ashtonbee"}),
		metrics.New(presence.NewReadPath(presence.NewProjector(), domain.OccupancyFilter{FacilityID: "ashtonbee"})),
		"edge-projection",
		WithHistoryReader(historyStore),
	)

	testCases := []string{
		"/api/v1/presence/history?since=2026-04-09T11:00:00Z&until=2026-04-09T13:00:00Z",
		"/api/v1/presence/history?facility=ashtonbee&until=2026-04-09T13:00:00Z",
		"/api/v1/presence/history?facility=ashtonbee&since=bad-time&until=2026-04-09T13:00:00Z",
		"/api/v1/presence/history?facility=ashtonbee&since=2026-04-09T13:00:00Z&until=2026-04-09T11:00:00Z",
	}

	for _, target := range testCases {
		request := httptest.NewRequest(http.MethodGet, target, nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("target %s status = %d, want %d", target, recorder.Code, http.StatusBadRequest)
		}
	}
}

func TestPresenceAnalyticsEndpointRequiresConfiguredAnalytics(t *testing.T) {
	handler := testHandler(t)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/presence/analytics?facility=ashtonbee&since=2026-04-09T11:00:00Z&until=2026-04-09T13:00:00Z", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	if !strings.Contains(recorder.Body.String(), "analytics are not configured") {
		t.Fatalf("body = %q, want missing analytics error", recorder.Body.String())
	}
}

func TestPresenceAnalyticsEndpointReturnsBoundedReport(t *testing.T) {
	report := edgehistory.AnalyticsReport{
		FacilityID:    "ashtonbee",
		ZoneID:        "gym-floor",
		NodeID:        "entry-node",
		Since:         time.Date(2026, 4, 9, 11, 0, 0, 0, time.UTC),
		Until:         time.Date(2026, 4, 9, 13, 0, 0, 0, time.UTC),
		BucketMinutes: 15,
		ObservationSummary: edgehistory.ObservationSummary{
			Total:         3,
			Pass:          2,
			Fail:          1,
			CommittedPass: 2,
		},
		SessionSummary: edgehistory.SessionSummary{
			ClosedCount:            1,
			UniqueVisitors:         1,
			AverageDurationSeconds: 300,
			MedianDurationSeconds:  300,
			OccupancyAtEnd:         0,
		},
		FlowBuckets: []edgehistory.FlowBucket{{
			StartedAt:    time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC),
			EndedAt:      time.Date(2026, 4, 9, 12, 15, 0, 0, time.UTC),
			PassIn:       1,
			PassOut:      0,
			FailIn:       0,
			FailOut:      0,
			OccupancyEnd: 1,
		}},
		Sessions: []edgehistory.SessionFact{{
			SessionID: "session-001",
			State:     "closed",
		}},
	}

	handler := NewHandler(
		presence.NewReadPath(presence.NewProjector(), domain.OccupancyFilter{FacilityID: "ashtonbee"}),
		metrics.New(presence.NewReadPath(presence.NewProjector(), domain.OccupancyFilter{FacilityID: "ashtonbee"})),
		"edge-projection",
		WithAnalyticsReader(&serverStubAnalyticsReader{report: report}),
		WithAnalyticsMaxWindow(24*time.Hour),
	)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/presence/analytics?facility=ashtonbee&zone=gym-floor&node=entry-node&since=2026-04-09T11:00:00Z&until=2026-04-09T13:00:00Z&bucket_minutes=15&session_limit=5", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "\"facility_id\":\"ashtonbee\"") {
		t.Fatalf("body = %q, want facility_id", body)
	}
	if !strings.Contains(body, "\"committed_pass\":2") {
		t.Fatalf("body = %q, want committed_pass summary", body)
	}
	if !strings.Contains(body, "\"session_id\":\"session-001\"") {
		t.Fatalf("body = %q, want session fact", body)
	}
}

func TestPresenceAnalyticsEndpointRejectsInvalidQueries(t *testing.T) {
	handler := NewHandler(
		presence.NewReadPath(presence.NewProjector(), domain.OccupancyFilter{FacilityID: "ashtonbee"}),
		metrics.New(presence.NewReadPath(presence.NewProjector(), domain.OccupancyFilter{FacilityID: "ashtonbee"})),
		"edge-projection",
		WithAnalyticsReader(&serverStubAnalyticsReader{}),
		WithAnalyticsMaxWindow(2*time.Hour),
	)

	testCases := []string{
		"/api/v1/presence/analytics?since=2026-04-09T11:00:00Z&until=2026-04-09T13:00:00Z",
		"/api/v1/presence/analytics?facility=ashtonbee&until=2026-04-09T13:00:00Z",
		"/api/v1/presence/analytics?facility=ashtonbee&since=bad-time&until=2026-04-09T13:00:00Z",
		"/api/v1/presence/analytics?facility=ashtonbee&since=2026-04-09T11:00:00Z&until=2026-04-09T14:00:01Z",
		"/api/v1/presence/analytics?facility=ashtonbee&since=2026-04-09T11:00:00Z&until=2026-04-09T13:00:00Z&bucket_minutes=0",
	}

	for _, target := range testCases {
		request := httptest.NewRequest(http.MethodGet, target, nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("target %s status = %d, want %d", target, recorder.Code, http.StatusBadRequest)
		}
	}
}

func TestFacilitiesEndpointRequiresConfiguredCatalog(t *testing.T) {
	handler := testHandler(t)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/facilities", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	if !strings.Contains(recorder.Body.String(), "facility catalog is not configured") {
		t.Fatalf("body = %q, want facility catalog error", recorder.Body.String())
	}
}

func TestFacilitiesEndpointReturnsCatalogSummaries(t *testing.T) {
	handler := NewHandler(
		presence.NewReadPath(presence.NewProjector(), domain.OccupancyFilter{FacilityID: "ashtonbee"}),
		metrics.New(presence.NewReadPath(presence.NewProjector(), domain.OccupancyFilter{FacilityID: "ashtonbee"})),
		"mock",
		WithFacilityStore(testFacilityStore(t)),
	)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/facilities", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "\"facility_id\":\"ashtonbee\"") {
		t.Fatalf("body = %q, want ashtonbee summary", body)
	}
	if !strings.Contains(body, "\"facility_id\":\"morningside\"") {
		t.Fatalf("body = %q, want morningside summary", body)
	}
}

func TestFacilityDetailEndpointReturnsFacilityTruth(t *testing.T) {
	handler := NewHandler(
		presence.NewReadPath(presence.NewProjector(), domain.OccupancyFilter{FacilityID: "ashtonbee"}),
		metrics.New(presence.NewReadPath(presence.NewProjector(), domain.OccupancyFilter{FacilityID: "ashtonbee"})),
		"mock",
		WithFacilityStore(testFacilityStore(t)),
	)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/facilities/ashtonbee", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "\"timezone\":\"America/Toronto\"") {
		t.Fatalf("body = %q, want facility timezone", body)
	}
	if !strings.Contains(body, "\"zone_id\":\"gym-floor\"") {
		t.Fatalf("body = %q, want gym-floor zone", body)
	}
	if !strings.Contains(body, "\"starts_at\":\"2026-07-01T12:00:00Z\"") {
		t.Fatalf("body = %q, want UTC-normalized closure start", body)
	}
}

func TestFacilityDetailEndpointReturnsNotFoundForUnknownFacility(t *testing.T) {
	handler := NewHandler(
		presence.NewReadPath(presence.NewProjector(), domain.OccupancyFilter{FacilityID: "ashtonbee"}),
		metrics.New(presence.NewReadPath(presence.NewProjector(), domain.OccupancyFilter{FacilityID: "ashtonbee"})),
		"mock",
		WithFacilityStore(testFacilityStore(t)),
	)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/facilities/missing", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if !strings.Contains(recorder.Body.String(), "facility not found") {
		t.Fatalf("body = %q, want facility not found", recorder.Body.String())
	}
}

func TestEdgeTapRouteMountsWhenConfigured(t *testing.T) {
	handler := NewHandler(
		presence.NewReadPath(
			presence.NewService(adapter.NewMockAdapter(adapter.MockConfig{
				FacilityID: "ashtonbee",
				Entries:    1,
				BaseTime:   time.Date(2026, 4, 1, 8, 30, 0, 0, time.UTC),
			})),
			domain.OccupancyFilter{FacilityID: "ashtonbee"},
		),
		metrics.New(
			presence.NewReadPath(
				presence.NewService(adapter.NewMockAdapter(adapter.MockConfig{
					FacilityID: "ashtonbee",
					Entries:    1,
					BaseTime:   time.Date(2026, 4, 1, 8, 30, 0, 0, time.UTC),
				})),
				domain.OccupancyFilter{FacilityID: "ashtonbee"},
			),
		),
		"mock",
		WithEdgeTapHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusAccepted)
		})),
	)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/edge/tap", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusAccepted)
	}
}

func TestEdgeTapProjectionUpdatesPresenceCount(t *testing.T) {
	projector := presence.NewProjectorWithClock(func() time.Time {
		return time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)
	})
	readPath := presence.NewReadPath(projector, domain.OccupancyFilter{FacilityID: "ashtonbee"})
	publisher := &serverStubPublisher{}
	edgeService, err := edgeingress.NewService(
		publisher,
		"salt",
		map[string]string{"entry-node": "entry-token"},
		edgeingress.WithProjection(projector),
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	handler := NewHandler(
		readPath,
		metrics.New(readPath),
		"edge-projection",
		WithEdgeTapHandler(edgeingress.NewHandler(edgeService)),
	)

	tapRequest := httptest.NewRequest(http.MethodPost, "/api/v1/edge/tap", strings.NewReader(`{
		"event_id":"edge-001",
		"account_raw":"301536642",
		"direction":"in",
		"facility_id":"ashtonbee",
		"zone_id":"gym-floor",
		"node_id":"entry-node",
		"observed_at":"2026-04-05T12:00:00Z",
		"result":"pass",
		"account_type":"Standard",
		"name":"Fixture Student",
		"status_message":"Access granted to Event."
	}`))
	tapRequest.Header.Set("Content-Type", "application/json")
	tapRequest.Header.Set("X-Ashton-Edge-Token", "entry-token")

	tapRecorder := httptest.NewRecorder()
	handler.ServeHTTP(tapRecorder, tapRequest)
	if tapRecorder.Code != http.StatusAccepted {
		t.Fatalf("tap status = %d, want %d", tapRecorder.Code, http.StatusAccepted)
	}

	countRequest := httptest.NewRequest(http.MethodGet, "/api/v1/presence/count?facility=ashtonbee&zone=gym-floor", nil)
	countRecorder := httptest.NewRecorder()
	handler.ServeHTTP(countRecorder, countRequest)
	if countRecorder.Code != http.StatusOK {
		t.Fatalf("count status = %d, want %d", countRecorder.Code, http.StatusOK)
	}
	body := countRecorder.Body.String()
	if !strings.Contains(body, "\"current_count\":1") {
		t.Fatalf("count body = %q, want current_count 1", body)
	}
	if len(publisher.subjects) != 1 {
		t.Fatalf("len(subjects) = %d, want 1", len(publisher.subjects))
	}
}

func TestEdgeTapProjectionKeepsFailObservationOutOfOccupancy(t *testing.T) {
	projector := presence.NewProjectorWithClock(func() time.Time {
		return time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)
	})
	readPath := presence.NewReadPath(projector, domain.OccupancyFilter{FacilityID: "ashtonbee"})
	publisher := &serverStubPublisher{}
	edgeService, err := edgeingress.NewService(
		publisher,
		"salt",
		map[string]string{"entry-node": "entry-token"},
		edgeingress.WithProjection(projector),
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	handler := NewHandler(
		readPath,
		metrics.New(readPath),
		"edge-projection",
		WithEdgeTapHandler(edgeingress.NewHandler(edgeService)),
	)

	tapRequest := httptest.NewRequest(http.MethodPost, "/api/v1/edge/tap", strings.NewReader(`{
		"event_id":"edge-fail-001",
		"account_raw":"301478835",
		"direction":"in",
		"facility_id":"ashtonbee",
		"zone_id":"gym-floor",
		"node_id":"entry-node",
		"observed_at":"2026-04-05T12:01:00Z",
		"result":"fail",
		"account_type":"Standard",
		"name":"Fixture Student",
		"status_message":"Access denied, no rule matches Account."
	}`))
	tapRequest.Header.Set("Content-Type", "application/json")
	tapRequest.Header.Set("X-Ashton-Edge-Token", "entry-token")

	tapRecorder := httptest.NewRecorder()
	handler.ServeHTTP(tapRecorder, tapRequest)
	if tapRecorder.Code != http.StatusAccepted {
		t.Fatalf("tap status = %d, want %d", tapRecorder.Code, http.StatusAccepted)
	}

	countRequest := httptest.NewRequest(http.MethodGet, "/api/v1/presence/count?facility=ashtonbee&zone=gym-floor", nil)
	countRecorder := httptest.NewRecorder()
	handler.ServeHTTP(countRecorder, countRequest)
	if countRecorder.Code != http.StatusOK {
		t.Fatalf("count status = %d, want %d", countRecorder.Code, http.StatusOK)
	}
	body := countRecorder.Body.String()
	if !strings.Contains(body, "\"current_count\":0") {
		t.Fatalf("count body = %q, want current_count 0", body)
	}
	if len(publisher.subjects) != 0 {
		t.Fatalf("len(subjects) = %d, want 0", len(publisher.subjects))
	}
}

func testFacilityStore(t *testing.T) *facility.Store {
	t.Helper()

	store, err := facility.NewStore(facility.Catalog{
		Facilities: []facility.Facility{
			{
				FacilityID: "morningside",
				Name:       "Morningside",
				Timezone:   "America/Toronto",
				Hours: []facility.HoursWindow{
					{Day: "monday", OpensAt: "06:00", ClosesAt: "22:00"},
				},
				Zones: []facility.Zone{
					{ZoneID: "weight-room", Name: "Weight Room"},
				},
				Metadata: map[string]string{
					"ingress_mode": "touchnet",
					"surface":      "internal-only",
				},
			},
			{
				FacilityID: "ashtonbee",
				Name:       "Ashtonbee",
				Timezone:   "America/Toronto",
				Hours: []facility.HoursWindow{
					{Day: "monday", OpensAt: "06:00", ClosesAt: "22:00"},
				},
				Zones: []facility.Zone{
					{ZoneID: "gym-floor", Name: "Gym Floor"},
					{ZoneID: "lobby", Name: "Lobby"},
				},
				ClosureWindows: []facility.ClosureWindow{
					{
						StartsAt: "2026-07-01T08:00:00-04:00",
						EndsAt:   "2026-07-01T12:00:00-04:00",
						Code:     "maintenance",
						Reason:   "Morning maintenance",
						ZoneIDs:  []string{"gym-floor"},
					},
				},
				Metadata: map[string]string{
					"ingress_mode": "touchnet",
					"surface":      "internal-only",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("facility.NewStore() error = %v", err)
	}

	return store
}
