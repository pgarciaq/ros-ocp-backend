INSERT INTO notification_code_definitions (code, name, severity, description) VALUES
    (31, 'SNAPSHOT_ORPHANED', 'WARNING', 'Source PVC was deleted; snapshot may no longer be needed'),
    (32, 'SNAPSHOT_NEVER_RESTORED', 'INFO', 'Snapshot has never been used to restore a volume'),
    (33, 'SNAPSHOT_REDUNDANT', 'INFO', 'Newer snapshot exists for the same PVC'),
    (34, 'SNAPSHOT_STALE', 'INFO', 'Snapshot older than retention threshold with no known usage'),
    (35, 'SNAPSHOT_MANAGED', 'INFO', 'Snapshot is managed by backup tool — review retention policy for cost optimization')
ON CONFLICT (code) DO UPDATE SET
    name = EXCLUDED.name,
    severity = EXCLUDED.severity,
    description = EXCLUDED.description;
