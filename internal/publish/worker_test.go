package publish

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ixxet/athena/internal/domain"
)

type stubAdapter struct {
	events []domain.PresenceEvent
}

type flakyPublisher struct {
	failOnCalls map[int]error
	calls       int
	messages    []publishedMessage
}

func (s stubAdapter) Name() string {
	return "stub"
}

func (s stubAdapter) ListEvents(context.Context) ([]domain.PresenceEvent, error) {
	return s.events, nil
}

func (p *flakyPublisher) Publish(_ context.Context, subject string, payload []byte) error {
	p.calls++
	if err, ok := p.failOnCalls[p.calls]; ok {
		return err
	}

	p.messages = append(p.messages, publishedMessage{
		subject: subject,
		payload: payload,
	})
	return nil
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

func TestWorkerRunOnceRetriesRemainingMessagesAfterTransientFailure(t *testing.T) {
	recordedAt := time.Date(2026, 4, 1, 12, 30, 0, 0, time.UTC)
	publisher := &flakyPublisher{
		failOnCalls: map[int]error{
			2: errors.New("broker unavailable"),
		},
	}
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
		WithRetryPolicy(2, time.Millisecond, time.Millisecond),
		WithSleep(func(context.Context, time.Duration) error { return nil }),
	)

	published, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	if published != 2 {
		t.Fatalf("RunOnce() published = %d, want 2", published)
	}
	if publisher.calls != 3 {
		t.Fatalf("publisher.calls = %d, want 3", publisher.calls)
	}
	if len(publisher.messages) != 2 {
		t.Fatalf("len(publisher.messages) = %d, want 2", len(publisher.messages))
	}
}

func TestWorkerRunOnceBoundsSeenMemory(t *testing.T) {
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
		WithSeenLimit(1),
	)

	published, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	if published != 2 {
		t.Fatalf("RunOnce() published = %d, want 2", published)
	}
	if len(worker.seen) != 1 {
		t.Fatalf("len(worker.seen) = %d, want 1", len(worker.seen))
	}
	if _, ok := worker.seen["mock-out-001"]; !ok {
		t.Fatalf("worker.seen = %#v, want latest message only", worker.seen)
	}
}
