package edgehistory

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ixxet/athena/internal/edge"
	"github.com/ixxet/athena/internal/presence"
)

type FileStore struct {
	path string
	mu   sync.Mutex
}

type ReplayResult struct {
	Total    int `json:"total"`
	Pass     int `json:"pass"`
	Fail     int `json:"fail"`
	Applied  int `json:"applied"`
	Observed int `json:"observed"`
}

type journalEntry struct {
	Kind        string                  `json:"kind,omitempty"`
	Observation *edge.ObservationRecord `json:"observation,omitempty"`
	Commit      *edge.ObservationCommit `json:"commit,omitempty"`
	Marker      *MarkerRecord           `json:"marker,omitempty"`
}

func NewFileStore(path string) (*FileStore, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return nil, fmt.Errorf("edge observation history path is required")
	}

	if err := os.MkdirAll(filepath.Dir(trimmed), 0o755); err != nil {
		return nil, fmt.Errorf("create edge observation history directory: %w", err)
	}

	file, err := os.OpenFile(trimmed, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open edge observation history: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close edge observation history: %w", err)
	}

	return &FileStore{path: trimmed}, nil
}

func (s *FileStore) Path() string {
	return s.path
}

func (s *FileStore) RecordObservation(ctx context.Context, record edge.ObservationRecord) error {
	return s.appendEntries(ctx, journalEntry{
		Kind:        "observation",
		Observation: &record,
	})
}

func (s *FileStore) RecordCommit(ctx context.Context, commit edge.ObservationCommit) error {
	if err := validateCommit(commit); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	entries := []journalEntry{
		{
			Kind:   "commit",
			Commit: &commit,
		},
	}
	marker, ok, err := markerForCommitLocked(s.path, commit)
	if err != nil {
		return err
	}
	if ok {
		entries = append(entries, journalEntry{
			Kind:   "marker",
			Marker: &marker,
		})
	}

	return appendEntriesLocked(s.path, entries...)
}

func (s *FileStore) appendEntries(ctx context.Context, entries ...journalEntry) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return appendEntriesLocked(s.path, entries...)
}

func appendEntriesLocked(path string, entries ...journalEntry) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open edge observation history for append: %w", err)
	}
	defer file.Close()

	for _, entry := range entries {
		payload, err := json.Marshal(entry)
		if err != nil {
			return fmt.Errorf("marshal edge observation history entry: %w", err)
		}
		payload = append(payload, '\n')

		if _, err := file.Write(payload); err != nil {
			return fmt.Errorf("append edge observation record: %w", err)
		}
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync edge observation history: %w", err)
	}

	return nil
}

func ReadAll(path string) ([]edge.ObservationRecord, error) {
	entries, err := readEntries(path)
	if err != nil {
		return nil, err
	}

	records := make([]edge.ObservationRecord, 0, len(entries))
	recordIndexByObservation := make(map[string]int, len(entries))
	observationIDsByEvent := make(map[string][]string, len(entries))
	commitsByObservation := make(map[string]time.Time, len(entries))
	legacyCommitsByEvent := make(map[string]time.Time, len(entries))
	for _, entry := range entries {
		switch entry.Kind {
		case "observation":
			record := *entry.Observation
			record.ObservationID = record.Identity()
			record.CommittedAt = nil
			if _, exists := recordIndexByObservation[record.ObservationID]; exists {
				return nil, fmt.Errorf("duplicate edge observation identity %q", record.ObservationID)
			}
			recordIndexByObservation[record.ObservationID] = len(records)
			observationIDsByEvent[record.EventID] = append(observationIDsByEvent[record.EventID], record.ObservationID)
			records = append(records, record)
		case "commit":
			if observationID := strings.TrimSpace(entry.Commit.ObservationID); observationID != "" {
				if existing, ok := commitsByObservation[observationID]; !ok || entry.Commit.CommittedAt.Before(existing) {
					commitsByObservation[observationID] = entry.Commit.CommittedAt.UTC()
				}
				continue
			}
			if existing, ok := legacyCommitsByEvent[entry.Commit.EventID]; !ok || entry.Commit.CommittedAt.Before(existing) {
				legacyCommitsByEvent[entry.Commit.EventID] = entry.Commit.CommittedAt.UTC()
			}
		case "marker":
			continue
		default:
			return nil, fmt.Errorf("unsupported edge observation history entry kind %q", entry.Kind)
		}
	}

	for eventID, committedAt := range legacyCommitsByEvent {
		observationIDs := observationIDsByEvent[eventID]
		switch len(observationIDs) {
		case 0:
			continue
		case 1:
			observationID := observationIDs[0]
			if existing, ok := commitsByObservation[observationID]; !ok || committedAt.Before(existing) {
				commitsByObservation[observationID] = committedAt.UTC()
			}
		default:
			return nil, fmt.Errorf("ambiguous legacy commit marker for event_id %q", eventID)
		}
	}

	for index := range records {
		committedAt, ok := commitsByObservation[records[index].ObservationID]
		if !ok {
			continue
		}
		committedAtCopy := committedAt
		records[index].CommittedAt = &committedAtCopy
	}

	return records, nil
}

func (s *FileStore) ReadMarker(ctx context.Context, key MarkerKey) (MarkerRecord, bool, error) {
	select {
	case <-ctx.Done():
		return MarkerRecord{}, false, ctx.Err()
	default:
	}

	return ReadMarker(s.path, key)
}

func readEntries(path string) ([]journalEntry, error) {
	file, err := os.Open(strings.TrimSpace(path))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open edge observation history: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	entries := make([]journalEntry, 0)
	for line := 1; scanner.Scan(); line++ {
		payload := strings.TrimSpace(scanner.Text())
		if payload == "" {
			continue
		}

		entry, err := decodeEntry([]byte(payload))
		if err != nil {
			return nil, fmt.Errorf("decode edge observation history line %d: %w", line, err)
		}
		if err := validateEntry(entry); err != nil {
			return nil, fmt.Errorf("invalid edge observation history line %d: %w", line, err)
		}

		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan edge observation history: %w", err)
	}

	return entries, nil
}

func ReadRecent(path string, limit int) ([]edge.ObservationRecord, error) {
	records, err := ReadAll(path)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || len(records) <= limit {
		return records, nil
	}

	return append([]edge.ObservationRecord(nil), records[len(records)-limit:]...), nil
}

func ReplayProjector(projector *presence.Projector, records []edge.ObservationRecord) (ReplayResult, error) {
	if projector == nil {
		return ReplayResult{}, fmt.Errorf("projector is required")
	}

	sorted := append([]edge.ObservationRecord(nil), records...)
	sort.Slice(sorted, func(i, j int) bool {
		left := sorted[i]
		right := sorted[j]

		leftObserved := left.ObservedAt.UTC()
		rightObserved := right.ObservedAt.UTC()
		if !leftObserved.Equal(rightObserved) {
			return leftObserved.Before(rightObserved)
		}
		if left.EventID != right.EventID {
			return left.EventID < right.EventID
		}
		leftStored := left.StoredAt.UTC()
		rightStored := right.StoredAt.UTC()
		if !leftStored.Equal(rightStored) {
			return leftStored.Before(rightStored)
		}
		return left.NodeID < right.NodeID
	})

	var result ReplayResult
	for _, record := range sorted {
		result.Total++
		switch record.Result {
		case "pass":
			result.Pass++
			if record.CommittedAt == nil {
				result.Observed++
				continue
			}
			projection, err := projector.Apply(record.PresenceEvent())
			if err != nil {
				return result, fmt.Errorf("apply edge observation %q: %w", record.EventID, err)
			}
			if projection.Applied {
				result.Applied++
			} else {
				result.Observed++
			}
		case "fail":
			result.Fail++
		default:
			return result, fmt.Errorf("unsupported result %q for edge observation %q", record.Result, record.EventID)
		}
	}

	return result, nil
}

func ReplayFile(path string, projector *presence.Projector) (ReplayResult, error) {
	records, err := ReadAll(path)
	if err != nil {
		return ReplayResult{}, err
	}

	return ReplayProjector(projector, records)
}

func decodeEntry(payload []byte) (journalEntry, error) {
	var entry journalEntry
	if err := json.Unmarshal(payload, &entry); err == nil && entry.Kind != "" {
		return entry, nil
	}

	var legacy edge.ObservationRecord
	if err := json.Unmarshal(payload, &legacy); err != nil {
		return journalEntry{}, err
	}

	return journalEntry{
		Kind:        "observation",
		Observation: &legacy,
	}, nil
}

func validateEntry(entry journalEntry) error {
	switch entry.Kind {
	case "observation":
		if entry.Observation == nil {
			return fmt.Errorf("observation payload is required")
		}
		return validateRecord(*entry.Observation)
	case "commit":
		if entry.Commit == nil {
			return fmt.Errorf("commit payload is required")
		}
		return validateCommit(*entry.Commit)
	case "marker":
		if entry.Marker == nil {
			return fmt.Errorf("marker payload is required")
		}
		return validateMarker(*entry.Marker)
	default:
		return fmt.Errorf("unsupported kind %q", entry.Kind)
	}
}

func ReadMarker(path string, key MarkerKey) (MarkerRecord, bool, error) {
	trimmedPath := strings.TrimSpace(path)
	if trimmedPath == "" {
		return MarkerRecord{}, false, fmt.Errorf("edge observation history path is required")
	}
	if strings.TrimSpace(key.FacilityID) == "" {
		return MarkerRecord{}, false, fmt.Errorf("facility_id is required")
	}
	if strings.TrimSpace(key.ExternalIdentityHash) == "" {
		return MarkerRecord{}, false, fmt.Errorf("external_identity_hash is required")
	}

	entries, err := readEntries(trimmedPath)
	if err != nil {
		return MarkerRecord{}, false, err
	}

	var (
		haveExplicit bool
		explicit     MarkerRecord
		haveDerived  bool
		derived      MarkerRecord
	)

	for _, entry := range entries {
		switch entry.Kind {
		case "marker":
			candidate := *entry.Marker
			if !markerMatches(candidate.MarkerKey, key) {
				continue
			}
			candidate.LastRecordedAt = candidate.LastRecordedAt.UTC()
			candidate.CommittedAt = candidate.CommittedAt.UTC()
			if !haveExplicit || compareMarkerOrder(candidate, explicit) > 0 {
				explicit = candidate
				haveExplicit = true
			}
		}
	}

	if haveExplicit {
		return explicit, true, nil
	}

	records, err := ReadAll(trimmedPath)
	if err != nil {
		return MarkerRecord{}, false, err
	}
	for _, record := range records {
		if record.Result != "pass" || record.CommittedAt == nil {
			continue
		}
		candidate := markerFromObservation(record)
		if !markerMatches(candidate.MarkerKey, key) {
			continue
		}
		if !haveDerived || compareMarkerOrder(candidate, derived) > 0 {
			derived = candidate
			haveDerived = true
		}
	}
	if haveDerived {
		return derived, true, nil
	}

	return MarkerRecord{}, false, nil
}

func markerForCommitLocked(path string, commit edge.ObservationCommit) (MarkerRecord, bool, error) {
	entries, err := readEntries(path)
	if err != nil {
		return MarkerRecord{}, false, err
	}

	if observationID := strings.TrimSpace(commit.ObservationID); observationID != "" {
		for _, entry := range entries {
			if entry.Kind != "observation" || entry.Observation == nil {
				continue
			}
			record := *entry.Observation
			if derivedObservationID(record) != observationID || record.Result != "pass" {
				continue
			}
			marker := markerFromObservation(record)
			marker.CommittedAt = commit.CommittedAt.UTC()
			return marker, true, nil
		}
		return MarkerRecord{}, false, nil
	}

	eventID := strings.TrimSpace(commit.EventID)
	if eventID == "" {
		return MarkerRecord{}, false, nil
	}

	var (
		haveCandidate bool
		candidate     MarkerRecord
	)
	for _, entry := range entries {
		if entry.Kind != "observation" || entry.Observation == nil {
			continue
		}
		record := *entry.Observation
		if record.EventID != eventID || record.Result != "pass" {
			continue
		}
		marker := markerFromObservation(record)
		marker.CommittedAt = commit.CommittedAt.UTC()
		if !haveCandidate || compareMarkerOrder(marker, candidate) > 0 {
			candidate = marker
			haveCandidate = true
		}
	}
	if !haveCandidate {
		return MarkerRecord{}, false, nil
	}
	return candidate, true, nil
}

func markerFromObservation(record edge.ObservationRecord) MarkerRecord {
	observationID := derivedObservationID(record)
	marker := MarkerRecord{
		MarkerKey: MarkerKey{
			FacilityID:           record.FacilityID,
			ZoneID:               record.ZoneID,
			ExternalIdentityHash: record.ExternalIdentityHash,
		},
		ObservationID:  observationID,
		LastRecordedAt: record.ObservedAt.UTC(),
		LastEventID:    record.EventID,
		Direction:      string(record.Direction),
	}
	if record.CommittedAt != nil {
		marker.CommittedAt = record.CommittedAt.UTC()
	}
	return marker
}

func derivedObservationID(record edge.ObservationRecord) string {
	if strings.TrimSpace(record.ObservationID) != "" {
		return strings.TrimSpace(record.ObservationID)
	}
	return record.Identity()
}

func validateMarker(marker MarkerRecord) error {
	if strings.TrimSpace(marker.FacilityID) == "" {
		return fmt.Errorf("facility_id is required")
	}
	if strings.TrimSpace(marker.ExternalIdentityHash) == "" {
		return fmt.Errorf("external_identity_hash is required")
	}
	if marker.LastRecordedAt.IsZero() {
		return fmt.Errorf("last_recorded_at is required")
	}
	if strings.TrimSpace(marker.LastEventID) == "" {
		return fmt.Errorf("last_event_id is required")
	}
	if marker.Direction != "in" && marker.Direction != "out" {
		return fmt.Errorf("direction %q must be one of in,out", marker.Direction)
	}
	if marker.CommittedAt.IsZero() {
		return fmt.Errorf("committed_at is required")
	}
	if strings.TrimSpace(marker.ObservationID) == "" {
		return fmt.Errorf("observation_id is required")
	}
	return nil
}

func markerMatches(left, right MarkerKey) bool {
	return strings.TrimSpace(left.FacilityID) == strings.TrimSpace(right.FacilityID) &&
		strings.TrimSpace(left.ZoneID) == strings.TrimSpace(right.ZoneID) &&
		strings.TrimSpace(left.ExternalIdentityHash) == strings.TrimSpace(right.ExternalIdentityHash)
}

func compareMarkerOrder(left, right MarkerRecord) int {
	leftAt := left.LastRecordedAt.UTC()
	rightAt := right.LastRecordedAt.UTC()
	if leftAt.Before(rightAt) {
		return -1
	}
	if leftAt.After(rightAt) {
		return 1
	}
	switch {
	case left.LastEventID < right.LastEventID:
		return -1
	case left.LastEventID > right.LastEventID:
		return 1
	default:
		return 0
	}
}

func validateRecord(record edge.ObservationRecord) error {
	if observationID := strings.TrimSpace(record.ObservationID); observationID != "" && observationID != deriveObservationIdentity(record) {
		return fmt.Errorf("observation_id %q does not match immutable record contents", observationID)
	}
	if strings.TrimSpace(record.EventID) == "" {
		return fmt.Errorf("event_id is required")
	}
	if strings.TrimSpace(record.FacilityID) == "" {
		return fmt.Errorf("facility_id is required")
	}
	if strings.TrimSpace(record.NodeID) == "" {
		return fmt.Errorf("node_id is required")
	}
	if strings.TrimSpace(record.ExternalIdentityHash) == "" {
		return fmt.Errorf("external_identity_hash is required")
	}
	if record.ObservedAt.IsZero() {
		return fmt.Errorf("observed_at is required")
	}
	if record.StoredAt.IsZero() {
		return fmt.Errorf("stored_at is required")
	}
	if record.Direction == "" {
		return fmt.Errorf("direction is required")
	}
	if record.Source == "" {
		return fmt.Errorf("source is required")
	}
	if record.Result != "pass" && record.Result != "fail" {
		return fmt.Errorf("result %q must be one of pass,fail", record.Result)
	}

	return nil
}

func validateCommit(commit edge.ObservationCommit) error {
	if strings.TrimSpace(commit.ObservationID) == "" && strings.TrimSpace(commit.EventID) == "" {
		return fmt.Errorf("observation_id or event_id is required")
	}
	if commit.CommittedAt.IsZero() {
		return fmt.Errorf("committed_at is required")
	}

	return nil
}

func deriveObservationIdentity(record edge.ObservationRecord) string {
	record.ObservationID = ""
	return record.Identity()
}
