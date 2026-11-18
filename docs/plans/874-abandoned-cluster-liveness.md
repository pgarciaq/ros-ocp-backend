# Abandoned-Cluster Liveness and Stale Retention (#874) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Mark recommendation rows stale for clusters whose `clusters.last_reported_at` is older than 14 days, and reclaim them 30 days after marking, so a decommissioned cluster stops being listed, counted and stored.

**Architecture:** Two independent clocks replace one coupled one. The sweep marks (`stale = true`, `staled_at = now()`) using `clusters.last_reported_at` as the liveness signal; the existing retention purge deletes on `staled_at`. Marking is reversible because both upserts clear `stale` and `staled_at`; deletion re-verifies liveness at delete time. `updated_at` stops being written by staleness paths, which also fixes a live `last_reported` misreport.

**Tech Stack:** Go, pgx/pgxpool, golang-migrate SQL migrations, prometheus client, testcontainers via `internal/testutil`.

**Spec:** [#874](https://github.com/pgarciaq/ros-ocp-backend/issues/874) body plus the lock and amendment comments ([lock](https://github.com/pgarciaq/ros-ocp-backend/issues/874#issuecomment-6965987080), [amendment](https://github.com/pgarciaq/ros-ocp-backend/issues/874#issuecomment-6966072761)).

## Global Constraints

- Liveness window: **14 days** (`ROS_CLUSTER_LIVENESS_HOURS`, default `336`), owner-approved 2026-11-18. Do not change it in this plan.
- `ROS_MAX_LOOKBACK_DAYS` keeps one meaning: the engine's reasoning window. The liveness sweep must not read it.
- No path whose only purpose is marking staleness may write `updated_at`.
- Both tables move together: `recommendation_sets` and `namespace_recommendation_sets`.
- The purge must never delete rows for a cluster whose `clusters.last_reported_at` is inside the window.
- No liveness predicate is added to tag filtering (#820 decided against it). Touching `internal/model/tag_filters*.go` is out of scope.
- Migrations: additive, numbered after `000222`, each with a `.down.sql`.
- New regression tests follow `docs/agents/adversarial-fixtures.md`: show the pre-fix failure, and check that its message names the bug mechanism.
- `make test-short` must pass; DB tests use `internal/testutil.SetupTestDB` and skip without Docker.

## Review Focus

1. A cluster with two `clusters` rows (two aliases) where one row is recent — must **not** be marked and must **not** be purged.
2. `last_reported_at IS NULL` (registered, never reported) — must **not** be marked, and its rows must **not** be purged by the sweep path.
3. A stale row whose `clusters` row was deleted by the Sources destroy path — must still be purged, on its legacy deadline.
4. Legacy Kruize namespace rows (`cluster_uuid` NULL, `term` NULL) — must never be marked.
5. `ROS_CLUSTER_LIVENESS_HOURS` set to `0` or negative — must fall back to `336`, never produce a future cutoff.

---

### Task 1: ADR-0358, the decision record

**Files:**
- Create: `docs/adr/0358-cluster-liveness-and-stale-retention.md`

**Interfaces:**
- Consumes: the issue body and both issue comments (spec).
- Produces: the in-repo spec every later task argues from.

- [ ] **Step 1: Write the ADR** with status `Accepted — implemented in #874`, phase `Data lifecycle / retention`, and these sections, each carrying the value from the spec verbatim:
  - Context: `stale` is only written by the per-cycle sweep, so a cluster that stops reporting is never marked; the 30-day purge never fires; lists, fleet, savings and stats count it.
  - Decision: liveness clock `clusters.last_reported_at`, window 14 d; marking writes `staled_at` and never `updated_at`; purge keyed on `staled_at` with a delete-time liveness re-verification; both recommendation tables in scope.
  - Alternatives rejected: 90 d (`ROS_MAX_LOOKBACK_DAYS`) — a dead cluster inflates aggregates for a quarter; bumping `updated_at` on mark — makes a dead cluster report "last reported: today"; a global "no cluster reported anywhere" guard — circular, fleet-global while the sweep is per-org, and a permanent no-op on single-cluster installs; a sleep exemption — `hcp_sleep_evidence` is written from reports and stops advancing when a cluster sleeps, so it cannot separate sleep from death past the window.
  - Consequences: visible stale window is `ROS_STALE_CLEANUP_DAYS` (30 d) from marking, independent of liveness; a >14 d ingestion outage hides clusters until they report again (reversible, `filter[stale]=true` still shows them); `COALESCE(staled_at, updated_at)` preserves legacy purge deadlines without a backfill.
  - Non-goals: node recommendations, node utilization, history, quota, PVC, GPU MIG, VM; alerting on absent clusters; a cause-based heartbeat guard.
  - What would reopen it: a cluster marked stale while reporting; a tenant where 14 d hides a cluster that is merely late.

- [ ] **Step 2: Run the docs checks**

Run: `make docs-lint`
Expected: `docs-lint: all checks passed`

- [ ] **Step 3: Commit**

```bash
git add docs/adr/0358-cluster-liveness-and-stale-retention.md
git commit -m "docs(#874): ADR-0358 cluster liveness and stale retention"
```

---

### Task 2: Migration — `staled_at` on both recommendation tables

**Files:**
- Create: `migrations/000223_add_staled_at_to_recommendation_tables.up.sql`
- Create: `migrations/000223_add_staled_at_to_recommendation_tables.down.sql`
- Modify: `internal/engine/migration_test_helpers_test.go` (`latestMigrationVersion` → `223`)
- Test: `internal/engine/retention_liveness_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: column `staled_at TIMESTAMPTZ NULL` on `recommendation_sets` and `namespace_recommendation_sets`.

**No backfill.** The purge uses `COALESCE(staled_at, updated_at)`, so a row that was already stale keeps the deadline it has today. A backfill statement could not be exercised by the shared test harness (migrations run against an empty database), and would be the only untested code in the change.

- [ ] **Step 1: Write the failing test**

`TestStaledAtColumnsExistAndAreNullable` — query `information_schema.columns` for both tables; assert `data_type = 'timestamp with time zone'`, `is_nullable = 'YES'`, `column_default IS NULL`.

- [ ] **Step 2: Run it**

Run: `go test ./internal/engine -run TestStaledAtColumnsExistAndAreNullable -count=1`
Expected: FAIL — no rows returned for `staled_at`.

- [ ] **Step 3: Write the migration**

Up: `ALTER TABLE recommendation_sets ADD COLUMN IF NOT EXISTS staled_at TIMESTAMPTZ;` then the same for `namespace_recommendation_sets`.
Down: `ALTER TABLE ... DROP COLUMN IF EXISTS staled_at;` for both.

- [ ] **Step 4: Run the test**

Run: `go test ./internal/engine -run TestStaledAtColumnsExistAndAreNullable -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add migrations/000223_add_staled_at_to_recommendation_tables.*.sql internal/engine/retention_liveness_test.go
git commit -m "feat(#874): add staled_at to both recommendation tables"
```

---

### Task 3: Writers — `staled_at` semantics, and stop touching `updated_at`

**Files:**
- Modify: `librobne/pgrec/write.go` (upsert `DO UPDATE` near line 69; `MarkUnreportedContainersStale` near line 141)
- Modify: `librobne/pgrec/namespace.go` (upsert `DO UPDATE` near line 62)
- Regenerate: `vendor/github.com/redhatinsights/ros-ocp-backend/librobne/pgrec/{write,namespace}.go` via `go mod vendor`
- Test: `internal/engine/retention_liveness_test.go`

**Why the test lives in `internal/engine`:** `librobne` is its own Go module, so its tests cannot import `internal/testutil` (module cycle). The parent module's tests exercise `pgrec` directly — precedent: `internal/model/recommendation_set_native_internal_test.go`.

**Why `go mod vendor`:** the parent module has `vendor/`, so `go test ./internal/...` builds the vendored copy of `librobne`, not `./librobne`. Editing the source without re-vendoring silently runs the old SQL. `make vendor-librobne-check` enforces the sync.

**Interfaces:**
- Consumes: the `staled_at` columns from Task 2.
- Produces: unchanged exported signatures. `MarkUnreportedContainersStale(ctx, pool, orgID, clusterUUID string, cycleStart time.Time) (int64, error)` keeps its shape; both upserts keep theirs.

The `DO UPDATE` clauses set `staled_at = CASE WHEN EXCLUDED.stale THEN now() ELSE NULL END`, so any writer that sets `stale = true` also records when. The **INSERT** path needs the same expression (`CASE WHEN $stale THEN now() ELSE NULL END`): a row first written already-stale (produce-time stale) would otherwise never be purgeable. `MarkUnreportedContainersStale` sets `stale = true, staled_at = COALESCE(staled_at, now())` and drops `updated_at = now()`.

- [ ] **Step 1: Write the failing tests**

- `TestMarkUnreportedContainersStale_PreservesUpdatedAt`: seed one `recommendation_sets` row with `updated_at = now() - 10 days`, `stale = false`; call the function with `cycleStart = now()`; assert `stale = true`, `staled_at` within 5 s of now, and `updated_at` **exactly** the seeded value.
- `TestWriteRecommendations_ClearsStaledAt`: seed a stale row with `staled_at` set; upsert a `types.ContainerRec` with `Stale: false`; assert `stale = false` and `staled_at IS NULL`.
- `TestWriteRecommendations_StaleRecSetsStaledAt`: upsert a rec with `Stale: true`; assert `staled_at IS NOT NULL`.
- `TestWriteNamespaceRecommendations_ClearsStaledAt`: same shape against `namespace_recommendation_sets` via `WriteNamespaceRecommendations`.

- [ ] **Step 2: Run them to confirm the first fails for the right reason**

Run: `go test ./internal/engine -run 'TestMarkUnreportedContainersStale_PreservesUpdatedAt|TestWriteRecommendations_ClearsStaledAt|TestWriteRecommendations_StaleRecSetsStaledAt|TestWriteNamespaceRecommendations_ClearsStaledAt' -count=1`
Expected: all four FAIL — `staled_at` is NULL where a mark was expected, stale rows keep their old `staled_at` after a clean report, and `updated_at` moves on mark.

- [ ] **Step 3: Implement the three writer changes**

Edit the SQL in place (INSERT column list + values, both `DO UPDATE` clauses, `MarkUnreportedContainersStale`), then `go mod vendor`. No new functions.

- [ ] **Step 4: Run the tests**

Run: the Step 2 command, plus `go mod vendor`.
Expected: PASS

- [ ] **Step 5: Mutation-check the `updated_at` test**

Temporarily restore `updated_at = now()` in `MarkUnreportedContainersStale`, `go mod vendor`, rerun Step 4, confirm the failure names `updated_at`, then revert.

- [ ] **Step 6: Commit**

```bash
git add librobne/pgrec/write.go librobne/pgrec/namespace.go \
  vendor/github.com/redhatinsights/ros-ocp-backend/librobne/pgrec/write.go \
  vendor/github.com/redhatinsights/ros-ocp-backend/librobne/pgrec/namespace.go \
  internal/engine/retention_liveness_test.go
git commit -m "fix(#874): staleness writes staled_at, never updated_at"
```

---

### Task 4: Sweep — mark abandoned clusters, wired into the retention run

**Files:**
- Modify: `internal/config/config.go` (field near line 167; `viper.SetDefault` near line 752; validation near line 1069)
- Modify: `internal/engine/retention.go` (new function after `purgeStaleRecommendations`; wire into `RunRetentionSweep` immediately before the stale purge at line ~196)
- Test: `internal/engine/retention_liveness_test.go`, `internal/api/handlers_fleet_integration_test.go`
- Test: `internal/config/config_liveness_test.go`

**Interfaces:**
- Consumes: `staled_at` (Task 2), writer semantics (Task 3).
- Produces:
  - `Config.ClusterLivenessHours int` with mapstructure key `ROS_CLUSTER_LIVENESS_HOURS`.
  - `func markAbandonedClustersStale(ctx context.Context, pool *pgxpool.Pool, livenessCutoff time.Time) (int64, error)` — unexported, returns rows marked across both tables.
  - Counter `retentionMarkedStale` named `rosocp_retention_rows_marked_stale_total`, declared beside `retentionDropped` (line 37).
  - `RunRetentionSweep` signature unchanged; it reads the window from `config.GetConfig()` alongside the existing `cfg.StaleCleanupDays`.

Predicate, applied once per table (`rs` = the recommendation table):

```sql
AND rs.stale = false
AND EXISTS (SELECT 1 FROM clusters c
            WHERE c.org_id = rs.org_id AND c.cluster_uuid = rs.cluster_uuid
              AND c.last_reported_at IS NOT NULL)
AND NOT EXISTS (SELECT 1 FROM clusters c
                WHERE c.org_id = rs.org_id AND c.cluster_uuid = rs.cluster_uuid
                  AND c.last_reported_at >= $1)
```

The first `EXISTS` implements "skip a registration that never reported"; the second is the liveness test. `NOT EXISTS` (not `EXISTS ... <`) makes a cluster with several alias rows stay live while any one of them is recent. Legacy namespace rows carry `cluster_uuid = NULL`, so the equality never matches and they are never marked.

- [ ] **Step 1: Write the failing end-to-end test first**

`TestRunRetentionSweep_MarksAndHidesAbandonedCluster` in `internal/api/handlers_fleet_integration_test.go`, following the existing `TestGetFleetSummary_Integration` harness: two clusters in one org, `last_reported_at = now() - 30 days` and `now()`; one live `recommendation_sets` row each with `estimated_savings_cents = 1050`; GET the summary (so it is cached), run `RunRetentionSweep`, GET again; assert `total_containers` and `cluster_count` drop to 1 with `active_containers`, savings to `10.50`, and `active + idle == total`.

**Fleet totals decision — REVISED (owner, 2026-11-18).** The owner first chose unfiltered totals, then asked what the recommendation was and accepted it: every counter on the card excludes stale, `total_containers` and `cluster_count` included. Reason: the UI divides `idle_containers` and `abandoned_containers` by `total_containers`, so a filtered numerator over an unfiltered denominator understates idleness by the dead cluster's weight, and `abandoned_containers` can exceed the total it is divided by; `total_containers > 0` is also the UI's has-data check, so an all-silent fleet must not render a phantom card. The test asserts the invariant `active + idle == total` so the denominators cannot drift apart again. Consequence to keep documented: the counts are as reversible as the flag, and Koku's cluster picker still lists the dead cluster until the purge.

**Blocker found in whole-branch review.** Marking alone is not enough: the list endpoints page and count through `org_container_keys.is_stale` / `org_namespace_keys`, which only `RefreshOrgMetadata` and `model.RefreshOrgNamespaceKeys` copy. Without those calls the sweep leaves `meta.count` counting the hidden cluster (3 vs 1 returned rows) and `filter[stale]=only` blind to the rows it is supposed to expose. The mark now runs both refreshes per affected org (the in-cycle writer already did exactly this), caches are invalidated only as the failure fallback, and `TestRunRetentionSweep_KeepsListCountAndRowsAgreeing` pins both lists.

- [ ] **Step 2: Run it to see the phantom**

Run: `go test ./internal/api -run TestRunRetentionSweep_MarksAndHidesAbandonedCluster -count=1`
Expected: FAIL, with the assertion message naming the abandoned cluster UUID as still counted.

- [ ] **Step 3: Write the remaining tests**

- `TestMarkAbandonedClustersStale_MarksBothTables`: dark cluster's rows in both tables marked; live cluster's untouched.
- `TestMarkAbandonedClustersStale_MultiRowClusterStaysLive`: two `clusters` rows for one UUID, one `last_reported_at = now()`, one 30 days old; assert untouched (Review Focus 1).
- `TestMarkAbandonedClustersStale_SkipsNullLastReported`: one row with `last_reported_at IS NULL`; assert untouched (Review Focus 2).
- `TestMarkAbandonedClustersStale_IgnoresLegacyNamespaceRows`: namespace row with `cluster_uuid = NULL`; assert untouched (Review Focus 4).
- `TestClusterLivenessHoursDefaultAndFallback`: unset → `336`; `0` and `-5` → `336` (Review Focus 5).

- [ ] **Step 4: Implement the config field, the sweep, and the wiring**

Default `viper.SetDefault("ROS_CLUSTER_LIVENESS_HOURS", 336)`; validation mirrors `MaxLookbackDays` (line 1056) and logs `config: ROS_CLUSTER_LIVENESS_HOURS (%d) is invalid; using 336`. In `RunRetentionSweep`, compute `livenessCutoff := time.Now().UTC().Add(-time.Duration(cfg.ClusterLivenessHours) * time.Hour)`, call the sweep before the stale purge, log the marked count at Info, and add it to `retentionMarkedStale`.

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/engine ./internal/api ./internal/config -run 'MarkAbandoned|RunRetentionSweep_MarksAndHides|ClusterLivenessHours' -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/engine/retention.go internal/config/config_test.go internal/engine/retention_liveness_test.go internal/api/handlers_fleet_integration_test.go
git commit -m "feat(#874): mark abandoned clusters stale from clusters.last_reported_at"
```

---

### Task 5: Purge — key on `staled_at`, both tables, re-verify before deleting

**Files:**
- Modify: `internal/engine/retention.go:270-300` (`purgeStaleRecommendations`), caller at line ~200
- Test: `internal/engine/retention_liveness_test.go`

**Interfaces:**
- Consumes: `markAbandonedClustersStale` and `livenessCutoff` from Task 4 (the caller already computes the cutoff).
- Produces: `func purgeStaleRecommendations(ctx context.Context, pool *pgxpool.Pool, staleCutoff, livenessCutoff time.Time) (int64, error)` — signature gains the second cutoff; returns rows purged across both tables and invalidates the three org caches once for the union of affected orgs.

Delete predicate, applied once per table:

```sql
AND rs.stale = true
AND COALESCE(rs.staled_at, rs.updated_at) < $1
AND NOT EXISTS (SELECT 1 FROM clusters c
                WHERE c.org_id = rs.org_id AND c.cluster_uuid = rs.cluster_uuid
                  AND c.last_reported_at >= $2)
```

`COALESCE` covers rows that were already stale before Task 2. The `NOT EXISTS` is the guard on the irreversible step: a cluster whose `clusters` row is gone purges (empty subquery), a cluster that is in fact reporting survives a mistaken mark.

- [ ] **Step 1: Write the failing tests**

- `TestPurge_DeletesDueSilentClusterInBothTables`: stale rows in both tables, `staled_at = now() - 31 days`, cluster `last_reported_at = now() - 60 days`; assert both rows gone.
- `TestPurge_KeepsMistakenlyMarkedCluster`: stale row, `staled_at = now() - 31 days`, cluster `last_reported_at = now()`; assert the row survives.
- `TestPurge_KeepsNotYetDue`: stale row, `staled_at = now() - 10 days`; assert it survives.
- `TestPurge_LegacyStaleRowUsesUpdatedAt`: `stale = true`, `staled_at IS NULL`, `updated_at = now() - 31 days`, cluster dark; assert it is purged.
- `TestRunRetentionSweep_MarksButDoesNotPurgeFreshMark`: cluster silent 30 days with a live row; one sweep marks it and the row **survives** — the visible window starts at the mark, so mark and purge can never happen in the same sweep.
- `TestRunRetentionSweep_PurgesWhenMarkAged`: both tables, `staled_at = now() - 31 days`, cluster silent 60 days; one sweep reclaims both.
- `TestPurge_ReclaimsRowsOfDestroyedCluster`: stale rows in both tables, no `clusters` row at all (the Sources-destroy path); assert both are reclaimed — the guard is `NOT EXISTS`, so an empty probe must not preserve them forever.
- `TestPurge_PrunesOrgListCatalogs`: catalog built as the mark leaves it (`RefreshOrgMetadata`), then purge; assert `org_container_keys` drops to zero. Dropping the purge refresh leaves the orphan.

- [ ] **Step 2: Run them**

Run: `go test ./internal/engine -run 'TestPurge_|TestRunRetentionSweep_MarkThenPurgeInOneSweep' -count=1`
Expected: FAIL — namespace rows survive `TestPurge_DeletesDueSilentClusterInBothTables`; the mistaken-mark and mark-then-purge cases fail because the current purge never sees a stale row marked in the same run.

- [ ] **Step 3: Implement the predicate for both tables**

Rewrite `purgeStaleRecommendations` to run the two DELETEs with `RETURNING org_id`, union the orgs, invalidate `fleetsummary`, `fleetheatmap` and `clustercache` once, then run `refreshOrgListCatalogs` for each org. The refresh is not cosmetic: the key catalogs prune orphans (`librobne/pgrec/metadata.go`, `internal/model/org_namespace_keys.go`), so without it a deleted cluster keeps a catalog entry that counts toward `filter[stale]=only` forever — the org whose only cluster died never runs another ingest to repair it. `TestPurge_PrunesOrgListCatalogs` pins it (mutation: dropping the refresh leaves `org_container_keys = 1`). Update the caller at line ~200.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/engine -run 'TestPurge_|TestRunRetentionSweep_MarkThenPurgeInOneSweep' -count=1`
Expected: PASS

- [ ] **Step 5: Mutation-check the re-verification**

Remove the `NOT EXISTS` clause, rerun Step 4, confirm `TestPurge_KeepsMistakenlyMarkedCluster` fails naming the deleted cluster UUID, then restore.

- [ ] **Step 6: Run the package suite and the short suite**

Run: `go test ./internal/engine -count=1` then `make test-short`
Expected: PASS both; the DB tests report `SKIP-NO-PG` under `-short`.

- [ ] **Step 7: Commit**

```bash
git add internal/engine/retention.go internal/engine/retention_liveness_test.go
git commit -m "feat(#874): purge on staled_at across both tables, re-verifying liveness"
```

---

### Task 6: Configuration docs, lifecycle docs, changelog

**Files:**
- Modify: `docs-site/configuration.md`, `docs/operations/configuration.md`
- Modify: `docs-site/features/tag-filtering.md` (the row-visibility paragraph added for #820)
- Modify: `CHANGELOG.md`
- Regenerate: `docs-site/changelog.md` (via `make docs-generate`)

**Interfaces:**
- Consumes: the config key and values from Task 4, the timings from Task 5.
- Produces: no code.

- [ ] **Step 1: Document the key in both config trees**

`ROS_CLUSTER_LIVENESS_HOURS` — default `336`; a cluster whose `clusters.last_reported_at` is older than this is marked stale; deletion follows `ROS_STALE_CLEANUP_DAYS` after marking; not related to `ROS_MAX_LOOKBACK_DAYS`.

- [ ] **Step 2: Correct the row-visibility paragraph**

Replace "keeps its place ... until its recommendation rows are purged" with the concrete rule: listed and counted until `ROS_CLUSTER_LIVENESS_HOURS` (14 d) of silence, hidden from default lists after that, deleted 30 days later; `filter[stale]=true` shows a hidden cluster; the next report restores it. Bump `> **Last verified:**` to the current date.

- [ ] **Step 3: Changelog entry**

Under `## [Unreleased] / ### Fixed`, an entry naming #874 and the user-visible effect (abandoned clusters stop being listed and counted after 14 days; their rows are reclaimed 30 days later).

- [ ] **Step 4: Regenerate and check the derived page**

Run: `make docs-generate && make docs-check-generated`
Expected: no diff reported after generation; `docs-check-generated` passes.

- [ ] **Step 5: Run all docs gates**

Run: `make docs-lint docs-drift docs-sync-check`
Expected: `docs-lint: all checks passed`, `docs-drift: all checks passed`; `docs-sync-check` unchanged except the expected DIFF set.

- [ ] **Step 6: Commit**

```bash
git add docs-site/configuration.md docs/operations/configuration.md docs-site/features/tag-filtering.md CHANGELOG.md docs-site/changelog.md
git commit -m "docs(#874): cluster liveness window, stale lifecycle, changelog"
```

---

## Self-Review

**Spec coverage:** liveness clock and 14 d (Task 4); `staled_at` and the `updated_at` collision (Tasks 2, 3); both tables (Tasks 4, 5); sleep handled by threshold, no code (Task 1 records it); non-gating surfaces out of scope (Task 1 non-goals); guard replaced by delete-time re-verification (Task 5); ADR (Task 1); docs and changelog (Task 6). Every lock-comment decision maps to a task.

**Deviation from the lock comment:** the amendment specified a backfill (`staled_at = updated_at WHERE stale = true`). The plan replaces it with `COALESCE(staled_at, updated_at)` in the purge predicate. Reason: a backfill cannot be exercised by the shared test harness, which migrates an empty database, and `COALESCE` preserves legacy deadlines exactly with testable behaviour (Task 5, `TestPurge_LegacyStaleRowUsesUpdatedAt`). This is a mechanism detail, not a decision change; record it in the implementation comment on #874.

**Type consistency:** `livenessCutoff time.Time` is produced in Task 4 and consumed unchanged by Task 5; `markAbandonedClustersStale` and the extended `purgeStaleRecommendations` are the only new/changed signatures; `Config.ClusterLivenessHours` is the only new field.

**Review Focus coverage:** each of the five lines has a test named in Tasks 4 and 5.

**Proportion:** six tasks, no function bodies; SQL predicates appear only where the shape is a decision (multi-row clusters, NULL liveness, legacy rows) and cannot be derived from the tests.
