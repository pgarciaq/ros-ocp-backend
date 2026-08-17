-- #484: dual-stream daily_node_digests (all_hours + business_hours).
-- Existing rows keep all_hours via DEFAULT. Partition key (bucket_date) stays in PK.

ALTER TABLE daily_node_digests
    ADD COLUMN schedule_type digest_schedule_type NOT NULL DEFAULT 'all_hours';

ALTER TABLE daily_node_digests
    DROP CONSTRAINT daily_node_digests_pkey;

ALTER TABLE daily_node_digests
    ADD PRIMARY KEY (org_id, cluster_uuid, node, bucket_date, schedule_type);

INSERT INTO notification_code_definitions (code, name, severity, description) VALUES
    (79, 'NODE_BH_NOT_PEAK_SAFE', 'WARNING',
     'Business-hours node sizing is not peak-safe — overnight spikes outside the cluster schedule are excluded')
ON CONFLICT (code) DO UPDATE SET
    name = EXCLUDED.name,
    severity = EXCLUDED.severity,
    description = EXCLUDED.description;
