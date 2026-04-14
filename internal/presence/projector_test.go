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

func TestProjectorConsultsDurableMarkerOnlyOnMiss(t *testing.T) {
	now := time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)
	calls := 0
	projector := NewProjectorWithClock(func() time.Time { return now },
		WithProjectionMarkerResolver(func(context.Context, domain.PresenceEvent) (ProjectionMarker, bool, error) {
			calls++
			return ProjectionMarker{}, false, nil
		}),
	)

	first, err := projector.Apply(testProjectedEvent("edge-001", "ashtonbee", "gym", "tag-001", domain.DirectionIn, now))
	if err != nil {
		t.Fatalf("Apply(first) error = %v", err)
	}
	if !first.Applied || first.Reason != "entered" {
		t.Fatalf("first result = %#v, want entered applied result", first)
	}
	if calls != 1 {
		t.Fatalf("marker lookup calls = %d, want 1 on miss", calls)
	}

	calls = 0
	second, err := projector.Apply(testProjectedEvent("edge-002", "ashtonbee", "gym", "tag-001", domain.DirectionIn, now.Add(time.Minute)))
	if err != nil {
		t.Fatalf("Apply(second) error = %v", err)
	}
	if second.Applied {
		t.Fatalf("second.Applied = true, want false (reason=%q)", second.Reason)
	}
	if second.Reason != "already_present" {
		t.Fatalf("second.Reason = %q, want already_present", second.Reason)
	}
	if calls != 0 {
		t.Fatalf("marker lookup calls = %d, want 0 on identity-state hit", calls)
	}
}

func TestProjectorRejectsOlderEventAgainstDurableMarker(t *testing.T) {
	now := time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)
	projector := NewProjectorWithClock(func() time.Time { return now },
		WithProjectionMarkerResolver(func(context.Context, domain.PresenceEvent) (ProjectionMarker, bool, error) {
			return ProjectionMarker{
				RecordedAt: now.Add(time.Minute),
				EventID:    "edge-002",
			}, true, nil
		}),
	)

	result, err := projector.Apply(testProjectedEvent("edge-001", "ashtonbee", "gym", "tag-001", domain.DirectionIn, now))
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
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
	if snapshot.CurrentCount != 0 {
		t.Fatalf("current_count = %d, want 0 after stale marker rejection", snapshot.CurrentCount)
	}
}

func TestProjectorKeepsOlderOutEventHarmlessAfterMarkerLookup(t *testing.T) {
	now := time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)
	projector := NewProjectorWithClock(func() time.Time { return now },
		WithProjectionMarkerResolver(func(context.Context, domain.PresenceEvent) (ProjectionMarker, bool, error) {
			return ProjectionMarker{
				RecordedAt: now.Add(time.Minute),
				EventID:    "edge-002",
			}, true, nil
		}),
	)

	result, err := projector.Apply(testProjectedEvent("edge-001", "ashtonbee", "gym", "tag-001", domain.DirectionOut, now))
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
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
	if snapshot.CurrentCount != 0 {
		t.Fatalf("current_count = %d, want 0 after stale out rejection", snapshot.CurrentCount)
	}
}

func TestProjectorFailsClosedWhenDurableMarkerLookupErrors(t *testing.T) {
	now := time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)
	projector := NewProjectorWithClock(func() time.Time { return now },
		WithProjectionMarkerResolver(func(context.Context, domain.PresenceEvent) (ProjectionMarker, bool, error) {
			return ProjectionMarker{}, false, context.DeadlineExceeded
		}),
	)

	if _, err := projector.Apply(testProjectedEvent("edge-001", "ashtonbee", "gym", "tag-001", domain.DirectionIn, now)); err == nil {
		t.Fatal("Apply() error = nil, want durable marker lookup failure")
	}
}

func TestProjectorConstructorsUseDefaultAbsentRetentionBounds(t *testing.T) {
	projector := NewProjector()
	if projector.absentRetention != DefaultAbsentIdentityRetention {
		t.Fatalf("absentRetention = %s, want %s", projector.absentRetention, DefaultAbsentIdentityRetention)
	}
	if projector.maxAbsentIdentities != DefaultMaxAbsentIdentities {
		t.Fatalf("maxAbsentIdentities = %d, want %d", projector.maxAbsentIdentities, DefaultMaxAbsentIdentities)
	}

	seeded := NewProjectorWithClock(nil)
	if seeded.absentRetention != DefaultAbsentIdentityRetention {
		t.Fatalf("seeded.absentRetention = %s, want %s", seeded.absentRetention, DefaultAbsentIdentityRetention)
	}
	if seeded.maxAbsentIdentities != DefaultMaxAbsentIdentities {
		t.Fatalf("seeded.maxAbsentIdentities = %d, want %d", seeded.maxAbsentIdentities, DefaultMaxAbsentIdentities)
	}
}

func TestProjectorPrunesAbsentIdentitiesByAge(t *testing.T) {
	projector := NewProjector(
		WithAbsentIdentityRetention(time.Hour),
		WithMaxAbsentIdentities(10),
	)

	applyAbsentIdentity(t, projector, "old-absent", "ashtonbee", "gym", "tag-old", time.Date(2026, 4, 5, 10, 5, 0, 0, time.UTC))
	applyAbsentIdentity(t, projector, "recent-absent", "ashtonbee", "gym", "tag-recent", time.Date(2026, 4, 5, 11, 20, 0, 0, time.UTC))

	presentAt := time.Date(2026, 4, 5, 11, 50, 0, 0, time.UTC)
	if _, err := projector.Apply(testProjectedEvent("present-001", "ashtonbee", "gym", "tag-present", domain.DirectionIn, presentAt)); err != nil {
		t.Fatalf("Apply(present) error = %v", err)
	}

	result, err := projector.Apply(testProjectedEvent("trigger-001", "ashtonbee", "gym", "tag-trigger", domain.DirectionIn, time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("Apply(trigger) error = %v", err)
	}
	if !result.Applied || result.Reason != "entered" {
		t.Fatalf("result = %#v, want entered applied result", result)
	}

	assertIdentityMissing(t, projector, identityKey{FacilityID: "ashtonbee", ZoneID: "gym", ExternalIdentityHash: "tag-old"})
	assertIdentityPresent(t, projector, identityKey{FacilityID: "ashtonbee", ZoneID: "gym", ExternalIdentityHash: "tag-recent"}, false)
	assertIdentityPresent(t, projector, identityKey{FacilityID: "ashtonbee", ZoneID: "gym", ExternalIdentityHash: "tag-present"}, true)
	assertIdentityPresent(t, projector, identityKey{FacilityID: "ashtonbee", ZoneID: "gym", ExternalIdentityHash: "tag-trigger"}, true)

	snapshot, err := projector.CurrentOccupancy(context.Background(), domain.OccupancyFilter{FacilityID: "ashtonbee", ZoneID: "gym"})
	if err != nil {
		t.Fatalf("CurrentOccupancy() error = %v", err)
	}
	if snapshot.CurrentCount != 2 {
		t.Fatalf("current_count = %d, want 2", snapshot.CurrentCount)
	}
}

func TestProjectorPrunesAbsentIdentitiesByCap(t *testing.T) {
	projector := NewProjector(
		WithAbsentIdentityRetention(24*time.Hour),
		WithMaxAbsentIdentities(2),
	)

	if _, err := projector.Apply(testProjectedEvent("present-001", "ashtonbee", "gym", "tag-present", domain.DirectionIn, time.Date(2026, 4, 5, 9, 50, 0, 0, time.UTC))); err != nil {
		t.Fatalf("Apply(present) error = %v", err)
	}
	applyAbsentIdentity(t, projector, "absent-001", "ashtonbee", "gym", "tag-old", time.Date(2026, 4, 5, 10, 5, 0, 0, time.UTC))
	applyAbsentIdentity(t, projector, "absent-002", "ashtonbee", "gym", "tag-middle", time.Date(2026, 4, 5, 10, 10, 0, 0, time.UTC))
	applyAbsentIdentity(t, projector, "absent-003", "ashtonbee", "gym", "tag-new", time.Date(2026, 4, 5, 10, 15, 0, 0, time.UTC))

	result, err := projector.Apply(testProjectedEvent("trigger-001", "ashtonbee", "gym", "tag-trigger", domain.DirectionIn, time.Date(2026, 4, 5, 10, 20, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("Apply(trigger) error = %v", err)
	}
	if !result.Applied || result.Reason != "entered" {
		t.Fatalf("result = %#v, want entered applied result", result)
	}

	assertIdentityMissing(t, projector, identityKey{FacilityID: "ashtonbee", ZoneID: "gym", ExternalIdentityHash: "tag-old"})
	assertIdentityPresent(t, projector, identityKey{FacilityID: "ashtonbee", ZoneID: "gym", ExternalIdentityHash: "tag-middle"}, false)
	assertIdentityPresent(t, projector, identityKey{FacilityID: "ashtonbee", ZoneID: "gym", ExternalIdentityHash: "tag-new"}, false)
	assertIdentityPresent(t, projector, identityKey{FacilityID: "ashtonbee", ZoneID: "gym", ExternalIdentityHash: "tag-present"}, true)
	assertIdentityPresent(t, projector, identityKey{FacilityID: "ashtonbee", ZoneID: "gym", ExternalIdentityHash: "tag-trigger"}, true)

	snapshot, err := projector.CurrentOccupancy(context.Background(), domain.OccupancyFilter{FacilityID: "ashtonbee", ZoneID: "gym"})
	if err != nil {
		t.Fatalf("CurrentOccupancy() error = %v", err)
	}
	if snapshot.CurrentCount != 2 {
		t.Fatalf("current_count = %d, want 2", snapshot.CurrentCount)
	}
}

func TestProjectorPruneOrderIsDeterministic(t *testing.T) {
	t.Run("oldest timestamp first", func(t *testing.T) {
		projector := NewProjector(
			WithAbsentIdentityRetention(24*time.Hour),
			WithMaxAbsentIdentities(2),
		)

		applyAbsentIdentity(t, projector, "old-001", "ashtonbee", "gym", "tag-old", time.Date(2026, 4, 5, 10, 0, 0, 0, time.UTC))
		applyAbsentIdentity(t, projector, "mid-001", "ashtonbee", "gym", "tag-mid", time.Date(2026, 4, 5, 10, 5, 0, 0, time.UTC))
		applyAbsentIdentity(t, projector, "new-001", "ashtonbee", "gym", "tag-new", time.Date(2026, 4, 5, 10, 10, 0, 0, time.UTC))

		if _, err := projector.Apply(testProjectedEvent("trigger-001", "ashtonbee", "gym", "tag-trigger", domain.DirectionIn, time.Date(2026, 4, 5, 10, 15, 0, 0, time.UTC))); err != nil {
			t.Fatalf("Apply(trigger) error = %v", err)
		}

		assertIdentityMissing(t, projector, identityKey{FacilityID: "ashtonbee", ZoneID: "gym", ExternalIdentityHash: "tag-old"})
		assertIdentityPresent(t, projector, identityKey{FacilityID: "ashtonbee", ZoneID: "gym", ExternalIdentityHash: "tag-mid"}, false)
		assertIdentityPresent(t, projector, identityKey{FacilityID: "ashtonbee", ZoneID: "gym", ExternalIdentityHash: "tag-new"}, false)
	})

	t.Run("event id lexical order", func(t *testing.T) {
		projector := NewProjector(
			WithAbsentIdentityRetention(24*time.Hour),
			WithMaxAbsentIdentities(1),
		)

		applyAbsentIdentity(t, projector, "event-b", "ashtonbee", "gym", "tag-b", time.Date(2026, 4, 5, 10, 0, 0, 0, time.UTC))
		applyAbsentIdentity(t, projector, "event-a", "ashtonbee", "gym", "tag-a", time.Date(2026, 4, 5, 10, 0, 0, 0, time.UTC))

		if _, err := projector.Apply(testProjectedEvent("trigger-001", "ashtonbee", "gym", "tag-trigger", domain.DirectionIn, time.Date(2026, 4, 5, 10, 5, 0, 0, time.UTC))); err != nil {
			t.Fatalf("Apply(trigger) error = %v", err)
		}

		assertIdentityMissing(t, projector, identityKey{FacilityID: "ashtonbee", ZoneID: "gym", ExternalIdentityHash: "tag-a"})
		assertIdentityPresent(t, projector, identityKey{FacilityID: "ashtonbee", ZoneID: "gym", ExternalIdentityHash: "tag-b"}, false)
	})

	t.Run("identity key lexical order", func(t *testing.T) {
		projector := NewProjector(
			WithAbsentIdentityRetention(24*time.Hour),
			WithMaxAbsentIdentities(1),
		)

		applyAbsentIdentity(t, projector, "event-001", "ashtonbee", "gym", "tag-a", time.Date(2026, 4, 5, 10, 0, 0, 0, time.UTC))
		applyAbsentIdentity(t, projector, "event-001", "ashtonbee", "gym", "tag-b", time.Date(2026, 4, 5, 10, 0, 0, 0, time.UTC))

		if _, err := projector.Apply(testProjectedEvent("trigger-001", "ashtonbee", "gym", "tag-trigger", domain.DirectionIn, time.Date(2026, 4, 5, 10, 5, 0, 0, time.UTC))); err != nil {
			t.Fatalf("Apply(trigger) error = %v", err)
		}

		assertIdentityMissing(t, projector, identityKey{FacilityID: "ashtonbee", ZoneID: "gym", ExternalIdentityHash: "tag-a"})
		assertIdentityPresent(t, projector, identityKey{FacilityID: "ashtonbee", ZoneID: "gym", ExternalIdentityHash: "tag-b"}, false)
	})
}

func TestProjectorRetainsStaleAndDuplicateBehaviorWithinRetentionWindow(t *testing.T) {
	projector := NewProjector(
		WithAbsentIdentityRetention(time.Hour),
		WithMaxAbsentIdentities(10),
	)

	if _, err := projector.Apply(testProjectedEvent("enter-001", "ashtonbee", "gym", "tag-001", domain.DirectionIn, time.Date(2026, 4, 5, 10, 0, 0, 0, time.UTC))); err != nil {
		t.Fatalf("Apply(entry) error = %v", err)
	}
	if _, err := projector.Apply(testProjectedEvent("exit-001", "ashtonbee", "gym", "tag-001", domain.DirectionOut, time.Date(2026, 4, 5, 10, 5, 0, 0, time.UTC))); err != nil {
		t.Fatalf("Apply(exit) error = %v", err)
	}

	duplicate, err := projector.Apply(testProjectedEvent("exit-001", "ashtonbee", "gym", "tag-001", domain.DirectionOut, time.Date(2026, 4, 5, 10, 5, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("Apply(duplicate exit) error = %v", err)
	}
	if duplicate.Applied || duplicate.Reason != "duplicate" {
		t.Fatalf("duplicate = %#v, want duplicate result", duplicate)
	}

	stale, err := projector.Apply(testProjectedEvent("exit-000", "ashtonbee", "gym", "tag-001", domain.DirectionOut, time.Date(2026, 4, 5, 10, 4, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("Apply(stale exit) error = %v", err)
	}
	if stale.Applied || stale.Reason != "stale" {
		t.Fatalf("stale = %#v, want stale result", stale)
	}

	snapshot, err := projector.CurrentOccupancy(context.Background(), domain.OccupancyFilter{FacilityID: "ashtonbee", ZoneID: "gym"})
	if err != nil {
		t.Fatalf("CurrentOccupancy() error = %v", err)
	}
	if snapshot.CurrentCount != 0 {
		t.Fatalf("current_count = %d, want 0", snapshot.CurrentCount)
	}
}

func TestProjectorFreshReentryAfterAbsentPruningIsAccepted(t *testing.T) {
	projector := NewProjector(
		WithAbsentIdentityRetention(time.Hour),
		WithMaxAbsentIdentities(10),
	)

	if _, err := projector.Apply(testProjectedEvent("enter-001", "ashtonbee", "gym", "tag-001", domain.DirectionIn, time.Date(2026, 4, 5, 10, 0, 0, 0, time.UTC))); err != nil {
		t.Fatalf("Apply(entry) error = %v", err)
	}
	if _, err := projector.Apply(testProjectedEvent("exit-001", "ashtonbee", "gym", "tag-001", domain.DirectionOut, time.Date(2026, 4, 5, 10, 5, 0, 0, time.UTC))); err != nil {
		t.Fatalf("Apply(exit) error = %v", err)
	}

	if _, err := projector.Apply(testProjectedEvent("trigger-001", "ashtonbee", "gym", "tag-trigger", domain.DirectionIn, time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC))); err != nil {
		t.Fatalf("Apply(trigger) error = %v", err)
	}
	assertIdentityMissing(t, projector, identityKey{FacilityID: "ashtonbee", ZoneID: "gym", ExternalIdentityHash: "tag-001"})

	reentry, err := projector.Apply(testProjectedEvent("enter-002", "ashtonbee", "gym", "tag-001", domain.DirectionIn, time.Date(2026, 4, 5, 12, 10, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("Apply(reentry) error = %v", err)
	}
	if !reentry.Applied || reentry.Reason != "entered" {
		t.Fatalf("reentry = %#v, want entered result", reentry)
	}

	assertIdentityPresent(t, projector, identityKey{FacilityID: "ashtonbee", ZoneID: "gym", ExternalIdentityHash: "tag-001"}, true)

	snapshot, err := projector.CurrentOccupancy(context.Background(), domain.OccupancyFilter{FacilityID: "ashtonbee", ZoneID: "gym"})
	if err != nil {
		t.Fatalf("CurrentOccupancy() error = %v", err)
	}
	if snapshot.CurrentCount != 2 {
		t.Fatalf("current_count = %d, want 2", snapshot.CurrentCount)
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

func applyAbsentIdentity(t *testing.T, projector *Projector, eventID, facilityID, zoneID, identityHash string, recordedAt time.Time) {
	t.Helper()

	result, err := projector.Apply(testProjectedEvent(eventID, facilityID, zoneID, identityHash, domain.DirectionOut, recordedAt))
	if err != nil {
		t.Fatalf("Apply(absent %s) error = %v", identityHash, err)
	}
	if result.Applied {
		t.Fatalf("Apply(absent %s) applied = true, want false (reason=%q)", identityHash, result.Reason)
	}
	if result.Reason != "already_absent" {
		t.Fatalf("Apply(absent %s) reason = %q, want already_absent", identityHash, result.Reason)
	}
}

func assertIdentityMissing(t *testing.T, projector *Projector, key identityKey) {
	t.Helper()

	if _, ok := projector.identities[key]; ok {
		t.Fatalf("identity %#v = present, want missing", key)
	}
}

func assertIdentityPresent(t *testing.T, projector *Projector, key identityKey, wantPresent bool) {
	t.Helper()

	state, ok := projector.identities[key]
	if !ok {
		t.Fatalf("identity %#v = missing, want present=%t", key, wantPresent)
	}
	if state.Present != wantPresent {
		t.Fatalf("identity %#v present = %t, want %t", key, state.Present, wantPresent)
	}
}
