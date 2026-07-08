-- Covering index for the recommendation digest query (issue #263).
--
-- The RecommendWorkloadsStreaming query filters on (org_id, cluster_uuid,
-- schedule_type, bucket_date range) and sorts by (namespace, workload,
-- workload_type, container_name, bucket_date). The existing lookback index
-- (migration 000076) covers only the WHERE clause; PostgreSQL must then do
-- an external merge sort of the result set, which spills to disk at scale
-- (14-19 MB per cluster with 4K containers).
--
-- This index puts equality predicates first, then the ORDER BY columns,
-- so PostgreSQL can satisfy both filter and sort from a single B-tree
-- index scan — eliminating the disk sort entirely.
--
-- golang-migrate wraps each file in a transaction, so this migration uses
-- plain CREATE INDEX IF NOT EXISTS (not CONCURRENTLY). For large production
-- databases, run the equivalent CREATE INDEX CONCURRENTLY statement from
-- migrations/README.md first; IF NOT EXISTS makes this migration a no-op
-- when the index already exists.

CREATE INDEX IF NOT EXISTS idx_daily_container_digests_recommend
    ON daily_container_digests (
        org_id,
        cluster_uuid,
        schedule_type,
        namespace,
        workload,
        workload_type,
        container_name,
        bucket_date
    );
