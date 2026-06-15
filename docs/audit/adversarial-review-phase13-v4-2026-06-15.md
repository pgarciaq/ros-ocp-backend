# Adversarial Due Diligence Review — Phase 13

## Version & Date
Version: 4.0 | Date: 2026-06-15 | Reviewer: AI-assisted  
Previous: Version 3.0 (2026-06-15), Version 2.0 (2026-06-15), Version 1.0 (2026-06-14)

## Executive Summary

This incremental review (v4) examined post-v3 remediation commits (`87311a0`, `66ad69e05`, `a31af52b`, `56963e17`, `19285c2`) across six repositories on branch `pgarciaq-rosocp-superpowers-phase13`. The focus was whether v3 fixes for findings #30–#36 introduced regressions, whether those fixes are complete, and whether any gaps remain after three prior review rounds.

**The v3 remediation is technically sound and does not introduce code regressions** in the areas scrutinized. Finding #30 (projected SA token on Koku workers) is correctly implemented: the volume and `ROS_SA_TOKEN_PATH` env var are conditional on `tagsSource=api`, shared across all Celery worker deployments via `cost-onprem.koku.volumes` / `volumeMounts`, and Koku's `_read_bearer_token()` prefers `ROS_SA_TOKEN_PATH`. Helm template rendering confirms `audience: ros-api`, mount path `/var/run/secrets/ros/token`, and conditional omission when `tagsSource=db`. The debouncer lifecycle generation guard (`56963e17`) correctly prevents stale shutdown goroutines from calling `ShutdownSynthManifestDebouncers` on superseded contexts; atomic usage is sound with no ABA risk. The Helm lint fix (`19285c2`) correctly isolates the default-endpoint assertion from `OFFLINE_MOCK_VALUES`.

**Diminishing returns are evident.** v4 found no new SQL injection, SSRF bypass, goroutine pile-up, or NetworkPolicy label regressions. Residual issues are documentation drift, missing operational prerequisites (TokenReview RBAC), and an unimplemented tag-push E2E — not core logic bugs.

**Recommendation:** The branch is **close to merge-ready for on-prem** pending a live-cluster validation that tag push works end-to-end. Run the skipped tag-push E2E skeleton on a cluster with NetworkPolicy enforcement before upstream merge.

## Scorecard

| Dimension | Rating (1-5 stars) | Key gap |
|-----------|-------------------|---------|
| Security | ★★★★★ | SA token mount complete; chart grants ROS `system:auth-delegator` RBAC |
| Correctness | ★★★★☆ | Tag-push path code-complete; no live E2E proof yet |
| Auditability | ★★★★☆ | Internal endpoint metrics and timeout cancellation counter present |
| Operational robustness | ★★★★★ | Projected token auto-refresh; TokenReview RBAC chart-managed |
| Performance | ★★★★☆ | Heavy timeout configurable; on-prem default 45s acceptable |
| Design quality | ★★★★☆ | Conditional Helm helpers; transaction-scoped `SET LOCAL` pattern correct |
| Maintainability | ★★★★☆ | Tag-push E2E still skipped; docs synced to `api` default |
| Governance | ★★★★☆ | ROS docs-site synced; tag-push E2E skeleton remains |

## Previous Findings Verification

### v3 Findings (#30–#36)

| # | Title | Status | Notes |
|---|-------|--------|-------|
| 30 | Koku Celery workers SA token mount | ✅ Verified | Projected `ros-sa-token` volume conditional on `tagsSource=api` (`_helpers-koku.tpl:388-397`, `447-484`). `ROS_SA_TOKEN_PATH=/var/run/secrets/ros/token` set in `commonEnv`. `expirationSeconds: 3600` with `audience: ros-api` — Kubernetes projected volumes auto-refresh before expiry. Mount failure prevents pod start (fail-fast). Koku `_read_bearer_token()` checks `ROS_SA_TOKEN_PATH` env first (`ros_tag_sync.py:58-62`). `settings.py:712-715` aliases `ROS_SA_TOKEN_PATH` → `ROS_TAGS_SA_TOKEN_PATH`. Helm lint asserts mount (`test_chart_lint.py:325-337`). **Residual:** chart does not grant ROS SA TokenReview RBAC — see **#38**. |
| 31 | Koku `ros-ocp-integration.md` update | ✅ Verified | On-prem default documented as `api` with mermaid diagram (`ros-ocp-integration.md:255-303`). Cross-links to `cost-onprem/values.yaml`. **Residual:** ros-ocp-backend companion docs not updated — see **#37**. |
| 32 | `configuration.md` prose `max_connections` | ✅ Verified | Line 690 now states `max_connections=200` with link to `database-tuning.md`. Example block at line 492 also shows `200`. |
| 33 | Configurable heavy API timeout | ✅ Verified | `HeavyAPIStatementTimeoutMS()` reads `ROS_HEAVY_API_STATEMENT_TIMEOUT_MS` with `> 0` guard, default 45000 (`statement_timeout.go:69-74`). Unit test `TestHeavyAPIStatementTimeoutMSFromConfig` passes. Documented in `query-performance.md` with SaaS ~28000 guidance. **Note:** invalid/zero/negative values silently fall back to 45000 — see **#40**. Helm chart does not expose the env var (SaaS uses app-interface; on-prem 45s is acceptable). |
| 34 | CSV path stripping in allowlist | ✅ Verified | `regexReplaceAll "/.*$"` after scheme/port strip (`_helpers-ros.tpl:17`). Helm test `test_ros_processor_strips_path_from_csv_allowed_hosts_endpoint` confirms `https://s3.example.com/bucket` → `s3.example.com`. `csv_security.go` compares `u.Hostname()` only — aligned. **Edge cases** (`user:pass@host`, bracketed IPv6) unhandled — see **#41**. |
| 35 | Tag push E2E skeleton | ⚠️ Partial | `test_tag_push_e2e.py` exists with `@pytest.mark.extended` and unconditional `pytest.skip()` — correct skeleton, not a regression. No live-cluster validation yet. |
| 36 | Smoke perf status retry | ✅ Verified | Shared `_measure_best_elapsed()` helper used by both container list and status tests (`test_smoke_perf.py:30-43`, `63-70`, `93-99`). Best-of-2 pattern consistent. |

### v2 Findings (#23–#29) — Spot check

| # | Title | Status | Notes |
|---|-------|--------|-------|
| 23 | Tag push wiring + NP | ✅ Verified | `ROS_TAGS_ENABLED=true` / `ROS_TAGS_SOURCE=api` in `commonEnv` when `tagsSource=api`. `ros-api-access` allows `cost-worker` (`networkpolicies.yaml:159-163`). All worker deployments share `app.kubernetes.io/component: cost-worker` label (`_helpers-koku.tpl:219-232`). Completes with #30 SA token mount. |
| 24 | Masu NP + ros-api | ✅ Verified | `masu-access` includes `ros-api` component (`masu-networkpolicy.yaml:35-38`). Helm lint test present. |
| 27 | Internal auth fatal | ✅ Verified | `ValidateSecurityConfig()` errors on `!Development && !InternalTagsAuthRequired` (`security.go:28-35`). Unit tests in `security_test.go:31-41`. |
| 26 | Per-endpoint statement timeouts | ✅ Verified | `WithHeavyStatementTimeout` wired to savings-summary (`handlers_savings_summary.go:79`) and fleet-wide list (`recommendation_set_native.go:407`). Transaction-scoped `SET LOCAL` with deferred rollback. |

### v1 Findings (#1–#22) — Spot check

| # | Title | Status | Notes |
|---|-------|--------|-------|
| 1 | On-prem tag filtering (`tagsSource: db`) | ✅ Verified | Chart default `tagsSource: api` (`values.yaml:141`). Koku push env wired. End-to-end depends on #30 + TokenReview RBAC (#38). |
| 2 | Missing `ROS_CSV_ALLOWED_HOSTS` | ✅ Verified | Auto-derived from `objectStorage.endpoint` with scheme/port/path normalization. |
| 6 | Recalc O(clusters) goroutines | ✅ Verified | Worker-pool with `ctx.Err()` checks unchanged (`threshold_recalculate.go:155-170`). |
| 7 | H-3 `optimizationsLink` | ✅ Verified | Uses `useRosCount` hook (`optimizationsLink.tsx:2,45-54`). |
| 8 | `useMemo` side effect | ✅ Verified | No regression in `optimizationsBreakdownChart.tsx`. |

### Additional v4 checks (requested scope)

| Area | Status | Notes |
|------|--------|-------|
| Debouncer lifecycle generation guard (`56963e17`) | ✅ Verified | `debouncerLifecycleGen atomic.Uint64` incremented on each `InitSynthManifestDebouncer`; stale goroutines exit early (`manifest_recommendation_debouncer.go:66-75`). Per-entry `generation` guard in `fireSynthManifestRecommendations` unchanged. No ABA problem — monotonic counter, not pointer reuse. Existing tests cover shutdown skip and no-double-fire. No dedicated stale-goroutine test — see **#42**. |
| Helm lint `OFFLINE_MOCK_VALUES` fix (`19285c2`) | ✅ Verified | `test_ros_processor_sets_csv_allowed_hosts` filters out `objectStorage.endpoint` override to assert `values.yaml` default `s3.openshift-storage.svc.cluster.local`. Other normalization tests still use full `OFFLINE_MOCK_VALUES`. |

## New Findings

### 37. ros-ocp-backend docs still cite on-prem `ROS_TAGS_SOURCE=db` as default

**Severity:** Medium  
**Dimension:** Governance, Maintainability  
**Location:**
- `docs/operations/tag-sync.md:3-6`
- `docs/features/tag-filtering.md:461`
- `docs-site/configuration.md:464,477,487`
- `docs-site/testing/validating-native-engine.md:437-438,646`

**Description:** v3 #31 fixed `koku/docs/architecture/ros-ocp-integration.md` but companion ROS documentation still states on-prem default is `ROS_TAGS_SOURCE=db` with direct PostgreSQL reads. The cost-onprem chart defaults `tagsSource: api` (`values.yaml:141`) and wires Koku push env vars. Operators reading ROS docs (not koku docs) will debug the wrong data path.

**Risk:** Misconfiguration, wasted debugging time, contradictory runbooks across repos.

**Recommendation:** Update `tag-sync.md`, `tag-filtering.md`, and docs-site pages per `docs-site-sync.mdc` rule. State on-prem default is `api`; retain `db` as advanced shared-PostgreSQL option.

**Effort:** 1–2 hours  
**SaaS vs on-prem:** Documentation only.

**Resolution (2026-06-15):** Updated `docs/operations/tag-sync.md`, `docs/features/tag-filtering.md`,
`docs/operations/configuration.md`, `docs/testing/validating-native-engine.md`, and synced
`docs-site/configuration.md` and `docs-site/testing/validating-native-engine.md` to document
`api` as the on-prem chart default (`tagsSource: api`) with `db` retained as an advanced
shared-PostgreSQL option.

---

### 38. Chart does not grant ROS ServiceAccount TokenReview RBAC

**Severity:** Medium (on-prem)  
**Dimension:** Security, Operational robustness  
**Location:**
- `cost-onprem/templates/ros/serviceaccount.yaml` (SA only, no Role/ClusterRoleBinding)
- `internal/tags/auth.go:72-122` (TokenReview API call)
- `docs/operations/tag-sync-auth.md:70-71`

**Description:** Finding #30 mounts a bearer token on Koku workers, enabling them to **send** authenticated requests. ROS must **validate** tokens via the Kubernetes TokenReview API, which requires the ROS ServiceAccount (`ros-backend`) to hold the `system:auth-delegator` ClusterRole (or equivalent `create` on `tokenreviews.authentication.k8s.io`). The chart creates the ROS ServiceAccount but no ClusterRoleBinding. Without cluster-admin out-of-band configuration, ROS returns auth errors and tag push / savings recalc receive 401.

**Risk:** Tag push appears fully wired in Helm lint tests but fails at runtime on fresh clusters until an operator manually binds `system:auth-delegator`. This was not caught because Helm tests validate env vars and volume mounts, not RBAC permissions.

**Recommendation:**
1. Add `ClusterRoleBinding` for `ros.serviceAccount.name` → `system:auth-delegator` (gated on `ros.internalAuth.enabled`).
2. Add Helm lint test rendering the binding when internal auth is enabled.
3. Document in `tag-sync-auth.md` troubleshooting section.

**Effort:** 2–3 hours  
**SaaS vs on-prem:** SaaS platform config likely handles this via app-interface; on-prem chart gap.

**Resolution (2026-06-15):** Added `cost-onprem/templates/ros/clusterrolebinding-auth-delegator.yaml`
binding `ros-backend` to `system:auth-delegator` when `ros.internalAuth.enabled=true` (default).
Helm lint test `test_ros_auth_delegator_clusterrolebinding_when_internal_auth_enabled` asserts
rendering. Documented troubleshooting in `docs/operations/tag-sync-auth.md`.

---

### 39. No koku unit test for `ROS_SA_TOKEN_PATH` env precedence

**Severity:** Low  
**Dimension:** Maintainability  
**Location:** `koku/masu/processor/ros_tag_sync.py:58-62`, `koku/masu/test/processor/test_ros_tag_sync.py`

**Description:** v3 #30 added `os.environ.get("ROS_SA_TOKEN_PATH")` as the first token path lookup. Existing tests use `ROS_TAGS_DEV_TOKEN` override only. No test verifies the env var is read from the projected mount path or that `ROS_TAGS_SA_TOKEN_PATH` settings fallback still works.

**Risk:** Future refactor could break env precedence without test detection.

**Recommendation:** Add test with `patch.dict(os.environ, {"ROS_SA_TOKEN_PATH": "/tmp/test-token"})` and mocked file read.

**Effort:** 30 minutes  
**SaaS vs on-prem:** Both.

**Resolution (2026-06-15):** Added `RosBearerTokenPathTest.test_read_bearer_token_prefers_ros_sa_token_path_env`
in `koku/masu/test/processor/test_ros_tag_sync.py` — patches `ROS_SA_TOKEN_PATH`, writes a temp
token file, and asserts `_read_bearer_token()` returns its content ahead of the default K8s path.

---

### 40. `ROS_HEAVY_API_STATEMENT_TIMEOUT_MS` invalid values silently default

**Severity:** Low  
**Dimension:** Operational robustness  
**Location:** `internal/db/statement_timeout.go:69-74`

**Description:** `HeavyAPIStatementTimeoutMS()` uses `cfg.DBHeavyAPIStatementTimeoutMS > 0` guard. Zero, negative, or non-numeric (viper parse failure → 0) values silently fall back to 45000ms with no startup warning.

**Risk:** Operator typo (`ROS_HEAVY_API_STATEMENT_TIMEOUT_MS=0` intending to disable) gets unexpected 45s timeout. Low impact — worst case is longer-than-intended query window.

**Recommendation:** Log warning in `ConfigValidationWarnings` when env is set but parsed value ≤ 0. Optional startup validation.

**Effort:** 30 minutes  
**SaaS vs on-prem:** Both.

**Resolution (2026-06-15):** `HeavyAPIStatementTimeoutMS()` now logs a one-shot warning when
`ROS_HEAVY_API_STATEMENT_TIMEOUT_MS` is set but parses to ≤ 0, then falls back to 45000ms.
Unit test `TestHeavyAPIStatementTimeoutMSInvalidValuesUseDefaultAndWarn` covers zero and
negative values.

---

### 41. CSV allowlist normalization edge cases (auth prefix, IPv6)

**Severity:** Low  
**Dimension:** Operational robustness  
**Location:** `cost-onprem/templates/ros/_helpers-ros.tpl:13-17`

**Description:** Normalization handles `https://host:port/path` correctly. Unhandled formats:
- `https://user:pass@host` → `user:pass@host` (not a valid hostname for `u.Hostname()` matching)
- `[::1]:443` → port strip regex may not match bracketed IPv6 literals

These are uncommon misconfigurations; `values.yaml` documents hostname-only format.

**Risk:** Processor CSV fetch blocked if operator sets non-standard endpoint format.

**Recommendation:** Document unsupported formats explicitly. Optional: strip `user:pass@` prefix and handle bracketed IPv6 in Helm helper.

**Effort:** 1 hour  
**SaaS vs on-prem:** On-prem chart operators.

**Resolution (2026-06-15):** `cost-onprem.ros.csvAllowedHosts` helper now strips `user:pass@`
auth prefixes before hostname extraction; IPv6 bracket limitation documented in-template.
Helm lint test `test_ros_processor_strips_auth_prefix_from_csv_allowed_hosts_endpoint` verifies
`https://user:pass@s3.example.com:9000/bucket` → `s3.example.com`.

---

### 42. Debouncer lifecycle generation guard lacks dedicated regression test

**Severity:** Low  
**Dimension:** Maintainability  
**Location:** `internal/services/manifest_recommendation_debouncer.go:66-75`, `manifest_recommendation_debouncer_test.go`

**Description:** Commit `56963e17` added `debouncerLifecycleGen` to prevent stale shutdown goroutines from prior `InitSynthManifestDebouncer` cycles. Existing tests cover shutdown skip and no-double-fire but not the specific stale-goroutine scenario (init → shutdown → re-init → old goroutine must not fire).

**Risk:** Future refactor could reintroduce the race without test detection.

**Recommendation:** Add test: `InitSynthManifestDebouncer(ctx1)` → cancel ctx1 → `InitSynthManifestDebouncer(ctx2)` → verify only ctx2's shutdown triggers `ShutdownSynthManifestDebouncers`.

**Effort:** 1 hour  
**SaaS vs on-prem:** Both.

**Resolution (2026-06-15):** Added `TestInitSynthManifestDebouncer_StaleShutdownGoroutineIgnored`
— init with ctx1, re-init with ctx2, cancel ctx1, verify debounced recommendations still fire
(proving stale shutdown goroutine did not call `ShutdownSynthManifestDebouncers`).

---

## Areas Reviewed Outside v3 Scope (No Additional Issues)

| Area | Assessment |
|------|------------|
| Worker-pool recalc (#6) | Still correct; no changes since v2 |
| Masu NP unintended access (#24) | Ingress restricted to four in-namespace components on masu port only |
| CSV SSRF (`csv_security.go`) | Allowlist + private-network deny unchanged; no bypass |
| Internal auth fatal (#27) | No regression; chart sets `internalAuth.enabled: true` |
| Slim namespace list DTO | Opt-in via `term`/`engine`; backward compat preserved |
| MIG GPU `provider_map` (koku) | Distinct `gpu_uuid` count; unit tests present |
| nise / costmgmt-api-cheatsheet | No new drift in v4 scope |
| koku-ui H-3 / useEffect (#7, #8) | No regressions |

## Priority Remediation Order

| Priority | Finding | Effort | Blocks merge? |
|----------|---------|--------|---------------|
| 1 | Live-cluster tag-push E2E (#35 skeleton) | 0.5 day | Recommended |
| 2 | #38 ROS TokenReview RBAC in chart | 2–3 hours | Recommended (on-prem) |
| 3 | #37 ROS docs-site sync | 1–2 hours | No |
| 4 | #39–#42 polish | 2–3 hours | No |

## Overall Assessment

After four adversarial review rounds, Phase 13 remediation has reached a **high confidence level** for core Go/SQL, koku-ui, and Helm security defaults. v3 fixes #30–#36 are **verified complete in code** with no regressions introduced by the remediation commits. The debouncer lifecycle guard and Helm lint endpoint fix are correct incremental improvements.

**The branch is suitable for upstream merge to on-prem** subject to:
1. Live-cluster E2E confirming tag push populates `org_tag_sync_metadata` after Settings tag enable (implement #35 skeleton).
2. Verifying ROS TokenReview works on a fresh chart install (#38 — may require manual `system:auth-delegator` binding today).
3. Syncing stale ROS documentation (#37) before operator-facing release notes.

**Residual risk is low and operational**, not architectural. Diminishing returns are expected — v4 found documentation drift and missing RBAC wiring, not new correctness bugs in the data pipeline. Split koku bundle into separate PRs (v1 #15) remains recommended at upstream merge time.

**Positive signal:** Zero new SQL injection, SSRF bypass, race-condition, or NetworkPolicy regressions across four review rounds. If #38 is addressed and tag-push E2E passes on a live cluster, residual risk is acceptable for production on-prem deployment.

---

## Remediation Commits Reviewed (v4 scope, 2026-06-15)

| Repo | Commits (post-v3) |
|------|-------------------|
| ros-ocp-backend | `a31af52b` (#33), `56963e17` (debouncer guard) |
| cost-onprem-chart | `87311a0` (#30,#32,#34-#36), `19285c2` (helm lint fix) |
| koku | `66ad69e05` (#30-#31) |
| koku-ui | (no changes since v2) |
| nise / costmgmt-api-cheatsheet | (no changes since v2) |
