-- #512 PR-5: drop the cluster-only GPU BH index from 000186.
--
-- After PR-4 every tenant-scoped GPU SELECT/DELETE predicates org_id.
-- idx_gpu_container_digests_org_cluster_sched_start (000187) covers
--   WHERE org_id AND cluster_uuid AND schedule_type AND interval_start range
-- The 000186 GPU index (cluster_uuid, schedule_type, interval_start) is
-- unused write amplification. Do not delete migrations/000186_*.sql.
--
-- Keep idx_ros_gpu_digest_cluster_interval (000061) and
-- idx_gpu_digest_cluster_interval_node (000080).
--
-- golang-migrate wraps the file in one transaction, so this uses
-- DROP INDEX IF EXISTS (not CONCURRENTLY). For large production
-- databases, run DROP INDEX CONCURRENTLY from migrations/README.md first;
-- IF EXISTS makes this migration a no-op when the index is already gone.
--
-- Advisory: gpu_container_digests is on the large-table lint list.

DROP INDEX IF EXISTS idx_gpu_container_digests_cluster_sched_start;
