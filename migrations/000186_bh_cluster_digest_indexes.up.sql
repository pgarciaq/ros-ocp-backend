-- Cluster-wide business-hours digest reads (#514 node, #515 GPU).
--
-- Dual-stream BH (000182–000185) roughly doubles digest rows on enabled
-- clusters. Node recommend and GPU recommend filter on schedule_type plus a
-- date/start range for the whole cluster. The node PK leads with `node`, so
-- it does not serve the cluster-wide scan. GPU already has
-- (cluster_uuid, interval_start) from 000061; this index adds schedule_type
-- as an equality key before the range.
--
-- Column order matches the WHERE clauses, not ORDER BY:
--   node: org_id, cluster_uuid, schedule_type, bucket_date, node
--   GPU:  cluster_uuid, schedule_type, interval_start
-- (issue #514 overclaims ORDER BY node, bucket_date; keep bucket_date before
-- node so the date range is contiguous.)
--
-- Do not drop idx_ros_gpu_digest_cluster_interval (000061) or
-- idx_gpu_digest_cluster_interval_node (000080). Do not add org_id to the
-- GPU index — that table had no org_id until #512; covering org index is 000187.
--
-- golang-migrate wraps each file in a transaction, so this migration uses
-- plain CREATE INDEX IF NOT EXISTS (not CONCURRENTLY). For large production
-- databases, run the equivalent CREATE INDEX CONCURRENTLY statements from
-- migrations/README.md first; IF NOT EXISTS makes this migration a no-op
-- when the indexes already exist.
--
-- Advisory: gpu_container_digests is on the large-table lint list. Same
-- pattern as 000061 / 000173 / 000174.

CREATE INDEX IF NOT EXISTS idx_daily_node_digests_cluster_sched_date
    ON daily_node_digests (
        org_id,
        cluster_uuid,
        schedule_type,
        bucket_date,
        node
    );

CREATE INDEX IF NOT EXISTS idx_gpu_container_digests_cluster_sched_start
    ON gpu_container_digests (
        cluster_uuid,
        schedule_type,
        interval_start
    );
