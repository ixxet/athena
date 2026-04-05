package presence

import (
	"context"
	"testing"
	"time"

	"github.com/ixxet/athena/internal/domain"
)

func TestProjectorFirstInAppliesAndIncrementsFacilityAndZone(t *testing.T) {
	now := time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)
	projector := NewProjectorWithClock(func() time.Time { return now })

	result, err := projector.Apply(testProjectedEvent("edge-001", "ashtonbee", "gym", "tag-001", domain.DirectionIn, now))
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if !result.Applied {
		t.Fatalf("result.Applied = false, want true (reason=%q)", result.Reason)
	}
	if result.Reason != "entered" {
		t.Fatalf("result.Reason = %q, want entered", result.Reason)
	}

	facilitySnapshot, err := projector.CurrentOccupancy(context.Background(), domain.OccupancyFilter{FacilityID: "ashtonbee"})
	if err != nil {
		t.Fatalf("CurrentOccupancy(facility) error = %v", err)
	}
	if facilitySnapshot.CurrentCount != 1 {
		t.Fatalf("facility current_count = %d, want 1", facilitySnapshot.CurrentCount)
	}

	zoneSnapshot, err := projector.CurrentOccupancy(context.Background(), domain.OccupancyFilter{FacilityID: "ashtonbee", ZoneID: "gym"})
	if err != nil {
		t.Fatalf("CurrentOccupancy(zone) error = %v", err)
	}
	if zoneSnapshot.CurrentCount != 1 {
		t.Fatalf("zone current_count = %d, want 1", zoneSnapshot.CurrentCount)
	}
}

func TestProjectorRepeatedInDoesNotInflate(t *testing.T) {
	now := time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)
	projector := NewProjectorWithClock(func() time.Time { return now })

	if _, err := projector.Apply(testProjectedEvent("edge-001", "ashtonbee", "gym", "tag-001", domain.DirectionIn, now)); err != nil {
		t.Fatalf("first Apply() error = %v", err)
	}

	result, err := projector.Apply(testProjectedEvent("edge-002", "ashtonbee", "gym", "tag-001", domain.DirectionIn, now.Add(time.Minute)))
	if err != nil {
		t.Fatalf("second Apply() error = %v", err)
	}
	if result.Applied {
		t.Fatalf("result.Applied = true, want false (reason=%q)", result.Reason)
	}
	if result.Reason != "already_present" {
		t.Fatalf("result.Reason = %q, want already_present", result.Reason)
	}

	snapshot, err := projector.CurrentOccupancy(context.Background(), domain.OccupancyFilter{FacilityID: "ashtonbee", ZoneID: "gym"})
	if err != nil {
		t.Fatalf("CurrentOccupancy() error = %v", err)
	}
	if snapshot.CurrentCount != 1 {
		t.Fatalf("current_count = %d, want 1", snapshot.CurrentCount)
	}
}

func TestProjectorOutAfterInDecrements(t *testing.T) {
	now := time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)
	projector := NewProjectorWithClock(func() time.Time { return now })

	if _, err := projector.Apply(testProjectedEvent("edge-001", "ashtonbee", "gym", "tag-001", domain.DirectionIn, now)); err != nil {
		t.Fatalf("entry Apply() error = %v", err)
	}

	result, err := projector.Apply(testProjectedEvent("edge-002", "ashtonbee", "gym", "tag-001", domain.DirectionOut, now.Add(time.Minute)))
	if err != nil {
		t.Fatalf("exit Apply() error = %v", err)
	}
	if !result.Applied {
		t.Fatalf("result.Applied = false, want true (reason=%q)", result.Reason)
	}
	if result.Reason != "exited" {
		t.Fatalf("result.Reason = %q, want exited", result.Reason)
	}

	snapshot, err := projector.CurrentOccupancy(context.Background(), domain.OccupancyFilter{FacilityID: "ashtonbee", ZoneID: "gym"})
	if err != nil {
		t.Fatalf("CurrentOccupancy() error = %v", err)
	}
	if snapshot.CurrentCount != 0 {
		t.Fatalf("current_count = %d, want 0", snapshot.CurrentCount)
	}
}

func TestProjectorRepeatedOutRemainsDeterministic(t *testing.T) {
	now := time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)
	projector := NewProjectorWithClock(func() time.Time { return now })

	result, err := projector.Apply(testProjectedEvent("edge-001", "ashtonbee", "gym", "tag-001", domain.DirectionOut, now))
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if result.Applied {
		t.Fatalf("result.Applied = true, want false (reason=%q)", result.Reason)
	}
	if result.Reason != "already_absent" {
		t.Fatalf("result.Reason = %q, want already_absent", result.Reason)
	}

	snapshot, err := projector.CurrentOccupancy(context.Background(), domain.OccupancyFilter{FacilityID: "ashtonbee", ZoneID: "gym"})
	if err != nil {
		t.Fatalf("CurrentOccupancy() error = %v", err)
	}
	if snapshot.CurrentCount != 0 {
		t.Fatalf("current_count = %d, want 0", snapshot.CurrentCount)
	}
}

func TestProjectorKeepsFacilitiesSeparated(t *testing.T) {
	now := time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)
	projector := NewProjectorWithClock(func() time.Time { return now })

	if _, err := projector.Apply(testProjectedEvent("edge-001", "ashtonbee", "", "tag-001", domain.DirectionIn, now)); err != nil {
		t.Fatalf("Apply(ashtonbee) error = %v", err)
	}
	if _, err := projector.Apply(testProjectedEvent("edge-002", "other", "", "tag-001", domain.DirectionIn, now.Add(time.Minute))); err != nil {
		t.Fatalf("Apply(other) error = %v", err)
	}

	ashtonbeeSnapshot, err := projector.CurrentOccupancy(context.Background(), domain.OccupancyFilter{FacilityID: "ashtonbee"})
	if err != nil {
		t.Fatalf("CurrentOccupancy(ashtonbee) error = %v", err)
	}
	if ashtonbeeSnapshot.CurrentCount != 1 {
		t.Fatalf("ashtonbee current_count = %d, want 1", ashtonbeeSnapshot.CurrentCount)
	}

	otherSnapshot, err := projector.CurrentOccupancy(context.Background(), domain.OccupancyFilter{FacilityID: "other"})
	if err != nil {
		t.Fatalf("CurrentOccupancy(other) error = %v", err)
	}
	if otherSnapshot.CurrentCount != 1 {
		t.Fatalf("other current_count = %d, want 1", otherSnapshot.CurrentCount)
	}
}

func TestProjectorKeepsZonesSeparated(t *testing.T) {
	now := time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)
	projector := NewProjectorWithClock(func() time.Time { return now })

	if _, err := projector.Apply(testProjectedEvent("edge-001", "ashtonbee", "gym", "tag-001", domain.DirectionIn, now)); err != nil {
		t.Fatalf("Apply(gym) error = %v", err)
	}
	if _, err := projector.Apply(testProjectedEvent("edge-002", "ashtonbee", "pool", "tag-002", domain.DirectionIn, now.Add(time.Minute))); err != nil {
		t.Fatalf("Apply(pool) error = %v", err)
	}

	facilitySnapshot, err := projector.CurrentOccupancy(context.Background(), domain.OccupancyFilter{FacilityID: "ashtonbee"})
	if err != nil {
		t.Fatalf("CurrentOccupancy(facility) error = %v", err)
	}
	if facilitySnapshot.CurrentCount != 2 {
		t.Fatalf("facility current_count = %d, want 2", facilitySnapshot.CurrentCount)
	}

	gymSnapshot, err := projector.CurrentOccupancy(context.Background(), domain.OccupancyFilter{FacilityID: "ashtonbee", ZoneID: "gym"})
	if err != nil {
		t.Fatalf("CurrentOccupancy(gym) error = %v", err)
	}
	if gymSnapshot.CurrentCount != 1 {
		t.Fatalf("gym current_count = %d, want 1", gymSnapshot.CurrentCount)
	}

	poolSnapshot, err := projector.CurrentOccupancy(context.Background(), domain.OccupancyFilter{FacilityID: "ashtonbee", ZoneID: "pool"})
	if err != nil {
		t.Fatalf("CurrentOccupancy(pool) error = %v", err)
	}
	if poolSnapshot.CurrentCount != 1 {
		t.Fatalf("pool current_count = %d, want 1", poolSnapshot.CurrentCount)
	}
}

func TestProjectorRejectsStaleEventOrder(t *testing.T) {
	now := time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)
	projector := NewProjectorWithClock(func() time.Time { return now })

	if _, err := projector.Apply(testProjectedEvent("edge-002", "ashtonbee", "gym", "tag-001", domain.DirectionIn, now.Add(time.Minute))); err != nil {
		t.Fatalf("Apply(newer) error = %v", err)
	}

	result, err := projector.Apply(testProjectedEvent("edge-001", "ashtonbee", "gym", "tag-001", domain.DirectionOut, now))
	if err != nil {
		t.Fatalf("Apply(stale) error = %v", err)
	}
	if result.Applied {
		t.Fatalf("result.Applied = true, want false (reason=%q)", result.Reason)
	}
	if result.Reason != "stale" {
		t.Fatalf("result.Reason = %q, want stale", result.Reason)
	}

	snapshot, err := projector.CurrentOccupancy(context.Background(), domain.OccupancyFilter{FacilityID: "ashtonbee", ZoneID: "gym"})
	if err != nil {
		t.Fatalf("CurrentOccupancy() error = %v", err)
	}
	if snapshot.CurrentCount != 1 {
		t.Fatalf("current_count = %d, want 1", snapshot.CurrentCount)
	}
}

func testProjectedEvent(id, facilityID, zoneID, identityHash string, direction domain.PresenceDirection, recordedAt time.Time) domain.PresenceEvent {
	return domain.PresenceEvent{
		ID:                   id,
		FacilityID:           facilityID,
		ZoneID:               zoneID,
		ExternalIdentityHash: identityHash,
		Direction:            direction,
		Source:               domain.SourceRFID,
		RecordedAt:           recordedAt.UTC(),
	}
}
