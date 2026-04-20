package edgehistory

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/ixxet/athena/internal/edge"
)

func (s *PostgresStore) RecordAcceptance(ctx context.Context, acceptance edge.PresenceAcceptance) error {
	if err := validateAcceptance(acceptance); err != nil {
		return err
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin edge acceptance transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	record, err := loadCommittedObservation(ctx, tx, acceptance.ObservationID)
	if err != nil {
		return err
	}

	subjectID, err := ensureSubjectLinkTx(ctx, tx, record.FacilityID, record.ExternalIdentityHash, record.AccountType)
	if err != nil {
		return err
	}

	tag, err := tx.Exec(ctx, `
		INSERT INTO athena.edge_presence_acceptances (
			facility_id,
			zone_id,
			subject_id,
			source_event_kind,
			source_event_id,
			observation_id,
			event_id,
			direction,
			accepted_at,
			acceptance_path,
			accepted_reason_code,
			policy_version_id
		) VALUES (
			$1, $2, $3, 'edge_observation', $4, $5, $6, $7, $8, $9, $10, NULLIF($11, '')::uuid
		)
		ON CONFLICT (observation_id) DO NOTHING
	`,
		record.FacilityID,
		record.ZoneID,
		subjectID,
		record.Identity(),
		record.Identity(),
		record.EventID,
		string(record.Direction),
		acceptance.AcceptedAt.UTC(),
		acceptance.AcceptancePath,
		acceptance.AcceptedReasonCode,
		strings.TrimSpace(acceptance.PolicyVersionID),
	)
	if err != nil {
		return fmt.Errorf("insert edge presence acceptance: %w", err)
	}
	if tag.RowsAffected() == 0 {
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit duplicate edge acceptance transaction: %w", err)
		}
		return nil
	}

	if err := upsertIdentityMarker(ctx, tx, record, acceptance.AcceptedAt.UTC()); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit edge acceptance transaction: %w", err)
	}

	return nil
}

func (s *PostgresStore) EvaluatePolicy(ctx context.Context, evaluation edge.PolicyEvaluation) (edge.PolicyDecision, error) {
	if strings.TrimSpace(evaluation.FacilityID) == "" {
		return edge.PolicyDecision{}, fmt.Errorf("facility_id is required")
	}
	if strings.TrimSpace(evaluation.ExternalIdentityHash) == "" {
		return edge.PolicyDecision{}, fmt.Errorf("external_identity_hash is required")
	}
	if evaluation.ObservedAt.IsZero() {
		return edge.PolicyDecision{}, fmt.Errorf("observed_at is required")
	}
	if evaluation.FailureReasonCode != edge.FailureReasonRecognizedDenied {
		return edge.PolicyDecision{}, nil
	}

	subjectID, _, err := resolveSubjectID(ctx, s.pool, evaluation.FacilityID, evaluation.ExternalIdentityHash)
	if err != nil {
		return edge.PolicyDecision{}, err
	}

	query := `
		WITH latest_versions AS (
			SELECT DISTINCT ON (v.policy_id)
				p.policy_id,
				p.facility_id,
				p.subject_id,
				v.policy_version_id,
				v.policy_mode,
				v.target_selector,
				v.reason_code,
				v.is_enabled,
				v.starts_at,
				v.ends_at,
				v.created_at
			FROM athena.edge_access_policies p
			JOIN athena.edge_access_policy_versions v
				ON v.policy_id = p.policy_id
			WHERE p.facility_id = $1
			ORDER BY v.policy_id, v.version_number DESC
		)
		SELECT
			policy_version_id::text,
			policy_mode,
			reason_code
		FROM latest_versions
		WHERE
			is_enabled
			AND starts_at <= $2
			AND (ends_at IS NULL OR ends_at >= $2)
			AND (
				(subject_id IS NOT NULL AND subject_id = NULLIF($3, '')::uuid AND target_selector = 'subject_only')
				OR
				(subject_id IS NULL AND target_selector = 'recognized_denied_only')
			)
		ORDER BY
			CASE WHEN subject_id IS NOT NULL THEN 0 ELSE 1 END,
			created_at DESC,
			policy_version_id DESC
		LIMIT 1
	`

	var (
		policyVersionID string
		policyMode      string
		reasonCode      string
	)
	if err := s.pool.QueryRow(
		ctx,
		query,
		strings.TrimSpace(evaluation.FacilityID),
		evaluation.ObservedAt.UTC(),
		subjectID,
	).Scan(&policyVersionID, &policyMode, &reasonCode); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return edge.PolicyDecision{}, nil
		}
		return edge.PolicyDecision{}, fmt.Errorf("evaluate edge policy: %w", err)
	}

	return edge.PolicyDecision{
		Admitted:           true,
		AcceptancePath:     policyMode,
		AcceptedReasonCode: reasonCode,
		PolicyVersionID:    policyVersionID,
	}, nil
}

func ensureSubjectLinkTx(ctx context.Context, tx pgx.Tx, facilityID, externalIdentityHash, accountType string) (string, error) {
	subjectID, found, err := resolveSubjectID(ctx, tx, facilityID, externalIdentityHash)
	if err != nil {
		return "", err
	}
	if found {
		return subjectID, nil
	}

	var insertedSubjectID string
	if err := tx.QueryRow(ctx, `
		WITH inserted_subject AS (
			INSERT INTO athena.edge_identity_subjects (facility_id)
			VALUES ($1)
			RETURNING subject_id, facility_id
		)
		INSERT INTO athena.edge_identity_links (
			subject_id,
			facility_id,
			link_kind,
			link_key,
			link_source,
			account_type
		)
		SELECT
			subject_id,
			facility_id,
			'external_identity_hash',
			$2,
			'automatic_observation',
			$3
		FROM inserted_subject
		ON CONFLICT (facility_id, link_kind, link_key) DO NOTHING
		RETURNING subject_id::text
	`, strings.TrimSpace(facilityID), strings.TrimSpace(externalIdentityHash), strings.TrimSpace(accountType)).Scan(&insertedSubjectID); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("insert edge identity subject/link: %w", err)
		}
	}
	if strings.TrimSpace(insertedSubjectID) != "" {
		return insertedSubjectID, nil
	}

	subjectID, found, err = resolveSubjectID(ctx, tx, facilityID, externalIdentityHash)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("resolve edge identity subject after insert race")
	}
	return subjectID, nil
}

type subjectLookupQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func resolveSubjectID(ctx context.Context, query subjectLookupQuery, facilityID, externalIdentityHash string) (string, bool, error) {
	var subjectID string
	if err := query.QueryRow(ctx, `
		SELECT subject_id::text
		FROM athena.edge_identity_links
		WHERE
			facility_id = $1
			AND link_kind = 'external_identity_hash'
			AND link_key = $2
	`, strings.TrimSpace(facilityID), strings.TrimSpace(externalIdentityHash)).Scan(&subjectID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("resolve edge identity subject: %w", err)
	}
	return subjectID, true, nil
}

func nullableText(value string) any {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return trimmed
}

func validateAcceptance(acceptance edge.PresenceAcceptance) error {
	if strings.TrimSpace(acceptance.ObservationID) == "" {
		return fmt.Errorf("observation_id is required")
	}
	if strings.TrimSpace(acceptance.EventID) == "" {
		return fmt.Errorf("event_id is required")
	}
	if strings.TrimSpace(acceptance.FacilityID) == "" {
		return fmt.Errorf("facility_id is required")
	}
	if strings.TrimSpace(acceptance.ExternalIdentityHash) == "" {
		return fmt.Errorf("external_identity_hash is required")
	}
	if acceptance.AcceptedAt.IsZero() {
		return fmt.Errorf("accepted_at is required")
	}
	if acceptance.Direction != "in" && acceptance.Direction != "out" {
		return fmt.Errorf("direction %q must be one of in,out", acceptance.Direction)
	}
	switch acceptance.AcceptancePath {
	case edge.AcceptancePathTouchNetPass:
		if strings.TrimSpace(acceptance.PolicyVersionID) != "" {
			return fmt.Errorf("policy_version_id must be empty for touchnet_pass acceptance")
		}
		if strings.TrimSpace(acceptance.AcceptedReasonCode) != "" {
			return fmt.Errorf("accepted_reason_code must be empty for touchnet_pass acceptance")
		}
	case edge.AcceptancePathAlwaysAdmit, edge.AcceptancePathGraceUntil, edge.AcceptancePathFacility:
		if strings.TrimSpace(acceptance.PolicyVersionID) == "" {
			return fmt.Errorf("policy_version_id is required for policy-backed acceptance")
		}
		if strings.TrimSpace(acceptance.AcceptedReasonCode) == "" {
			return fmt.Errorf("accepted_reason_code is required for policy-backed acceptance")
		}
	default:
		return fmt.Errorf("acceptance_path %q is unsupported", acceptance.AcceptancePath)
	}
	return nil
}
