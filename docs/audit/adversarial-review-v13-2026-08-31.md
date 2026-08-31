# Adversarial Due Diligence Review — ros-ocp-backend

## Version & Date
Version: 13 | Date: 2026-08-31 | Reviewer: AI-assisted (incremental, librobne extract + CLI + business-hours extra cases)

## Scope

Incremental review of `pgarciaq-rosocp-superpowers-phase17` since [v12](adversarial-review-v12-2026-07-25.md) (2026-07-25). This is not a whole-repo re-audit. Focus:

- **librobne** nested module: CSV parse/classify/load, digest compute, `pgdigest` / `pgrec`, entity recommend packages
- **Ingest dedup** onto `librobne/csv.ForEach*` (#475, #501) and `pgdigest.ForEachSchedule` (#476)
- **robne CLI** Phase 1–3 (CSV recommend, PostgreSQL upsert, BH siblings, explain)
- **Business-hours extra cases:** overnight windows (#488), node/GPU/VM/timeslicing dual-stream + codes 79–82 (#484–#492), namespace list omit (#497), Peak hours UI/docs
- **Out of scope:** robne-operator (#138), koku `./` tar matching (#466 closed), NISE GPU row explosion (#502), operator DCGM join (#503)

Prior reviews: v12 (2026-07-25). Historical record is unchanged; this document appends.

## Executive Summary

Phase 17 is the largest engine-shaped delta since the package split: recommendation compute, CSV parse, and digest I/O now live in `librobne`, the processor streams through the same parsers, and business-hours sizing exists for every major entity (not only containers). The extract is disciplined — org/cluster identity is stamped from caller YAML/headers rather than CSV cells, SQL is parameterized, tar members are parsed in-stream (not extracted to disk), overnight BH has tests and a PUT warning, and codes 79–82 are appended as `[]int16`. `MergeNotificationCodes` is slice-based ([#509](https://github.com/pgarciaq/ros-ocp-backend/issues/509)); the 1–63 `uint64` bitmap is removed.

The review found **no Critical and no High** issues. The three Medium findings (skip counts, CLI PVC/quota LWW, directory vs tar `Load`) and all four ticketed Low/Informational findings are **resolved** on this branch ([#504](https://github.com/pgarciaq/ros-ocp-backend/issues/504)–[#510](https://github.com/pgarciaq/ros-ocp-backend/issues/510) closed). Findings 6, 8, and 10 stay accepted / docs-when-convenient with no tickets.

v12 finding 1 (`vmTerm` string interpolation) is **resolved** (now `$N`). v12 finding 7 (`ctx.Err()` in CSV loops) is **resolved** on `ForEach*` (every 10_000 rows). See [Prior Findings Status](#prior-findings-status-v12--v13). v11 bitmap merge is **resolved** via [#509](https://github.com/pgarciaq/ros-ocp-backend/issues/509) (slice merge; `NotificationCodeBitmap` removed).

Overall assessment: **Good / Very Good** for the extract and BH extra cases. v13 remediations are closed. Next real work is GPU producers ([#502](https://github.com/pgarciaq/ros-ocp-backend/issues/502) / [#503](https://github.com/pgarciaq/ros-ocp-backend/issues/503)). Parked: robne-operator ([#138](https://github.com/pgarciaq/ros-ocp-backend/issues/138)). Postponed leftovers: [#511](https://github.com/pgarciaq/ros-ocp-backend/issues/511)–[#513](https://github.com/pgarciaq/ros-ocp-backend/issues/513).

Trackers on `pgarciaq/ros-ocp-backend` (v13 findings only): [#504](https://github.com/pgarciaq/ros-ocp-backend/issues/504) [#505](https://github.com/pgarciaq/ros-ocp-backend/issues/505) [#506](https://github.com/pgarciaq/ros-ocp-backend/issues/506) [#508](https://github.com/pgarciaq/ros-ocp-backend/issues/508) [#507](https://github.com/pgarciaq/ros-ocp-backend/issues/507) [#510](https://github.com/pgarciaq/ros-ocp-backend/issues/510) [#509](https://github.com/pgarciaq/ros-ocp-backend/issues/509) — **all closed**. Findings 6, 8, and 10 stay in this document without issues (accepted / docs-when-convenient).

## Scorecard

| Dimension | Rating | Key gap |
|-----------|--------|---------|
| Security | ★★★★☆ | Tar is stream-parsed (no extract-to-disk). PVC unique key still omits `org_id` (conflict no longer rewrites it; [#508](https://github.com/pgarciaq/ros-ocp-backend/issues/508); follow-on [#511](https://github.com/pgarciaq/ros-ocp-backend/issues/511)). No gzip/row cap on `Load` (finding 6, accepted). |
| Correctness | ★★★★★ | Overnight BH + DST tests ([#507](https://github.com/pgarciaq/ros-ocp-backend/issues/507)). CLI PVC/quota merge matches ingest ([#505](https://github.com/pgarciaq/ros-ocp-backend/issues/505)). Directory and tar `Load` share one missing-column policy ([#506](https://github.com/pgarciaq/ros-ocp-backend/issues/506)). Merge keeps codes ≥ 1 ([#509](https://github.com/pgarciaq/ros-ocp-backend/issues/509)). |
| Auditability | ★★★★☆ | Container/namespace/PVC ingest now increment skip metrics ([#504](https://github.com/pgarciaq/ros-ocp-backend/issues/504)), matching VM/snapshot/quota. |
| Operational robustness | ★★★★☆ | Processor `ForEach*` honors `ctx`. CLI `ParseRows`/`Load` use `context.Background()` (finding 10, accepted). |
| Performance | ★★★★☆ | Streaming ingest is the right shape. CLI `Load` materializes every entity slice from a tarball. |
| Design quality | ★★★★☆ | Nested module + replace is clean. GPU digests still have no `org_id` ([#512](https://github.com/pgarciaq/ros-ocp-backend/issues/512)). Timeslicing BH is cluster-window; GPU container BH is namespace-window (finding 8, docs-when-convenient). |
| Maintainability | ★★★★☆ | CI fails on vendored librobne drift ([#510](https://github.com/pgarciaq/ros-ocp-backend/issues/510)). `compat.go` still ~400 lines ([#513](https://github.com/pgarciaq/ros-ocp-backend/issues/513)). |
| Governance | ★★★★★ | CHANGELOG, CLI spec, issue split (#465/#502/#503), BH notification catalog tests. |

## Prior Findings Status (v12 → v13)

The first draft of this table marked v12-2/3/4/8/10/11 as **Still Open** with “not in this delta.” That was wrong: those items were not in the librobne/BH delta, but they had already been fixed or documented after v12. This table is the re-verified status. No v12 leftovers were opened as GitHub issues.

| v12 # | Title | Severity | Status | Verification |
|--------|-------|----------|--------|--------------|
| 1 | `vmTerm` SQL string interpolation | Medium | **Resolved** | `handlers_savings_summary.go` uses `vmTermRef := fmt.Sprintf("$%d", vmTermParam)` and bind args |
| 2 | Reship poller discards `RetryPending` | Medium | **Resolved** | `internal/reship/poller.go` logs a warn with org/cluster when `RetryPending` is skipped |
| 3 | `ConvertCents` rate=0 | Medium | **Resolved** | `internal/costdata/conversion.go` returns cents unchanged when `rate <= 0` |
| 4 | Koku error body unbounded | Low | **Resolved** | `provider.go` reads error bodies with `io.LimitReader(..., 1<<20)` |
| 5 | `SQLOrderByFragment` interpolation | Low | **Still Accepted** | Unchanged (allowlisted fragments) |
| 6 | `CPUSavingsMicroCents` overflow | Low | **Still Accepted** | Unchanged (physically implausible product; documented) |
| 7 | Missing `ctx.Err()` in CSV loops | Low | **Resolved** | `librobne/csv` `ForEach*` checks `ctx.Err()` every 10_000 accepted rows; ingest uses those helpers |
| 8 | `log.Printf` in costdata | Low | **Resolved** | Structured `.Warn("exchange rate unavailable, defaulting to 1.0")` |
| 9 | Migration 000179 down | Informational | **Still Accepted** | Unchanged (`stranded_resource`) |
| 10 | Quota headroom truncation | Informational | **Accepted (documented)** | `librobne/quota/recommend.go` `applyHeadroom` documents integer truncation; production quotas are large |
| 11 | `FormatCentsToAmount` MinInt64 | Informational | **Accepted (documented)** | `internal/money/format.go` godoc: MinInt64 negation overflow; savings stay well below MaxInt64 |

v11 items that v12 carried (re-verified 2026-08-31):

| v11 | Title | Status | Verification |
|-----|-------|--------|--------------|
| 1 | Bitmap codes >63 | **Resolved** ([#509](https://github.com/pgarciaq/ros-ocp-backend/issues/509)) | `MergeNotificationCodes` is slice-based; `NotificationCodeBitmap` removed |
| — | `ResolveGPUThresholdSettings` nil func | **Resolved** | `internal/engine/gpu/settings.go` `init` installs defaults if nil |
| — | `ComputeRecommendedReplicas` overflow | **Accepted (documented)** | Comment in `librobne/container/replica_optimization.go`: physically implausible |
| — | GPU MIG `groupCol` interpolation | **Resolved** | `internal/engine/gpu/mig_list.go` uses `pgx.Identifier{}.Sanitize()` |
| — | `compat.go` ~400 lines | **Open (chore)** [#513](https://github.com/pgarciaq/ros-ocp-backend/issues/513) | Architecture leftover; not a defect |
| — | `WriteRecommendationHistory` missing `ctx.Err()` | **Resolved** | `internal/engine/container/history.go` checks `ctx.Err()` |
| — | `EvaluateNotificationsWithThresholds` mutates input | **Resolved** | Builds a fresh `[]int16`; does not mutate `rec.NotificationCodes` |

**Summary:** 6 Resolved (v12-1/2/3/4/7/8), 3 still Accepted as before (v12-5/6/9), 2 Accepted with in-code docs (v12-10/11). **Zero actionable v12 leftovers.** v11 bitmap merge is **resolved** ([#509](https://github.com/pgarciaq/ros-ocp-backend/issues/509)).

## Findings Status Summary

| # | Title | Severity | Dimension | Tracker | Status |
|---|-------|----------|-----------|---------|--------|
| 1 | Container/namespace/PVC ingest discards CSV skip counts | Medium | Auditability | [#504](https://github.com/pgarciaq/ros-ocp-backend/issues/504) | **Resolved** (closed) |
| 2 | CLI `pgdigest` LWW vs processor GREATEST/LEAST on PVC and quota | Medium | Correctness | [#505](https://github.com/pgarciaq/ros-ocp-backend/issues/505) | **Resolved** (closed) |
| 3 | Directory `Load` silently skips missing-column ROS files; tar fails | Medium | Correctness | [#506](https://github.com/pgarciaq/ros-ocp-backend/issues/506) | **Resolved** (closed) |
| 4 | PVC unique key omits `org_id`; CLI upsert rewrites tenant | Low | Security | [#508](https://github.com/pgarciaq/ros-ocp-backend/issues/508) | **Resolved** (closed; unique-key residual [#511](https://github.com/pgarciaq/ros-ocp-backend/issues/511); GPU [#512](https://github.com/pgarciaq/ros-ocp-backend/issues/512)) |
| 5 | Overnight BH uses wall-clock minutes (DST gap/overlap) | Low | Correctness | [#507](https://github.com/pgarciaq/ros-ocp-backend/issues/507) | **Resolved** (closed; wall clock documented + DST tests) |
| 6 | CLI `Load` has no tar member / row / gzip size bound | Low | Security | No ticket (accepted: CLI trust model) | **Accepted** |
| 7 | Dual `librobne` trees (`./librobne` vs vendor) without CI drift check | Low | Maintainability | [#510](https://github.com/pgarciaq/ros-ocp-backend/issues/510) | **Resolved** (closed) |
| 8 | GPU container BH uses namespace window; timeslicing BH uses cluster | Informational | Design | No ticket (docs when Peak hours docs are next edited) | **Accepted** (docs-when-convenient) |
| 9 | `MergeNotificationCodes` still drops codes >63 | Informational | Correctness | [#509](https://github.com/pgarciaq/ros-ocp-backend/issues/509) | **Resolved** (closed; bitmap removed) |
| 10 | CLI `ParseRows` ignores caller cancellation | Informational | Operational | No ticket (CLI nicety) | **Accepted** |

## Findings Detail

### Finding 1: Container/namespace/PVC ingest discards CSV skip counts

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Dimension** | Auditability |
| **Location** | `internal/ingestion/csvparser.go:107`, `internal/ingestion/namespace.go:74`, `internal/ingestion/pvc.go:22` / `257` |
| **Description** | `#475` / `#501` routed ingest through `libcsv.ForEachRow` / `ForEachNamespace` / `ForEachPVC`, which return a `skipped` count for unparseable numbers/timestamps. Callers assign `_`. VM, VM-PVC, VM-GPU, snapshot, and cluster-quota wrappers call `metrics.IncCSVRowsSkipped` and `Warnf` when `skipped > 0`. Container/namespace/PVC — the hottest files — do not. Invalid rows are still dropped inside the parser (same as before the extract). |
| **Risk** | A truncated or mixed-type ROS container CSV can zero-fill percentiles with a successful ingest and no Prometheus series. Operators cannot tell “thin day” from “bad rows.” |
| **Recommendation** | Match `forEachVMCSVRow`: keep `skipped`, `IncCSVRowsSkipped("container"\|"namespace"\|"pvc", skipped)`, warn at Info/Warn. Do not fail the payload solely because some rows skipped (that remains a product choice). |
| **Effort** | S |
| **Tracker** | [#504](https://github.com/pgarciaq/ros-ocp-backend/issues/504) |
| **Status** | **Resolved** (closed). Ingest wrappers increment `rosocp_csv_rows_skipped_total` and warn. |

### Finding 2: CLI `pgdigest` LWW vs processor GREATEST/LEAST on PVC and quota

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Dimension** | Correctness |
| **Location** | `librobne/pgdigest/write_pvc.go:75-87`, `librobne/pgdigest/write_quota.go:61-62`, `internal/ingestion/pvc.go:231-237`, `internal/ingestion/namespace_quota.go:115-128`, `internal/ingestion/cluster_quota.go:167-181` |
| **Description** | Processor PVC/quota upserts merge with `GREATEST`/`LEAST` and accumulate `sample_count`. CLI `pgdigest` documents last-write-wins and overwrites those columns from the current CSV day. Container CLI writes are also LWW (`write.go:158-196`); GPU ingest is already LWW, so GPU is consistent. PVC and quota are not. |
| **Risk** | `robne recommend --output postgres` against the ROS database used by the listener replaces a merged PVC/quota day with a single-file snapshot. Later hours can disappear. The CLI spec says a CLI-owned database; DSN is just a URL. |
| **Recommendation** | Either (a) refuse `--output` unless a config flag `cli_owned_database: true` is set, or (b) use the same GREATEST/LEAST SQL as ingest for PVC/quota (and document container LWW as the remaining exception), or (c) keep LWW but log a loud warning when the DSN host is not localhost. Prefer (a) or (b). |
| **Effort** | M |
| **Tracker** | [#505](https://github.com/pgarciaq/ros-ocp-backend/issues/505) |
| **Status** | **Resolved** (closed). CLI PVC/quota writers copy ingest `GREATEST`/`LEAST`. Container/namespace/node/GPU/VM stay LWW. |

### Finding 3: Directory `Load` silently skips missing-column ROS files; tar fails

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Dimension** | Correctness |
| **Location** | `librobne/csv/load.go:112-150` (dir) vs `load.go:284-327` (tar) |
| **Description** | `loadDir` `continue`s on `MissingROSColumnsError`, missing namespace/storage/cluster-quota/snapshot/sidecar columns. `loadTarGz` returns the error when `kind` is container/namespace/storage/cluster-quota/snapshot. VM missing columns fail in both. A directory of mixed CSVs can omit a broken `ocp_ros_usage.csv` and still `finishLoad` successfully from namespace/PVC files. |
| **Risk** | Lab `robne recommend --input ./outdir` reports namespace/PVC recs and no containers, with a zero exit, after a header-typo in the container file. The same files in a `.tar.gz` fail. Operators will not learn the same lesson twice. |
| **Recommendation** | Make directory and tar share one error policy: fail if a **classified** ROS kind is missing required columns; skip only `KindUnknown` / cost-only. Add a test: dir containing a container-named CSV without ROS headers must error. |
| **Effort** | S |
| **Tracker** | [#506](https://github.com/pgarciaq/ros-ocp-backend/issues/506) |
| **Status** | **Resolved** (closed). Directory `Load` fails on classified primary ROS CSVs missing required columns, matching tar. |

### Finding 4: PVC unique key omits `org_id`; CLI upsert rewrites tenant

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Security |
| **Location** | `migrations/000047_create_pvc_tables.up.sql:21-22`, `librobne/pgdigest/write_pvc.go:75-78` |
| **Description** | `ux_daily_pvc_digests_key` is `(cluster_uuid, namespace, persistentvolumeclaim, bucket_date)`. CLI `ON CONFLICT` sets `org_id = EXCLUDED.org_id`. GPU `gpu_container_digests` has **no** `org_id` column (natural key is cluster-scoped; documented in `write_gpu.go:28`). OpenShift cluster IDs are UUIDs, so SaaS collision is unlikely. CLI `--output` with a wrong `org_id` and a real `cluster_uuid` rewrites the PVC row’s tenant. |
| **Risk** | Misconfigured CLI against a shared DB. Not a remote unauthenticated exploit. GPU path cannot even stamp org. |
| **Recommendation** | Do not “fix” GPU uniqueness in this branch (schema change). For PVC: stop updating `org_id` on conflict (keep existing tenant) or add `org_id` to the unique index in a later migration. Document CLI `org_id` as “must match existing rows.” |
| **Effort** | S (doc / stop rewriting org_id) / L (add org_id to GPU table) |
| **Tracker** | [#508](https://github.com/pgarciaq/ros-ocp-backend/issues/508) (S path only: do not rewrite `org_id`. GPU schema stays accepted.) |
| **Status** | **Resolved** (closed). Conflict SQL does not rewrite `org_id` (shipped with [#505](https://github.com/pgarciaq/ros-ocp-backend/issues/505)); docs require YAML `org_id` to match existing PVC rows. Unique key still omits `org_id` — follow-on [#511](https://github.com/pgarciaq/ros-ocp-backend/issues/511). GPU schema — follow-on [#512](https://github.com/pgarciaq/ros-ocp-backend/issues/512). |

### Finding 5: Overnight BH uses wall-clock minutes (DST gap/overlap)

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Correctness |
| **Location** | `librobne/bhschedule/schedule.go:118-133` |
| **Description** | `#488` allows `start > end` (e.g. 22:00–06:00). `InBusinessHours` compares `Hour()*60+Minute()` in the IANA zone. Spring-forward can skip 02:00; fall-back repeats an hour. Post-midnight attribution to `previousWeekday` is tested for non-DST instants (`schedule_test.go`). |
| **Risk** | One duplicated or missing hour per DST transition on overnight schedules. Recs move slightly; not empty-digest. |
| **Recommendation** | Add two DST test cases (America/New_York spring-forward and fall-back on a Friday overnight window). Document “wall clock, not elapsed duration” next to the PUT overnight warning. Do not invent a second clock. |
| **Effort** | S |
| **Tracker** | [#507](https://github.com/pgarciaq/ros-ocp-backend/issues/507) |
| **Status** | **Resolved** (closed). DST tests (America/New_York, Europe/Madrid overnight). Docs/PUT warning: wall clock, not elapsed duration. |

### Finding 6: CLI `Load` has no tar member / row / gzip size bound

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Security |
| **Location** | `librobne/csv/load.go:247-336`, `parse.go:217-218` |
| **Description** | `loadTarGz` reads each regular-file CSV member through `parseCSVReader` with no `hdr.Size` cap and no max row count. `ParseRows` appends unbounded. Processor ingest streams and is bounded by Kafka/payload policy. CLI is a local tool (`//nolint:gosec G304`). |
| **Risk** | A hostile `.tar.gz` passed to `robne recommend` can OOM the workstation. Not an API issue. |
| **Recommendation** | Reject members with `hdr.Size > N` (e.g. 512 MiB) and/or wrap the gzip reader with a byte cap. Fail closed. |
| **Effort** | S |
| **Tracker** | None. Accepted: CLI is a trusted local binary; gzip bombs are workstation DoS. |
| **Status** | **Accepted** (no ticket). |

### Finding 7: Dual `librobne` trees without CI drift check

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Maintainability |
| **Location** | `librobne/` vs `vendor/github.com/redhatinsights/ros-ocp-backend/librobne/`, `Dockerfile` (`COPY . .` then `go build`), `go.mod` `replace => ./librobne` |
| **Description** | Container builds with a `vendor/` tree present use vendored modules. Production `.go` files currently match (tests/`go.mod` live only under `./librobne`). There is no workflow that fails on `go mod vendor` drift. `#499` already copies comments into vendor by hand. |
| **Risk** | Next librobne fix ships in `./librobne` (local tests pass) and the image runs yesterday’s vendor copy. |
| **Recommendation** | CI: `go mod vendor && git diff --exit-code vendor/github.com/redhatinsights/ros-ocp-backend/librobne`. Or build with `-mod=mod` and stop vendoring librobne. |
| **Effort** | S |
| **Tracker** | [#510](https://github.com/pgarciaq/ros-ocp-backend/issues/510) |
| **Status** | **Resolved** (closed). CI `make vendor-librobne-check` fails on vendored librobne drift. |

### Finding 8: GPU container BH uses namespace window; timeslicing BH uses cluster

| Field | Value |
|-------|-------|
| **Severity** | Informational |
| **Dimension** | Design |
| **Location** | `internal/engine/recommend_gpu_business_hours.go:40`, `internal/engine/recommend_gpu_timeslicing_business_hours.go:140-146` |
| **Description** | Container GPU Peak hours resolve `cache.Resolve(project)` (namespace override). Timeslicing Peak hours compare each container’s namespace window to the **cluster** schedule and omit the nest when they differ (code 81). Notification copy already says office window vs cluster window. |
| **Risk** | Support confusion: GPU nest present, timeslicing nest absent, same node. Not a silent wrong number if the omit path is taken. |
| **Recommendation** | One sentence in Peak hours docs: namespace GPU BH vs cluster timeslicing BH. No code change unless product wants timeslicing to follow namespace too. |
| **Effort** | S |
| **Tracker** | None. Add one sentence when Peak hours docs are next edited. |
| **Status** | **Accepted** (docs-when-convenient). |

### Finding 9: `MergeNotificationCodes` still drops codes >63

| Field | Value |
|-------|-------|
| **Severity** | Informational |
| **Dimension** | Correctness |
| **Location** | `librobne/types/notifications_merge.go` (was `notifications_bitmap.go`) |
| **Description** | v11 finding 1. Codes 79–82 are stored via `AppendUnique` / fixed slices (`cmd/robne/output.go`, engine attach helpers). `MergeNotificationCodes` still routes through the uint64 bitmap and would drop 79–82. Grep shows no production callers of `MergeNotificationCodes` outside the helper itself. |
| **Risk** | Future persist/merge using this helper strips Peak-hours warnings. |
| **Recommendation** | Implement merge via `AppendUnique` / a set, or document `MergeNotificationCodes` as “codes 1–63 only.” Add a test that merging `{79}` does not vanish. |
| **Effort** | S |
| **Tracker** | [#509](https://github.com/pgarciaq/ros-ocp-backend/issues/509) |
| **Status** | **Resolved** (closed). Merge is `AppendUnique` + sort; `NotificationCodeBitmap` deleted. Tests pin 64 / 70 / 79. |

### Finding 10: CLI `ParseRows` ignores caller cancellation

| Field | Value |
|-------|-------|
| **Severity** | Informational |
| **Dimension** | Operational robustness |
| **Location** | `librobne/csv/parse.go:261-266` |
| **Description** | `ParseRows` calls `ForEachRow(context.Background(), ...)`. CLI `Load` therefore cannot stop mid-file on SIGINT until the parse returns. Processor ingest passes a real `ctx`. |
| **Risk** | Annoying CLI hang on huge files. Not production API. |
| **Recommendation** | Thread `cmd` context into `Load`/`ParseRows` when convenient. |
| **Effort** | S |
| **Tracker** | None. CLI nicety; processor ingest already passes `ctx`. |
| **Status** | **Accepted** (no ticket). |

## Priority Remediation Order

All ticketed items are **closed** on `pgarciaq-rosocp-superpowers-phase17`:

1. ~~[#504](https://github.com/pgarciaq/ros-ocp-backend/issues/504) Finding 1~~ — skip metrics on container/namespace/PVC ingest.
2. ~~[#506](https://github.com/pgarciaq/ros-ocp-backend/issues/506) Finding 3~~ — one `Load` error policy for dir vs tar.
3. ~~[#505](https://github.com/pgarciaq/ros-ocp-backend/issues/505) Finding 2~~ — CLI PVC/quota GREATEST/LEAST.
4. ~~[#510](https://github.com/pgarciaq/ros-ocp-backend/issues/510) Finding 7~~ — vendor drift CI.
5. ~~[#507](https://github.com/pgarciaq/ros-ocp-backend/issues/507) Finding 5~~ — DST tests + wall-clock docs.
6. ~~[#508](https://github.com/pgarciaq/ros-ocp-backend/issues/508) / [#509](https://github.com/pgarciaq/ros-ocp-backend/issues/509)~~ — PVC `org_id` on conflict; bitmap merge.

Remaining in this document (no tickets): findings 6, 8, 10 — accepted / docs-when-convenient.

## Accepted Risks

| Item | Rationale |
|------|-----------|
| PVC unique key without `org_id` | Conflict no longer rewrites `org_id` ([#508](https://github.com/pgarciaq/ros-ocp-backend/issues/508)). Adding `org_id` to the unique index is [#511](https://github.com/pgarciaq/ros-ocp-backend/issues/511) (P3 / postponed). |
| GPU digest unique key without `org_id` | Pre-librobne schema. Cluster UUID is the isolation key. Schema epic: [#512](https://github.com/pgarciaq/ros-ocp-backend/issues/512) (P3 / postponed). |
| CLI is a trusted local binary | `--input` path and Postgres DSN are operator-controlled. Gzip bombs are workstation DoS, not multi-tenant. |
| `#502` / `#503` GPU row cardinality | Out of this review. NISE still one row per container; operator DCGM join still orphaned. Digest `gpu_count` from distinct UUIDs remains incomplete until those land. |
| `#466` koku `./` tar members | Closed as producer contract (flat names). CLI already `stripDotSlash`. |

## Strengths (this delta)

- Identity for `pgdigest` writes comes from YAML/`EnsureAccountCluster`, not CSV `cluster_id` cells.
- Tar classify uses `stripDotSlash` + `filepath.Base`; members are not written to the filesystem.
- Overnight windows have unit tests (previous-weekday attribution) and a Settings PUT warning string.
- BH codes 79–82 have catalog tests that prevent cross-plugin leakage (79 not on VM, etc.).
- Namespace unfiltered list strips `business_hours` (#497); GPU list copies clear nested BH (code 80).
- `ForEach*` `ctx.Err()` cadence closes v12-7 without weakening parse.
- v12-1 `vmTerm` is a bind parameter.

## Current State

| | Count |
|--|--|
| New findings | 10 (0 Critical, 0 High, 3 Medium, 4 Low, 3 Informational) |
| Ticketed | 7 ([#504](https://github.com/pgarciaq/ros-ocp-backend/issues/504)–[#510](https://github.com/pgarciaq/ros-ocp-backend/issues/510)) — **all closed** |
| No ticket | 3 (findings 6, 8, 10) — still accepted / docs-when-convenient |
| v12 resolved (re-verified) | 6 (v12-1/2/3/4/7/8); first draft of this report only credited 1 and 7 |
| v12 still open | 0 actionable; 10/11 documented in code; 5/6/9 still Accepted |
| v11 bitmap | **Resolved** ([#509](https://github.com/pgarciaq/ros-ocp-backend/issues/509)) |
| Accepted this pass | CLI gzip trust model, Peak hours window mismatch (docs), CLI ctx, #502/#503, #466 |
| Follow-on (postponed) | [#511](https://github.com/pgarciaq/ros-ocp-backend/issues/511) PVC unique key + `org_id`; [#512](https://github.com/pgarciaq/ros-ocp-backend/issues/512) GPU digest `org_id`; [#513](https://github.com/pgarciaq/ros-ocp-backend/issues/513) `compat.go` chore |

v13 remediations landed on `pgarciaq-rosocp-superpowers-phase17`. Finding text above is the original problem statement; **Status** rows record the close-out.
