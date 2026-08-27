# GPU Time-Slicing Recommendations

Node-level recommendations to share underutilized physical GPUs across containers via
NVIDIA device-plugin time-slicing (`nvidia.com/gpu.replicas`). One persisted row per
node × GPU model × term when the engine emits a recommendation.

| Item | Value |
|------|-------|
| API (list) | `GET /api/cost-management/v1/recommendations/openshift/gpu/timeslicing` |
| API (detail) | `GET /api/cost-management/v1/recommendations/openshift/gpu/timeslicing/{node}` |
| API (history) | `GET /api/cost-management/v1/recommendations/openshift/gpu/timeslicing/history` |
| Handler | [`GetNodeRecommendations`](../../internal/api/handlers_node_recs.go) |
| Engine | [`ComputeAndPersistNodeGPUTimeSlicingRecs`](../../internal/engine/gpu_timeslicing_persist.go) |
| Live table | `node_gpu_timeslicing_recommendations` |
| History table | `node_gpu_timeslicing_recommendation_history` |

Uses **recommendation terms** (`short` / `medium` / `long`), not `filter[engine]=cost|performance`.
Savings appear on the list (`total_node_savings`, `savings_per_gpu` as `MoneyAmount`) and on
container detail (`estimated_monthly_timeslicing_savings` on `gpu.{term}`). Nested
`business_hours` on **detail** never includes dollar savings.

## Persistence at ingest

Node GPU time-slicing recommendations are **computed and persisted during ingest** (after
`StoreGPUClassifications`), not on every API read. Each upsert writes:

- Live row in `node_gpu_timeslicing_recommendations` (replicas, confidence, savings cents, candidate/impacted JSONB lists)
- Append-only history row in `node_gpu_timeslicing_recommendation_history`
- Candidate cross-reference on `recommendation_sets` (`time_slicing_node`, `time_slicing_replicas`)

The list endpoint reads from the live table when the org has any persisted rows. During
backfill, orgs with no persisted rows still use the compute-at-read fallback (digest scan +
engine) until the backfill endpoint or next ingest cycle runs.

**Backfill:** `POST /api/cost-management/v1/internal/backfill-gpu-timeslicing?org_id=…&cluster_uuid=…`
(service-account auth, same as tag sync). Re-runs classification + persist for matching orgs/clusters.

GPU time-slicing savings remain **excluded from** `GET .../savings-summary` (`by_plugin.gpu` is 0).
Query the time-slicing list or container `gpu` block for dollar fields.

## Flow

1. Daily [`gpu_container_digests`](../../internal/testutil/fixtures.go) (DCGM aggregates, per container × node).
2. `StoreGPUClassifications` → `recommendation_sets` (classification, MIG/idle savings).
3. `ComputeAndPersistNodeGPUTimeSlicingRecs` → live + history tables + candidate denormalization.
4. API list reads persisted rows; container enrichment reads `time_slicing_*` from `recommendation_sets`.

## API (list)

| Filter | Alias | Notes |
|--------|-------|-------|
| `filter[cluster]` | `cluster`, `cluster_uuid` | RBAC + unknown cluster → empty 200 |
| `node_name` | — | Exact match (case-insensitive) |
| `gpu_model` | `filter[gpu_model]` | Substring match |
| `filter[tag:<key>]` | `tag=key:value` | Requires `ROS_TAGS_ENABLED` |
| `term` | — | Term window filter |

`order_by`: `node_name`, `cluster_uuid`, `gpu_model`, `recommended_replicas`, `confidence`,
`total_node_savings`. `limit` / `offset` (default 100, max 1000). `format=csv` or `Accept: text/csv`.

## Detail

`GET .../gpu/timeslicing/{node}` returns all GPU-model × term rows for one node (same
row shape as the list). Nested `business_hours` is attached here only when org ⊕ cluster
is enabled and every container in the node × GPU model group uses the cluster window.
Heterogeneous namespace windows omit the nested object. Namespace-only enablement does
not produce timeslicing BH. Replica sizing on the nested object emits notification **81**
(`GPU_TS_BH_CLUSTER_WINDOW`). Reason-only insufficient-data blocks omit 81. Nested BH
includes replica sizing plus SM/DRAM/tensor/FB averages from BH candidate GPU recs and
catalog `total_fb_mib`. Nested BH never includes dollar savings.

**Optimizations UI ([#493](https://github.com/pgarciaq/ros-ocp-backend/issues/493) / [#494](https://github.com/pgarciaq/ros-ocp-backend/issues/494)):**
Breakdown fetches this detail path with `gpu_model` + `term` (not the list). Peak
hours shows replica count when the nest has sizing; **81** is the nest `message`
on that card. Peak hours radar uses nest SM/VRAM. Lists stay all-hours. Hide Peak
hours charts when the nest is reason-only.

Query params: `cluster_uuid` / `cluster` / `filter[cluster]`, `filter[term]`,
`filter[gpu_model]`, `include=explanation`.

## History

`GET .../gpu/timeslicing/history?cluster_uuid=&node_name=&gpu_model=&term=` returns append-only
snapshots from `node_gpu_timeslicing_recommendation_history` (90-day retention).

## Settings

| Endpoint | Purpose |
|----------|---------|
| `GET/PUT/DELETE .../settings/gpu` | GPU + time-slicing thresholds |
| `GET/PUT/DELETE .../settings/terms?recommendation_type=gpu` | Term windows |

Time-slicing keys: `timeslicing_min_replicas`, `timeslicing_max_replicas`,
`timeslicing_majority_threshold`, `timeslicing_base_penalty`, `timeslicing_impacted_weight`,
`node_freshness_days`.

## Notifications

Code **36** (`NotifGPUTimeSharingCandidate` / `GPU_TIMESLICING_CANDIDATE`) on list rows and candidate containers.
Code **81** (`GPU_TS_BH_CLUSTER_WINDOW`) is only on nested detail `business_hours` when replica sizing is present — never on list, history, summary, or parent `notification_codes`.

## RBAC

- Clusters: `openshift.cluster`
- Nodes: `openshift.node`
- No ROS permissions → **403**

Integration coverage: [`handlers_node_recs_integration_test.go`](../../internal/api/handlers_node_recs_integration_test.go).

## Plugin enablement

Routes require the **`gpu`** plugin (`ROS_ENABLED_PLUGINS`). Omit `gpu` → **404** on `/gpu/*`.

## Test data generation

```bash
nise report ocp \
  --static-report-file /path/to/ocp_static_data.yml \
  --ocp-cluster-id <cluster_uuid> \
  --ros-ocp-info \
  --write-monthly
```

Use GPU nodes with multiple underutilized containers (low SM/DRAM on T4/L4-class hardware).

## Summary vs list counts

`GET .../gpu` → `timeslicing.count` = telemetry coverage (node×model triples in digests).
`GET .../gpu/timeslicing` → `meta.count` = actionable persisted recommendations.

## Related

- [GPU MIG (internal)](gpu-mig.md)
- [GPU time-slicing persistence plan](../plans/gpu-time-slicing-persistence.md)
- [Validating native engine](../../docs-site/testing/validating-native-engine.md)
