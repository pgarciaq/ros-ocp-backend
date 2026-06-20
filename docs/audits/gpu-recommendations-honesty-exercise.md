# GPU Recommendations Honesty Exercise — MIG and Time-Slicing

**Date:** 2026-06-20
**Branch:** `pgarciaq-rosocp-superpowers-phase14`
**Scope:** Cross-source alignment audit for GPU MIG and time-slicing recommendations

---

## Executive Summary

The GPU recommendations feature is **mature and well-aligned** across the ecosystem.
Both MIG and time-slicing are fully implemented with:
- Dedicated API endpoints (list, detail, summary, history)
- Persistence at ingest (migration 000145 for time-slicing, 000146 for explanation columns)
- Full operator collection of DCGM metrics
- IQE, E2E, and unit test coverage
- Bruno collection, cheatsheet, and OpenAPI documentation

**Key finding:** No broken functionality. Two minor documentation gaps were identified and
fixed in this exercise (docs-site retention tables and missing history endpoint listing).

---

## Phase 1: Discovery Summary

### Endpoints Discovered

| Endpoint | Method | Handler | Purpose |
|----------|--------|---------|---------|
| `/recommendations/openshift/gpu` | GET | `GetGPUSummary` | Aggregate counts for MIG + timeslicing |
| `/recommendations/openshift/gpu/mig` | GET | `GetGPUMIGRecommendations` | MIG profile list |
| `/recommendations/openshift/gpu/timeslicing` | GET | `GetNodeRecommendations` | Time-slicing list (persisted) |
| `/recommendations/openshift/gpu/timeslicing/history` | GET | `GetNodeGPUTimeslicingRecommendationHistory` | Append-only history |
| `/recommendations/openshift/settings/gpu` | GET/PUT/DELETE | Threshold settings | GPU classification thresholds |
| `/internal/backfill-gpu-timeslicing` | POST | `HandleGPUTimeslicingBackfill` | Backfill persisted rows |

### Key Models

| Model | Table | Purpose |
|-------|-------|---------|
| `GPUMIGRecommendationEntry` | (computed from digests) | MIG list response row |
| `NodeGPUTimeslicingRecommendation` | `node_gpu_timeslicing_recommendations` | Live persisted time-slicing rec |
| `NodeGPUTimeslicingRecommendationHistory` | `node_gpu_timeslicing_recommendation_history` | Append-only history |
| `GPUSummaryResponse` | (computed) | Summary counts |

### Notification Codes

| Code | Name | Plugin | Context |
|------|------|--------|---------|
| 10 | `GPU_UNDERUTILIZED` | gpu | MIG container detail |
| 26 | `GPU_IDLE` | gpu | MIG/container |
| 27 | `GPU_MEMORY_BOUND` | gpu | MIG/container |
| 28 | `GPU_NO_PROFILING_DATA` | gpu | MIG/container |
| 36 | `GPU_TIMESLICING_CANDIDATE` | gpu | Time-slicing list + container enrichment |

### Migrations

| Migration | Purpose |
|-----------|---------|
| 000145 | Create `node_gpu_timeslicing_recommendations` + history tables, add `time_slicing_node`/`time_slicing_replicas` to `recommendation_sets` |
| 000146 | Add `expl_data_days`, `expl_candidate_count`, `expl_impacted_count`, `expl_classification_rule` to both GPU time-slicing tables |

---

## Phase 2: Detailed Audit

### MIG List Endpoint (`GET .../gpu/mig`)

| Aspect | Value |
|--------|-------|
| Filters | `filter[cluster]`, `filter[project]`, `filter[gpu_idle_state]`, `filter[tag:<key>]` |
| Pagination | offset/limit + keyset cursor (`after=<meta.next_cursor>`) |
| order_by | `cluster_uuid`, `namespace`, `workload`, `container`, `term`, `gpu_model`, `confidence` |
| CSV export | Yes (`format=csv` or `Accept: text/csv`) |
| Response fields | `cluster_uuid`, `namespace`, `workload`, `container`, `term`, `gpu_model`, `node_name`, `recommended_gpu_profile`, `current_gpu_profile`, `gpu_classification`, `confidence`, `confidence_level`, `gpu_idle_state` |
| `meta.currency` | ✅ Present |
| Savings | Not on this list — available on container detail only |

### Time-Slicing List Endpoint (`GET .../gpu/timeslicing`)

| Aspect | Value |
|--------|-------|
| Filters | `filter[cluster]`, `node_name`, `gpu_model`/`filter[gpu_model]`, `filter[tag:<key>]`, `term` |
| Pagination | offset/limit + keyset cursor |
| order_by | `node_name` (default), `cluster_uuid`, `gpu_model`, `recommended_replicas`, `confidence`, `total_node_savings` |
| CSV export | Yes (`format=csv` or `Accept: text/csv`) |
| Response fields | `org_id`, `cluster_uuid`, `node_name`, `gpu_model`, `term`, `recommended_replicas`, `confidence`, `confidence_level`, `candidate_count`, `impacted_count`, `candidate_containers`, `impacted_containers`, `notification_codes`, `estimated_savings_cents`, `savings_per_gpu_cents`, `last_seen_at`, `updated_at` |
| `meta.currency` | Metadata includes currency via standard list response |
| Savings | Included on list rows (`estimated_savings_cents`, `savings_per_gpu_cents`) |

### Time-Slicing History Endpoint (`GET .../gpu/timeslicing/history`)

| Aspect | Value |
|--------|-------|
| Required params | `cluster_uuid`, `node_name` |
| Optional params | `gpu_model`, `term`, `limit`, `offset`, `order_by`, `order_how` |
| order_by | `recorded_at` (default desc), `recommended_replicas`, `confidence`, `candidate_count`, `impacted_count` |
| Retention | 90 days (`ROS_HISTORY_RETENTION_DAYS`) |

### GPU Summary Endpoint (`GET .../gpu`)

| Aspect | Value |
|--------|-------|
| Response fields | `mig.count`, `mig.link`, `timeslicing.count`, `timeslicing.link`, `total_gpus_analyzed`, `clusters_with_gpu_data` |
| Note | `timeslicing.count` = node×model triples with telemetry (coverage), not actionable recs |

---

## Phase 3: Alignment Matrix

| Aspect | Requirements (ADR) | Go Code | Internal Docs | Public Docs | Cheatsheet | Bruno | Unit Tests | E2E Tests | IQE Tests | OpenAPI | Operator |
|--------|:------------------:|:-------:|:-------------:|:-----------:|:----------:|:-----:|:----------:|:---------:|:---------:|:-------:|:--------:|
| **MIG list endpoint** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | — |
| **MIG detail (container gpu block)** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | — |
| **Time-slicing list endpoint** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | — |
| **Time-slicing history endpoint** | ✅ | ✅ | ✅ | ⚠️→✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | — |
| **GPU summary endpoint** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | — |
| **GPU settings endpoint** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | — | ✅ | ✅ | — |
| **GPU-specific filters** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | — |
| **Response fields (mig)** | ✅ | ✅ | ✅ | ✅ | ✅ | — | ✅ | ✅ | ✅ | ✅ | — |
| **Response fields (timeslicing)** | ✅ | ✅ | ✅ | ✅ | ✅ | — | ✅ | ✅ | ✅ | ✅ | — |
| **Notification code 36** | ✅ | ✅ | ✅ | ✅ | ✅ | — | ✅ | ✅ | ✅ | ✅ | — |
| **Notification codes 10, 26-28** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | — |
| **CSV export** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | — |
| **GPU data generation (nise)** | ✅ | — | ✅ | — | ✅ | — | — | ✅ | ✅ | — | — |
| **Operator GPU Prometheus queries** | ✅ | — | — | — | — | — | — | — | — | — | ✅ |
| **Koku GPU CSV processing** | ✅ | — | — | — | — | — | — | — | — | — | — |
| **GPU cost model integration** | ✅ | ✅ | ✅ | ✅ | ✅ | — | ✅ | — | — | — | — |
| **Time-slicing persistence (mig 145)** | ✅ | ✅ | ✅ | ✅ | ✅ | — | ✅ | ✅ | ✅ | ✅ | — |
| **Explanation columns (mig 146)** | ✅ | ✅ | ✅ | — | — | — | ✅ | — | — | — | — |
| **Retention tables** | ✅ | ✅ | ✅ | ⚠️→✅ | ✅ | — | ✅ | — | — | — | — |
| **Backfill endpoint** | ✅ | ✅ | ✅ | ✅ | ✅ | — | ✅ | — | — | — | — |
| **Plugin gating (404 when disabled)** | ✅ | ✅ | ✅ | ✅ | ✅ | — | ✅ | — | — | ✅ | — |

Legend: ✅ aligned | ⚠️ partially (before fix) | ⚠️→✅ fixed in this exercise | ❌ wrong/missing | — N/A

---

## Phase 4: Discrepancies Found and Fixed

### Fix 1: docs-site retention tables incomplete

**File:** `docs-site/plugin-reference/gpu.md` (line 15)

**Problem:** Listed only `gpu_container_digests` as retention table; code (`RetentionTables()`) returns 3 tables.

**Fix:** Updated to list all three: `gpu_container_digests`, `node_gpu_timeslicing_recommendations`, `node_gpu_timeslicing_recommendation_history`.

### Fix 2: docs-site missing history endpoint in Endpoints section

**File:** `docs-site/plugin-reference/gpu.md` (line 107)

**Problem:** Endpoints listing omitted `GET .../gpu/timeslicing/history`. The code registers it, OpenAPI documents it, cheatsheet covers it, Bruno has a request for it, but the public docs-site didn't list it.

**Fix:** Added `GET /api/cost-management/v1/recommendations/openshift/gpu/timeslicing/history` to the endpoint listing.

### Fix 3: docs-site missing RetentionProvider trait

**File:** `docs-site/plugin-reference/gpu.md` (traits table)

**Problem:** Traits table listed 4 traits but the code implements 5 (CSVIngestor:No, IngestHook:Yes, APIEnricher:Yes, APIProvider:Yes, RetentionProvider:Yes, TermProvider:Yes). RetentionProvider was missing.

**Fix:** Added `RetentionProvider` row and updated APIProvider description to include "history".

---

## Phase 5: Verification

```
$ go build ./...     # ✅ passes
$ go test ./internal/... -count=1 -short   # ✅ all packages pass (49 packages)
```

---

## Phase 6: Report

### What Works End-to-End for GPU MIG Recommendations

1. **Data pipeline:** Operator collects DCGM metrics (`DCGM_FI_PROF_GR_ENGINE_ACTIVE`, `DCGM_FI_DEV_FB_USED`, `DCGM_FI_PROF_SM_ACTIVE`, `DCGM_FI_PROF_PIPE_TENSOR_ACTIVE`, `DCGM_FI_PROF_DRAM_ACTIVE`, `DCGM_FI_DEV_MIG_MAX_SLICES`) → CSV reports → Koku ingestion → ros-ocp-backend GPU digests
2. **Engine:** Classification (compute-bound, memory-bound, idle, mixed) + MIG profile bin-packing (P98 FB × headroom → smallest profile)
3. **API:** List endpoint with filtering, sorting, keyset pagination, CSV export
4. **Container enrichment:** `gpu` block on detail with `recommended_gpu_profile`, savings, notifications
5. **Savings:** Persisted as `estimated_gpu_savings_cents` on `recommendation_sets`, refreshable via `recalculate-savings`
6. **Notifications:** Codes 10, 26, 27, 28 on container detail
7. **Settings:** Per-org thresholds (idle, underutilized SM, FB headroom, MIG percentile)
8. **Tests:** Unit tests, IQE (`test_gpu_recommendations.py`), E2E flow (`test_gpu_mig_recommendations_flow.py`)

### What Works End-to-End for GPU Time-Slicing Recommendations

1. **Persistence:** Compute-at-ingest (ADR-0297 superseded ADR-0115's read-time approach); live + history tables
2. **API:** List from persisted table with fallback to compute-at-read; history endpoint; backfill endpoint
3. **Savings:** `estimated_savings_cents` and `savings_per_gpu_cents` persisted at ingest (when cost rates available)
4. **Container cross-reference:** `time_slicing_node` and `time_slicing_replicas` on `recommendation_sets`
5. **Notifications:** Code 36 (`GPU_TIMESLICING_CANDIDATE`)
6. **Retention:** 90-day history pruning via `PruneNodeGPUTimeslicingRecommendationHistory`
7. **Tests:** Unit tests, IQE, E2E flow (`test_gpu_timeslicing_flow.py`)
8. **Explanation columns:** Migration 000146 adds `expl_data_days`, `expl_candidate_count`, `expl_impacted_count`, `expl_classification_rule`

### Feature Maturity Comparison

| Dimension | Container Recs | GPU MIG | GPU Time-Slicing |
|-----------|:-----------:|:-------:|:----------------:|
| Persistence | ✅ Full | ✅ On `recommendation_sets` | ✅ Dedicated table |
| History | ✅ `recommendation_history` | ⚠️ Via container history | ✅ `node_gpu_timeslicing_recommendation_history` |
| Explanation columns | ✅ Full | ✅ GPU-specific `expl_gpu_*` | ✅ `expl_data_days`, etc. |
| Savings in fleet summary | ✅ Included | ❌ Excluded (by design, ADR-0071) | ❌ Excluded (by design) |
| Settings/thresholds | ✅ | ✅ | ✅ (shared settings endpoint) |
| Keyset cursor pagination | ✅ | ✅ | ✅ |
| CSV export | ✅ | ✅ | ✅ |
| Tag filtering | ✅ | ✅ | ✅ |
| RBAC | ✅ cluster + project | ✅ cluster + node | ✅ cluster + node |
| IQE tests | ✅ | ✅ | ✅ |
| E2E tests | ✅ | ✅ | ✅ |
| Nise data gen | ✅ | ✅ | ✅ |
| Plugin gating | ✅ | ✅ | ✅ |

### What's Genuinely Missing or Planned

1. **Explanation factors on API responses** — `expl_*` columns exist in DB (migration 146) but are not yet exposed on the time-slicing list/history API JSON. The model struct has `json:"expl_data_days,omitempty"` etc. but OpenAPI doesn't document them yet. This is per ADR-0296 Phase 2c (future work).

2. **GPU savings in fleet summary** — Intentionally excluded (ADR-0071). Both MIG/idle and time-slicing savings are available via their respective endpoints but not rolled into `GET .../savings-summary`. Product decision pending.

3. **koku-ui GPU recommendations** — The frontend has basic GPU cost report components (`gpuTable.tsx`, `migData.tsx`) for Koku cost reports but does **not** have dedicated ROS GPU recommendation UI components (MIG list, time-slicing list). ROS GPU recommendations would be consumed via the optimizations page or a future dedicated GPU view.

### Cross-Repo Coordination Issues

| Flow | Status |
|------|--------|
| Operator → Koku (GPU CSV) | ✅ Well-defined contract (`ocp_gpu_usage.csv` columns match `OCPGPUUsageLineItem` model) |
| Operator → ros-ocp-backend (ROS CSV) | ✅ DCGM columns in ROS container CSVs feed `gpu_container_digests` |
| Koku → ros-ocp-backend (cost rates) | ✅ `effective_rates` endpoint provides `gpu_cost_per_month` for savings |
| Nise → ingress → processing | ✅ `--ros-ocp-info` flag + GPU YAML templates produce correct CSV columns |
| COST-7241 (GPU tag key exception) | ✅ Handled — GPU tag keys don't need special Koku-side validation |

### Checklist Results

- [x] MIG profile names in code match what the operator collects (`modelName` → `gpu_model`, `GPU_I_PROFILE` → MIG profile)
- [x] Time-slicing persistence migrations exist and are numbered correctly (000145, 000146)
- [x] GPU enrichment logic handles missing GPU model data gracefully (returns empty/nil, no crash)
- [x] OpenAPI spec documents both MIG and time-slicing endpoints (including history)
- [x] Bruno has requests for both MIG and time-slicing (17 GPU-related `.bru` files)
- [x] Cheatsheet covers GPU recommendations (MIG list, time-slicing list, history, settings, backfill)
- [x] Notification codes for GPU are in `catalog.go` with correct plugin association (`"gpu": {10, 26, 27, 28, 36}`)
- [x] GPU tag key validation exception (COST-7241) is still working
- [x] Nise generates GPU usage data (`tests/ocp_gpu_static_report.yml`, cost-onprem-chart templates)
- [x] Koku's GPU report type detection works with operator CSV columns (`"gpu_usage"` in `TRINO_LINE_ITEM_TABLE_MAP`)
- [x] E2E tests cover GPU data flow (`test_gpu_mig_recommendations_flow.py`, `test_gpu_timeslicing_flow.py`)
- [x] History table for time-slicing is properly retained/pruned (`PruneNodeGPUTimeslicingRecommendationHistory`, 90-day retention)

---

## Appendix: Operator GPU Prometheus Queries

### Container-Level ROS (fed to ros-ocp-backend)

| Query Key | DCGM Metric | Aggregation | Purpose |
|-----------|-------------|-------------|---------|
| `ros:accelerator_frame_buffer_usage_min/max/avg` | `DCGM_FI_DEV_FB_USED` | per container×pod×namespace | Framebuffer usage for MIG sizing |
| `ros:tensor_pipe_active_min/max/avg` | `DCGM_FI_PROF_PIPE_TENSOR_ACTIVE` | per container | Tensor core activity |
| `ros:dram_active_min/max/avg` | `DCGM_FI_PROF_DRAM_ACTIVE` | per container | Memory bandwidth activity |
| `ros:sm_active_min/max/avg` | `DCGM_FI_PROF_SM_ACTIVE` | per container | Streaming multiprocessor activity |
| `ros:nvidia_gpu_max_slices` | `DCGM_FI_DEV_MIG_MAX_SLICES` | per container | MIG capability detection |

### Cost-Level (fed to Koku)

| Query Key | Purpose |
|-----------|---------|
| `cost:nvidia_gpu_capacity_memory_mib_mig` | MIG GPU memory capacity |
| `cost:nvidia_gpu_capacity_memory_mib_non_mig` | Non-MIG GPU memory capacity |
| `cost:nvidia_gpu_utilization` | GPU utilization for cost reports |
| `cost:nvidia_gpu_pod_uptime` | Pod GPU uptime (billing) |
| `cost:nvidia_gpu_max_slices` | MIG slice count for cost allocation |

### VM-Level (KubeVirt)

| Query Key | DCGM Metric | Purpose |
|-----------|-------------|---------|
| `ros:vm_gpu_utilization_avg/max` | `DCGM_FI_PROF_GR_ENGINE_ACTIVE` | VM GPU utilization |
| `ros:vm_gpu_fb_used_avg/max` | `DCGM_FI_DEV_FB_USED` | VM framebuffer |
| `ros:vm_gpu_sm_active_avg` | `DCGM_FI_PROF_SM_ACTIVE` | VM SM activity |
| `ros:vm_gpu_tensor_active_avg` | `DCGM_FI_PROF_PIPE_TENSOR_ACTIVE` | VM tensor core |
| `ros:vm_gpu_dram_active_avg` | `DCGM_FI_PROF_DRAM_ACTIVE` | VM DRAM activity |
| `ros:vm_gpu_max_slices` | `DCGM_FI_DEV_MIG_MAX_SLICES` | VM MIG capability |
