# Business Hours — Internal Reference

Business Hours is a cross-cutting enrichment feature that adds schedule-aware CPU and memory sizing to container and namespace recommendations.

See [design doc](../features-business-hours.md) for complete specification.

## Key API

- **Settings** (3 scopes): org, cluster, namespace — `GET/PUT/DELETE .../settings/business-hours` (+ cluster/namespace subpaths)
- **Effective schedule**: `GET .../settings/business-hours/effective?cluster_id=&namespace=` — resolves inheritance and returns `resolved_from`
- **Recommendations**: nested `business_hours` block on container and namespace detail/list engines when a schedule applies

## Key files

- `internal/bhschedule/` — schedule evaluation and inheritance
- `internal/ingestion/pipeline_business_hours.go` — dual digest stream at ingest
- `internal/engine/recommend_business_hours.go` — BH-specific recommendation computation

## Scope & Rationale

**v1 Scope: Container + Namespace only**

Business hours sizing targets workloads with diurnal load patterns (busy during work hours, quiet at night). Containers are the canonical example — a web app handles 10x more requests 9-5 than overnight.

- **Nodes** — infrastructure sized for peak demand regardless of time. A node must handle batch jobs at night too.
- **GPUs** — typically run ML training/inference 24/7 or in bursts; don't follow business-hour patterns.
- **PVCs** — storage capacity doesn't vary by time of day.
- **VMs** — could benefit (similar to containers) but have fewer users; deferred to Phase 2.

These exclusions are enforced by negative tests (`internal/plugins/node/plugin_test.go`, `gpu/plugin_test.go`, `pvc/plugin_test.go`).

## Design Decision: Nested vs Separate Rows

**Design Decision: Nested enrichment (no filter/group_by)**

Business hours recommendations are returned as a nested `business_hours` block inside each container/namespace recommendation — not as separate rows. The design chose nested enrichment (one recommendation with an optional BH sibling) over separate recommendation resources. This way clients see both perspectives (all-hours vs business-hours) side-by-side without extra API calls or filter/group_by params.

The `business_hours` block is simply absent when no schedule applies to that workload, so no filtering is needed to exclude non-BH containers.

## Effective schedule endpoint

`GET /api/cost-management/v1/recommendations/openshift/settings/business-hours/effective`

| Query param | Behavior |
|-------------|----------|
| (none) | Org default row only |
| `cluster_id` | Namespace → cluster → org for that cluster |
| `cluster_id` + `namespace` | Same chain, namespace first |

Response adds `resolved_from`: `namespace`, `cluster`, `org`, or `none` when no schedule row applies.
