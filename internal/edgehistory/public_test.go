package edgehistory

import (
	"context"
	"testing"
	"time"

	"github.com/ixxet/athena/internal/domain"
	"github.com/ixxet/athena/internal/edge"
)

func TestReadPublicObservationsFiltersAndSanitizesHistory(t *testing.T) {
	historyPath := t.TempDir() + "/edge-history.jsonl"
	store, err := NewFileStore(historyPath)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}

	pass := edge.ObservationRecord{
		EventID:              "edge-accepted-001",
		FacilityID:           "ashtonbee",
		NodeID:               "entry-node",
		Direction:            domain.DirectionIn,
		Result:               "pass",
		Source:               domain.SourceRFID,
		ExternalIdentityHash: "hash-001",
		ObservedAt:           time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC),
		StoredAt:             time.Date(2026, 4, 9, 12, 0, 1, 0, time.UTC),
		AccountType:          "Standard",
		NamePresent:          true,
	}
	pass.ObservationID = pass.Identity()
	if err := store.RecordObservation(context.Background(), pass); err != nil {
		t.Fatalf("RecordObservation(pass) error = %v", err)
	}
	if err := store.RecordCommit(context.Background(), edge.ObservationCommit{
		ObservationID: pass.ObservationID,
		EventID:       pass.EventID,
		CommittedAt:   time.Date(2026, 4, 9, 12, 0, 2, 0, time.UTC),
	}); err != nil {
		t.Fatalf("RecordCommit(pass) error = %v", err)
	}

	fail := edge.ObservationRecord{
		EventID:              "edge-fail-001",
		FacilityID:           "ashtonbee",
		NodeID:               "entry-node",
		Direction:            domain.DirectionOut,
		Result:               "fail",
		Source:               domain.SourceRFID,
		ExternalIdentityHash: "hash-002",
		ObservedAt:           time.Date(2026, 4, 9, 13, 0, 0, 0, time.UTC),
		StoredAt:             time.Date(2026, 4, 9, 13, 0, 1, 0, time.UTC),
	}
	fail.ObservationID = fail.Identity()
	if err := store.RecordObservation(context.Background(), fail); err != nil {
		t.Fatalf("RecordObservation(fail) error = %v", err)
	}

	otherFacility := edge.ObservationRecord{
		EventID:              "edge-other-001",
		FacilityID:           "morningside",
		NodeID:               "entry-node",
		Direction:            domain.DirectionIn,
		Result:               "pass",
		Source:               domain.SourceRFID,
		ExternalIdentityHash: "hash-003",
		ObservedAt:           time.Date(2026, 4, 9, 14, 0, 0, 0, time.UTC),
		StoredAt:             time.Date(2026, 4, 9, 14, 0, 1, 0, time.UTC),
	}
	otherFacility.ObservationID = otherFacility.Identity()
	if err := store.RecordObservation(context.Background(), otherFacility); err != nil {
		t.Fatalf("RecordObservation(otherFacility) error = %v", err)
	}

	observations, err := ReadPublicObservations(historyPath, PublicFilter{
		FacilityID: "ashtonbee",
		Since:      time.Date(2026, 4, 9, 11, 0, 0, 0, time.UTC),
		Until:      time.Date(2026, 4, 9, 13, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ReadPublicObservations() error = %v", err)
	}

	if len(observations) != 2 {
		t.Fatalf("len(observations) = %d, want 2", len(observations))
	}
	if observations[0].FacilityID != "ashtonbee" {
		t.Fatalf("observations[0].FacilityID = %q, want ashtonbee", observations[0].FacilityID)
	}
	if observations[0].Direction != domain.DirectionIn {
		t.Fatalf("observations[0].Direction = %q, want in", observations[0].Direction)
	}
	if observations[0].Result != "pass" {
		t.Fatalf("observations[0].Result = %q, want pass", observations[0].Result)
	}
	if !observations[0].Committed {
		t.Fatal("observations[0].Committed = false, want true")
	}
	if observations[1].Result != "fail" {
		t.Fatalf("observations[1].Result = %q, want fail", observations[1].Result)
	}
	if observations[1].Committed {
		t.Fatal("observations[1].Committed = true, want false")
	}
}

func TestReadPublicObservationsValidatesFilter(t *testing.T) {
	testCases := []struct {
		name   string
		path   string
		filter PublicFilter
	}{
		{
			name: "missing path",
			filter: PublicFilter{
				FacilityID: "ashtonbee",
				Since:      time.Date(2026, 4, 9, 11, 0, 0, 0, time.UTC),
				Until:      time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC),
			},
		},
		{
			name: "missing facility",
			path: t.TempDir() + "/edge-history.jsonl",
			filter: PublicFilter{
				Since: time.Date(2026, 4, 9, 11, 0, 0, 0, time.UTC),
				Until: time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC),
			},
		},
		{
			name: "reversed time range",
			path: t.TempDir() + "/edge-history.jsonl",
			filter: PublicFilter{
				FacilityID: "ashtonbee",
				Since:      time.Date(2026, 4, 9, 13, 0, 0, 0, time.UTC),
				Until:      time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC),
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := ReadPublicObservations(testCase.path, testCase.filter)
			if err == nil {
				t.Fatal("ReadPublicObservations() error = nil, want validation error")
			}
		})
	}
}
