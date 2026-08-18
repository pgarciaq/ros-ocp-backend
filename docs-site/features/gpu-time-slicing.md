# GPU time-slicing recommendations

> **Last verified:** 2026-08-06

!!! info "Quick Facts"
    **API:** `GET /api/cost-management/v1/recommendations/openshift/gpu/timeslicing` (list),
    `GET .../gpu/timeslicing/{node}` (detail),
    `GET .../gpu/timeslicing/history` (history)  
    **Scope:** Per-node × GPU model (not per-container)  
    **Engines:** Recommendation **terms** only (`short`, `medium`, `long`) — **not** cost/performance dual engine  
    **Savings:** Yes — `total_node_savings` and `savings_per_gpu` on list rows (when cost-model rates available at ingest)  
    **Configurable:** Yes (GPU thresholds + term windows via Settings API)

Node-level recommendations to share underutilized physical GPUs across containers via NVIDIA
device-plugin time-slicing. Recommendations are **persisted at ingest** and served from
`node_gpu_timeslicing_recommendations`.

## Endpoints

```
GET /api/cost-management/v1/recommendations/openshift/gpu/timeslicing
GET /api/cost-management/v1/recommendations/openshift/gpu/timeslicing/{node}
GET /api/cost-management/v1/recommendations/openshift/gpu/timeslicing/history
```

Container list/detail includes time-slicing cross-reference fields on the `gpu` block when a
workload is a sharing candidate (`time_slicing_node`, `time_slicing_replicas`, notification 36).

## Persistence

During each ingest cycle (when the `gpu` plugin is enabled):

1. GPU classifications are stored on `recommendation_sets`.
2. Node time-slicing recommendations are computed and upserted into `node_gpu_timeslicing_recommendations`.
3. A history snapshot is appended to `node_gpu_timeslicing_recommendation_history`.
4. Candidate containers receive `time_slicing_node` and `time_slicing_replicas` on `recommendation_sets`.

The list API reads persisted rows. Orgs without persisted data fall back to compute-at-read until
backfill or the next ingest completes.

### Backfill

Platform operators can re-run persistence without re-ingesting reports:

```
POST /api/cost-management/v1/internal/backfill-gpu-timeslicing?org_id=<org>&cluster_uuid=<uuid>
```

Requires service-account authentication (same as `/internal/tags/sync`). Omit `org_id` to process
all organizations; omit `cluster_uuid` to process all clusters in the scope.

## Savings

When GPU cost-model rates are available at ingest, `estimated_savings_cents` and
`savings_per_gpu_cents` are stored on the live row and exposed as `MoneyAmount` on the list API.
When rates are unavailable, savings columns are NULL and dollar fields are omitted from responses.

Time-slicing savings are **not** included in `GET .../savings-summary` fleet totals.

## Detail

`GET .../gpu/timeslicing/{node}` returns all GPU-model × term rows for one node (same
row shape as the list). Nested `business_hours` is attached here only when org ⊕ cluster
is enabled and every container in the node × GPU model group uses the cluster window.
Heterogeneous namespace windows omit the nested object. Replica sizing on the nested
object emits notification **81** (`GPU_TS_BH_CLUSTER_WINDOW`). Nested BH never includes
dollar savings. List, history, and GPU summary `timeslicing.count` stay all-hours.

## History

The history endpoint returns prior replica counts and savings for a given node × GPU model × term.
History rows are retained for 90 days.

## Settings

Configure thresholds and term windows via:

- `GET/PUT/DELETE .../settings/gpu`
- `GET/PUT/DELETE .../settings/terms?recommendation_type=gpu`

See [GPU plugin reference](../plugin-reference/gpu.md) for classification and confidence details.

## Related

- [GPU plugin reference](../plugin-reference/gpu.md)
- [Query parameters](../plugin-reference/query-parameters.md)
