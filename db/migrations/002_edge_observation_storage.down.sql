DROP INDEX IF EXISTS athena.idx_edge_sessions_node_lookup;
DROP INDEX IF EXISTS athena.idx_edge_sessions_window_lookup;
DROP INDEX IF EXISTS athena.idx_edge_sessions_open_lookup;
DROP INDEX IF EXISTS athena.idx_edge_sessions_exit_observation;
DROP INDEX IF EXISTS athena.idx_edge_sessions_entry_observation;
DROP TABLE IF EXISTS athena.edge_sessions;

DROP INDEX IF EXISTS athena.idx_edge_observation_commits_committed_at;
DROP TABLE IF EXISTS athena.edge_observation_commits;

DROP INDEX IF EXISTS athena.idx_edge_observations_identity_observed_at;
DROP INDEX IF EXISTS athena.idx_edge_observations_facility_node_observed_at;
DROP INDEX IF EXISTS athena.idx_edge_observations_facility_zone_observed_at;
DROP INDEX IF EXISTS athena.idx_edge_observations_facility_observed_at;
DROP TABLE IF EXISTS athena.edge_observations;
