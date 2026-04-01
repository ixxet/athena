package presence

import (
	"context"
	"testing"

	"github.com/ixxet/athena/internal/domain"
)

func TestReadPathUsesDefaultFacilityWhenMissing(t *testing.T) {
	readPath := NewReadPath(
		NewService(stubAdapter{
			events: []domain.PresenceEvent{
				{ID: "1", FacilityID: "ashtonbee", Direction: domain.DirectionIn},
				{ID: "2", FacilityID: "other", Direction: domain.DirectionIn},
			},
		}),
		domain.OccupancyFilter{FacilityID: "ashtonbee"},
	)

	snapshot, err := readPath.CurrentOccupancy(context.Background(), domain.OccupancyFilter{})
	if err != nil {
		t.Fatalf("CurrentOccupancy() error = %v", err)
	}

	if snapshot.FacilityID != "ashtonbee" {
		t.Fatalf("CurrentOccupancy() facility_id = %q, want %q", snapshot.FacilityID, "ashtonbee")
	}
	if snapshot.CurrentCount != 1 {
		t.Fatalf("CurrentOccupancy() current_count = %d, want 1", snapshot.CurrentCount)
	}
}

func TestReadPathPreservesExplicitFacilityFilter(t *testing.T) {
	readPath := NewReadPath(
		NewService(stubAdapter{
			events: []domain.PresenceEvent{
				{ID: "1", FacilityID: "ashtonbee", Direction: domain.DirectionIn},
				{ID: "2", FacilityID: "other", Direction: domain.DirectionIn},
			},
		}),
		domain.OccupancyFilter{FacilityID: "ashtonbee"},
	)

	snapshot, err := readPath.CurrentOccupancy(context.Background(), domain.OccupancyFilter{
		FacilityID: "other",
	})
	if err != nil {
		t.Fatalf("CurrentOccupancy() error = %v", err)
	}

	if snapshot.FacilityID != "other" {
		t.Fatalf("CurrentOccupancy() facility_id = %q, want %q", snapshot.FacilityID, "other")
	}
	if snapshot.CurrentCount != 1 {
		t.Fatalf("CurrentOccupancy() current_count = %d, want 1", snapshot.CurrentCount)
	}
}
