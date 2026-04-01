DROP INDEX IF EXISTS athena.idx_presence_events_identity_recorded_at;
DROP INDEX IF EXISTS athena.idx_presence_events_source_recorded_at;
DROP INDEX IF EXISTS athena.idx_presence_events_facility_recorded_at;
DROP TABLE IF EXISTS athena.presence_events;
DROP TABLE IF EXISTS athena.facilities;
DROP SCHEMA IF EXISTS athena;
