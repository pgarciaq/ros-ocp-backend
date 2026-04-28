# GPU Time-Slicing Recommendations — Implementation Plan

## Overview

Add per-node GPU time-slicing recommendations for non-MIG GPUs (T4, L4, L40,
L40S, A10) that are underutilized.  This is the first node-level recommendation
type, establishing the pattern for future node recs (instance type, reserved
instance, etc.).

Time-slicing recommendations are **mutually exclusive** with MIG
recommendations per container: if a GPU gets a MIG recommendation, it does not
get a time-slicing recommendation.  MIG provides hardware isolation; time-slicing
is temporal multiplexing with no memory isolation.

### Decision Summary

| Question | Decision |
|---|---|
| Aggregation scope | Per-node (natural action boundary for device plugin config) |
| Storage | New `node_recommendations` table (separate from `recommendation_sets`) |
| API shape | New endpoint `GET /api/cost-management/v1/recommendations/openshift/nodes` |
| Persistence | Computed at API-read time (matching existing GPU rec pattern), persisted later if perf requires |

---

## Phase 0: Ingest Node Name (prerequisite)

**Problem:** The ROS CSV has a `node` column (position 6), but the ros-ocp-backend
CSV parser does not map it.  `MetricRow`, `gpu_container_digests`, and
`recommendation_sets` all lack node information.

### Phase 0a: Add `node` to CSV parser and `MetricRow`

**File:** `internal/ingestion/models.go`

Add `Node string` field to `MetricRow`.

**File:** `internal/ingestion/csvparser.go`

Add `node int` to `csvColumnIndex`.  In `buildColumnIndex`, map `"node"` → `idx.node`.
In `parseRecord`, set `row.Node = optionalStringField(record, idx.node)`.

The `node` column is NOT required (older operator versions may not produce it);
treat it as optional with empty-string default.

**Tests:** `internal/ingestion/csvparser_test.go` — add test with and without `node`
column in header.

### Phase 0b: Store `node` in `gpu_container_digests`

**Migration:** `000044_add_node_to_gpu_digests.up.sql`

```sql
ALTER TABLE gpu_container_digests ADD COLUMN IF NOT EXISTS node_name TEXT DEFAULT '';
```

Down: `ALTER TABLE gpu_container_digests DROP COLUMN IF EXISTS node_name;`

**File:** `internal/ingestion/pipeline.go` — `upsertGPUDigests`

Include `node_name` in the INSERT and the ON CONFLICT UPDATE.  Source the value
from `MetricRow.Node`.

**Tests:** `internal/ingestion/pipeline_test.go` — verify node_name is persisted.

---

## Phase 1: Time-Slicing Engine Logic

### Phase 1a: Add `RecommendTimeslicing` function

**File:** `internal/engine/gpu_timeslicing.go` (new file)

```go
package engine

// TimeslicingRec holds the node-level GPU time-slicing recommendation.
type TimeslicingRec struct {
    NodeName             string
    ClusterUUID          string
    GPUModel             string
    RecommendedReplicas  int
    SavingsPerGPU        float32
    TotalNodeSavings     float32
    Confidence           float32
    CandidateContainers  []GPUContainerRef
    ImpactedContainers   []GPUContainerRef
    NotificationCodes    []int16
}

type GPUContainerRef struct {
    Namespace      string
    Workload       string
    Container      string
    SMActiveAvg    float32
    Classification GPUClassification
}
```

**Algorithm — `ComputeNodeTimeslicingRecs`:**

Input: a map of `node_name → []containerGPUInfo` where each entry has:
- namespace, workload, container
- the container's `*GPURec` (from `RecommendGPU`)
- the GPU's `*GPUModelSpec` (from `MatchGPUModel`)

For each node × GPU model combination:

1. **Partition containers** into candidates (classification is `underutilized` or
   `compute_bound_underutil`, AND no MIG recommendation was issued) and impacted
   (classification is `well_utilized` or has a MIG recommendation).

2. **Skip** if:
   - Zero candidates (nothing to recommend)
   - All containers are `idle` (recommend "remove GPU" instead, existing path)
   - GPU model is MIG-capable and MIG recommendations were issued for all containers

3. **Check majority threshold:** `len(candidates) / len(all_gpu_containers) >= 0.5`.
   If not met AND there are impacted containers, skip.  (If there are no
   impacted containers, proceed even below 50% — all GPUs are underutilized.)

4. **Compute replicas** from the average utilization of candidate containers:
   ```
   avg_sm   = mean(candidate SM_active_avg values)
   avg_dram = mean(candidate DRAM_active_avg values)
   avg_fb   = mean(candidate p98_FB / total_FB values)
   peak_utilization = max(avg_sm, avg_dram, avg_fb)
   replicas = floor(1.0 / peak_utilization)
   replicas = clamp(replicas, 2, 8)
   ```

5. **Compute savings** (requires cost data, per-GPU monthly rate):
   ```
   savings_per_gpu = rate * (1 - 1/replicas)
   total_savings   = savings_per_gpu * len(candidate_containers)
   ```
   (Only candidates contribute savings; impacted containers don't save anything.)

6. **Confidence:**
   ```
   base = average confidence across candidate GPURecs
   base *= 0.7                          # time-slicing penalty
   base *= (1 - 0.3 * len(impacted) / len(all_gpu))  # impacted containers penalty
   ```

7. **Notification codes:** `NotifGPUTimeSharingCandidate` (29) for the node rec.
   Each candidate container's `GPURec` also gets this code.

**Tests:** `internal/engine/gpu_timeslicing_test.go`

- Test with 4 underutilized T4s → expect replicas = N, savings calculated
- Test with 2 underutilized + 2 well-utilized → expect recommendation with confidence penalty
- Test with 1 underutilized + 3 well-utilized → expect skip (below majority)
- Test with all idle → expect skip (idle path handles it)
- Test with MIG-capable A100 underutilized but MIG recommended → expect skip
- Test with non-MIG T4 underutilized → expect time-slicing recommendation
- Test with zero GPU containers → expect nil
- Test replicas clamping: very low utilization → capped at 8; moderate → 2-4
- Test with no cost data → savings = nil

### Phase 1b: Add notification code

**File:** `internal/engine/notifications.go`

```go
NotifGPUTimeSharingCandidate int16 = 29
```

Also add notification message in the notification descriptions map (if one exists).

---

## Phase 2: Wire Time-Slicing Into GPU Enrichment

### Phase 2a: Extend `QueryGPURecommendations` to return node info

**File:** `internal/engine/gpu_query.go`

The current query doesn't SELECT `node_name`.  Add it:

```sql
SELECT interval_start, namespace, workload, container_name, node_name,
    ...
FROM gpu_container_digests
WHERE cluster_uuid = $1 AND interval_start >= $2 AND interval_start <= $3
ORDER BY namespace, workload, container_name, interval_start
```

Add `NodeName string` to the `GPUDigestRow` struct in `gpu_recommender.go`.

Change the return type to include node mapping.  Two options:

**Option A (preferred):** Return a second map alongside the existing one:
```go
func QueryGPURecommendations(ctx, pool, clusterUUID, start, end) (
    map[string]*GPURec,          // key: "ns/workload/container"
    map[string]string,           // key: "ns/workload/container" → node_name
    error,
)
```

**Option B:** Add `NodeName` to `GPURec` itself.  Simpler but mixes node info
into a per-container struct.

Go with Option A for cleanliness — `GPURec` stays a pure recommendation struct.

### Phase 2b: Compute node recs in `enrichWithGPU`

**File:** `internal/api/gpu_enrichment.go`

After computing per-container GPU recs (existing code), aggregate by node:

```go
// Build nodeContainers map: node_name → []containerGPUInfo
for key, gpuRec := range gpuRecs {
    nodeName := nodeMap[key]
    // ...build containerGPUInfo with namespace, workload, container, gpuRec, spec
}

// Compute node time-slicing recommendations
nodeRecs := engine.ComputeNodeTimeslicingRecs(nodeContainers, costData)
```

Store `nodeRecs` somewhere accessible to the API handler — either:
- Attach to the response directly (if the node endpoint queries independently)
- Or return from `enrichWithGPU` alongside the modified results

Since the node recommendations endpoint will be **separate** from the container
endpoint, the cleanest approach is a **separate enrichment function**:

```go
func queryNodeGPURecs(clusterUUIDs []string, orgID string) ([]model.NodeGPURecommendation, error)
```

This function:
1. For each cluster, calls `QueryGPURecommendations` (with node map)
2. Aggregates by node
3. Calls `ComputeNodeTimeslicingRecs`
4. Fetches cost data, calls `ApplyTimeslicingSavings`
5. Returns `[]model.NodeGPURecommendation`

---

## Phase 3: API Endpoint

### Phase 3a: Model types

**File:** `internal/model/node_recommendation.go` (new file)

```go
package model

type NodeGPURecommendation struct {
    NodeName              string                `json:"node_name"`
    ClusterUUID           string                `json:"cluster_uuid"`
    RecommendationType    string                `json:"recommendation_type"`
    GPUModel              string                `json:"gpu_model"`
    RecommendedReplicas   int                   `json:"recommended_replicas"`
    SavingsPerGPUUSD      *float32              `json:"savings_per_gpu_usd"`
    TotalNodeSavingsUSD   *float32              `json:"total_node_savings_usd"`
    Confidence            float32               `json:"confidence"`
    CandidateContainers   []NodeContainerRef    `json:"candidate_containers"`
    ImpactedContainers    []NodeContainerRef    `json:"impacted_containers"`
    NotificationCodes     []int16               `json:"notification_codes"`
}

type NodeContainerRef struct {
    Namespace      string  `json:"namespace"`
    Workload       string  `json:"workload"`
    Container      string  `json:"container"`
    SMActiveAvg    float32 `json:"sm_active_avg"`
    Classification string  `json:"classification"`
}

type NodeRecommendationListResponse struct {
    Meta NodeRecommendationMeta    `json:"meta"`
    Data []NodeGPURecommendation   `json:"data"`
}

type NodeRecommendationMeta struct {
    Count            int      `json:"count"`
    TotalSavingsUSD  *float32 `json:"total_savings_usd,omitempty"`
}
```

### Phase 3b: Handler

**File:** `internal/api/handlers_node_recs.go` (new file)

```go
func (s *Server) GetNodeRecommendations(c echo.Context) error {
    // 1. Extract org_id from identity header
    // 2. Parse query params: cluster_uuid, node_name, gpu_model,
    //    recommendation_type, min_savings_usd
    // 3. Get list of cluster UUIDs for this org
    //    (from existing clusters table or from container recs)
    // 4. Call queryNodeGPURecs(clusterUUIDs, orgID)
    // 5. Apply filters (node_name, gpu_model, min_savings)
    // 6. Return NodeRecommendationListResponse
}
```

Query parameters:

| Param | Type | Description |
|---|---|---|
| `cluster_uuid` | string | Filter by cluster |
| `node_name` | string | Filter by node name |
| `gpu_model` | string | Filter by GPU model (e.g., "T4") |
| `recommendation_type` | string | Filter by rec type (default: `gpu_time_slicing`) |
| `min_savings_usd` | float | Only return recs with total savings above threshold |

### Phase 3c: Route registration

**File:** `internal/api/server.go`

Add to the native engine route group:

```go
nativeGroup.GET("/recommendations/openshift/nodes", s.GetNodeRecommendations)
```

### Phase 3d: OpenAPI spec update

**File:** `api/openapi.json` (or wherever the spec lives)

Add:
- `/recommendations/openshift/nodes` GET endpoint
- `NodeGPURecommendation` schema
- `NodeContainerRef` schema
- `NodeRecommendationListResponse` schema

---

## Phase 4: Tests

### Unit tests

| File | Tests |
|---|---|
| `internal/engine/gpu_timeslicing_test.go` | Algorithm: replicas, savings, confidence, skip conditions |
| `internal/ingestion/csvparser_test.go` | Node column parsing (present and absent) |
| `internal/ingestion/pipeline_test.go` | node_name persisted in gpu_container_digests |
| `internal/api/handlers_node_recs_test.go` | API endpoint: filters, response shape, empty results |

### Integration / E2E tests

1. Generate nise data with GPU pods on named nodes (already done in example YAML)
2. Postprocess with `postprocess_ros_csvs.py` to assign GPU scenarios
3. Upload to Apollo
4. Call `GET /recommendations/openshift/nodes?cluster_uuid=...`
5. Verify:
   - Node with underutilized T4s → `recommended_replicas` between 2-8
   - Node with well-utilized A100s → no time-slicing recommendation
   - Candidate containers listed with correct SM values
   - Savings calculated when cost model has `gpu_cost_per_month` rate

---

## Phase 5: Documentation and Memory Dump

1. Update `docs/gpu-recommendations-plan.md` with time-slicing section
2. Update `docs/AGENT_MEMORY_DUMP.md` — close Gap 3 (time-slicing)
3. Update OpenAPI spec with new endpoint documentation
4. Add node recommendation section to the test plan

---

## Implementation Order

```
Phase 0a: MetricRow + CSV parser (node column)
Phase 0b: Migration + pipeline (node_name in gpu_container_digests)
    ↓
Phase 1a: TimeslicingRec struct + ComputeNodeTimeslicingRecs algorithm
Phase 1b: Notification code 29
    ↓
Phase 2a: QueryGPURecommendations returns node map
Phase 2b: queryNodeGPURecs enrichment function
    ↓
Phase 3a: API model types
Phase 3b: Handler
Phase 3c: Route registration
Phase 3d: OpenAPI spec
    ↓
Phase 4:  Tests (unit + integration)
Phase 5:  Documentation
```

Phases 0-1 can be committed independently.  Phases 2-3 are tightly coupled.
Phase 4 should be done alongside each phase.

---

## Estimated Effort

| Phase | Effort |
|---|---|
| Phase 0 (node ingestion) | ~1 hour |
| Phase 1 (engine logic) | ~2 hours |
| Phase 2 (wiring) | ~1.5 hours |
| Phase 3 (API) | ~1.5 hours |
| Phase 4 (tests) | ~2 hours |
| Phase 5 (docs) | ~30 min |

**Total: ~8.5 hours**

---

## Future Considerations

- **Persistence:** If API-read-time computation becomes slow for large clusters
  (hundreds of nodes), add a `node_recommendations` table and compute during the
  ingestion pipeline (after GPU digests are written).  The API then reads from
  the table.  This is a performance optimization, not a design change.

- **Node-level detail endpoint:** `GET /recommendations/openshift/nodes/:node-name`
  with full history of time-slicing recommendations over time.

- **Other node rec types:** Instance type recommendations and reserved instance
  recommendations follow the same `node_recommendations` table pattern with
  `recommendation_type = 'instance_type'` or `'reserved_instance'`.

- **UI:** Stefan's mockups will determine how node recs are displayed.  The API
  shape is designed to support both a dedicated "Node Recommendations" page and
  inline hints on the container recommendation detail view (via the
  `NotifGPUTimeSharingCandidate` notification code on per-container recs).
