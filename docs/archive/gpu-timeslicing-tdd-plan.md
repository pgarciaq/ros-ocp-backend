> **Status: Complete.** All TDD cycles (TS-01 through TS-21) have been implemented and tested.

# GPU Time-Slicing Recommendations — TDD Plan (Red-Green-Refactor)

This document structures the time-slicing implementation as strict TDD cycles.
Every production function is driven by a failing test written first.

**Parent docs:**
- Design: [gpu-recommendations.md](gpu-recommendations.md) (Phase E)
- Implementation: [../plans/gpu-time-slicing-persistence.md](../plans/gpu-time-slicing-persistence.md)
- Test inventory: [gpu-recommendations-test-plan.md](gpu-recommendations-test-plan.md) (Phase E)

**Conventions:**
- Each cycle is numbered: `TS-01`, `TS-02`, etc.
- RED = write the failing test. GREEN = minimal code to pass. REFACTOR = clean up.
- After each REFACTOR step, run `go test ./...` to confirm no regressions.
- Commit after each GREEN or REFACTOR that leaves the suite green.

---

## Cycle TS-01: Parse `node` column from CSV

**Goal:** `MetricRow` gets a `Node` field populated from the CSV `node` column.

### RED

**File:** `internal/ingestion/csvparser_test.go`

```go
func TestParseRecord_NodeColumn(t *testing.T) {
    // Build a CSV header that includes a "node" column (position 6 in real data)
    header := baseTestHeader() // existing helper
    header = append(header, "node")

    record := baseTestRecord()
    record = append(record, "gpu-worker-1")

    idx := buildColumnIndex(header)
    row, err := parseRecord(record, idx)
    require.NoError(t, err)
    assert.Equal(t, "gpu-worker-1", row.Node)
}

func TestParseRecord_NodeColumnMissing(t *testing.T) {
    // Older CSV without "node" column
    header := baseTestHeader()
    record := baseTestRecord()

    idx := buildColumnIndex(header)
    row, err := parseRecord(record, idx)
    require.NoError(t, err)
    assert.Equal(t, "", row.Node)
}
```

**Expected:** Compile error — `MetricRow` has no `Node` field.

### GREEN

1. **`internal/ingestion/models.go`**: Add `Node string` to `MetricRow` (after `Pod`).
2. **`internal/ingestion/csvparser.go`**: Add `node int` to `csvColumnIndex`.
   In `buildColumnIndex`, map `"node" → idx.node`. Initialize to `-1`.
   In `parseRecord`, set `row.Node = optionalStringField(record, idx.node)`.

### REFACTOR

- Ensure `node` follows the same optional-column pattern as
  `acceleratorModelName` (default `-1`, guarded read).
- Run `go test ./internal/ingestion/ -v`.

---

## Cycle TS-02: Store `node_name` in `gpu_container_digests`

**Goal:** The `upsertGPUDigests` pipeline writes `node_name` to the DB.

### RED

**File:** `internal/ingestion/gpu_digest_test.go` (or `pipeline_test.go`)

```go
func TestUpsertGPUDigests_StoresNodeName(t *testing.T) {
    // ... set up testcontainers DB, apply migrations ...
    rows := []MetricRow{gpuMetricRow(withNode("gpu-worker-1"))}
    err := upsertGPUDigests(ctx, pool, "org1234567", "cluster-uuid", rows)
    require.NoError(t, err)

    var nodeName string
    err = pool.QueryRow(ctx,
        `SELECT node_name FROM gpu_container_digests
         WHERE cluster_uuid = $1 LIMIT 1`, "cluster-uuid").Scan(&nodeName)
    require.NoError(t, err)
    assert.Equal(t, "gpu-worker-1", nodeName)
}
```

**Expected:** `column "node_name" does not exist` (migration not applied yet).

### GREEN

1. **Migration `000044_add_node_to_gpu_digests.up.sql`:**
   ```sql
   ALTER TABLE gpu_container_digests ADD COLUMN IF NOT EXISTS node_name TEXT DEFAULT '';
   ```
   Down: `ALTER TABLE gpu_container_digests DROP COLUMN IF EXISTS node_name;`

2. **`internal/ingestion/pipeline.go`** (`upsertGPUDigests`): Add `node_name`
   to the INSERT columns and the ON CONFLICT UPDATE set. Source from
   `MetricRow.Node`.

### REFACTOR

- Verify migration roundtrip: up then down leaves no trace.
- Run `go test ./internal/ingestion/ -v -timeout 120s`.

---

## Cycle TS-03: Notification code 29

**Goal:** `NotifGPUTimeSharingCandidate` constant exists.

### RED

**File:** `internal/engine/gpu_timeslicing_test.go` (new file)

```go
package engine

import "testing"

func TestNotifGPUTimeSharingCandidate_Exists(t *testing.T) {
    // Verify the constant is defined and has the expected value
    assert.Equal(t, int16(29), NotifGPUTimeSharingCandidate)
}
```

**Expected:** Compile error — `NotifGPUTimeSharingCandidate` undefined.

### GREEN

**File:** `internal/engine/notifications.go`

```go
NotifGPUTimeSharingCandidate int16 = 29
```

### REFACTOR

- Nothing to refactor; run `go test ./internal/engine/ -run TestNotif -v`.

---

## Cycle TS-04: `TimeslicingRec` and `GPUContainerRef` types

**Goal:** Define the result types that `ComputeNodeTimeslicingRecs` will return.

### RED

**File:** `internal/engine/gpu_timeslicing_test.go`

```go
func TestTimeslicingRec_ZeroValue(t *testing.T) {
    var rec TimeslicingRec
    assert.Equal(t, "", rec.NodeName)
    assert.Equal(t, 0, rec.RecommendedReplicas)
    assert.Nil(t, rec.SavingsPerGPU)
    assert.Empty(t, rec.CandidateContainers)
    assert.Empty(t, rec.ImpactedContainers)
}
```

**Expected:** Compile error — `TimeslicingRec` undefined.

### GREEN

**File:** `internal/engine/gpu_timeslicing.go` (new file)

```go
package engine

type TimeslicingRec struct {
    NodeName            string
    ClusterUUID         string
    GPUModel            string
    RecommendedReplicas int
    SavingsPerGPU       *float32
    TotalNodeSavings    *float32
    Confidence          float32
    CandidateContainers []GPUContainerRef
    ImpactedContainers  []GPUContainerRef
    NotificationCodes   []int16
}

type GPUContainerRef struct {
    Namespace      string
    Workload       string
    Container      string
    SMActiveAvg    float32
    Classification GPUClassification
}
```

### REFACTOR

- Confirm types compile with all existing tests passing.

---

## Cycle TS-05: `computeReplicas` — pure function, clamped [2, 8]

**Goal:** Isolate the replicas formula as a testable pure function before
integrating it into the larger algorithm.

### RED

**File:** `internal/engine/gpu_timeslicing_test.go`

```go
func TestComputeReplicas(t *testing.T) {
    tests := []struct {
        name     string
        smAvg    float32
        dramAvg  float32
        fbFrac   float32
        wantReps int
        wantOK   bool  // false if peak util too high (replicas < 2)
    }{
        {"very_low_util",   0.05, 0.03, 0.02, 8, true},    // floor(1/0.05)=20, clamped to 8
        {"moderate_util",   0.20, 0.15, 0.10, 5, true},     // floor(1/0.20)=5
        {"higher_util",     0.40, 0.30, 0.20, 2, true},     // floor(1/0.40)=2
        {"too_high_util",   0.55, 0.30, 0.20, 0, false},    // floor(1/0.55)=1, below 2
        {"dram_dominates",  0.10, 0.40, 0.10, 2, true},     // peak=0.40, floor(1/0.40)=2
        {"fb_dominates",    0.10, 0.10, 0.60, 1, false},    // peak=0.60, floor(1/0.60)=1 < 2
        {"exact_50pct",     0.50, 0.20, 0.10, 2, true},     // floor(1/0.50)=2
        {"all_zero",        0.00, 0.00, 0.00, 8, true},     // edge: 1/0 -> clamp to 8
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            reps, ok := computeReplicas(tt.smAvg, tt.dramAvg, tt.fbFrac)
            assert.Equal(t, tt.wantOK, ok)
            if ok {
                assert.Equal(t, tt.wantReps, reps)
            }
        })
    }
}
```

**Expected:** Compile error — `computeReplicas` undefined.

### GREEN

**File:** `internal/engine/gpu_timeslicing.go`

```go
func computeReplicas(avgSM, avgDRAM, avgFBFrac float32) (replicas int, ok bool) {
    peak := avgSM
    if avgDRAM > peak { peak = avgDRAM }
    if avgFBFrac > peak { peak = avgFBFrac }

    if peak <= 0 {
        return 8, true // all-zero edge case
    }
    r := int(1.0 / peak)
    if r < 2 { return 0, false }
    if r > 8 { r = 8 }
    return r, true
}
```

### REFACTOR

- Consider extracting the clamp to a generic `clampInt(v, lo, hi)` if one
  doesn't exist.
- Run `go test ./internal/engine/ -run TestComputeReplicas -v`.

---

## Cycle TS-06: `computeTimeslicingConfidence`

**Goal:** Isolate the confidence formula.

### RED

**File:** `internal/engine/gpu_timeslicing_test.go`

```go
func TestComputeTimeslicingConfidence(t *testing.T) {
    tests := []struct {
        name         string
        avgCandConf  float32
        nImpacted    int
        nTotal       int
        wantConf     float32
    }{
        {"no_impacted",      0.8, 0, 4, 0.56},  // 0.8*0.7*(1-0)=0.56
        {"one_impacted_of4", 0.8, 1, 4, 0.518},  // 0.8*0.7*(1-0.3*0.25)=0.518
        {"half_impacted",    0.8, 2, 4, 0.476},  // 0.8*0.7*(1-0.3*0.5)=0.476
        {"all_impacted",     0.8, 4, 4, 0.392},  // 0.8*0.7*(1-0.3*1.0)=0.392
        {"low_base_conf",    0.3, 0, 2, 0.21},   // 0.3*0.7=0.21
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := computeTimeslicingConfidence(tt.avgCandConf, tt.nImpacted, tt.nTotal)
            assert.InDelta(t, tt.wantConf, got, 0.01)
        })
    }
}
```

**Expected:** Compile error — `computeTimeslicingConfidence` undefined.

### GREEN

**File:** `internal/engine/gpu_timeslicing.go`

```go
func computeTimeslicingConfidence(avgCandidateConf float32, nImpacted, nTotal int) float32 {
    const basePenalty = 0.7
    impactedRatio := float32(nImpacted) / float32(nTotal)
    return avgCandidateConf * basePenalty * (1.0 - 0.3*impactedRatio)
}
```

### REFACTOR

- Constants `0.7` and `0.3` could be named (`timeslicingBasePenalty`,
  `impactedContainerPenaltyWeight`) if readability warrants it.
- Run `go test ./internal/engine/ -run TestComputeTimeslicingConfidence -v`.

---

## Cycle TS-07: `computeTimeslicingSavings`

**Goal:** Per-GPU and total-node savings calculation.

### RED

**File:** `internal/engine/gpu_timeslicing_test.go`

```go
func TestComputeTimeslicingSavings(t *testing.T) {
    rate := float32(300.0)
    t.Run("4_replicas_3_candidates", func(t *testing.T) {
        perGPU, total := computeTimeslicingSavings(4, 3, &rate)
        require.NotNil(t, perGPU)
        require.NotNil(t, total)
        assert.InDelta(t, 225.0, *perGPU, 0.01)  // 300*(1-1/4)
        assert.InDelta(t, 675.0, *total, 0.01)    // 225*3
    })
    t.Run("no_cost_data", func(t *testing.T) {
        perGPU, total := computeTimeslicingSavings(4, 3, nil)
        assert.Nil(t, perGPU)
        assert.Nil(t, total)
    })
    t.Run("2_replicas_1_candidate", func(t *testing.T) {
        perGPU, total := computeTimeslicingSavings(2, 1, &rate)
        require.NotNil(t, perGPU)
        assert.InDelta(t, 150.0, *perGPU, 0.01)  // 300*(1-1/2)
        assert.InDelta(t, 150.0, *total, 0.01)    // 150*1
    })
}
```

**Expected:** Compile error — `computeTimeslicingSavings` undefined.

### GREEN

**File:** `internal/engine/gpu_timeslicing.go`

```go
func computeTimeslicingSavings(replicas, nCandidates int, gpuMonthlyRate *float32) (perGPU, total *float32) {
    if gpuMonthlyRate == nil {
        return nil, nil
    }
    pg := *gpuMonthlyRate * (1.0 - 1.0/float32(replicas))
    tot := pg * float32(nCandidates)
    return &pg, &tot
}
```

### REFACTOR

- Run `go test ./internal/engine/ -run TestComputeTimeslicingSavings -v`.

---

## Cycle TS-08: `partitionContainers` — separate candidates from impacted

**Goal:** The function that classifies GPU containers on a node into
`candidates` (time-slicing applicable) and `impacted` (collateral).

### RED

**File:** `internal/engine/gpu_timeslicing_test.go`

```go
// containerInput is a test helper to build input for partitionContainers.
type containerInput struct {
    key            string  // "ns/workload/container"
    classification GPUClassification
    hasMIGRec      bool
    smActiveAvg    float32
}

func TestPartitionContainers(t *testing.T) {
    t.Run("mixed_node", func(t *testing.T) {
        inputs := []containerInput{
            {"ns/wl/c1", GPUClassUnderutilized, false, 0.12},
            {"ns/wl/c2", GPUClassUnderutilized, false, 0.08},
            {"ns/wl/c3", GPUClassWellUtilized, false, 0.67},
        }
        candidates, impacted := partitionContainers(inputs)
        assert.Len(t, candidates, 2)
        assert.Len(t, impacted, 1)
    })
    t.Run("idle_excluded", func(t *testing.T) {
        inputs := []containerInput{
            {"ns/wl/c1", GPUClassIdle, false, 0.01},
            {"ns/wl/c2", GPUClassUnderutilized, false, 0.12},
        }
        candidates, impacted := partitionContainers(inputs)
        assert.Len(t, candidates, 1)  // idle excluded from candidates
        assert.Len(t, impacted, 0)    // idle not counted as impacted either
    })
    t.Run("memory_bound_excluded", func(t *testing.T) {
        inputs := []containerInput{
            {"ns/wl/c1", GPUClassMemoryBound, false, 0.30},
            {"ns/wl/c2", GPUClassUnderutilized, false, 0.12},
        }
        candidates, impacted := partitionContainers(inputs)
        assert.Len(t, candidates, 1)
        assert.Len(t, impacted, 0)    // memory_bound excluded, not impacted
    })
    t.Run("mig_rec_takes_precedence", func(t *testing.T) {
        inputs := []containerInput{
            {"ns/wl/c1", GPUClassUnderutilized, true, 0.12},  // has MIG rec
            {"ns/wl/c2", GPUClassUnderutilized, false, 0.10},
        }
        candidates, impacted := partitionContainers(inputs)
        assert.Len(t, candidates, 1)  // only c2
        assert.Len(t, impacted, 0)    // c1 with MIG is excluded, not impacted
    })
}
```

**Expected:** Compile error — `partitionContainers` undefined.

### GREEN

**File:** `internal/engine/gpu_timeslicing.go`

```go
func partitionContainers(inputs []containerInput) (candidates, impacted []containerInput) {
    for _, c := range inputs {
        switch {
        case c.classification == GPUClassIdle:
            // Excluded: separate "remove GPU" path handles idle
        case c.classification == GPUClassMemoryBound:
            // Excluded: sharing with memory-bound workloads is risky
        case c.hasMIGRec:
            // Excluded: MIG takes precedence
        case c.classification == GPUClassUnderutilized ||
             c.classification == GPUClassComputeBoundUnderutil:
            candidates = append(candidates, c)
        default:
            impacted = append(impacted, c)
        }
    }
    return
}
```

### REFACTOR

- Decide whether `containerInput` should be the test helper or the real type.
  If real, rename to something like `nodeGPUContainerInfo` and put in
  `gpu_timeslicing.go`. If test-only, keep in `_test.go` and have the real
  function take `[]GPURec + node mapping` instead.
- Run full suite.

---

## Cycle TS-08b: `avgCandidateUtilization` — derives FB fraction from specs

**Goal:** Bridge the gap between raw `FBUsageMaxMiB` and the `fbFrac`
parameter that `computeReplicas` expects.  This function computes average
SM, DRAM, and FB fraction across candidate containers, using `GPUModelSpec`
to derive the FB fraction.

### RED

**File:** `internal/engine/gpu_timeslicing_test.go`

```go
func TestAvgCandidateUtilization(t *testing.T) {
    // T4 has 16384 MiB total FB
    candidates := []nodeGPUContainer{
        {Rec: &GPURec{SMActiveAvg: 0.12, DRAMActiveAvg: 0.08, FBUsageMaxMiB: 4096}},
        {Rec: &GPURec{SMActiveAvg: 0.18, DRAMActiveAvg: 0.10, FBUsageMaxMiB: 8192}},
    }
    totalFBMiB := float32(16384) // from GPUModelSpec

    avgSM, avgDRAM, avgFBFrac := avgCandidateUtilization(candidates, totalFBMiB)
    assert.InDelta(t, 0.15, avgSM, 0.01)     // (0.12+0.18)/2
    assert.InDelta(t, 0.09, avgDRAM, 0.01)   // (0.08+0.10)/2
    assert.InDelta(t, 0.375, avgFBFrac, 0.01) // ((4096+8192)/2)/16384
}

func TestAvgCandidateUtilization_ZeroTotalFB(t *testing.T) {
    candidates := []nodeGPUContainer{
        {Rec: &GPURec{SMActiveAvg: 0.12, DRAMActiveAvg: 0.08, FBUsageMaxMiB: 4096}},
    }
    avgSM, avgDRAM, avgFBFrac := avgCandidateUtilization(candidates, 0)
    assert.InDelta(t, 0.12, avgSM, 0.01)
    assert.InDelta(t, 0.08, avgDRAM, 0.01)
    assert.InDelta(t, 0.0, avgFBFrac, 0.01) // avoid div-by-zero
}
```

**Expected:** Compile error — `avgCandidateUtilization` undefined.

### GREEN

**File:** `internal/engine/gpu_timeslicing.go`

```go
func avgCandidateUtilization(candidates []nodeGPUContainer, totalFBMiB float32) (avgSM, avgDRAM, avgFBFrac float32) {
    if len(candidates) == 0 {
        return 0, 0, 0
    }
    var sumSM, sumDRAM, sumFB float32
    for _, c := range candidates {
        sumSM += c.Rec.SMActiveAvg
        sumDRAM += c.Rec.DRAMActiveAvg
        sumFB += c.Rec.FBUsageMaxMiB
    }
    n := float32(len(candidates))
    avgSM = sumSM / n
    avgDRAM = sumDRAM / n
    if totalFBMiB > 0 {
        avgFBFrac = (sumFB / n) / totalFBMiB
    }
    return
}
```

### REFACTOR

- `ComputeNodeTimeslicingRec` (TS-09) will call this instead of inlining
  the math.
- Run `go test ./internal/engine/ -run TestAvgCandidate -v`.

---

## Cycle TS-08c: `isNodeFresh` — 7-day freshness check

**Goal:** Skip nodes that haven't been seen in 7 days (decommissioned or
rescheduled).

### RED

**File:** `internal/engine/gpu_timeslicing_test.go`

```go
func TestIsNodeFresh(t *testing.T) {
    now := time.Now().UTC()
    t.Run("seen_today", func(t *testing.T) {
        assert.True(t, isNodeFresh(now.AddDate(0, 0, -1), now))
    })
    t.Run("seen_6_days_ago", func(t *testing.T) {
        assert.True(t, isNodeFresh(now.AddDate(0, 0, -6), now))
    })
    t.Run("seen_7_days_ago", func(t *testing.T) {
        assert.True(t, isNodeFresh(now.AddDate(0, 0, -7), now))
    })
    t.Run("seen_8_days_ago", func(t *testing.T) {
        assert.False(t, isNodeFresh(now.AddDate(0, 0, -8), now))
    })
    t.Run("seen_30_days_ago", func(t *testing.T) {
        assert.False(t, isNodeFresh(now.AddDate(0, 0, -30), now))
    })
}
```

**Expected:** Compile error — `isNodeFresh` undefined.

### GREEN

**File:** `internal/engine/gpu_timeslicing.go`

```go
const nodeFreshnessDays = 7

func isNodeFresh(lastSeen, now time.Time) bool {
    return now.Sub(lastSeen) <= time.Duration(nodeFreshnessDays)*24*time.Hour
}
```

### REFACTOR

- `ComputeNodeTimeslicingRec` (or its caller) will check `isNodeFresh`
  before computing recommendations for a node group.
- Run `go test ./internal/engine/ -run TestIsNodeFresh -v`.

---

## Cycle TS-09: `ComputeNodeTimeslicingRecs` — happy path

**Goal:** The main orchestrator function for a single node.  Calls
`partitionContainers`, `avgCandidateUtilization`, `computeReplicas`,
`computeTimeslicingSavings`, `computeTimeslicingConfidence`.

### RED

**File:** `internal/engine/gpu_timeslicing_test.go`

```go
func TestComputeNodeTimeslicingRecs_HappyPath(t *testing.T) {
    gpuRate := float32(300.0)
    input := nodeGPUGroup{
        NodeName:    "gpu-t4-worker-1",
        ClusterUUID: "cluster-1",
        GPUModel:    "T4",
        Containers: []nodeGPUContainer{
            {Namespace: "ns", Workload: "wl", Container: "c1",
             Rec: &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.12, Confidence: 0.8}},
            {Namespace: "ns", Workload: "wl", Container: "c2",
             Rec: &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.08, Confidence: 0.8}},
            {Namespace: "ns", Workload: "wl", Container: "c3",
             Rec: &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.18, Confidence: 0.8}},
            {Namespace: "ns", Workload: "wl", Container: "c4",
             Rec: &GPURec{Classification: GPUClassWellUtilized, SMActiveAvg: 0.67, Confidence: 0.8}},
        },
    }

    rec := ComputeNodeTimeslicingRec(input, &gpuRate)
    require.NotNil(t, rec)
    assert.Equal(t, "gpu-t4-worker-1", rec.NodeName)
    assert.Equal(t, "T4", rec.GPUModel)
    assert.GreaterOrEqual(t, rec.RecommendedReplicas, 2)
    assert.LessOrEqual(t, rec.RecommendedReplicas, 8)
    assert.Len(t, rec.CandidateContainers, 3)
    assert.Len(t, rec.ImpactedContainers, 1)
    assert.Contains(t, rec.NotificationCodes, NotifGPUTimeSharingCandidate)
    require.NotNil(t, rec.SavingsPerGPU)
    assert.Greater(t, *rec.SavingsPerGPU, float32(0))
    assert.Greater(t, rec.Confidence, float32(0))
    assert.Less(t, rec.Confidence, float32(1.0))
}
```

**Expected:** Compile error — `ComputeNodeTimeslicingRec`, `nodeGPUGroup`,
`nodeGPUContainer` undefined.

### GREEN

**File:** `internal/engine/gpu_timeslicing.go`

Define `nodeGPUGroup`, `nodeGPUContainer` input types.
Implement `ComputeNodeTimeslicingRec(group nodeGPUGroup, gpuRate *float32) *TimeslicingRec`:

1. Call `partitionContainers`
2. Check majority threshold (>=50% or zero impacted)
3. Compute avg SM/DRAM/FB from candidates
4. Call `computeReplicas`
5. Call `computeTimeslicingSavings`
6. Call `computeTimeslicingConfidence`
7. Build and return `TimeslicingRec`

### REFACTOR

- Extract the "compute avg utilization across candidates" into a helper
  `avgCandidateUtilization()`.
- Run `go test ./internal/engine/ -run TestComputeNodeTimeslicing -v`.

---

## Cycle TS-10: `ComputeNodeTimeslicingRec` — skip: majority not met

### RED

```go
func TestComputeNodeTimeslicingRec_SkipBelowMajority(t *testing.T) {
    input := nodeGPUGroup{
        NodeName: "node-1", ClusterUUID: "c1", GPUModel: "T4",
        Containers: []nodeGPUContainer{
            {Rec: &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.12, Confidence: 0.8}},
            {Rec: &GPURec{Classification: GPUClassWellUtilized, SMActiveAvg: 0.67, Confidence: 0.8}},
            {Rec: &GPURec{Classification: GPUClassWellUtilized, SMActiveAvg: 0.55, Confidence: 0.8}},
            {Rec: &GPURec{Classification: GPUClassWellUtilized, SMActiveAvg: 0.72, Confidence: 0.8}},
        },
    }
    rec := ComputeNodeTimeslicingRec(input, nil)
    assert.Nil(t, rec) // 1/4 = 25%, below 50% threshold
}
```

### GREEN

Already handled by majority check in TS-09. Should pass immediately ("born green").
If it doesn't, fix the threshold logic.

### REFACTOR

- Nothing needed if it passed.

---

## Cycle TS-11: `ComputeNodeTimeslicingRec` — skip: all idle

### RED

```go
func TestComputeNodeTimeslicingRec_SkipAllIdle(t *testing.T) {
    input := nodeGPUGroup{
        NodeName: "node-idle", ClusterUUID: "c1", GPUModel: "T4",
        Containers: []nodeGPUContainer{
            {Rec: &GPURec{Classification: GPUClassIdle, SMActiveAvg: 0.01, Confidence: 0.8}},
            {Rec: &GPURec{Classification: GPUClassIdle, SMActiveAvg: 0.005, Confidence: 0.8}},
        },
    }
    rec := ComputeNodeTimeslicingRec(input, nil)
    assert.Nil(t, rec) // idle containers are excluded; 0 candidates
}
```

### GREEN

Should pass from TS-09 logic (idle → excluded → 0 candidates → nil).

---

## Cycle TS-12: `ComputeNodeTimeslicingRec` — skip: all MIG

### RED

```go
func TestComputeNodeTimeslicingRec_SkipAllMIG(t *testing.T) {
    input := nodeGPUGroup{
        NodeName: "node-mig", ClusterUUID: "c1", GPUModel: "A100",
        Containers: []nodeGPUContainer{
            {Rec: &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.10,
                RecommendedGPUProfile: "3g.40gb", Confidence: 0.8}},
            {Rec: &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.15,
                RecommendedGPUProfile: "1g.10gb", Confidence: 0.8}},
        },
    }
    rec := ComputeNodeTimeslicingRec(input, nil)
    assert.Nil(t, rec) // both have MIG recs → excluded → 0 candidates
}
```

### GREEN

Requires `partitionContainers` to check `rec.RecommendedGPUProfile != ""`
as the MIG indicator.  Adjust `hasMIGRec` derivation from the `GPURec`.

### REFACTOR

- Decide how `hasMIGRec` is derived: from `GPURec.RecommendedGPUProfile != ""`
  and `!= "full_gpu"`.  Add a helper `GPURec.HasMIGRecommendation() bool`.

---

## Cycle TS-13: `ComputeNodeTimeslicingRec` — all underutilized, no impacted

### RED

```go
func TestComputeNodeTimeslicingRec_AllUnderutilized(t *testing.T) {
    rate := float32(300.0)
    input := nodeGPUGroup{
        NodeName: "node-all-under", ClusterUUID: "c1", GPUModel: "T4",
        Containers: []nodeGPUContainer{
            {Rec: &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.20, Confidence: 0.8}},
            {Rec: &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.20, Confidence: 0.8}},
        },
    }
    rec := ComputeNodeTimeslicingRec(input, &rate)
    require.NotNil(t, rec)
    assert.Empty(t, rec.ImpactedContainers)
    // Confidence should have NO impacted penalty, only the 0.7 base
    assert.InDelta(t, 0.8*0.7, rec.Confidence, 0.01)
}
```

### GREEN

Should pass from TS-09 logic if impacted penalty correctly handles `nImpacted=0`.

---

## Cycle TS-14: `ComputeNodeTimeslicingRec` — mixed GPU models grouped separately

### RED

```go
func TestComputeNodeTimeslicingRecs_MultipleGPUModels(t *testing.T) {
    rate := float32(300.0)
    groups := []nodeGPUGroup{
        {NodeName: "mixed-node", ClusterUUID: "c1", GPUModel: "T4",
         Containers: []nodeGPUContainer{
            {Rec: &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.15, Confidence: 0.8}},
            {Rec: &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.10, Confidence: 0.8}},
        }},
        {NodeName: "mixed-node", ClusterUUID: "c1", GPUModel: "L4",
         Containers: []nodeGPUContainer{
            {Rec: &GPURec{Classification: GPUClassWellUtilized, SMActiveAvg: 0.60, Confidence: 0.8}},
        }},
    }

    var results []*TimeslicingRec
    for _, g := range groups {
        if r := ComputeNodeTimeslicingRec(g, &rate); r != nil {
            results = append(results, r)
        }
    }
    assert.Len(t, results, 1)         // only T4 group qualifies
    assert.Equal(t, "T4", results[0].GPUModel)
}
```

### GREEN

Should pass — the caller is responsible for grouping by node×model.
This test validates the caller pattern, not `ComputeNodeTimeslicingRec` itself.

### REFACTOR

- If a batch function `ComputeAllNodeTimeslicingRecs(groups []nodeGPUGroup, ...)`
  is desired, add it now with a simple loop. Otherwise, leave it to the API layer.

---

## Cycle TS-15: Notification code 29 on candidate `GPURec`s

**Goal:** After computing time-slicing, the candidate containers' `GPURec`
objects should also receive notification code 29.

### RED

```go
func TestComputeNodeTimeslicingRec_SetsNotifOnCandidates(t *testing.T) {
    rec1 := &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.12, Confidence: 0.8}
    rec2 := &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.10, Confidence: 0.8}
    input := nodeGPUGroup{
        NodeName: "node-1", ClusterUUID: "c1", GPUModel: "T4",
        Containers: []nodeGPUContainer{
            {Rec: rec1},
            {Rec: rec2},
        },
    }
    ComputeNodeTimeslicingRec(input, nil)
    assert.Contains(t, rec1.NotificationCodes, NotifGPUTimeSharingCandidate)
    assert.Contains(t, rec2.NotificationCodes, NotifGPUTimeSharingCandidate)
}
```

### GREEN

In `ComputeNodeTimeslicingRec`, after computing the recommendation, iterate
over candidate containers and append `NotifGPUTimeSharingCandidate` to each
container's `GPURec.NotificationCodes`.

### REFACTOR

- Run full `./internal/engine/` suite.

---

## Cycle TS-16: `GPUDigestRow` gets `NodeName`

**Goal:** The `QueryGPURecommendations` data path needs `NodeName` on the
digest row struct.

### RED

**File:** `internal/engine/gpu_recommender_test.go` (or `gpu_query_test.go`)

```go
func TestGPUDigestRow_HasNodeName(t *testing.T) {
    row := GPUDigestRow{NodeName: "gpu-worker-1"}
    assert.Equal(t, "gpu-worker-1", row.NodeName)
}
```

**Expected:** Compile error — `GPUDigestRow` has no `NodeName` field.

### GREEN

**File:** `internal/engine/gpu_recommender.go`

Add `NodeName string` to `GPUDigestRow`.

### REFACTOR

- Update `QueryGPURecommendations` SQL to `SELECT node_name` and scan it.

---

## Cycle TS-17: `QueryGPURecommendations` returns node map

**Goal:** The function returns both per-container `GPURec` and a node mapping.

### RED

**File:** `internal/engine/gpu_query_test.go` (integration test, needs DB)

```go
func TestQueryGPURecommendations_ReturnsNodeMap(t *testing.T) {
    // Seed gpu_container_digests with node_name = "gpu-worker-1"
    // ... testcontainers setup ...
    recs, nodeMap, err := QueryGPURecommendations(ctx, pool, clusterUUID, start, end)
    require.NoError(t, err)
    for key := range recs {
        assert.NotEmpty(t, nodeMap[key], "node map should have entry for %s", key)
    }
}
```

**Expected:** Compile error — `QueryGPURecommendations` returns 2 values not 3.

### GREEN

**File:** `internal/engine/gpu_query.go`

Change return signature to `(map[string]*GPURec, map[string]string, error)`.
Scan `node_name` from the query, populate the node map.

### REFACTOR

- Update all callers of `QueryGPURecommendations` (currently `gpu_enrichment.go`)
  to accept the third return value.  For now the callers can ignore it with `_`.
- Run `go test ./...` to verify no compile errors across the project.

---

## Cycle TS-18: API model types

**Goal:** `NodeGPURecommendation` and friends for the API response.

### RED

**File:** `internal/model/node_recommendation_test.go` (new file)

```go
func TestNodeGPURecommendation_JSONRoundtrip(t *testing.T) {
    rec := NodeGPURecommendation{
        NodeName:            "gpu-worker-1",
        ClusterUUID:         "abc-123",
        RecommendationType:  "gpu_time_slicing",
        GPUModel:            "T4",
        RecommendedReplicas: 4,
        Confidence:          0.65,
    }
    data, err := json.Marshal(rec)
    require.NoError(t, err)
    assert.Contains(t, string(data), `"node_name":"gpu-worker-1"`)
    assert.Contains(t, string(data), `"recommended_replicas":4`)
}
```

**Expected:** Compile error — `NodeGPURecommendation` undefined.

### GREEN

**File:** `internal/model/node_recommendation.go` (new file)

Define `NodeGPURecommendation`, `NodeContainerRef`,
`NodeRecommendationListResponse`, `NodeRecommendationMeta` with JSON tags.

### REFACTOR

- Run `go test ./internal/model/ -v`.

---

## Cycle TS-19: API handler + route

**Goal:** `GET /recommendations/openshift/gpu/timeslicing` returns 200 with correct shape.

### RED

**File:** `internal/api/handlers_node_recs_test.go` (new file, integration test)

```go
func TestGetNodeRecommendations_Empty(t *testing.T) {
    // ... testcontainers + httptest setup (follow existing handler test pattern) ...
    resp := httptest.NewRecorder()
    req := httptest.NewRequest("GET", "/api/cost-management/v1/recommendations/openshift/gpu/timeslicing", nil)
    req.Header.Set("x-rh-identity", testIdentity)
    router.ServeHTTP(resp, req)

    assert.Equal(t, 200, resp.Code)
    var body model.NodeRecommendationListResponse
    err := json.Unmarshal(resp.Body.Bytes(), &body)
    require.NoError(t, err)
    assert.Equal(t, 0, body.Meta.Count)
    assert.Empty(t, body.Data)
}
```

**Expected:** 404 — route not registered.

### GREEN

1. **`internal/api/handlers_node_recs.go`** (new file): Implement
   `GetNodeRecommendations` handler skeleton returning empty list.
2. **`internal/api/server.go`**: Register route.

### REFACTOR

- Run integration test suite.

---

## Cycle TS-20: API handler with seeded data

**Goal:** Handler returns actual time-slicing recommendations when
`gpu_container_digests` has data.

### RED

**File:** `internal/api/handlers_node_recs_test.go`

```go
func TestGetNodeRecommendations_WithData(t *testing.T) {
    // Seed gpu_container_digests for "gpu-t4-worker-1" with 3 underutilized T4s
    // ... insert test data ...
    resp := httptest.NewRecorder()
    req := httptest.NewRequest("GET",
        "/api/cost-management/v1/recommendations/openshift/gpu/timeslicing", nil)
    req.Header.Set("x-rh-identity", testIdentity)
    router.ServeHTTP(resp, req)

    assert.Equal(t, 200, resp.Code)
    var body model.NodeRecommendationListResponse
    require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
    assert.Equal(t, 1, body.Meta.Count)
    assert.Equal(t, "gpu-t4-worker-1", body.Data[0].NodeName)
    assert.Equal(t, "T4", body.Data[0].GPUModel)
    assert.GreaterOrEqual(t, body.Data[0].RecommendedReplicas, 2)
}
```

### GREEN

Wire the handler to:
1. Query clusters for the org
2. Call `QueryGPURecommendations` (returns recs + node map)
3. Group by node × GPU model into `[]nodeGPUGroup`
4. For each group, call `ComputeNodeTimeslicingRec`
5. Convert to `[]model.NodeGPURecommendation`
6. Return as JSON

### REFACTOR

- Extract the "group by node × GPU model" logic into a helper
  `groupByNodeAndModel(recs, nodeMap, specs)`.
- Run full suite.

---

## Cycle TS-21: API filters (node_name, gpu_model)

### RED

```go
func TestGetNodeRecommendations_FilterByNodeName(t *testing.T) {
    // Seed data for 2 nodes
    // GET ?node_name=gpu-t4-worker-1
    // Assert only 1 result
}

func TestGetNodeRecommendations_FilterByGPUModel(t *testing.T) {
    // Seed data for T4 and L4 nodes
    // GET ?gpu_model=T4
    // Assert only T4 results
}
```

### GREEN

Add query param parsing and post-computation filtering in the handler.

### REFACTOR

- Extract filter logic into `filterNodeRecs(recs, params)`.

---

## Summary: Execution Order

| Cycle | What it drives | File(s) created/modified |
|---|---|---|
| TS-01 | CSV `node` column parsing | `csvparser.go`, `models.go` |
| TS-02 | DB persistence of `node_name` | migration 000044, `pipeline.go` |
| TS-03 | Notification constant | `notifications.go` |
| TS-04 | Result types | `gpu_timeslicing.go` (new) |
| TS-05 | `computeReplicas` | `gpu_timeslicing.go` |
| TS-06 | `computeTimeslicingConfidence` | `gpu_timeslicing.go` |
| TS-07 | `computeTimeslicingSavings` | `gpu_timeslicing.go` |
| TS-08 | `partitionContainers` | `gpu_timeslicing.go` |
| TS-08b | `avgCandidateUtilization` (FB fraction derivation) | `gpu_timeslicing.go` |
| TS-08c | `isNodeFresh` (7-day freshness check) | `gpu_timeslicing.go` |
| TS-09 | `ComputeNodeTimeslicingRec` happy path | `gpu_timeslicing.go` |
| TS-10 | Skip: below majority | (test only — born green) |
| TS-11 | Skip: all idle | (test only — born green) |
| TS-12 | Skip: all MIG | `gpu_timeslicing.go` + `gpu_recommender.go` helper |
| TS-13 | All underutilized, no impacted | (test only — born green) |
| TS-14 | Multi-model grouping | (test validates caller pattern) |
| TS-15 | Notif 29 on candidate GPURecs | `gpu_timeslicing.go` |
| TS-16 | `GPUDigestRow.NodeName` | `gpu_recommender.go` |
| TS-17 | `QueryGPURecommendations` node map | `gpu_query.go` |
| TS-18 | API model types | `node_recommendation.go` (new) |
| TS-19 | API handler + route (empty) | `handlers_node_recs.go` (new), `server.go` |
| TS-20 | API handler with real data | `handlers_node_recs.go`, enrichment wiring |
| TS-21 | API filters | `handlers_node_recs.go` |

### Rules

1. **Never write production code without a failing test first.**
   The only exception is struct definitions needed for the test to compile
   (TS-04), where the test asserts zero-value behavior.

2. **Each GREEN step is the minimum code to pass.**
   No speculative features, no "while I'm here" additions.

3. **REFACTOR only when tests are green.**
   Extract helpers, rename, restructure — but never change behavior.

4. **Commit after each GREEN or REFACTOR.**
   Commit message: `TDD TS-XX: <one-line description>`.

5. **Run `go test ./...` after every REFACTOR.**
   Any regression means the refactor introduced a bug — revert and retry.

6. **Integration tests (TS-02, TS-17, TS-19, TS-20, TS-21) require testcontainers.**
   Run them with `-timeout 120s`.  Skip in CI if Docker is unavailable
   (`if testing.Short() { t.Skip("requires Docker") }`).

---

## Implementation Status

All 21 TDD cycles (TS-01 through TS-21) are **complete**.

Additional post-TDD work:

| Feature | Status |
|---|---|
| RBAC: cluster + node filtering for `/gpu/timeslicing` | **Done** |
| Pagination: limit, offset, order_by via listoptions | **Done** |
| ResourceNode type in `rbac/query_builder.go` | **Done** |
| 7-day node freshness check wired into `ComputeNodeTimeslicingRec` | **Done** |
| `QueryGPURecommendations` returns `nodeLastSeen` map (4th return) | **Done** |
| OpenAPI spec updated with pagination params + links schema | **Done** |
| E-T17: Container cross-reference (`time_slicing_node`, `time_slicing_replicas`) | **Done** |

### Test counts by package

| Package | Unit tests | Integration tests |
|---|---|---|
| `internal/engine/` | 33 (timeslicing + existing) | — |
| `internal/api/` | 34 (filter, sort, pagination, RBAC, cross-ref) | 18 (endpoint, org isolation, RBAC, pagination) |
| `internal/model/` | 3 (JSON roundtrip, pagination meta) | — |
| `internal/rbac/` | 4 (ResourceNode, wildcard, disabled) | — |
| `internal/ingestion/` | 1 (node column parsing) | — |
