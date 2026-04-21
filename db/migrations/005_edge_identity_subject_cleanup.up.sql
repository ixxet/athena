DELETE FROM athena.edge_identity_subjects s
WHERE NOT EXISTS (
    SELECT 1
    FROM athena.edge_identity_links l
    WHERE l.subject_id = s.subject_id
        AND l.facility_id = s.facility_id
)
AND NOT EXISTS (
    SELECT 1
    FROM athena.edge_access_policies p
    WHERE p.subject_id = s.subject_id
        AND p.facility_id = s.facility_id
)
AND NOT EXISTS (
    SELECT 1
    FROM athena.edge_presence_acceptances a
    WHERE a.subject_id = s.subject_id
        AND a.facility_id = s.facility_id
);
