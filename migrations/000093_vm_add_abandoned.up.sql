ALTER TABLE vm_recommendations ADD COLUMN IF NOT EXISTS is_abandoned BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_vm_recommendations_abandoned
    ON vm_recommendations(org_id, cluster_uuid, is_abandoned)
    WHERE is_abandoned = TRUE;

INSERT INTO notification_code_definitions (code, name, severity, description)
VALUES (43, 'VM_ABANDONED', 'CRITICAL', 'VM has zero CPU and memory usage — likely abandoned')
ON CONFLICT (code) DO NOTHING;
