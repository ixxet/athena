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

type stubObservationRecorder struct {
	records     []ObservationRecord
	commits     []ObservationCommit
	acceptances []PresenceAcceptance
	err         error
}

type stubPolicyEvaluator struct {
	decision PolicyDecision
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

func (s *stubObservationRecorder) RecordObservation(_ context.Context, record ObservationRecord) error {
	if s.err != nil {
		return s.err
	}

	s.records = append(s.records, record)
	return nil
}

func (s *stubObservationRecorder) RecordCommit(_ context.Context, commit ObservationCommit) error {
	if s.err != nil {
		return s.err
	}

	s.commits = append(s.commits, commit)
	return nil
}

func (s *stubObservationRecorder) RecordAcceptance(_ context.Context, acceptance PresenceAcceptance) error {
	if s.err != nil {
		return s.err
	}

	s.acceptances = append(s.acceptances, acceptance)
	return nil
}

func (s *stubPolicyEvaluator) EvaluatePolicy(_ context.Context, _ PolicyEvaluation) (PolicyDecision, error) {
	if s.err != nil {
		return PolicyDecision{}, s.err
	}
	return s.decision, nil
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

func TestAcceptTapAcceptsRecognizedDeniedFailThroughPolicy(t *testing.T) {
	publisher := &stubPublisher{}
	recorder := &stubObservationRecorder{}
	projector := presence.NewProjector()
	service, err := NewService(
		publisher,
		"salt",
		map[string]string{"entry-node": "entry-token"},
		WithProjection(projector),
		WithObservationRecorder(recorder),
		WithAcceptedPresenceRecorder(recorder),
		WithPolicyEvaluator(&stubPolicyEvaluator{
			decision: PolicyDecision{
				Admitted:           true,
				AcceptancePath:     AcceptancePathFacility,
				AcceptedReasonCode: "testing_rollout",
				PolicyVersionID:    "policy-version-001",
			},
		}),
		WithPolicyAcceptanceEnabled(true),
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	result, err := service.AcceptTap(context.Background(), "entry-token", TapRequest{
		EventID:       "edge-fail-policy-001",
		AccountRaw:    "301536642",
		Direction:     "in",
		FacilityID:    "ashtonbee",
		ZoneID:        "gym-floor",
		NodeID:        "entry-node",
		ObservedAt:    "2026-04-04T12:00:00Z",
		Result:        "fail",
		AccountType:   "Standard",
		Name:          "Fixture Student",
		StatusMessage: "No active access rule for this account.",
	})
	if err != nil {
		t.Fatalf("AcceptTap() error = %v", err)
	}

	if result.Status != "accepted" {
		t.Fatalf("result.Status = %q, want accepted", result.Status)
	}
	if result.Result != "fail" {
		t.Fatalf("result.Result = %q, want fail", result.Result)
	}
	if !result.Published {
		t.Fatal("result.Published = false, want true")
	}
	if len(publisher.messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(publisher.messages))
	}
	if len(recorder.records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(recorder.records))
	}
	if recorder.records[0].FailureReasonCode != FailureReasonRecognizedDenied {
		t.Fatalf("FailureReasonCode = %q, want %q", recorder.records[0].FailureReasonCode, FailureReasonRecognizedDenied)
	}
	if len(recorder.commits) != 0 {
		t.Fatalf("len(commits) = %d, want 0", len(recorder.commits))
	}
	if len(recorder.acceptances) != 1 {
		t.Fatalf("len(acceptances) = %d, want 1", len(recorder.acceptances))
	}
	if recorder.acceptances[0].AcceptancePath != AcceptancePathFacility {
		t.Fatalf("AcceptancePath = %q, want %q", recorder.acceptances[0].AcceptancePath, AcceptancePathFacility)
	}
	if recorder.acceptances[0].AcceptedReasonCode != "testing_rollout" {
		t.Fatalf("AcceptedReasonCode = %q, want testing_rollout", recorder.acceptances[0].AcceptedReasonCode)
	}

	snapshot, err := projector.CurrentOccupancy(context.Background(), domain.OccupancyFilter{
		FacilityID: "ashtonbee",
		ZoneID:     "gym-floor",
	})
	if err != nil {
		t.Fatalf("CurrentOccupancy() error = %v", err)
	}
	if snapshot.CurrentCount != 1 {
		t.Fatalf("snapshot.CurrentCount = %d, want 1", snapshot.CurrentCount)
	}
}

func TestAcceptTapRecordsDurableObservationsForPassAndFail(t *testing.T) {
	publisher := &stubPublisher{}
	recorder := &stubObservationRecorder{}
	service, err := NewService(
		publisher,
		"salt",
		map[string]string{"entry-node": "entry-token"},
		WithObservationRecorder(recorder),
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	passReq := TapRequest{
		EventID:       "edge-pass-001",
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
	if _, err := service.AcceptTap(context.Background(), "entry-token", passReq); err != nil {
		t.Fatalf("AcceptTap(pass) error = %v", err)
	}

	failReq := TapRequest{
		EventID:       "edge-fail-002",
		AccountRaw:    "301536642",
		Direction:     "out",
		FacilityID:    "ashtonbee",
		NodeID:        "entry-node",
		ObservedAt:    "2026-04-04T12:05:00Z",
		Result:        "fail",
		AccountType:   "ISO",
		Name:          "Another Student",
		StatusMessage: "Access denied, no rule matches Account.",
	}
	if _, err := service.AcceptTap(context.Background(), "entry-token", failReq); err != nil {
		t.Fatalf("AcceptTap(fail) error = %v", err)
	}

	if len(recorder.records) != 2 {
		t.Fatalf("len(records) = %d, want 2", len(recorder.records))
	}
	if len(recorder.commits) != 1 {
		t.Fatalf("len(commits) = %d, want 1", len(recorder.commits))
	}

	passRecord := recorder.records[0]
	if passRecord.EventID != passReq.EventID {
		t.Fatalf("passRecord.EventID = %q, want %q", passRecord.EventID, passReq.EventID)
	}
	if passRecord.ExternalIdentityHash != HashAccount(passReq.AccountRaw, "salt") {
		t.Fatalf("passRecord.ExternalIdentityHash = %q, want hashed account", passRecord.ExternalIdentityHash)
	}
	if passRecord.AccountType != "Standard" {
		t.Fatalf("passRecord.AccountType = %q, want Standard", passRecord.AccountType)
	}
	if passRecord.CommittedAt != nil {
		t.Fatalf("passRecord.CommittedAt = %v, want nil on observed row", passRecord.CommittedAt)
	}
	if !passRecord.NamePresent {
		t.Fatal("passRecord.NamePresent = false, want true")
	}
	if passRecord.StoredAt.IsZero() {
		t.Fatal("passRecord.StoredAt = zero, want durable timestamp")
	}
	if passRecord.ObservationID == "" {
		t.Fatal("passRecord.ObservationID = empty, want immutable durable observation identity")
	}

	failRecord := recorder.records[1]
	if failRecord.Result != "fail" {
		t.Fatalf("failRecord.Result = %q, want fail", failRecord.Result)
	}
	if failRecord.Direction != domain.DirectionOut {
		t.Fatalf("failRecord.Direction = %q, want out", failRecord.Direction)
	}
	if failRecord.ExternalIdentityHash != HashAccount(failReq.AccountRaw, "salt") {
		t.Fatalf("failRecord.ExternalIdentityHash = %q, want hashed account", failRecord.ExternalIdentityHash)
	}
	if recorder.commits[0].EventID != passReq.EventID {
		t.Fatalf("commit.EventID = %q, want %q", recorder.commits[0].EventID, passReq.EventID)
	}
	if recorder.commits[0].ObservationID == "" {
		t.Fatal("commit.ObservationID = empty, want durable observation identity")
	}
	if recorder.commits[0].ObservationID != passRecord.ObservationID {
		t.Fatalf("commit.ObservationID = %q, want %q", recorder.commits[0].ObservationID, passRecord.ObservationID)
	}
	if recorder.commits[0].CommittedAt.IsZero() {
		t.Fatal("commit.CommittedAt = zero, want durable commit timestamp")
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
		StatusMessage: "Access entry granted for Fixture Student 1000123456.",
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
	if strings.Contains(logOutput, req.StatusMessage) {
		t.Fatalf("log output leaked status_message: %s", logOutput)
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
		StatusMessage: "Access entry granted for Fixture Student 1000123456.",
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
	if strings.Contains(logOutput, req.StatusMessage) {
		t.Fatalf("log output leaked status_message: %s", logOutput)
	}
	if !strings.Contains(logOutput, HashAccount(req.AccountRaw, "salt")) {
		t.Fatalf("log output missing external_identity_hash: %s", logOutput)
	}
}

func TestAcceptTapShadowWriteFailureDoesNotChangeAcceptOutcome(t *testing.T) {
	logs := captureSlog(t)

	publisher := &stubPublisher{}
	recorder := &stubObservationRecorder{err: errors.New("disk full")}
	service, err := NewService(
		publisher,
		"salt",
		map[string]string{"entry-node": "entry-token"},
		WithObservationRecorder(recorder),
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	req := TapRequest{
		EventID:       "edge-shadow-write-001",
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

	result, err := service.AcceptTap(context.Background(), "entry-token", req)
	if err != nil {
		t.Fatalf("AcceptTap() error = %v", err)
	}
	if result.Status != "accepted" || !result.Published {
		t.Fatalf("result = %#v, want accepted published result", result)
	}
	if len(publisher.messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(publisher.messages))
	}
	if len(recorder.records) != 0 {
		t.Fatalf("len(records) = %d, want 0 after failed durable write", len(recorder.records))
	}
	if len(recorder.commits) != 0 {
		t.Fatalf("len(commits) = %d, want 0 after failed durable write", len(recorder.commits))
	}

	logOutput := logs.String()
	if !strings.Contains(logOutput, "edge observation durable write failed") {
		t.Fatalf("log output = %q, want durable write failure message", logOutput)
	}
	if strings.Contains(logOutput, req.AccountRaw) {
		t.Fatalf("log output leaked raw account number: %s", logOutput)
	}
	if strings.Contains(logOutput, req.Name) {
		t.Fatalf("log output leaked resolved name: %s", logOutput)
	}
}

func TestAcceptTapPublishFailureDoesNotRecordDurableCommitMarker(t *testing.T) {
	publisher := &stubPublisher{err: errors.New("nats unavailable")}
	recorder := &stubObservationRecorder{}
	service, err := NewService(
		publisher,
		"salt",
		map[string]string{"entry-node": "entry-token"},
		WithObservationRecorder(recorder),
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	req := TapRequest{
		EventID:       "edge-commit-fail-001",
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
	} else if !errors.Is(err, ErrPublishUnavailable) {
		t.Fatalf("AcceptTap() error = %v, want ErrPublishUnavailable", err)
	}

	if len(recorder.records) != 1 {
		t.Fatalf("len(records) = %d, want 1 observed row", len(recorder.records))
	}
	if len(recorder.commits) != 0 {
		t.Fatalf("len(commits) = %d, want 0 commit markers", len(recorder.commits))
	}
}

func TestAcceptTapDurableRecordDropsFreeTextStatusMessage(t *testing.T) {
	recorder := &stubObservationRecorder{}
	service, err := NewService(
		&stubPublisher{},
		"salt",
		map[string]string{"entry-node": "entry-token"},
		WithObservationRecorder(recorder),
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	req := TapRequest{
		EventID:       "edge-status-redaction-001",
		AccountRaw:    "1000123456",
		Direction:     "in",
		FacilityID:    "ashtonbee",
		NodeID:        "entry-node",
		ObservedAt:    "2026-04-04T12:00:00Z",
		Result:        "fail",
		AccountType:   "Standard",
		Name:          "Fixture Student",
		StatusMessage: "Denied for Fixture Student 1000123456",
	}

	if _, err := service.AcceptTap(context.Background(), "entry-token", req); err != nil {
		t.Fatalf("AcceptTap() error = %v", err)
	}

	if len(recorder.records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(recorder.records))
	}
	if recorder.records[0].AccountType != "Standard" {
		t.Fatalf("AccountType = %q, want Standard", recorder.records[0].AccountType)
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

func TestAcceptTapWithProjectionRejectsOlderInEventAfterDurableMarkerLookup(t *testing.T) {
	publisher := &stubPublisher{}
	projector := presence.NewProjector(
		presence.WithProjectionMarkerResolver(func(context.Context, domain.PresenceEvent) (presence.ProjectionMarker, bool, error) {
			return presence.ProjectionMarker{
				RecordedAt: time.Date(2026, 4, 4, 12, 1, 0, 0, time.UTC),
				EventID:    "edge-newer-marker",
			}, true, nil
		}),
	)
	service, err := NewService(
		publisher,
		"salt",
		map[string]string{"entry-node": "entry-token"},
		WithProjection(projector),
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	result, err := service.AcceptTap(context.Background(), "entry-token", TapRequest{
		EventID:    "edge-old-in",
		AccountRaw: "1000123456",
		Direction:  "in",
		FacilityID: "ashtonbee",
		ZoneID:     "gym-floor",
		NodeID:     "entry-node",
		ObservedAt: "2026-04-04T12:00:00Z",
		Result:     "pass",
	})
	if err != nil {
		t.Fatalf("AcceptTap() error = %v", err)
	}
	if result.Status != "observed" {
		t.Fatalf("result.Status = %q, want observed", result.Status)
	}
	if result.Published {
		t.Fatal("result.Published = true, want false")
	}
	if result.Result != "pass" {
		t.Fatalf("result.Result = %q, want pass", result.Result)
	}
	if len(publisher.messages) != 0 {
		t.Fatalf("len(messages) = %d, want 0", len(publisher.messages))
	}

	snapshot, err := projector.CurrentOccupancy(context.Background(), domain.OccupancyFilter{
		FacilityID: "ashtonbee",
		ZoneID:     "gym-floor",
	})
	if err != nil {
		t.Fatalf("CurrentOccupancy() error = %v", err)
	}
	if snapshot.CurrentCount != 0 {
		t.Fatalf("current_count = %d, want 0 after stale marker rejection", snapshot.CurrentCount)
	}
}

func TestAcceptTapWithProjectionFailsClosedOnDurableMarkerLookupError(t *testing.T) {
	publisher := &stubPublisher{}
	projector := presence.NewProjector(
		presence.WithProjectionMarkerResolver(func(context.Context, domain.PresenceEvent) (presence.ProjectionMarker, bool, error) {
			return presence.ProjectionMarker{}, false, context.DeadlineExceeded
		}),
	)
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
		EventID:    "edge-old-in",
		AccountRaw: "1000123456",
		Direction:  "in",
		FacilityID: "ashtonbee",
		ZoneID:     "gym-floor",
		NodeID:     "entry-node",
		ObservedAt: "2026-04-04T12:00:00Z",
		Result:     "pass",
	}); err == nil {
		t.Fatal("AcceptTap() error = nil, want durable marker lookup failure")
	}
	if len(publisher.messages) != 0 {
		t.Fatalf("len(messages) = %d, want 0 on failed lookup", len(publisher.messages))
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
