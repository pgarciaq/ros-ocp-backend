# Adversarial review v18 — v17 remediation verification + #947 union-semantics review

**Date:** 2026-11-26  
**Reviewer:** AI-assisted (session agent); locally executed probes, not a security certification.  
**Verdict:** **PASS with one Low and four Informational findings.** No P1/P2 demonstrated. In this reviewer's judgement the branch is not blocked from sign-off — but **no phase-17 sign-off has been given**; that is the owner's call, not this review's, and none is recorded anywhere in this file. V18-01 is the only code change recommended, and it shares a patch with V18-03.  
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
**Revised scope (§6.5):** the inventory's per-surface column *is* #950's output, so the docs land after #950 — not before it.

### V18-03 — the new resolver swallows lookup failures into a false "no matches" 200

**Severity:** Informational.  
**Dimension:** Operational robustness.  
**Location:** `cluster_filter_predicate.go:62-76`.  
**Description:** `resolveClusterFilterUUIDs` returns `nil` on `pool.Query` failure and never checks `rows.Err()`. Callers (`handlers_node_recs.go:110-125`, `handlers_gpu_mig.go:69-76`) then return the #790 empty response — **HTTP 200, count 0** — for a request whose lookup failed, where the same handlers use 503 for a failed cluster resolution. A mid-iteration failure also produces a truncated union that looks like a legitimate row set. The sibling `resolveClusterFilterUUID` has the same shape (and predates #947), but this is new code.  
**Risk:** a transient DB error is reported to the user as "no matching data" rather than a retryable failure; no exposure, a false negative.  
**Recommendation:** return `(uuids []string, err error)`, treat `rows.Err()` as an error, and let the callers answer 503 (or log-and-keep the empty contract, explicitly). Same patch as V18-01.  
**Effort:** S.  
**Revised scope (§6.1, §6.3):** the singular resolver's ten callers use it for the **display currency only**, so the failure-shape change touches two row-scope call sites, not ten — the split that keeps this S is recorded in §6.3, and the #790 200-empty contract must stay distinguishable from the new 503.

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
**Revised scope (§6.2):** (b)'s mechanism is worse than filed — the total is **unit-blind** (each row's `.Units` is ignored), so a single-currency page can mislabel its total; and the field is a shared `openapi.json` contract that most node surfaces never populate. (a) also changes the currency label on the nine other surfaces that share the resolver, so it is a visible behavior change, not a comment fix.

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
**Revised scope (§6.4):** the seeded fixture pair (`{filter_value}-a` / `-b`, grant on `-a`) already supports the authorized-empty leg exactly as filed — no fixture redesign, one assertion per surface.

### Residual observations (recorded, not filed)

- **ILIKE metacharacters are honored.** `filter[cluster]=%` (or `_`) is a pattern, not a literal, so it returns the entire RBAC-visible fleet (verified with `psql`: `'team-a' ILIKE '%' || '%' || '%'` matches). Pre-existing on the five predicate surfaces; #947 extends it to the TS/MIG row scope. Org/RBAC-scoped, so no cross-tenant exposure — the impact is "the filter silently does not filter", and `query-parameters.md` says value wildcards are not supported. Decide separately (escape `%`/`_`/`\` or document pattern support). **Filed 2026-11-26 as [#956](https://github.com/pgarciaq/ros-ocp-backend/issues/956)** — see §6.6.
- **Singular/plural helper names.** `resolveClusterFilterUUID` (first match, `""` on failure) and `resolveClusterFilterUUIDs` (all matches, `nil` on failure) differ by one letter and by failure semantics. They are **not** the same contract: the plural one governs row scope, the singular one only the display currency (§6.1, §6.3) — so the trap is resolved by documenting the divergence, not by folding one into the other; `first(resolveClusterFilterUUIDs(...))` would give the currency path a 503 it does not want.
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

## 6. Remediation readiness pass (2026-11-26, post-filing)

A second pass read the five filed issues back against the tree **before** implementation, because an issue that understates its own blast radius costs more at review time than at filing time. Seven discoveries: four changed a filed issue's scope, one is a new finding of the same class, one is #950's ordering dependency, and one is the rebase board. Each is marked **verified** (a command, a probe, or a file read in this pass) or **asserted**.

### 6.1 D1 — the resolver is a ten-caller helper, not a two-endpoint fix (verified)

`resolveClusterFilterUUID` (singular) is called from ten handlers:

| Surface | Call site |
|---|---|
| Node / GPU time-slicing recommendations | `handlers_node_recs.go:307` |
| Node persist (TS/MIG rows) | `handlers_node_recs_persist.go:489` |
| Node utilization | `handlers_node_utilization.go:501` |
| PVC | `handlers_pvc.go:242` |
| PVC group | `handlers_pvc_group.go:90` |
| Quota recommendations | `handlers_quota_recs.go:310`, `:437` |
| Cluster-quota recommendations | `handlers_cluster_quota_recs.go:284` |
| Snapshots | `handlers_snapshot.go:238` |
| Snapshot group | `handlers_snapshot_group.go:89` |

**All ten call sites use the result for the display currency only** (verified by reading each): every one feeds `fetchClusterCurrency`/`fetchExchangeRate`; none uses it in a WHERE clause or an intersection. Row scope is carried by `clusterFilterPredicate` (SQL) and, since #947, by `resolveClusterFilterUUIDs` — which has **two** call sites (`handlers_node_recs.go:110`, `handlers_gpu_mig.go:69`).

Consequences for the filed issues:

- **V18-03's signature change touches two call sites, not ten** — S stands. The ten-caller fact still matters twice: the currency path needs a documented best-effort contract (§6.3), and its failure must not be conflated with a row-scope failure.
- **V18-04(a) is a visible behaviour change.** `ORDER BY` picks which cluster's currency labels the page on all ten surfaces for a multi-cluster alias filter. It is not a comment correction.
- **V18-01's normalization belongs in both resolvers.** The Go-side set comparison is case-sensitive while every SQL comparison is not; normalizing in the shared pair keeps "the cluster" in one canonical form instead of relying on Postgres's uuid input parsing. Normalizing only in the plural helper would leave the singular one (which feeds all ten currency lookups) on the raw value.
- The resolver is org-scoped and its result is intersected with the RBAC set, so it is tenant-isolation-adjacent. The mandated pre-commit review question ("how could these tests pass while the bug lives?") applies to the batch.

### 6.2 D2 — V18-04(b) is a shared public-contract field, and the sum is unit-blind (verified)

Three facts the finding as filed understated:

1. `NodeRecommendationMeta.estimated_monthly_savings` is in `openapi.json` as a nullable `MoneyAmount` on the **shared** node meta schema — not a GPU-TS-private field.
2. **Only the GPU time-slicing list sets it** (`handlers_node_recs.go:433`, inside `respondNodeGPURecommendations`). The nodes list and the GPU group response build the same meta without it (`:115`, `:134`, `handlers_node_recs_group.go:149`), so the schema already promises a field most surfaces never populate.
3. `sumNodeGPUSavings` sums **unit-blind**: `savingsObjectUSDValue` parses each row's `.Value` with `ParseFloat`, **ignores `.Units`**, and the total is rebuilt with `money.USDToCents` and formatted in the page currency. A row that kept its stored currency (`cur = sc` in `convertNodeGPURecsToUserCurrency`) is therefore added as if it were the page currency — so a **single-currency page** can mislabel its total, not only a mixed one. "Mixed denominations across clusters" understates the mechanism.

Consumers: no koku-ui-ros reader of the *meta* field (grep over `apps/koku-ui-ros/src`; the per-row amounts and the savings-summary endpoint's top-level field are different code paths), and no IQE assertion on it.

**Owner decision (2026-11-26):** changing this contract is acceptable — the service has internal consumers only so far. The remedy space therefore includes dropping the field from the shared schema or moving it onto the GPU-TS response type, not merely re-labelling it. `openapi.json` must move with whichever option is chosen.

### 6.3 D3 — V18-03 collides with the #790 contract and the error-semantics rule (verified)

Two outcomes must stay distinguishable, and only one of them may change:

| Outcome | Current | Must remain |
|---|---|---|
| Unknown alias, lookup succeeded, 0 rows | 200 + count 0 | **200 + count 0** (#790; pinned by the in-repo test and IQE leg 5) |
| Lookup failed (query error, `rows.Err()`, mid-iteration failure) | 200 + count 0, silently | 503 — or an explicitly commented empty + warn log |

UI check (verified by reading `optimizationsNodesTable.tsx:236-245` and its seven siblings): the ROS tables treat **404/501** as "not configured" and **every other error — 503 included — as `NotAvailable`**. A 503 is therefore not misread as "no recommendations"; the change needs **no UI work**. There is no retry affordance on that path: a UX nicety, not a defect.

**The split that keeps this small (§6.1):**

- **Row scope — `resolveClusterFilterUUIDs`** (two call sites): return `(uuids, error)`, treat `rows.Err()` as an error, answer **503** on failure. This is where "I don't know" must not be reported as "nothing matched".
- **Display currency — `resolveClusterFilterUUID`** (ten call sites): keep `""` on failure, add a warn log, and document the contract as **best-effort** — every caller already falls back (`userCurrency`, the first row's cluster, the org default), and a currency label is not worth failing a page. No signature change, no handler churn.
- The singular/plural naming trap is then resolved by documentation, not by folding one into the other: `first(resolveClusterFilterUUIDs(...))` would hand the currency path a 503 it does not want.

`AGENTS.md` requires UI/IQE coordination and contract-test coverage for API error-semantics changes. The honest coverage split:

- **In-repo** — a fault-injection test (closed pool or canceled context) asserting 503 on the three union surfaces, plus the existing 200-empty contract test. This is the gate that can actually fail.
- **IQE** — the gateway cannot fault-inject the database, so the 503 path is not reachable from an API test. Record that limitation on the tracker rather than inventing a suite that cannot exercise it; IQE keeps the 200-empty leg it already has.

### 6.4 D4 — V18-05's fixture already supports the authorized-empty leg (verified)

`_seed_multi_match_pair` already creates `{filter_value}-a` and `{filter_value}-b` and grants only `-a`, so `filter_value + "-b"` is the authorized-empty filter exactly as filed — no new fixture, one assertion per surface. The `IQE_ROS_DB_DSN` change must move three artifacts together: the module docstring's documented default, the runbook line, and the test's skip path. `docs-drift` catches a partial move.

### 6.5 D5 — V18-02 must follow #950 (verified)

V18-02's remedy has two halves. The union paragraph and the three wrong doc rows are stable today (#947 is merged and #950 does not change it). The **inventory's per-surface column is #950's output**, so the docs land after #950 decides unify-vs-document for the detail and heatmap surfaces — otherwise the page describes a state that changes in the same phase.

### 6.6 D6 — the wildcard residual is unfiled and will collide with the first patch (verified earlier; restated)

`filter[cluster]=%` (or any `_`) acts as an ILIKE pattern and returns the entire RBAC-visible fleet — verified with `psql` in the review pass. It is org-scoped and RBAC-intersected, so it is a scope surprise, not an exposure; but `query-parameters.md` states that value wildcards are not supported, which makes the current behaviour a documented-vs-actual gap. It was left unfiled for lack of a user case, yet the first remediation batch **rewrites this exact resolver**, so the reviewer will raise it there. Filed 2026-11-26 as [#956](https://github.com/pgarciaq/ros-ocp-backend/issues/956). Recommended fix: escape `%`/`_`/`\` in the same patch (two lines plus one test), and name the behaviour change — an alias containing a literal `_` stops matching other single characters, which is a correctness improvement but observable.

### 6.7 D7 — the rebase surface: thirteen upstream commits, two decisions, and two KMO branches that are not missing work

**Not in flight.** #944 is the phase-18 kickoff and has not started. This subsection records what that rebase will present when it runs — the facts were verified now because the review surfaced them, not because anything is scheduled. Nothing here gates §7's batch A.

**ros-ocp-backend** (`git log HEAD..origin/main`, verified): **13** commits, not the 33 that the fork's local `main` is behind. They touch `internal/api/listoptions/list_options.go`, `internal/api/handlers.go`, `server.go`, `metrics.go` — not the cluster resolver, but they change pagination on the pages under remediation. Two resolutions are decisions, not merge mechanics:

- **`go.mod`** — ours is `1.26.9`, upstream's is `1.26.7`: keep ours.
- **`limit=-1`** — ours passes it through (only `< -1` errors, `list_options.go:189-193`); upstream adds `ListOptions.EffectiveDBLimit`, capping JSON `limit <= 0` to `MAX_LIMIT` (1000) and pinning CSV to `MAX_LIMIT_CSV`. That is a semantic fork on the endpoints under review and must be resolved deliberately at #944, with the CSV path checked for the union surfaces.

Most of the 13 are build/hygiene on a Kruize-based upstream and are irrelevant to the native engine, but four are not: `#722` (CVE fixes), `#723` (stop committing `.env` with MinIO credentials), `#727`/`#728` (hermetic build configuration). The rebase is worth doing for those; the rest comes free.

**koku-metrics-operator** — `pgarciaq-764-composite-key-identity` and `pgarciaq-768-no-optin-hooks` investigated; **nothing is missing** (verified):

- The `+` prefix in `git branch -vv` is git's marker for **"checked out in another worktree"** — not a remote or status flag. Both branches are materialized: `~/dev/koku/kmoperator-764` and `~/dev/koku/kmoperator-768`.
- Both tips are ancestors of the phase-17 HEAD (`9f22f7cf`): `git merge-base --is-ancestor 8799bf17 HEAD` and `... 546b5c5b HEAD` both exit 0; `git branch --contains` lists `pgarciaq-rosocp-superpowers-phase17`; `git rev-list --left-right --count HEAD...<branch>` is `15 0` and `14 0` — the branches contain **zero** commits HEAD lacks.
- Both worktrees are clean (`git status --porcelain` empty) and both branches match their `pgarciaq/` remotes (`0 0`), so no local-only work exists to lose.

Recommendation: remove the two stale worktrees and delete the local branches (their history stays on `pgarciaq/` and in the phase branch) so the next branch audit does not re-open this question.

**Done 2026-11-26:** both worktrees removed and both local branches deleted with `git branch -d` (safe delete — it refuses unmerged tips, and it accepted both). `git worktree list` now shows only the main checkout at `9f22f7cf`, the working tree is clean, and both tips are still on `pgarciaq/` at the same SHAs (`8799bf17`, `546b5c5b`).

### 6.8 Decisions on record

| Item | Decision | State |
|---|---|---|
| Public-contract change for V18-04(b) | Acceptable — internal consumers only (owner, 2026-11-26) | **decided** |
| V18-03 failure shape | Row scope: 503 on lookup failure (2 call sites). Currency: stays best-effort with a warn log (10 call sites). In-repo fault-injection test; the IQE limitation is recorded | **recommended, pending** |
| V18-06 wildcard escaping | Escaping the ILIKE metacharacters in the same patch as V18-01/V18-03 | **filed as [#956](https://github.com/pgarciaq/ros-ocp-backend/issues/956)** |
| Remediation batching | A: V18-01+03+04a (+#956) · B: V18-05 (IQE, parallel) · C: #950 → V18-02 · D: V18-04b; no #944 dependency | **recommended, pending** |
| #950 unify-vs-document | Options note posted on the issue 2026-11-27 (unify all five recommended; the detail is list-shaped, so no ambiguity rule is needed) | **owner decision pending** |
| #944 resolutions | Keep `go 1.26.9`; decide `limit=-1` explicitly; adopt the CVE/`.env`/hermetic commits | **notes for #944 — not in flight** |
| KMO stale worktrees/branches | Removed / deleted; tips verified on `pgarciaq/` and in the phase branch | **done 2026-11-26** |

## 7. Priority remediation order

Proposed batching — one PR per letter. **Batch A does not depend on #944**: it fixes defects in #947's own phase-17 change and can land on the current phase-17 branch. B–E are follow-ups whose timing is the owner's call; the rebase (§6.7) changes their base, not their content.

1. **Batch A — V18-01 + V18-03 + V18-04(a)** (plus [#956](https://github.com/pgarciaq/ros-ocp-backend/issues/956)'s wildcard escaping). One file, one function family, one review. Splitting these means three conflicting diffs on `cluster_filter_predicate.go` and three CI cycles. Carries the ten-caller audit and the failure-shape decision.
2. **Batch B — #950.** The detail/heatmap unify-vs-document decisions; produces the inventory column V18-02 needs.
3. **Batch C — V18-02 (docs).** The union paragraph, the three wrong rows, the CHANGELOG head.
4. **Batch D — V18-04(b).** A contract change (OpenAPI, possibly the Go response type), kept separate so the error-semantics change and the money-contract change keep their own evidence trails.
5. **Batch E — V18-05.** The IQE repo; can run in parallel with C/D and should land last so its pins describe final behaviour.

## 8. Current state

5 findings: 1 Low, 4 Informational; no P1/P2. All claims above carry an executed probe, a gate run, or a file read; the live E2E and IQE runs are **reported on their issues, not re-executed here**. The phase-17 branch's #947 change holds up under adversarial review: the union predicate is org-scoped, the intersection can only narrow, each regression leg is mutation-sensitive, and all gates are green at `c4cc36ca`. **This review does not block sign-off** — but it is not a sign-off either, and none has been given. V18-01 is a small consistency defect worth landing while the resolver is fresh.

One process note for the record: on 2026-11-26 the phase-17 board was empty — every filed issue closed — and that was reported as the phase's last known items. This pass then found five findings in code closed the same afternoon (#947, `51cd1485`, closed 18:08). An empty board means the known backlog is exhausted. It is not a completion signal, and it must not be restated as one — not as "signed off", not as "the last issues", not as "ready" without the owner's word.

The readiness pass (§6) raised four of the five findings' scope — two of them (V18-03's ten callers; V18-04(b)'s unit-blind sum on a shared schema) from a patch into decisions that must be taken before a diff exists. None of it changes the verdict, and no remediation code had been written when this section was added.

