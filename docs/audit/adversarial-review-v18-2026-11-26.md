# Adversarial review v18 — v17 remediation verification + #947 union-semantics review

**Date:** 2026-11-26  
**Reviewer:** AI-assisted (session agent); locally executed probes, not a security certification.  
**Verdict:** **PASS with one Low and four Informational findings.** No P1/P2 demonstrated. The branch is sign-off-ready; V18-01 is the only code change recommended, and it shares a two-line patch with V18-03.  
**Findings:** 1 Low, 4 Informational.  
**Scope:** verification of the four V17 findings' remediations, plus adversarial review of the work since the V17 HEAD (`51cd1485` #947 union semantics; `c4cc36ca` the #949 runbook section).

## 1. Scope and exact revisions

| Repository | V17 reviewed HEAD | V18 reviewed HEAD | Delta |
|---|---|---|---|
| ros-ocp-backend | `1fdc648a` | `c4cc36ca` | 2 commits: `51cd1485` (#947 union semantics, 7 files +277/−36), `c4cc36ca` (runbook section, docs only) |
| iqe-ros-ocp-plugin | `e9d142e`-era | `ba7eafd` | `test_cluster_alias_multi_match.py` (392 lines, the #949 pins) |
| koku-metrics-operator | `27b82673` | unchanged | — |
| nise | `5beda91` | unchanged | — |

Review method: read the prior review and verify its fixes at HEAD; read the full changed files (not only the hunks) plus every caller of the new helpers; executed probes where the claim was testable (an uppercase-UUID filter probe against the in-repo test DB, mutation substitutions of each fix leg, the full gates); code-read where a probe would need per-table fixtures. Risk-based, not exhaustive line coverage.

## 2. V17 remediation verification — all four resolved

| V17 finding | Issue | Fix verified at HEAD | Evidence |
|---|---|---|---|
| V17-01 `MinInt64` recursion | #945 | ✅ `internal/costdata/conversion.go:91` guards before negation; the doc comment records it | `conversion_overflow_regression_test.go:53` (`TestConvertCentsPPB_MinInt64`); `go test ./internal/costdata` green |
| V17-02 dead float path | #946 | ✅ `money.ConvertAmount` is gone from the tree (only stale copies under `.muse/worktrees/`, not built) | `grep -rn ConvertAmount --include=*.go .` → worktree copies only; `go test ./internal/money` green |
| V17-03 substring-alias semantics | #947 | ⚠️ The fix under review in this pass | §3 |
| V17-04 overstated doc bound | #948 | ✅ the comment now says the terms "overflow only when the true converted amount itself exceeds int64 — never where the result fits" | Code read, `conversion.go:76-92` |

**No regressions of the V17 fixes were found** in the #947 change (it touches none of the money paths; the #945 guard and the #948 comment are intact at `c4cc36ca`).

## 3. New findings (this pass)

### V18-01 — UUID cluster filters are case-sensitive in the new multi-match resolver

**Severity:** Low.  
**Dimension:** Correctness / cross-surface consistency.  
**Location:** `internal/api/cluster_filter_predicate.go:52-77` (`resolveClusterFilterUUIDs`), `:81-93` (`intersectClusterUUIDs`); the same exact-compare family in `handlers_gpu_timeslicing_detail.go:83-95`, `handlers_machinesets.go:113-131`, `handlers_vm_recs.go:307-321`.  
**Description:** for a UUID-shaped filter the resolver returns the **raw input string**, and `intersectClusterUUIDs` compares Go strings against `cluster_uuid::text` (canonical lowercase — `clusters.cluster_uuid` is a `uuid` column since migration `000041`). The SQL predicate binds the same value to a `uuid` comparison, which is case-insensitive. So the two implementations of "which clusters does this filter match" disagree on case, and the surfaces disagree with each other.  
**Executed proof** (temporary test against the in-repo test DB, since deleted): with one cluster (`bbbb…b001`) holding a node row and a TS row,

| Surface | `filter[cluster]=<lowercase uuid>` | `filter[cluster]=<UPPERCASE uuid>` |
|---|---|---|
| `/recommendations/openshift/nodes` | 200, count 1 | **200, count 1** (the SQL uuid bind) |
| `/recommendations/openshift/gpu/timeslicing` | 200, count 1 | **200, count 0** (the Go intersection) |

**Risk:** a direct API consumer echoing a UUID from another system (uppercase is valid on input everywhere else) gets a silently empty GPU list on TS/MIG while the nodes tab returns rows. Same-org only; no leak.  
**Recommendation:** normalize the UUID branch once — `if u, err := uuid.Parse(value); err == nil { return []string{u.String()} }` — in `resolveClusterFilterUUIDs` and `resolveClusterFilterUUID`. That also collapses the singular/plural duplication flagged below and fixes the currency key on the same patch. The detail/machineset/VM exact compares are #950's family; normalize there when #950 lands.  
**Effort:** S (two lines).

### V18-02 — "consistent across all surfaces" has no inventory; three list surfaces still exact-match, and the public docs still say UUID-only

**Severity:** Informational.  
**Dimension:** Design quality / public contract (docs).  
**Location:** `handlers_vm_recs.go:307-321`, `handlers_machinesets.go:113-131`, `handlers_savings_summary.go:226-245` and `:262-275`; `docs-site/features/gpu-mig.md:140`, `docs-site/features/quota-recommendations.md:226`, `docs-site/features/cluster-resource-quota.md:182`, `docs-site/plugin-reference/query-parameters.md:74`; the CHANGELOG bullet's head line.  
**Description:** #947 unified the GPU time-slicing and MIG **lists** with the five predicate surfaces. But `filter[cluster]` is compared **literally** to the allowed UUID list on three more list/summary surfaces, so a substring alias there returns 200 + empty:

| Surface | Code | Alias behavior |
|---|---|---|
| VM recommendations list | `handlers_vm_recs.go:307-321` | 200 + empty |
| Machine sets list | `handlers_machinesets.go:113-131` | 200 + empty |
| Savings summary `group_by[idle_state]` / `group_by[tag:*]` | `handlers_savings_summary.go:226-245`, `:262-275` | 200 + empty |
| GPU TS detail, fleet heatmap | already filed as [#950](https://github.com/pgarciaq/ros-ocp-backend/issues/950) | 404 / 200 + empty |

The CHANGELOG body is correctly scoped ("on the GPU time-slicing and MIG lists"), but its bold head line claims consistency "across all surfaces", and the VM/machineset/savings-summary surfaces are not in #950's table. The public docs also never state the union rule: `gpu-mig.md:140` says "Cluster UUID" (wrong twice over — aliases worked before #947 as first-match), and quota/cluster-quota say "Limit to one cluster", which the (pre-existing) predicate union already contradicts.

**Risk:** a user who reads the changelog and points a script at the alias contract gets 200 + empty on three surfaces with no error, and the next review inherits the same "is it unified yet?" question. No data impact; same-org only.  
**Recommendation:** (a) correct the three doc rows and add one union-semantics paragraph to `query-parameters.md` (the canonical table); (b) extend #950's inventory with the three exact-match list surfaces and decide unify-vs-document per surface there, rather than leaving them out of the tracker; (c) soften the CHANGELOG head to the body's scope.  
**Effort:** S (docs) + #950 scope.

### V18-03 — the new resolver swallows lookup failures into a false "no matches" 200

**Severity:** Informational.  
**Dimension:** Operational robustness.  
**Location:** `cluster_filter_predicate.go:62-76`.  
**Description:** `resolveClusterFilterUUIDs` returns `nil` on `pool.Query` failure and never checks `rows.Err()`. Callers (`handlers_node_recs.go:110-125`, `handlers_gpu_mig.go:69-76`) then return the #790 empty response — **HTTP 200, count 0** — for a request whose lookup failed, where the same handlers use 503 for a failed cluster resolution. A mid-iteration failure also produces a truncated union that looks like a legitimate row set. The sibling `resolveClusterFilterUUID` has the same shape (and predates #947), but this is new code.  
**Risk:** a transient DB error is reported to the user as "no matching data" rather than a retryable failure; no exposure, a false negative.  
**Recommendation:** return `(uuids []string, err error)`, treat `rows.Err()` as an error, and let the callers answer 503 (or log-and-keep the empty contract, explicitly). Same patch as V18-01.  
**Effort:** S.

### V18-04 — the first-match currency rule is non-deterministic, and the page total still sums mixed denominations

**Severity:** Informational.  
**Dimension:** Correctness (money).  
**Location:** `cluster_filter_predicate.go:111-123` (the `LIMIT 1` with no `ORDER BY`), `internal/api/currency.go:213-241` (`convertNodeGPURecsToUserCurrency`), `internal/api/handlers_node_recs.go:660-686` (`sumNodeGPUSavings`).  
**Description:** #947 documents first-match as the currency rule for multi-match filters. Two residues:

- **(a) "First" is arbitrary.** The query is `… WHERE org_id = $1 AND cluster_alias ILIKE $2 LIMIT 1` with no `ORDER BY`, so which matched cluster's currency labels the page can vary with plan/heap order. The new comment states the rule as if it were deterministic.
- **(b) The TS list total can sum different denominations.** `convertNodeGPURecsToUserCurrency` leaves a row in its **stored** currency when no FX rate exists (`cur = sc`), per row; `sumNodeGPUSavings` then adds the raw values and `respondNodeGPURecommendations` labels the sum `nodeCurrency` (the first-match cluster's display currency) as `meta.estimated_monthly_savings`. A page mixing USD and EUR with no EUR rate yields a "USD" total that adds EUR values.

(b) is pre-existing — an unfiltered fleet page already mixes clusters — but #947 makes a mixed-currency page reachable through a single-looking alias filter, so it belongs in the union-semantics record.  
**Risk:** a mislabeled total in a narrow configuration (multi-currency org, missing FX pair). Per-row amounts stay correctly labeled (`money.PatchUnits` per row).  
**Recommendation:** (a) add `ORDER BY cluster_uuid` — one clause, makes the documented rule true; (b) record the decision (sum only when the page is single-currency, or split/label the total, or accept with a note) rather than fixing blind.  
**Effort:** S (a); (b) is a decision.

### V18-05 — the #949 IQE test misses the authorized-empty branch; fixture mutations are unasserted; the default DSN collides with the koku test-DB convention

**Severity:** Informational.  
**Dimension:** Test quality / coverage.  
**Location:** `iqe-ros-ocp-plugin/iqe_ros_ocp/tests/rest/test_cluster_alias_multi_match.py`.  
**Description:** the five legs pin the seed, the admin union on three surfaces, the SA baseline, the authorized-happy intersection, and the unknown-alias contract. Gaps:

1. **The authorized-empty intersection is unpinned**: a substring matching **only the non-granted** cluster should be 200 + count 0 (not a leak, not a 404) on all three surfaces. One extra `_list_clusters(..., filter_value + "-b")` call per surface would pin it; today both the in-repo test (org-admin only) and the IQE test only exercise a matching-and-granted alias.
2. `_create_sa_cluster_grant` fires the principal-add and role-add `requests.post` calls **without asserting their status**, so a silent RBAC failure surfaces later as a misleading leg-3 message ("must see only the granted cluster — got []").
3. `DB_DSN` defaults to `…:15432/costonprem_ros`. 15432 is the **koku test-DB** port by convention (it is on this reviewer's host); the runbook documents the 15433 override, but a default that can point at the wrong database is a foot-gun. Requiring `IQE_ROS_DB_DSN` (skip with the runbook pointer when unset) removes it.
4. Skip-not-fail for fixture infrastructure is the right call, but a whole run can report zero failures and zero executed assertions; the runbook's "1 passed" line is the only proof. The extra leg and the asserted mutations raise the test's signal.

**Evidence:** code read of the test; the live run (1 passed, `branch-202611261759`) is the issue's record, **not re-executed in this pass**.  
**Effort:** S.

### Residual observations (recorded, not filed)

- **ILIKE metacharacters are honored.** `filter[cluster]=%` (or `_`) is a pattern, not a literal, so it returns the entire RBAC-visible fleet (verified with `psql`: `'team-a' ILIKE '%' || '%' || '%'` matches). Pre-existing on the five predicate surfaces; #947 extends it to the TS/MIG row scope. Org/RBAC-scoped, so no cross-tenant exposure — the impact is "the filter silently does not filter", and `query-parameters.md` says value wildcards are not supported. Decide separately (escape `%`/`_`/`\` or document pattern support). Not filed: no demonstrated user case.
- **Singular/plural helper names.** `resolveClusterFilterUUID` (first match, `""` on failure) and `resolveClusterFilterUUIDs` (all matches, `nil` on failure) differ by one letter and by failure semantics. Implementing the singular as `first(resolveClusterFilterUUIDs(...))` removes the trap; it rides the V18-01/V18-03 patch.
- **Duplicated empty response.** `handlers_node_recs.go` now carries two identical 12-line empty-list literals (the filter-miss and the empty-scope branches). MIG has `emptyGPUMIGResponse`; a `emptyNodeGPUResponse` helper would keep them in step. Cosmetic.
- **Resolver scoping.** `resolveClusterFilterUUIDs` is org-scoped and called **after** `filterClustersByRBAC`, and its result is intersected with the allowed set, so it cannot widen what the caller may see. Confirmed by reading both call sites.

## 4. Checked and clean (this pass)

- **`clusterFilterPredicate` ↔ `resolveClusterFilterUUIDs` agreement.** Both use the same org-scoped `cluster_alias ILIKE '%value%'`; the only divergence is V18-01 (UUID case). Keeping both is defensible defense-in-depth (the group and detail paths use the predicate without entry resolution).
- **RBAC composition.** The intersection is a subset of the RBAC-filtered org set; `#753`'s "empty allowed set must return empty" holds before and after the new early returns (TS line 131; MIG line 82).
- **Unknown-alias contract (#790).** 200 + count 0 on all three surfaces, via the empty intersection — pinned by the new test and the IQE leg 5.
- **Argument indexing.** `buildNodeGPUTimeslicingFilterSQL` advances `argIdx` correctly through the predicate's one-vs-two placeholder shapes; `= ANY(clusterUUIDs)` still binds first. Verified by reading and by the full package suite.
- **Triple-SQL digest path.** Filtering only by the intersected set is correct for a substring filter (that path has no SQL predicate), and the pre-existing `FilterByCluster` test covers it.
- **The #947 regression's mutation sensitivity (executed).** Substituting first-match for the multi-match resolver in the TS entry fails **only** the TS leg, with "a multi-match substring alias must return every matching cluster's rows (#947)"; substituting it in the MIG entry fails **only** the MIG leg, with "cluster B's rows must appear". Each leg pins its own fix for the right reason. Tree restored; HEAD clean at `c4cc36ca`.
- **Gates at the reviewed HEAD (executed):** `go test -short ./...` → 78 package lines, exit 0; `go test -C librobne -short ./...` → ok; `go test ./internal/api -run TestClusterFilter` → ok; `go test -short ./internal/api/... ./internal/costdata ./internal/money` → ok; `make lint` → 0 issues (both modules); `make docs-lint docs-drift` and `make docs-check-generated` → passed. One testcontainers startup flake ("matched 1 times, expected 2 / context deadline exceeded") happened on the first non-short run and cleared on retry — infrastructure, not the change.
- **The runbook section (`c4cc36ca`).** The port-forward/trust explanation matches the lab mechanics recorded in `ACCESS.md`; the DSN override and the drift-resync pointer are accurate; the skip semantics are stated as a design choice, which they are.
- **The #947/#949 reporting discipline.** The completion reports distinguish verified from asserted, the live E2E and IQE runs are on the issues, and the #911 deferral was corrected onto the live tracker (#949) — the process lesson held.

## 5. Scorecard

| Dimension | Rating | Key note |
|-----------|--------|----------|
| Security | ★★★★★ | No new exposure; both resolvers org-scoped and intersected with the RBAC set; no new unparameterized SQL |
| Correctness | ★★★★☆ | V18-01 (UUID case) is the only defect; V18-04(b) is a pre-existing money-labeling residue |
| Auditability | ★★★★★ | Issue records, live evidence, mutation checks, and the versioned audit trail all maintained |
| Operational robustness | ★★★★☆ | V18-03 turns a DB failure into a false empty |
| Performance | ★★★★☆ | One extra small org-scoped query per alias-filtered request (aliases only; UUID filters skip it) |
| Design quality | ★★★★☆ | V18-02's missing inventory and the first-match currency rule's non-determinism |
| Maintainability | ★★★★☆ | Singular/plural trap and the duplicated empty response |
| Governance | ★★★★★ | ADR/changelog/docs gates; docs-derivation clean; the review cadence itself held |

## 5.1 Finding trackers

Filed 2026-11-26 on `pgarciaq/ros-ocp-backend` (one issue per finding, per the standing rule that a deferral needs a live tracker).

| Finding | Issue |
|---|---|
| V18-01 (UUID filter case) | [#951](https://github.com/pgarciaq/ros-ocp-backend/issues/951) |
| V18-02 (surface inventory + docs contract) | [#952](https://github.com/pgarciaq/ros-ocp-backend/issues/952) — extends [#950](https://github.com/pgarciaq/ros-ocp-backend/issues/950) |
| V18-03 (resolver swallows lookup errors) | [#953](https://github.com/pgarciaq/ros-ocp-backend/issues/953) |
| V18-04 (currency first-match determinism + mixed-denomination total) | [#954](https://github.com/pgarciaq/ros-ocp-backend/issues/954) |
| V18-05 (IQE authorized-empty leg + fixture asserts) | [#955](https://github.com/pgarciaq/ros-ocp-backend/issues/955) |

## 6. Priority remediation order

1. **V18-01 + V18-03 + the singular/plural collapse** — one two-to-six-line patch on the resolver pair; the only code change recommended before sign-off.
2. **V18-02(docs)** — the three table rows, one union-semantics paragraph, the CHANGELOG head; then extend #950's inventory.
3. **V18-04(a)** — `ORDER BY cluster_uuid`; cheap and makes the documented rule true. (b) is a decision, not a fix.
4. **V18-05** — the authorized-empty IQE leg and the fixture asserts, next time the suite runs.

## 7. Current state

5 findings: 1 Low, 4 Informational; no P1/P2. All claims above carry an executed probe, a gate run, or a file read; the live E2E and IQE runs are **reported on their issues, not re-executed here**. The phase-17 branch's #947 change holds up under adversarial review: the union predicate is org-scoped, the intersection can only narrow, each regression leg is mutation-sensitive, and all gates are green at `c4cc36ca`. **Sign-off is not blocked**; V18-01 is a small consistency defect worth landing while the resolver is fresh.
