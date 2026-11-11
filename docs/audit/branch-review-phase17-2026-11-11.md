# Phase17 Branch Review — Integration Review (GPT 6.1 Sol)

**Date:** 2026-11-11
**Reviewer:** GPT 6.1 Sol (fresh-context subagent; the audit-lineage model for v6–v16), dispatched by the phase workflow with anti-anchoring instructions.
**Scope:** `main..pgarciaq-rosocp-superpowers-phase17` @ `3757cc635c3eb5779b81dca460acbc9795ae06d8` — 1,889 commits, 7,326 files.
**Purpose:** the integration-level review before the Engineering-colleagues hand-off (the fork-transition sign-off). Per-issue remediation cycles and the v6–v16 audit series covered their own scopes; this review owns what per-issue reviews structurally miss: cross-feature interactions, drift, dead code, migration integrity, and the pre-v6 thin region.
**Companion artifact:** `branch-residue-adjudication-2026-11-11.md` (same date, same reviewer) — the pre-rebase phase-branch reconciliation.

**Verdict: BLOCK hand-off as merge-ready.** One verified tenant-isolation defect and six further verified Important findings; two Minor findings. All findings below are verified against code by the reviewer; executed probes are distinguished from code-path verification; none is claimed as a live customer incident.

---

## 1. Findings

### 1. Important — Container-list detail expansion can read another tenant’s recommendations

**Location:** `internal/model/recommendation_set_native.go:511–520`

**Trigger and mechanism:** Two organizations contain the same cluster UUID and container identity—namespace, workload, workload type, container name. Page selection filters `ock.org_id`, but the subsequent detail query neither filters `rs.org_id` nor joins it to the page’s organization. Filtering the joined `clusters` row by organization does **not** constrain the recommendation row’s organization.

The schema permits this shape: `migrations/000028_alter_recommendation_sets_add_relational_columns.up.sql:72–73` includes `org_id` in the recommendation primary key. Result assembly groups without organization (`recommendation_set_native.go:805–825`) and serializes the resulting recommendation variants (`:920–961`).

**Evidence:** An executed, read-only PostgreSQL CTE reproducing these joins returned:

```text
A|short|cost|100
B|long|performance|9000
```

for an A-scoped cluster join and page. This demonstrates the SQL mechanism, **not an HTTP-endpoint reproduction**. No automatic tenant-scoping callback exists in the inspected DB initialization (`internal/db/db.go:116–145`).

The passing `TestDetailQueries_ScopeByOrgID` covers single-record detail builders, not this list expansion (`internal/model/recommendation_detail_org_scope_test.go:46–55`).

**Suggested fix:** Explicitly constrain `rs.org_id` during list expansion. Add a regression with identical full identities across organizations and distinguishable term/engine values.

**Severity rationale:** Confidentiality and recommendation-integrity impact is demonstrated, but requires identity collision; arbitrary cross-tenant enumeration is not claimed.

### 2. Important — Accepted category filters disappear; container engine filters disappear too

**Locations:**
- `internal/model/native_list_keys.go:31–75,113–120`
- `internal/model/native_ns_list_keys.go:32–78,115–122`

**Trigger and mechanism:** Request `filter[category]=zombie` on either native list, or an engine filter on the container list.

These keys are accepted by the API/query allowlists, but absent from the corresponding detail-only classifications and key-table remapping tables. The splitter sends them to the keys leg; the remapper logs and skips them. They never reach the detail query.

**Evidence:** A source-derived classification probe produced:

```text
rs.engine IN ?   -> sent to keys but unmapped
rs.category IN ? -> sent to keys but unmapped
ns.category IN ? -> sent to keys but unmapped
```

The API emits these predicates at `internal/api/handlers.go:405–406,434–445` and `internal/api/utils.go:849–860`. Acceptance is confirmed by `internal/model/native_query_allowlist.go:16–18,74–77`.

Response projection can hide unwanted engine fields, but does not repair database eligibility, counts, or CSV behavior. Category filtering has no such masking protection.

**Suggested fix:** Classify every accepted predicate deliberately and apply recommendation-dependent predicates to eligibility before pagination. Test category and engine filters independently—not only combined projection requests.

### 3. Important — Recommendation-dependent filters run after pagination, prematurely ending traversal

**Locations:**
- Container: `internal/model/recommendation_set_native.go:499–532`
- Namespace: `internal/model/native_ns_list_keys.go:315–346`

**Trigger and mechanism:** Early sorted keys lack the requested term, while later keys contain it.

Both paths select and limit keys before applying detail-only predicates. Nonmatching keys disappear during expansion. `HasNext` is then calculated from surviving results, so an empty or short page can report no continuation even though matching records remain beyond the selected keys. Counts also omit these predicates.

This affects term filters and other detail-only filters; namespace engine filters share the same placement.

**Evidence:** An executed read-only PostgreSQL CTE used three keys: `a` and `b` with short-term data, `z` with long-term data. Selecting the first two keys and then filtering for long produced:

```text
reported_count=3 | returned_rows=0
```

The production code consequently calculates `HasNext=false`.

Historical interaction: `31591568` added namespace term filtering before the key-table implementation; `184bd61d` subsequently introduced the separate page-selection leg without carrying those predicates into eligibility.

**Suggested fix:** Apply recommendation eligibility consistently to count and key-page selection, using an appropriately scoped `EXISTS` or equivalent mechanism. Test matching records beyond a full page of nonmatches.

### 4. Important — Namespace key-table pagination sorts numeric values lexicographically

**Location:** `internal/model/native_ns_list_keys.go:303–317,375–415`

**Trigger and mechanism:** Sort namespaces by savings, waste, or another numeric field with different digit lengths.

The page key is cast to text, then outer page ordering operates directly on that text. The namespace DISTINCT fallback preserves a typed raw value (`internal/model/namespace_recommendation_set_native.go:187–207`); the keys path does not. Thus changing stale/tag routing can change sorting semantics.

**Evidence:** An executed PostgreSQL probe using the same text-cast ordering returned descending values:

```text
2, 100, 10
```

rather than `100, 10, 2`.

Container pagination explicitly handles this distinction with numeric casts (`internal/model/container_recommendation_pagination.go:50–80`).

**Suggested fix:** Preserve a typed sort value through namespace page selection, ordering, and cursor seeking. Test digit-length boundaries, negatives, NULLs, and multiple pages on both query paths.

### 5. Important — Namespace request/variation sort aliases still target unpopulated legacy columns

**Locations:**
- `internal/api/listoptions/list_options.go:131–146`
- `internal/model/namespace_recommendation_set_native.go:18–24`

**Trigger and mechanism:** Sort native namespaces by `cpu_request_current`, `memory_request_current`, or a term/engine-specific variation alias.

Namespace remapping changes only the table alias. It leaves legacy column names intact. Native persistence writes `current_cpu_request_millicores`, `current_memory_request_kib`, and row-level variation columns—not those legacy sort columns.

**Evidence:** The production writer is `librobne/pgrec/namespace.go:31–59`, called by `internal/engine/recommend_namespace.go:123–124`. Usage searches found legacy-column writes in the Kruize model path, but not native persistence.

The corresponding container implementation translates these aliases and selects the relevant term/engine (`internal/model/recommendation_set_native.go:54–76`). Namespace pagination lacks that translation.

**Suggested fix:** Implement namespace-specific native sort translation, including term/engine selection for variation aliases. Keep legacy query semantics separate. Use fixtures whose native and legacy values differ.

### 6. Important — UTC weekday applicability permanently suppresses valid timezone-shifted sleep schedules

**Locations:**
- Server: `internal/engine/correlate/sleep.go:262,280–296`
- Offline: `librobne/advisory/sleep.go:224,235–251`

**Trigger and mechanism:** An enabled Mon–Fri `00:00–08:00` schedule in `Pacific/Kiritimati`.

The producer classifies timestamps in the schedule’s local timezone (`librobne/bhschedule/schedule.go:108–127`). The evaluator separately decides whether a UTC day requires BH evidence using that UTC day’s weekday.

For this schedule, business hours occupy **Sunday–Thursday UTC**, but the evaluator requires BH evidence **Monday–Friday UTC**. Fully observed Friday UTC correctly has no BH leg, yet is rejected as unknown. A normal multiweek window therefore never qualifies.

**Evidence:** Executed timezone enumeration confirmed:

```text
Friday: UTC_day_applicable=True, classified_BH_hours=[]
Sunday: UTC_day_applicable=False, classified_BH_hours=[10..17]
```

The reverse mismatch also skips BH completeness checks on UTC days containing legitimate business hours.

**Suggested fix:** Derive applicability from classified possible hours—already calculated by the evaluator—not a separate UTC weekday check. Test timezone-shifted and overnight weekday schedules.

### 7. Important — Populated rollback fails when uniqueness contracts narrow

**Locations:**
- `migrations/000113_fix_pvc_recommendation_term_unique.down.sql:13–15`
- `migrations/000064_pvc_recommendations_add_term.down.sql`
- `migrations/000125_namespace_quota_identity.down.sql:4–9`

**Trigger and mechanism:** Normal supported data contains multiple terms for one PVC, or multiple ResourceQuotas in one namespace.

The down migrations recreate narrower unique constraints without resolving the newly valid duplicates. PostgreSQL cannot create those constraints over such rows.

Migration 113 additionally restores the **wrong preceding contract**: both 64-up and 113-up establish five-column PVC uniqueness including term, but 113-down replaces it with four-column uniqueness.

**Evidence:** Direct comparison verified the identical term-inclusive uniqueness in both up migrations. Migration 125-up explicitly permits separate quota names (`:42–47`), whereas its down migration immediately prohibits them.

This is **code-verified, not an executed populated downgrade**. The existing generic roundtrip test does not seed these collision shapes before downgrading (`internal/engine/migration_roundtrip_test.go:47–64`).

**Suggested fix:** Make 113-down restore the actual preceding schema. For genuine narrowing migrations, define an explicit downgrade policy and test populated cases; do not silently introduce arbitrary deletion.

### 8. Minor — Platform-row schedule attestation breaks server/offline sleep parity

**Locations:**
- `librobne/pgdigest/read_sleep_evidence.go:63–68`
- `librobne/advisory/sleep.go:194–196`

**Trigger and mechanism:** User workload evidence is complete and current, but a platform-namespace row retains an old or NULL schedule fingerprint.

The server excludes platform workloads before enforcing schedule attestation (`internal/engine/correlate/sleep.go:309–332`). The offline reader instead produces one window-wide boolean covering **every** row, and the advisory immediately rejects a false value.

The same stored evidence can therefore produce server advice but offline silence. One obsolete platform row can block offline recovery after user evidence has rebuilt.

**Evidence:** The CLI consumes this global flag at `cmd/robne/recommend.go:568–576` and `cmd/robne/advisory_context.go:165–167`. ADR-0348 explicitly describes user-workload-scoped attestation (`docs/adr/0348-w8-nonprod-sleep-advisory.md:71`).

**Suggested fix:** Preserve workload-grain attestation until platform exclusions are applied, using the same compiled and synced exclusions as the server.

### 9. Minor — Weekly sleep reclaim omits entirely non-business days

**Locations:**
- Server: `internal/engine/correlate/sleep.go:46–51`
- Offline: `librobne/advisory/sleep.go:328–340`

**Trigger and mechanism:** A weekday-only business-hours schedule.

Both implementations calculate:

```text
selected days × outside-hours within each selected day
```

They omit weekends, even though weekend activity is explicitly evaluated because those hours are part of the proposed sleep window.

**Evidence:** Mon–Fri 09:00–17:00 yields `80` hours instead of `168 − 40 = 128`. Two workers report `160` instead of `256` node-hours/week: a **37.5% underestimate**. The passing test encodes the incorrect expectation (`librobne/advisory/sleep_test.go:68–69`).

The public contract describes worker nodes multiplied by non-business hours (`docs-site/features/hosted-control-plane.md:195–199`).

**Suggested fix:** Calculate the full weekly complement consistently with schedule semantics; update weekday and overnight expectations.

## 2. Verified non-findings

- **Migration numbering and latest-version constant agree.** `TestMigrationSequence_NoGapsAndPairedFiles` passed: contiguous versions through 222, paired filenames, matching latest-version constant. This does not establish populated reversibility.
- **The recent sleep gates compose for the exercised migration shapes.** Targeted tests passed for off-p95 consumption, partial migration, legacy rows, sparse coverage, schedule-change invalidation, and complete overnight observation. The loader independently enforces p95 completeness, minimum coverage, and fingerprint agreement (`internal/engine/correlate/sleep.go:149–219`).
- **The exercised tag-warning cluster boundaries hold.** All `TestTagWarningClusterScope_*` tests passed. Caller inspection confirmed server-owned scope, explicit empty-set handling, fail-silent resolution errors, and separation from client cluster filters. This does not close known project/node-grain issue #834.
- **No checked first-party vendor drift.** Byte comparison covered **174 vendored librobne `.go` files**, with zero mismatches. The mutating `go mod vendor` target and third-party dependency audits were not run.
- **The superseded container DISTINCT route is intentionally unreachable.** Usage grep and `usesOrgContainerKeys()==true` confirm this; ADR-0306 records the disposition.
- **Inspected HCP policy/default mappings agree.** `TestPolicyFromSettings_MirrorsEngineDefaults` passed. Coverage fraction is intentionally not a tenant knob. The unused etcd setting is documented as awaiting wiring.
- **Documentation gates passed:** `make docs-lint docs-drift man-check`. Their inspected implementations check links, selected textual invariants, stamps, and generated command documentation—not general semantic agreement with code.

## 3. Coverage statement

### Deeply covered

- Native container/namespace list routing, predicate splitting, page selection, detail expansion, counts, result assembly, and sort/cursor helpers.
- Sleep evidence producer → persistence → server aggregation/evaluation → offline reader/evaluation, emphasizing migration completeness, schedule identity, platform exclusions, and timezone boundaries.
- Tag-warning scope construction and production attachment sites.

### Sampled

- Migration sequence metadata across **000001–000222**; selected down-migration bodies and narrowing constraints; sleep migrations **219–222** read directly.
- HCP policy/settings/configuration mappings.
- Retention and schedule-pruning hooks.
- ADR-0348, ADR-0306, public HCP/configuration material, upgrade guidance, and documentation-check implementations.
- Prior reports and current issue titles for duplicate avoidance; not a fresh attestation of every prior remediation.

### Thin-region accounting

Author-date filtering found **1,157 pre-2026-07-04 commits** in the delta. The reviewer inspected relevant, path-limited patches from **eight selected commits**:

`8d505a10`, `184bd61d`, `31591568`, `2500cc1e`, `394863c5`, `04a32f5a`, `311df3b4`, `d81835aa`.

That is **eight sampled commit diffs—not 1,157 reviewed commits**. The early list architecture remains load-bearing and contains several findings above.

### Not covered

- All 1,889 commits or all 7,326 files.
- Full engine/plugin numerical correctness across resource types.
- Every migration’s populated upgrade/downgrade behavior.
- Dependency security, FIPS, licensing, deployment images, live clusters, SaaS configuration, scale benchmarks, or other repositories’ implementations.
- Exhaustive ADR amendment, CHANGELOG, or docs-mirror completeness.
- Fresh mutation checks or new endpoint regressions for these findings.

No source files were modified.

## 4. Review-package material

**API/model serving:** The key-table architecture separates identity selection from recommendation expansion, but correctness requirements have not consistently crossed that boundary. Tenant scope is lost during container expansion; accepted predicates are dropped or applied too late; namespace sorting retains both text-ordering and legacy-column defects. Engineering review should prioritize matching count/page/detail eligibility and organization boundaries, rather than reviewing handlers independently.

**Sleep ingestion/correlation/librobne:** The replacement evidence model has explicit cold-start, p95-completeness, coverage, and schedule-attestation defenses, with targeted regressions passing. Remaining integration defects concern timezone-day applicability, platform-scoped parity, and weekly reclaim arithmetic. Known p95 rotation-fleet (#835) and disabled-override (#839) issues remain excluded from new findings.

**Migrations:** Numbering, pairing, and the latest-version constant are consistent. The principal sampled risk is populated rollback: supported cardinality expansions are not consistently reversed safely, and migration 113 does not restore its actual predecessor. Empty-schema roundtrips should not be presented as production rollback assurance.

**Configuration and documentation:** Inspected HCP defaults and policy mappings agree, and documentation checks pass. Those checks are intentionally narrow: the sleep attestation contract and reclaim description still disagree with particular implementations. Exhaustive public configuration, ADR, and CHANGELOG reconciliation remains an Engineering-review coverage gap.

**Security and operational boundaries:** The sampled tag-warning remediation preserves cluster scope, empty authorization sets, and fail-silent resolution. That success does not establish tenant isolation elsewhere: the container list detail-expansion defect is independently verified and should be resolved before the branch is presented as merge-ready.

---

*Findings tracker: see the phase17 branch-review remediation tracker issue. The residue adjudication companion report records the pre-rebase phase-branch reconciliation.*
