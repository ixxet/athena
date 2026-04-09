package facility

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadReturnsDeterministicCatalog(t *testing.T) {
	path := writeCatalogFixture(t, `{
  "facilities": [
    {
      "facility_id": "morningside",
      "name": "Morningside",
      "timezone": "America/Toronto",
      "hours": [
        {"day": "tuesday", "opens_at": "06:00", "closes_at": "22:00"},
        {"day": "monday", "opens_at": "06:00", "closes_at": "22:00"}
      ],
      "zones": [
        {"zone_id": "weight-room", "name": "Weight Room"}
      ],
      "closure_windows": [
        {
          "starts_at": "2026-12-25T00:00:00-05:00",
          "ends_at": "2026-12-25T23:00:00-05:00",
          "code": "holiday",
          "reason": "Holiday closure"
        }
      ],
      "metadata": {
        "ingress_mode": "touchnet",
        "surface": "internal-only"
      }
    },
    {
      "facility_id": "ashtonbee",
      "name": "Ashtonbee",
      "timezone": "America/Toronto",
      "hours": [
        {"day": "monday", "opens_at": "06:00", "closes_at": "22:00"}
      ],
      "zones": [
        {"zone_id": "gym-floor", "name": "Gym Floor"},
        {"zone_id": "lobby", "name": "Lobby"}
      ],
      "closure_windows": [
        {
          "starts_at": "2026-07-01T08:00:00-04:00",
          "ends_at": "2026-07-01T12:00:00-04:00",
          "code": "maintenance",
          "reason": "Morning maintenance",
          "zone_ids": ["gym-floor"]
        }
      ],
      "metadata": {
        "ingress_mode": "touchnet",
        "surface": "internal-only"
      }
    }
  ]
}`)

	store, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	summaries := store.List()
	if len(summaries) != 2 {
		t.Fatalf("len(List()) = %d, want 2", len(summaries))
	}
	if summaries[0].FacilityID != "ashtonbee" || summaries[1].FacilityID != "morningside" {
		t.Fatalf("List() = %#v, want facility_id sorted order", summaries)
	}

	facility, ok := store.Facility("ashtonbee")
	if !ok {
		t.Fatal("Facility(ashtonbee) ok = false, want true")
	}
	if facility.Hours[0].Day != "monday" {
		t.Fatalf("Hours[0].Day = %q, want monday", facility.Hours[0].Day)
	}
	if facility.ClosureWindows[0].StartsAt != "2026-07-01T12:00:00Z" {
		t.Fatalf("StartsAt = %q, want UTC-normalized closure start", facility.ClosureWindows[0].StartsAt)
	}
	if facility.ClosureWindows[0].ZoneIDs[0] != "gym-floor" {
		t.Fatalf("ZoneIDs = %#v, want [gym-floor]", facility.ClosureWindows[0].ZoneIDs)
	}
}

func TestLoadRejectsOverlappingHours(t *testing.T) {
	path := writeCatalogFixture(t, `{
  "facilities": [
    {
      "facility_id": "ashtonbee",
      "name": "Ashtonbee",
      "timezone": "America/Toronto",
      "hours": [
        {"day": "monday", "opens_at": "06:00", "closes_at": "12:00"},
        {"day": "monday", "opens_at": "11:30", "closes_at": "22:00"}
      ],
      "zones": [{"zone_id": "gym-floor", "name": "Gym Floor"}],
      "metadata": {"ingress_mode": "touchnet"}
    }
  ]
}`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want overlapping hours error")
	}
	if !strings.Contains(err.Error(), "overlapping hours windows") {
		t.Fatalf("Load() error = %q, want overlapping hours context", err)
	}
}

func TestLoadRejectsUnknownClosureZone(t *testing.T) {
	path := writeCatalogFixture(t, `{
  "facilities": [
    {
      "facility_id": "ashtonbee",
      "name": "Ashtonbee",
      "timezone": "America/Toronto",
      "hours": [{"day": "monday", "opens_at": "06:00", "closes_at": "22:00"}],
      "zones": [{"zone_id": "gym-floor", "name": "Gym Floor"}],
      "closure_windows": [
        {
          "starts_at": "2026-07-01T08:00:00-04:00",
          "ends_at": "2026-07-01T12:00:00-04:00",
          "reason": "Maintenance",
          "zone_ids": ["missing-zone"]
        }
      ],
      "metadata": {"ingress_mode": "touchnet"}
    }
  ]
}`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want unknown closure zone error")
	}
	if !strings.Contains(err.Error(), "unknown closure zone") {
		t.Fatalf("Load() error = %q, want unknown closure zone context", err)
	}
}

func TestLoadRejectsConflictingClosures(t *testing.T) {
	path := writeCatalogFixture(t, `{
  "facilities": [
    {
      "facility_id": "ashtonbee",
      "name": "Ashtonbee",
      "timezone": "America/Toronto",
      "hours": [{"day": "monday", "opens_at": "06:00", "closes_at": "22:00"}],
      "zones": [
        {"zone_id": "gym-floor", "name": "Gym Floor"},
        {"zone_id": "lobby", "name": "Lobby"}
      ],
      "closure_windows": [
        {
          "starts_at": "2026-07-01T08:00:00-04:00",
          "ends_at": "2026-07-01T12:00:00-04:00",
          "reason": "Maintenance",
          "zone_ids": ["gym-floor"]
        },
        {
          "starts_at": "2026-07-01T09:00:00-04:00",
          "ends_at": "2026-07-01T10:00:00-04:00",
          "reason": "Inspection",
          "zone_ids": ["gym-floor"]
        }
      ],
      "metadata": {"ingress_mode": "touchnet"}
    }
  ]
}`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want conflicting closure error")
	}
	if !strings.Contains(err.Error(), "conflicting closure windows") {
		t.Fatalf("Load() error = %q, want conflicting closure context", err)
	}
}

func TestLoadRejectsMissingMetadata(t *testing.T) {
	path := writeCatalogFixture(t, `{
  "facilities": [
    {
      "facility_id": "ashtonbee",
      "name": "Ashtonbee",
      "timezone": "America/Toronto",
      "hours": [{"day": "monday", "opens_at": "06:00", "closes_at": "22:00"}],
      "zones": [{"zone_id": "gym-floor", "name": "Gym Floor"}]
    }
  ]
}`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want missing metadata error")
	}
	if !strings.Contains(err.Error(), "facility metadata") {
		t.Fatalf("Load() error = %q, want facility metadata context", err)
	}
}

func TestLoadRequiresPath(t *testing.T) {
	_, err := Load("")
	if err == nil {
		t.Fatal("Load() error = nil, want catalog not configured")
	}
	if !errorsIs(err, ErrCatalogNotConfigured) {
		t.Fatalf("Load() error = %v, want ErrCatalogNotConfigured", err)
	}
}

func writeCatalogFixture(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "facilities.json")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	return path
}

func errorsIs(err, target error) bool {
	return errors.Is(err, target)
}
