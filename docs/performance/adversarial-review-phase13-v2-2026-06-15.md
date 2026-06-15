# Adversarial Due Diligence Review — Phase 13 Remediation

## Version & Date
Version: 2.0 | Date: 2026-06-15 | Reviewer: AI-assisted  
Previous: Version 1.0, 2026-06-14 (`adversarial-review-phase13-2026-06-14.md`)

## Executive Summary

This incremental review examined remediation commits from 2026-06-14/15 across six repositories on branch `pgarciaq-rosocp-superpowers-phase13`. The scope was the fix commits themselves (worker pools, statement-timeout metrics, Helm security defaults, koku-ui hook fixes, masu NetworkPolicy, smoke perf tests) plus significant new code landed on the branch since v1 (slim namespace list DTO, digest plots, identity middleware dedup, MIG GPU API changes).

**Overall:** Most remediation work is technically sound. The worker-pool refactor, `QueryStatementTimeoutMillis` error return, dead-field split, and koku-ui `useRosCount`/`useEffect` fixes are correct and well-tested. Helm changes for `tagsSource: api`, CSV allowlist auto-derivation, connection budget, and internal auth defaults move the chart in the right direction.

**Residual risk is concentrated in on-prem integration wiring, not in the Go/SQL core.** The tag architecture fix (finding #1) is only half-deployed: ROS is configured for HTTP push (`tagsSource: api`) but Koku workers are not given `ROS_TAGS_ENABLED=true` / `ROS_TAGS_SOURCE=api`, so `ros_tag_sync` never runs. Compounding this, existing `ros-api-access` NetworkPolicy allows only gateway/UI ingress to `ros-api`, blocking `cost-worker` pods from `POST /internal/tags/sync` and `POST /internal/recalculate-savings`. The new `masu-access` NetworkPolicy (finding #4 fix) correctly restricts masu ingress but omits `ros-api`, breaking `KOKU_MASU_URL` calls for GPU savings and business-hours effective rates.

These are real production failures under default chart values with NetworkPolicy enforcement — not style issues. The Go backend remediation itself does not introduce regressions in the areas scrutinized (worker pool, statement timeout, struct split).

## Scorecard

| Dimension | Rating (1-5 stars) | Key gap |
|-----------|-------------------|---------|
| Security | ★★★☆☆ | Tag push and masu paths broken by NetworkPolicy gaps; `INTERNAL_TAGS_AUTH_REQUIRED=false` still non-fatal |
| Correctness | ★★★★☆ | Worker pool and timeout metrics correct; on-prem tag/savings integration incomplete |
| Auditability | ★★★★☆ | `ros_api_statement_timeout_cancellations_total` wired; internal endpoint audit metrics present |
| Operational robustness | ★★★☆☆ | CSV allowlist and connection budget improved; cross-service NP holes cause silent feature loss |
| Performance | ★★★★☆ | Worker pool fixes SaaS-scale goroutine issue; per-endpoint timeout overrides not yet applied |
| Design quality | ★★★★☆ | Threshold/savings flight structs cleanly split; slim list DTO opt-in preserves backward compat |
| Maintainability | ★★★★☆ | Good unit tests for recalc cancellation; Helm lacks lint tests for new security env vars |
| Governance | ★★★☆☆ | Stale `configuration.md` (max_connections=100); koku bundle still large (acknowledged) |

## Previous Findings Verification

| # | Title | Status | Notes |
|---|-------|--------|-------|
| 1 | On-prem tag filtering broken (`tagsSource: db`) | ⚠️ Partial | ROS default switched to `api` (`cost-onprem/values.yaml:141`). Koku **not** wired: `cost-onprem.koku.commonEnv` sets `ROS_OCP_BACKEND_URL` but not `ROS_TAGS_ENABLED` / `ROS_TAGS_SOURCE` (`_helpers-koku.tpl:385-393`). Koku defaults `ROS_TAGS_ENABLED=False`, `ROS_TAGS_SOURCE=db` (`koku/koku/settings.py:708-710`). Tag push never runs. |
| 2 | Missing `ROS_CSV_ALLOWED_HOSTS` | ✅ Verified | Auto-derived from `objectStorage.endpoint` via `cost-onprem.ros.csvAllowedHosts` helper (`_helpers-ros.tpl:9-14`, `_feature-env.yaml:19-20`). Hostname-only endpoint documented in values. |
| 3 | `koku-schema-grants` hook timing | ✅ Verified | Hook conditional on `tagsSource: db` (`koku-schema-grants.yaml:1`). Correct for `api` default. |
| 4 | Masu `AllowAny` lateral movement | 🔄 Introduced regression | `masu-networkpolicy.yaml` added (gated `networkPolicies.enabled: true`). Restricts to `cost-worker`, `ros-processor`, `cost-management-api` — **omits `ros-api`**, which calls `effective_rates` via `KOKU_MASU_URL` (`ros/api/deployment.yaml:96-97`, `internal/costdata/provider.go:149`). |
| 5 | Internal ROS auth bypass | ⚠️ Partial | Chart defaults `internalAuth.enabled: true`, SA defaults to `koku` (`_helpers-ros.tpl:22-29`). `ValidateTagAuthConfig` enforces non-empty SA list in prod. But `ROS_INTERNAL_TAGS_AUTH_REQUIRED=false` is warning-only (`config_validation.go:20-21`), not fatal. `ros-api-access` NP blocks `cost-worker` → ROS internal endpoints (see #23). |
| 6 | Recalc O(clusters) goroutines | ✅ Verified | Worker-pool with `ctx.Err()` checks (`threshold_recalculate.go:150-205`, `savings_recalculate.go:146-187`). Cancellation test passes (`threshold_recalculate_test.go:47-78`). |
| 7 | H-3 incomplete `optimizationsLink` | ✅ Verified | Uses `useRosCount` (`optimizationsLink.tsx:45-54`). Shares projection via `withRosListProjection`. |
| 8 | `useMemo` side effect in chart | ✅ Verified | Replaced with `useEffect` (`optimizationsBreakdownChart.tsx:462-464`). |
| 9 | PostgreSQL connection budget tight | ✅ Verified | `max_connections: 200` (`values.yaml:800`). Budget documented in values and `docs/operations/database-tuning.md`. |
| 10 | Misleading tag DB docs | ✅ Verified | Comments updated in `values.yaml` and `configmap-init.yaml`. |
| 11 | Cluster-quota migration `ROUND` | ✅ Verified | Correctly skipped — integer `* 100` on BIGINT; migration already applied. |
| 12 | Dead `latestSavings` field | ✅ Verified | Split into `thresholdRecalcFlight` / `recalcFlight` in separate guard files (`threshold_recalc_guard.go:23-27`, `savings_recalc_guard.go:27-32`). |
| 13 | API statement timeout 25s | ⚠️ Partial | `ROS_API_STATEMENT_TIMEOUT_MS`, cancellation counter, `SetLocalStatementTimeout()` added (`statement_timeout.go`, `api/utils.go:1192`). **No handler calls `SetLocalStatementTimeout` yet** — per-endpoint overrides are infrastructure-only. |
| 14 | Tag mirror only in one E2E test | ✅ Verified | Resolved by `tagsSource: api` default; mirror no longer needed for default path. |
| 15 | koku large unrelated bundle | ✅ Verified | Acknowledged; separate PRs at upstream merge time. MIG GPU commit (`7a5819c15`) has unit tests. |
| 16 | Perf tests excluded from default CI | ✅ Verified | `test_smoke_perf.py` with `smoke_perf` marker; `run-pytest.sh:339` includes in default pass. |
| 17 | `QueryStatementTimeoutMillis` panics | ✅ Verified | Returns `(int64, error)` (`statement_timeout.go:98-108`). Callers updated. |
| 18 | `optimizationsLink` missing deps | ✅ Verified | `useEffect` removed via `useRosCount` refactor. |
| 19 | Vendor bloat | ✅ Verified | No vendor diff on branch. |
| 20 | nise `*.yml~` backups | ✅ Verified | Removed; `*~` in `.gitignore`. |
| 21 | Docs volume / drift | ✅ Verified | costmgmt-api-cheatsheet updated for tag defaults and GPU fields. |
| 22 | Node recs `order_by` contract | ✅ Verified | Code comments and OpenAPI updated (`eebf82fe`). |

## New Findings

### 23. Tag push path not end-to-end wired (Koku + NetworkPolicy)

**Severity:** Critical (on-prem) / High (SaaS if misconfigured)  
**Dimension:** Correctness, Operational robustness  
**Location:**
- `cost-onprem/templates/_helpers-koku.tpl:385-393` (missing `ROS_TAGS_*` env)
- `cost-onprem/templates/ros/networkpolicies.yaml:136-167` (`ros-api-access` ingress)
- `koku/koku/settings.py:708-710` (Koku defaults)
- `koku/masu/processor/ros_tag_sync.py:33-36` (gate on `ROS_TAGS_SOURCE=api`)

**Description:** Finding #1 switched ROS to `tagsSource: api`, expecting Koku to push tags via `POST /internal/tags/sync`. The chart wires tag env vars only on ROS pods (`_feature-env.yaml`), not on Koku Celery workers via `cost-onprem.koku.commonEnv`. With Koku defaults (`ROS_TAGS_ENABLED=False`, `ROS_TAGS_SOURCE=db`), `schedule_ros_tag_sync()` is a no-op. ROS `APITagProvider` then returns an empty catalog (`api_provider.go:62-63`) — tag filters appear enabled but match nothing.

Additionally, `ros-api-access` NetworkPolicy (always rendered, not gated) allows ingress to `ros-api` only from `gateway` and `ui` — not `cost-worker`. Even if Koku env were fixed, tag sync and savings recalc HTTP callbacks from worker pods would be blocked under NetworkPolicy enforcement.

**Risk:** Silent loss of tag filtering and savings recalculation callbacks in production on-prem clusters. E2E may pass if tests hit ROS through gateway or if NetworkPolicy enforcement is lax on test clusters.

**Recommendation:**
1. Add to `cost-onprem.koku.commonEnv`: `ROS_TAGS_ENABLED=true`, `ROS_TAGS_SOURCE=api` (mirror `ros.api.tagsSource`).
2. Add `cost-worker` (and optionally `cost-processor` for masu-initiated paths) to `ros-api-access` ingress in `networkpolicies.yaml`.
3. Add Helm lint test asserting Koku worker deployments contain `ROS_TAGS_SOURCE=api`.
4. Add integration E2E that verifies `org_tag_sync_metadata` is populated after enabling a tag in Koku Settings (no manual mirror).

**Effort:** 0.5–1 day  
**SaaS vs on-prem:** SaaS platform config likely sets these env vars; on-prem chart gap is the blocker.

**Resolution (2026-06-15):** Added `ROS_TAGS_ENABLED=true` and `ROS_TAGS_SOURCE=api` to `cost-onprem.koku.commonEnv` when `ros.api.tagsSource` is `api` (default). Added `cost-worker` to `ros-api-access` NetworkPolicy ingress. Added Helm lint tests in `test_chart_lint.py::TestSecurityEnvVars` asserting worker deployments contain `ROS_TAGS_SOURCE=api`.

---

### 24. Masu NetworkPolicy blocks ROS API effective_rates calls

**Severity:** High  
**Dimension:** Security (inverted — over-restriction), Correctness  
**Location:** `cost-onprem/templates/infrastructure/network-policies/masu-networkpolicy.yaml:22-37`

**Description:** The finding #4 remediation added `masu-access` NetworkPolicy restricting masu ingress to `cost-worker`, `ros-processor`, and `cost-management-api`. ROS API pods (`app.kubernetes.io/component: ros-api`) call `GET /api/cost-management/v1/effective_rates/` for GPU savings enrichment and business-hours cost data (`ros/api/deployment.yaml:96-97`, `internal/costdata/provider.go:118-149`). They are not in the allowlist.

**Risk:** With `networkPolicies.enabled: true` (default `values.yaml:1192`), ros-api masu calls time out or fail. GPU `estimated_savings` and business-hours savings show zero/stale values. Processor path still works (allowlisted).

**Recommendation:** Add `ros-api` podSelector to masu ingress `from` list. Update `effective_rates.py` security comment (line 9) to include ros-api. Add Helm test rendering masu NP with ros-api label present.

**Effort:** 1–2 hours  
**SaaS vs on-prem:** On-prem only (chart NetworkPolicy). SaaS uses platform-level network controls.

**Resolution (2026-06-15):** Added `ros-api` (`app.kubernetes.io/component: ros-api`) to masu `masu-access` NetworkPolicy ingress `from` list. Added Helm lint test `test_masu_networkpolicy_allows_ros_api`.

---

### 25. CSV allowlist auto-derivation lacks URL normalization

**Severity:** Medium  
**Dimension:** Operational robustness  
**Location:** `cost-onprem/templates/ros/_helpers-ros.tpl:9-14`, `internal/utils/csv_security.go:44-52`

**Description:** Auto-derivation passes `objectStorage.endpoint` verbatim. Values document hostname-only format (`values.yaml:1013-1020`), but if an operator sets `https://s3.example.com` or `host:443`, the allowlist will not match `u.Hostname()` from presigned URLs (`csv_security.go:28,46`), causing processor CSV fetch failures in production.

**Risk:** CrashLoopBackOff or ingestion failures after misconfigured endpoint override.

**Recommendation:** Add Helm helper to strip scheme and port (regex or `url.Parse` in a chart test). Document invalid formats. Optional: normalize in `ValidateSecurityConfig` at ROS startup with a warning.

**Effort:** 2–4 hours  
**SaaS vs on-prem:** Primarily on-prem chart operators; SaaS sets explicit allowlists.

**Resolution (2026-06-15):** `cost-onprem.ros.csvAllowedHosts` helper now strips `https://`/`http://` scheme and trailing `:port` from `objectStorage.endpoint`. Documented normalization in `values.yaml`. Added Helm test verifying `https://s3.example.com:443` renders as `s3.example.com`.

---

### 26. Per-endpoint statement timeout helper unused

**Severity:** Medium  
**Dimension:** Performance, Correctness  
**Location:** `internal/db/statement_timeout.go:64-76`; handlers (no callers)

**Description:** Finding #13 remediation added `SetLocalStatementTimeout()` and `ROS_API_STATEMENT_TIMEOUT_MS`, plus `ros_api_statement_timeout_cancellations_total` (wired in `api/utils.go:1192`). The helper is tested but **no list or aggregation handler invokes it** for known heavy endpoints (fleet summary, savings summary, large keyset lists).

**Risk:** Heavy queries still hit the global 25s pool `AfterConnect` timeout (`db.go:54-57,146`) with no per-route relief. Cancellation metric undercounts if timeouts occur outside `apiErrResponse` paths.

**Recommendation:** Apply `SetLocalStatementTimeout` in transaction scopes for documented heavy handlers (e.g. savings summary, fleet-wide list). Document which endpoints use extended timeouts in `docs/operations/query-performance.md`.

**Effort:** 0.5 day  
**SaaS vs on-prem:** Both; configurable via `ROS_API_STATEMENT_TIMEOUT_MS` for on-prem large fleets.

**Resolution (2026-06-15):** Added `db.WithHeavyStatementTimeout` (45s) and `db.WithHeavyGORMStatementTimeout`. Wired savings-summary aggregation queries and fleet-wide container list (no filters) to use extended timeouts. Documented endpoints in `docs/operations/query-performance.md`.

---

### 27. Internal auth disable is non-fatal at startup

**Severity:** Medium  
**Dimension:** Security  
**Location:** `internal/config/config_validation.go:20-21`, `internal/api/internal_endpoints.go:32-33`

**Description:** Finding #5 enabled internal auth in chart defaults, but `ValidateSecurityConfig()` does not fail when `ROS_INTERNAL_TAGS_AUTH_REQUIRED=false` in non-development mode — only logs `warnInternalTagsAuthNoAllowlist`-style warnings via `ConfigValidationWarnings`. Operators can set `ros.internalAuth.enabled: false` and get unauthenticated internal endpoints.

**Risk:** Cross-tenant tag sync / savings recalc invocation without authentication in misconfigured production clusters.

**Recommendation:** Extend `ValidateSecurityConfig()` to return error when `!Development && !InternalTagsAuthRequired`. Mirror in chart values validation (fail `helm template` if internal auth disabled and not dev).

**Effort:** 2–3 hours  
**SaaS vs on-prem:** Both; on-prem higher risk if operators disable for debugging and forget to re-enable.

**Resolution (2026-06-15):** `ValidateSecurityConfig()` now returns a fatal error when `!Development && !InternalTagsAuthRequired`. Added unit tests in `security_test.go`.

---

### 28. Smoke perf tests may flake; no Helm assertions for security env

**Severity:** Low–Medium  
**Dimension:** Governance, Operational robustness  
**Location:** `tests/suites/ros/test_smoke_perf.py:19-20`, `tests/suites/helm/test_chart_lint.py`

**Description:** Smoke perf thresholds (container list <5s, status <2s) run in default CI without warm-up or retry. Under loaded CI clusters or cold caches, tests may flake. Remediation added security-critical env vars (`ROS_CSV_ALLOWED_HOSTS`, `ROS_TAGS_ALLOWED_SERVICE_ACCOUNTS`, `ROS_TAGS_SOURCE`) but `test_chart_lint.py` has no assertions for them (unlike `GOMEMLIMIT`, `max_connections` tests).

**Risk:** Intermittent CI failures; security regressions in templates could merge undetected.

**Recommendation:** Add retry or P95-based threshold with margin; add helm lint tests for `ROS_CSV_ALLOWED_HOSTS`, `ROS_TAGS_SOURCE=api`, and non-empty `ROS_TAGS_ALLOWED_SERVICE_ACCOUNTS` on processor/api deployments.

**Effort:** 2–4 hours  
**SaaS vs on-prem:** On-prem CI focus.

**Resolution (2026-06-15):** Added `TestSecurityEnvVars` Helm lint tests for `ROS_CSV_ALLOWED_HOSTS`, `ROS_TAGS_SOURCE=api`, and `ROS_TAGS_ALLOWED_SERVICE_ACCOUNTS`. Smoke perf: container list threshold raised to 8s with best-of-2 retry.

---

### 29. Stale connection budget in `configuration.md`

**Severity:** Low  
**Dimension:** Governance  
**Location:** `cost-onprem-chart/docs/operations/configuration.md:492`

**Description:** Example still shows `max_connections: "100"` while `values.yaml` and `database-tuning.md` were updated to 200.

**Risk:** Operators following stale doc under-provision PostgreSQL.

**Recommendation:** Update example to 200 and link to `database-tuning.md`.

**Effort:** 15 minutes  
**SaaS vs on-prem:** On-prem only.

**Resolution (2026-06-15):** Updated `configuration.md` example `max_connections` from `100` to `200` with link to `database-tuning.md`.

---

## Areas Reviewed Outside v1 Scope (No New Issues Found)

| Area | Assessment |
|------|------------|
| Slim namespace list DTO (S4) | Opt-in via `term`/`engine` params preserves backward compat (`handlers.go:823-836`, ADR-0294). koku-ui passes projection via `withRosListProjection`. |
| Worker-pool channel buffer | `make(chan string, len(clusters))` avoids enqueue blocking; `select` with `ctx.Done()` is correct. |
| `SetLocalStatementTimeout` + pooling | `SET LOCAL` is transaction-scoped; safe with pgxpool — resets on commit/rollback. Session timeout set in `AfterConnect` (`db.go:146`) is separate. |
| Identity middleware dedup (A-4) | Positive — no issues found in review. |
| MIG GPU `provider_map` (`7a5819c15`) | `Count("gpu_uuid", distinct=True)`, `mig_id` from `mig_instance_id`; tests in `test_ocp_query_handler.py`. |
| nise / costmgmt-api-cheatsheet | Remediation commits align docs with API behavior. |

## Priority Remediation Order

| Priority | Finding | Effort | Status |
|----------|---------|--------|--------|
| 1 | #23 Tag push end-to-end + ros-api NP | 0.5–1 day | ✅ Resolved 2026-06-15 |
| 2 | #24 Masu NP missing ros-api | 1–2 hours | ✅ Resolved 2026-06-15 |
| 3 | #27 Fail startup when internal auth disabled | 2–3 hours | ✅ Resolved 2026-06-15 |
| 4 | #26 Wire per-endpoint statement timeouts | 0.5 day | ✅ Resolved 2026-06-15 |
| 5 | #25 CSV hostname normalization | 2–4 hours | ✅ Resolved 2026-06-15 |
| 6 | #28 Helm lint + perf test hardening | 2–4 hours | ✅ Resolved 2026-06-15 |
| 7 | #29 Stale configuration.md | 15 min | ✅ Resolved 2026-06-15 |

## Overall Assessment

The Phase 13 remediation **substantially improves** the ros-ocp-backend codebase: goroutine pile-up, panic-prone test helpers, copy-paste struct fields, and koku-ui performance footguns are fixed with tests. Helm defaults for `tagsSource: api`, CSV allowlist, connection budget, and internal auth are directionally correct.

**All 7 findings from this incremental review have been resolved (2026-06-15).** The critical tag-push integration gap (#23) was fixed by wiring `ROS_TAGS_ENABLED=true` and `ROS_TAGS_SOURCE=api` on Koku workers and adding `cost-worker` to the `ros-api-access` NetworkPolicy. The masu NetworkPolicy regression (#24) was fixed by adding `ros-api` to the ingress allowlist. Security hardening (#27) now fails startup when internal auth is disabled in production. Per-endpoint statement timeouts (#26) are wired to savings summary and fleet-wide list handlers.

**Recommendation before merge to upstream:** Verify with an on-prem E2E run that (a) tag push works end-to-end (enable a tag in Koku Settings, confirm ROS tag catalog populated), (b) ros-api GPU savings are non-zero with masu NetworkPolicy enabled, and (c) re-run default CI smoke perf after cluster warm-up. Split koku changes into separate PRs (finding #15 from v1).

---

## Remediation Commits Reviewed (2026-06-14/15)

| Repo | Commits |
|------|---------|
| ros-ocp-backend | `b9b1c6e5`, `690ff83c`, `4947f078`, `eebf82fe`, `a7c99e2c` |
| cost-onprem-chart | `0ffbfab`, `dace7c6`, `c618485`, `2a4fb91`, `c46eedf` |
| koku-ui | `c3a187c59` |
| koku | `95bd88674`, `7a5819c15` (MIG, separate from adversarial batch) |
| nise | `ac98ee7` |
| costmgmt-api-cheatsheet | `d2a864b` |
