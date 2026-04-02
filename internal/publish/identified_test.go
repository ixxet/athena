package publish

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	protoevents "github.com/ixxet/ashton-proto/events"
	"github.com/ixxet/athena/internal/domain"
)

type stubPublisher struct {
	messages []publishedMessage
	err      error
}

type publishedMessage struct {
	subject string
	payload []byte
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

func TestBuildBatchFiltersToIdentifiedPresenceEvents(t *testing.T) {
	arrivalRecordedAt := time.Date(2026, 4, 1, 12, 30, 0, 0, time.UTC)
	departureRecordedAt := time.Date(2026, 4, 1, 12, 45, 0, 0, time.UTC)
	batch, err := BuildBatch([]domain.PresenceEvent{
		{
			ID:                   "mock-in-001",
			FacilityID:           "ashtonbee",
			ZoneID:               "weight-room",
			ExternalIdentityHash: "tag_tracer2_001",
			Direction:            domain.DirectionIn,
			Source:               domain.SourceMock,
			RecordedAt:           arrivalRecordedAt,
		},
		{
			ID:         "mock-in-002",
			FacilityID: "ashtonbee",
			Direction:  domain.DirectionIn,
			Source:     domain.SourceMock,
			RecordedAt: arrivalRecordedAt,
		},
		{
			ID:                   "mock-out-001",
			FacilityID:           "ashtonbee",
			ZoneID:               "weight-room",
			ExternalIdentityHash: "tag_tracer5_001",
			Direction:            domain.DirectionOut,
			Source:               domain.SourceMock,
			RecordedAt:           departureRecordedAt,
		},
		{
			ID:         "mock-out-002",
			FacilityID: "ashtonbee",
			Direction:  domain.DirectionOut,
			Source:     domain.SourceMock,
			RecordedAt: departureRecordedAt,
		},
	})
	if err != nil {
		t.Fatalf("BuildBatch() error = %v", err)
	}

	if len(batch) != 2 {
		t.Fatalf("len(batch) = %d, want 2", len(batch))
	}
	if batch[0].Subject != protoevents.SubjectIdentifiedPresenceArrived {
		t.Fatalf("batch[0].Subject = %q, want %q", batch[0].Subject, protoevents.SubjectIdentifiedPresenceArrived)
	}
	if string(batch[0].Payload) != string(protoevents.ValidIdentifiedPresenceArrivedFixture()) {
		t.Fatalf("batch[0].Payload = %s, want fixture %s", batch[0].Payload, protoevents.ValidIdentifiedPresenceArrivedFixture())
	}
	if batch[1].Subject != protoevents.SubjectIdentifiedPresenceDeparted {
		t.Fatalf("batch[1].Subject = %q, want %q", batch[1].Subject, protoevents.SubjectIdentifiedPresenceDeparted)
	}
	if string(batch[1].Payload) != string(protoevents.ValidIdentifiedPresenceDepartedFixture()) {
		t.Fatalf("batch[1].Payload = %s, want fixture %s", batch[1].Payload, protoevents.ValidIdentifiedPresenceDepartedFixture())
	}
}

func TestBuildBatchRejectsMalformedIdentifiedArrival(t *testing.T) {
	_, err := BuildBatch([]domain.PresenceEvent{
		{
			ID:                   "mock-in-001",
			ExternalIdentityHash: "tag_tracer2_001",
			Direction:            domain.DirectionIn,
			Source:               domain.SourceMock,
			RecordedAt:           time.Date(2026, 4, 1, 12, 30, 0, 0, time.UTC),
		},
	})
	if err == nil {
		t.Fatal("BuildBatch() error = nil, want malformed identified arrival error")
	}
}

func TestPublishBatchReturnsBrokerUnavailableError(t *testing.T) {
	publisher := &stubPublisher{err: errors.New("broker unavailable")}
	batch := []Message{{
		ID:      "mock-in-001",
		Subject: protoevents.SubjectIdentifiedPresenceArrived,
		Payload: []byte(`{"id":"mock-in-001"}`),
	}}

	published, err := PublishBatch(context.Background(), publisher, batch)
	if err == nil {
		t.Fatal("PublishBatch() error = nil, want broker unavailable error")
	}
	if !strings.Contains(err.Error(), "publish identified presence") {
		t.Fatalf("PublishBatch() error = %v, want publish context", err)
	}
	if published != 0 {
		t.Fatalf("PublishBatch() published = %d, want 0", published)
	}
}

func TestBuildBatchRejectsUnsupportedSource(t *testing.T) {
	_, err := BuildBatch([]domain.PresenceEvent{
		{
			ID:                   "mock-in-001",
			FacilityID:           "ashtonbee",
			ExternalIdentityHash: "tag_tracer2_001",
			Direction:            domain.DirectionIn,
			Source:               domain.PresenceSource("infrared"),
			RecordedAt:           time.Date(2026, 4, 1, 12, 30, 0, 0, time.UTC),
		},
	})
	if err == nil {
		t.Fatal("BuildBatch() error = nil, want unsupported source error")
	}
}

func TestBuildDirectionalBatchFiltersToIdentifiedDepartures(t *testing.T) {
	recordedAt := time.Date(2026, 4, 1, 12, 45, 0, 0, time.UTC)
	batch, err := BuildDirectionalBatch([]domain.PresenceEvent{
		{
			ID:                   "mock-in-001",
			FacilityID:           "ashtonbee",
			ExternalIdentityHash: "tag_tracer2_001",
			Direction:            domain.DirectionIn,
			Source:               domain.SourceMock,
			RecordedAt:           recordedAt,
		},
		{
			ID:                   "mock-out-001",
			FacilityID:           "ashtonbee",
			ZoneID:               "weight-room",
			ExternalIdentityHash: "tag_tracer5_001",
			Direction:            domain.DirectionOut,
			Source:               domain.SourceMock,
			RecordedAt:           recordedAt,
		},
	}, domain.DirectionOut)
	if err != nil {
		t.Fatalf("BuildDirectionalBatch() error = %v", err)
	}

	if len(batch) != 1 {
		t.Fatalf("len(batch) = %d, want 1", len(batch))
	}
	if batch[0].Subject != protoevents.SubjectIdentifiedPresenceDeparted {
		t.Fatalf("batch[0].Subject = %q, want %q", batch[0].Subject, protoevents.SubjectIdentifiedPresenceDeparted)
	}
	if string(batch[0].Payload) != string(protoevents.ValidIdentifiedPresenceDepartedFixture()) {
		t.Fatalf("batch[0].Payload = %s, want fixture %s", batch[0].Payload, protoevents.ValidIdentifiedPresenceDepartedFixture())
	}
}

func TestBuildBatchRejectsMalformedIdentifiedDeparture(t *testing.T) {
	_, err := BuildBatch([]domain.PresenceEvent{
		{
			ID:                   "mock-out-001",
			ExternalIdentityHash: "tag_tracer5_001",
			Direction:            domain.DirectionOut,
			Source:               domain.SourceMock,
			RecordedAt:           time.Date(2026, 4, 1, 12, 45, 0, 0, time.UTC),
		},
	})
	if err == nil {
		t.Fatal("BuildBatch() error = nil, want malformed identified departure error")
	}
}

func TestFlushContextAddsDeadlineWhenMissing(t *testing.T) {
	ctx, cancel := flushContext(context.Background(), time.Second)
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("flushContext() deadline missing, want timeout-backed context")
	}
	if time.Until(deadline) <= 0 {
		t.Fatalf("flushContext() deadline = %s, want future deadline", deadline)
	}
}

func TestFlushContextPreservesExistingDeadline(t *testing.T) {
	parent, parentCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer parentCancel()

	ctx, cancel := flushContext(parent, time.Second)
	defer cancel()

	parentDeadline, parentOK := parent.Deadline()
	deadline, ok := ctx.Deadline()
	if !parentOK || !ok {
		t.Fatal("flushContext() deadline missing, want existing deadline preserved")
	}
	if !deadline.Equal(parentDeadline) {
		t.Fatalf("flushContext() deadline = %s, want %s", deadline, parentDeadline)
	}
}
