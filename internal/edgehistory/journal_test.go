package edgehistory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ixxet/athena/internal/domain"
	"github.com/ixxet/athena/internal/edge"
	"github.com/ixxet/athena/internal/presence"
)

type stubPublisher struct {
	err error
}

func (s *stubPublisher) Publish(context.Context, string, []byte) error {
	return s.err
}

func TestFileStoreRoundTripRedactsRawIdentity(t *testing.T) {
	historyPath := filepath.Join(t.TempDir(), "edge-history.jsonl")
	store, err := NewFileStore(historyPath)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}

	service, err := edge.NewService(
		&stubPublisher{},
		"salt",
		map[string]string{"entry-node": "entry-token"},
		edge.WithObservationRecorder(store),
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	req := edge.TapRequest{
		EventID:       "edge-001",
		AccountRaw:    "1000123456",
		Direction:     "in",
		FacilityID:    "ashtonbee",
		ZoneID:        "gym-floor",
		NodeID:        "entry-node",
		ObservedAt:    "2026-04-04T12:00:00Z",
		Result:        "pass",
		AccountType:   "Standard",
		Name:          "Fixture Student",
		StatusMessage: "Access entry granted for Fixture Student 1000123456.",
	}

	if _, err := service.AcceptTap(context.Background(), "entry-token", req); err != nil {
		t.Fatalf("AcceptTap() error = %v", err)
	}

	contents, err := os.ReadFile(historyPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if strings.Contains(string(contents), req.AccountRaw) {
		t.Fatalf("history file leaked raw account number: %s", contents)
	}
	if strings.Contains(string(contents), req.Name) {
		t.Fatalf("history file leaked resolved name: %s", contents)
	}
	if strings.Contains(string(contents), req.StatusMessage) {
		t.Fatalf("history file leaked status_message: %s", contents)
	}
	if !strings.Contains(string(contents), `"kind":"marker"`) {
		t.Fatalf("history file did not record a marker entry: %s", contents)
	}

	marker, ok, err := ReadMarker(historyPath, MarkerKey{
		FacilityID:           "ashtonbee",
		ZoneID:               "gym-floor",
		ExternalIdentityHash: edge.HashAccount(req.AccountRaw, "salt"),
	})
	if err != nil {
		t.Fatalf("ReadMarker() error = %v", err)
	}
	if !ok {
		t.Fatal("ReadMarker() ok = false, want true")
	}
	if marker.ObservationID == "" {
		t.Fatal("marker.ObservationID = empty, want exact committed observation identity")
	}
	if marker.LastEventID != req.EventID {
		t.Fatalf("marker.LastEventID = %q, want %q", marker.LastEventID, req.EventID)
	}
	if marker.Direction != "in" {
		t.Fatalf("marker.Direction = %q, want in", marker.Direction)
	}
	if marker.LastRecordedAt != mustTime(t, req.ObservedAt) {
		t.Fatalf("marker.LastRecordedAt = %s, want %s", marker.LastRecordedAt, req.ObservedAt)
	}
	if marker.CommittedAt.IsZero() {
		t.Fatal("marker.CommittedAt = zero, want durable committed timestamp")
	}

	records, err := ReadAll(historyPath)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}
	if records[0].ExternalIdentityHash != edge.HashAccount(req.AccountRaw, "salt") {
		t.Fatalf("ExternalIdentityHash = %q, want hashed account", records[0].ExternalIdentityHash)
	}
	if !records[0].NamePresent {
		t.Fatal("NamePresent = false, want true")
	}
	if records[0].ObservationID == "" {
		t.Fatal("ObservationID = empty, want immutable durable observation identity")
	}
	if records[0].CommittedAt == nil {
		t.Fatal("CommittedAt = nil, want committed pass marker joined into read model")
	}
}

func TestReplayFileRebuildsProjectorAfterRestart(t *testing.T) {
	historyPath := filepath.Join(t.TempDir(), "edge-history.jsonl")
	store, err := NewFileStore(historyPath)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}

	liveProjector := presence.NewProjector()
	service, err := edge.NewService(
		&stubPublisher{},
		"salt",
		map[string]string{"entry-node": "entry-token"},
		edge.WithProjection(liveProjector),
		edge.WithObservationRecorder(store),
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	requests := []edge.TapRequest{
		{
			EventID:    "edge-001",
			AccountRaw: "1000123456",
			Direction:  "in",
			FacilityID: "ashtonbee",
			ZoneID:     "gym-floor",
			NodeID:     "entry-node",
			ObservedAt: "2026-04-04T12:00:00Z",
			Result:     "pass",
		},
		{
			EventID:    "edge-002",
			AccountRaw: "1000123456",
			Direction:  "in",
			FacilityID: "ashtonbee",
			ZoneID:     "gym-floor",
			NodeID:     "entry-node",
			ObservedAt: "2026-04-04T12:01:00Z",
			Result:     "pass",
		},
		{
			EventID:    "edge-003",
			AccountRaw: "1000123456",
			Direction:  "in",
			FacilityID: "ashtonbee",
			ZoneID:     "gym-floor",
			NodeID:     "entry-node",
			ObservedAt: "2026-04-04T12:02:00Z",
			Result:     "fail",
		},
		{
			EventID:    "edge-004",
			AccountRaw: "1000123456",
			Direction:  "out",
			FacilityID: "ashtonbee",
			ZoneID:     "gym-floor",
			NodeID:     "entry-node",
			ObservedAt: "2026-04-04T12:03:00Z",
			Result:     "pass",
		},
	}
	for _, req := range requests {
		if _, err := service.AcceptTap(context.Background(), "entry-token", req); err != nil {
			t.Fatalf("AcceptTap(%s) error = %v", req.EventID, err)
		}
	}

	restartedProjector := presence.NewProjector()
	replay, err := ReplayFile(historyPath, restartedProjector)
	if err != nil {
		t.Fatalf("ReplayFile() error = %v", err)
	}
	if replay.Total != 4 {
		t.Fatalf("replay.Total = %d, want 4", replay.Total)
	}
	if replay.Pass != 3 {
		t.Fatalf("replay.Pass = %d, want 3", replay.Pass)
	}
	if replay.Fail != 1 {
		t.Fatalf("replay.Fail = %d, want 1", replay.Fail)
	}
	if replay.Applied != 2 {
		t.Fatalf("replay.Applied = %d, want 2", replay.Applied)
	}
	if replay.Observed != 1 {
		t.Fatalf("replay.Observed = %d, want 1", replay.Observed)
	}

	liveSnapshot, err := liveProjector.CurrentOccupancy(context.Background(), domain.OccupancyFilter{
		FacilityID: "ashtonbee",
		ZoneID:     "gym-floor",
	})
	if err != nil {
		t.Fatalf("live CurrentOccupancy() error = %v", err)
	}
	restartedSnapshot, err := restartedProjector.CurrentOccupancy(context.Background(), domain.OccupancyFilter{
		FacilityID: "ashtonbee",
		ZoneID:     "gym-floor",
	})
	if err != nil {
		t.Fatalf("restarted CurrentOccupancy() error = %v", err)
	}

	if restartedSnapshot.CurrentCount != liveSnapshot.CurrentCount {
		t.Fatalf("restarted current_count = %d, live current_count = %d, want equal after replay", restartedSnapshot.CurrentCount, liveSnapshot.CurrentCount)
	}
}

func TestReplayFileSkipsPassObservationThatNeverCommittedLive(t *testing.T) {
	historyPath := filepath.Join(t.TempDir(), "edge-history.jsonl")
	store, err := NewFileStore(historyPath)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}

	service, err := edge.NewService(
		&stubPublisher{err: context.DeadlineExceeded},
		"salt",
		map[string]string{"entry-node": "entry-token"},
		edge.WithObservationRecorder(store),
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	req := edge.TapRequest{
		EventID:       "edge-503-001",
		AccountRaw:    "1000123456",
		Direction:     "in",
		FacilityID:    "ashtonbee",
		ZoneID:        "gym-floor",
		NodeID:        "entry-node",
		ObservedAt:    "2026-04-04T12:00:00Z",
		Result:        "pass",
		AccountType:   "Standard",
		Name:          "Fixture Student",
		StatusMessage: "Access entry granted for Fixture Student 1000123456.",
	}

	if _, err := service.AcceptTap(context.Background(), "entry-token", req); err == nil {
		t.Fatal("AcceptTap() error = nil, want publish failure")
	}

	records, err := ReadAll(historyPath)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}
	if records[0].CommittedAt != nil {
		t.Fatalf("CommittedAt = %v, want nil for uncommitted pass", records[0].CommittedAt)
	}

	restartedProjector := presence.NewProjector()
	replay, err := ReplayFile(historyPath, restartedProjector)
	if err != nil {
		t.Fatalf("ReplayFile() error = %v", err)
	}
	if replay.Total != 1 {
		t.Fatalf("replay.Total = %d, want 1", replay.Total)
	}
	if replay.Pass != 1 {
		t.Fatalf("replay.Pass = %d, want 1", replay.Pass)
	}
	if replay.Applied != 0 {
		t.Fatalf("replay.Applied = %d, want 0", replay.Applied)
	}
	if replay.Observed != 1 {
		t.Fatalf("replay.Observed = %d, want 1", replay.Observed)
	}

	snapshot, err := restartedProjector.CurrentOccupancy(context.Background(), domain.OccupancyFilter{
		FacilityID: "ashtonbee",
		ZoneID:     "gym-floor",
	})
	if err != nil {
		t.Fatalf("CurrentOccupancy() error = %v", err)
	}
	if snapshot.CurrentCount != 0 {
		t.Fatalf("current_count = %d, want 0 after replay of uncommitted pass", snapshot.CurrentCount)
	}

	contents, err := os.ReadFile(historyPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if strings.Contains(string(contents), req.AccountRaw) {
		t.Fatalf("history file leaked raw account number: %s", contents)
	}
	if strings.Contains(string(contents), req.Name) {
		t.Fatalf("history file leaked resolved name: %s", contents)
	}
	if strings.Contains(string(contents), req.StatusMessage) {
		t.Fatalf("history file leaked status_message: %s", contents)
	}

	_, ok, err := ReadMarker(historyPath, MarkerKey{
		FacilityID:           "ashtonbee",
		ZoneID:               "gym-floor",
		ExternalIdentityHash: edge.HashAccount(req.AccountRaw, "salt"),
	})
	if err != nil {
		t.Fatalf("ReadMarker() error = %v", err)
	}
	if ok {
		t.Fatalf("ReadMarker() ok = true, want false for uncommitted pass")
	}
}

func TestReadMarkerDerivesLatestCommittedPassFromLegacyHistory(t *testing.T) {
	historyPath := filepath.Join(t.TempDir(), "edge-history.jsonl")
	payload := strings.Join([]string{
		`{"kind":"observation","observation":{"event_id":"edge-legacy-001","facility_id":"ashtonbee","zone_id":"gym-floor","node_id":"entry-node","direction":"in","result":"pass","source":"rfid","external_identity_hash":"hash-001","observed_at":"2026-04-04T12:00:00Z","stored_at":"2026-04-04T12:00:01Z"}}`,
		`{"kind":"commit","commit":{"event_id":"edge-legacy-001","committed_at":"2026-04-04T12:00:02Z"}}`,
		`{"kind":"observation","observation":{"event_id":"edge-legacy-002","facility_id":"ashtonbee","zone_id":"gym-floor","node_id":"entry-node","direction":"out","result":"pass","source":"rfid","external_identity_hash":"hash-001","observed_at":"2026-04-04T12:01:00Z","stored_at":"2026-04-04T12:01:01Z"}}`,
		`{"kind":"commit","commit":{"event_id":"edge-legacy-002","committed_at":"2026-04-04T12:01:02Z"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(historyPath, []byte(payload), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	marker, ok, err := ReadMarker(historyPath, MarkerKey{
		FacilityID:           "ashtonbee",
		ZoneID:               "gym-floor",
		ExternalIdentityHash: "hash-001",
	})
	if err != nil {
		t.Fatalf("ReadMarker() error = %v", err)
	}
	if !ok {
		t.Fatal("ReadMarker() ok = false, want true")
	}
	if marker.LastEventID != "edge-legacy-002" {
		t.Fatalf("marker.LastEventID = %q, want edge-legacy-002", marker.LastEventID)
	}
	if marker.Direction != "out" {
		t.Fatalf("marker.Direction = %q, want out", marker.Direction)
	}
	if marker.LastRecordedAt != mustTime(t, "2026-04-04T12:01:00Z") {
		t.Fatalf("marker.LastRecordedAt = %s, want 2026-04-04T12:01:00Z", marker.LastRecordedAt)
	}

	records, err := ReadAll(historyPath)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("len(records) = %d, want 2", len(records))
	}
	if records[1].CommittedAt == nil {
		t.Fatal("records[1].CommittedAt = nil, want legacy committed pass to remain readable")
	}
}

func TestReplayFileCommitsOnlyExactObservationUnderEventIDCollision(t *testing.T) {
	historyPath := filepath.Join(t.TempDir(), "edge-history.jsonl")
	store, err := NewFileStore(historyPath)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}

	committedRecord := edge.ObservationRecord{
		EventID:              "edge-collision-001",
		FacilityID:           "ashtonbee",
		ZoneID:               "gym-floor",
		NodeID:               "entry-node",
		Direction:            domain.DirectionIn,
		Result:               "pass",
		Source:               domain.SourceRFID,
		ExternalIdentityHash: "hash-committed",
		ObservedAt:           mustTime(t, "2026-04-04T12:00:00Z"),
		StoredAt:             mustTime(t, "2026-04-04T12:00:01Z"),
		AccountType:          "Standard",
	}
	committedRecord.ObservationID = committedRecord.Identity()
	if err := store.RecordObservation(context.Background(), committedRecord); err != nil {
		t.Fatalf("RecordObservation(committed) error = %v", err)
	}
	if err := store.RecordCommit(context.Background(), edge.ObservationCommit{
		ObservationID: committedRecord.ObservationID,
		EventID:       committedRecord.EventID,
		CommittedAt:   mustTime(t, "2026-04-04T12:00:02Z"),
	}); err != nil {
		t.Fatalf("RecordCommit() error = %v", err)
	}

	uncommittedRecord := edge.ObservationRecord{
		EventID:              "edge-collision-001",
		FacilityID:           "ashtonbee",
		ZoneID:               "gym-floor",
		NodeID:               "entry-node",
		Direction:            domain.DirectionIn,
		Result:               "pass",
		Source:               domain.SourceRFID,
		ExternalIdentityHash: "hash-uncommitted",
		ObservedAt:           mustTime(t, "2026-04-04T12:01:00Z"),
		StoredAt:             mustTime(t, "2026-04-04T12:01:01Z"),
		AccountType:          "ISO",
	}
	uncommittedRecord.ObservationID = uncommittedRecord.Identity()
	if err := store.RecordObservation(context.Background(), uncommittedRecord); err != nil {
		t.Fatalf("RecordObservation(uncommitted) error = %v", err)
	}

	records, err := ReadAll(historyPath)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("len(records) = %d, want 2", len(records))
	}
	if records[0].ObservationID != committedRecord.ObservationID {
		t.Fatalf("records[0].ObservationID = %q, want %q", records[0].ObservationID, committedRecord.ObservationID)
	}
	if records[0].CommittedAt == nil {
		t.Fatal("records[0].CommittedAt = nil, want committed pass marker on exact observation only")
	}
	if records[1].ObservationID != uncommittedRecord.ObservationID {
		t.Fatalf("records[1].ObservationID = %q, want %q", records[1].ObservationID, uncommittedRecord.ObservationID)
	}
	if records[1].CommittedAt != nil {
		t.Fatalf("records[1].CommittedAt = %v, want nil for colliding uncommitted observation", records[1].CommittedAt)
	}

	restartedProjector := presence.NewProjector()
	replay, err := ReplayFile(historyPath, restartedProjector)
	if err != nil {
		t.Fatalf("ReplayFile() error = %v", err)
	}
	if replay.Total != 2 {
		t.Fatalf("replay.Total = %d, want 2", replay.Total)
	}
	if replay.Pass != 2 {
		t.Fatalf("replay.Pass = %d, want 2", replay.Pass)
	}
	if replay.Applied != 1 {
		t.Fatalf("replay.Applied = %d, want 1", replay.Applied)
	}
	if replay.Observed != 1 {
		t.Fatalf("replay.Observed = %d, want 1", replay.Observed)
	}

	snapshot, err := restartedProjector.CurrentOccupancy(context.Background(), domain.OccupancyFilter{
		FacilityID: "ashtonbee",
		ZoneID:     "gym-floor",
	})
	if err != nil {
		t.Fatalf("CurrentOccupancy() error = %v", err)
	}
	if snapshot.CurrentCount != 1 {
		t.Fatalf("current_count = %d, want 1 after replay of one committed collision record", snapshot.CurrentCount)
	}
}

func TestReadAllRejectsAmbiguousLegacyEventIDCommitMarker(t *testing.T) {
	historyPath := filepath.Join(t.TempDir(), "edge-history.jsonl")
	payload := strings.Join([]string{
		`{"kind":"observation","observation":{"event_id":"edge-collision-legacy","facility_id":"ashtonbee","zone_id":"gym-floor","node_id":"entry-node","direction":"in","result":"pass","source":"rfid","external_identity_hash":"hash-001","observed_at":"2026-04-04T12:00:00Z","stored_at":"2026-04-04T12:00:01Z"}}`,
		`{"kind":"observation","observation":{"event_id":"edge-collision-legacy","facility_id":"ashtonbee","zone_id":"gym-floor","node_id":"entry-node","direction":"in","result":"pass","source":"rfid","external_identity_hash":"hash-002","observed_at":"2026-04-04T12:01:00Z","stored_at":"2026-04-04T12:01:01Z"}}`,
		`{"kind":"commit","commit":{"event_id":"edge-collision-legacy","committed_at":"2026-04-04T12:01:02Z"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(historyPath, []byte(payload), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := ReadAll(historyPath)
	if err == nil {
		t.Fatal("ReadAll() error = nil, want ambiguous legacy commit marker failure")
	}
	if !strings.Contains(err.Error(), "ambiguous legacy commit marker") {
		t.Fatalf("ReadAll() error = %q, want ambiguous legacy commit marker context", err)
	}
}

func TestReplayFileRejectsCorruptHistory(t *testing.T) {
	historyPath := filepath.Join(t.TempDir(), "edge-history.jsonl")
	if err := os.WriteFile(historyPath, []byte("{not-json}\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := ReplayFile(historyPath, presence.NewProjector())
	if err == nil {
		t.Fatal("ReplayFile() error = nil, want decode failure")
	}
	if !strings.Contains(err.Error(), "decode edge observation history") {
		t.Fatalf("ReplayFile() error = %q, want decode context", err)
	}
}

func TestReadRecentReturnsNewestRecords(t *testing.T) {
	historyPath := filepath.Join(t.TempDir(), "edge-history.jsonl")
	store, err := NewFileStore(historyPath)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}

	records := []edge.ObservationRecord{
		{
			EventID:              "edge-001",
			FacilityID:           "ashtonbee",
			NodeID:               "entry-node",
			Direction:            domain.DirectionIn,
			Result:               "pass",
			Source:               domain.SourceRFID,
			ExternalIdentityHash: "hash-001",
			ObservedAt:           mustTime(t, "2026-04-04T12:00:00Z"),
			StoredAt:             mustTime(t, "2026-04-04T12:00:01Z"),
		},
		{
			EventID:              "edge-002",
			FacilityID:           "ashtonbee",
			NodeID:               "entry-node",
			Direction:            domain.DirectionOut,
			Result:               "fail",
			Source:               domain.SourceRFID,
			ExternalIdentityHash: "hash-002",
			ObservedAt:           mustTime(t, "2026-04-04T12:01:00Z"),
			StoredAt:             mustTime(t, "2026-04-04T12:01:01Z"),
		},
	}
	for _, record := range records {
		if err := store.RecordObservation(context.Background(), record); err != nil {
			t.Fatalf("Record(%s) error = %v", record.EventID, err)
		}
	}

	recent, err := ReadRecent(historyPath, 1)
	if err != nil {
		t.Fatalf("ReadRecent() error = %v", err)
	}
	if len(recent) != 1 {
		t.Fatalf("len(recent) = %d, want 1", len(recent))
	}
	if recent[0].EventID != "edge-002" {
		t.Fatalf("recent[0].EventID = %q, want edge-002", recent[0].EventID)
	}
}

func mustTime(t *testing.T, value string) (parsedTime domainTime) {
	t.Helper()

	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("time.Parse(%q) error = %v", value, err)
	}
	return parsed
}

type domainTime = time.Time
