ALTER TABLE athena.edge_observations
    ADD COLUMN failure_reason_code TEXT;

UPDATE athena.edge_observations
SET failure_reason_code = CASE
    WHEN result = 'fail' THEN 'unclassified_fail'
    ELSE NULL
END
WHERE failure_reason_code IS NULL;

ALTER TABLE athena.edge_observations
    ADD CONSTRAINT chk_edge_observations_failure_reason_code
    CHECK (
        (result = 'pass' AND failure_reason_code IS NULL)
        OR
        (result = 'fail' AND failure_reason_code IN (
            'bad_account_number',
            'recognized_denied',
            'unclassified_fail'
        ))
    );

CREATE INDEX idx_edge_observations_failure_reason_observed_at
    ON athena.edge_observations (failure_reason_code, observed_at DESC)
    WHERE failure_reason_code IS NOT NULL;

CREATE TABLE athena.edge_identity_subjects (
    subject_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    facility_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (subject_id, facility_id)
);

CREATE INDEX idx_edge_identity_subjects_facility_created_at
    ON athena.edge_identity_subjects (facility_id, created_at DESC);

CREATE TABLE athena.edge_identity_links (
    link_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    subject_id UUID NOT NULL,
    facility_id TEXT NOT NULL,
    link_kind TEXT NOT NULL CHECK (link_kind IN (
        'external_identity_hash',
        'member_account',
        'qr_identity'
    )),
    link_key TEXT NOT NULL,
    link_source TEXT NOT NULL CHECK (link_source IN (
        'automatic_observation',
        'self_signup',
        'owner_confirmed',
        'trusted_import'
    )),
    account_type TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (facility_id, link_kind, link_key),
    FOREIGN KEY (subject_id, facility_id)
        REFERENCES athena.edge_identity_subjects (subject_id, facility_id)
        ON DELETE CASCADE
);

CREATE INDEX idx_edge_identity_links_subject_created_at
    ON athena.edge_identity_links (subject_id, created_at DESC);

CREATE TABLE athena.edge_access_policies (
    policy_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    facility_id TEXT NOT NULL,
    subject_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (policy_id, facility_id),
    FOREIGN KEY (subject_id, facility_id)
        REFERENCES athena.edge_identity_subjects (subject_id, facility_id)
        ON DELETE CASCADE
);

CREATE INDEX idx_edge_access_policies_facility_created_at
    ON athena.edge_access_policies (facility_id, created_at DESC);

CREATE INDEX idx_edge_access_policies_subject_created_at
    ON athena.edge_access_policies (subject_id, created_at DESC)
    WHERE subject_id IS NOT NULL;

CREATE TABLE athena.edge_access_policy_versions (
    policy_version_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    policy_id UUID NOT NULL REFERENCES athena.edge_access_policies (policy_id) ON DELETE CASCADE,
    version_number INTEGER NOT NULL CHECK (version_number > 0),
    is_enabled BOOLEAN NOT NULL,
    policy_mode TEXT NOT NULL CHECK (policy_mode IN (
        'always_admit',
        'grace_until',
        'facility_window'
    )),
    target_selector TEXT NOT NULL CHECK (target_selector IN (
        'subject_only',
        'recognized_denied_only'
    )),
    starts_at TIMESTAMPTZ NOT NULL,
    ends_at TIMESTAMPTZ,
    reason_code TEXT NOT NULL CHECK (reason_code IN (
        'testing_rollout',
        'alumni_exception',
        'semester_rollover',
        'owner_exception'
    )),
    created_by_actor_kind TEXT NOT NULL CHECK (created_by_actor_kind IN (
        'owner_user',
        'service_account',
        'system'
    )),
    created_by_actor_id TEXT NOT NULL DEFAULT '',
    created_by_surface TEXT NOT NULL CHECK (created_by_surface IN (
        'athena_cli',
        'migration_seed',
        'future_admin_http'
    )),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (policy_id, version_number),
    CHECK (
        NOT is_enabled
        OR (
            policy_mode = 'always_admit'
            AND target_selector = 'subject_only'
            AND ends_at IS NULL
        )
        OR (
            policy_mode = 'grace_until'
            AND target_selector = 'subject_only'
            AND ends_at IS NOT NULL
            AND ends_at >= starts_at
        )
        OR (
            policy_mode = 'facility_window'
            AND target_selector = 'recognized_denied_only'
            AND ends_at IS NOT NULL
            AND ends_at >= starts_at
        )
    )
);

CREATE INDEX idx_edge_access_policy_versions_latest
    ON athena.edge_access_policy_versions (policy_id, version_number DESC);

CREATE INDEX idx_edge_access_policy_versions_window
    ON athena.edge_access_policy_versions (is_enabled, starts_at, ends_at, created_at DESC);

CREATE TABLE athena.edge_presence_acceptances (
    acceptance_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    facility_id TEXT NOT NULL,
    zone_id TEXT NOT NULL DEFAULT '',
    subject_id UUID NOT NULL,
    source_event_kind TEXT NOT NULL CHECK (source_event_kind IN ('edge_observation')),
    source_event_id TEXT NOT NULL,
    observation_id TEXT NOT NULL REFERENCES athena.edge_observations (observation_id) ON DELETE CASCADE,
    event_id TEXT NOT NULL,
    direction TEXT NOT NULL CHECK (direction IN ('in', 'out')),
    accepted_at TIMESTAMPTZ NOT NULL,
    acceptance_path TEXT NOT NULL CHECK (acceptance_path IN (
        'touchnet_pass',
        'always_admit',
        'grace_until',
        'facility_window'
    )),
    accepted_reason_code TEXT NOT NULL DEFAULT '',
    policy_version_id UUID REFERENCES athena.edge_access_policy_versions (policy_version_id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (facility_id, source_event_kind, source_event_id),
    UNIQUE (observation_id),
    FOREIGN KEY (subject_id, facility_id)
        REFERENCES athena.edge_identity_subjects (subject_id, facility_id)
        ON DELETE CASCADE,
    CHECK (
        (acceptance_path = 'touchnet_pass' AND policy_version_id IS NULL AND accepted_reason_code = '')
        OR
        (acceptance_path <> 'touchnet_pass' AND policy_version_id IS NOT NULL AND accepted_reason_code <> '')
    )
);

CREATE INDEX idx_edge_presence_acceptances_facility_accepted_at
    ON athena.edge_presence_acceptances (facility_id, accepted_at DESC);

CREATE INDEX idx_edge_presence_acceptances_facility_zone_accepted_at
    ON athena.edge_presence_acceptances (facility_id, zone_id, accepted_at DESC);

CREATE INDEX idx_edge_presence_acceptances_subject_accepted_at
    ON athena.edge_presence_acceptances (subject_id, accepted_at DESC);

WITH distinct_identities AS (
    SELECT
        base.facility_id,
        base.external_identity_hash,
        gen_random_uuid() AS subject_id
    FROM (
        SELECT DISTINCT
            facility_id,
            external_identity_hash
        FROM athena.edge_observations
    ) AS base
),
inserted_subjects AS (
    INSERT INTO athena.edge_identity_subjects (
        subject_id,
        facility_id
    )
    SELECT
        subject_id,
        facility_id
    FROM distinct_identities
)
INSERT INTO athena.edge_identity_links (
    subject_id,
    facility_id,
    link_kind,
    link_key,
    link_source
)
SELECT
    subject_id,
    facility_id,
    'external_identity_hash',
    external_identity_hash,
    'automatic_observation'
FROM distinct_identities;

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
)
SELECT
    o.facility_id,
    o.zone_id,
    l.subject_id,
    'edge_observation',
    o.observation_id,
    o.observation_id,
    o.event_id,
    o.direction,
    c.committed_at,
    'touchnet_pass',
    '',
    NULL
FROM athena.edge_observation_commits c
JOIN athena.edge_observations o
    ON o.observation_id = c.observation_id
JOIN athena.edge_identity_links l
    ON l.facility_id = o.facility_id
    AND l.link_kind = 'external_identity_hash'
    AND l.link_key = o.external_identity_hash
WHERE o.result = 'pass'
ON CONFLICT (observation_id) DO NOTHING;
