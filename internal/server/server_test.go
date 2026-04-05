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
	edgeingress "github.com/ixxet/athena/internal/edge"
	"github.com/ixxet/athena/internal/metrics"
	"github.com/ixxet/athena/internal/presence"
)

type serverStubPublisher struct {
	subjects []string
	payloads [][]byte
}

func (s *serverStubPublisher) Publish(_ context.Context, subject string, payload []byte) error {
	s.subjects = append(s.subjects, subject)
	s.payloads = append(s.payloads, payload)
	return nil
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
