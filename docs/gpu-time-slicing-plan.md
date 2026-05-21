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

**Routes (native engine):** GPU time-slicing is served at **`GET /api/cost-management/v1/recommendations/openshift/gpu/timeslicing`** (`GetNodeRecommendations`). Tier 1 node CPU/memory utilization recommendations use **`GET .../recommendations/openshift/nodes`** (`GetNodeUtilizationRecs`) — a different response type and handler.

### Decision Summary


| Question          | Decision                                                                                        |
| ----------------- | ----------------------------------------------------------------------------------------------- |
| Aggregation scope | Per-node (natural action boundary for device plugin config)                                     |
| Storage           | New `node_recommendations` table (separate from `recommendation_sets`)                          |
| API shape         | Canonical endpoint `GET /api/cost-management/v1/recommendations/openshift/gpu/timeslicing` (GPU time-slicing only; historically also exposed under `/nodes` before the split) |
| Persistence       | Computed at API-read time (matching existing GPU rec pattern), persisted later if perf requires |


---

## Phase 0: Ingest Node Name (prerequisite) — DONE

**Status:** Fully implemented.

`MetricRow.Node` is populated by the CSV parser (`csvparser.go`), `node_name`
is stored in `gpu_container_digests` (migration 000044), and
`QueryGPURecommendations` returns a node map alongside GPU recommendations.
The `node` column is optional — older operator versions without it produce
empty-string defaults.

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


| Param                 | Type   | Description                                         |
| --------------------- | ------ | --------------------------------------------------- |
| `cluster_uuid`        | string | Filter by cluster                                   |
| `node_name`           | string | Filter by node name                                 |
| `gpu_model`           | string | Filter by GPU model (e.g., "T4")                    |
| `recommendation_type` | string | Filter by rec type (default: `gpu_time_slicing`)    |
| `min_savings_usd`     | float  | Only return recs with total savings above threshold |


### Phase 3c: Route registration

**File:** `internal/api/server.go`

Add to the native engine route group:

```go
nativeGroup.GET("/recommendations/openshift/gpu/timeslicing", s.GetNodeRecommendations)
```

### Phase 3d: OpenAPI spec update

**File:** `api/openapi.json` (or wherever the spec lives)

Add:

- `/recommendations/openshift/gpu/timeslicing` GET endpoint (GPU time-slicing)
- `/recommendations/openshift/gpu` GET endpoint (optional summary of GPU listings)
- `NodeGPURecommendation` schema
- `NodeContainerRef` schema
- `NodeRecommendationListResponse` schema

---

## Phase 4: Tests (TDD)

**This phase uses strict TDD red-green-refactor.** See
[plans/gpu-timeslicing-tdd-plan.md](plans/gpu-timeslicing-tdd-plan.md) for the
full 21-cycle TDD plan with exact test code and execution order.

### Summary of test files

| File                                      | Tests                                                     |
| ----------------------------------------- | --------------------------------------------------------- |
| `internal/engine/gpu_timeslicing_test.go` | Algorithm: replicas, savings, confidence, skip conditions |
| `internal/ingestion/csvparser_test.go`    | Node column parsing (present and absent)                  |
| `internal/ingestion/pipeline_test.go`     | node_name persisted in gpu_container_digests              |
| `internal/api/handlers_node_recs_test.go` | API endpoint: filters, response shape, empty results      |
| `internal/model/node_recommendation_test.go` | JSON serialization roundtrip                           |

### Integration / E2E tests

1. Generate nise data with GPU pods on named nodes (already done in example YAML)
2. Postprocess with `postprocess_ros_csvs.py` to assign GPU scenarios
3. Upload to Apollo
4. Call `GET /recommendations/openshift/gpu/timeslicing?cluster_uuid=...`
5. Verify:
  - Node with underutilized T4s → `recommended_replicas` between 2-8
  - Node with well-utilized A100s → no time-slicing recommendation
  - Candidate containers listed with correct SM values
  - Savings calculated when cost model has `gpu_cost_per_month` rate

---

## Phase 5: Pagination, RBAC, and Documentation

### Phase 5a: Pagination (limit, offset, order_by)

The `/gpu/timeslicing` endpoint supports the same `listoptions` pattern as all other
list endpoints for consistency:

- `limit` (default 10, -1 for unlimited)
- `offset` (default 0)
- `order_by` (allowed: `node_name`, `cluster_uuid`, `gpu_model`,
  `recommended_replicas`, `confidence`, `total_node_savings`)
- `order_how` (`asc` or `desc`, default `desc`)

Since node recommendations are computed in-memory (not via SQL), sorting
and pagination are applied in Go after all recommendations are computed.
The response uses the same `meta.count`/`meta.limit`/`meta.offset`/`links`
shape as the standard `CollectionResponse`.

**Files:**
- `internal/api/listoptions/list_options.go` — `NodeRecsAllowedOrderBy` map,
  `DefaultNodeRecsOrderBy` constant
- `internal/api/handlers_node_recs.go` — `sortNodeRecs`, `applyNodePagination`,
  `buildNodeLinks` helpers
- `internal/model/node_recommendation.go` — `NodeRecommendationMeta` gains
  `Limit`/`Offset` fields; `NodeRecommendationLinks` struct added

### Phase 5b: RBAC for `/gpu/timeslicing` endpoint

Two-tiered RBAC filtering matching the existing `/recommendations` pattern:

1. **Cluster-level:** `openshift.cluster` permissions restrict which cluster
   UUIDs are queried for GPU data.
2. **Node-level:** `openshift.node` permissions restrict which node names
   appear in the results. Uses the ZED schema's existing `openshift.node`
   permission type.

Since the handler uses `pgx` (not GORM), RBAC filtering is implemented as
Go functions (`filterClustersByRBAC`, `filterNodeRecsByRBAC`) applied
in-memory after computation.

A `ResourceNode` type was also added to `internal/rbac/query_builder.go` for
future use by GORM-based node queries.

### Phase 5c: Documentation

1. Update `docs/gpu-time-slicing-plan.md` with pagination and RBAC sections
2. Update `docs/plans/gpu-recommendations-test-plan.md` Phase E status
3. Update `docs/plans/gpu-timeslicing-tdd-plan.md` status
4. Update OpenAPI spec with pagination parameters and links schema

---

## Implementation Order (TDD)

Tests are written **before** production code in every step.  See
[plans/gpu-timeslicing-tdd-plan.md](plans/gpu-timeslicing-tdd-plan.md) for
the cycle-by-cycle breakdown.

```
TS-01: RED test for node column parsing → GREEN: MetricRow.Node + csvparser
TS-02: RED test for DB persistence      → GREEN: migration 000044 + pipeline
    ↓
TS-03: RED test for notification code   → GREEN: NotifGPUTimeSharingCandidate = 29
TS-04: RED test for result types        → GREEN: TimeslicingRec, GPUContainerRef
TS-05: RED test for computeReplicas     → GREEN: pure function with clamping
TS-06: RED test for confidence          → GREEN: computeTimeslicingConfidence
TS-07: RED test for savings             → GREEN: computeTimeslicingSavings
TS-08: RED test for partitioning        → GREEN: partitionContainers
TS-09: RED test for orchestrator        → GREEN: ComputeNodeTimeslicingRec
TS-10..TS-15: Edge cases and skip conditions (born green or minor fixes)
    ↓
TS-16: RED test for GPUDigestRow.NodeName → GREEN: add field
TS-17: RED test for node map return       → GREEN: QueryGPURecommendations 3-return
    ↓
TS-18: RED test for API model types     → GREEN: NodeGPURecommendation
TS-19: RED test for empty endpoint      → GREEN: handler + route registration
TS-20: RED test for endpoint with data  → GREEN: full wiring
TS-21: RED test for API filters         → GREEN: filter logic
    ↓
Phase 5: Documentation + OpenAPI spec
```

Phases 0-1 (TS-01 through TS-15) can be committed independently.
Phases 2-3 (TS-16 through TS-21) are tightly coupled.

```
Phase 5a: Pagination — limit, offset, order_by via listoptions
Phase 5b: RBAC — cluster + node permission filtering
Phase 5c: Documentation updates
```

---

## Estimated Effort


| Phase                          | Effort     | Status     |
| ------------------------------ | ---------- | ---------- |
| Phase 0 (node ingestion)       | ~1 hour    | **Done**   |
| Phase 1 (engine logic)         | ~2 hours   | **Done**   |
| Phase 2 (wiring)               | ~1.5 hours | **Done**   |
| Phase 3 (API)                  | ~1.5 hours | **Done**   |
| Phase 4 (tests)                | ~2 hours   | **Done**   |
| Phase 5a (pagination)          | ~1 hour    | **Done**   |
| Phase 5b (RBAC)                | ~1.5 hours | **Done**   |
| Phase 5c (docs + OpenAPI)      | ~30 min    | **Done**   |


**Total: ~11 hours**

---

## Future Considerations

- **Persistence (DONE):** The `node_recommendations` table is now implemented
(migration 000058) with per-term storage. Recommendations are computed during
ingestion and persisted with a `term` column (short/medium/long), eliminating
API-read-time computation overhead.
- **Configurable terms (DONE):** Node recommendations use the TermProvider trait
with defaults of 1d (short), 7d (medium), 15d (long) and a 90-day maximum.
Terms are customizable per-tenant via the Settings API.
- **Node-level detail endpoint:** `GET /recommendations/openshift/gpu/timeslicing` with filters (or a future `:node` sub-resource) for full history of time-slicing recommendations over time.
- **Other node rec types:** Instance type recommendations and reserved instance
recommendations follow the same `node_recommendations` table pattern with
`recommendation_type = 'instance_type'` or `'reserved_instance'`.
- **UI:** Stefan's mockups will determine how node recs are displayed.  The API
shape is designed to support both a dedicated "Node Recommendations" page and
inline hints on the container recommendation detail view (via the
`NotifGPUTimeSharingCandidate` notification code on per-container recs).

---

## Design Decisions

Decisions made during design review.  See also
[plans/gpu-recommendations.md](plans/gpu-recommendations.md) Phase E section E13
for the canonical list with full rationale.

| # | Decision | Summary |
|---|---|---|
| D1 | Node name: last-seen, not unique key | `node_name` is a regular column, overwritten on upsert. Pod rescheduling snaps to the new node. |
| D2 | Savings = cost avoidance | Savings represent potential reduction if GPU provisioning is adjusted, not direct removal. |
| D3 | FB = Frame Buffer | GPU video memory (HBM). `fb_usage_max / total_fb` limits safe time-slicing (shared memory). |
| D4 | 1 container = 1 GPU (v1) | No `gpu_count` column yet. Multi-GPU containers treated as 1 GPU. Follow-up: add `gpu_request_count` to operator ROS CSV (~3h total across operator + backend). |
| D5 | `node` column always present | In ROS CSV since ROS support was added (column 13). No minimum version concern. |
| D6 | 7-day freshness for node recs | Use 30-day digest window but skip nodes with no data in the last 7 days. |
| D7 | Container cross-reference (Done) | Candidate containers get notification 29 + `time_slicing_node` and `time_slicing_replicas` fields on their GPU block. `enrichWithGPU` now runs the time-slicing engine so container-level API responses include the cross-reference. |
