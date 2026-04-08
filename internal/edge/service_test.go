package edge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	protoevents "github.com/ixxet/ashton-proto/events"
	athenav1 "github.com/ixxet/ashton-proto/gen/go/ashton/athena/v1"
	"github.com/ixxet/athena/internal/domain"
	"github.com/ixxet/athena/internal/presence"
)

type stubPublisher struct {
	messages []publishedMessage
	err      error
}

type publishedMessage struct {
	subject string
	payload []byte
}

func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()

	var logs bytes.Buffer
	previous := slog.Default()
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)
	t.Cleanup(func() {
		slog.SetDefault(previous)
	})

	return &logs
}

func (s *stubPublisher) Publish(_ context.Context, subject string, payload []byte) error {
	if s.err != nil {
		return s.err
	}

	s.messages = append(s.messages, publishedMessage{
		subject: subject,
		payload: payload,
	})

	return nil
}

func TestAcceptTapPublishesRFIDArrival(t *testing.T) {
	publisher := &stubPublisher{}
	service, err := NewService(publisher, "salt", map[string]string{"entry-node": "entry-token"})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	req := TapRequest{
		EventID:       "edge-001",
		AccountRaw:    "1000123456",
		Direction:     "in",
		FacilityID:    "ashtonbee",
		ZoneID:        "gym-floor",
		NodeID:        "entry-node",
		ObservedAt:    "2026-04-04T12:00:00Z",
		Result:        "pass",
		AccountType:   "Standard",
		Name:          "Fixture Student",
		StatusMessage: "Access entry granted.",
	}

	result, err := service.AcceptTap(context.Background(), "entry-token", req)
	if err != nil {
		t.Fatalf("AcceptTap() error = %v", err)
	}

	if result.Status != "accepted" {
		t.Fatalf("result.Status = %q, want accepted", result.Status)
	}
	if result.Result != "pass" {
		t.Fatalf("result.Result = %q, want pass", result.Result)
	}
	if result.Direction != "in" {
		t.Fatalf("result.Direction = %q, want in", result.Direction)
	}
	if !result.Published {
		t.Fatal("result.Published = false, want true")
	}
	if len(publisher.messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(publisher.messages))
	}
	if publisher.messages[0].subject != protoevents.SubjectIdentifiedPresenceArrived {
		t.Fatalf("subject = %q, want %q", publisher.messages[0].subject, protoevents.SubjectIdentifiedPresenceArrived)
	}
	if strings.Contains(string(publisher.messages[0].payload), req.AccountRaw) {
		t.Fatalf("payload leaked raw account number: %s", publisher.messages[0].payload)
	}

	event, err := protoevents.ParseIdentifiedPresenceArrived(publisher.messages[0].payload)
	if err != nil {
		t.Fatalf("ParseIdentifiedPresenceArrived() error = %v", err)
	}
	if event.ID != req.EventID {
		t.Fatalf("event.ID = %q, want %q", event.ID, req.EventID)
	}
	if event.Data.GetExternalIdentityHash() != HashAccount(req.AccountRaw, "salt") {
		t.Fatalf("external_identity_hash = %q, want hashed value", event.Data.GetExternalIdentityHash())
	}
	if event.Data.GetSource() != athenav1.PresenceSource_PRESENCE_SOURCE_RFID {
		t.Fatalf("source = %s, want RFID", event.Data.GetSource())
	}
}

func TestAcceptTapObservesFailWithoutPublishing(t *testing.T) {
	publisher := &stubPublisher{}
	service, err := NewService(publisher, "salt", map[string]string{"entry-node": "entry-token"})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	req := TapRequest{
		EventID:       "edge-fail-001",
		AccountRaw:    "301536642",
		Direction:     "in",
		FacilityID:    "ashtonbee",
		ZoneID:        "gym-floor",
		NodeID:        "entry-node",
		ObservedAt:    "2026-04-04T12:00:00Z",
		Result:        "fail",
		AccountType:   "Standard",
		Name:          "Fixture Student",
		StatusMessage: "Account not initialized.",
	}

	result, err := service.AcceptTap(context.Background(), "entry-token", req)
	if err != nil {
		t.Fatalf("AcceptTap() error = %v", err)
	}

	if result.Status != "observed" {
		t.Fatalf("result.Status = %q, want observed", result.Status)
	}
	if result.Result != "fail" {
		t.Fatalf("result.Result = %q, want fail", result.Result)
	}
	if result.Direction != "in" {
		t.Fatalf("result.Direction = %q, want in", result.Direction)
	}
	if result.Published {
		t.Fatal("result.Published = true, want false")
	}
	if result.Subject != "" {
		t.Fatalf("result.Subject = %q, want empty", result.Subject)
	}
	if len(publisher.messages) != 0 {
		t.Fatalf("len(messages) = %d, want 0", len(publisher.messages))
	}
}

func TestAcceptTapAcceptedLogsRedactRawIdentity(t *testing.T) {
	logs := captureSlog(t)

	publisher := &stubPublisher{}
	service, err := NewService(publisher, "salt", map[string]string{"entry-node": "entry-token"})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	req := TapRequest{
		EventID:       "edge-log-accepted-001",
		AccountRaw:    "1000123456",
		Direction:     "in",
		FacilityID:    "ashtonbee",
		NodeID:        "entry-node",
		ObservedAt:    "2026-04-04T12:00:00Z",
		Result:        "pass",
		AccountType:   "Standard",
		Name:          "Fixture Student",
		StatusMessage: "Access entry granted.",
	}

	if _, err := service.AcceptTap(context.Background(), "entry-token", req); err != nil {
		t.Fatalf("AcceptTap() error = %v", err)
	}

	logOutput := logs.String()
	if strings.Contains(logOutput, req.AccountRaw) {
		t.Fatalf("log output leaked raw account number: %s", logOutput)
	}
	if strings.Contains(logOutput, req.Name) {
		t.Fatalf("log output leaked resolved name: %s", logOutput)
	}
	if !strings.Contains(logOutput, HashAccount(req.AccountRaw, "salt")) {
		t.Fatalf("log output missing external_identity_hash: %s", logOutput)
	}
}

func TestAcceptTapPublishFailureLogsRedactRawIdentity(t *testing.T) {
	logs := captureSlog(t)

	publisher := &stubPublisher{err: errors.New("nats unavailable")}
	service, err := NewService(publisher, "salt", map[string]string{"entry-node": "entry-token"})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	req := TapRequest{
		EventID:       "edge-log-error-001",
		AccountRaw:    "1000123456",
		Direction:     "in",
		FacilityID:    "ashtonbee",
		NodeID:        "entry-node",
		ObservedAt:    "2026-04-04T12:00:00Z",
		Result:        "pass",
		AccountType:   "Standard",
		Name:          "Fixture Student",
		StatusMessage: "Access entry granted.",
	}

	if _, err := service.AcceptTap(context.Background(), "entry-token", req); err == nil {
		t.Fatal("AcceptTap() error = nil, want publish failure")
	}

	logOutput := logs.String()
	if strings.Contains(logOutput, req.AccountRaw) {
		t.Fatalf("log output leaked raw account number: %s", logOutput)
	}
	if strings.Contains(logOutput, req.Name) {
		t.Fatalf("log output leaked resolved name: %s", logOutput)
	}
	if !strings.Contains(logOutput, HashAccount(req.AccountRaw, "salt")) {
		t.Fatalf("log output missing external_identity_hash: %s", logOutput)
	}
}

func TestAcceptTapDuplicatePayloadKeepsStableEventID(t *testing.T) {
	publisher := &stubPublisher{}
	service, err := NewService(publisher, "salt", map[string]string{"entry-node": "entry-token"})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	req := TapRequest{
		EventID:    "edge-duplicate-001",
		AccountRaw: "1000123456",
		Direction:  "out",
		FacilityID: "ashtonbee",
		NodeID:     "entry-node",
		ObservedAt: "2026-04-04T12:30:00Z",
	}

	for i := 0; i < 2; i++ {
		if _, err := service.AcceptTap(context.Background(), "entry-token", req); err != nil {
			t.Fatalf("AcceptTap() call %d error = %v", i+1, err)
		}
	}

	if len(publisher.messages) != 2 {
		t.Fatalf("len(messages) = %d, want 2", len(publisher.messages))
	}

	first, err := protoevents.ParseIdentifiedPresenceDeparted(publisher.messages[0].payload)
	if err != nil {
		t.Fatalf("ParseIdentifiedPresenceDeparted(first) error = %v", err)
	}
	second, err := protoevents.ParseIdentifiedPresenceDeparted(publisher.messages[1].payload)
	if err != nil {
		t.Fatalf("ParseIdentifiedPresenceDeparted(second) error = %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("first.ID = %q, second.ID = %q, want same event id", first.ID, second.ID)
	}
}

func TestAcceptTapWithProjectionSkipsRepeatedPassIn(t *testing.T) {
	publisher := &stubPublisher{}
	projector := presence.NewProjector()
	service, err := NewService(
		publisher,
		"salt",
		map[string]string{"entry-node": "entry-token"},
		WithProjection(projector),
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	first, err := service.AcceptTap(context.Background(), "entry-token", TapRequest{
		EventID:    "edge-001",
		AccountRaw: "1000123456",
		Direction:  "in",
		FacilityID: "ashtonbee",
		ZoneID:     "gym-floor",
		NodeID:     "entry-node",
		ObservedAt: "2026-04-04T12:00:00Z",
		Result:     "pass",
	})
	if err != nil {
		t.Fatalf("first AcceptTap() error = %v", err)
	}
	if !first.Published {
		t.Fatal("first.Published = false, want true")
	}

	second, err := service.AcceptTap(context.Background(), "entry-token", TapRequest{
		EventID:    "edge-002",
		AccountRaw: "1000123456",
		Direction:  "in",
		FacilityID: "ashtonbee",
		ZoneID:     "gym-floor",
		NodeID:     "entry-node",
		ObservedAt: "2026-04-04T12:01:00Z",
		Result:     "pass",
	})
	if err != nil {
		t.Fatalf("second AcceptTap() error = %v", err)
	}
	if second.Status != "observed" {
		t.Fatalf("second.Status = %q, want observed", second.Status)
	}
	if second.Published {
		t.Fatal("second.Published = true, want false")
	}
	if len(publisher.messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(publisher.messages))
	}

	snapshot, err := projector.CurrentOccupancy(context.Background(), domain.OccupancyFilter{
		FacilityID: "ashtonbee",
		ZoneID:     "gym-floor",
	})
	if err != nil {
		t.Fatalf("CurrentOccupancy() error = %v", err)
	}
	if snapshot.CurrentCount != 1 {
		t.Fatalf("current_count = %d, want 1", snapshot.CurrentCount)
	}
}

func TestAcceptTapWithProjectionPublishesDepartureAfterArrival(t *testing.T) {
	publisher := &stubPublisher{}
	projector := presence.NewProjector()
	service, err := NewService(
		publisher,
		"salt",
		map[string]string{"entry-node": "entry-token"},
		WithProjection(projector),
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	if _, err := service.AcceptTap(context.Background(), "entry-token", TapRequest{
		EventID:    "edge-in-001",
		AccountRaw: "1000123456",
		Direction:  "in",
		FacilityID: "ashtonbee",
		NodeID:     "entry-node",
		ObservedAt: "2026-04-04T12:00:00Z",
		Result:     "pass",
	}); err != nil {
		t.Fatalf("arrival AcceptTap() error = %v", err)
	}

	result, err := service.AcceptTap(context.Background(), "entry-token", TapRequest{
		EventID:    "edge-out-001",
		AccountRaw: "1000123456",
		Direction:  "out",
		FacilityID: "ashtonbee",
		NodeID:     "entry-node",
		ObservedAt: "2026-04-04T12:05:00Z",
		Result:     "pass",
	})
	if err != nil {
		t.Fatalf("departure AcceptTap() error = %v", err)
	}
	if !result.Published {
		t.Fatal("result.Published = false, want true")
	}
	if result.Direction != "out" {
		t.Fatalf("result.Direction = %q, want out", result.Direction)
	}
	if len(publisher.messages) != 2 {
		t.Fatalf("len(messages) = %d, want 2", len(publisher.messages))
	}

	event, err := protoevents.ParseIdentifiedPresenceDeparted(publisher.messages[1].payload)
	if err != nil {
		t.Fatalf("ParseIdentifiedPresenceDeparted() error = %v", err)
	}
	if event.ID != "edge-out-001" {
		t.Fatalf("event.ID = %q, want edge-out-001", event.ID)
	}
}

func TestAcceptTapWithProjectionDoesNotCommitOnPublishFailure(t *testing.T) {
	publisher := &stubPublisher{err: errors.New("broker unavailable")}
	projector := presence.NewProjector()
	service, err := NewService(
		publisher,
		"salt",
		map[string]string{"entry-node": "entry-token"},
		WithProjection(projector),
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	req := TapRequest{
		EventID:    "edge-001",
		AccountRaw: "1000123456",
		Direction:  "in",
		FacilityID: "ashtonbee",
		ZoneID:     "gym-floor",
		NodeID:     "entry-node",
		ObservedAt: "2026-04-04T12:00:00Z",
		Result:     "pass",
	}

	if _, err := service.AcceptTap(context.Background(), "entry-token", req); err == nil {
		t.Fatal("AcceptTap() error = nil, want publish failure")
	} else if !errors.Is(err, ErrPublishUnavailable) {
		t.Fatalf("AcceptTap() error = %v, want ErrPublishUnavailable", err)
	}

	snapshot, err := projector.CurrentOccupancy(context.Background(), domain.OccupancyFilter{
		FacilityID: "ashtonbee",
		ZoneID:     "gym-floor",
	})
	if err != nil {
		t.Fatalf("CurrentOccupancy() error = %v", err)
	}
	if snapshot.CurrentCount != 0 {
		t.Fatalf("current_count = %d, want 0 after failed publish", snapshot.CurrentCount)
	}

	publisher.err = nil

	result, err := service.AcceptTap(context.Background(), "entry-token", req)
	if err != nil {
		t.Fatalf("AcceptTap(retry) error = %v", err)
	}
	if !result.Published {
		t.Fatal("result.Published = false, want true")
	}
	if len(publisher.messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(publisher.messages))
	}
}

func TestHandlerRejectsMissingToken(t *testing.T) {
	service, err := NewService(&stubPublisher{}, "salt", map[string]string{"entry-node": "entry-token"})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/edge/tap", strings.NewReader(`{
		"event_id":"edge-001",
		"account_raw":"1000123456",
		"direction":"in",
		"facility_id":"ashtonbee",
		"node_id":"entry-node",
		"observed_at":"2026-04-04T12:00:00Z"
	}`))
	request.Header.Set("Content-Type", "application/json")

	NewHandler(service).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestHandlerRejectsTokenNodeMismatch(t *testing.T) {
	service, err := NewService(&stubPublisher{}, "salt", map[string]string{"entry-node": "entry-token"})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/edge/tap", strings.NewReader(`{
		"event_id":"edge-001",
		"account_raw":"1000123456",
		"direction":"in",
		"facility_id":"ashtonbee",
		"node_id":"entry-node",
		"observed_at":"2026-04-04T12:00:00Z"
	}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Ashton-Edge-Token", "wrong-token")

	NewHandler(service).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
}

func TestHandlerRejectsInvalidDirection(t *testing.T) {
	service, err := NewService(&stubPublisher{}, "salt", map[string]string{"entry-node": "entry-token"})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/edge/tap", strings.NewReader(`{
		"event_id":"edge-001",
		"account_raw":"1000123456",
		"direction":"sideways",
		"facility_id":"ashtonbee",
		"node_id":"entry-node",
		"observed_at":"2026-04-04T12:00:00Z"
	}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Ashton-Edge-Token", "entry-token")

	NewHandler(service).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if strings.Contains(recorder.Body.String(), "1000123456") {
		t.Fatalf("response leaked raw account number: %s", recorder.Body.String())
	}
}

func TestHandlerRejectsInvalidResult(t *testing.T) {
	service, err := NewService(&stubPublisher{}, "salt", map[string]string{"entry-node": "entry-token"})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/edge/tap", strings.NewReader(`{
		"event_id":"edge-001",
		"account_raw":"1000123456",
		"direction":"in",
		"facility_id":"ashtonbee",
		"node_id":"entry-node",
		"observed_at":"2026-04-04T12:00:00Z",
		"result":"maybe"
	}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Ashton-Edge-Token", "entry-token")

	NewHandler(service).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestHandlerReturnsServiceUnavailableWhenPublisherFails(t *testing.T) {
	service, err := NewService(&stubPublisher{err: errors.New("broker unavailable")}, "salt", map[string]string{"entry-node": "entry-token"})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/edge/tap", strings.NewReader(`{
		"event_id":"edge-001",
		"account_raw":"1000123456",
		"direction":"in",
		"facility_id":"ashtonbee",
		"node_id":"entry-node",
		"observed_at":"2026-04-04T12:00:00Z"
	}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Ashton-Edge-Token", "entry-token")

	NewHandler(service).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	if strings.Contains(recorder.Body.String(), "1000123456") {
		t.Fatalf("response leaked raw account number: %s", recorder.Body.String())
	}
}

func TestDeriveEventIDIsStable(t *testing.T) {
	observedAt := time.Date(2026, 4, 4, 12, 0, 0, 123000000, time.UTC)
	first := DeriveEventID("entry-node", "in", "1000123456", observedAt)
	second := DeriveEventID("entry-node", "in", "1000123456", observedAt)
	if first != second {
		t.Fatalf("first = %q, second = %q, want identical ids", first, second)
	}
}

func TestHandlerReturnsAcceptedPayload(t *testing.T) {
	service, err := NewService(&stubPublisher{}, "salt", map[string]string{"entry-node": "entry-token"})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/edge/tap", strings.NewReader(`{
		"event_id":"edge-accepted-001",
		"account_raw":"1000123456",
		"direction":"in",
		"facility_id":"ashtonbee",
		"node_id":"entry-node",
		"observed_at":"2026-04-04T12:00:00Z"
	}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Ashton-Edge-Token", "entry-token")

	NewHandler(service).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusAccepted)
	}

	var response AcceptedTap
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("Unmarshal(response) error = %v", err)
	}
	if response.EventID != "edge-accepted-001" {
		t.Fatalf("response.EventID = %q, want edge-accepted-001", response.EventID)
	}
	if response.Result != "pass" {
		t.Fatalf("response.Result = %q, want pass", response.Result)
	}
	if response.Direction != "in" {
		t.Fatalf("response.Direction = %q, want in", response.Direction)
	}
	if !response.Published {
		t.Fatal("response.Published = false, want true")
	}
}
