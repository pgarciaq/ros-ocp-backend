# Adversarial Due Diligence Review — Phase 13

## Version & Date
Version: 3.0 | Date: 2026-06-15 | Reviewer: AI-assisted  
Previous: Version 2.0 (2026-06-15), Version 1.0 (2026-06-14)

## Executive Summary

This incremental review (v3) examined remediation commits landed on 2026-06-15 after v2, focusing on whether fixes for findings #23–#29 introduced regressions, whether those fixes are complete, and whether any gaps remain across the six-repo branch diff.

**The Go/SQL and koku-ui remediation from v1/v2 is sound.** Worker pools, internal-auth fatal validation, masu/ros-api NetworkPolicy label selectors, CSV scheme/port normalization, per-endpoint `SET LOCAL` timeouts, and Helm lint assertions are implemented correctly in isolation. Helm template rendering confirms `ROS_TAGS_SOURCE=api`, normalized `ROS_CSV_ALLOWED_HOSTS=s3.example.com` from `https://s3.example.com:443`, and `ros-api` on the masu ingress allowlist.

**However, the critical on-prem tag-push path (#23) is still not production-ready.** v2 wired Koku `ROS_TAGS_*` env vars and opened `ros-api-access` to `cost-worker`, but every Koku Celery worker pod sets `automountServiceAccountToken: false` without a projected service-account token volume. With `ROS_INTERNAL_TAGS_AUTH_REQUIRED=true` (chart default), `ros_tag_sync` cannot obtain a bearer token and fails; `ros_savings_recalc` posts without auth and receives 401. Tag filters remain empty under default chart values despite appearing correctly configured.

**Secondary gaps** include stale Koku integration docs still describing on-prem `tagsSource=db` as default, a 45s heavy-query timeout that exceeds the SaaS 30s ingress ceiling, and incomplete v2 follow-ups (tag-push E2E, configuration.md prose).

## Scorecard

| Dimension | Rating (1-5 stars) | Key gap |
|-----------|-------------------|---------|
| Security | ★★★☆☆ | Tag push auth token unavailable on workers; internal auth hardening (#27) is correct |
| Correctness | ★★★☆☆ | End-to-end tag push and savings recalc callbacks fail silently/with errors |
| Auditability | ★★★★☆ | `rosocp_internal_endpoint_calls_total` and timeout cancellation metrics present |
| Operational robustness | ★★★☆☆ | SA token mount missing; CSV path-in-endpoint edge case; no tag-push E2E |
| Performance | ★★★★☆ | Heavy timeouts wired; 45s exceeds SaaS ingress budget |
| Design quality | ★★★★☆ | Transaction-scoped `SET LOCAL` pattern is correct; flight structs cleanly split |
| Maintainability | ★★★☆☆ | Helm security tests skip without cluster fixture; stale architecture docs |
| Governance | ★★★☆☆ | `ros-ocp-integration.md` contradicts chart defaults; configuration.md prose drift |

## Previous Findings Verification (v2 #23–#29)

| # | Title | Status | Notes |
|---|-------|--------|-------|
| 23 | Tag push wiring + NP | 🔄 Regression | `cost-onprem.koku.commonEnv` correctly sets `ROS_TAGS_ENABLED=true` / `ROS_TAGS_SOURCE=api` when `tagsSource: api` (`_helpers-koku.tpl:388-394`). `ros-api-access` allows `cost-worker` with matching labels (`networkpolicies.yaml:159-163`, `_helpers-koku.tpl:219-232`). **But** all Celery workers use `automountServiceAccountToken: false` with no projected SA token (`deployment-worker-default.yaml:23`, `_helpers-koku.tpl:435-468`) — see **#30**. v2 item 4 (tag-push E2E) not implemented — see **#35**. |
| 24 | Masu NP + ros-api | ✅ Verified | `masu-access` includes `app.kubernetes.io/component: ros-api` (`masu-networkpolicy.yaml:35-38`). Label matches `ros/api/deployment.yaml:21`. Masu pod selector `cost-processor` matches `masu/deployment.yaml:19`. No unintended cross-namespace access (all selectors include `app.kubernetes.io/instance`). |
| 25 | CSV hostname normalization | ⚠️ Partial | Scheme/port stripping works (`_helpers-ros.tpl:14-16`); Helm render of `https://s3.example.com:443` → `s3.example.com` confirmed. URL paths not stripped (`https://host/bucket` → `host/bucket`) — see **#34**. Documented as hostname-only in `values.yaml:1013-1014`. |
| 26 | Per-endpoint statement timeouts | ⚠️ Partial | `WithHeavyStatementTimeout` / `WithHeavyGORMStatementTimeout` use transaction-scoped `SET LOCAL` with deferred rollback (`statement_timeout.go:77-114`) — resets correctly on commit. Wired to savings-summary (`handlers_savings_summary.go:79`) and fleet-wide container list (`recommendation_set_native.go:405-414`). 45s exceeds SaaS 30s ingress — see **#33**. No dedicated unit tests for heavy helpers. |
| 27 | Internal auth fatal | ✅ Verified | `ValidateSecurityConfig()` returns error when `!Development && !InternalTagsAuthRequired` (`security.go:28-35`). Unit tests cover prod/dev paths (`security_test.go:31-52`). `DEVELOPMENT` parsed as bool via viper (`config.go:33`). Chart does not set `DEVELOPMENT` on ROS pods (defaults false) while `internalAuth.enabled: true` (`_feature-env.yaml:13-14`). |
| 28 | Helm lint + smoke perf | ⚠️ Partial | `TestSecurityEnvVars` class added (`test_chart_lint.py:310-374`). Tests skip in environments without Keycloak route (fixture dependency). Container list uses 8s threshold with best-of-2 retry (`test_smoke_perf.py:19-67`); status endpoint has no retry — see **#36**. |
| 29 | Stale configuration.md | ⚠️ Partial | Example block updated to `max_connections: "200"` (`configuration.md:492`). Prose at line 690 still states bundled default is `max_connections=100` — see **#32**. |

## New Findings

### 30. Koku Celery workers cannot obtain SA token for ROS internal auth

**Severity:** Critical (on-prem)  
**Dimension:** Correctness, Security, Operational robustness  
**Location:**
- `cost-onprem/templates/cost-management/celery/deployment-worker-default.yaml:23`
- `cost-onprem/templates/_helpers-koku.tpl:435-468` (no projected token volume)
- `koku/masu/processor/ros_tag_sync.py:52-64` (`_read_bearer_token`)
- `koku/masu/processor/ros_savings_recalc.py:41-45`, `72-75`
- `cost-onprem/values.yaml:144` (comment references projected SA token but chart does not mount it)

**Description:** v2 #23 enabled Koku tag push (`ROS_TAGS_SOURCE=api`) and ROS internal auth (`ROS_INTERNAL_TAGS_AUTH_REQUIRED=true`). Tag sync requires a Kubernetes service-account bearer token at `/var/run/secrets/kubernetes.io/serviceaccount/token`. Every Koku Celery worker deployment sets `automountServiceAccountToken: false` and the shared `cost-onprem.koku.volumes` helper provides only `tmp`, `aws-config`, and CA bundle volumes — no projected service-account token.

**Failure modes:**
- `sync_ros_ocp_tags` → `_read_bearer_token()` logs warning, raises `RuntimeError("ROS tag sync bearer token is not configured")` — Celery task fails, tags never populate `org_tag_sync_metadata`.
- `notify_ros_savings_recalculation` → posts without `Authorization` → ROS returns 401; logged as warning only (silent savings staleness).

**Risk:** Production on-prem clusters with default chart values and NetworkPolicy enforcement appear correctly configured but tag filtering and post–cost-model savings recalc do not work.

**Recommendation:**
1. Add projected SA token volume to `cost-onprem.koku.volumes` / `volumeMounts` (or set `automountServiceAccountToken: true` on workers if acceptable to security review).
2. Add Helm lint test asserting worker pods mount a service-account token when `ros.api.tagsSource=api`.
3. Add E2E verifying `org_tag_sync_metadata` populated after enabling a tag in Koku Settings.

**Effort:** 2–4 hours  
**SaaS vs on-prem:** SaaS platform config likely mounts tokens via app-interface; on-prem chart gap is the blocker.

---

### 31. Koku `ros-ocp-integration.md` still documents on-prem `tagsSource=db` default

**Severity:** Medium  
**Dimension:** Governance, Maintainability  
**Location:** `koku/docs/architecture/ros-ocp-integration.md:255-298`, `491-508`

**Description:** The integration guide mermaid diagram and configuration tables state on-prem default is `ROS_TAGS_SOURCE=db` with direct PostgreSQL reads. The cost-onprem chart now defaults `tagsSource: api` (`values.yaml:141`) and wires Koku push env vars. Operators following this doc will misconfigure deployments or debug the wrong data path.

**Risk:** Wrong architecture decisions, wasted debugging time, contradictory runbooks across repos.

**Recommendation:** Update the doc to reflect on-prem default `api` mode; retain `db` as advanced/shared-PostgreSQL option. Cross-link `cost-onprem/values.yaml` comments.

**Effort:** 1–2 hours  
**SaaS vs on-prem:** Documentation only; affects both audiences reading koku docs.

---

### 32. `configuration.md` prose still cites `max_connections=100` default

**Severity:** Low  
**Dimension:** Governance  
**Location:** `cost-onprem-chart/docs/operations/configuration.md:690`

**Description:** v2 #29 updated the YAML example to `200` (line 492) but narrative text at line 690 still says "bundled PostgreSQL image default is `max_connections=100`". `values.yaml:800` and `database-tuning.md` correctly document 200.

**Risk:** Operators sizing external DBaaS from stale prose under-provision connections.

**Recommendation:** Change line 690 to 200 and link to `database-tuning.md`.

**Effort:** 15 minutes  
**SaaS vs on-prem:** On-prem only.

---

### 33. Heavy API statement timeout (45s) exceeds SaaS ingress budget (30s)

**Severity:** Medium (SaaS) / Low (on-prem)  
**Dimension:** Performance, Operational robustness  
**Location:** `internal/db/statement_timeout.go:67-68`, `handlers_savings_summary.go:79`, `recommendation_set_native.go:407`

**Description:** `HeavyAPIStatementTimeoutMS()` is hardcoded to 45000ms. Savings-summary and fleet-wide list handlers use this extended window. On console.redhat.com the ingress/gateway timeout is ~30s. Clients receive 504/timeout while PostgreSQL continues the query until 45s, wasting pool slots and connection budget.

**Risk:** SaaS users see timeouts on heavy fleet queries; DB load continues after client disconnect; cancellation metric may undercount (client abort vs statement_timeout).

**Recommendation:** Cap heavy timeout at `min(45000, ingress_timeout - buffer)` for SaaS deployments, or make `ROS_HEAVY_API_STATEMENT_TIMEOUT_MS` configurable via Helm with SaaS default ~28s. Document in `query-performance.md`.

**Effort:** 2–4 hours  
**SaaS vs on-prem:** Primarily SaaS; on-prem has no external ingress timeout by default.

---

### 34. CSV allowlist normalization does not strip URL paths

**Severity:** Low  
**Dimension:** Operational robustness  
**Location:** `cost-onprem/templates/ros/_helpers-ros.tpl:13-17`

**Description:** Normalization strips `https://`/`http://` prefix and trailing `:port` but not path segments. An operator setting `objectStorage.endpoint: https://s3.example.com/bucket` would render `ROS_CSV_ALLOWED_HOSTS=s3.example.com/bucket`, which will not match `u.Hostname()` from presigned URLs (`csv_security.go:28,46`).

**Risk:** Processor CSV fetch blocked if endpoint includes a path component (uncommon but possible with misconfigured values).

**Recommendation:** Add `regexReplaceAll "/.*$" ""` after scheme/port stripping, or split on `/` and take first segment. Extend Helm test.

**Effort:** 1 hour  
**SaaS vs on-prem:** On-prem chart operators.

---

### 35. No E2E test for end-to-end tag push (v2 #23 follow-up)

**Severity:** Low–Medium  
**Dimension:** Governance, Operational robustness  
**Location:** `cost-onprem-chart/tests/` (absent)

**Description:** v2 #23 recommended an integration E2E verifying `org_tag_sync_metadata` is populated after enabling a tag in Koku Settings without manual mirror. This was not implemented. Combined with #30, E2E suites may pass on clusters where tag data was seeded by other means or auth is relaxed.

**Risk:** Regression of tag-push wiring merges undetected.

**Recommendation:** Add pytest that enables an OCP tag key via Koku Settings API, polls `GET /internal/tags/status`, asserts fresh `synced_at`.

**Effort:** 0.5 day  
**SaaS vs on-prem:** On-prem CI focus.

---

### 36. Smoke perf status endpoint lacks best-of-2 retry

**Severity:** Low  
**Dimension:** Operational robustness  
**Location:** `tests/suites/ros/test_smoke_perf.py:69-94`

**Description:** v2 #28 added best-of-2 retry for container list latency only. Status endpoint uses single-attempt 2s threshold. Under loaded CI clusters, asymmetric flake risk remains.

**Risk:** Intermittent CI failures on status check while container list passes.

**Recommendation:** Apply same `_best_elapsed_seconds` pattern to status test or share a helper.

**Effort:** 30 minutes  
**SaaS vs on-prem:** CI only.

---

## Areas Reviewed Outside v2 Scope (No Additional Issues)

| Area | Assessment |
|------|------------|
| Worker-pool recalc (#6) | Still correct; `ctx.Err()` checks and channel buffer pattern unchanged |
| koku-ui H-3 / useEffect (#7, #8) | No regressions in `optimizationsLink.tsx` or `optimizationsBreakdownChart.tsx` |
| Slim namespace list DTO | Opt-in via `term`/`engine`; backward compat preserved |
| MIG GPU `provider_map` (koku `7a5819c15`) | Distinct `gpu_uuid` count; unit tests present |
| nise / costmgmt-api-cheatsheet | No new drift detected in v3 scope |
| Masu NP unintended access (#24 regression check) | Ingress restricted to four in-namespace components on masu port only |

## Priority Remediation Order

| Priority | Finding | Effort | Blocks merge? |
|----------|---------|--------|---------------|
| 1 | #30 SA token mount on Koku workers | 2–4 hours | **Yes** (on-prem) |
| 2 | #31 Update koku integration doc | 1–2 hours | No |
| 3 | #35 Tag-push E2E | 0.5 day | Recommended |
| 4 | #33 Heavy timeout vs SaaS ingress | 2–4 hours | SaaS only |
| 5 | #32 / #34 / #36 Doc and test polish | 1–2 hours | No |

## Overall Assessment

Phase 13 remediation **substantially improved** code quality: goroutine pile-up, UI performance footguns, statement-timeout infrastructure, and Helm security defaults are materially better than v1. v2 fixes #24, #27, and most of #25–#28 are **verified complete** in code review and Helm template inspection.

**The branch is not ready for upstream merge to on-prem** until **#30** is resolved. Wiring `ROS_TAGS_SOURCE=api` without mounting a service-account token on Koku workers creates a worse failure mode than v1's silent tag disable: Celery tasks now run, fail on auth, and may retry — while operators believe tag push is enabled. This was not caught in v2 because commit messages and Helm lint tests validated env vars and NetworkPolicy labels but not token availability.

**Residual risk after #30 fix:** Run on-prem E2E confirming (a) `org_tag_sync_metadata` populated after Settings tag enable, (b) GPU savings non-zero with masu NP enabled, (c) savings recalc succeeds after cost model update. Split koku bundle into separate PRs (v1 #15). Reconcile SaaS heavy-query timeout with ingress budget (#33).

**Positive signal:** No new SQL injection, SSRF bypass, or race-condition regressions were found in the v2 fix commits. If #30 is addressed promptly, the branch is close to merge-ready for on-prem.

---

## Remediation Commits Reviewed (v3 scope, 2026-06-15)

| Repo | Commits (post-v2) |
|------|-------------------|
| ros-ocp-backend | `8191d3fb`, `9c32cf01`, `9981f95c`, `7bf8bbba` |
| cost-onprem-chart | `32cabac`, `6dfc889`, `907166d`, `03473aa`, `c9eaf21` |
| koku | (no new adversarial commits since v2; `7a5819c15` MIG GPU reviewed) |
| koku-ui | (no changes since v2) |
| nise / costmgmt-api-cheatsheet | (no changes since v2) |
