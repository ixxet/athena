CREATE TABLE athena.edge_observations (
    observation_id TEXT PRIMARY KEY,
    event_id TEXT NOT NULL,
    facility_id TEXT NOT NULL,
    zone_id TEXT NOT NULL DEFAULT '',
    node_id TEXT NOT NULL,
    direction TEXT NOT NULL CHECK (direction IN ('in', 'out')),
    result TEXT NOT NULL CHECK (result IN ('pass', 'fail')),
    source TEXT NOT NULL CHECK (source IN ('mock', 'rfid', 'tof', 'database', 'csv')),
    external_identity_hash TEXT NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    stored_at TIMESTAMPTZ NOT NULL,
    account_type TEXT NOT NULL DEFAULT '',
    name_present BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_edge_observations_facility_observed_at
    ON athena.edge_observations (facility_id, observed_at DESC);

CREATE INDEX idx_edge_observations_facility_zone_observed_at
    ON athena.edge_observations (facility_id, zone_id, observed_at DESC);

CREATE INDEX idx_edge_observations_facility_node_observed_at
    ON athena.edge_observations (facility_id, node_id, observed_at DESC);

CREATE INDEX idx_edge_observations_identity_observed_at
    ON athena.edge_observations (external_identity_hash, observed_at DESC);

CREATE TABLE athena.edge_observation_commits (
    observation_id TEXT PRIMARY KEY REFERENCES athena.edge_observations(observation_id) ON DELETE CASCADE,
    event_id TEXT NOT NULL,
    committed_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_edge_observation_commits_committed_at
    ON athena.edge_observation_commits (committed_at DESC);

CREATE TABLE athena.edge_sessions (
    session_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    facility_id TEXT NOT NULL,
    zone_id TEXT NOT NULL DEFAULT '',
    external_identity_hash TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('open', 'closed', 'unmatched_exit')),
    entry_observation_id TEXT REFERENCES athena.edge_observations(observation_id),
    entry_event_id TEXT,
    entry_node_id TEXT,
    entry_at TIMESTAMPTZ,
    exit_observation_id TEXT REFERENCES athena.edge_observations(observation_id),
    exit_event_id TEXT,
    exit_node_id TEXT,
    exit_at TIMESTAMPTZ,
    duration_seconds BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (
        (state = 'open'
            AND entry_observation_id IS NOT NULL
            AND entry_event_id IS NOT NULL
            AND entry_node_id IS NOT NULL
            AND entry_at IS NOT NULL
            AND exit_observation_id IS NULL
            AND exit_event_id IS NULL
            AND exit_node_id IS NULL
            AND exit_at IS NULL
            AND duration_seconds IS NULL)
        OR
        (state = 'closed'
            AND entry_observation_id IS NOT NULL
            AND entry_event_id IS NOT NULL
            AND entry_node_id IS NOT NULL
            AND entry_at IS NOT NULL
            AND exit_observation_id IS NOT NULL
            AND exit_event_id IS NOT NULL
            AND exit_node_id IS NOT NULL
            AND exit_at IS NOT NULL
            AND duration_seconds IS NOT NULL)
        OR
        (state = 'unmatched_exit'
            AND entry_observation_id IS NULL
            AND entry_event_id IS NULL
            AND entry_node_id IS NULL
            AND entry_at IS NULL
            AND exit_observation_id IS NOT NULL
            AND exit_event_id IS NOT NULL
            AND exit_node_id IS NOT NULL
            AND exit_at IS NOT NULL
            AND duration_seconds IS NULL)
    )
);

CREATE UNIQUE INDEX idx_edge_sessions_entry_observation
    ON athena.edge_sessions (entry_observation_id)
    WHERE entry_observation_id IS NOT NULL;

CREATE UNIQUE INDEX idx_edge_sessions_exit_observation
    ON athena.edge_sessions (exit_observation_id)
    WHERE exit_observation_id IS NOT NULL;

CREATE INDEX idx_edge_sessions_open_lookup
    ON athena.edge_sessions (facility_id, zone_id, external_identity_hash, state, entry_at DESC);

CREATE INDEX idx_edge_sessions_window_lookup
    ON athena.edge_sessions (facility_id, zone_id, entry_at, exit_at);

CREATE INDEX idx_edge_sessions_node_lookup
    ON athena.edge_sessions (facility_id, entry_node_id, exit_node_id);
