package publish

import (
	"context"
	"testing"
	"time"

	"github.com/ixxet/athena/internal/domain"
)

type stubAdapter struct {
	events []domain.PresenceEvent
}

func (s stubAdapter) Name() string {
	return "stub"
}

func (s stubAdapter) ListEvents(context.Context) ([]domain.PresenceEvent, error) {
	return s.events, nil
}

func TestWorkerRunOnceDedupesPublishedEvents(t *testing.T) {
	recordedAt := time.Date(2026, 4, 1, 12, 30, 0, 0, time.UTC)
	publisher := &stubPublisher{}
	worker := NewWorker(
		NewService(stubAdapter{
			events: []domain.PresenceEvent{
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
					ExternalIdentityHash: "tag_tracer5_001",
					Direction:            domain.DirectionOut,
					Source:               domain.SourceMock,
					RecordedAt:           recordedAt.Add(time.Minute),
				},
			},
		}, publisher),
		time.Second,
	)

	firstPublished, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("first RunOnce() error = %v", err)
	}
	secondPublished, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("second RunOnce() error = %v", err)
	}

	if firstPublished != 2 {
		t.Fatalf("first RunOnce() published = %d, want 2", firstPublished)
	}
	if secondPublished != 0 {
		t.Fatalf("second RunOnce() published = %d, want 0", secondPublished)
	}
	if len(publisher.messages) != 2 {
		t.Fatalf("len(publisher.messages) = %d, want 2", len(publisher.messages))
	}
}
