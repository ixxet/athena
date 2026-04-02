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

func TestBuildBatchFiltersToIdentifiedArrivals(t *testing.T) {
	recordedAt := time.Date(2026, 4, 1, 12, 30, 0, 0, time.UTC)
	batch, err := BuildBatch([]domain.PresenceEvent{
		{
			ID:                   "mock-in-001",
			FacilityID:           "ashtonbee",
			ZoneID:               "weight-room",
			ExternalIdentityHash: "tag_tracer2_001",
			Direction:            domain.DirectionIn,
			Source:               domain.SourceMock,
			RecordedAt:           recordedAt,
		},
		{
			ID:         "mock-in-002",
			FacilityID: "ashtonbee",
			Direction:  domain.DirectionIn,
			Source:     domain.SourceMock,
			RecordedAt: recordedAt,
		},
		{
			ID:                   "mock-out-001",
			FacilityID:           "ashtonbee",
			ExternalIdentityHash: "tag_tracer2_001",
			Direction:            domain.DirectionOut,
			Source:               domain.SourceMock,
			RecordedAt:           recordedAt,
		},
	})
	if err != nil {
		t.Fatalf("BuildBatch() error = %v", err)
	}

	if len(batch) != 1 {
		t.Fatalf("len(batch) = %d, want 1", len(batch))
	}
	if batch[0].Subject != protoevents.SubjectIdentifiedPresenceArrived {
		t.Fatalf("batch[0].Subject = %q, want %q", batch[0].Subject, protoevents.SubjectIdentifiedPresenceArrived)
	}
	if string(batch[0].Payload) != string(protoevents.ValidIdentifiedPresenceArrivedFixture()) {
		t.Fatalf("batch[0].Payload = %s, want fixture %s", batch[0].Payload, protoevents.ValidIdentifiedPresenceArrivedFixture())
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
	if !strings.Contains(err.Error(), "publish identified arrival") {
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
