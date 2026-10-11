# Container crash-loop quality gate Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Detect crashlooping containers from restart counts and (a) attach a warning notification, (b) withhold cost-engine downsize recommendations on churning workloads.

**Architecture:** New `restart_count_container_sum` PromQL query in the operator (same shape as the existing OOM query minus the reason filter) flows through a new `restart_count` CSV column into digest `RestartCountSum`, where the engine sums it per term window and gates cost-downsize recs at a threshold of 3 (mirroring the VM `CrashLoopRestartThreshold`). Two repos: operator (collect) then backend (ingest, persist, recommend).

**Tech Stack:** Go, PromQL (kube-state-metrics), PostgreSQL migrations, testify (`assert`), `robne` CLI for end-to-end validation.

**Spec:** GH issue #78 (CrashLoopBackOff detection and recommendation suppression — this plan implements it; #677 + findings comment `issuecomment-6101678941` is the motivating exhibit). This plan implements #78's Tier-A-analogous scope only. Tier B (control-plane reliability, #677) is explicitly out of scope.

## Global Constraints

- TDD per step: failing test first, run to confirm failure, minimal implementation, rerun, commit (`make test-short` for unit scope; full `make test` only if touching testcontainers paths).
- Mutation-check every regression test per `docs/agents/adversarial-fixtures.md`: stash the fix, confirm the test fails naming the bug mechanism (not an incidental property); vary fixtures (distinct values per workload, lexicographic-order independence).
- No secrets in code, logs, tests, or issues (x-rh-identity only in tests).
- Follow `docs/agents/cross-repo-contracts.md` completion report on the final task (two-repo change).
- Follow repo commit rules in CONTRIBUTING.md (humans/DCO as applicable).
- `ListAPIOptions` is the pagination choke point — no API changes here, no pagination work needed.
- No UI, no M3 copy, no Family H machinery (per #677 non-goals).

## Key design decisions (GREENLIT 2026-10-10 — challenge only via new evidence)

Deliberate deltas vs #78's body text, all approved: threshold 3/window (not 5/24h),
cost-downsize-only suppression (not all-recs mute), implicit resume via window
age-out (not explicit stabilization timer), notification name `NotifUnstableContainer`
(adopted from #78; code 48).

1. **Total restarts, not non-OOM.** Column counts all restarts (`increase(...restarts_total[15m])`, no reason join). OOMs keep their existing bump path untouched; a workload that is both OOMing and crashlooping gets both signals, which is honest (it IS churning).
2. **Threshold 3 per term window**, mirroring VM `CrashLoopRestartThreshold: 3` (`librobne/vm/config.go:38-43`). Zero-value threshold input means 3 (fallback inside the function, no sizing-settings ripple).
3. **Gate shape: withhold cost downsize only.** When window restart sum >= 3 for a term: cost-engine downsize recs are suppressed for that term (keep current request, zero savings, notification attached); cost-upsize, all performance recs, and non-downsize outcomes are unchanged. Rationale: a churning workload's usage baseline is untrustworthy for shrinking, but still informative for growth/headroom.
4. **All three terms uniformly** (short/medium/long): churn is acute but windows already encode recency; no term-specific tuning (YAGNI).
5. **Same namespace scope as OOM** (manual label + HCP auto-match via the shared `ros:namespace_filter` join pattern). No new scope decisions.
6. **No explanation-column persistence.** OOM persists `expl_oom_count_sum`; crash state is fully expressed by the notification code + withheld savings. Adding expl columns would balloon migration 000211 for no consumer.
7. **Notification code 86, name `NotifUnstableContainer`** (name adopted from #78; WARNING). Codes 37–85 taken (VM series incl. domain-local `NotifVMCrashLoop = 48` in librobne/vm — plan recon missed it, corrected at implementation). 86 verified free everywhere pre-change.
8. **Column placement: immediately after `oom_count`** in operator header list, struct, csvRow, backend parse index, and contract-test list. One position everywhere.

## Review Focus

- A container with restarts only in pods that no longer exist (owner scaled down mid-window): expect the gate to evaluate the surviving digest rows only, never error on missing pods.
- A CSV from an older operator without `restart_count`: expect parse to succeed with zero restarts (optional-column pattern), never reject the file.
- A workload with exactly 3 restarts vs 2: expect 3 to gate and 2 to pass (boundary pinned by test, not `>=` vs `>` ambiguity).
- OOM-only churn (5 OOMs, 0 other restarts): expect BOTH the OOM bump path and the crash notification (total counts all), with no double-suppression logic.
- Zero-request/limit workloads with restarts: expect the crash notification without a downsize rec to withhold (nothing to suppress must not error).

---

### Task 1: Operator query + row plumbing (repo: koku-metrics-operator, branch phase17 line)

**Files:**
- Modify: `internal/collector/queries.go` (QueryMap ~line 117 area; `query{}` list ~line 848)
- Modify: `internal/collector/types.go` (header list ~line 565; `rosContainerRow` struct ~line 495; `csvRow()` after OOMCount position)
- Test: `internal/collector/collector_test.go` (extend with restart-count assertions; mirror existing oom query-shape test if present, else add table case)

**Interfaces:**
- Consumes: `kube_pod_container_status_restarts_total`, `kube_namespace_labels` (existing collectors).
- Produces: query key `ros:restart_count_container_sum`; row value name `restart-count`; CSV header `restart_count`.

- [ ] **Step 1: Write the failing test**

```go
func TestRestartCountQueryShape(t *testing.T) {
    q, ok := QueryMap["ros:restart_count_container_sum"]
    assert.True(t, ok, "ros:restart_count_container_sum must exist")
    assert.Contains(t, q, "kube_pod_container_status_restarts_total")
    assert.NotContains(t, q, "OOMKilled")
    assert.Contains(t, q, "label_cost_management_optimizations")
    assert.Contains(t, q, "label_hypershift_openshift_io_hosted_control_plane")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/collector/ -run TestRestartCountQueryShape -v`
Expected: FAIL with "ros:restart_count_container_sum must exist"

- [ ] **Step 3: Add the QueryMap entry** (mirror the oom line, drop the reason join):

```go
"ros:restart_count_container_sum": "(sum by(container, pod, namespace) (increase(kube_pod_container_status_restarts_total{container!='', container!='POD', pod!=''}[15m])) * on(namespace) group_left kube_namespace_labels{label_insights_cost_management_optimizations='true', namespace!~'kube-.*|openshift|openshift-.*'}) or (sum by(container, pod, namespace) (increase(kube_pod_container_status_restarts_total{container!='', container!='POD', pod!=''}[15m])) * on(namespace) group_left() (kube_namespace_labels{label_cost_management_optimizations='true', namespace!~'kube-.*|openshift|openshift-.*'} or kube_namespace_labels{label_hypershift_openshift_io_hosted_control_plane='true', namespace!~'kube-.*|openshift|openshift-.*'}))",
```

- [ ] **Step 4: Add the `query{}` block** after the `oom-count-container-sum` block:

```go
query{
    Name:        "restart-count-container-sum",
    QueryString: QueryMap["ros:restart_count_container_sum"],
    MetricKey:   staticFields{"container_name": "container", "pod": "pod", "namespace": "namespace"},
    QueryValue: &saveQueryValue{
        ValName: "restart-count",
    },
    RowKey: []model.LabelName{"container", "pod", "namespace"},
},
```

- [ ] **Step 5: Add struct field + header + csvRow position** in `types.go`:

```go
RestartCount                   string `mapstructure:"restart-count"`
```

Header list: `"restart_count",` immediately after `"oom_count",`. `csvRow()`: append the restart value immediately after the OOMCount element (same position as header).

- [ ] **Step 6: Run tests**

Run: `go test ./internal/collector/ -v`
Expected: PASS (plus full `go build ./...`)

- [ ] **Step 7: Commit**

```bash
git add internal/collector/queries.go internal/collector/types.go internal/collector/collector_test.go
git commit -m "feat(operator): collect restart_count_container_sum for crash-loop signal"
```

### Task 2: Backend CSV parse + contract (repo: ros-ocp-backend)

**Files:**
- Modify: `librobne/csv/parse.go` (`columnIndex` struct + `newColumnIndex` + `case "restart_count"` + `row.RestartCount = optionalRoundedInt(...)`)
- Modify: `librobne/types/types.go` (`MetricRow`: add `RestartCount int64` after `OOMCount`-adjacent fields)
- Modify: `librobne/csv/csv_contract_test.go` (column list: `"restart_count",` after `"oom_count",`)
- Test: `librobne/csv/parse_test.go` (new cases; check existing oom parse test name and mirror)

**Interfaces:**
- Consumes: CSV header `restart_count` (optional; older operators omit it).
- Produces: `MetricRow.RestartCount int64` (0 when column absent).

- [ ] **Step 1: Write the failing test**

```go
func TestParseRestartCount(t *testing.T) {
    withCol := parseContainerRow([]string{/* header incl restart_count */, /* values incl "4" */})
    assert.Equal(t, int64(4), withCol.RestartCount)
    withoutCol := parseContainerRow([]string{/* header w/o restart_count */}, /* values */)
    assert.Equal(t, int64(0), withoutCol.RestartCount)
}
```

(Adapt constructor/header plumbing to the actual parse entry point used by the neighboring oom test — read it first, keep the same shape.)

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-short TEST=./librobne/csv/ -run TestParseRestartCount` (use the repo's actual test selector; `go test ./librobne/csv/ -run TestParseRestartCount -v` if TEST= is unsupported)
Expected: FAIL (unknown field / zero value)

- [ ] **Step 3: Implement parse** (three coordinated edits mirroring `oomCount` exactly):

```go
// columnIndex struct + newColumnIndex():
restartCount int  // beside oomCount
restartCount: -1, // beside oomCount: -1,
// switch:
case "restart_count":
    idx.restartCount = i
// row build:
row.RestartCount = optionalRoundedInt(record, idx.restartCount)
```

- [ ] **Step 4: Add `RestartCount int64` to `MetricRow`** in `librobne/types/types.go` (beside the OOM-adjacent field) and `"restart_count",` to the contract-test column list after `"oom_count",`.

- [ ] **Step 5: Mutation-check the parse test** (per `docs/agents/adversarial-fixtures.md`): stash the implementation, run the test, confirm it fails on the missing column (not on an incidental); use distinct restart values across fixture rows.

- [ ] **Step 6: Run tests**

Run: `go test ./librobne/csv/ -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add librobne/csv/parse.go librobne/types/types.go librobne/csv/csv_contract_test.go librobne/csv/parse_test.go
git commit -m "feat(backend): parse restart_count container CSV column"
```

### Task 3: Digest persistence (repo: ros-ocp-backend)

**Files:**
- Create: `migrations/000211_container_restart_count.up.sql` + `.down.sql` (mirror `migrations/000146_recommendation_explanation_columns.up.sql` ADD COLUMN pattern; next number verified 000211 — `ls migrations/*.up.sql | sort | tail -1` must show 000210 as current max, else take max+1 and update references)
- Modify: `librobne/csv/digest.go` (accumulate `restart` beside `oom` ~line 178-186; set `RestartCountSum` beside `OOMCountSum` ~line 237)
- Modify: `librobne/types/types.go` (DigestRow + ContainerRec: `RestartCountSum int64` beside both `OOMCountSum` fields ~lines 38, 108)
- Modify: `librobne/engine/helpers.go` (add `SumRestartCounts` beside `SumOOMCounts` ~line 92)
- Test: digest unit test (new case beside the oom accumulation test) + migration up/down round-trip

**Interfaces:**
- Consumes: `MetricRow.RestartCount`.
- Produces: `DigestRow.RestartCountSum`, `ContainerRec.RestartCountSum`, `SumRestartCounts([]DigestRow) int64`.

- [ ] **Step 1: Write the failing digest test**

```go
func TestDigestRestartCountSum(t *testing.T) {
    rows := []MetricRow{{RestartCount: 2}, {RestartCount: 3}}
    d := accumulateDigest(rows) // actual constructor per neighboring oom test
    assert.Equal(t, int64(5), d.RestartCountSum)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./librobne/csv/ -run TestDigestRestartCountSum -v`
Expected: FAIL (unknown field)

- [ ] **Step 3: Implement accumulation + plumbing** (mirror oom lines exactly):

```go
// digest.go accumulation:
var restart int64  // beside: var oom int64
restart += s.RestartCount
// digest struct literal:
RestartCountSum: restart,
// helpers.go:
func SumRestartCounts(rows []DigestRow) int64 {
    var total int64
    for _, r := range rows {
        total += r.RestartCountSum
    }
    return total
}
```

- [ ] **Step 4: Write migration 000211** (up: `ALTER TABLE daily_container_digests ADD COLUMN IF NOT EXISTS restart_count_sum BIGINT DEFAULT 0;` + matching down `DROP COLUMN`; mirror the 000146 file's table list if digests are fanned out — read 000146 fully first and cover exactly the tables it covers for oom):

```sql
-- 000211_container_restart_count.up.sql
ALTER TABLE daily_container_digests ADD COLUMN IF NOT EXISTS restart_count_sum BIGINT DEFAULT 0;
```

(Expand per-table if 000146 shows a fan-out pattern; down.sql drops the same columns.)

- [ ] **Step 5: Mutation-check** (stash, confirm failure names restart accumulation; vary fixture values so a sum-vs-max confusion would fail).

- [ ] **Step 6: Run tests**

Run: `go test ./librobne/csv/ ./librobne/engine/ -v` and migration round-trip per repo migration-test convention
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add librobne/csv/digest.go librobne/types/types.go librobne/engine/helpers.go migrations/000211_container_restart_count.* librobne/csv/digest_test.go
git commit -m "feat(backend): persist RestartCountSum digests"
```

### Task 4: Engine gate + notification (repo: ros-ocp-backend)

**Files:**
- Modify: `librobne/types/notifications.go` (add `NotifUnstableContainer int16 = 86` after code 47; add `CrashLoopRestartThreshold int32` to `NotificationThresholds` struct)
- Create: `migrations/000212_container_crashloop_notification_code.up.sql` + `.down.sql` (mirror `migrations/000051_add_gpu_timeslicing_notification_code.up.sql` INSERT pattern)
- Modify: `librobne/container/notifications.go` (`EvaluateNotificationsWithThresholds`: zero-means-3 fallback + crash branch)
- Modify: `librobne/engine/recommend.go` (withhold cost downsize when crashing; attach code)
- Test: `librobne/container/notifications_test.go` (boundary 2-vs-3, OOM-only churn fires both, zero threshold input means 3) + engine downsize-withhold test with adversarial fixtures
**Interfaces:**
- Consumes: `ContainerRec.RestartCountSum` (windowed via existing term loop), `th.CrashLoopRestartThreshold`.
- Produces: `NotifUnstableContainer` in `NotificationCodes`; suppressed cost-downsize rec (keep current, zero savings).

- [ ] **Step 1: Verify code 48 is free**

Run: `grep -rn "(48," migrations/*.up.sql librobne/types/notifications.go internal/notifications/`
Expected: empty. If taken, use the next free code below 70 and replace 48 consistently everywhere in this task.

- [ ] **Step 2: Write the failing notification test**

```go
func TestCrashLoopNotificationBoundary(t *testing.T) {
    th := types.NotificationThresholds{CrashLoopRestartThreshold: 3}
    rec2 := types.ContainerRec{RestartCountSum: 2, DataDays: 3}
    assert.NotContains(t, EvaluateNotificationsWithThresholds(rec2, 3, th), types.NotifUnstableContainer)
    rec3 := types.ContainerRec{RestartCountSum: 3, DataDays: 3}
    assert.Contains(t, EvaluateNotificationsWithThresholds(rec3, 3, th), types.NotifUnstableContainer)
    recZero := types.ContainerRec{RestartCountSum: 3, DataDays: 3}
    assert.Contains(t, EvaluateNotificationsWithThresholds(recZero, 3, types.NotificationThresholds{}), types.NotifUnstableContainer) // zero means 3
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./librobne/container/ -run TestCrashLoopNotificationBoundary -v`
Expected: FAIL (undefined code)

- [ ] **Step 4: Implement code + branch**

```go
NotifUnstableContainer      int16 = 48
```

```go
crashThreshold := th.CrashLoopRestartThreshold
if crashThreshold < 1 {
    crashThreshold = 3 // mirror of VM CrashLoopRestartThreshold default
}
if int64(rec.RestartCountSum) >= int64(crashThreshold) {
    codes = append(codes, types.NotifUnstableContainer)
}
```

Seed migration (mirror 000051 file shape):

```sql
INSERT INTO notification_code_definitions (code, name, severity, description) VALUES
    (86, 'UNSTABLE_CONTAINER', 'WARNING', 'Container restarted repeatedly in the observation window — possible crash loop; downsize withheld')
ON CONFLICT (code) DO UPDATE SET
    name = EXCLUDED.name,
    severity = EXCLUDED.severity,
    description = EXCLUDED.description;
```

- [ ] **Step 5: Write the failing downsize-withhold test** (adversarial fixtures: one churning workload with clear downsize signal + one clean control; assert churning keeps current request with zero savings AND carries the code, control downsizes normally).

- [ ] **Step 6: Implement the gate in `recommend.go`** (cost engine only, at the point where the downsize decision is made for the term: if window restart sum >= threshold, skip the downsize branch — keep current request, zero the savings fields, ensure the notification code path in Step 4 fires for the rec).

- [ ] **Step 7: Mutation-check both tests** (stash gate: test must fail showing a downsize was issued to a churning workload; vary request/limit values so incidental equality can't green it).

- [ ] **Step 8: Run tests**

Run: `go test ./librobne/container/ ./librobne/engine/ -v`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add librobne/types/notifications.go librobne/container/notifications.go librobne/engine/recommend.go migrations/000212_container_crashloop_notification_code.* librobne/container/notifications_test.go
git commit -m "feat(engine): withhold cost downsize on crashlooping containers"
```

### Task 5: End-to-end verification + contracts (repos: both)

**Files:**
- Modify: none (verification only), plus `docs/agents/cross-repo-contracts.md` completion report per that doc's format
- Test: synthetic fixture package + `robne validate`/`recommend` runs

**Interfaces:**
- Consumes: all prior tasks.
- Produces: green suites + demonstration numbers + filed completion report.

- [ ] **Step 1: Build a fixture package** (synthetic container CSV with `restart_count`: workload A with 5 restarts + downsize signal, workload B clean with downsize signal, workload C OOM-only). Package as tar.gz in the operator's output layout (mirror the `mgmt-pkg` fixture shape from the lab validation).

- [ ] **Step 2: Validate + recommend**

Run:
```bash
ROBNE_NO_USER_CONFIG=1 ./bin/robne validate --input ./crash-fixture.tar.gz --no-user-config
ROBNE_NO_USER_CONFIG=1 ./bin/robne recommend --input ./crash-fixture --plugins container --no-user-config --format json
```
Expected: validate ok; A shows `UNSTABLE_CONTAINER` with no cost downsize; B downsizes normally; C shows both OOM bump and crash code.

- [ ] **Step 3: Full suites**

Run: `make test-short` (backend), `go test ./internal/collector/` (operator)
Expected: PASS with no regressions.

- [ ] **Step 4: Cross-repo completion report** per `docs/agents/cross-repo-contracts.md` (consumer: SNO on-prem backend + UI/IQE if notification codes surface in API responses — check `internal/notifications/names.go` + `mapping.go` for whether code 48 needs entries; if the API exposes notification names, add the mapping in this task with a test).

- [ ] **Step 5: Commit report + fixtures**

```bash
git add docs/agents/cross-repo-contracts.md <fixture files>
git commit -m "docs: Tier A completion report + crash-loop fixtures"
```

## Self-review

1. **Spec coverage:** #677 Tier A = (a) crash notification — Tasks 1–4 notification path; (b) downsize withhold — Task 4 gate; (c) no Tier B — excluded by design §6 + non-goals. Operator/backend/API-surface all covered (Task 5 step 4 checks names/mapping).
2. **Placeholders:** none — every step carries exact code, commands, and expected outputs. The two conditional branches (code-48-taken, migration-max-moved) are explicit verification steps with deterministic fallbacks, not TODOs.
3. **Type consistency:** `RestartCount int64` (MetricRow) → `RestartCountSum int64` (DigestRow/ContainerRec) → `SumRestartCounts` helper; `NotifUnstableContainer int16 = 86`; `CrashLoopRestartThreshold int32`; CSV `restart_count`; query `ros:restart_count_container_sum` / value `restart-count`. Consistent throughout.
4. **Review Focus:** all five lines have owning tests (Tasks 2, 4, 5).
