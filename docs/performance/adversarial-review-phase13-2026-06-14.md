# Adversarial Code Review: `pgarciaq-rosocp-superpowers-phase13`

**Date:** 2026-06-14
**Scope:** Diff vs `main` merge-base across six repos.
**Primary focus:** ros-ocp-backend Go/SQL, cost-onprem-chart Helm/E2E, koku-ui ROS, koku masu/GPU, supporting docs/nise.

---

## CRITICAL (must fix before merge)

### 1. On-prem tag filtering is broken by default (`tagsSource: db` + separate databases)

**Repos:** cost-onprem-chart, ros-ocp-backend, koku

| Location | Lines |
|----------|-------|
| `cost-onprem/values.yaml` | 125–128 |
| `cost-onprem/templates/ros/_feature-env.yaml` | 11–12 |
| `internal/tags/db_provider.go` | 17–40 |
| `internal/tags/startup.go` | 33–41 |
| `koku/masu/processor/ros_tag_sync.py` | 33–36 |
| `tests/e2e_helpers.py` | 600–675 |

**Issue:** Chart defaults `tagsSource: db` with comment "shared PostgreSQL." ROS `DATABASE_URL` points at `costonprem_ros`; Koku tag tables (`reporting_enabledtagkeys`, `reporting_ocptags_values`) live in `costonprem_koku` tenant schemas. `DBTagProvider` queries those tables on the **ROS** pool only. Koku push sync (`ros_tag_sync`) runs only when `ROS_TAGS_SOURCE=api`.

**Failure mode:** `RunStartupHealthCheck` cannot find Koku tables in the ROS database and **silently disables** tag filtering via `config.DisableTagsFeature()` — no fatal error, no user-visible failure.

**Why it matters:** Tag filters appear enabled in values/docs but do nothing in production. E2E only works because `mirror_koku_ocp_tags_to_ros_db()` manually copies tables cross-database (`test_namespace_recommendations.py`); that helper is **not** deployed by the chart.

**Suggested fix (pick one):**
- Switch on-prem default to `tagsSource: api` and wire Koku `ROS_TAGS_SOURCE=api` + periodic `ros_tag_sync`, **or**
- Add a production tag-mirror job (Celery/cron) from `costonprem_koku` → `costonprem_ros`, **or**
- Add `KOKU_DATABASE_URL` second pool in ROS for `DBTagProvider` (grants in `koku-schema-grants.yaml` then become meaningful).

**SaaS vs On-prem implications:**

| | SaaS | On-prem |
|--|------|---------|
| Databases | Shared RDS instance; cross-DB access managed by platform | Two logical DBs in same PG pod; cross-DB access possible but not wired |
| Correct mode | `tagsSource: api` (Koku pushes via `ros_tag_sync`) | `tagsSource: api` (same push mechanism) |
| Current default | `api` (works) | `db` (broken — ROS queries its own DB for Koku tables that don't exist) |
| Recommended fix | No change | Switch default to `tagsSource: api`; ensure `ros_tag_sync` runs on Koku worker |

---

### 2. Missing `ROS_CSV_ALLOWED_HOSTS` will block processor startup in production

**Repos:** ros-ocp-backend, cost-onprem-chart

| Location | Lines |
|----------|-------|
| `internal/config/security.go` | 25–36 |
| `cmd/start.go` | 35 |
| `cost-onprem/templates/ros/processor/deployment.yaml` | 61–120 |
| `cost-onprem/templates/ros/_feature-env.yaml` | (no CSV env) |

**Issue:** Phase13 adds `ValidateSecurityConfig()` — fatal when `DEVELOPMENT=false` and `ROS_CSV_ALLOWED_HOSTS` is empty. Chart never sets `ROS_CSV_ALLOWED_HOSTS` or object-storage hostname (NooBaa/RGW). Processor ingestion always fetches CSVs via HTTP (`utils.ReadCSVFromUrl`).

**Why it matters:** Upgrading to phase13 ROS images on an existing cluster → **CrashLoopBackOff** on `ros-processor` unless operators manually inject the allowlist.

**Suggested fix:** Add `ros.csvAllowedHosts` (or derive from S3 endpoint) to Helm values and inject `ROS_CSV_ALLOWED_HOSTS` on processor **and** API deployments.

**SaaS vs On-prem implications:**

| | SaaS | On-prem |
|--|------|---------|
| CSV source | S3 (AWS) — hostname known at deploy time | NooBaa/Ceph RGW/MinIO — varies per cluster |
| Current state | Platform team sets in app-interface config | Chart never sets it → CrashLoopBackOff on upgrade |
| Recommended fix | Document in deployment runbook | Auto-derive from `objectStorage.endpoint` in Helm values; fail at install if empty |

---

## HIGH (should fix before merge)

### 3. `koku-schema-grants` hook misses tenants on fresh install and new orgs

**Repo:** cost-onprem-chart

| Location | Lines |
|----------|-------|
| `cost-onprem/templates/ros/jobs/koku-schema-grants.yaml` | 12–15, 75–85 |
| Comment vs reality | Hook says "after migrations so tenant schemas exist" |

**Issue:** Tenant schemas (`org*`) are created when **koku-server** first boots (django-tenants), which runs **after** Helm pre-install hooks. The grants job loops `pg_namespace WHERE nspname LIKE 'org%'` at hook time — often **zero schemas** on first install. `ALTER DEFAULT PRIVILEGES` covers future tables, not `GRANT USAGE ON SCHEMA` for schemas created later.

**Why it matters:** Even if ROS gains a Koku DB connection (finding #1), new tenants after install/upgrade won't get schema USAGE until the next `helm upgrade` re-runs the hook.

**Suggested fix:** Post-start CronJob or koku-server init hook that re-applies grants when new `org*` schemas appear; or run grants from tenant-creation path in Koku.

**SaaS vs On-prem implications:**

| | SaaS | On-prem |
|--|------|---------|
| Tenant creation | Managed by platform; schemas exist before ROS | Created by django-tenants on koku-server boot — after Helm hooks |
| Impact | N/A — hook not used | Hook finds zero schemas on fresh install |
| Recommended fix | N/A | With `tagsSource: api` (finding #1), grants hook is unnecessary for tags. Keep hook but document it's only needed for `db` mode |

---

### 4. Masu internal endpoints use `AllowAny` — cluster lateral movement risk

**Repo:** koku

| Location | Lines |
|----------|-------|
| `koku/masu/api/effective_rates.py` | 147 |
| `koku/masu/api/reship_ros.py` | 294 |

**Issue:** `effective_rates` exposes cost model rates + namespace aggregates; `reship_ros` can republish Kafka messages and presigned S3 URLs. Both use `@permission_classes((AllowAny,))`.

**Why it matters:** Any pod that can reach `koku-masu:8000` (broader than ROS SA) can read tenant cost data or trigger re-ships for arbitrary `org_id`/`provider_uuid` if network policies are loose.

**Suggested fix:** Require service-account token or shared secret; restrict via NetworkPolicy to ROS worker SA only; at minimum document as **intentional internal-only** with mandatory network isolation.

**SaaS vs On-prem implications:**

| | SaaS | On-prem |
|--|------|---------|
| Network isolation | Strong — service mesh + NetworkPolicies enforced by platform | Weak — single namespace, no default NetworkPolicies |
| Impact | Acceptable with platform-level defense-in-depth | Higher risk — any pod can reach masu-server |
| Recommended fix | Document as intentional internal-only | Add NetworkPolicy restricting masu-server ingress to koku-worker and ros-processor only |

---

### 5. Internal ROS endpoints bypass identity middleware; auth can be disabled

**Repo:** ros-ocp-backend

| Location | Lines |
|----------|-------|
| `internal/api/server.go` | 179–184 |
| `internal/api/internal_endpoints.go` | 31–40 |

**Issue:** `/api/cost-management/v1/internal/*` has no Identity/RBAC. `authenticateInternalCaller` returns `"auth-disabled"` when `ROS_INTERNAL_TAGS_AUTH_REQUIRED=false`. Chart does not set `ROS_TAGS_ALLOWED_SERVICE_ACCOUNTS` (empty default) — with auth on, **all** internal calls fail until configured.

**Why it matters:** Mis-set env in production → unauthenticated cross-tenant tag sync / savings recalc. Empty SA allowlist with auth on → Koku tag push and savings recalc silently broken.

**Suggested fix:** Fail startup when `!DEVELOPMENT && !InternalTagsAuthRequired`; chart should populate `ROS_TAGS_ALLOWED_SERVICE_ACCOUNTS` with koku-worker SA; document required values.

**SaaS vs On-prem implications:**

| | SaaS | On-prem |
|--|------|---------|
| `INTERNAL_TAGS_AUTH_REQUIRED` | `true` (enforced by platform) | `false` (chart default) — unauthenticated |
| `TAGS_ALLOWED_SERVICE_ACCOUNTS` | Set to koku-worker SA by platform config | Empty — never configured |
| Recommended fix (SaaS) | Ensure SA list populated; fail startup if true + empty list | |
| Recommended fix (on-prem) | | Set auth to `true`; populate SA list from chart; add NetworkPolicy |

---

### 6. Threshold/savings recalc spawns O(clusters) goroutines without context cancellation

**Repo:** ros-ocp-backend

| Location | Lines |
|----------|-------|
| `internal/engine/threshold_recalculate.go` | 149–188 |
| `internal/engine/savings_recalculate.go` | ~145–169 (same pattern) |

**Issue:** For each cluster, a goroutine is spawned. Semaphore limits concurrent DB work to 3, but goroutines still start for every cluster. No `ctx.Done()` check before acquiring semaphore or inside work loops.

**Why it matters:** Large fleets + SIGTERM during recalc → goroutine pile-up, delayed shutdown (30s grace in `asyncjobs/shutdown.go`), connection pool pressure during rolling deploys.

**Suggested fix:** Worker-pool pattern: feed cluster IDs through a buffered channel with fixed worker count; check `ctx.Err()` before and after semaphore acquire.

**SaaS vs On-prem implications:**

| | SaaS | On-prem |
|--|------|---------|
| Cluster count | Potentially thousands (multi-tenant) | Typically 1–20 |
| Impact | High — goroutine pile-up, pool exhaustion during rolling deploys | Low — few clusters |
| Recommended fix | Fix with worker-pool + `ctx.Done()` (SaaS scalability issue) | Same fix, lower priority but applied for code correctness |

---

### 7. H-3 duplicate-call fix is incomplete in `optimizationsLink`

**Repo:** koku-ui

| Location | Lines |
|----------|-------|
| `apps/koku-ui-ros/src/hooks/useRosCount.ts` | 26–81 (partial fix) |
| `apps/koku-ui-ros/src/routes/optimizations/optimizationsLink/optimizationsLink.tsx` | 76–79 |

**Issue:** Badge/summary paths use `useRosCount` (`limit=1` fallback), but `optimizationsLink` still dispatches full `fetchRosReport` without `limit=1` on every cluster/project navigation.

**Why it matters:** Phase13 performance goal (H-3) only partially delivered; link navigation still pulls full recommendation payloads.

**Suggested fix:** Use `useRosCount` or add `limit=1` + projection params to link prefetch; share Redux count cache key with parent views.

**SaaS vs On-prem implications:**

| | SaaS | On-prem |
|--|------|---------|
| Traffic volume | High — many users navigating clusters/projects | Low — single operator |
| Impact | Medium — unnecessary full payloads on every navigation | Low — few concurrent users |
| Recommended fix | Fix before merge | Same fix for code hygiene |

---

### 8. `useMemo` used for side effects in percentile breakdown chart

**Repo:** koku-ui

| Location | Lines |
|----------|-------|
| `apps/koku-ui-ros/src/routes/optimizations/optimizationsBreakdown/optimizationsBreakdownChart.tsx` | 462–464 |

**Issue:**
```tsx
useMemo(() => {
  initDatum();
}, [limitData, requestData, usageData]);
```
`initDatum()` mutates chart state (`setSeries`, `setHiddenSeries`). React does not guarantee `useMemo` runs when dependencies change (only when recalculating during render).

**Why it matters:** Stale or missing percentile band updates under concurrent renders / Strict Mode — E-2 chart correctness risk.

**Suggested fix:** Replace with `useEffect` with the same dependency array.

**SaaS vs On-prem implications:**

| | SaaS | On-prem |
|--|------|---------|
| Impact | Same — chart may not update correctly under React Strict Mode | Same |
| Recommended fix | `useMemo` → `useEffect` — trivial, no config difference | Same |

---

### 9. PostgreSQL connection budget is extremely tight at default scale

**Repo:** cost-onprem-chart

| Location | Lines |
|----------|-------|
| `cost-onprem/values.yaml` | 65, 115–117, 774 |

**Issue:** `max_connections: 100`, `ros.dbMaxConns: 5`, HPA `maxReplicas: 4` → up to 20 ROS API connections alone, plus processor, housekeeper, Koku workers, RBAC, Kruize, admin.

**Why it matters:** Enabling HPA or adding ROS components → `FATAL: too many connections` under normal load.

**Suggested fix:** Raise `max_connections` in production values profile; or lower `dbMaxConns` / `maxReplicas` with explicit connection budget doc in chart README.

**SaaS vs On-prem implications:**

| | SaaS | On-prem |
|--|------|---------|
| PostgreSQL | AWS RDS (configurable, typically 200–1000 connections) | Chart-managed PG pod (`max_connections: 100`) or external DBaaS |
| Impact | Not an issue — RDS handles it | Real risk — HPA scaling → connection exhaustion |
| Recommended fix | N/A | Raise to 200, document connection budget in README, cap HPA based on headroom |

---

## MEDIUM (fix soon after merge)

### 10. Misleading architecture comments/docs for tag DB mode

**Repos:** cost-onprem-chart, ros-ocp-backend

| Location | Lines |
|----------|-------|
| `cost-onprem/values.yaml` | 126 |
| `cost-onprem/templates/infrastructure/database/configmap-init.yaml` | 115–116 |
| `internal/tags/db_provider.go` | 17 |

**Issue:** Docs/comments say "shared PostgreSQL" and `GRANT CONNECT ON DATABASE koku` enables tag filtering, but ROS never opens a Koku DB connection.

**Suggested fix:** Align docs with chosen architecture from CRITICAL #1.

**SaaS vs On-prem implications:**

Same issue in both — docs say "shared PostgreSQL" but architecture doesn't share. Fix follows from finding #1 decision.

---

### 11. Cluster-quota savings migration lacks `ROUND` (integer dollars → cents)

**Repo:** ros-ocp-backend

| Location | Lines |
|----------|-------|
| `migrations/000132_cluster_quota_savings_to_cents.up.sql` | 4–5 |

**Issue:** `USING (COALESCE(savings_dollars_monthly, 0) * 100)` on INT column — fine for whole dollars, but inconsistent with VM migration pattern using `ROUND`.

**Why it matters:** Low risk today; future fractional-dollar columns would silently truncate.

**Suggested fix:** `ROUND(COALESCE(...) * 100)` for consistency.

**SaaS vs On-prem implications:**

Same in both — pure code correctness. No deployment difference.

---

### 12. Dead field in threshold recalc flight struct (copy-paste)

**Repo:** ros-ocp-backend

| Location | Lines |
|----------|-------|
| `internal/engine/threshold_recalc_guard.go` | 23–28 |

**Issue:** `latestSavings savingsRecalcParams` in `recalcFlight` is unused (copied from savings guard).

**Suggested fix:** Remove field or implement coalescing params if intended.

**SaaS vs On-prem implications:**

Same in both — pure code cleanup. No deployment difference.

---

### 13. API statement timeout (25s) may truncate heavy list queries

**Repo:** ros-ocp-backend

| Location | Lines |
|----------|-------|
| `internal/db/db.go` | 54–56, 146 |
| `internal/db/statement_timeout.go` | 16–22 |

**Issue:** Global 25s `statement_timeout` on pool connect for API path; ingestion uses separate 120s `SET LOCAL`.

**Why it matters:** Large fleet list/keyset queries or savings summary aggregations may hit timeout under load → 500s classified as transient.

**Suggested fix:** Per-endpoint timeout override or raise API timeout for known heavy endpoints; add metrics on `statement_timeout` cancellations.

**SaaS vs On-prem implications:**

| | SaaS | On-prem |
|--|------|---------|
| External timeout | 30s ingress/gateway timeout imposed by HCCM team | No external timeout by default |
| 25s statement_timeout | Well-calibrated (5s buffer for response serialization) | Could raise, but 25s is reasonable default |
| Recommended fix | Keep 25s default; add per-endpoint overrides + cancellation metrics | Same default; expose as Helm value for operators with large fleets |

---

### 14. Tag mirror only in one E2E test — not session fixture

**Repo:** cost-onprem-chart

| Location | Lines |
|----------|-------|
| `tests/e2e_helpers.py` | 600–675 |
| `tests/suites/ros/test_namespace_recommendations.py` | 472 |

**Issue:** `mirror_koku_ocp_tags_to_ros_db` is called from a single test, not `data_seeding.py` autouse fixture.

**Why it matters:** Other tag-filter E2E tests may pass/fail depending on whether mirror ran; false confidence in production tag behavior.

**Suggested fix:** Integrate mirror into seeding fixture when `tagsSource=db`, or switch chart to `api` mode for tests.

**SaaS vs On-prem implications:**

On-prem only — mirror is an E2E test workaround for finding #1. SaaS uses `api` mode. Resolving finding #1 resolves this.

---

### 15. koku phase13 bundles large unrelated changes (merge risk)

**Repo:** koku

**Scope:** ~209 files — price lists, `effective_rates`, `reship_ros`, `ros_tag_sync`, GPU `provider_map` (`gpu_count` → `Count("gpu_uuid", distinct=True)`, `mig_id`, `rank_group_by`).

**Why it matters:** GPU API semantics changed (distinct GPU count, MIG-level ranking). IQE notes dependency on migration `0344` (`mig_instance_id`). Mixing FinOps price-list work with ROS perf increases regression surface.

**Suggested fix:** Separate PRs or explicit compatibility matrix in PR description; run GPU + ROS IQE profiles.

**SaaS vs On-prem implications:**

Process issue, same for both. Split into separate PRs regardless of deployment target.

---

### 16. Performance test suite excluded from default CI

**Repo:** cost-onprem-chart

**Issue:** Performance suites use `extended` marker; default `run-pytest.sh` excludes them (~88 tests in ~3 min CI vs 15+ min extended).

**Why it matters:** Phase13 perf regressions may merge without automated detection.

**Suggested fix:** Add smoke perf assertions to default CI or nightly extended job.

**SaaS vs On-prem implications:**

| | SaaS | On-prem |
|--|------|---------|
| CI | Own pipeline with IQE profiles catching perf regressions | `run-pytest.sh` excludes extended/perf tests |
| Recommended fix | Already covered by IQE | Add smoke perf assertions to default CI |

---

### 17. `QueryStatementTimeoutMillis` panics on DB error

**Repo:** ros-ocp-backend

| Location | Lines |
|----------|-------|
| `internal/db/statement_timeout.go` | 42–50 |

**Issue:** `panic(fmt.Sprintf(...))` on scan failure — test-only helper today, but fragile if reused.

**Suggested fix:** Return error instead of panic.

**SaaS vs On-prem implications:**

Same in both — pure code quality. No deployment difference.

---

## LOW (nice to have)

### 18. `optimizationsLink` `useEffect` missing dispatch deps

**Repo:** koku-ui — `optimizationsLink.tsx` lines 76–80

**Issue:** ESLint exhaustive-deps likely suppressed; `dispatch`, `reportFetchStatus`, `reportError` omitted.

---

### 19. Vendor directory bloat in ros-ocp-backend branch

**Issue:** Thousands of vendor file changes inflate review diff and merge conflict risk. Prefer `go mod vendor` in CI only or separate vendor sync commit.

---

### 20. nise example backup files (`*.yml~`)

**Repo:** nise — check diff for committed editor backups; remove from branch.

---

### 21. costmgmt-api-cheatsheet / docs-site volume

**Repos:** costmgmt-api-cheatsheet, ros-ocp-backend `docs-site/`, cost-onprem-chart `docs/`

**Issue:** Large doc additions may reference `tagsSource: db` "shared PostgreSQL" incorrectly (finding #10). Spot-check Bruno examples against live OpenAPI after savings-cents migrations.

---

### 22. Node recs `order_by=savings` maps to DB column but some sorts are in-memory

**Repo:** ros-ocp-backend — `handlers_node_recs.go` / list options

**Issue:** Confusing API contract, not necessarily wrong. Document which `order_by` values are DB-backed vs post-aggregate.

**SaaS vs On-prem implications (findings 18–22):**

All code-quality or documentation issues with no SaaS/on-prem config difference.

---

## Positive observations (not issues)

- **Order-by SQL injection:** `listoptions` uses whitelist `OrderByMap` — no raw user SQL.
- **CSV SSRF:** Allowlist + private-network deny in `csv_security.go`; redirects disabled.
- **Kafka parallel commits:** Mutex serialization in `internal/kafka/commit.go` (ADR note).
- **Internal savings recalc:** `PostRecalculateSavings` uses `authenticateInternalCaller` (unlike masu `AllowAny`).
- **Savings API JSON:** Cluster-quota still exposes `estimated_savings` as `MoneyAmount` despite DB cents migration — backward-compatible at HTTP layer.
- **Async shutdown:** `asyncjobs` package has graceful drain with 30s grace.

---

## Architecture pattern summary

The findings cluster into three groups:

1. **On-prem-only issues** (findings 1, 2, 3, 5, 9, 14): The Helm chart defaults are wrong or incomplete for production. SaaS sidesteps these because platform teams configure things differently.

2. **SaaS-amplified issues** (findings 6, 7, 13): The code is technically wrong in both environments but only causes real problems at SaaS scale (many tenants, large fleets, many concurrent users).

3. **Same in both** (findings 4, 8, 10–12, 15–22): Code correctness, security posture, or process issues that apply equally.

The biggest takeaway: the on-prem chart needs a serious defaults review. Most critical/high findings stem from defaults that only work with manual intervention.

## Recommended pre-merge checklist

1. **Resolve tag architecture** (CRITICAL #1) — do not merge with silent tag disable.
2. **Add `ROS_CSV_ALLOWED_HOSTS` to Helm** (CRITICAL #2) — verify processor starts with `DEVELOPMENT=False`.
3. **Fix koku-ui `useMemo` → `useEffect`** (HIGH #8).
4. **Complete H-3 in optimizationsLink** (HIGH #7).
5. **Document or restrict masu `AllowAny` endpoints** (HIGH #4).
6. **Run extended perf + ROS tag-filter E2E** with production-like env (no manual mirror).
7. **IQE smoke/extended** with `SKIP_GPU_TESTS=false` after koku GPU migration.

---

## Repo change summary

| Repository | Approx. scope | Highest-risk areas |
|------------|---------------|-------------------|
| **ros-ocp-backend** | Largest (migrations, engine, API, ingestion) | Tag DB provider, CSV allowlist, recalc goroutines, internal auth |
| **cost-onprem-chart** | Helm, PG tuning, E2E helpers | `tagsSource: db`, missing CSV env, grants hook timing |
| **koku-ui** | ROS hooks, percentile charts | H-3 incomplete, `useMemo` side effect |
| **koku** | masu endpoints, GPU maps, ros_tag_sync | `AllowAny` masu APIs, push sync gated on `api` mode |
| **costmgmt-api-cheatsheet** | Bruno/API docs | Drift vs implemented tag/CSV config |
| **nise** | OCP/ROS generators | Data shape for native engine CSV plugins |
