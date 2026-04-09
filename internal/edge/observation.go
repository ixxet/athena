package edge

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"github.com/ixxet/athena/internal/domain"
)

type ObservationRecord struct {
	ObservationID        string                   `json:"observation_id,omitempty"`
	EventID              string                   `json:"event_id"`
	FacilityID           string                   `json:"facility_id"`
	ZoneID               string                   `json:"zone_id,omitempty"`
	NodeID               string                   `json:"node_id"`
	Direction            domain.PresenceDirection `json:"direction"`
	Result               string                   `json:"result"`
	Source               domain.PresenceSource    `json:"source"`
	ExternalIdentityHash string                   `json:"external_identity_hash"`
	ObservedAt           time.Time                `json:"observed_at"`
	StoredAt             time.Time                `json:"stored_at"`
	AccountType          string                   `json:"account_type,omitempty"`
	NamePresent          bool                     `json:"name_present,omitempty"`
	CommittedAt          *time.Time               `json:"committed_at,omitempty"`
}

type ObservationCommit struct {
	ObservationID string    `json:"observation_id,omitempty"`
	EventID       string    `json:"event_id,omitempty"`
	CommittedAt   time.Time `json:"committed_at"`
}

func newObservationRecord(observed observedTap, storedAt time.Time) ObservationRecord {
	record := ObservationRecord{
		EventID:              observed.event.ID,
		FacilityID:           observed.event.FacilityID,
		ZoneID:               observed.event.ZoneID,
		NodeID:               observed.nodeID,
		Direction:            observed.event.Direction,
		Result:               observed.result,
		Source:               observed.event.Source,
		ExternalIdentityHash: observed.event.ExternalIdentityHash,
		ObservedAt:           observed.event.RecordedAt.UTC(),
		StoredAt:             storedAt.UTC(),
		AccountType:          observed.accountType,
		NamePresent:          observed.name != "",
	}
	record.ObservationID = record.Identity()
	return record
}

func newObservationCommit(record ObservationRecord, committedAt time.Time) ObservationCommit {
	return ObservationCommit{
		ObservationID: record.Identity(),
		EventID:       record.EventID,
		CommittedAt:   committedAt.UTC(),
	}
}

func (r ObservationRecord) PresenceEvent() domain.PresenceEvent {
	metadata := map[string]string{
		"node_id": r.NodeID,
		"result":  r.Result,
	}
	if r.AccountType != "" {
		metadata["account_type"] = r.AccountType
	}
	if r.NamePresent {
		metadata["name_present"] = strconv.FormatBool(r.NamePresent)
	}

	return domain.PresenceEvent{
		ID:                   r.EventID,
		FacilityID:           r.FacilityID,
		ZoneID:               r.ZoneID,
		ExternalIdentityHash: r.ExternalIdentityHash,
		Direction:            r.Direction,
		Source:               r.Source,
		RecordedAt:           r.ObservedAt.UTC(),
		Metadata:             metadata,
	}
}

func (r ObservationRecord) Identity() string {
	if existing := strings.TrimSpace(r.ObservationID); existing != "" {
		return existing
	}

	sum := sha256.Sum256([]byte(strings.Join([]string{
		strings.TrimSpace(r.EventID),
		strings.TrimSpace(r.FacilityID),
		strings.TrimSpace(r.ZoneID),
		strings.TrimSpace(r.NodeID),
		string(r.Direction),
		strings.TrimSpace(r.Result),
		string(r.Source),
		strings.TrimSpace(r.ExternalIdentityHash),
		r.ObservedAt.UTC().Format(time.RFC3339Nano),
		r.StoredAt.UTC().Format(time.RFC3339Nano),
		strings.TrimSpace(r.AccountType),
		strconv.FormatBool(r.NamePresent),
	}, "|")))

	return "obs-" + hex.EncodeToString(sum[:16])
}
