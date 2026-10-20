package edgehistory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ixxet/athena/internal/domain"
	"github.com/ixxet/athena/internal/edge"
	"github.com/ixxet/athena/internal/presence"
)

func TestBuildIngressBridgeReportClassifiesEvidenceAndRedactsIdentity(t *testing.T) {
	base := time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)
	hashA := edge.HashAccount("1000123456", "salt")
	hashB := edge.HashAccount("1000123457", "salt")
	hashC := edge.HashAccount("1000123458", "salt")
	hashD := edge.HashAccount("1000123459", "salt")
	hashE := edge.HashAccount("1000123460", "salt")
	hashF := edge.HashAccount("1000123461", "salt")
	hashG := edge.HashAccount("1000123462", "salt")
	hashUnknown := edge.HashAccount("1000123463", "salt")

	known := map[string]bool{
		hashA: true,
		hashB: true,
		hashC: true,
		hashD: true,
		hashE: true,
		hashF: true,
		hashG: true,
	}

	observations := []edge.ObservationRecord{
		passObservation("arrive-pass-001", hashA, domain.DirectionIn, base, base.Add(time.Second), true),
		passObservation("depart-pass-001", hashA, domain.DirectionOut, base.Add(10*time.Minute), base.Add(10*time.Minute+time.Second), true),
		policyAcceptedFailObservation("policy-fail-001", hashB, base.Add(time.Minute), base.Add(time.Minute+time.Second)),
		failObservation("source-fail-001", hashC, base.Add(2*time.Minute), base.Add(2*time.Minute+time.Second)),
		passObservation("duplicate-in-001", hashD, domain.DirectionIn, base.Add(3*time.Minute), base.Add(3*time.Minute+time.Second), true),
		passObservation("fresh-in-001", hashE, domain.DirectionIn, base.Add(20*time.Minute), base.Add(4*time.Minute), true),
		passObservation("stale-in-001", hashE, domain.DirectionIn, base.Add(5*time.Minute), base.Add(5*time.Minute+time.Second), false),
		passObservation("out-only-001", hashF, domain.DirectionOut, base.Add(6*time.Minute), base.Add(6*time.Minute+time.Second), true),
		passObservation("duplicate-in-001", hashD, domain.DirectionIn, base.Add(3*time.Minute), base.Add(7*time.Minute), false),
		passObservation("missing-identity-001", "", domain.DirectionIn, base.Add(8*time.Minute), base.Add(8*time.Minute+time.Second), false),
		passObservation("missing-facility-001", hashG, domain.DirectionIn, base.Add(9*time.Minute), base.Add(9*time.Minute+time.Second), true),
		passObservation("missing-timestamp-001", hashG, domain.DirectionIn, time.Time{}, base.Add(9*time.Minute+2*time.Second), false),
		passObservation("unknown-identity-001", hashUnknown, domain.DirectionIn, base.Add(11*time.Minute), base.Add(11*time.Minute+time.Second), true),
	}
	observations[10].FacilityID = ""

	duration := int64(600)
	sessions := []analyticsSessionRow{
		{
			SessionID:            "session-closed-001",
			ExternalIdentityHash: hashA,
			State:                "closed",
			EntryEventID:         "arrive-pass-001",
			EntryNodeID:          "entry-node",
			EntryAt:              ptrTime(base),
			ExitEventID:          "depart-pass-001",
			ExitNodeID:           "entry-node",
			ExitAt:               ptrTime(base.Add(10 * time.Minute)),
			DurationSeconds:      &duration,
		},
		{
			SessionID:            "session-open-001",
			ExternalIdentityHash: hashD,
			State:                "open",
			EntryEventID:         "duplicate-in-001",
			EntryNodeID:          "entry-node",
			EntryAt:              ptrTime(base.Add(3 * time.Minute)),
		},
		{
			SessionID:            "session-unmatched-001",
			ExternalIdentityHash: hashF,
			State:                "unmatched_exit",
			ExitEventID:          "out-only-001",
			ExitNodeID:           "entry-node",
			ExitAt:               ptrTime(base.Add(6 * time.Minute)),
		},
	}

	report, err := BuildIngressBridgeReport(IngressBridgeFilter{
		FacilityID:          "ashtonbee",
		ZoneID:              "gym-floor",
		Since:               base.Add(-time.Minute),
		Until:               base.Add(30 * time.Minute),
		KnownIdentityHashes: known,
	}, observations, sessions)
	if err != nil {
		t.Fatalf("BuildIngressBridgeReport() error = %v", err)
	}

	arrival := findEvidence(t, report, "arrive-pass-001", true)
	if !arrival.Eligibility.CoPresenceProof.Eligible {
		t.Fatal("source pass arrival co-presence eligibility = false, want true")
	}
	if !arrival.Eligibility.PrivateDailyPresence.Eligible {
		t.Fatal("source pass arrival daily eligibility = false, want true")
	}
	if !arrival.Eligibility.ReliabilityVerification.Eligible {
		t.Fatal("source pass arrival reliability eligibility = false, want true")
	}

	departure := findEvidence(t, report, "depart-pass-001", true)
	if !departure.Eligibility.CoPresenceProof.Eligible {
		t.Fatal("source pass departure co-presence eligibility = false, want true")
	}
	if departure.Eligibility.PrivateDailyPresence.Eligible {
		t.Fatal("source pass departure daily eligibility = true, want false")
	}
	assertHasReason(t, departure.Eligibility.PrivateDailyPresence.ReasonCodes, ReasonNotArrival)
	if !departure.Eligibility.ReliabilityVerification.Eligible {
		t.Fatal("source pass departure reliability eligibility = false, want true")
	}

	policyFail := findEvidence(t, report, "policy-fail-001", false)
	if policyFail.SourceResult != "fail" || !policyFail.AcceptedPresence {
		t.Fatalf("policy accepted fail source/accepted = %s/%t, want fail/true", policyFail.SourceResult, policyFail.AcceptedPresence)
	}
	if !policyFail.Eligibility.CoPresenceProof.Eligible {
		t.Fatal("policy accepted fail co-presence eligibility = false, want true")
	}
	if !policyFail.Eligibility.PrivateDailyPresence.Eligible {
		t.Fatal("policy accepted fail daily eligibility = false, want true")
	}
	if policyFail.Eligibility.ReliabilityVerification.Eligible {
		t.Fatal("policy accepted fail reliability eligibility = true, want false")
	}
	assertHasReason(t, policyFail.Eligibility.ReliabilityVerification.ReasonCodes, ReasonAcceptedPresenceWithoutSourcePassSession)

	sourceFail := findEvidence(t, report, "source-fail-001", false)
	assertNoEligibleSignals(t, sourceFail.Eligibility)
	assertHasReason(t, sourceFail.ReasonCodes, ReasonSourceFailWithoutAcceptedPresence)

	duplicate := findEvidence(t, report, "duplicate-in-001", false)
	assertNoEligibleSignals(t, duplicate.Eligibility)
	assertHasReason(t, duplicate.ReasonCodes, ReasonDuplicateReplay)

	stale := findEvidence(t, report, "stale-in-001", false)
	assertNoEligibleSignals(t, stale.Eligibility)
	assertHasReason(t, stale.ReasonCodes, ReasonStaleEvent)

	outOnly := findEvidence(t, report, "out-only-001", true)
	assertNoEligibleSignals(t, outOnly.Eligibility)
	assertHasReason(t, outOnly.ReasonCodes, ReasonOutOfOrderLifecycle)

	missingIdentity := findEvidence(t, report, "missing-identity-001", false)
	assertHasReason(t, missingIdentity.ReasonCodes, ReasonMissingIdentity)

	missingFacility := findEvidence(t, report, "missing-facility-001", true)
	assertHasReason(t, missingFacility.ReasonCodes, ReasonMissingFacility)

	missingTimestamp := findEvidence(t, report, "missing-timestamp-001", false)
	assertHasReason(t, missingTimestamp.ReasonCodes, ReasonMissingTimestamp)

	unknownIdentity := findEvidence(t, report, "unknown-identity-001", true)
	assertHasReason(t, unknownIdentity.ReasonCodes, ReasonUnknownIdentity)

	closedSession := findSession(t, report, "session-closed-001")
	if !closedSession.Eligibility.ReliabilityVerification.Eligible {
		t.Fatal("closed source-pass session reliability eligibility = false, want true")
	}
	openSession := findSession(t, report, "session-open-001")
	assertHasReason(t, openSession.ReasonCodes, ReasonIncompleteLifecycle)
	unmatchedSession := findSession(t, report, "session-unmatched-001")
	assertHasReason(t, unmatchedSession.ReasonCodes, ReasonOutOfOrderLifecycle)

	payload, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("Marshal(report) error = %v", err)
	}
	output := string(payload)
	for _, unsafe := range []string{
		hashA,
		hashB,
		hashUnknown,
		"external_identity_hash",
		"1000123456",
		"Fixture Student",
	} {
		if strings.Contains(output, unsafe) {
			t.Fatalf("bridge report leaked %q in JSON: %s", unsafe, output)
		}
	}
	if !strings.Contains(output, "identity-") {
		t.Fatalf("bridge report missing redacted identity refs: %s", output)
	}
}

func TestPostgresStoreReadIngressBridgeUsesDurableTruth(t *testing.T) {
	store := newPostgresStore(t)
	projector := presence.NewProjector()

	if _, err := store.CreateFacilityWindowPolicy(context.Background(), CreateFacilityWindowPolicyInput{
		FacilityID:         "ashtonbee",
		StartsAt:           time.Date(2026, 4, 4, 11, 0, 0, 0, time.UTC),
		EndsAt:             time.Date(2026, 4, 4, 13, 0, 0, 0, time.UTC),
		ReasonCode:         "testing_rollout",
		CreatedByActorKind: "owner_user",
		CreatedByActorID:   "tester",
		CreatedBySurface:   "athena_cli",
	}); err != nil {
		t.Fatalf("CreateFacilityWindowPolicy() error = %v", err)
	}

	service, err := edge.NewService(
		&stubPublisher{},
		"salt",
		map[string]string{"entry-node": "entry-token", "other-node": "other-token"},
		edge.WithProjection(projector),
		edge.WithObservationRecorder(store),
		edge.WithAcceptedPresenceRecorder(store),
		edge.WithPolicyEvaluator(store),
		edge.WithPolicyAcceptanceEnabled(true),
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	requests := []struct {
		token string
		req   edge.TapRequest
	}{
		{
			token: "entry-token",
			req: edge.TapRequest{
				EventID:    "bridge-arrive-001",
				AccountRaw: "1000123456",
				Direction:  "in",
				FacilityID: "ashtonbee",
				ZoneID:     "gym-floor",
				NodeID:     "entry-node",
				ObservedAt: "2026-04-04T12:00:00Z",
				Result:     "pass",
			},
		},
		{
			token: "entry-token",
			req: edge.TapRequest{
				EventID:    "bridge-depart-001",
				AccountRaw: "1000123456",
				Direction:  "out",
				FacilityID: "ashtonbee",
				ZoneID:     "gym-floor",
				NodeID:     "entry-node",
				ObservedAt: "2026-04-04T12:10:00Z",
				Result:     "pass",
			},
		},
		{
			token: "entry-token",
			req: edge.TapRequest{
				EventID:       "bridge-policy-fail-001",
				AccountRaw:    "1000123457",
				Direction:     "in",
				FacilityID:    "ashtonbee",
				ZoneID:        "gym-floor",
				NodeID:        "entry-node",
				ObservedAt:    "2026-04-04T12:20:00Z",
				Result:        "fail",
				AccountType:   "Standard",
				Name:          "Fixture Student",
				StatusMessage: "No active access rule for this account.",
			},
		},
		{
			token: "entry-token",
			req: edge.TapRequest{
				EventID:    "bridge-source-fail-001",
				AccountRaw: "1000123458",
				Direction:  "in",
				FacilityID: "ashtonbee",
				ZoneID:     "gym-floor",
				NodeID:     "entry-node",
				ObservedAt: "2026-04-04T12:25:00Z",
				Result:     "fail",
			},
		},
		{
			token: "other-token",
			req: edge.TapRequest{
				EventID:    "bridge-other-node-001",
				AccountRaw: "1000123459",
				Direction:  "in",
				FacilityID: "ashtonbee",
				ZoneID:     "gym-floor",
				NodeID:     "other-node",
				ObservedAt: "2026-04-04T12:30:00Z",
				Result:     "pass",
			},
		},
	}
	for _, request := range requests {
		if _, err := service.AcceptTap(context.Background(), request.token, request.req); err != nil {
			t.Fatalf("AcceptTap(%s) error = %v", request.req.EventID, err)
		}
	}

	report, err := store.ReadIngressBridge(context.Background(), IngressBridgeFilter{
		FacilityID:   "ashtonbee",
		ZoneID:       "gym-floor",
		NodeID:       "entry-node",
		Since:        time.Date(2026, 4, 4, 11, 0, 0, 0, time.UTC),
		Until:        time.Date(2026, 4, 4, 13, 0, 0, 0, time.UTC),
		SessionLimit: 10,
	})
	if err != nil {
		t.Fatalf("ReadIngressBridge() error = %v", err)
	}

	if report.Summary.TotalEvidence != 4 {
		t.Fatalf("TotalEvidence = %d, want 4 scoped to entry-node", report.Summary.TotalEvidence)
	}
	if report.Summary.AcceptedSourcePass != 2 {
		t.Fatalf("AcceptedSourcePass = %d, want 2", report.Summary.AcceptedSourcePass)
	}
	if report.Summary.AcceptedPolicy != 1 {
		t.Fatalf("AcceptedPolicy = %d, want 1", report.Summary.AcceptedPolicy)
	}

	arrival := findEvidence(t, report, "bridge-arrive-001", true)
	if !arrival.Eligibility.ReliabilityVerification.Eligible {
		t.Fatal("arrival reliability eligibility = false, want true from closed source-pass session")
	}
	policyFail := findEvidence(t, report, "bridge-policy-fail-001", false)
	if !policyFail.Eligibility.PrivateDailyPresence.Eligible {
		t.Fatal("policy accepted fail daily eligibility = false, want true")
	}
	assertHasReason(t, policyFail.Eligibility.ReliabilityVerification.ReasonCodes, ReasonAcceptedPresenceWithoutSourcePassSession)
	sourceFail := findEvidence(t, report, "bridge-source-fail-001", false)
	assertHasReason(t, sourceFail.ReasonCodes, ReasonSourceFailWithoutAcceptedPresence)

	for _, evidence := range report.Evidence {
		if evidence.NodeID != "entry-node" {
			t.Fatalf("evidence node = %q, want scoped entry-node", evidence.NodeID)
		}
		if evidence.IdentityRef == "" && evidence.IdentityPresent {
			t.Fatalf("identified evidence %s missing redacted identity_ref", evidence.EventID)
		}
	}
}

func passObservation(eventID, hash string, direction domain.PresenceDirection, observedAt, storedAt time.Time, committed bool) edge.ObservationRecord {
	record := edge.ObservationRecord{
		EventID:              eventID,
		FacilityID:           "ashtonbee",
		ZoneID:               "gym-floor",
		NodeID:               "entry-node",
		Direction:            direction,
		Result:               "pass",
		Source:               domain.SourceRFID,
		ExternalIdentityHash: hash,
		ObservedAt:           observedAt,
		StoredAt:             storedAt,
	}
	if committed {
		record.CommittedAt = ptrTime(storedAt.Add(time.Second))
	}
	return record
}

func failObservation(eventID, hash string, observedAt, storedAt time.Time) edge.ObservationRecord {
	return edge.ObservationRecord{
		EventID:              eventID,
		FacilityID:           "ashtonbee",
		ZoneID:               "gym-floor",
		NodeID:               "entry-node",
		Direction:            domain.DirectionIn,
		Result:               "fail",
		Source:               domain.SourceRFID,
		ExternalIdentityHash: hash,
		ObservedAt:           observedAt,
		StoredAt:             storedAt,
		FailureReasonCode:    edge.FailureReasonUnclassifiedFail,
	}
}

func policyAcceptedFailObservation(eventID, hash string, observedAt, storedAt time.Time) edge.ObservationRecord {
	record := failObservation(eventID, hash, observedAt, storedAt)
	record.FailureReasonCode = edge.FailureReasonRecognizedDenied
	record.AcceptedAt = ptrTime(storedAt.Add(time.Second))
	record.AcceptancePath = edge.AcceptancePathFacility
	record.AcceptedReasonCode = "testing_rollout"
	return record
}

func ptrTime(value time.Time) *time.Time {
	copy := value.UTC()
	return &copy
}

func findEvidence(t *testing.T, report IngressBridgeReport, eventID string, sourceCommitted bool) IngressBridgeEvidence {
	t.Helper()
	for _, evidence := range report.Evidence {
		if evidence.EventID == eventID && evidence.SourceCommitted == sourceCommitted {
			return evidence
		}
	}
	t.Fatalf("evidence event_id=%s source_committed=%t not found in %#v", eventID, sourceCommitted, report.Evidence)
	return IngressBridgeEvidence{}
}

func findSession(t *testing.T, report IngressBridgeReport, sessionID string) IngressBridgeSession {
	t.Helper()
	for _, session := range report.Sessions {
		if session.SessionID == sessionID {
			return session
		}
	}
	t.Fatalf("session_id=%s not found in %#v", sessionID, report.Sessions)
	return IngressBridgeSession{}
}

func assertNoEligibleSignals(t *testing.T, eligibility IngressBridgeEligibility) {
	t.Helper()
	if eligibility.CoPresenceProof.Eligible || eligibility.PrivateDailyPresence.Eligible || eligibility.ReliabilityVerification.Eligible {
		t.Fatalf("eligibility = %#v, want no eligible signals", eligibility)
	}
}

func assertHasReason(t *testing.T, reasons []string, want string) {
	t.Helper()
	for _, reason := range reasons {
		if reason == want {
			return
		}
	}
	t.Fatalf("reasons = %#v, want %q", reasons, want)
}

func TestIngressBridgeInputBudgets(t *testing.T) {
	base := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	filter := IngressBridgeFilter{FacilityID: "ashtonbee", Since: base, Until: base.Add(time.Hour), SessionLimit: 1}
	observations := make([]edge.ObservationRecord, IngressBridgeMaxObservations)
	for i := range observations {
		observations[i] = passObservation(fmt.Sprintf("bounded-%04d", i), "same-hash", domain.DirectionIn, base, base.Add(time.Duration(i)*time.Millisecond), true)
	}
	report, err := BuildIngressBridgeReport(filter, observations, nil)
	if err != nil || len(report.Evidence) != IngressBridgeMaxObservations {
		t.Fatalf("exact observation limit: len=%d err=%v", len(report.Evidence), err)
	}
	if _, err := BuildIngressBridgeReport(filter, append(observations, observations[0]), nil); !errors.Is(err, ErrIngressBridgeBudget) {
		t.Fatalf("observation overflow = %v", err)
	}
	if _, err := BuildIngressBridgeReport(filter, nil, make([]analyticsSessionRow, 2)); !errors.Is(err, ErrIngressBridgeBudget) {
		t.Fatalf("session overflow = %v", err)
	}
	filter.Until = base.Add(IngressBridgeMaxWindow)
	if _, err := BuildIngressBridgeReport(filter, nil, nil); err != nil {
		t.Fatalf("exact window: %v", err)
	}
	filter.Until = filter.Until.Add(time.Nanosecond)
	if _, err := BuildIngressBridgeReport(filter, nil, nil); err == nil {
		t.Fatal("accepted excessive window")
	}
	filter.Until = base
	for _, limit := range []int{-1, 251} {
		filter.SessionLimit = limit
		if _, err := BuildIngressBridgeReport(filter, nil, nil); err == nil {
			t.Fatalf("accepted session limit %d", limit)
		}
	}
}

func TestIngressBridgePreWindowCrossNodeAndStoredOrder(t *testing.T) {
	store := newPostgresStore(t)
	ctx := context.Background()
	base := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	service, err := edge.NewService(&stubPublisher{}, "salt", map[string]string{"entry": "in-token", "exit": "out-token"}, edge.WithProjection(presence.NewProjector()), edge.WithObservationRecorder(store))
	if err != nil {
		t.Fatal(err)
	}
	taps := []struct{ id, node, direction, at string }{
		{"a-entry", "entry", "in", "2026-10-20T09:00:00Z"},
		{"b-repeat", "exit", "in", "2026-10-20T10:01:00Z"},
		{"c-exit", "exit", "out", "2026-10-20T10:02:00Z"},
		{"d-entry", "entry", "in", "2026-10-20T10:03:00Z"},
		{"e-stale", "exit", "out", "2026-10-20T10:00:00Z"},
		{"f-same-time", "exit", "out", "2026-10-20T10:03:00Z"},
	}
	for _, tap := range taps {
		token := "in-token"
		if tap.node == "exit" {
			token = "out-token"
		}
		if _, err := service.AcceptTap(ctx, token, edge.TapRequest{EventID: tap.id, AccountRaw: "synthetic-bridge-account", Direction: tap.direction, FacilityID: "ashtonbee", ZoneID: "gym-floor", NodeID: tap.node, ObservedAt: tap.at, Result: "pass"}); err != nil {
			t.Fatal(err)
		}
	}
	filter := IngressBridgeFilter{FacilityID: "ashtonbee", ZoneID: "gym-floor", NodeID: "exit", Since: base, Until: base.Add(time.Hour), SessionLimit: 10}
	report, err := store.ReadIngressBridge(ctx, filter)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Completeness.ReplayContext || report.Contract.Version != IngressBridgeVersion {
		t.Fatalf("metadata: %+v", report)
	}
	for _, id := range []string{"c-exit", "f-same-time"} {
		e := findEvidence(t, report, id, true)
		if !e.Eligibility.CoPresenceProof.Eligible || e.ProjectionReason != "exited" {
			t.Fatalf("cross-node %s: %+v", id, e)
		}
	}
	repeat := findEvidence(t, report, "b-repeat", false)
	assertHasReason(t, repeat.ReasonCodes, ReasonDuplicateReplay)
	assertNoEligibleSignals(t, repeat.Eligibility)
	stale := findEvidence(t, report, "e-stale", false)
	assertHasReason(t, stale.ReasonCodes, ReasonStaleEvent)
	assertNoEligibleSignals(t, stale.Eligibility)
	filter.Until = base.Add(2 * time.Minute)
	earlier, err := store.ReadIngressBridge(ctx, filter)
	if err != nil {
		t.Fatal(err)
	}
	if !findEvidence(t, earlier, "c-exit", true).Eligibility.CoPresenceProof.Eligible {
		t.Fatal("later marker polluted earlier replay")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := store.ReadIngressBridge(canceled, filter); err == nil {
		t.Fatal("canceled read succeeded")
	}
}

func TestIngressBridgePostgresBudgetsAndQueryPlans(t *testing.T) {
	store := newPostgresStore(t)
	ctx := context.Background()
	base := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	seed := func(facility, prefix string, count int, at time.Time) {
		t.Helper()
		_, err := store.pool.Exec(ctx, `INSERT INTO athena.edge_observations(observation_id,event_id,facility_id,zone_id,node_id,direction,result,source,external_identity_hash,observed_at,stored_at)
 SELECT $1||'-'||n,$1||'-'||n,$2,'gym-floor','entry-node','in','fail','rfid','synthetic', $3::timestamptz, $3::timestamptz + n*interval '1 microsecond' FROM generate_series(1,$4) n`, prefix, facility, at, count)
		if err != nil {
			t.Fatal(err)
		}
	}
	seed("bounded", "window", IngressBridgeMaxObservations, base)
	filter := IngressBridgeFilter{FacilityID: "bounded", Since: base, Until: base.Add(time.Hour), SessionLimit: 1}
	if report, err := store.ReadIngressBridge(ctx, filter); err != nil || len(report.Evidence) != IngressBridgeMaxObservations {
		t.Fatalf("exact DB limit len=%d err=%v", len(report.Evidence), err)
	}
	seed("bounded", "overflow", 1, base)
	if _, err := store.ReadIngressBridge(ctx, filter); !errors.Is(err, ErrIngressBridgeBudget) {
		t.Fatalf("DB overflow = %v", err)
	}
	seed("context", "history", IngressBridgeMaxContext, base.Add(-time.Hour))
	current := passObservation("context-current", edge.HashAccount("synthetic-current", "salt"), domain.DirectionIn, base, base.Add(time.Second), true)
	current.FacilityID = "context"
	if err := store.RecordObservation(ctx, current); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordCommit(ctx, edge.ObservationCommit{ObservationID: current.Identity(), EventID: current.EventID, CommittedAt: base.Add(time.Second)}); err != nil {
		t.Fatal(err)
	}
	filter.FacilityID = "context"
	report, err := store.ReadIngressBridge(ctx, filter)
	if err != nil || !report.Completeness.ReplayContext {
		t.Fatalf("exact context: %+v %v", report.Completeness, err)
	}
	if !report.Evidence[0].Eligibility.CoPresenceProof.Eligible || len(report.Sessions) != 1 {
		t.Fatalf("complete fixture has no eligible signal: %+v", report)
	}
	seed("context", "history-overflow", 1, base.Add(-time.Hour))
	report, err = store.ReadIngressBridge(ctx, filter)
	if err != nil {
		t.Fatal(err)
	}
	if report.Completeness.ReplayContext {
		t.Fatal("overflow context labelled complete")
	}
	assertNoEligibleSignals(t, report.Evidence[0].Eligibility)
	assertHasReason(t, report.Evidence[0].ReasonCodes, ReasonIncompleteReplay)
	// Cancellation must interrupt real database work, not merely an already-canceled call.
	lock, err := store.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lock.Exec(ctx, "LOCK TABLE athena.edge_observations IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	waiting, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	_, waitErr := store.ReadIngressBridge(waiting, filter)
	cancel()
	if err := lock.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if waitErr == nil {
		t.Fatal("blocked database query ignored cancellation")
	}
	for name, query := range map[string]string{
		"window":   `SELECT observation_id FROM athena.edge_observations WHERE facility_id='context' AND observed_at>='2026-10-20T10:00:00Z' ORDER BY stored_at, observed_at, event_id, observation_id LIMIT 1001`,
		"history":  `SELECT observation_id FROM athena.edge_observations WHERE facility_id='context' AND stored_at<='2026-10-20T11:00:00Z' ORDER BY stored_at, observed_at, event_id, observation_id LIMIT 4001`,
		"sessions": `SELECT session_id FROM athena.edge_sessions WHERE facility_id='context' ORDER BY COALESCE(exit_at,entry_at), session_id LIMIT 2`,
	} {
		rows, err := store.pool.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT) "+query)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatal(err)
			}
			t.Logf("%s plan: %s", name, line)
		}
		rows.Close()
	}
}

func TestIngressBridgeSessionTruthStopsAtWindow(t *testing.T) {
	base := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	observation := passObservation("entry-before-future-exit", "synthetic", domain.DirectionIn, base, base, true)
	report, err := BuildIngressBridgeReport(IngressBridgeFilter{FacilityID: "ashtonbee", Since: base, Until: base.Add(time.Hour)}, []edge.ObservationRecord{observation}, []analyticsSessionRow{{SessionID: "future-exit", ExternalIdentityHash: "synthetic", State: "closed", EntryEventID: observation.EventID, EntryAt: ptrTime(base), ExitEventID: "later", ExitAt: ptrTime(base.Add(2 * time.Hour))}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Sessions[0].State != "open" || report.Evidence[0].SessionState != "open" || report.Evidence[0].Eligibility.ReliabilityVerification.Eligible {
		t.Fatalf("future departure affected window: %+v", report)
	}
}

type bridgeFailurePublisher struct{ fail bool }

func (p *bridgeFailurePublisher) Publish(context.Context, string, []byte) error {
	if p.fail {
		return errors.New("synthetic publish failure")
	}
	return nil
}

func TestIngressBridgeUncommittedStateIsExplicitlyUnknown(t *testing.T) {
	store := newPostgresStore(t)
	publisher := &bridgeFailurePublisher{fail: true}
	service, err := edge.NewService(publisher, "salt", map[string]string{"entry": "token"}, edge.WithProjection(presence.NewProjector()), edge.WithObservationRecorder(store))
	if err != nil {
		t.Fatal(err)
	}
	request := edge.TapRequest{EventID: "failed-before-window", AccountRaw: "synthetic-uncommitted", Direction: "in", FacilityID: "ashtonbee", ZoneID: "gym-floor", NodeID: "entry", ObservedAt: "2026-10-20T09:00:00Z", Result: "pass"}
	if _, err := service.AcceptTap(context.Background(), "token", request); !errors.Is(err, edge.ErrPublishUnavailable) {
		t.Fatalf("failed publish=%v", err)
	}
	publisher.fail = false
	request.EventID = "successful-retry"
	request.ObservedAt = "2026-10-20T10:00:00Z"
	accepted, err := service.AcceptTap(context.Background(), "token", request)
	if err != nil || !accepted.Published {
		t.Fatalf("retry not accepted: %+v %v", accepted, err)
	}
	base := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	report, err := store.ReadIngressBridge(context.Background(), IngressBridgeFilter{FacilityID: "ashtonbee", Since: base, Until: base.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if report.Completeness.ReplayContext {
		t.Fatal("ambiguous uncommitted context labelled complete")
	}
	assertNoEligibleSignals(t, report.Evidence[0].Eligibility)
	assertNoEligibleSignals(t, report.Sessions[0].Eligibility)
	assertHasReason(t, report.Evidence[0].ReasonCodes, ReasonIncompleteReplay)
}

func TestIngressBridgeDatabaseSessionLimitAndHistoricalNode(t *testing.T) {
	store := newPostgresStore(t)
	ctx := context.Background()
	base := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	record := func(id, identity, node string, direction domain.PresenceDirection, at time.Time) {
		t.Helper()
		observation := passObservation(id, edge.HashAccount(identity, "salt"), direction, at, at.Add(time.Second), true)
		observation.NodeID = node
		if err := store.RecordObservation(ctx, observation); err != nil {
			t.Fatal(err)
		}
		if err := store.RecordCommit(ctx, edge.ObservationCommit{ObservationID: observation.Identity(), EventID: id, CommittedAt: at.Add(2 * time.Second)}); err != nil {
			t.Fatal(err)
		}
	}
	record("pre-entry", "first", "entry", domain.DirectionIn, base.Add(-time.Hour))
	record("future-exit", "first", "exit", domain.DirectionOut, base.Add(2*time.Hour))
	filter := IngressBridgeFilter{FacilityID: "ashtonbee", NodeID: "entry", Since: base, Until: base.Add(time.Hour), SessionLimit: 1}
	report, err := store.ReadIngressBridge(ctx, filter)
	if err != nil || len(report.Sessions) != 1 || report.Sessions[0].State != "open" {
		t.Fatalf("exact session limit: %+v %v", report.Sessions, err)
	}
	filter.NodeID = "exit"
	report, err = store.ReadIngressBridge(ctx, filter)
	if err != nil || len(report.Sessions) != 0 || len(report.Evidence) != 0 {
		t.Fatalf("future node leaked into historical scope: %+v %v", report, err)
	}
	record("second-entry", "second", "entry", domain.DirectionIn, base)
	filter.NodeID = "entry"
	if _, err := store.ReadIngressBridge(ctx, filter); !errors.Is(err, ErrIngressBridgeBudget) {
		t.Fatalf("DB session overflow=%v", err)
	}
	filter.SessionLimit = 2
	if report, err := store.ReadIngressBridge(ctx, filter); err != nil || len(report.Sessions) != 2 {
		t.Fatalf("exact expanded session cap: %+v %v", report.Sessions, err)
	}
}
