package edgehistory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ixxet/athena/internal/domain"
	"github.com/ixxet/athena/internal/edge"
	"github.com/ixxet/athena/internal/presence"
)

const (
	IngressBridgeMaxObservations                   = 1000
	IngressBridgeMaxContext                        = 4000
	IngressBridgeMaxSessions                       = 250
	IngressBridgeMaxBytes                          = 2 << 20
	IngressBridgeMaxWindow                         = 24 * time.Hour
	IngressBridgeTimeout                           = 5 * time.Second
	IngressBridgeVersion                           = "athena_ingress_bridge_v2"
	ReasonIncompleteReplay                         = "incomplete_replay_context"
	ReasonMissingIdentity                          = "missing_identity"
	ReasonUnknownIdentity                          = "unknown_identity"
	ReasonSourceFailWithoutAcceptedPresence        = "source_fail_without_accepted_presence"
	ReasonStaleEvent                               = "stale_event"
	ReasonDuplicateReplay                          = "duplicate_replay"
	ReasonOutOfOrderLifecycle                      = "out_of_order_lifecycle"
	ReasonMissingFacility                          = "missing_facility"
	ReasonMissingTimestamp                         = "missing_timestamp"
	ReasonIncompleteLifecycle                      = "incomplete_lifecycle"
	ReasonAcceptedPresenceWithoutSourcePassSession = "accepted_presence_without_source_pass_session"
	ReasonSourcePassNotCommitted                   = "source_pass_not_committed"
	ReasonNotArrival                               = "not_arrival"
	ReasonUnsupportedDirection                     = "unsupported_direction"
	ReasonUnsupportedSourceResult                  = "unsupported_source_result"
)

var ErrIngressBridgeBudget = errors.New("ingress bridge budget exceeded")

type IngressBridgeFilter struct {
	FacilityID          string
	ZoneID              string
	NodeID              string
	Since               time.Time
	Until               time.Time
	SessionLimit        int
	KnownIdentityHashes map[string]bool
}

type IngressBridgeReport struct {
	FacilityID   string                    `json:"facility_id"`
	ZoneID       string                    `json:"zone_id,omitempty"`
	NodeID       string                    `json:"node_id,omitempty"`
	Since        time.Time                 `json:"since"`
	Until        time.Time                 `json:"until"`
	Completeness IngressBridgeCompleteness `json:"completeness"`
	Contract     IngressBridgeContract     `json:"contract"`
	Summary      IngressBridgeSummary      `json:"summary"`
	Evidence     []IngressBridgeEvidence   `json:"evidence"`
	Sessions     []IngressBridgeSession    `json:"sessions"`
}

type IngressBridgeCompleteness struct {
	Observations  bool     `json:"observations"`
	Sessions      bool     `json:"sessions"`
	ReplayContext bool     `json:"replay_context"`
	ReasonCodes   []string `json:"reason_codes"`
}

type IngressBridgeContract struct {
	Version               string `json:"version"`
	ReplayOrder           string `json:"replay_order"`
	IdentityScope         string `json:"identity_scope"`
	Scope                 string `json:"scope"`
	SourceTruth           string `json:"source_truth"`
	AcceptedPresenceTruth string `json:"accepted_presence_truth"`
	SessionTruth          string `json:"session_truth"`
	IdentityOutput        string `json:"identity_output"`
	UnknownIdentityScope  string `json:"unknown_identity_scope"`
}

type IngressBridgeSummary struct {
	TotalEvidence                   int           `json:"total_evidence"`
	TotalSessions                   int           `json:"total_sessions"`
	SourcePass                      int           `json:"source_pass"`
	SourceFail                      int           `json:"source_fail"`
	AcceptedSourcePass              int           `json:"accepted_source_pass"`
	AcceptedPolicy                  int           `json:"accepted_policy"`
	EligibleCoPresence              int           `json:"eligible_co_presence"`
	EligibleDailyPresence           int           `json:"eligible_daily_presence"`
	EligibleReliabilityVerification int           `json:"eligible_reliability_verification"`
	NoEligibleSignals               int           `json:"no_eligible_signals"`
	ReasonCounts                    []ReasonCount `json:"reason_counts"`
}

type ReasonCount struct {
	Code  string `json:"code"`
	Count int    `json:"count"`
}

type IngressBridgeEvidence struct {
	EvidenceID         string                   `json:"evidence_id"`
	EventID            string                   `json:"event_id,omitempty"`
	IdentityPresent    bool                     `json:"identity_present"`
	IdentityRef        string                   `json:"identity_ref,omitempty"`
	FacilityID         string                   `json:"facility_id,omitempty"`
	ZoneID             string                   `json:"zone_id,omitempty"`
	NodeID             string                   `json:"node_id,omitempty"`
	Direction          domain.PresenceDirection `json:"direction,omitempty"`
	Source             domain.PresenceSource    `json:"source,omitempty"`
	SourceResult       string                   `json:"source_result,omitempty"`
	ObservedAt         *time.Time               `json:"observed_at,omitempty"`
	StoredAt           *time.Time               `json:"stored_at,omitempty"`
	SourceCommitted    bool                     `json:"source_committed"`
	AcceptedPresence   bool                     `json:"accepted_presence"`
	AcceptancePath     string                   `json:"acceptance_path,omitempty"`
	AcceptedReasonCode string                   `json:"accepted_reason_code,omitempty"`
	ProjectionApplied  bool                     `json:"projection_applied"`
	ProjectionReason   string                   `json:"projection_reason,omitempty"`
	SessionState       string                   `json:"session_state,omitempty"`
	Eligibility        IngressBridgeEligibility `json:"eligibility"`
	ReasonCodes        []string                 `json:"reason_codes,omitempty"`
}

type IngressBridgeSession struct {
	SessionID       string                   `json:"session_id"`
	IdentityPresent bool                     `json:"identity_present"`
	IdentityRef     string                   `json:"identity_ref,omitempty"`
	State           string                   `json:"state"`
	EntryEventID    string                   `json:"entry_event_id,omitempty"`
	EntryNodeID     string                   `json:"entry_node_id,omitempty"`
	EntryAt         *time.Time               `json:"entry_at,omitempty"`
	ExitEventID     string                   `json:"exit_event_id,omitempty"`
	ExitNodeID      string                   `json:"exit_node_id,omitempty"`
	ExitAt          *time.Time               `json:"exit_at,omitempty"`
	DurationSeconds *int64                   `json:"duration_seconds,omitempty"`
	Eligibility     IngressBridgeEligibility `json:"eligibility"`
	ReasonCodes     []string                 `json:"reason_codes,omitempty"`
}

type IngressBridgeEligibility struct {
	CoPresenceProof         EligibilitySignal `json:"co_presence_proof"`
	PrivateDailyPresence    EligibilitySignal `json:"private_daily_presence"`
	ReliabilityVerification EligibilitySignal `json:"reliability_verification"`
}

type EligibilitySignal struct {
	Eligible    bool     `json:"eligible"`
	ReasonCodes []string `json:"reason_codes,omitempty"`
}

type sessionBridgeIndex struct {
	byEventID map[string]analyticsSessionRow
}

// ReadIngressBridge uses one read-only snapshot and bounds every materialised set.
// The context cap is deliberately conservative: retained history must fit in full.
func (s *PostgresStore) ReadIngressBridge(ctx context.Context, filter IngressBridgeFilter) (IngressBridgeReport, error) {
	normalized, err := normalizeIngressBridgeFilter(filter)
	if err != nil {
		return IngressBridgeReport{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, IngressBridgeTimeout)
	defer cancel()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return IngressBridgeReport{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SET LOCAL statement_timeout = '4000ms'"); err != nil {
		return IngressBridgeReport{}, err
	}
	observations, err := readIngressBridgeObservations(ctx, tx, normalized)
	if err != nil {
		return IngressBridgeReport{}, err
	}
	sessions, err := readIngressBridgeSessions(ctx, tx, normalized)
	if err != nil {
		return IngressBridgeReport{}, err
	}
	var contextRecords []edge.ObservationRecord
	complete := true
	if len(observations) > 0 || len(sessions) > 0 {
		var latestStored time.Time
		if len(observations) > 0 {
			latestStored = observations[len(observations)-1].StoredAt
		} else if err := tx.QueryRow(ctx, "SELECT transaction_timestamp()").Scan(&latestStored); err != nil {
			return IngressBridgeReport{}, err
		}
		rows, queryErr := tx.Query(ctx, observationSelect+`
   WHERE o.facility_id = $1 AND ($2 = '' OR o.zone_id = $2)
    AND o.stored_at <= $3
    AND NOT (o.observed_at >= $4 AND o.observed_at <= $5 AND ($6 = '' OR o.node_id = $6))
   ORDER BY o.stored_at, o.observed_at, o.event_id, o.observation_id
   LIMIT $7`, normalized.FacilityID, normalized.ZoneID, latestStored, normalized.Since, normalized.Until, normalized.NodeID, IngressBridgeMaxContext+1)
		if queryErr != nil {
			return IngressBridgeReport{}, fmt.Errorf("read bridge replay context: %w", queryErr)
		}
		contextRecords, err = collectObservationRows(rows)
		rows.Close()
		if err != nil {
			return IngressBridgeReport{}, err
		}
		if len(contextRecords) > IngressBridgeMaxContext {
			contextRecords = nil
			complete = false
		}
	}
	report, err := buildIngressBridgeReport(ctx, normalized, observations, sessions, contextRecords, complete)
	if err != nil {
		return IngressBridgeReport{}, err
	}
	payload, err := json.Marshal(report)
	if err != nil {
		return IngressBridgeReport{}, err
	}
	if len(payload) > IngressBridgeMaxBytes {
		return IngressBridgeReport{}, fmt.Errorf("%w: response bytes", ErrIngressBridgeBudget)
	}
	if err := tx.Commit(ctx); err != nil {
		return IngressBridgeReport{}, err
	}
	return report, nil
}

func BuildIngressBridgeReport(filter IngressBridgeFilter, observations []edge.ObservationRecord, sessions []analyticsSessionRow) (IngressBridgeReport, error) {
	return buildIngressBridgeReport(context.Background(), filter, observations, sessions, nil, true)
}

func buildIngressBridgeReport(ctx context.Context, filter IngressBridgeFilter, observations []edge.ObservationRecord, sessions []analyticsSessionRow, contextRecords []edge.ObservationRecord, contextComplete bool) (IngressBridgeReport, error) {
	normalized, err := normalizeIngressBridgeFilter(filter)
	if err != nil {
		return IngressBridgeReport{}, err
	}
	if len(observations) > IngressBridgeMaxObservations || len(sessions) > normalized.SessionLimit || len(contextRecords) > IngressBridgeMaxContext {
		return IngressBridgeReport{}, ErrIngressBridgeBudget
	}

	sessions = append([]analyticsSessionRow(nil), sessions...)
	for i := range sessions {
		session := &sessions[i]
		// Current session rows may have closed after this historical window.
		if session.ExitAt != nil && session.ExitAt.After(normalized.Until) {
			session.State = "open"
			session.ExitAt = nil
			session.ExitEventID = ""
			session.ExitNodeID = ""
			session.DurationSeconds = nil
		}
	}
	identityRefs := buildIdentityRefs(observations, sessions)
	sessionIndex := buildSessionBridgeIndex(sessions)
	projector := presence.NewProjector(presence.WithAbsentIdentityRetention(100*365*24*time.Hour), presence.WithMaxAbsentIdentities(IngressBridgeMaxContext+IngressBridgeMaxObservations))
	reasonCounts := make(map[string]int)

	report := IngressBridgeReport{
		FacilityID:   normalized.FacilityID,
		ZoneID:       normalized.ZoneID,
		NodeID:       normalized.NodeID,
		Since:        normalized.Since,
		Until:        normalized.Until,
		Completeness: IngressBridgeCompleteness{Observations: true, Sessions: true, ReplayContext: contextComplete, ReasonCodes: []string{}},
		Contract: IngressBridgeContract{
			Version:               IngressBridgeVersion,
			ReplayOrder:           "stored_at,observed_at,event_id,observation_id",
			IdentityScope:         "report_local",
			Scope:                 "repo_local_runtime_proof",
			SourceTruth:           "source pass/fail remains immutable observation truth",
			AcceptedPresenceTruth: "policy accepted presence is separate from source pass/fail truth",
			SessionTruth:          "source-pass edge_sessions only; accepted-presence session cutover is not claimed",
			IdentityOutput:        "raw account ids, names, and external identity hashes are redacted",
			UnknownIdentityScope:  "ATHENA can prove hashed identity presence; caller-supplied known identity sets classify unknown identities when applicable",
		},
	}

	if !contextComplete {
		report.Completeness.ReasonCodes = []string{ReasonIncompleteReplay}
	}
	// Context participates in replay, never in report identity assignment or output.
	replayOrder := append(append([]edge.ObservationRecord(nil), observations...), contextRecords...)
	output := make(map[string]bool, len(observations))
	for _, record := range observations {
		output[record.Identity()] = true
	}
	sortIngressBridgeObservations(replayOrder)
	report.Evidence = make([]IngressBridgeEvidence, 0, len(replayOrder))
	for _, record := range replayOrder {
		if err := ctx.Err(); err != nil {
			return IngressBridgeReport{}, err
		}
		if !output[record.Identity()] {
			if projectableBridgeRecord(record, acceptedPresence(record)) && canProjectBridgeRecord(record) {
				projection, err := projector.Apply(record.PresenceEvent())
				if err != nil {
					return IngressBridgeReport{}, err
				}
				if projection.Applied && record.Result == "pass" && !acceptedPresence(record) {
					contextComplete = false
				}
			}
			continue
		}
		evidence, err := classifyIngressBridgeEvidence(len(report.Evidence)+1, normalized, record, identityRefs, sessionIndex, projector)
		if err != nil {
			return IngressBridgeReport{}, err
		}
		if evidence.ProjectionApplied && record.Result == "pass" && !acceptedPresence(record) {
			contextComplete = false
		}
		report.Evidence = append(report.Evidence, evidence)
	}
	report.Completeness.ReplayContext = contextComplete
	if !contextComplete {
		report.Completeness.ReasonCodes = []string{ReasonIncompleteReplay}
	}
	for i := range report.Evidence {
		evidence := &report.Evidence[i]
		if !contextComplete {
			blockBridgeEligibility(&evidence.Eligibility)
			evidence.ReasonCodes = appendReason(evidence.ReasonCodes, ReasonIncompleteReplay)
		}
		accumulateEvidenceSummary(&report.Summary, *evidence, reasonCounts)
	}

	sessionOrder := append([]analyticsSessionRow(nil), sessions...)
	sort.Slice(sessionOrder, func(i, j int) bool {
		left := sessionActivityAt(sessionOrder[i])
		right := sessionActivityAt(sessionOrder[j])
		if left.Equal(right) {
			return sessionOrder[i].SessionID < sessionOrder[j].SessionID
		}
		return left.Before(right)
	})
	report.Sessions = make([]IngressBridgeSession, 0, len(sessionOrder))
	for _, session := range sessionOrder {
		if err := ctx.Err(); err != nil {
			return IngressBridgeReport{}, err
		}

		classified := classifyIngressBridgeSession(session, identityRefs)
		if !contextComplete {
			blockBridgeEligibility(&classified.Eligibility)
			classified.ReasonCodes = appendReason(classified.ReasonCodes, ReasonIncompleteReplay)
		}
		report.Sessions = append(report.Sessions, classified)
		accumulateSessionSummary(&report.Summary, classified, reasonCounts)
	}

	report.Summary.ReasonCounts = sortedReasonCounts(reasonCounts)
	return report, nil
}

func readIngressBridgeObservations(ctx context.Context, tx pgx.Tx, filter IngressBridgeFilter) ([]edge.ObservationRecord, error) {
	rows, err := tx.Query(ctx, observationSelect+`
		WHERE
			o.facility_id = $1
			AND ($2 = '' OR o.zone_id = $2)
			AND ($3 = '' OR o.node_id = $3)
			AND o.observed_at >= $4
			AND o.observed_at <= $5
		ORDER BY o.stored_at ASC, o.observed_at ASC, o.event_id ASC, o.observation_id ASC
  LIMIT $6
 `, filter.FacilityID, filter.ZoneID, filter.NodeID, filter.Since, filter.Until, IngressBridgeMaxObservations+1)
	if err != nil {
		return nil, fmt.Errorf("query edge ingress bridge observations: %w", err)
	}
	defer rows.Close()

	records, err := collectObservationRows(rows)
	if err != nil {
		return nil, err
	}
	if len(records) > IngressBridgeMaxObservations {
		return nil, fmt.Errorf("%w: observations", ErrIngressBridgeBudget)
	}
	return records, nil
}

func readIngressBridgeSessions(ctx context.Context, tx pgx.Tx, filter IngressBridgeFilter) ([]analyticsSessionRow, error) {
	rows, err := tx.Query(ctx, `
 SELECT session_id::text, external_identity_hash, state, COALESCE(entry_event_id,''), COALESCE(entry_node_id,''), entry_at,
 COALESCE(exit_event_id,''), COALESCE(exit_node_id,''), exit_at, duration_seconds
 FROM athena.edge_sessions
 WHERE facility_id = $1 AND ($2 = '' OR zone_id = $2)
 AND ($3 = '' OR entry_node_id = $3 OR (exit_node_id = $3 AND exit_at <= $4))
 AND ((entry_at IS NOT NULL AND entry_at <= $4 AND (exit_at IS NULL OR exit_at >= $5))
 OR (entry_at IS NULL AND exit_at >= $5 AND exit_at <= $4))
 ORDER BY COALESCE(exit_at, entry_at), session_id LIMIT $6`, filter.FacilityID, filter.ZoneID, filter.NodeID, filter.Until, filter.Since, filter.SessionLimit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sessions := make([]analyticsSessionRow, 0)
	for rows.Next() {
		var row analyticsSessionRow
		if err := rows.Scan(&row.SessionID, &row.ExternalIdentityHash, &row.State, &row.EntryEventID, &row.EntryNodeID, &row.EntryAt, &row.ExitEventID, &row.ExitNodeID, &row.ExitAt, &row.DurationSeconds); err != nil {
			return nil, err
		}
		sessions = append(sessions, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(sessions) > filter.SessionLimit {
		return nil, fmt.Errorf("%w: sessions", ErrIngressBridgeBudget)
	}
	return sessions, nil
}

func blockBridgeEligibility(eligibility *IngressBridgeEligibility) {
	for _, signal := range []*EligibilitySignal{&eligibility.CoPresenceProof, &eligibility.PrivateDailyPresence, &eligibility.ReliabilityVerification} {
		signal.Eligible = false
		signal.ReasonCodes = appendReason(signal.ReasonCodes, ReasonIncompleteReplay)
	}
}

func normalizeIngressBridgeFilter(filter IngressBridgeFilter) (IngressBridgeFilter, error) {
	normalized := IngressBridgeFilter{
		FacilityID:          strings.TrimSpace(filter.FacilityID),
		ZoneID:              strings.TrimSpace(filter.ZoneID),
		NodeID:              strings.TrimSpace(filter.NodeID),
		Since:               filter.Since.UTC(),
		Until:               filter.Until.UTC(),
		SessionLimit:        filter.SessionLimit,
		KnownIdentityHashes: filter.KnownIdentityHashes,
	}
	if normalized.FacilityID == "" {
		return IngressBridgeFilter{}, fmt.Errorf("facility_id is required")
	}
	if normalized.Since.IsZero() {
		return IngressBridgeFilter{}, fmt.Errorf("since is required")
	}
	if normalized.Until.IsZero() {
		return IngressBridgeFilter{}, fmt.Errorf("until is required")
	}
	if normalized.Until.Before(normalized.Since) {
		return IngressBridgeFilter{}, fmt.Errorf("until must be greater than or equal to since")
	}
	if normalized.Until.Sub(normalized.Since) > IngressBridgeMaxWindow {
		return IngressBridgeFilter{}, fmt.Errorf("window exceeds 24 hours")
	}
	if normalized.SessionLimit == 0 {
		normalized.SessionLimit = 50
	}
	if normalized.SessionLimit < 1 || normalized.SessionLimit > IngressBridgeMaxSessions {
		return IngressBridgeFilter{}, fmt.Errorf("session_limit must be between 1 and 250")
	}
	return normalized, nil
}

func classifyIngressBridgeEvidence(
	index int,
	filter IngressBridgeFilter,
	record edge.ObservationRecord,
	identityRefs map[string]string,
	sessionIndex sessionBridgeIndex,
	projector *presence.Projector,
) (IngressBridgeEvidence, error) {
	reasons := make([]string, 0)
	identityHash := strings.TrimSpace(record.ExternalIdentityHash)
	accepted := acceptedPresence(record)
	acceptancePath := acceptancePath(record)

	evidence := IngressBridgeEvidence{
		EvidenceID:         fmt.Sprintf("evidence-%03d", index),
		EventID:            strings.TrimSpace(record.EventID),
		IdentityPresent:    identityHash != "",
		IdentityRef:        identityRefs[identityHash],
		FacilityID:         strings.TrimSpace(record.FacilityID),
		ZoneID:             strings.TrimSpace(record.ZoneID),
		NodeID:             strings.TrimSpace(record.NodeID),
		Direction:          record.Direction,
		Source:             record.Source,
		SourceResult:       strings.TrimSpace(record.Result),
		ObservedAt:         reportTime(record.ObservedAt),
		StoredAt:           reportTime(record.StoredAt),
		SourceCommitted:    record.CommittedAt != nil,
		AcceptedPresence:   accepted,
		AcceptancePath:     acceptancePath,
		AcceptedReasonCode: strings.TrimSpace(record.AcceptedReasonCode),
	}

	if evidence.FacilityID == "" {
		reasons = appendReason(reasons, ReasonMissingFacility)
	}
	if record.ObservedAt.IsZero() {
		reasons = appendReason(reasons, ReasonMissingTimestamp)
	}
	if identityHash == "" {
		reasons = appendReason(reasons, ReasonMissingIdentity)
	} else if filter.KnownIdentityHashes != nil && !filter.KnownIdentityHashes[identityHash] {
		reasons = appendReason(reasons, ReasonUnknownIdentity)
	}
	if record.Direction != domain.DirectionIn && record.Direction != domain.DirectionOut {
		reasons = appendReason(reasons, ReasonUnsupportedDirection)
	}
	if record.Result != "pass" && record.Result != "fail" {
		reasons = appendReason(reasons, ReasonUnsupportedSourceResult)
	}
	if record.Result == "fail" && !accepted {
		reasons = appendReason(reasons, ReasonSourceFailWithoutAcceptedPresence)
	}
	if record.Result == "pass" && record.CommittedAt == nil {
		reasons = appendReason(reasons, ReasonSourcePassNotCommitted)
	}

	if projectableBridgeRecord(record, accepted) && canProjectBridgeRecord(record) {
		projection, err := projector.Apply(record.PresenceEvent())
		if err != nil {
			return IngressBridgeEvidence{}, fmt.Errorf("classify ingress bridge evidence %q: %w", record.EventID, err)
		}
		evidence.ProjectionApplied = projection.Applied
		evidence.ProjectionReason = projection.Reason
		switch projection.Reason {
		case "stale":
			reasons = appendReason(reasons, ReasonStaleEvent)
		case "duplicate", "already_present":
			reasons = appendReason(reasons, ReasonDuplicateReplay)
		case "already_absent":
			reasons = appendReason(reasons, ReasonOutOfOrderLifecycle)
		}
	}

	if session, ok := sessionIndex.byEventID[record.EventID]; ok {
		evidence.SessionState = session.State
	}

	evidence.Eligibility = evidenceEligibility(record, accepted, sessionIndex, reasons)
	evidence.ReasonCodes = unionReasons(reasons, evidence.Eligibility)
	return evidence, nil
}

func classifyIngressBridgeSession(session analyticsSessionRow, identityRefs map[string]string) IngressBridgeSession {
	reasons := make([]string, 0)
	identityHash := strings.TrimSpace(session.ExternalIdentityHash)
	if identityHash == "" {
		reasons = appendReason(reasons, ReasonMissingIdentity)
	}

	classified := IngressBridgeSession{
		SessionID:       strings.TrimSpace(session.SessionID),
		IdentityPresent: identityHash != "",
		IdentityRef:     identityRefs[identityHash],
		State:           strings.TrimSpace(session.State),
		EntryEventID:    strings.TrimSpace(session.EntryEventID),
		EntryNodeID:     strings.TrimSpace(session.EntryNodeID),
		EntryAt:         copyTime(session.EntryAt),
		ExitEventID:     strings.TrimSpace(session.ExitEventID),
		ExitNodeID:      strings.TrimSpace(session.ExitNodeID),
		ExitAt:          copyTime(session.ExitAt),
		DurationSeconds: copyInt64(session.DurationSeconds),
	}

	switch classified.State {
	case "closed":
		if classified.EntryAt == nil || classified.ExitAt == nil || classified.EntryEventID == "" || classified.ExitEventID == "" {
			reasons = appendReason(reasons, ReasonIncompleteLifecycle)
		}
	case "open":
		reasons = appendReason(reasons, ReasonIncompleteLifecycle)
	case "unmatched_exit":
		reasons = appendReason(reasons, ReasonOutOfOrderLifecycle)
	default:
		reasons = appendReason(reasons, ReasonIncompleteLifecycle)
	}

	baseBlocked := containsAnyReason(reasons, ReasonMissingIdentity, ReasonOutOfOrderLifecycle)
	coPresenceEligible := !baseBlocked && classified.EntryAt != nil
	dailyEligible := !baseBlocked && classified.EntryAt != nil
	reliabilityEligible := !baseBlocked && classified.State == "closed" && !containsReason(reasons, ReasonIncompleteLifecycle)

	classified.Eligibility = IngressBridgeEligibility{
		CoPresenceProof: EligibilitySignal{
			Eligible:    coPresenceEligible,
			ReasonCodes: reasonCodesIfBlocked(coPresenceEligible, reasons),
		},
		PrivateDailyPresence: EligibilitySignal{
			Eligible:    dailyEligible,
			ReasonCodes: reasonCodesIfBlocked(dailyEligible, reasons),
		},
		ReliabilityVerification: EligibilitySignal{
			Eligible:    reliabilityEligible,
			ReasonCodes: reasonCodesIfBlocked(reliabilityEligible, reasons),
		},
	}
	classified.ReasonCodes = unionReasons(reasons, classified.Eligibility)
	return classified
}

func evidenceEligibility(record edge.ObservationRecord, accepted bool, sessionIndex sessionBridgeIndex, reasons []string) IngressBridgeEligibility {
	blockingReasons := blockingEvidenceReasons(reasons)
	baseEligible := accepted && len(blockingReasons) == 0

	coPresence := EligibilitySignal{
		Eligible:    baseEligible,
		ReasonCodes: reasonCodesIfBlocked(baseEligible, blockingReasons),
	}

	dailyReasons := append([]string(nil), blockingReasons...)
	if record.Direction != domain.DirectionIn {
		dailyReasons = appendReason(dailyReasons, ReasonNotArrival)
	}
	dailyEligible := accepted && len(dailyReasons) == 0

	reliabilityReasons := append([]string(nil), blockingReasons...)
	if record.Result == "fail" && accepted {
		reliabilityReasons = appendReason(reliabilityReasons, ReasonAcceptedPresenceWithoutSourcePassSession)
	}
	if record.Result == "pass" {
		session, ok := sessionIndex.byEventID[record.EventID]
		if !ok || session.State != "closed" {
			reliabilityReasons = appendReason(reliabilityReasons, ReasonIncompleteLifecycle)
		}
	}
	reliabilityEligible := accepted &&
		record.Result == "pass" &&
		record.CommittedAt != nil &&
		len(reliabilityReasons) == 0

	return IngressBridgeEligibility{
		CoPresenceProof: coPresence,
		PrivateDailyPresence: EligibilitySignal{
			Eligible:    dailyEligible,
			ReasonCodes: reasonCodesIfBlocked(dailyEligible, dailyReasons),
		},
		ReliabilityVerification: EligibilitySignal{
			Eligible:    reliabilityEligible,
			ReasonCodes: reasonCodesIfBlocked(reliabilityEligible, reliabilityReasons),
		},
	}
}

func blockingEvidenceReasons(reasons []string) []string {
	blocking := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		switch reason {
		case ReasonAcceptedPresenceWithoutSourcePassSession, ReasonIncompleteLifecycle, ReasonNotArrival:
			continue
		default:
			blocking = appendReason(blocking, reason)
		}
	}
	return blocking
}

func acceptedPresence(record edge.ObservationRecord) bool {
	return record.AcceptedAt != nil || (record.Result == "pass" && record.CommittedAt != nil)
}

func projectableBridgeRecord(record edge.ObservationRecord, accepted bool) bool {
	return record.Result == "pass" || (record.Result == "fail" && accepted)
}

func canProjectBridgeRecord(record edge.ObservationRecord) bool {
	return strings.TrimSpace(record.EventID) != "" &&
		strings.TrimSpace(record.FacilityID) != "" &&
		strings.TrimSpace(record.ExternalIdentityHash) != "" &&
		!record.ObservedAt.IsZero() &&
		(record.Direction == domain.DirectionIn || record.Direction == domain.DirectionOut)
}

func buildSessionBridgeIndex(sessions []analyticsSessionRow) sessionBridgeIndex {
	index := sessionBridgeIndex{byEventID: make(map[string]analyticsSessionRow)}
	for _, session := range sessions {
		if eventID := strings.TrimSpace(session.EntryEventID); eventID != "" {
			index.byEventID[eventID] = session
		}
		if eventID := strings.TrimSpace(session.ExitEventID); eventID != "" {
			index.byEventID[eventID] = session
		}
	}
	return index
}

func buildIdentityRefs(observations []edge.ObservationRecord, sessions []analyticsSessionRow) map[string]string {
	seen := make(map[string]struct{})
	for _, observation := range observations {
		if hash := strings.TrimSpace(observation.ExternalIdentityHash); hash != "" {
			seen[hash] = struct{}{}
		}
	}
	for _, session := range sessions {
		if hash := strings.TrimSpace(session.ExternalIdentityHash); hash != "" {
			seen[hash] = struct{}{}
		}
	}

	hashes := make([]string, 0, len(seen))
	for hash := range seen {
		hashes = append(hashes, hash)
	}
	sort.Strings(hashes)

	refs := make(map[string]string, len(hashes))
	for index, hash := range hashes {
		refs[hash] = fmt.Sprintf("identity-%03d", index+1)
	}
	return refs
}

func sortIngressBridgeObservations(records []edge.ObservationRecord) {
	sort.Slice(records, func(i, j int) bool {
		left := records[i]
		right := records[j]
		leftStored := left.StoredAt.UTC()
		rightStored := right.StoredAt.UTC()
		if !leftStored.Equal(rightStored) {
			if left.StoredAt.IsZero() {
				return false
			}
			if right.StoredAt.IsZero() {
				return true
			}
			return leftStored.Before(rightStored)
		}
		leftObserved := left.ObservedAt.UTC()
		rightObserved := right.ObservedAt.UTC()
		if !leftObserved.Equal(rightObserved) {
			if left.ObservedAt.IsZero() {
				return false
			}
			if right.ObservedAt.IsZero() {
				return true
			}
			return leftObserved.Before(rightObserved)
		}
		if left.EventID != right.EventID {
			return left.EventID < right.EventID
		}
		return left.Identity() < right.Identity()
	})
}

func accumulateEvidenceSummary(summary *IngressBridgeSummary, evidence IngressBridgeEvidence, reasonCounts map[string]int) {
	summary.TotalEvidence++
	switch evidence.SourceResult {
	case "pass":
		summary.SourcePass++
	case "fail":
		summary.SourceFail++
	}
	switch evidence.AcceptancePath {
	case edge.AcceptancePathTouchNetPass:
		if evidence.AcceptedPresence {
			summary.AcceptedSourcePass++
		}
	case edge.AcceptancePathAlwaysAdmit, edge.AcceptancePathGraceUntil, edge.AcceptancePathFacility:
		if evidence.AcceptedPresence {
			summary.AcceptedPolicy++
		}
	}
	if evidence.Eligibility.CoPresenceProof.Eligible {
		summary.EligibleCoPresence++
	}
	if evidence.Eligibility.PrivateDailyPresence.Eligible {
		summary.EligibleDailyPresence++
	}
	if evidence.Eligibility.ReliabilityVerification.Eligible {
		summary.EligibleReliabilityVerification++
	}
	if noEligibleSignals(evidence.Eligibility) {
		summary.NoEligibleSignals++
	}
	for _, reason := range evidence.ReasonCodes {
		reasonCounts[reason]++
	}
}

func accumulateSessionSummary(summary *IngressBridgeSummary, session IngressBridgeSession, reasonCounts map[string]int) {
	summary.TotalSessions++
	if session.Eligibility.CoPresenceProof.Eligible {
		summary.EligibleCoPresence++
	}
	if session.Eligibility.PrivateDailyPresence.Eligible {
		summary.EligibleDailyPresence++
	}
	if session.Eligibility.ReliabilityVerification.Eligible {
		summary.EligibleReliabilityVerification++
	}
	if noEligibleSignals(session.Eligibility) {
		summary.NoEligibleSignals++
	}
	for _, reason := range session.ReasonCodes {
		reasonCounts[reason]++
	}
}

func noEligibleSignals(eligibility IngressBridgeEligibility) bool {
	return !eligibility.CoPresenceProof.Eligible &&
		!eligibility.PrivateDailyPresence.Eligible &&
		!eligibility.ReliabilityVerification.Eligible
}

func sortedReasonCounts(counts map[string]int) []ReasonCount {
	reasons := make([]string, 0, len(counts))
	for reason := range counts {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)

	result := make([]ReasonCount, 0, len(reasons))
	for _, reason := range reasons {
		result = append(result, ReasonCount{Code: reason, Count: counts[reason]})
	}
	return result
}

func unionReasons(base []string, eligibility IngressBridgeEligibility) []string {
	reasons := append([]string(nil), base...)
	for _, reason := range eligibility.CoPresenceProof.ReasonCodes {
		reasons = appendReason(reasons, reason)
	}
	for _, reason := range eligibility.PrivateDailyPresence.ReasonCodes {
		reasons = appendReason(reasons, reason)
	}
	for _, reason := range eligibility.ReliabilityVerification.ReasonCodes {
		reasons = appendReason(reasons, reason)
	}
	sort.Strings(reasons)
	return reasons
}

func reasonCodesIfBlocked(eligible bool, reasons []string) []string {
	if eligible || len(reasons) == 0 {
		return nil
	}
	result := append([]string(nil), reasons...)
	sort.Strings(result)
	return result
}

func appendReason(reasons []string, reason string) []string {
	reason = strings.TrimSpace(reason)
	if reason == "" || containsReason(reasons, reason) {
		return reasons
	}
	return append(reasons, reason)
}

func containsAnyReason(reasons []string, targets ...string) bool {
	for _, target := range targets {
		if containsReason(reasons, target) {
			return true
		}
	}
	return false
}

func containsReason(reasons []string, target string) bool {
	for _, reason := range reasons {
		if reason == target {
			return true
		}
	}
	return false
}

func reportTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	copy := value.UTC()
	return &copy
}
