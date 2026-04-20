DROP INDEX IF EXISTS athena.idx_edge_presence_acceptances_subject_accepted_at;
DROP INDEX IF EXISTS athena.idx_edge_presence_acceptances_facility_zone_accepted_at;
DROP INDEX IF EXISTS athena.idx_edge_presence_acceptances_facility_accepted_at;
DROP TABLE IF EXISTS athena.edge_presence_acceptances;

DROP INDEX IF EXISTS athena.idx_edge_access_policy_versions_window;
DROP INDEX IF EXISTS athena.idx_edge_access_policy_versions_latest;
DROP TABLE IF EXISTS athena.edge_access_policy_versions;

DROP INDEX IF EXISTS athena.idx_edge_access_policies_subject_created_at;
DROP INDEX IF EXISTS athena.idx_edge_access_policies_facility_created_at;
DROP TABLE IF EXISTS athena.edge_access_policies;

DROP INDEX IF EXISTS athena.idx_edge_identity_links_subject_created_at;
DROP TABLE IF EXISTS athena.edge_identity_links;

DROP INDEX IF EXISTS athena.idx_edge_identity_subjects_facility_created_at;
DROP TABLE IF EXISTS athena.edge_identity_subjects;

DROP INDEX IF EXISTS athena.idx_edge_observations_failure_reason_observed_at;
ALTER TABLE athena.edge_observations
    DROP CONSTRAINT IF EXISTS chk_edge_observations_failure_reason_code;
ALTER TABLE athena.edge_observations
    DROP COLUMN IF EXISTS failure_reason_code;
