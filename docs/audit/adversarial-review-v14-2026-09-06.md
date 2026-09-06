# Adversarial Due Diligence Review — ros-ocp-backend

## Version & Date
Version: 14 | Date: 2026-09-06 | Reviewer: AI-assisted (incremental, Sep 1–6 delta)

## Scope

Incremental review of `pgarciaq-rosocp-superpowers-phase17` since
[v13](adversarial-review-v13-2026-08-31.md) (2026-08-31). This is not a
whole-repo re-audit. some 50 commits landed in window, all of them hardening,
remediation, or governance:

- Tenant isolation: #445 slices (clusters.org_id), #512 (GPU digest org_id),
  #525 (machineset/MIG alias joins), #523 (quality pgx + alias joins)
- Fail-closed and observability: #532 (RBAC pagination), #534 (reship errors),
  #538 (malformed-JSON metric), #531 (history pagination), #540 (savings 503s)
- Supply chain and CI: #535 (mapstructure v2, builder 1.26), #524 (CGO guard),
  vendor-librobne-check usage
- Governance: AGENTS.md hub (#537) + verification discipline, #529 docs/drift
  guard, #468 seam matrix, #536/#541 test hygiene

Prior reviews: v13 (2026-08-31). Historical record is unchanged; this document appends.

## Executive Summary

The September delta is the cleanest this reviewer has seen on this branch:
every change inspected is a hardening, a remediation with mutation-checked
tests, or governance. There are **no Critical, no High, and no Medium**
findings. The 26 new findings below are all Low (defense-in-depth with narrow
preconditions) or Informational (hygiene, alert shape, message accuracy).

Two findings deserve attention disproportionate to their severity. First,
v12-1 (`vmTerm` $N binds, resolved) **regressed through the fix itself**: the
shared-helper surplus arg broke two savings endpoints (#540, now fixed) — the
first observed case of a remediation introducing its own bug class on this
repo, and the reason V14-19 exists. Second, V14-08 shows the alias-join
problem (#525) is only two-thirds done: 15 GORM sites still lack `c.org_id`,
deferred by reference (#523/#375) but never ticketed as such.

## Scorecard

| Dimension | Rating | Key gap |
|-----------|--------|---------|
| Security | ★★★★★ | No Critical/High; residual alias-display (V14-08) and orchestrated-write edge cases, all Low |
| Correctness | ★★★★★ | #540 root-caused and fixed; remaining items are edge inputs (V14-14/15) and fail-open-tomorrow coupling (V14-17) |
| Auditability | ★★★★☆ | Three new unlabeled/bounded counters landed correctly; `reason="truncated"` pollutes the errors metric (V14-17) |
| Operational robustness | ★★★★☆ | No image build in CI (V14-19); theoretical libc skew pending a deploy check (V14-20) |
| Performance | ★★★★★ | Quality scans verified field-by-field; no new N+1/unbounded queries found |
| Design quality | ★★★★☆ | Hook-func-var and site-label allowlist unenforced (V14-12); compat.go still open (#513) |
| Maintainability | ★★★★☆ | Dead config defaults accumulating (V14-07); migration lint unenforced (V14-18) |
| Governance | ★★★★☆ | AGENTS.md trigger line overgeneralizes (V14-25); CHANGELOG coverage otherwise complete |

## Prior Findings Status (v13 → v14)

Re-verified deltas only; v13's resolved/accepted table otherwise stands.

| v13 # | Title | v13 status | v14 status |
|-------|-------|-----------|-----------|
| 4 | PVC unique key omits `org_id` | Resolved (#508), residual #511 | Unchanged (#511 still open/postponed) |
| 6/8/10 | CLI gzip bounds, BH window docs, CLI ctx | Accepted, no ticket | Unchanged; finding 8's docs trigger fired (#527 edited BH docs; namespace-vs-cluster window now documented) |
| 7 | Vendor drift CI | Resolved (#510) | Unchanged; check used repeatedly this window |
| 9 | MergeNotificationCodes | Resolved (#509) | Unchanged |
| — | GPU digest `org_id` (#512) | Postponed in v13 | **Implemented** (000186–000190, reads/prune/housekeeper); residual colliding-UUID backfill attribution → V14-09 |
| — | `compat.go` (#513) | Open chore | Unchanged |
| v12-1 | `vmTerm` $N binds | Resolved | **Regressed via shared-helper surplus arg, re-resolved** ([#540](https://github.com/pgarciaq/ros-ocp-backend/issues/540)); position coupling remains → V14-17 |

## Findings Status Summary

| # | Title | Severity | Dimension | Tracker | Status |
|---|-------|----------|-----------|---------|--------|
| V14-01 | RBAC 429/408 misclassified as 403 denial | Low | Security | [#546](https://github.com/pgarciaq/ros-ocp-backend/issues/546) | Open |
| V14-02 | Truncated RBAC partials served AND cached for full TTL | Low | Security | [#543](https://github.com/pgarciaq/ros-ocp-backend/issues/543) | Open |
| V14-03 | RBAC cache returns mutable reference | Informational | Security | [#545](https://github.com/pgarciaq/ros-ocp-backend/issues/545) | Open |
| V14-04 | Rate limiter runs after RBAC | Informational | Security | [#547](https://github.com/pgarciaq/ros-ocp-backend/issues/547) | Open |
| V14-05 | CA file predictable path, symlink truncate, never cleaned | Low | Security | [#544](https://github.com/pgarciaq/ros-ocp-backend/issues/544) | Open |
| V14-06 | DSN keyword injection via unquoted Sprintf | Informational | Security | [#549](https://github.com/pgarciaq/ros-ocp-backend/issues/549) | Open |
| V14-07 | Dead/duplicate config defaults left behind | Informational | Maintainability | [#550](https://github.com/pgarciaq/ros-ocp-backend/issues/550) | Open |
| V14-08 | 15 remaining alias-only `JOIN clusters` without `c.org_id` | Low | Security | [#552](https://github.com/pgarciaq/ros-ocp-backend/issues/552) (#375-adjacent) | Open |
| V14-09 | `CreateCluster` reintroduces #508 pattern (`org_id` on conflict) | Low | Security | [#551](https://github.com/pgarciaq/ros-ocp-backend/issues/551) | Open |
| V14-10 | GPU backfill stamps colliding UUIDs to arbitrary org | Low | Security | [#548](https://github.com/pgarciaq/ros-ocp-backend/issues/548) | Open |
| V14-11 | Analytics/hook flags silently no-op pre-backfill | Informational | Correctness | [#554](https://github.com/pgarciaq/ros-ocp-backend/issues/554) | Open |
| V14-12 | Malformed-JSON hook unlocked func-var + unvalidated site | Informational | Design | [#553](https://github.com/pgarciaq/ros-ocp-backend/issues/553) | Open |
| V14-13 | Force-empty can flip downstream classification | Informational | Correctness | [#556](https://github.com/pgarciaq/ros-ocp-backend/issues/556) | Open |
| V14-14 | Zero-spellings bypass ParsePagination caller default | Low | Correctness | [#555](https://github.com/pgarciaq/ros-ocp-backend/issues/555) | Open |
| V14-15 | Offset int64-overflow serves page 1 as 200 | Low | Correctness | [#557](https://github.com/pgarciaq/ros-ocp-backend/issues/557) | Open |
| V14-16 | Negative Limit panics quality scans (vs GORM SQL error) | Informational | Correctness | [#561](https://github.com/pgarciaq/ros-ocp-backend/issues/561) | Open |
| V14-17 | Served-traffic truncation counted in errors metric | Informational | Auditability | [#542](https://github.com/pgarciaq/ros-ocp-backend/issues/542) adjacent; [#559](https://github.com/pgarciaq/ros-ocp-backend/issues/559) | Open |
| V14-18 | Migration lint unenforced, misdocumented, UNIQUE-blind | Low | Maintainability | [#560](https://github.com/pgarciaq/ros-ocp-backend/issues/560) | Open |
| V14-19 | CI never builds the image (CGO guard is text-only) | Low | Operational | [#558](https://github.com/pgarciaq/ros-ocp-backend/issues/558) | Open |
| V14-20 | ubi10-builder/ubi9-runtime libc skew now load-bearing | Low | Operational | [#562](https://github.com/pgarciaq/ros-ocp-backend/issues/562) (verify on deploy) | Open |
| V14-21 | #540 trim is position-coupled to helper append order | Low | Correctness | [#565](https://github.com/pgarciaq/ros-ocp-backend/issues/565) | Open |
| V14-22 | #536 test-hygiene overreach (tautological parse test; 406 premise) | Low | Maintainability | [#564](https://github.com/pgarciaq/ros-ocp-backend/issues/564) | Open |
| V14-23 | Quality scans lack no-DB column-count pin | Informational | Maintainability | [#567](https://github.com/pgarciaq/ros-ocp-backend/issues/567) | Open |
| V14-24 | #535 message overclaims; #533 hygiene unlogged | Informational | Governance | [#566](https://github.com/pgarciaq/ros-ocp-backend/issues/566) (process note) | Open |
| V14-25 | AGENTS.md trigger convention overgeneralizes | Low | Governance | [#563](https://github.com/pgarciaq/ros-ocp-backend/issues/563) | Open |
| V14-26 | 000187/000188 backfill misattributes colliding UUIDs, no repair | Low | Security | merged into V14-10 → [#548](https://github.com/pgarciaq/ros-ocp-backend/issues/548) | Open |

Note: V14-10 and V14-26 describe the same backfill from opposite ends
(migration behavior vs product impact) and are kept as one remediation item.

## Findings Detail

### V14-01: RBAC 429 (and 408) misclassified as 403 denial

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Security |
| **Location** | `internal/api/middleware/rbac.go:163-168` |
| **Description** | Any `400–499` from RBAC returns `(nil, nil)`, which `Rbac()` maps to 403. Rate-limit (`429`) and timeout (`408`) responses are therefore reported as "not authorized." Fail-closed, so safe — but the wrong signal. |
| **Risk** | Operators chase "bad identity" during an upstream capacity event while retry logic keyed on 503/429 never fires. |
| **Recommendation** | Deny only on authoritative codes (`401/403`, arguably `404`); map `408/425/429` and other 4xx to error → 503. |
| **Effort** | S |

### V14-02: Truncated RBAC partials are served AND cached for full TTL

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Security |
| **Location** | `internal/api/middleware/rbac.go:190-192`, `117-119`; `rbac_cache.go:78-89` |
| **Description** | On `maxRBACPages` exhaustion the collected (partial) ACL set is served and then stored via `storeCachedRBACPermissions` for the full TTL (60s default). Later requests hit cache: no retry, no new `truncated` increment. |
| **Risk** | An org past 50×100 ACLs (or a lowered RBAC `limit`) loses legitimate clusters for a minute per incident, with a single warn in logs. Direction is deny-legit-access (partial ⊆ full union), never grant. |
| **Recommendation** | Do not cache truncated results (pass a `truncated bool` out of `request_user_access` and skip the store), or cache with 5–10s TTL. |
| **Effort** | S |

### V14-03: RBAC cache returns a mutable reference

| Field | Value |
|-------|-------|
| **Severity** | Informational |
| **Dimension** | Security |
| **Location** | `internal/api/middleware/rbac_cache.go:69-76` (read) vs `:82-85` (copy-on-store) |
| **Description** | Reads return the LRU's map/slices directly; only stores copy. No current mutator found (all `userPerms` uses are reads). |
| **Risk** | A future handler `append` could corrupt the shared entry across identities sharing a cache key — privilege narrowing or widening. |
| **Recommendation** | Deep-copy on read, mirroring the store path. |
| **Effort** | S |

### V14-04: Rate limiter runs after RBAC

| Field | Value |
|-------|-------|
| **Severity** | Informational |
| **Dimension** | Security |
| **Location** | `internal/api/server.go:207-212`; `internal/api/middleware/rate_limiter.go:52-58` |
| **Description** | Ordering predates the delta, but #532's fail-closed 503 makes the cost concrete: spam with valid-shaped identities pays a full RBAC round-trip (up to 30s timeout) before the per-org bucket is consulted. |
| **Risk** | RBAC becomes the DDoS sink the limiter was meant to shield. No demonstrated abuse. |
| **Recommendation** | Move `NewRateLimiter` above `Rbac` (keep after `Identity`); verify 429-before-503 in tests. |
| **Effort** | S |

### V14-05: CA file predictable path, symlink-following truncate, never cleaned

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Security |
| **Location** | `internal/db/db.go:239-264` |
| **Description** | Deterministic `/tmp/rosocp-rds-ca-<sha16>.pem` opened `O_RDWR\|O_CREATE\|O_TRUNC` without `O_EXCL`/`O_NOFOLLOW`; no removal on the success path; 0600 enforced post-hoc. |
| **Risk** | Needs local `/tmp` write (pod exec/RCE): pre-created symlink truncates a victim file; same-cert writers race (benign, identical content); one file per distinct cert accumulates. CA bundle itself is public — corruption/DoS, not key leak. |
| **Recommendation** | `O_EXCL` create + rename-into-place, or `O_NOFOLLOW`, or a `0700` private dir; unlink on shutdown optional. |
| **Effort** | S |

### V14-06: DSN keyword injection via unquoted Sprintf values

| Field | Value |
|-------|-------|
| **Severity** | Informational |
| **Dimension** | Security |
| **Location** | `internal/db/db.go:153-154` (password correctly moved to `ConnConfig.Password`, `:165`) |
| **Description** | Remaining `user/dbhname/host/port/sslmode` values are interpolated unquoted; a space or quote (copy-paste, Helm value) injects extra `keyword=value` pairs (e.g. silently downgrading TLS). Operator/Clowder-controlled input only. |
| **Risk** | Confusing `ParseConfig` failure or silent TLS downgrade; availability, not tenant isolation. |
| **Recommendation** | Build `pgx.ConnConfig` struct directly instead of string interpolation. |
| **Effort** | S |

### V14-07: Dead/duplicate config defaults left behind

| Field | Value |
|-------|-------|
| **Severity** | Informational |
| **Dimension** | Maintainability |
| **Location** | `internal/config/config.go:738-739` (`ROS_DB_MAX_CONNS` twice); `:712-713` (`DISABLE_NAMESPACE_RECOMMENDATION`, `ROS_USE_NATIVE_ENGINE` with zero non-config readers) |
| **Description** | No behavior change today, pure maintenance trap: the next editor changes the first `SetDefault` and nothing moves, or wires a reader to a dead key assuming it does something. |
| **Risk** | Misconfiguration by confusion; negligible runtime impact. |
| **Recommendation** | Delete line 738 (keep the named-constant one) and the dead defaults — or wire/document them if intentional. |
| **Effort** | S |

### V14-08: 15 remaining alias-only `JOIN clusters` without `c.org_id`

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Security |
| **Location** | `internal/model/recommendation_set_native.go:397,491,513,579,603,790`; `namespace_recommendation_set_native.go:165,191,275,318,342,370`; `native_ns_list_keys.go:266,276,323`; `recommendation_history.go:120,132` |
| **Description** | Each drives off an `org_id`-filtered base table and selects only `c.cluster_alias`, so no tenant rows leak — only the alias display string can come from a colliding UUID's foreign row. Explicitly NOT findings: `common.go:53,83` (surrogate-PK join, no collision possible); the five quality models, machinesets, MIG, savings, heatmap (all carry the predicate after #445/#523/#525); `librobne/` has zero `JOIN clusters` (verified). |
| **Risk** | Org A sees org B's alias on UUID re-registration/fixture reuse. Confusing, not a breach. |
| **Recommendation** | Append `AND c.org_id = ?` at each site with a colliding-UUID test reusing one UUID under two orgs (the #525 pattern). #375-adjacent; ticket as its follow-on or standalone. |
| **Effort** | M |

### V14-09: `CreateCluster` reintroduces the #508 pattern

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Security |
| **Location** | `internal/model/cluster.go:34-39` (`DoUpdates` includes `org_id`); same in `librobne/pgrec/cluster.go:48-53` |
| **Description** | v13/#508 deliberately stopped rewriting `org_id` on PVC conflicts; the cluster upsert (and the pgrec twin) overwrite it on `(tenant_id, source_id, cluster_uuid, cluster_alias)` conflict. Caller identity is gateway-trusted, so exploitation needs a lying caller — integrity-only. The 000191 `clusters_fill_org_id()` trigger fills only empty values and does not correct mismatches. |
| **Risk** | Replayed/stale Kafka identity repoints a cluster row to another org; `c.org_id`-scoped joins and `clustercache` follow the lie. |
| **Recommendation** | Drop `"org_id"` from `DoUpdates` (first-writer wins, like PVC) or validate against `rh_accounts.org_id` before upsert, in both files. |
| **Effort** | S |

### V14-10/26: GPU backfill stamps colliding UUIDs to an arbitrary org, no repair

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Security |
| **Location** | `migrations/000187:34-46`, `000188:20-32` (`DISTINCT ON (cluster_uuid) ORDER BY cluster_uuid, a.id`) |
| **Description** | One UUID under two tenants backfills every pre-existing GPU row to the lowest-`rh_accounts.id` org; 000188 re-runs the same attribution so the loser is never corrected. Post-PR-4 org-scoped reads then hide the loser's digests under the winner's org — invisible, not deleted. Preconditions are narrow but acknowledged real by #525's own cloned-cluster entry. |
| **Risk** | Orphaned GPU digests for cloned clusters; silent data invisibility. |
| **Recommendation** | Pre-flight guard aborting with a row dump on colliding UUIDs, or document as a known limitation with a repair `UPDATE` for post-189 rows. |
| **Effort** | S–M |

### V14-11: Analytics/hook flags silently no-op pre-backfill

| Field | Value |
|-------|-------|
| **Severity** | Informational |
| **Dimension** | Correctness |
| **Location** | `internal/engine/cluster_analytics.go:20-26`, `cluster_ingest_hooks.go:20-26` |
| **Description** | `WHERE c.org_id = $1 …` matches zero rows on pre-backfill NULLs; `Exec` returns nil error so callers never know. Migration-window only, fail-silent in the safe direction. |
| **Risk** | Rolling deploy against a pre-backfill DB under-reports degraded clusters until backfill lands. |
| **Recommendation** | Check `RowsAffected() == 0` and `Warn` with org/cluster (the #534 reship pattern). |
| **Effort** | S |

### V14-12: Malformed-JSON hook unlocked func-var + unvalidated site

| Field | Value |
|-------|-------|
| **Severity** | Informational |
| **Dimension** | Design |
| **Location** | `librobne/types/malformed.go:17,29-31`; `internal/services/metrics.go:70-74` |
| **Description** | Plain func var, lock-free hot-path read; `WithLabelValues(site)` has no allowlist. Prod writes once at startup and tests serialize, so no race observed — unenforced. All three call sites pass constants and vendor is in sync (verified). |
| **Risk** | A future `t.Parallel` test or late setter races the hot path; a future caller passing tenant data as `site` explodes cardinality (the ADR-0243 violation the constants prevent). Bound holds today by discipline only. |
| **Recommendation** | `atomic.Pointer[func(string)]` + `switch site` allowlist in the wire function dropping/logging unknown sites. |
| **Effort** | S |

### V14-13: Force-empty can flip downstream classification

| Field | Value |
|-------|-------|
| **Severity** | Informational |
| **Dimension** | Correctness |
| **Location** | `librobne/csv/parse_snapshot.go:228-238` |
| **Description** | Strictly better than the pre-#538 phantom-partial decode, but label-gated logic now sees "no labels" for corrupt-but-present sets. Only signal is the counter — no log sample, row count, or alert rule. Accepted design; flagging the trade-off. |
| **Risk** | Misclassified snapshots on corrupt label sets, discovered only via metric. |
| **Recommendation** | Keep force-empty; add a debug-level redacted log sample and a dashboard/alert note next to the metric; document "empty means unparseable OR absent." |
| **Effort** | S |

### V14-14: Zero-spellings bypass ParsePagination caller default

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Correctness |
| **Location** | `internal/api/listoptions/list_options.go:194` |
| **Description** | `raw != "" && raw != "0"` falls through to `parseLimit`, whose `i == 0` branch returns package `DefaultLimit` (100), discarding the caller's 20. `?limit=0` → 20 but `?limit=00` → 100: violates the change's own contract (CHANGELOG) on a real input shape. |
| **Risk** | Inconsistent paging (5× rows) and excess per-response cost on zero-padded clients. |
| **Recommendation** | Numeric-zero check instead of string compare (no new `TrimSpace`; main doesn't trim). Add `00`/`+0` cases to `TestParsePagination`. |
| **Effort** | S |

### V14-15: Offset int64-overflow silently serves page 1

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Correctness |
| **Location** | `list_options.go:149-161` (`parseOffset` maps every `Atoi` error to default, conflating syntax and range errors) |
| **Description** | `?offset=10001` → 400 but `?offset=9999999999999999999999` → 200 with `offset: 0`. Newly exposed on both history endpoints via #531 (old code did the same, but #531 owns the contract now). Same tenant, same data — masks client bugs, potentially looping page 1 forever. |
| **Risk** | Hidden client bugs; no disclosure. |
| **Recommendation** | Distinguish `*strconv.NumError` with `Err == strconv.ErrRange` and return the over-max error. |
| **Effort** | S |

### V14-16: Negative Limit panics quality scans vs GORM SQL error

| Field | Value |
|-------|-------|
| **Severity** | Informational |
| **Dimension** | Correctness |
| **Location** | `internal/model/quality_pgx_scan.go:18,41,64,87,109` (callers pass unchecked `opts.Limit`) |
| **Description** | `make([]T, 0, capacity)` with negative capacity panics; GORM `Find` would SQL-error instead. Unreachable via API (`ListAPIOptions` guarantees 1–1000; negatives are 400). Only direct in-process misuse. |
| **Risk** | Future caller (CSV export-all, admin backfill) passing `Limit: -1` crashes a handler goroutine instead of erroring. |
| **Recommendation** | Clamp at call sites or in scans (`if capacity < 0 { capacity = 0 }`). |
| **Effort** | S |

### V14-17: Served-traffic truncation counted in errors metric

| Field | Value |
|-------|-------|
| **Severity** | Informational |
| **Dimension** | Auditability |
| **Location** | `internal/api/middleware/rbac.go:190-192` (deliberate per #532 lock) |
| **Description** | Alert-hygiene note, not a bug: `reason="truncated"` fires on the only fail-open path of an otherwise fail-closed rewrite. |
| **Risk** | SLO/error-budget alerts on `rosocp_.*errors.*` page on-call for a known capacity cap serving traffic by design. Noted for #542 (alert owner). |
| **Recommendation** | Separate counter or document the `reason!="truncated"` alert exclusion next to the metric. |
| **Effort** | S |

### V14-18: Migration lint unenforced, misdocumented, UNIQUE-blind

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Maintainability |
| **Location** | `scripts/lint-migrations.sh:10,44-48`; `migrations/README.md:5`; no workflow references the script |
| **Description** | Born in `933ac631`, never wired into any workflow while the README claims coverage. Usage comment says "changed vs origin/main" but code finds **all** files (and fails on pre-existing `000015/000017/000179` when run that way — reproduced). Regex misses `CREATE UNIQUE INDEX`, so `000189:39` and `000193:11,16` passed silently (`000189:27` documents the blind spot instead of fixing it). |
| **Risk** | 000193's non-concurrent UNIQUE rebuilds on large namespace tables passed silently; future large-table indexes land without the K8s-job pre-step while contributors believe they're guarded. |
| **Recommendation** | Wire lint into a workflow on changed migration files; extend regex to `CREATE\s+(UNIQUE\s+)?INDEX`; correct the docstring; or downgrade the README claim. |
| **Effort** | M |

### V14-19: CI never builds the image (CGO guard is text-only)

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Operational robustness |
| **Location** | `.github/workflows/build.yml:19-54` |
| **Description** | The #524 guard (grep + negative compile + `make robne`) is sound — negative-compile premise empirically re-verified (`CGO_ENABLED=0` fails on `kafka.*` stubs). But no `docker build`: bad base tag, broken COPY, or runtime skew all pass CI. |
| **Risk** | Dockerfile bit-rot discovered only at on-prem deploy time. |
| **Recommendation** | Image-build smoke step (no push required). |
| **Effort** | S |

### V14-20: ubi10-builder/ubi9-runtime libc skew now load-bearing

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Operational robustness |
| **Location** | `Dockerfile:1` (`ubi10/go-toolset:1.26`) vs `:12` (`ubi9/ubi-minimal:latest`, floating) |
| **Description** | Pre-Sep-2 the `CGO_ENABLED=0` binary had no libc dependence; #522's CGO=1 restoration reintroduced dynamic libc linkage, activating the skew (#535 widened the builder side, runtime untouched). Theoretical until refuted by a post-Sep-2 deploy. |
| **Risk** | `GLIBC_X.XX not found` at pod startup on SNO. |
| **Recommendation** | Check built-binary symbol versions (`objdump -T`) or confirm on next on-prem rollout; align runtime to `ubi10-minimal` if refuted. No ticket unless confirmed — recheck, don't file blind. |
| **Effort** | S to check, M to rebase |

### V14-21: #540 trim is position-coupled to helper append order

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Correctness |
| **Location** | `handlers_savings_summary.go:~629`, `handlers_savings_summary_tag.go:~47` |
| **Description** | Correct today (verified `$engine/$term` positions survive), but a future appended/reordered helper arg silently rebinds `$N` to the wrong value — wrong savings numbers with no error, unlike the current fail-loud surplus behavior. |
| **Risk** | Silent savings miscomputation after an innocent helper extension. |
| **Recommendation** | Build args without vmTerm for these callers, or assert the dropped value equals `savingsSummaryVMTerm(termProfile)`. |
| **Effort** | S |

### V14-22: #536 test-hygiene claims overreach in two spots

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Maintainability |
| **Location** | `openapi_handler_test.go:158-170`; `handlers_namespace_format_test.go:19-21` |
| **Description** | (i) The parse test exercises a 5-line `json.Unmarshal` wrapper, not the `sync.Once` cached loader — a corrupt `openapi.json` at boot takes an untested path (tautological). (ii) The 406 test mounts the legacy handler directly, but the production plural path serves CSV 200 via `serveNativeNamespaceList` when native rows exist — locking fallback-only behavior under a false comment. |
| **Risk** | False confidence; future CSV work breaks a test whose premise is wrong. |
| **Recommendation** | Drive the 406 test through `WithFallback` (empty→406, seeded→200 CSV); test the loader with an injectable reader. |
| **Effort** | S |

### V14-23: Quality scans lack a no-DB column-count pin

| Field | Value |
|-------|-------|
| **Severity** | Informational |
| **Dimension** | Maintainability |
| **Location** | New `internal/model/quality_pgx_scan.go` ("column order must match the SELECT") |
| **Description** | The integration smoke test pins behavior but is Docker-gated (skipped under `-short`). No static guard like v13's ARV-13 remediation for detail selects. |
| **Risk** | SELECT/scan order drift surfaces only in the ~30-min integration suite. |
| **Recommendation** | Token-count parity tests per scan function. |
| **Effort** | S |

### V14-24: #535 message overclaims; #533 hygiene unlogged

| Field | Value |
|-------|-------|
| **Severity** | Informational |
| **Dimension** | Governance |
| **Location** | `8ceae653 --stat` |
| **Description** | Commit message says "pin logrus" but touches no logrus files (decision recorded in-issue only); #533's dead-default removal is a true no-op. Process accuracy, not behavior. |
| **Recommendation** | Convention note only: keep dependency-swap messages to what the diff contains. No code action. |
| **Effort** | S (process) |

### V14-25: AGENTS.md trigger convention overgeneralizes

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Governance |
| **Location** | `AGENTS.md:57` (verify current line; written as the `main` + `pgarciaq-rosocp-superpowers-*` example) |
| **Description** | `codeql.yml`, `govulncheck.yml`, `openapi-changelog-check.yml`, `adr-reminder.yml` are `main`-only; only build/docs/vendor-drift follow the phase pattern. A phase-branch PR skips CodeQL, govulncheck, and the OpenAPI advisory check. An agent reading the hub assumes coverage that doesn't exist. |
| **Risk** | Misplaced confidence in CI gates on phase branches. |
| **Recommendation** | Scope the example to build/docs/vendor-drift pushes, or list the main-only exceptions. |
| **Effort** | S |

### V14-26: (merged into V14-10; kept as number for traceability)

See V14-10. The migration-side and product-side descriptions were one remediation item; a single tracker should own it.

## Priority Remediation Order

**Short-term (Low severity, concrete value, all S except noted):**
1. V14-18 migration lint wiring + UNIQUE regex (M) — process guard with teeth; the 000193 gap already slipped through once
2. V14-08 remaining alias joins (M) — completes the #525 pattern; #375-adjacent
3. V14-09 cluster `org_id` overwrite (S) — contradicts the #508 direction in both `model` and `pgrec`
4. V14-02 truncated-partial caching (S) — skip store on truncation
5. V14-01 4xx refinement to 401/403 (+404) (S)
6. V14-05 CA `O_EXCL`/private dir (S)
7. V14-19 image-build smoke step (S)
8. V14-14/V14-15 pagination edges (S each)
9. V14-21 trim assertion (S), V14-22 test fixes (S), V14-04 limiter reorder (S), V14-06 DSN struct (S)

**Backlog (Informational):** V14-03, V14-11, V14-12, V14-13, V14-16, V14-17 (fold into #542), V14-23, V14-24 (process only), V14-20 (verify on deploy, no ticket unless confirmed), V14-25 (AGENTS.md line).

## Accepted Risks

| Item | Rationale |
|------|-----------|
| v13-6/8/10, v12-5/6/9, v11 items | Unchanged from v13; finding 8's docs trigger fired and is documented |
| Truncation serve-partial | Deliberate capacity-cap exception, metered, CHANGELOG-disclosed (#532) |
| `reason="truncated"` in errors metric | Deliberate; alert exclusion belongs to #542 (V14-17) |
| Pre-existing `docs-lint` strict failures | Same 3 links as `08c8f80e`; no Sep regression |
| NISE/GPU producers (#502/#503) | Out of scope (v13-accepted, unchanged) |
| Kruize legacy path | Untouched by the delta; #65 owns removal |

## Strengths (this delta)

- Every remediation shipped mutation-checked tests (verified pattern, not claimed).
- Tenant isolation advanced on four fronts at once (clusters denormalize, GPU digests, alias joins, quality joins) with colliding-UUID tests.
- RBAC fail-closed with 403/503 semantics, generic 503 bodies, and bounded metric labels.
- No new `_ =` swallows, panics, or `log.Fatal` in production code paths (verified via diff).
- CHANGELOG discipline held across user-visible changes (verified against `git log`).
- The `parseOffset`/`ListAPIOptions` choke points make pagination validation auditable in one place.

## Current State

| | Count |
|--|--|
| New findings | 26 numbers, 25 live items (V14-26 merged into V14-10) |
| Severity split | 0 Critical, 0 High, 0 Medium, 14 Low (01, 02, 05, 08, 09, 10, 14, 15, 18, 19, 20, 21, 22, 25), 11 Informational (03, 04, 06, 07, 11, 12, 13, 16, 17, 23, 24) |
| Ticketed | All 25 live items tracked: #543–#567 (see Tracker column; V14-26 via #548). Suggested order in the Priority Remediation Order section. |
| v13 carry | All resolved stay resolved; accepted stay accepted; v12-1 amended (regressed → re-resolved) |
| Accepted this pass | v13 set, truncation exception, metric-shape note, pre-existing lint failures, #502/#503, Kruize |
| Follow-on | #511 (PVC key), #512-done/#513 (compat chore), #375-adjacent V14-08 |
