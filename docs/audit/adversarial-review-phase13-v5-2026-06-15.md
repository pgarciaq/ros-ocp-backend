# Adversarial Due Diligence Review — Phase 13

## Version & Date
Version: 5.0 | Date: 2026-06-15 | Reviewer: AI-assisted  
Previous: Version 4.0 (2026-06-15), Version 3.0 (2026-06-15), Version 2.0 (2026-06-15), Version 1.0 (2026-06-14)

## Executive Summary

This incremental review (v5) examined post-v4 remediation commits (`5a310921`, `fadfd58`, `3d6a128b1`) across six repositories on branch `pgarciaq-rosocp-superpowers-phase13`. The focus was whether v4 fixes for findings #37–#42 introduced regressions, whether those fixes are complete, and whether any gaps remain after four prior review rounds.

**The v4 remediation is technically sound and does not introduce code regressions.** Finding #38 (ClusterRoleBinding for TokenReview) renders correct YAML, is gated on `ros.internalAuth.enabled`, uses standard chart helpers, and binds `ros-backend` in the release namespace to `system:auth-delegator` — verified with `helm template` (0 bindings when disabled, 1 when enabled). Finding #40 (heavy timeout warning) uses `sync.Once` for one-shot logging; unit tests pass for zero and negative values. Finding #41 (CSV auth-prefix stripping) correctly normalizes `https://user:pass@s3.example.com:9000/bucket` → `s3.example.com`. Finding #42 (debouncer stale-goroutine test) passes and correctly proves ctx1 cancellation does not prevent ctx2 debounced runs. Finding #39 (Koku `ROS_SA_TOKEN_PATH` test) exercises the right code path with temp-file cleanup.

**Finding #37 (docs sync) is partially complete.** Primary operator pages (`tag-sync.md`, `tag-filtering.md`, `configuration.md`, `validating-native-engine.md`, and their docs-site mirrors) now state `api` as the on-prem chart default. Three customer-facing pages were not updated and still describe on-prem default as `db` — see **#43**. This is documentation drift only, not a runtime bug.

**Diminishing returns are confirmed.** v5 found no new SQL injection, SSRF bypass, goroutine race, NetworkPolicy regression, or Helm wiring defect. Residual items are minor docs-site sync gaps (#43) and the unimplemented tag-push live E2E (#35 skeleton from v3). The review/fix cycle has converged.

**Recommendation:** The branch is **merge-ready for on-prem** subject to a one-time live-cluster tag-push validation (#35) and optional docs-site sync for #43 before operator-facing release notes.

## Scorecard

| Dimension | Rating (1-5 stars) | Key gap |
|-----------|-------------------|---------|
| Security | ★★★★★ | TokenReview RBAC chart-managed; SA token mount complete |
| Correctness | ★★★★☆ | Tag-push path code-complete; no live E2E proof (#35) |
| Auditability | ★★★★☆ | Internal endpoint metrics; timeout cancellation counter |
| Operational robustness | ★★★★★ | Projected token auto-refresh; invalid heavy-timeout warning |
| Performance | ★★★★☆ | Heavy timeout configurable; on-prem 45s acceptable |
| Design quality | ★★★★☆ | Conditional Helm helpers; debouncer lifecycle guard sound |
| Maintainability | ★★★★★ | v4 polish tests added (#39, #40, #42); Helm lint for #38/#41 |
| Governance | ★★★★☆ | Three docs-site pages still cite `db` on-prem default (#43) |

## Previous Findings Verification (v4 #37-#42)

| # | Title | Status | Notes |
|---|-------|--------|-------|
| 37 | ROS docs cite on-prem `db` as default | ⚠️ Partial | **Updated:** `tag-sync.md`, `tag-filtering.md`, `operations/configuration.md`, `validating-native-engine.md`, `docs-site/configuration.md`, `docs-site/testing/validating-native-engine.md`, and `tag-sync-auth.md` variable table + TokenReview troubleshooting. **Still stale:** `docs-site/architecture/cost-integration.md:67`, `docs-site/plugin-reference/query-parameters.md:146`, `docs/architecture/cost-integration.md:67`, and `tag-sync-auth.md:7-30` (opens with `db` mode as primary on-prem narrative). Koku/binary default columns correctly show `db` when env unset; chart default columns show `api`. See **#43**. |
| 38 | Chart TokenReview RBAC | ✅ Verified | `clusterrolebinding-auth-delegator.yaml` conditional on `ros.internalAuth.enabled` (default `true`). Uses `cost-onprem.fullname`, `cost-onprem.labels`, `ros.serviceAccount.name`, `Release.Namespace`. No conflict with kruize binding (distinct name suffix). `helm template --set ros.internalAuth.enabled=false` renders 0 bindings; `true` renders 1. Helm lint `test_ros_auth_delegator_clusterrolebinding_when_internal_auth_enabled` asserts binding + `system:auth-delegator` + `ros-backend`. **No negative lint test** when `internalAuth.enabled=false` — low risk. |
| 39 | Koku `ROS_SA_TOKEN_PATH` test | ✅ Verified | `RosBearerTokenPathTest.test_read_bearer_token_prefers_ros_sa_token_path_env` patches `ROS_SA_TOKEN_PATH`, writes temp token, asserts `_read_bearer_token()` returns content. Cleanup via `finally: os.unlink(token_path)`. Precedence order in `ros_tag_sync.py:58-62` is `ROS_SA_TOKEN_PATH` → `ROS_TAGS_SA_TOKEN_PATH` → default K8s path. Does not test `ROS_TAGS_DEV_TOKEN` precedence (dev token wins by design). |
| 40 | Heavy timeout invalid-value warning | ✅ Verified | `HeavyAPIStatementTimeoutMS()` uses `sync.Once` + `warnHeavyAPIStatementTimeoutFallback()` when env is set but parsed value ≤ 0. Test `TestHeavyAPIStatementTimeoutMSInvalidValuesUseDefaultAndWarn` covers zero and negative via injectable warn hook. One-shot: repeated calls do not re-warn. Non-numeric values (viper → 0) would also warn because hook checks raw `os.Getenv`. |
| 41 | CSV allowlist auth-prefix stripping | ✅ Verified | `_helpers-ros.tpl:17` strips `user:pass@` via `regexReplaceAll "^[^@]*@"`. IPv6 bracket limitation documented in-template (`:18-19`). Helm lint `test_ros_processor_strips_auth_prefix_from_csv_allowed_hosts_endpoint` passes. **Known edge case:** `@` in password (`user:pa@ss@host`) strips at first `@` — uncommon for object-storage endpoints; documented in v4. |
| 42 | Debouncer stale-goroutine test | ✅ Verified | `TestInitSynthManifestDebouncer_StaleShutdownGoroutineIgnored`: init ctx1 → init ctx2 → cancel ctx1 → defer recommendations → `runCount == 1` within 3s. Proves stale shutdown goroutine did not call `ShutdownSynthManifestDebouncers`. Test passes (`go test ./internal/services/... -run StaleShutdown`). Uses integration DB (existing pattern); `Eventually` tolerates timing variance. |

### v3 Finding #35 (carry-forward, not v4 scope)

| # | Title | Status | Notes |
|---|-------|--------|-------|
| 35 | Tag push E2E skeleton | ⚠️ Partial | `test_tag_push_e2e.py` still unconditionally `pytest.skip()`. No regression from v4 fixes; live-cluster validation remains the recommended pre-merge check. |

## New Findings

### 43. Residual docs-site pages still cite on-prem `db` as default (incomplete #37 sync)

**Severity:** Low  
**Dimension:** Governance, Maintainability  
**Location:**
- `docs-site/architecture/cost-integration.md:65-68`
- `docs-site/plugin-reference/query-parameters.md:144-147`
- `docs/architecture/cost-integration.md:65-68` (internal; should sync per `docs-site-sync.mdc`)
- `docs/operations/tag-sync-auth.md:7-30` (section ordering still leads with `db` mode narrative)

**Description:** v4 #37 updated the primary configuration and tag-sync pages but did not sync the cost-integration architecture page or query-parameters plugin reference. Both docs-site pages still state **On-prem (default) | `db`**. Operators following these cross-linked pages will believe direct PostgreSQL JOIN is the default path, contradicting `cost-onprem/values.yaml:141` (`tagsSource: api`) and the updated `docs-site/configuration.md`.

**Risk:** Misconfiguration debugging time; contradictory runbooks. No runtime impact when chart defaults are used.

**Recommendation:** Update the two docs-site pages (and internal `docs/architecture/cost-integration.md`) to match `configuration.md`: on-prem chart default is `api`; `db` is advanced shared-PostgreSQL. Reorder `tag-sync-auth.md` to lead with `api` mode for chart-default deployments.

**Effort:** 30–60 minutes  
**SaaS vs on-prem:** Documentation only.

---

No additional new findings (#44+). Areas reviewed outside v4 fix scope showed no regressions:

| Area | Assessment |
|------|------------|
| Debouncer lifecycle guard (`56963e17`) | Unchanged since v4; no regression |
| Koku SA token mount (#30) | Unchanged; Helm lint assertions present |
| Masu/ros-api NetworkPolicy (#24) | Unchanged |
| CSV SSRF (`csv_security.go`) | Unchanged |
| koku-ui H-3 / useEffect (#7, #8) | No changes since v2 |
| nise / costmgmt-api-cheatsheet | Aligned (`ROS_TAGS_SOURCE` default `api` on chart) |
| MIG GPU provider_map (koku) | Unchanged since v1 |

## Convergence Assessment

| Round | New findings | Severity mix | Theme |
|-------|-------------|--------------|-------|
| v1 | 22 | High/Medium — broken defaults, missing env, NP gaps | Core wiring broken |
| v2 | 7 (#23–#29) | Medium — tag push not wired, missing tests | Integration gaps |
| v3 | 7 (#30–#36) | Medium/Low — SA token mount, docs, timeouts | Operational completeness |
| v4 | 6 (#37–#42) | Medium/Low — docs drift, RBAC, test polish | Documentation + RBAC |
| v5 | 1 (#43) | Low — residual docs-site sync | Documentation only |

The review/fix cycle has **converged**. Finding count dropped from 22 → 1 over five rounds. Severity progressed from architectural correctness bugs to documentation sync gaps. v4 code fixes (#38–#42) are verified complete with no regressions introduced. v5's sole new finding is a partial-completion artifact of #37, not a new defect class.

**Expected outcome at this stage:** Zero or near-zero new code findings. v5 confirms that expectation.

## Overall Assessment

After five adversarial review rounds, Phase 13 remediation has reached **high confidence for merge**. v4 fixes #37–#42 are verified in code (#38–#42 complete; #37 substantially complete with #43 residual). No regressions were introduced by remediation commits `5a310921`, `fadfd58`, or `3d6a128b1`.

**The branch is suitable for upstream merge to on-prem** subject to:

1. **Recommended:** Run live-cluster tag-push E2E (implement #35 skeleton) to confirm `org_tag_sync_metadata` populates after Settings tag enable.
2. **Optional before release notes:** Sync #43 docs-site pages.
3. **At upstream merge:** Split koku bundle into separate PRs (v1 #15) remains recommended.

**Residual risk is low and operational**, not architectural. If tag-push E2E passes on a live cluster, residual risk is acceptable for production on-prem deployment.

**Positive signal:** Zero new correctness, security, or concurrency defects in v5. Four consecutive rounds (v2–v5) found no SQL injection, SSRF bypass, race-condition, or NetworkPolicy regressions in touched code.

---

## Remediation Commits Reviewed (v5 scope, 2026-06-15)

| Repo | Commits (post-v4) |
|------|-------------------|
| ros-ocp-backend | `5a310921` (#37 partial, #40, #42) |
| cost-onprem-chart | `fadfd58` (#38, #41) |
| koku | `3d6a128b1` (#39) |
| koku-ui | (no changes since v2) |
| nise / costmgmt-api-cheatsheet | (no changes since v2) |
