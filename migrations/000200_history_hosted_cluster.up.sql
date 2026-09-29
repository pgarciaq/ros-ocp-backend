-- #634 history hosted distinction (implements #623): freeze the HC ID on
-- recommendation_history snapshots so recreated HCs cannot overwrite each
-- other's same-day rows. Postgres forbids NULL in PKs, so unassociated rows
-- carry '' (API maps back to null, never leaked) instead of true NULLs.
-- Existing rows backfill to '' (no proven association without snapshots),
-- preserving uniqueness one-to-one. Partition key recorded_at retained in
-- the key, as partitioned tables require. Plain DDL (no CONCURRENTLY):
-- fresh installs run inside a transaction (golang-migrate wraps this file);
-- for large production databases run the equivalent statements from
-- migrations/README.md first, same large-table policy as 000061/000062.
ALTER TABLE recommendation_history ADD COLUMN IF NOT EXISTS hosted_cluster_id TEXT NOT NULL DEFAULT '';

ALTER TABLE recommendation_history DROP CONSTRAINT IF EXISTS recommendation_history_pkey;
ALTER TABLE recommendation_history ADD PRIMARY KEY (org_id, cluster_uuid, namespace, workload, workload_type, container_name, term, engine, recorded_at, hosted_cluster_id);
