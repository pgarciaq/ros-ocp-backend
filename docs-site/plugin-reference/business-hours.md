# Business Hours

> **Last verified:** 2026-08-17

Business Hours is a cross-cutting enrichment feature (not a standalone plugin) that adds schedule-aware CPU and memory sizing to container and namespace recommendations, nested cores/GiB sizing on **node detail**, and nested GPU sizing on **container detail** `gpu.{term}`.

## How it works

Administrators configure a weekly schedule (timezone, days, start/end time). During ingestion, samples are filtered by the effective schedule into a parallel `business_hours` digest stream alongside the existing `all_hours` stream. Containers, namespaces, and GPU container digests inherit org → cluster → namespace. Nodes dual-write `daily_node_digests` from **org ⊕ cluster only** (namespace-only enablement is ignored). The recommendation engine computes BH-specific sizing alongside all-hours recommendations.

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

Node **detail** engines nest `recommendation_engines.{cost|performance}.business_hours` with cores/GiB (not request/limit amounts) when org ⊕ cluster is enabled. List omits that object. Notification **79** is on the nested block when sizing is present.

Container **detail** `gpu.{term}` nests `business_hours` when the namespace schedule is enabled. List, MIG list, and timeslicing omit that object. Notification **80** is on the nested GPU block when sizing is present. The GPU plugin `APIEnricher` stays rates-only.

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

**v1: Container + Namespace** (list + detail). **Nodes ([#484](https://github.com/pgarciaq/ros-ocp-backend/issues/484)):** nested `business_hours` on **detail only**. List stays all-hours. Namespace-only schedules do not dual-write node BH. **GPU ([#485](https://github.com/pgarciaq/ros-ocp-backend/issues/485)):** nested `business_hours` on **container detail** `gpu.{term}` only (namespace schedule; namespace-only enablement **does** dual-write GPU BH). Container list, MIG list, and timeslicing stay all-hours. Timeslicing BH is [#491](https://github.com/pgarciaq/ros-ocp-backend/issues/491). PVC/VM remain out of scope. CLI JSON BH siblings for node/GPU/VM are [#487](https://github.com/pgarciaq/ros-ocp-backend/issues/487). No workload-type Settings API.

## Notification codes

Container/namespace BH uses standard plugin codes (for example **25** `NO_COST_DATA` when savings estimates cannot be computed — unrelated to BH). Node detail nested `business_hours` emits **79** `NODE_BH_NOT_PEAK_SAFE` (WARNING) when sizing is present — never on list rows or parent engine maps. Reason-only insufficient-data blocks omit 79. Container detail nested `gpu.{term}.business_hours` emits **80** `GPU_BH_OFFICE_WINDOW` (WARNING) when sizing is present — never on list, MIG, timeslicing, or parent GPU maps. Reason-only insufficient-data blocks omit 80.

## Related documentation

- Business Hours admin guide — see `docs/business-hours-admin-guide.md` (internal)
- Design specification — see `docs/features-business-hours.md` (internal)
