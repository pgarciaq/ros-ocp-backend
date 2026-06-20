# Business Hours

Business Hours is a cross-cutting enrichment feature (not a standalone plugin) that adds schedule-aware CPU and memory sizing to container and namespace recommendations.

## How it works

Administrators configure a weekly schedule (timezone, days, start/end time). During ingestion, samples are filtered by the effective schedule into a parallel `business_hours` digest stream alongside the existing `all_hours` stream. The recommendation engine computes BH-specific sizing alongside all-hours recommendations using the same cost/performance percentiles.

## Settings API

`GET` / `PUT` / `DELETE` at three scopes with inheritance:

| Scope | Path suffix |
|-------|-------------|
| Org default | `/settings/business-hours` |
| Cluster override | `/settings/business-hours/clusters/:cluster_id` |
| Namespace override | `/settings/business-hours/clusters/:cluster_id/namespaces/:namespace` |
| Effective (resolved) | `GET /settings/business-hours/effective?cluster_id=&namespace=` |

The effective endpoint returns the inherited schedule for optional `cluster_id` and `namespace` query parameters, with `resolved_from` set to `namespace`, `cluster`, `org`, or `none`.

## Response format

Container and namespace list/detail responses include a nested block when a schedule applies:

`recommendation_engines.{cost|performance}.business_hours`

Same `amount`/`format` shape as the parent engine (CPU and memory requests/limits).

Business hours are **nested enrichment**, not separate recommendation rows: each container/namespace item may include an optional `business_hours` sibling alongside all-hours engines. When no schedule applies, the block is omitted — clients do not need filter or `group_by` parameters to hide non-BH workloads.

## Key settings

| Field | Purpose |
|-------|---------|
| `timezone` | IANA timezone for schedule boundaries |
| `schedule.days[]` | Lowercase English day names |
| `schedule.start_time` / `end_time` | 24-hour `HH:MM` in the configured timezone |
| `off_hours_weight` | Weight for off-hours samples in BH percentiles (`0.0` = in-window only) |
| `enabled` | Whether BH applies at this scope |

## Inheritance

Most specific wins: **namespace → cluster → org → disabled** (no BH digests/recommendations when no schedule applies).

## Kill-switch

`ROS_BUSINESS_HOURS_ENABLED` (default `true`). When `false`, business-hours settings routes are not registered, OpenAPI paths are stripped, capabilities omit `business_hours`, and ingestion produces only `all_hours` digests.

## Reship

Schedule changes set `reship_pending_since` and trigger async historical re-processing via Koku masu `reship_ros` so `business_hours` digests can be rebuilt from stored ROS CSVs.

Full request/response contract: [Cost Integration — Business-hours reship](../architecture/cost-integration.md#business-hours-reship-reship_ros).

## Scope

**v1: Container + Namespace only**

Business hours targets diurnal workloads (busy 9–5, quiet overnight). Containers are the canonical fit; nodes are peak-sized for 24/7 batch work; GPUs and PVCs do not follow business-hour patterns; VMs are deferred to Phase 2. Negative tests in `node`, `gpu`, and `pvc` plugins enforce the exclusion.

## Notification codes

No codes are specific to business hours. Standard container codes apply (for example code **25** `NO_COST_DATA` when savings estimates cannot be computed — unrelated to BH).

## Related documentation

- Business Hours admin guide — see `docs/business-hours-admin-guide.md` (internal)
- Design specification — see `docs/features-business-hours.md` (internal)
