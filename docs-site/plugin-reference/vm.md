# VM (OpenShift Virtualization)

Package: [`internal/plugins/vm`](../../internal/plugins/vm/)

**VM right-sizing** — analyzes KubeVirt virtual machines (whole vCPU/GiB), instance type matching, idle/abandoned detection, disk projection, GPU guest optimization, and placement hints.

## Plugin metadata

| Property | Value |
|----------|-------|
| Name | `vm` |
| Phase | 1 (Produce) |
| Priority | 40 |
| CSV types | `ros-openshift-vm-usage` (and optional GPU device CSV) |
| Retention tables | `daily_vm_digests`, `vm_recommendations`, `vm_recommendation_history` |

## Traits

| Trait | Supported |
|-------|-----------|
| CSVIngestor | Yes — parses ROS VM usage CSV |
| APIProvider | Yes — list, detail, history |
| RetentionProvider | Yes |
| TermProvider | Yes — short/medium/long (max 90 days) |

## What it does

1. Ingest 15-minute VM samples into `daily_vm_digests`.
2. Run native `recommendVM()` (cost and performance engines; no Kruize path).
3. Persist recommendations with notifications, optional `savings`, and metadata flags.
4. Append bounded history rows for the history API.

See [Virtual Machine recommendations](../features/virtual-machines.md).

## Endpoints

```
GET /api/cost-management/v1/recommendations/openshift/vm
GET /api/cost-management/v1/recommendations/openshift/vm/detail
  ?cluster_uuid={uuid}&namespace={ns}&vm_name={name}&term=medium_term&engine=cost
GET /api/cost-management/v1/recommendations/openshift/vms/{vm_name}/history
  ?cluster_uuid={uuid}&namespace={ns}&term=medium_term&engine=cost
```

| Endpoint | Purpose |
|----------|---------|
| `GET .../vm` | VM list (rightsizing, idle, GPU, network-bound filters) |
| `GET .../vm/detail` | Single VM with `daily_digests[]` |
| `GET .../vms/{vm_name}/history` | Append-only recommendation history (plural `vms` + path param) |

List filters include `filter[cluster]`, `filter[project]` / `filter[namespace]`, `filter[vm_name]`, `filter[engine]`, `filter[term]`, `filter[is_idle]`, `filter[has_gpu]`, `filter[tag:<key>]` (when `ROS_TAGS_ENABLED=true`), and others. Routes return **404** when `ROS_ENABLE_VM_RECS=false`.

Tag filter syntax: `filter[tag:<key>]=<value>` (see [Query parameters](query-parameters.md)).

Handlers: [`internal/api/handlers_vm_recs.go`](../../internal/api/handlers_vm_recs.go), [`internal/api/handlers_vm_history.go`](../../internal/api/handlers_vm_history.go).

## Savings

Each VM recommendation includes a `savings` object (`value` + `units`) computed from:

- **Downsize:** Delta between current and recommended vCPU/memory × rates
- **Idle/Abandoned:** Full allocation cost (100% recoverable)
- **Power-off:** Full VM cost
- **GPU reduction:** GPU count delta × GPU rate

When no cost data is available, `savings` returns `null` (unlike container/node/PVC which return $0 with notification code 25).

`savings.value` can be **negative** when current VM allocation is already below the recommended target. Display as additional monthly cost, not as a savings opportunity.

VM savings are included in fleet `savings-summary` totals under `by_plugin.vm`.

## Kill-Switch Behavior

When `ROS_SAVINGS_ESTIMATES_ENABLED=false`, VM savings return `null` (not computed).

## Settings

VM thresholds are configurable via the Settings API (`GET/PUT/DELETE .../settings/vm` and `.../settings/vm/terms`).

## Related

- [Virtual Machine recommendations](../features/virtual-machines.md) — feature guide and notification table
- [Savings Estimations](../features/savings-estimations.md) — fleet rollup, accuracy notes
- [Cost Integration](../architecture/cost-integration.md) — rate resolution chain
