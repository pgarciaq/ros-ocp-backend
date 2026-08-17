DELETE FROM daily_node_digests WHERE schedule_type = 'business_hours';

ALTER TABLE daily_node_digests
    DROP CONSTRAINT daily_node_digests_pkey;

ALTER TABLE daily_node_digests
    DROP COLUMN schedule_type;

ALTER TABLE daily_node_digests
    ADD PRIMARY KEY (org_id, cluster_uuid, node, bucket_date);

DELETE FROM notification_code_definitions WHERE code = 79;
