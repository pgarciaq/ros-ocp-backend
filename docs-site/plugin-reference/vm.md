# VM (OpenShift Virtualization)

> **Last verified:** 2026-08-06

Package: [`internal/plugins/vm`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/plugins/vm)

**VM right-sizing** — analyzes KubeVirt virtual machines (whole vCPU/GiB), instance type matching, idle/abandoned detection, disk projection, GPU guest optimization, and placement hints.

## Plugin metadata

| Property | Value |
|----------|-------|
| Name | `vm` |
| Phase | 1 (Produce) |
| Priority | 40 |
| CSV types | `vm` (VM usage CSV), `vm-gpu` (VM GPU device CSV) |
| Retention tables | `daily_vm_digests`, `vm_recommendations`, `vm_recommendation_history`, `hourly_vm_digests` |

## Traits

| Trait | Supported |
|-------|-----------|
| CSVIngestor | Yes — parses ROS VM usage CSV |
| APIProvider | Yes — list, detail, history |
| RetentionProvider | Yes |
| TermProvider | Yes — short/medium/long (max 90 days) |

!!! note "VM term naming convention"
    VM terms use the `_term` suffix (`short_term`, `medium_term`, `long_term`) in both code
    and API, unlike container/node/namespace/GPU plugins which use bare names (`short`,
    `medium`, `long`). This matches the legacy Kruize convention used by the original VM engine.

## What it does

1. Ingest 15-minute VM samples into `daily_vm_digests`.
2. Run native `recommendVM()` (cost and performance engines; no Kruize path).
3. Persist recommendations with notifications, optional `estimated_monthly_savings`, and metadata flags.
4. Append bounded history rows for the history API.

See [Virtual Machine recommendations](../features/virtual-machines.md).

**Business hours:** not applicable. Business-hours weighting applies to container and namespace recommendations only.

## Endpoints

```
GET /api/cost-management/v1/recommendations/openshift/vm
GET /api/cost-management/v1/recommendations/openshift/vm/detail
  ?cluster_uuid={uuid}&namespace={ns}&vm_name={name}&term=medium_term&engine=cost
GET /api/cost-management/v1/recommendations/openshift/vms/{vm_name}/history
  ?cluster_uuid={uuid}&namespace={ns}&term=medium_term&engine=cost
GET /api/cost-management/v1/recommendations/openshift/instance-types?cluster_uuid={uuid}
```

| Endpoint | Purpose |
|----------|---------|
| `GET .../vm` | VM list (rightsizing, idle, GPU, network-bound filters); `?format=csv` or `Accept: text/csv` for export |
| `GET .../vm/detail` | Single VM with `daily_digests[]` |
| `GET .../vms/{vm_name}/history` | Append-only recommendation history (plural `vms` + path param); `?format=csv` supported |
| `GET .../instance-types` | Cluster instancetypes, preferences, and matching metadata (`cluster_uuid` required) |

List filters include `filter[cluster]`, `filter[project]` / `filter[namespace]`, `filter[vm_name]`, `filter[engine]`, `filter[term]`, `filter[is_idle]`, `filter[is_abandoned]`, `filter[has_gpu]`, `filter[tag:<key>]` (when `ROS_TAGS_ENABLED=true`), and others. Routes return **404** when the `vm` plugin is disabled (i.e., listed in `ROS_DISABLED_PLUGINS`).

### Idle and abandoned

| Flag | Code | Meaning |
|------|------|---------|
| `is_idle` | **18** | Negligible CPU/memory activity across the term window |
| `is_abandoned` | **43** | Powered off or unscheduled for an extended period (default ≥ 14 days) |

List filters: `filter[is_idle]=true`, `filter[is_abandoned]=true`. Abandoned supersedes idle for classification.

### CSV export

`GET .../vm?format=csv` and `GET .../vms/{vm_name}/history?format=csv` return `text/csv` with the same query filters as JSON (pagination, `filter[term]`, `filter[engine]`, etc.). Prefer `Accept: text/csv` for explicit content negotiation.

### Notification codes

VM notifications use numeric `code` values in the **18–69** range (idle/stale **18**–**19**, sizing **37**–**54**, I/O/GPU/network **55**–**59**, placement **60**–**63**, power-off **64**, network QoS **65**–**66**, storage tiering **67**–**69**). Catalog: `GET .../notification-codes?filter[plugin]=vm`. Full reference: [Notification codes](../architecture/notification-codes.md).

Tag filter syntax: `filter[tag:<key>]=<value>` (see [Query parameters](query-parameters.md)).

Handlers: [`internal/api/handlers_vm_recs.go`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/api/handlers_vm_recs.go), [`internal/api/handlers_vm_history.go`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/api/handlers_vm_history.go).

### Hourly activity (feature-gated)

When both `ROS_VISUAL_INSIGHTS_ENABLED` and `ROS_HOURLY_VM_DIGESTS_ENABLED` are `true`:

```
GET /api/cost-management/v1/recommendations/openshift/vm/hourly-activity
```

Returns hourly CPU/memory activity data for VMs, used by visual-insights dashboards. Not registered when either feature flag is disabled.

## Savings

Each VM recommendation includes an `estimated_monthly_savings` object (`value` + `units`) computed from:

- **Downsize:** Delta between current and recommended vCPU/memory × rates
- **Idle/Abandoned:** Full allocation cost (100% recoverable)
- **Power-off:** Full VM cost
- **GPU reduction:** GPU count delta × GPU rate

When no cost data is available, `estimated_monthly_savings` returns `null` (unlike container/node/PVC which return $0 with notification code 25).

`estimated_monthly_savings.value` can be **negative** when current VM allocation is already below the recommended target. Display as additional monthly cost, not as a savings opportunity.

VM savings are included in fleet `savings-summary` totals under `by_plugin.vm`.

## Kill-Switch Behavior

When `ROS_SAVINGS_ESTIMATES_ENABLED=false`, VM `estimated_monthly_savings` returns `null` (not computed).

## Settings

VM thresholds are configurable via the Settings API (`GET/PUT/DELETE .../settings/vm` and `.../settings/vm/terms`). Partial PUT accepts `thresholds`, `memory_floors`, `stability`, `disk`, `io`, `gpu`, `placement`, `network`, `instance_type_matching`, and optional sub-blocks:

- **`power_schedule`** — simplified power-off candidates (notification **64**)
- **`network_qos`** — SR-IOV / DPDK hints for network-bound VMs (notifications **65**–**66**)
- **`storage_tiering`** — multi-day I/O tier hints (notifications **67**–**69**)

Read-only `history_retention_days` mirrors env `ROS_VM_REC_HISTORY_RETENTION_DAYS` (default **90** days) for `vm_recommendation_history` retention.

## Related

- [Virtual Machine recommendations](../features/virtual-machines.md) — feature guide and notification table
- [Savings Estimations](../features/savings-estimations.md) — fleet rollup, accuracy notes
- [Cost Integration](../architecture/cost-integration.md) — rate resolution chain
