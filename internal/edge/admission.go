package edge

import (
	"context"
	"time"

	"github.com/ixxet/athena/internal/domain"
)

const (
	FailureReasonBadAccountNumber = "bad_account_number"
	FailureReasonRecognizedDenied = "recognized_denied"
	FailureReasonUnclassifiedFail = "unclassified_fail"
)

const (
	AcceptancePathTouchNetPass = "touchnet_pass"
	AcceptancePathAlwaysAdmit  = "always_admit"
	AcceptancePathGraceUntil   = "grace_until"
	AcceptancePathFacility     = "facility_window"
)

type PresenceAcceptance struct {
	ObservationID        string                   `json:"observation_id"`
	EventID              string                   `json:"event_id"`
	FacilityID           string                   `json:"facility_id"`
	ZoneID               string                   `json:"zone_id,omitempty"`
	ExternalIdentityHash string                   `json:"external_identity_hash"`
	Direction            domain.PresenceDirection `json:"direction"`
	AcceptedAt           time.Time                `json:"accepted_at"`
	AcceptancePath       string                   `json:"acceptance_path"`
	AcceptedReasonCode   string                   `json:"accepted_reason_code,omitempty"`
	PolicyVersionID      string                   `json:"policy_version_id,omitempty"`
}

type PolicyEvaluation struct {
	FacilityID           string
	ZoneID               string
	ExternalIdentityHash string
	FailureReasonCode    string
	ObservedAt           time.Time
}

type PolicyDecision struct {
	Admitted           bool
	AcceptancePath     string
	AcceptedReasonCode string
	PolicyVersionID    string
}

type AcceptedPresenceRecorder interface {
	RecordAcceptance(context.Context, PresenceAcceptance) error
}

type PolicyEvaluator interface {
	EvaluatePolicy(context.Context, PolicyEvaluation) (PolicyDecision, error)
}
