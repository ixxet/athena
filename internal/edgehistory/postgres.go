package edgehistory

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ixxet/athena/internal/domain"
	"github.com/ixxet/athena/internal/edge"
)

const (
	defaultAnalyticsBucketSize   = 15 * time.Minute
	defaultAnalyticsSessionLimit = 20
)

type PostgresStore struct {
	pool *pgxpool.Pool
}

type analyticsSessionRow struct {
	SessionID            string
	ExternalIdentityHash string
	State                string
	EntryEventID         string
	EntryNodeID          string
	EntryAt              *time.Time
	ExitEventID          string
	ExitNodeID           string
	ExitAt               *time.Time
	DurationSeconds      *int64
}

func NewPostgresStore(ctx context.Context, dsn string) (*PostgresStore, error) {
	trimmed := strings.TrimSpace(dsn)
	if trimmed == "" {
		return nil, fmt.Errorf("edge Postgres dsn is required")
	}

	cfg, err := pgxpool.ParseConfig(trimmed)
	if err != nil {
		return nil, fmt.Errorf("parse edge Postgres dsn: %w", err)
	}
	if cfg.MaxConns == 0 {
		cfg.MaxConns = 4
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open edge Postgres pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping edge Postgres: %w", err)
	}

	return &PostgresStore{pool: pool}, nil
}

func (s *PostgresStore) Close() {
	if s == nil || s.pool == nil {
		return
	}
	s.pool.Close()
}

func (s *PostgresStore) RecordObservation(ctx context.Context, record edge.ObservationRecord) error {
	if err := validateRecord(record); err != nil {
		return err
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin edge observation transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if _, err := ensureSubjectLinkTx(ctx, tx, record.FacilityID, record.ExternalIdentityHash, record.AccountType); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO athena.edge_observations (
			observation_id,
			event_id,
			facility_id,
			zone_id,
			node_id,
			direction,
			result,
			source,
			external_identity_hash,
			observed_at,
			stored_at,
			account_type,
			name_present,
			failure_reason_code
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
		)
		ON CONFLICT (observation_id) DO NOTHING
	`,
		record.Identity(),
		record.EventID,
		record.FacilityID,
		record.ZoneID,
		record.NodeID,
		string(record.Direction),
		record.Result,
		string(record.Source),
		record.ExternalIdentityHash,
		record.ObservedAt.UTC(),
		record.StoredAt.UTC(),
		record.AccountType,
		record.NamePresent,
		nullableText(record.FailureReasonCode),
	); err != nil {
		return fmt.Errorf("insert edge observation: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit edge observation transaction: %w", err)
	}

	return nil
}

func (s *PostgresStore) RecordCommit(ctx context.Context, commit edge.ObservationCommit) error {
	if err := validateCommit(commit); err != nil {
		return err
	}
	if strings.TrimSpace(commit.ObservationID) == "" {
		return fmt.Errorf("observation_id is required for Postgres edge commits")
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin edge commit transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	tag, err := tx.Exec(ctx, `
		INSERT INTO athena.edge_observation_commits (
			observation_id,
			event_id,
			committed_at
		) VALUES ($1, $2, $3)
		ON CONFLICT (observation_id) DO NOTHING
	`, commit.ObservationID, commit.EventID, commit.CommittedAt.UTC())
	if err != nil {
		return fmt.Errorf("insert edge observation commit: %w", err)
	}
	if tag.RowsAffected() == 0 {
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit duplicate edge commit transaction: %w", err)
		}
		return nil
	}

	record, err := loadCommittedObservation(ctx, tx, commit.ObservationID)
	if err != nil {
		return err
	}

	switch record.Direction {
	case domain.DirectionIn:
		if err := insertOpenSession(ctx, tx, record); err != nil {
			return err
		}
	case domain.DirectionOut:
		if err := closeOrInsertExitSession(ctx, tx, record); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported direction %q for committed observation", record.Direction)
	}

	if record.Result == "pass" {
		if err := upsertIdentityMarker(ctx, tx, record, commit.CommittedAt); err != nil {
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit edge observation transaction: %w", err)
	}

	return nil
}

func (s *PostgresStore) ReadAll(ctx context.Context) ([]edge.ObservationRecord, error) {
	rows, err := s.pool.Query(ctx, observationSelect+`
		ORDER BY o.observed_at ASC, o.stored_at ASC, o.observation_id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query edge observations: %w", err)
	}
	defer rows.Close()

	return collectObservationRows(rows)
}

func (s *PostgresStore) ReadRecent(ctx context.Context, limit int) ([]edge.ObservationRecord, error) {
	if limit <= 0 {
		return s.ReadAll(ctx)
	}

	rows, err := s.pool.Query(ctx, observationSelect+`
		ORDER BY o.stored_at DESC, o.observation_id DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("query recent edge observations: %w", err)
	}
	defer rows.Close()

	records, err := collectObservationRows(rows)
	if err != nil {
		return nil, err
	}
	reverseObservations(records)
	return records, nil
}

func (s *PostgresStore) ReadMarker(ctx context.Context, key MarkerKey) (MarkerRecord, bool, error) {
	if strings.TrimSpace(key.FacilityID) == "" {
		return MarkerRecord{}, false, fmt.Errorf("facility_id is required")
	}
	if strings.TrimSpace(key.ExternalIdentityHash) == "" {
		return MarkerRecord{}, false, fmt.Errorf("external_identity_hash is required")
	}

	row := s.pool.QueryRow(ctx, `
		SELECT
			facility_id,
			zone_id,
			external_identity_hash,
			observation_id,
			last_recorded_at,
			last_event_id,
			direction,
			committed_at
		FROM athena.edge_identity_markers
		WHERE
			facility_id = $1
			AND zone_id = $2
			AND external_identity_hash = $3
	`, strings.TrimSpace(key.FacilityID), strings.TrimSpace(key.ZoneID), strings.TrimSpace(key.ExternalIdentityHash))

	var marker MarkerRecord
	if err := row.Scan(
		&marker.FacilityID,
		&marker.ZoneID,
		&marker.ExternalIdentityHash,
		&marker.ObservationID,
		&marker.LastRecordedAt,
		&marker.LastEventID,
		&marker.Direction,
		&marker.CommittedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return MarkerRecord{}, false, nil
		}
		return MarkerRecord{}, false, fmt.Errorf("query edge identity marker: %w", err)
	}

	marker.LastRecordedAt = marker.LastRecordedAt.UTC()
	marker.CommittedAt = marker.CommittedAt.UTC()

	return marker, true, nil
}

func (s *PostgresStore) ReadPublicObservations(ctx context.Context, filter PublicFilter) ([]PublicObservation, error) {
	facilityID := strings.TrimSpace(filter.FacilityID)
	if facilityID == "" {
		return nil, fmt.Errorf("facility_id is required")
	}
	if filter.Since.IsZero() {
		return nil, fmt.Errorf("since is required")
	}
	if filter.Until.IsZero() {
		return nil, fmt.Errorf("until is required")
	}

	since := filter.Since.UTC()
	until := filter.Until.UTC()
	if until.Before(since) {
		return nil, fmt.Errorf("until must be greater than or equal to since")
	}

	rows, err := s.pool.Query(ctx, `
		SELECT
			o.direction,
			o.result,
			o.observed_at,
			c.committed_at,
			a.accepted_at,
			COALESCE(a.acceptance_path, ''),
			COALESCE(a.accepted_reason_code, '')
		FROM athena.edge_observations o
		LEFT JOIN athena.edge_observation_commits c
			ON c.observation_id = o.observation_id
		LEFT JOIN athena.edge_presence_acceptances a
			ON a.observation_id = o.observation_id
		WHERE
			o.facility_id = $1
			AND o.observed_at >= $2
			AND o.observed_at <= $3
		ORDER BY o.observed_at ASC, o.result ASC, o.direction ASC
	`, facilityID, since, until)
	if err != nil {
		return nil, fmt.Errorf("query public edge observations: %w", err)
	}
	defer rows.Close()

	observations := make([]PublicObservation, 0)
	for rows.Next() {
		var (
			direction          string
			result             string
			observedAt         time.Time
			committedAt        *time.Time
			acceptedAt         *time.Time
			acceptancePathText string
			acceptedReasonCode string
		)
		if err := rows.Scan(&direction, &result, &observedAt, &committedAt, &acceptedAt, &acceptancePathText, &acceptedReasonCode); err != nil {
			return nil, fmt.Errorf("scan public edge observation: %w", err)
		}
		accepted := acceptedAt != nil || (result == "pass" && committedAt != nil)
		if acceptancePathText == "" && result == "pass" && committedAt != nil {
			acceptancePathText = edge.AcceptancePathTouchNetPass
		}
		observations = append(observations, PublicObservation{
			FacilityID:         facilityID,
			Direction:          domain.PresenceDirection(direction),
			Result:             result,
			ObservedAt:         observedAt.UTC(),
			Committed:          committedAt != nil,
			Accepted:           accepted,
			AcceptancePath:     acceptancePathText,
			AcceptedReasonCode: acceptedReasonCode,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate public edge observations: %w", err)
	}

	return observations, nil
}

func (s *PostgresStore) ReadAnalytics(ctx context.Context, filter AnalyticsFilter) (AnalyticsReport, error) {
	normalized, err := normalizeAnalyticsFilter(filter)
	if err != nil {
		return AnalyticsReport{}, err
	}

	observations, err := s.readAnalyticsObservations(ctx, normalized)
	if err != nil {
		return AnalyticsReport{}, err
	}

	sessions, err := s.readAnalyticsSessions(ctx, normalized)
	if err != nil {
		return AnalyticsReport{}, err
	}

	return buildAnalyticsReport(normalized, observations, sessions), nil
}

const observationSelect = `
	SELECT
		o.observation_id,
		o.event_id,
		o.facility_id,
		o.zone_id,
		o.node_id,
		o.direction,
		o.result,
		o.source,
		o.external_identity_hash,
		o.observed_at,
		o.stored_at,
		o.account_type,
		o.name_present,
		o.failure_reason_code,
		c.committed_at,
		a.accepted_at,
		COALESCE(a.acceptance_path, ''),
		COALESCE(a.accepted_reason_code, '')
	FROM athena.edge_observations o
	LEFT JOIN athena.edge_observation_commits c
		ON c.observation_id = o.observation_id
	LEFT JOIN athena.edge_presence_acceptances a
		ON a.observation_id = o.observation_id
`

func collectObservationRows(rows pgx.Rows) ([]edge.ObservationRecord, error) {
	records := make([]edge.ObservationRecord, 0)
	for rows.Next() {
		record, err := scanObservationRow(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate edge observations: %w", err)
	}

	return records, nil
}

func scanObservationRow(rows pgx.Rows) (edge.ObservationRecord, error) {
	var (
		record            edge.ObservationRecord
		direction         string
		source            string
		committedAt       *time.Time
		acceptedAt        *time.Time
		failureReasonCode *string
	)
	if err := rows.Scan(
		&record.ObservationID,
		&record.EventID,
		&record.FacilityID,
		&record.ZoneID,
		&record.NodeID,
		&direction,
		&record.Result,
		&source,
		&record.ExternalIdentityHash,
		&record.ObservedAt,
		&record.StoredAt,
		&record.AccountType,
		&record.NamePresent,
		&failureReasonCode,
		&committedAt,
		&acceptedAt,
		&record.AcceptancePath,
		&record.AcceptedReasonCode,
	); err != nil {
		return edge.ObservationRecord{}, fmt.Errorf("scan edge observation: %w", err)
	}
	record.Direction = domain.PresenceDirection(direction)
	record.Source = domain.PresenceSource(source)
	if failureReasonCode != nil {
		record.FailureReasonCode = strings.TrimSpace(*failureReasonCode)
	}
	if committedAt != nil {
		copy := committedAt.UTC()
		record.CommittedAt = &copy
	}
	if acceptedAt != nil {
		copy := acceptedAt.UTC()
		record.AcceptedAt = &copy
	}
	record.ObservedAt = record.ObservedAt.UTC()
	record.StoredAt = record.StoredAt.UTC()

	return record, nil
}

func upsertIdentityMarker(ctx context.Context, tx pgx.Tx, record edge.ObservationRecord, committedAt time.Time) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO athena.edge_identity_markers (
			facility_id,
			zone_id,
			external_identity_hash,
			observation_id,
			last_recorded_at,
			last_event_id,
			direction,
			committed_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (facility_id, zone_id, external_identity_hash) DO UPDATE
		SET
			observation_id = EXCLUDED.observation_id,
			last_recorded_at = EXCLUDED.last_recorded_at,
			last_event_id = EXCLUDED.last_event_id,
			direction = EXCLUDED.direction,
			committed_at = EXCLUDED.committed_at,
			updated_at = NOW()
		WHERE (
			athena.edge_identity_markers.last_recorded_at,
			athena.edge_identity_markers.last_event_id
		) < (
			EXCLUDED.last_recorded_at,
			EXCLUDED.last_event_id
		)
	`,
		record.FacilityID,
		record.ZoneID,
		record.ExternalIdentityHash,
		record.ObservationID,
		record.ObservedAt.UTC(),
		record.EventID,
		string(record.Direction),
		committedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("upsert edge identity marker: %w", err)
	}

	return nil
}

func reverseObservations(records []edge.ObservationRecord) {
	for left, right := 0, len(records)-1; left < right; left, right = left+1, right-1 {
		records[left], records[right] = records[right], records[left]
	}
}

func loadCommittedObservation(ctx context.Context, tx pgx.Tx, observationID string) (edge.ObservationRecord, error) {
	row := tx.QueryRow(ctx, `
		SELECT
			observation_id,
			event_id,
			facility_id,
			zone_id,
			node_id,
			direction,
			result,
			source,
			external_identity_hash,
			observed_at,
			stored_at,
			account_type,
			name_present,
			failure_reason_code
		FROM athena.edge_observations
		WHERE observation_id = $1
	`, observationID)

	var (
		record            edge.ObservationRecord
		direction         string
		source            string
		failureReasonCode *string
	)
	if err := row.Scan(
		&record.ObservationID,
		&record.EventID,
		&record.FacilityID,
		&record.ZoneID,
		&record.NodeID,
		&direction,
		&record.Result,
		&source,
		&record.ExternalIdentityHash,
		&record.ObservedAt,
		&record.StoredAt,
		&record.AccountType,
		&record.NamePresent,
		&failureReasonCode,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return edge.ObservationRecord{}, fmt.Errorf("load committed edge observation %q: observation not found", observationID)
		}
		return edge.ObservationRecord{}, fmt.Errorf("load committed edge observation %q: %w", observationID, err)
	}

	record.Direction = domain.PresenceDirection(direction)
	record.Source = domain.PresenceSource(source)
	if failureReasonCode != nil {
		record.FailureReasonCode = strings.TrimSpace(*failureReasonCode)
	}
	record.ObservedAt = record.ObservedAt.UTC()
	record.StoredAt = record.StoredAt.UTC()
	return record, nil
}

func insertOpenSession(ctx context.Context, tx pgx.Tx, record edge.ObservationRecord) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO athena.edge_sessions (
			facility_id,
			zone_id,
			external_identity_hash,
			state,
			entry_observation_id,
			entry_event_id,
			entry_node_id,
			entry_at
		) VALUES ($1, $2, $3, 'open', $4, $5, $6, $7)
	`,
		record.FacilityID,
		record.ZoneID,
		record.ExternalIdentityHash,
		record.Identity(),
		record.EventID,
		record.NodeID,
		record.ObservedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("insert open edge session for %q: %w", record.ObservationID, err)
	}

	return nil
}

func closeOrInsertExitSession(ctx context.Context, tx pgx.Tx, record edge.ObservationRecord) error {
	tag, err := tx.Exec(ctx, `
		WITH candidate AS (
			SELECT session_id
			FROM athena.edge_sessions
			WHERE
				facility_id = $1
				AND zone_id = $2
				AND external_identity_hash = $3
				AND state = 'open'
			ORDER BY entry_at DESC, created_at DESC, session_id DESC
			LIMIT 1
			FOR UPDATE
		)
		UPDATE athena.edge_sessions s
		SET
			state = 'closed',
			exit_observation_id = $4,
			exit_event_id = $5,
			exit_node_id = $6,
			exit_at = $7,
			duration_seconds = GREATEST(0, EXTRACT(EPOCH FROM ($7 - s.entry_at)))::BIGINT,
			updated_at = NOW()
		FROM candidate
		WHERE s.session_id = candidate.session_id
	`,
		record.FacilityID,
		record.ZoneID,
		record.ExternalIdentityHash,
		record.Identity(),
		record.EventID,
		record.NodeID,
		record.ObservedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("close open edge session for %q: %w", record.ObservationID, err)
	}
	if tag.RowsAffected() > 0 {
		return nil
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO athena.edge_sessions (
			facility_id,
			zone_id,
			external_identity_hash,
			state,
			exit_observation_id,
			exit_event_id,
			exit_node_id,
			exit_at
		) VALUES ($1, $2, $3, 'unmatched_exit', $4, $5, $6, $7)
	`,
		record.FacilityID,
		record.ZoneID,
		record.ExternalIdentityHash,
		record.Identity(),
		record.EventID,
		record.NodeID,
		record.ObservedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("insert unmatched exit session for %q: %w", record.ObservationID, err)
	}

	return nil
}

func normalizeAnalyticsFilter(filter AnalyticsFilter) (AnalyticsFilter, error) {
	normalized := AnalyticsFilter{
		FacilityID:   strings.TrimSpace(filter.FacilityID),
		ZoneID:       strings.TrimSpace(filter.ZoneID),
		NodeID:       strings.TrimSpace(filter.NodeID),
		Since:        filter.Since.UTC(),
		Until:        filter.Until.UTC(),
		BucketSize:   filter.BucketSize,
		SessionLimit: filter.SessionLimit,
	}
	if normalized.FacilityID == "" {
		return AnalyticsFilter{}, fmt.Errorf("facility_id is required")
	}
	if normalized.Since.IsZero() {
		return AnalyticsFilter{}, fmt.Errorf("since is required")
	}
	if normalized.Until.IsZero() {
		return AnalyticsFilter{}, fmt.Errorf("until is required")
	}
	if normalized.Until.Before(normalized.Since) {
		return AnalyticsFilter{}, fmt.Errorf("until must be greater than or equal to since")
	}
	if normalized.BucketSize <= 0 {
		normalized.BucketSize = defaultAnalyticsBucketSize
	}
	if normalized.SessionLimit <= 0 {
		normalized.SessionLimit = defaultAnalyticsSessionLimit
	}
	return normalized, nil
}

func (s *PostgresStore) readAnalyticsObservations(ctx context.Context, filter AnalyticsFilter) ([]edge.ObservationRecord, error) {
	rows, err := s.pool.Query(ctx, observationSelect+`
		WHERE
			o.facility_id = $1
			AND ($2 = '' OR o.zone_id = $2)
			AND ($3 = '' OR o.node_id = $3)
			AND o.observed_at >= $4
			AND o.observed_at <= $5
		ORDER BY o.observed_at ASC, o.stored_at ASC, o.observation_id ASC
	`, filter.FacilityID, filter.ZoneID, filter.NodeID, filter.Since, filter.Until)
	if err != nil {
		return nil, fmt.Errorf("query edge analytics observations: %w", err)
	}
	defer rows.Close()

	return collectObservationRows(rows)
}

func (s *PostgresStore) readAnalyticsSessions(ctx context.Context, filter AnalyticsFilter) ([]analyticsSessionRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			session_id::text,
			external_identity_hash,
			state,
			COALESCE(entry_event_id, ''),
			COALESCE(entry_node_id, ''),
			entry_at,
			COALESCE(exit_event_id, ''),
			COALESCE(exit_node_id, ''),
			exit_at,
			duration_seconds
		FROM athena.edge_sessions
		WHERE
			facility_id = $1
			AND ($2 = '' OR zone_id = $2)
			AND (
				$3 = ''
				OR entry_node_id = $3
				OR exit_node_id = $3
			)
			AND (
				(entry_at IS NOT NULL AND entry_at <= $4 AND (exit_at IS NULL OR exit_at >= $5))
				OR
				(entry_at IS NULL AND exit_at IS NOT NULL AND exit_at >= $5 AND exit_at <= $4)
			)
	`, filter.FacilityID, filter.ZoneID, filter.NodeID, filter.Until, filter.Since)
	if err != nil {
		return nil, fmt.Errorf("query edge analytics sessions: %w", err)
	}
	defer rows.Close()

	sessions := make([]analyticsSessionRow, 0)
	for rows.Next() {
		var row analyticsSessionRow
		if err := rows.Scan(
			&row.SessionID,
			&row.ExternalIdentityHash,
			&row.State,
			&row.EntryEventID,
			&row.EntryNodeID,
			&row.EntryAt,
			&row.ExitEventID,
			&row.ExitNodeID,
			&row.ExitAt,
			&row.DurationSeconds,
		); err != nil {
			return nil, fmt.Errorf("scan edge analytics session: %w", err)
		}
		if row.EntryAt != nil {
			value := row.EntryAt.UTC()
			row.EntryAt = &value
		}
		if row.ExitAt != nil {
			value := row.ExitAt.UTC()
			row.ExitAt = &value
		}
		sessions = append(sessions, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate edge analytics sessions: %w", err)
	}

	return sessions, nil
}

func buildAnalyticsReport(filter AnalyticsFilter, observations []edge.ObservationRecord, sessions []analyticsSessionRow) AnalyticsReport {
	buckets := makeBuckets(filter.Since, filter.Until, filter.BucketSize)
	nodeIndex := make(map[string]int)
	nodeBreakdown := make([]NodeBreakdown, 0)
	visitors := make(map[string]struct{})
	durations := make([]int64, 0)

	var report AnalyticsReport
	report.FacilityID = filter.FacilityID
	report.ZoneID = filter.ZoneID
	report.NodeID = filter.NodeID
	report.Since = filter.Since
	report.Until = filter.Until
	report.BucketMinutes = int(filter.BucketSize / time.Minute)
	report.FlowBuckets = buckets

	for _, observation := range observations {
		report.ObservationSummary.Total++
		switch observation.Result {
		case "pass":
			report.ObservationSummary.Pass++
			if observation.CommittedAt != nil {
				report.ObservationSummary.CommittedPass++
			}
		case "fail":
			report.ObservationSummary.Fail++
		}
		switch observation.FailureReasonCode {
		case edge.FailureReasonRecognizedDenied:
			report.ObservationSummary.RecognizedDenied++
		case edge.FailureReasonBadAccountNumber:
			report.ObservationSummary.BadAccountNumber++
		case edge.FailureReasonUnclassifiedFail:
			report.ObservationSummary.UnclassifiedFail++
		}
		if observation.AcceptedAt != nil || (observation.Result == "pass" && observation.CommittedAt != nil) {
			report.ObservationSummary.Accepted++
			switch acceptancePath(observation) {
			case edge.AcceptancePathTouchNetPass:
				report.ObservationSummary.AcceptedTouchnetPass++
			case edge.AcceptancePathAlwaysAdmit, edge.AcceptancePathGraceUntil, edge.AcceptancePathFacility:
				report.ObservationSummary.AcceptedTestingPolicy++
			}
		}

		index, ok := nodeIndex[observation.NodeID]
		if !ok {
			index = len(nodeBreakdown)
			nodeIndex[observation.NodeID] = index
			nodeBreakdown = append(nodeBreakdown, NodeBreakdown{NodeID: observation.NodeID})
		}
		nodeBreakdown[index].Total++
		if observation.Result == "pass" {
			nodeBreakdown[index].Pass++
			if observation.CommittedAt != nil {
				nodeBreakdown[index].CommittedPass++
			}
		} else if observation.Result == "fail" {
			nodeBreakdown[index].Fail++
		}
		if observation.AcceptedAt != nil || (observation.Result == "pass" && observation.CommittedAt != nil) {
			nodeBreakdown[index].Accepted++
			switch acceptancePath(observation) {
			case edge.AcceptancePathAlwaysAdmit, edge.AcceptancePathGraceUntil, edge.AcceptancePathFacility:
				nodeBreakdown[index].AcceptedTestingPolicy++
			}
		}

		bucketIndex := bucketForTime(report.FlowBuckets, observation.ObservedAt)
		if bucketIndex < 0 {
			continue
		}
		switch {
		case observation.Result == "pass" && observation.Direction == domain.DirectionIn:
			report.FlowBuckets[bucketIndex].PassIn++
		case observation.Result == "pass" && observation.Direction == domain.DirectionOut:
			report.FlowBuckets[bucketIndex].PassOut++
		case observation.Result == "fail" && observation.Direction == domain.DirectionIn:
			report.FlowBuckets[bucketIndex].FailIn++
		case observation.Result == "fail" && observation.Direction == domain.DirectionOut:
			report.FlowBuckets[bucketIndex].FailOut++
		}
	}

	sort.Slice(nodeBreakdown, func(i, j int) bool {
		return nodeBreakdown[i].NodeID < nodeBreakdown[j].NodeID
	})
	report.NodeBreakdown = nodeBreakdown

	sort.Slice(sessions, func(i, j int) bool {
		left := sessionActivityAt(sessions[i])
		right := sessionActivityAt(sessions[j])
		if left.Equal(right) {
			return sessions[i].SessionID > sessions[j].SessionID
		}
		return left.After(right)
	})

	for _, observation := range observations {
		if observation.AcceptedAt == nil && !(observation.Result == "pass" && observation.CommittedAt != nil) {
			continue
		}
		if observation.ExternalIdentityHash != "" {
			visitors[observation.ExternalIdentityHash] = struct{}{}
		}
	}

	for _, session := range sessions {
		if session.ExternalIdentityHash != "" {
			visitors[session.ExternalIdentityHash] = struct{}{}
		}
		switch session.State {
		case "open":
			report.SessionSummary.OpenCount++
		case "closed":
			report.SessionSummary.ClosedCount++
			if session.DurationSeconds != nil {
				durations = append(durations, *session.DurationSeconds)
			}
		case "unmatched_exit":
			report.SessionSummary.UnmatchedExitCount++
		}
	}
	report.SessionSummary.UniqueVisitors = len(visitors)
	report.SessionSummary.AverageDurationSeconds = averageDuration(durations)
	report.SessionSummary.MedianDurationSeconds = medianDuration(durations)
	report.SessionSummary.OccupancyAtEnd = occupancyFromAcceptedObservationsAt(filter.Until, observations)

	for index := range report.FlowBuckets {
		report.FlowBuckets[index].OccupancyEnd = occupancyFromAcceptedObservationsAt(report.FlowBuckets[index].EndedAt, observations)
	}

	sessionLimit := filter.SessionLimit
	if sessionLimit > len(sessions) {
		sessionLimit = len(sessions)
	}
	report.Sessions = make([]SessionFact, 0, sessionLimit)
	for _, session := range sessions[:sessionLimit] {
		report.Sessions = append(report.Sessions, SessionFact{
			SessionID:       session.SessionID,
			State:           session.State,
			EntryEventID:    session.EntryEventID,
			EntryNodeID:     session.EntryNodeID,
			EntryAt:         copyTime(session.EntryAt),
			ExitEventID:     session.ExitEventID,
			ExitNodeID:      session.ExitNodeID,
			ExitAt:          copyTime(session.ExitAt),
			DurationSeconds: copyInt64(session.DurationSeconds),
		})
	}

	return report
}

func makeBuckets(since, until time.Time, bucketSize time.Duration) []FlowBucket {
	if bucketSize <= 0 {
		bucketSize = defaultAnalyticsBucketSize
	}

	start := since.UTC()
	end := until.UTC()
	buckets := make([]FlowBucket, 0)
	for cursor := start; !cursor.After(end); cursor = cursor.Add(bucketSize) {
		bucketEnd := cursor.Add(bucketSize)
		if bucketEnd.After(end) {
			bucketEnd = end
		}
		buckets = append(buckets, FlowBucket{
			StartedAt: cursor,
			EndedAt:   bucketEnd,
		})
		if bucketEnd.Equal(end) {
			break
		}
	}
	return buckets
}

func bucketForTime(buckets []FlowBucket, observedAt time.Time) int {
	for index, bucket := range buckets {
		if observedAt.Before(bucket.StartedAt) {
			continue
		}
		if observedAt.After(bucket.EndedAt) {
			continue
		}
		return index
	}
	return -1
}

func sessionActivityAt(session analyticsSessionRow) time.Time {
	if session.ExitAt != nil {
		return session.ExitAt.UTC()
	}
	if session.EntryAt != nil {
		return session.EntryAt.UTC()
	}
	return time.Time{}
}

func occupancyAt(at time.Time, sessions []analyticsSessionRow) int {
	count := 0
	target := at.UTC()
	for _, session := range sessions {
		if session.EntryAt == nil {
			continue
		}
		entryAt := session.EntryAt.UTC()
		if entryAt.After(target) {
			continue
		}
		if session.ExitAt != nil && !session.ExitAt.After(target) {
			continue
		}
		count++
	}
	return count
}

func occupancyFromAcceptedObservationsAt(at time.Time, observations []edge.ObservationRecord) int {
	count := 0
	target := at.UTC()
	for _, observation := range observations {
		if observation.AcceptedAt == nil && !(observation.Result == "pass" && observation.CommittedAt != nil) {
			continue
		}
		observedAt := observation.ObservedAt.UTC()
		if observedAt.After(target) {
			continue
		}
		switch observation.Direction {
		case domain.DirectionIn:
			count++
		case domain.DirectionOut:
			count--
			if count < 0 {
				count = 0
			}
		}
	}
	return count
}

func averageDuration(durations []int64) int64 {
	if len(durations) == 0 {
		return 0
	}
	var total int64
	for _, duration := range durations {
		total += duration
	}
	return total / int64(len(durations))
}

func medianDuration(durations []int64) int64 {
	if len(durations) == 0 {
		return 0
	}
	sorted := append([]int64(nil), durations...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})
	middle := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[middle]
	}
	return (sorted[middle-1] + sorted[middle]) / 2
}

func copyTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := value.UTC()
	return &copy
}

func copyInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
