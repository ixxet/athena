package presence

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

func TestCurrentOccupancyCountsMatchingEvents(t *testing.T) {
	service := NewService(stubAdapter{
		events: []domain.PresenceEvent{
			{
				ID:         "1",
				FacilityID: "ashtonbee",
				Direction:  domain.DirectionIn,
				RecordedAt: time.Now().UTC().Add(-3 * time.Minute),
			},
			{
				ID:         "2",
				FacilityID: "ashtonbee",
				Direction:  domain.DirectionIn,
				RecordedAt: time.Now().UTC().Add(-2 * time.Minute),
			},
			{
				ID:         "3",
				FacilityID: "ashtonbee",
				Direction:  domain.DirectionOut,
				RecordedAt: time.Now().UTC().Add(-time.Minute),
			},
			{
				ID:         "4",
				FacilityID: "other",
				Direction:  domain.DirectionIn,
				RecordedAt: time.Now().UTC(),
			},
		},
	})

	snapshot, err := service.CurrentOccupancy(context.Background(), domain.OccupancyFilter{
		FacilityID: "ashtonbee",
	})
	if err != nil {
		t.Fatalf("CurrentOccupancy() error = %v", err)
	}

	if snapshot.CurrentCount != 1 {
		t.Fatalf("CurrentOccupancy() current_count = %d, want 1", snapshot.CurrentCount)
	}
}
