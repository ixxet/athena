CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE SCHEMA IF NOT EXISTS athena;

CREATE TABLE athena.facilities (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL UNIQUE,
    capacity INTEGER NOT NULL CHECK (capacity > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE athena.presence_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    facility_id UUID NOT NULL REFERENCES athena.facilities(id),
    zone_id TEXT,
    external_identity_hash TEXT,
    direction TEXT NOT NULL CHECK (direction IN ('in', 'out')),
    source TEXT NOT NULL CHECK (source IN ('mock', 'rfid', 'tof', 'database', 'csv')),
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX idx_presence_events_facility_recorded_at
    ON athena.presence_events (facility_id, recorded_at DESC);

CREATE INDEX idx_presence_events_source_recorded_at
    ON athena.presence_events (source, recorded_at DESC);

CREATE INDEX idx_presence_events_identity_recorded_at
    ON athena.presence_events (external_identity_hash, recorded_at DESC);
