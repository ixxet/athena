CREATE TABLE athena.edge_identity_markers (
    facility_id TEXT NOT NULL,
    zone_id TEXT NOT NULL DEFAULT '',
    external_identity_hash TEXT NOT NULL,
    observation_id TEXT NOT NULL REFERENCES athena.edge_observations(observation_id) ON DELETE CASCADE,
    last_recorded_at TIMESTAMPTZ NOT NULL,
    last_event_id TEXT NOT NULL,
    direction TEXT NOT NULL CHECK (direction IN ('in', 'out')),
    committed_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (facility_id, zone_id, external_identity_hash)
);

CREATE INDEX idx_edge_identity_markers_identity_order
    ON athena.edge_identity_markers (facility_id, zone_id, external_identity_hash, last_recorded_at DESC, last_event_id DESC);

CREATE INDEX idx_edge_identity_markers_committed_at
    ON athena.edge_identity_markers (committed_at DESC);
