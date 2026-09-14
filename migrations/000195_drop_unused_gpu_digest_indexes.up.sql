-- #526: drop the pre-000187 GPU digest interval indexes.
--
-- EXPLAIN gate (recorded on the issue): pg_stat showed zero scans on both
-- indexes across a full ingest/API/recommendation workload while
-- idx_gpu_container_digests_org_cluster_sched_start (000187) and the natural
-- key serve all tenant-scoped reads. Forced-plan check notes the 000061
-- child as a plausible range path, so large deployments should drop
-- CONCURRENTLY first (see migrations/README.md); IF EXISTS makes this a
-- no-op when already gone.
--
-- Does NOT delete migrations/000061_*.sql or migrations/000080_*.sql.
-- Advisory: gpu_container_digests is on the large-table lint list.

DROP INDEX IF EXISTS idx_ros_gpu_digest_cluster_interval;
DROP INDEX IF EXISTS idx_gpu_digest_cluster_interval_node;
