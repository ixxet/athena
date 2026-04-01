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

func TestCurrentPresenceStateCountsMatchingEvents(t *testing.T) {
	fixedNow := time.Date(2026, 4, 1, 8, 30, 0, 0, time.UTC)
	service := NewService(stubAdapter{
		events: []domain.PresenceEvent{
			{
				ID:         "1",
				FacilityID: "ashtonbee",
				Direction:  domain.DirectionIn,
				RecordedAt: fixedNow.Add(-3 * time.Minute),
			},
			{
				ID:         "2",
				FacilityID: "ashtonbee",
				Direction:  domain.DirectionIn,
				RecordedAt: fixedNow.Add(-2 * time.Minute),
			},
			{
				ID:         "3",
				FacilityID: "ashtonbee",
				Direction:  domain.DirectionOut,
				RecordedAt: fixedNow.Add(-time.Minute),
			},
			{
				ID:         "4",
				FacilityID: "other",
				Direction:  domain.DirectionIn,
				RecordedAt: fixedNow,
			},
		},
	}, WithClock(func() time.Time { return fixedNow }))

	state, err := service.CurrentPresenceState(context.Background(), domain.OccupancyFilter{
		FacilityID: "ashtonbee",
	})
	if err != nil {
		t.Fatalf("CurrentPresenceState() error = %v", err)
	}

	if state.Arrivals != 2 {
		t.Fatalf("CurrentPresenceState() arrivals = %d, want 2", state.Arrivals)
	}
	if state.Departures != 1 {
		t.Fatalf("CurrentPresenceState() departures = %d, want 1", state.Departures)
	}
	if state.CurrentCount() != 1 {
		t.Fatalf("CurrentPresenceState() current_count = %d, want 1", state.CurrentCount())
	}
	if !state.ObservedAt.Equal(fixedNow.Add(-time.Minute)) {
		t.Fatalf("CurrentPresenceState() observed_at = %s, want %s", state.ObservedAt, fixedNow.Add(-time.Minute))
	}
}

func TestCurrentOccupancyClampsAtZero(t *testing.T) {
	service := NewService(stubAdapter{
		events: []domain.PresenceEvent{
			{
				ID:         "1",
				FacilityID: "ashtonbee",
				Direction:  domain.DirectionOut,
				RecordedAt: time.Date(2026, 4, 1, 8, 0, 0, 0, time.UTC),
			},
			{
				ID:         "2",
				FacilityID: "ashtonbee",
				Direction:  domain.DirectionOut,
				RecordedAt: time.Date(2026, 4, 1, 8, 1, 0, 0, time.UTC),
			},
		},
	})

	snapshot, err := service.CurrentOccupancy(context.Background(), domain.OccupancyFilter{
		FacilityID: "ashtonbee",
	})
	if err != nil {
		t.Fatalf("CurrentOccupancy() error = %v", err)
	}

	if snapshot.CurrentCount != 0 {
		t.Fatalf("CurrentOccupancy() current_count = %d, want 0", snapshot.CurrentCount)
	}
}

func TestCurrentOccupancyKeepsFacilitiesIsolated(t *testing.T) {
	service := NewService(stubAdapter{
		events: []domain.PresenceEvent{
			{ID: "1", FacilityID: "ashtonbee", Direction: domain.DirectionIn, RecordedAt: time.Date(2026, 4, 1, 8, 0, 0, 0, time.UTC)},
			{ID: "2", FacilityID: "ashtonbee", Direction: domain.DirectionIn, RecordedAt: time.Date(2026, 4, 1, 8, 1, 0, 0, time.UTC)},
			{ID: "3", FacilityID: "other", Direction: domain.DirectionIn, RecordedAt: time.Date(2026, 4, 1, 8, 2, 0, 0, time.UTC)},
			{ID: "4", FacilityID: "other", Direction: domain.DirectionOut, RecordedAt: time.Date(2026, 4, 1, 8, 3, 0, 0, time.UTC)},
		},
	})

	snapshot, err := service.CurrentOccupancy(context.Background(), domain.OccupancyFilter{
		FacilityID: "ashtonbee",
	})
	if err != nil {
		t.Fatalf("CurrentOccupancy() error = %v", err)
	}

	if snapshot.CurrentCount != 2 {
		t.Fatalf("CurrentOccupancy() current_count = %d, want 2", snapshot.CurrentCount)
	}
}

func TestCurrentOccupancyReturnsZeroForEmptyEventSet(t *testing.T) {
	fixedNow := time.Date(2026, 4, 1, 8, 45, 0, 0, time.UTC)
	service := NewService(stubAdapter{}, WithClock(func() time.Time { return fixedNow }))

	snapshot, err := service.CurrentOccupancy(context.Background(), domain.OccupancyFilter{
		FacilityID: "ashtonbee",
	})
	if err != nil {
		t.Fatalf("CurrentOccupancy() error = %v", err)
	}

	if snapshot.CurrentCount != 0 {
		t.Fatalf("CurrentOccupancy() current_count = %d, want 0", snapshot.CurrentCount)
	}
	if !snapshot.ObservedAt.Equal(fixedNow) {
		t.Fatalf("CurrentOccupancy() observed_at = %s, want %s", snapshot.ObservedAt, fixedNow)
	}
}

func TestCurrentOccupancyReturnsZeroForUnknownFacility(t *testing.T) {
	service := NewService(stubAdapter{
		events: []domain.PresenceEvent{
			{ID: "1", FacilityID: "ashtonbee", Direction: domain.DirectionIn, RecordedAt: time.Date(2026, 4, 1, 8, 0, 0, 0, time.UTC)},
		},
	})

	snapshot, err := service.CurrentOccupancy(context.Background(), domain.OccupancyFilter{
		FacilityID: "missing",
	})
	if err != nil {
		t.Fatalf("CurrentOccupancy() error = %v", err)
	}

	if snapshot.CurrentCount != 0 {
		t.Fatalf("CurrentOccupancy() current_count = %d, want 0", snapshot.CurrentCount)
	}
	if snapshot.FacilityID != "missing" {
		t.Fatalf("CurrentOccupancy() facility_id = %q, want %q", snapshot.FacilityID, "missing")
	}
}

func TestCurrentOccupancySupportsHighVolume(t *testing.T) {
	events := make([]domain.PresenceEvent, 0, 100001)
	for i := 0; i < 100000; i++ {
		events = append(events, domain.PresenceEvent{
			ID:         "in",
			FacilityID: "ashtonbee",
			Direction:  domain.DirectionIn,
			RecordedAt: time.Date(2026, 4, 1, 8, 0, 0, 0, time.UTC),
		})
	}
	events = append(events, domain.PresenceEvent{
		ID:         "out",
		FacilityID: "ashtonbee",
		Direction:  domain.DirectionOut,
		RecordedAt: time.Date(2026, 4, 1, 8, 1, 0, 0, time.UTC),
	})

	service := NewService(stubAdapter{events: events})
	snapshot, err := service.CurrentOccupancy(context.Background(), domain.OccupancyFilter{
		FacilityID: "ashtonbee",
	})
	if err != nil {
		t.Fatalf("CurrentOccupancy() error = %v", err)
	}

	if snapshot.CurrentCount != 99999 {
		t.Fatalf("CurrentOccupancy() current_count = %d, want 99999", snapshot.CurrentCount)
	}
}
