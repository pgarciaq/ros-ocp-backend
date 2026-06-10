# Large-table index migrations

PostgreSQL `CREATE INDEX CONCURRENTLY` cannot run inside a transaction. [golang-migrate](https://github.com/golang-migrate/migrate) wraps each `*.up.sql` file in a transaction, so **standard migration files must use plain `CREATE INDEX`** (blocking on large tables).

For production clusters with millions of rows, apply concurrent indexes **out of band** before or during upgrade.

## Workflow

1. **Document in migration** — Add the index DDL as a comment in the numbered migration file so operators know what the Job will create.
2. **Run K8s Job** — Apply `deploy/migrations/concurrent-index-job.yaml` (or a release-specific copy) against the target database **before** `./rosocp db migrate up` when the table is large.
3. **Run migrate** — The migration uses `CREATE INDEX IF NOT EXISTS`; if the Job already created the index, the migration is a no-op.
4. **CI lint** — `./scripts/lint-migrations.sh` fails when a new migration adds a non-`CONCURRENTLY` index on a configured large table.

## Large tables (default lint list)

Configure with `ROS_MIGRATION_LARGE_TABLES` (comma-separated). Defaults:

- `recommendation_sets`
- `namespace_recommendation_sets`
- `node_recommendations`
- `gpu_container_digests`
- `daily_container_digests`
- `recommendation_history`
- `org_container_keys`
- `snapshot_recommendation_sets`
- `snapshot_inventory`

## Example: concurrent index Job SQL

```sql
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_ros_nr_org_cluster_node_term
    ON node_recommendations (org_id, cluster_uuid, node, term);
```

## Upgrade runbook checklist

- [ ] Identify new indexes in the release migration comments
- [ ] For each index on a large table, render and apply the concurrent Job in maintenance window
- [ ] Verify `\d+ table_name` shows the new index
- [ ] Run `./rosocp db migrate up`
- [ ] Confirm migration logs show skip/no-op for pre-created indexes

See also [`migrations/README.md`](../migrations/README.md) for historical per-migration notes (000045, 000061, 000079, 000080).
