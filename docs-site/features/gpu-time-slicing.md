# GPU time-slicing recommendations

Node-level recommendations to share underutilized physical GPUs across containers via NVIDIA
device-plugin time-slicing. Recommendations are **persisted at ingest** and served from
`node_gpu_timeslicing_recommendations`.

## Endpoints

```
GET /api/cost-management/v1/recommendations/openshift/gpu/timeslicing
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
