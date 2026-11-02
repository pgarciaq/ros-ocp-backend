# #779 Fixture Month-Boundary Fix — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `make test` (full suite) pass on valid production behavior by fixing date-fragile test fixtures (retention month choice, month-boundary inserts) and one stale contract expectation (`TestContractCompatListDisplayParity`).

**Architecture:** Test-only changes — zero production code changes. A pure date-math helper picks fixture months provably on the correct side of both retention cutoffs; seeders ensure partitions for every timestamp they insert; the compat-parity test is updated to pin the shipped #692 contract (friendly alias with UUID fallback) on both branches.

**Tech Stack:** Go 1.2x, testify, pgx/v5, testcontainers (local Docker verified working).

**Spec:** [GH #779](https://github.com/pgarciaq/ros-ocp-backend/issues/779) (V15-29) + session recon 2026-11-02 (all findings below verified at HEAD `66999e32` unless noted).

**Recon-verified facts this plan argues from:**

- Production `RunRetentionSweep` (`internal/engine/retention.go:102-174`) drops a partition only when its `YYYYMM < cutoffYM` (string compare, `SweepPartitionedTables:314`). Digest cutoff = `now − retentionMonths`; history cutoff = `now − historyDays` (config, default 90, tests set 45).
- Retention fixtures (`internal/engine/retention_history_trait_test.go:52,107`) pick `month(now − 2 months)`. On 2026-11-02 that is September, which overlaps the 45-day cutoff (Sep 18) → production correctly keeps it → assertion fails. **Both tests reproduce locally at HEAD.**
- The interval `(now − retentionMonths, now − historyDays)` with defaults-for-tests (3 mo, 45 d) is ~46 days wide, so a calendar month `YM` with `digestCutoffYM ≤ YM < historyCutoffYM` always exists.
- `seedAPITaxWindow` (`internal/engine/correlate/apitax_test.go:21-49`) calls `ingestion.EnsureAPITaxPartitionsForMonth(ctx, pool, windowStart)` (idempotent, `internal/ingestion/apitax.go:87`) but also inserts `windowStart − 1h` — in the first hour of a month UTC that row lands in an unensured month → SQLSTATE 23514. Same class: `handlers_quality_subtypes_test.go:174-182` inserts `now − i·h` into month-partitioned quality tables (migrations 000029/000159/000160/000161).
- `TestContractCompatListDisplayParity` (`internal/api/contract_test.go:624`) still pins the #600 contract "linkage dead → alias = UUID text". Commit `58cf7b6f` (#692, 2026-10-16) intentionally changed `internal/model/common.go:44-48` to `COALESCE(scalar-subquery alias, UUID)` and added its own regression test but did not update this one. **Fails locally with `expected UUID, actual "contract-cluster"`.** Root cause: stale expectation, not production drift.

## Global Constraints

- **Zero production-code changes.** Close criteria: "do not change correct whole-partition production retention."
- **Commits wait for owner go-ahead** (repo approval gate) — the commit steps below run only after explicit instruction.
- No `t.Skip`, no weakened assertions, no `try/except`-style guarding (repo behavioral rules).
- Every new/changed assertion carries its one-sentence "which pre-fix behavior this rules out" comment (adversarial-fixtures.md §1).
- Helper functions live in `_test.go` files only.
- Push target is the fork remote (`pgarciaq`); branch `pgarciaq-rosocp-superpowers-phase17`.

## Review Focus

1. **Impossible window** — `retentionMonths=1, historyDays=25`: the (now−1mo, now−25d) interval is ~5 days; no calendar month qualifies. The helper must return a clear error, never a wrong-side month. → Task 1 table test.
2. **Cutoff-class swap** — if the history sweep rode the digest months cutoff, the fixtures must fail naming the days-config mechanism. → Task 2 mutation check.
3. **Boundary-hour insert** — a window starting 00:30 UTC on the 1st with its −1h row in the previous month must seed cleanly (pre-fix: 23514). → Tasks 3 and 4 regression tests.
4. **Ensure-helper idempotency** — ensuring the same month twice (both windows inside one month) must not error. → Task 3 test case.
5. **Alias both branches** — an aliased cluster must surface the friendly name (detects alias-resolution loss) AND an orphan cluster must fall back to UUID (detects fallback loss). → Task 5 two-branch fixture.

---

### Task 1: Pure date-math helper for retention fixture months

**Files:**
- Create: `internal/engine/retention_fixture_math_test.go` (helper + table tests, package `engine`)

**Interfaces:**
- Produces: `retentionFixtureMonth(now time.Time, historyDays, retentionMonths int) (time.Time, error)` — returns a UTC month-start whose `YYYYMM` satisfies `digestCutoffYM ≤ YM < historyCutoffYM` where `digestCutoffYM = now.AddDate(0,-retentionMonths,0).Format("200601")` and `historyCutoffYM = now.AddDate(0,0,-historyDays).Format("200601")`. Error (no month picked) when no qualifying month exists.

- [ ] **Step 1: Write the failing table test**

```go
func TestRetentionFixtureMonth(t *testing.T) {
    // cases: (name, now, historyDays, retentionMonths, wantYMs or wantErr)
    // - "typical nov"      : 2026-11-02, 45, 3  → exactly one valid YM (202608); assert membership + both invariants
    // - "mid-month"        : 2026-11-15, 45, 3  → valid YM exists; assert invariants
    // - "month start"      : 2026-12-01 00:30, 45, 3 → assert invariants
    // - "year boundary"    : 2026-01-05, 45, 3  → valid YM spans prior year; assert invariants
    // - "leap feb"         : 2028-03-01, 45, 3  → assert invariants
    // - "impossible window": 2026-11-02, 25, 1  → want error mentioning no qualifying month
}
// Invariant assertions for every non-error case:
//   got.Format("200601") >= now.AddDate(0,-retentionMonths,0).Format("200601")  // months cutoff would KEEP it
//   got.Format("200601") <  now.AddDate(0,0,-historyDays).Format("200601")      // days cutoff sweeps it
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/engine/ -run TestRetentionFixtureMonth -count=1`
Expected: FAIL — `undefined: retentionFixtureMonth`

- [ ] **Step 3: Implement the helper in the same test file**

Algorithm: start at `monthStart(now.AddDate(0, 0, -historyDays))` and step back one month at a time while `YM < digestCutoffYM`, max 2 iterations (window ≤ ~62 days); if still `YM < digestCutoffYM` after stepping to `digestCutoff`'s own month, return the error. Always return the derived month-start via `time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)`.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/engine/ -run TestRetentionFixtureMonth -count=1 -v`
Expected: PASS, all cases.

- [ ] **Step 5: Commit** (after owner go-ahead)

```bash
git add internal/engine/retention_fixture_math_test.go
git commit -m "test(#779): retention fixture month helper — provably inside [months, days) cutoff window"
```

### Task 2: Apply the helper to both retention fixtures + mutation check

**Files:**
- Modify: `internal/engine/retention_history_trait_test.go:51-54` (PluginDeclaration test) and `:107-109` (HcpCapacity test)

**Interfaces:**
- Consumes: `retentionFixtureMonth` from Task 1.

- [ ] **Step 1: Replace the fragile month choice in both tests**

In each test, replace `old := time.Now().UTC().AddDate(0, -2, 0)` with:
`old, err := retentionFixtureMonth(time.Now().UTC(), 45, 3)` + `require.NoError(t, err)`. Keep the partition DDL and assertions untouched. Update each test's leading comment to keep the #701 narrative and add: "fixture month computed to lie wholly outside historyDays=45 and at/inside retentionMonths=3 on every calendar date."

- [ ] **Step 2: Run both tests — verify green**

Run: `go test ./internal/engine/ -run 'TestHistoryRetention_PluginDeclarationUsesDaysCutoff|TestHistoryRetention_HcpCapacityHistoryUsesDaysConfig' -count=1`
Expected: PASS (both were red at HEAD before Task 2).

- [ ] **Step 3: Mutation check — cutoff-class swap (adversarial-fixtures §2)**

Temporarily edit `internal/engine/retention.go:148` to `historyCutoff := time.Now().UTC().AddDate(0, -retentionMonths, 0)` (the months cutoff). Run both tests. Expected: both FAIL with the assertion message naming the days-config cutoff mechanism (e.g. "must sweep at the days-config cutoff"). Revert the mutation. If they pass under the mutation, the fixture is blind — stop and strengthen before proceeding.

- [ ] **Step 4: Weakened-fixture pass (adversarial-fixtures §3)**

With the fix held, mentally verify (and note in the PR description) that the fixture month is derived, not hardcoded — there is no insertion-order or lexicographic surface here; the masking catalog's relevant rows (`MAX` on text, `ON CONFLICT` seeds) do not apply to partition-existence assertions. State this explicitly in the #779 close-out comment.

- [ ] **Step 5: Commit** (after owner go-ahead)

```bash
git add internal/engine/retention_history_trait_test.go
git commit -m "test(#779): retention fixtures pick a month inside the [months, days) cutoff window on every date"
```

### Task 3: apitax seeder — ensure partitions for every inserted window

**Files:**
- Modify: `internal/engine/correlate/apitax_test.go:23` (inside `seedAPITaxWindow`)
- Test: new case in `internal/engine/correlate/apitax_test.go`

**Interfaces:**
- Consumes: `ingestion.EnsureAPITaxPartitionsForMonth(ctx, pool, monthStart)` (idempotent; `internal/ingestion/apitax.go:87`).

- [ ] **Step 1: Write the failing boundary regression test**

```go
// Pre-fix behavior ruled out: seeding a window whose -1h snapshot falls in the
// previous month fails with SQLSTATE 23514 (no partition) on the 1st, 00:00-01:00 UTC.
func TestSeedAPITaxWindow_MonthBoundary(t *testing.T) {
    pool := testutil.SetupTestDB(t)
    ctx := context.Background()
    // First hour of the current month: windowStart 00:30 on the 1st.
    n := time.Now().UTC()
    windowStart := time.Date(n.Year(), n.Month(), 1, 0, 30, 0, 0, time.UTC)
    seedAPITaxWindow(t, ctx, pool, "org-apitax-boundary", "cluster-b", "webhook-b", windowStart, 1.0, 100, 0)
    // count rows for both window_start values — both must exist (2 per LE bucket set)
}
```

Run: `go test ./internal/engine/correlate/ -run TestSeedAPITaxWindow_MonthBoundary -count=1`
Expected: FAIL — insert error naming the missing previous-month partition (23514 class).

- [ ] **Step 2: Fix the seeder**

In `seedAPITaxWindow`, ensure both inserted windows' months:
```go
require.NoError(t, ingestion.EnsureAPITaxPartitionsForMonth(ctx, pool, windowStart))
require.NoError(t, ingestion.EnsureAPITaxPartitionsForMonth(ctx, pool, windowStart.Add(-time.Hour)))
```
(The helper is idempotent — same-month double-call is a no-op; that is Review Focus 4 satisfied by this same test.)

- [ ] **Step 3: Run to verify green, plus the existing apitax suite**

Run: `go test ./internal/engine/correlate/ -count=1`
Expected: PASS (new test included).

- [ ] **Step 4: Commit** (after owner go-ahead)

```bash
git add internal/engine/correlate/apitax_test.go
git commit -m "test(#779): apitax seeder ensures partitions for both hourly windows incl. month boundary"
```

### Task 4: quality fixtures — ensure partitions for every seeded timestamp

**Files:**
- Modify: `internal/api/handlers_quality_subtypes_test.go` (`seedQualityBase` and the pagination test's insert loop)
- Test: new boundary case in the same file

**Interfaces:**
- Produces: `ensureQualityPartitions(ctx context.Context, pool *pgxpool.Pool, times ...time.Time)` test helper (same file) — for each distinct month in `times`, creates the month partition for every partitioned quality table. Verify at implementation which of `pvc_/vm_/gpu_mig_/snapshot_recommendation_quality` are `PARTITION BY RANGE` (000159/000160/000161 + the snapshot table's migration — read them before writing the helper) and cover exactly those, mirroring the `EnsureAPITaxPartitionsForMonth` existence-check pattern.

- [ ] **Step 1: Write the failing boundary regression test**

```go
// Pre-fix behavior ruled out: pagination-fixture rows at now-2h on the 1st
// (previous month) hit an unensured quality partition (SQLSTATE 23514).
func TestQualitySubtypes_MonthBoundarySeed(t *testing.T) {
    // seed base; n := time.Now().UTC(); boundary := time.Date(y, m, 1, 0, 30, 0, 0, time.UTC)
    // insert one pvc_recommendation_quality row at boundary.Add(-time.Hour)
    // require.NoError on the insert; then assert the row is queryable via the API
}
```

Run: `go test ./internal/api/ -run TestQualitySubtypes_MonthBoundarySeed -count=1`
Expected: FAIL — insert error naming the missing previous-month partition.

- [ ] **Step 2: Implement the helper; call it from the fixtures**

`seedQualityBase` gains the ensure call for the timestamps its callers insert: change the pagination test (and smoke test) to pass their insert times through `ensureQualityPartitions` before inserting. Keep every existing assertion untouched.

- [ ] **Step 3: Run the quality suite**

Run: `go test ./internal/api/ -run 'TestQualitySubtypes' -count=1`
Expected: PASS.

- [ ] **Step 4: Commit** (after owner go-ahead)

```bash
git add internal/api/handlers_quality_subtypes_test.go
git commit -m "test(#779): quality fixtures ensure partitions for every seeded timestamp incl. month boundary"
```

### Task 5: pin the shipped #692 alias contract in the compat-parity test

**Files:**
- Modify: `internal/api/contract_test.go:603-628` (`TestContractCompatListDisplayParity`)

**Interfaces:**
- Consumes: existing `setupContractTestApp` / `contractGET` helpers; seed pattern from `TestHCPList_ClusterAliasFromClustersTable` (`internal/api/handlers_hcp_test.go`, added in `58cf7b6f`).

- [ ] **Step 1: Update the test to cover both branches of the #692 contract**

Replace the single `assert.Equal(t, testutil.TestClusterUUID, item["cluster_alias"])` (line 624) with a two-cluster fixture:
- Cluster A (seeded `clusters` row with alias): `cluster_alias` must equal the friendly alias — rules out losing alias resolution (#692 regression).
- Cluster B (recommendation row whose UUID has no `clusters` row — orphan): `cluster_alias` must equal the UUID text — rules out losing the fallback (the #600 inheritance).
Update the leading comment: #600's "linkage dead → UUID fallback" is superseded by #692's "COALESCE(scalar subquery, UUID)"; keep the `source_id == ""` assertion (`common.go:37` still hardcodes it).

- [ ] **Step 2: Run to verify green**

Run: `go test ./internal/api/ -run TestContractCompatListDisplayParity -count=1`
Expected: PASS (was red at HEAD with `expected UUID, actual "contract-cluster"`).

- [ ] **Step 3: Mutation check**

Temporarily revert `internal/model/common.go:44-48` to the hardcoded `cluster_uuid::text AS cluster_alias`. Run the test. Expected: FAIL on cluster A's friendly-alias assertion (names the alias-resolution mechanism). Restore. (This is a production-file mutation for verification only — never committed.)

- [ ] **Step 4: Commit** (after owner go-ahead)

```bash
git add internal/api/contract_test.go
git commit -m "test(#779): compat display parity pins the #692 alias contract — friendly alias and UUID fallback both branches"
```

### Task 6: full verification and close-out

- [ ] **Step 1: Affected packages**

Run: `go test ./internal/engine/... ./internal/api/... ./internal/engine/correlate/... -count=1`
Expected: PASS.

- [ ] **Step 2: Full suite**

Run: `make test` (~30m, testcontainers)
Expected: PASS — this is the headline claim of #779; do not report success without this output.

- [ ] **Step 3: Comment on #779 with findings and close with the completion-report table**

Comment must state: compat-list root cause (#692 `58cf7b6f` left `contract_test.go:624` stale; CI red on that test since 2026-10-16), the mutation-check results from Tasks 2/5, and the verified/asserted distinction per the completion-report discipline. Then `gh issue close 779 --reason completed` with the table.

---

## Self-review notes

- **Spec coverage:** #779 close criterion 1 (partitions for every fixture timestamp) → Tasks 3-4; criterion 2 (retention partitions wholly on one side; no production change) → Tasks 1-2; criterion 3 (boundary cases without weakened assertions) → Tasks 1, 3, 4 boundary tests + Task 6 full suite; criterion 4 (compat-list separated and diagnosed) → Task 5 + Task 6 comment (diagnosis: stale expectation vs intentional #692 change).
- **Proportion:** no production code; five small test changes plus one helper. The plan names exact files, line ranges, signatures, and the failing output each step must produce.
