package edgehistory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type IdentityLinkRecord struct {
	LinkID      string    `json:"link_id"`
	LinkKind    string    `json:"link_kind"`
	LinkKey     string    `json:"link_key"`
	LinkSource  string    `json:"link_source"`
	AccountType string    `json:"account_type,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

type IdentitySubjectRecord struct {
	SubjectID  string               `json:"subject_id"`
	FacilityID string               `json:"facility_id"`
	CreatedAt  time.Time            `json:"created_at"`
	Links      []IdentityLinkRecord `json:"links"`
}

type CreateSubjectPolicyInput struct {
	FacilityID         string
	SubjectID          string
	PolicyMode         string
	StartsAt           time.Time
	EndsAt             *time.Time
	ReasonCode         string
	CreatedByActorKind string
	CreatedByActorID   string
	CreatedBySurface   string
}

type CreateFacilityWindowPolicyInput struct {
	FacilityID         string
	StartsAt           time.Time
	EndsAt             time.Time
	ReasonCode         string
	CreatedByActorKind string
	CreatedByActorID   string
	CreatedBySurface   string
}

type DisablePolicyInput struct {
	PolicyID           string
	CreatedByActorKind string
	CreatedByActorID   string
	CreatedBySurface   string
}

type PolicyRecord struct {
	PolicyID           string     `json:"policy_id"`
	PolicyVersionID    string     `json:"policy_version_id"`
	FacilityID         string     `json:"facility_id"`
	SubjectID          string     `json:"subject_id,omitempty"`
	VersionNumber      int        `json:"version_number"`
	IsEnabled          bool       `json:"is_enabled"`
	PolicyMode         string     `json:"policy_mode"`
	TargetSelector     string     `json:"target_selector"`
	StartsAt           time.Time  `json:"starts_at"`
	EndsAt             *time.Time `json:"ends_at,omitempty"`
	ReasonCode         string     `json:"reason_code"`
	CreatedByActorKind string     `json:"created_by_actor_kind"`
	CreatedByActorID   string     `json:"created_by_actor_id,omitempty"`
	CreatedBySurface   string     `json:"created_by_surface"`
	CreatedAt          time.Time  `json:"created_at"`
}

func (s *PostgresStore) GetIdentitySubject(ctx context.Context, facilityID, subjectID, externalIdentityHash string) (IdentitySubjectRecord, bool, error) {
	facilityID = strings.TrimSpace(facilityID)
	subjectID = strings.TrimSpace(subjectID)
	externalIdentityHash = strings.TrimSpace(externalIdentityHash)
	if facilityID == "" {
		return IdentitySubjectRecord{}, false, fmt.Errorf("facility_id is required")
	}
	if subjectID == "" && externalIdentityHash == "" {
		return IdentitySubjectRecord{}, false, fmt.Errorf("subject_id or external_identity_hash is required")
	}

	if subjectID == "" {
		var found bool
		var err error
		subjectID, found, err = resolveSubjectID(ctx, s.pool, facilityID, externalIdentityHash)
		if err != nil {
			return IdentitySubjectRecord{}, false, err
		}
		if !found {
			return IdentitySubjectRecord{}, false, nil
		}
	}

	row := s.pool.QueryRow(ctx, `
		SELECT subject_id::text, facility_id, created_at
		FROM athena.edge_identity_subjects
		WHERE subject_id = $1 AND facility_id = $2
	`, subjectID, facilityID)

	var subject IdentitySubjectRecord
	if err := row.Scan(&subject.SubjectID, &subject.FacilityID, &subject.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return IdentitySubjectRecord{}, false, nil
		}
		return IdentitySubjectRecord{}, false, fmt.Errorf("query edge identity subject: %w", err)
	}

	linkRows, err := s.pool.Query(ctx, `
		SELECT
			link_id::text,
			link_kind,
			link_key,
			link_source,
			account_type,
			created_at
		FROM athena.edge_identity_links
		WHERE subject_id = $1 AND facility_id = $2
		ORDER BY created_at ASC, link_kind ASC, link_key ASC
	`, subjectID, facilityID)
	if err != nil {
		return IdentitySubjectRecord{}, false, fmt.Errorf("query edge identity links: %w", err)
	}
	defer linkRows.Close()

	links := make([]IdentityLinkRecord, 0)
	for linkRows.Next() {
		var link IdentityLinkRecord
		if err := linkRows.Scan(&link.LinkID, &link.LinkKind, &link.LinkKey, &link.LinkSource, &link.AccountType, &link.CreatedAt); err != nil {
			return IdentitySubjectRecord{}, false, fmt.Errorf("scan edge identity link: %w", err)
		}
		links = append(links, link)
	}
	if err := linkRows.Err(); err != nil {
		return IdentitySubjectRecord{}, false, fmt.Errorf("iterate edge identity links: %w", err)
	}
	subject.Links = links
	return subject, true, nil
}

func (s *PostgresStore) AddIdentityLink(ctx context.Context, facilityID, subjectID, linkKind, linkKey, linkSource, accountType string) error {
	if strings.TrimSpace(facilityID) == "" {
		return fmt.Errorf("facility_id is required")
	}
	if strings.TrimSpace(subjectID) == "" {
		return fmt.Errorf("subject_id is required")
	}
	if err := validateLinkKind(linkKind); err != nil {
		return err
	}
	if err := validateLinkSource(linkSource); err != nil {
		return err
	}
	if strings.TrimSpace(linkKey) == "" {
		return fmt.Errorf("link_key is required")
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO athena.edge_identity_links (
			subject_id,
			facility_id,
			link_kind,
			link_key,
			link_source,
			account_type
		) VALUES ($1, $2, $3, $4, $5, $6)
	`, strings.TrimSpace(subjectID), strings.TrimSpace(facilityID), strings.TrimSpace(linkKind), strings.TrimSpace(linkKey), strings.TrimSpace(linkSource), strings.TrimSpace(accountType))
	if err != nil {
		return fmt.Errorf("insert edge identity link: %w", err)
	}
	return nil
}

func (s *PostgresStore) CreateSubjectPolicy(ctx context.Context, input CreateSubjectPolicyInput) (PolicyRecord, error) {
	if strings.TrimSpace(input.FacilityID) == "" {
		return PolicyRecord{}, fmt.Errorf("facility_id is required")
	}
	if strings.TrimSpace(input.SubjectID) == "" {
		return PolicyRecord{}, fmt.Errorf("subject_id is required")
	}
	if input.StartsAt.IsZero() {
		return PolicyRecord{}, fmt.Errorf("starts_at is required")
	}
	if err := validatePolicyReasonCode(input.ReasonCode); err != nil {
		return PolicyRecord{}, err
	}
	if err := validateActor(input.CreatedByActorKind, input.CreatedBySurface); err != nil {
		return PolicyRecord{}, err
	}

	targetSelector := "subject_only"
	switch strings.TrimSpace(input.PolicyMode) {
	case "always_admit":
		if input.EndsAt != nil {
			return PolicyRecord{}, fmt.Errorf("ends_at must be empty for always_admit")
		}
	case "grace_until":
		if input.EndsAt == nil || input.EndsAt.IsZero() {
			return PolicyRecord{}, fmt.Errorf("ends_at is required for grace_until")
		}
	default:
		return PolicyRecord{}, fmt.Errorf("policy_mode %q must be one of always_admit,grace_until", input.PolicyMode)
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return PolicyRecord{}, fmt.Errorf("begin subject policy transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var policyID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO athena.edge_access_policies (
			facility_id,
			subject_id
		) VALUES ($1, $2)
		RETURNING policy_id::text
	`, strings.TrimSpace(input.FacilityID), strings.TrimSpace(input.SubjectID)).Scan(&policyID); err != nil {
		return PolicyRecord{}, fmt.Errorf("insert edge access policy: %w", err)
	}

	record, err := insertPolicyVersionTx(ctx, tx, policyID, strings.TrimSpace(input.FacilityID), strings.TrimSpace(input.SubjectID), true, strings.TrimSpace(input.PolicyMode), targetSelector, input.StartsAt.UTC(), copyTime(input.EndsAt), strings.TrimSpace(input.ReasonCode), strings.TrimSpace(input.CreatedByActorKind), strings.TrimSpace(input.CreatedByActorID), strings.TrimSpace(input.CreatedBySurface))
	if err != nil {
		return PolicyRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PolicyRecord{}, fmt.Errorf("commit subject policy transaction: %w", err)
	}
	return record, nil
}

func (s *PostgresStore) CreateFacilityWindowPolicy(ctx context.Context, input CreateFacilityWindowPolicyInput) (PolicyRecord, error) {
	if strings.TrimSpace(input.FacilityID) == "" {
		return PolicyRecord{}, fmt.Errorf("facility_id is required")
	}
	if input.StartsAt.IsZero() {
		return PolicyRecord{}, fmt.Errorf("starts_at is required")
	}
	if input.EndsAt.IsZero() {
		return PolicyRecord{}, fmt.Errorf("ends_at is required")
	}
	if input.EndsAt.Before(input.StartsAt) {
		return PolicyRecord{}, fmt.Errorf("ends_at must be greater than or equal to starts_at")
	}
	if err := validatePolicyReasonCode(input.ReasonCode); err != nil {
		return PolicyRecord{}, err
	}
	if err := validateActor(input.CreatedByActorKind, input.CreatedBySurface); err != nil {
		return PolicyRecord{}, err
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return PolicyRecord{}, fmt.Errorf("begin facility policy transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var policyID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO athena.edge_access_policies (
			facility_id,
			subject_id
		) VALUES ($1, NULL)
		RETURNING policy_id::text
	`, strings.TrimSpace(input.FacilityID)).Scan(&policyID); err != nil {
		return PolicyRecord{}, fmt.Errorf("insert facility edge access policy: %w", err)
	}

	endsAtUTC := input.EndsAt.UTC()
	record, err := insertPolicyVersionTx(ctx, tx, policyID, strings.TrimSpace(input.FacilityID), "", true, "facility_window", "recognized_denied_only", input.StartsAt.UTC(), &endsAtUTC, strings.TrimSpace(input.ReasonCode), strings.TrimSpace(input.CreatedByActorKind), strings.TrimSpace(input.CreatedByActorID), strings.TrimSpace(input.CreatedBySurface))
	if err != nil {
		return PolicyRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PolicyRecord{}, fmt.Errorf("commit facility policy transaction: %w", err)
	}
	return record, nil
}

func (s *PostgresStore) DisablePolicy(ctx context.Context, input DisablePolicyInput) (PolicyRecord, error) {
	if strings.TrimSpace(input.PolicyID) == "" {
		return PolicyRecord{}, fmt.Errorf("policy_id is required")
	}
	if err := validateActor(input.CreatedByActorKind, input.CreatedBySurface); err != nil {
		return PolicyRecord{}, err
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return PolicyRecord{}, fmt.Errorf("begin disable policy transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	row := tx.QueryRow(ctx, `
		SELECT
			p.facility_id,
			COALESCE(p.subject_id::text, ''),
			v.version_number,
			v.policy_mode,
			v.target_selector,
			v.reason_code,
			v.starts_at
		FROM athena.edge_access_policies p
		JOIN athena.edge_access_policy_versions v
			ON v.policy_id = p.policy_id
		WHERE p.policy_id = $1
		ORDER BY v.version_number DESC
		LIMIT 1
	`, strings.TrimSpace(input.PolicyID))

	var (
		facilityID     string
		subjectID      string
		versionNumber  int
		policyMode     string
		targetSelector string
		reasonCode     string
		startsAt       time.Time
	)
	if err := row.Scan(&facilityID, &subjectID, &versionNumber, &policyMode, &targetSelector, &reasonCode, &startsAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PolicyRecord{}, fmt.Errorf("policy %q not found", strings.TrimSpace(input.PolicyID))
		}
		return PolicyRecord{}, fmt.Errorf("load latest edge policy version: %w", err)
	}

	disabledAtUTC := time.Now().UTC()
	record, err := insertPolicyVersionTx(ctx, tx, strings.TrimSpace(input.PolicyID), facilityID, subjectID, false, policyMode, targetSelector, startsAt.UTC(), &disabledAtUTC, reasonCode, strings.TrimSpace(input.CreatedByActorKind), strings.TrimSpace(input.CreatedByActorID), strings.TrimSpace(input.CreatedBySurface))
	if err != nil {
		return PolicyRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PolicyRecord{}, fmt.Errorf("commit disable policy transaction: %w", err)
	}
	return record, nil
}

func (s *PostgresStore) ListPolicies(ctx context.Context, facilityID, subjectID string, activeAt *time.Time) ([]PolicyRecord, error) {
	facilityID = strings.TrimSpace(facilityID)
	subjectID = strings.TrimSpace(subjectID)
	if facilityID == "" {
		return nil, fmt.Errorf("facility_id is required")
	}

	query := `
		WITH latest_versions AS (
			SELECT DISTINCT ON (v.policy_id)
				p.policy_id::text,
				p.facility_id,
				COALESCE(p.subject_id::text, ''),
				v.policy_version_id::text,
				v.version_number,
				v.is_enabled,
				v.policy_mode,
				v.target_selector,
				v.starts_at,
				v.ends_at,
				v.reason_code,
				v.created_by_actor_kind,
				v.created_by_actor_id,
				v.created_by_surface,
				v.created_at
			FROM athena.edge_access_policies p
			JOIN athena.edge_access_policy_versions v
				ON v.policy_id = p.policy_id
			WHERE
				p.facility_id = $1
				AND ($2 = '' OR COALESCE(p.subject_id::text, '') = $2)
			ORDER BY v.policy_id, v.version_number DESC
		)
		SELECT
			policy_id,
			policy_version_id,
			facility_id,
			subject_id,
			version_number,
			is_enabled,
			policy_mode,
			target_selector,
			starts_at,
			ends_at,
			reason_code,
			created_by_actor_kind,
			created_by_actor_id,
			created_by_surface,
			created_at
		FROM latest_versions
		ORDER BY created_at DESC, policy_id DESC
	`

	rows, err := s.pool.Query(ctx, query, facilityID, subjectID)
	if err != nil {
		return nil, fmt.Errorf("query edge policies: %w", err)
	}
	defer rows.Close()

	records := make([]PolicyRecord, 0)
	for rows.Next() {
		var record PolicyRecord
		if err := rows.Scan(
			&record.PolicyID,
			&record.PolicyVersionID,
			&record.FacilityID,
			&record.SubjectID,
			&record.VersionNumber,
			&record.IsEnabled,
			&record.PolicyMode,
			&record.TargetSelector,
			&record.StartsAt,
			&record.EndsAt,
			&record.ReasonCode,
			&record.CreatedByActorKind,
			&record.CreatedByActorID,
			&record.CreatedBySurface,
			&record.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan edge policy: %w", err)
		}
		record.StartsAt = record.StartsAt.UTC()
		record.EndsAt = copyTime(record.EndsAt)
		record.CreatedAt = record.CreatedAt.UTC()
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate edge policies: %w", err)
	}

	if activeAt == nil || activeAt.IsZero() {
		return records, nil
	}

	target := activeAt.UTC()
	filtered := make([]PolicyRecord, 0, len(records))
	for _, record := range records {
		if !record.IsEnabled {
			continue
		}
		if record.StartsAt.After(target) {
			continue
		}
		if record.EndsAt != nil && record.EndsAt.Before(target) {
			continue
		}
		filtered = append(filtered, record)
	}
	return filtered, nil
}

func insertPolicyVersionTx(ctx context.Context, tx pgx.Tx, policyID, facilityID, subjectID string, isEnabled bool, policyMode, targetSelector string, startsAt time.Time, endsAt *time.Time, reasonCode, actorKind, actorID, actorSurface string) (PolicyRecord, error) {
	var record PolicyRecord
	if err := tx.QueryRow(ctx, `
		WITH next_version AS (
			SELECT COALESCE(MAX(version_number), 0) + 1 AS version_number
			FROM athena.edge_access_policy_versions
			WHERE policy_id = $1
		)
		INSERT INTO athena.edge_access_policy_versions (
			policy_id,
			version_number,
			is_enabled,
			policy_mode,
			target_selector,
			starts_at,
			ends_at,
			reason_code,
			created_by_actor_kind,
			created_by_actor_id,
			created_by_surface
		)
		SELECT
			$1,
			version_number,
			$2,
			$3,
			$4,
			$5,
			$6,
			$7,
			$8,
			$9,
			$10
		FROM next_version
		RETURNING
			policy_version_id::text,
			version_number,
			is_enabled,
			policy_mode,
			target_selector,
			starts_at,
			ends_at,
			reason_code,
			created_by_actor_kind,
			created_by_actor_id,
			created_by_surface,
			created_at
	`, policyID, isEnabled, policyMode, targetSelector, startsAt.UTC(), endsAt, reasonCode, actorKind, actorID, actorSurface).Scan(
		&record.PolicyVersionID,
		&record.VersionNumber,
		&record.IsEnabled,
		&record.PolicyMode,
		&record.TargetSelector,
		&record.StartsAt,
		&record.EndsAt,
		&record.ReasonCode,
		&record.CreatedByActorKind,
		&record.CreatedByActorID,
		&record.CreatedBySurface,
		&record.CreatedAt,
	); err != nil {
		return PolicyRecord{}, fmt.Errorf("insert edge policy version: %w", err)
	}

	record.PolicyID = policyID
	record.FacilityID = facilityID
	record.SubjectID = subjectID
	record.StartsAt = record.StartsAt.UTC()
	record.EndsAt = copyTime(record.EndsAt)
	record.CreatedAt = record.CreatedAt.UTC()
	return record, nil
}

func validateLinkKind(linkKind string) error {
	switch strings.TrimSpace(linkKind) {
	case "external_identity_hash", "member_account", "qr_identity":
		return nil
	default:
		return fmt.Errorf("link_kind %q must be one of external_identity_hash,member_account,qr_identity", linkKind)
	}
}

func validateLinkSource(linkSource string) error {
	switch strings.TrimSpace(linkSource) {
	case "automatic_observation", "self_signup", "owner_confirmed", "trusted_import":
		return nil
	default:
		return fmt.Errorf("link_source %q must be one of automatic_observation,self_signup,owner_confirmed,trusted_import", linkSource)
	}
}

func validatePolicyReasonCode(reasonCode string) error {
	switch strings.TrimSpace(reasonCode) {
	case "testing_rollout", "alumni_exception", "semester_rollover", "owner_exception":
		return nil
	default:
		return fmt.Errorf("reason_code %q must be one of testing_rollout,alumni_exception,semester_rollover,owner_exception", reasonCode)
	}
}

func validateActor(actorKind, actorSurface string) error {
	switch strings.TrimSpace(actorKind) {
	case "owner_user", "service_account", "system":
	default:
		return fmt.Errorf("actor_kind %q must be one of owner_user,service_account,system", actorKind)
	}
	switch strings.TrimSpace(actorSurface) {
	case "athena_cli", "migration_seed", "future_admin_http":
	default:
		return fmt.Errorf("created_by_surface %q must be one of athena_cli,migration_seed,future_admin_http", actorSurface)
	}
	return nil
}
