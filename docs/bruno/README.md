# Bruno API collections

ROS and Cost Management HTTP examples for manual testing live in the sibling
**[costmgmt-api-cheatsheet](https://github.com/project-koku/costmgmt-api-cheatsheet)**
repository under `bruno/Optimizations/`.

Node-related requests (2026):

| Request | Path |
|---------|------|
| Node utilization | `GET .../recommendations/openshift/nodes` |
| MachineSet recommendations | `GET .../recommendations/openshift/machinesets` |
| Node utilization - filters | List with `filter[category]`, `filter[machineset_name]`, etc. |
| Node utilization detail | `filter[node]` + `filter[cluster]` + `limit=1` |
| Node utilization CSV export | `?format=csv` |
| PUT Settings Thresholds - Node | Idle/zombie and pod headroom fields |
| Fleet savings summary - term | `?term=short\|medium\|long` |
| POST internal recalculate-savings | Service-account token |
| Fleet heatmap | `GET .../recommendations/openshift/fleet-heatmap` |
| Fleet heatmap - memory | `?metric=memory` |
| Fleet heatmap - filter cluster | `?filter[cluster]=<uuid>` |
| VM hourly activity | `GET .../recommendations/openshift/vm/hourly-activity?cluster_uuid=<uuid>&vm_name=<name>&namespace=<ns>` |
| VM hourly activity - 30 days | `?days=30` |
| Node hourly utilization | `GET .../recommendations/openshift/node/<node_name>/hourly-utilization?cluster_uuid=<uuid>` |
| Node hourly utilization - 30 days | `?days=30` |

**Unified category filtering:** Node, container, and namespace endpoints all use
`filter[category]` as the single classification filter. This replaces the former
`filter[is_underutilized]`, `filter[is_overcommitted]`, `filter[stranded_resource]`,
and `filter[idle_state]` parameters.

- **Node** category values: `idle`, `overcommitted`, `stranded_cpu`, `stranded_memory`, `underutilized`, `optimized`
- **Container/Namespace** category values: `idle`, `zombie`, `undersized`, `oversized`, `optimized`

Comma-separated values are ORed: `filter[category]=idle,overcommitted`.

Open the collection in Bruno with environment `bruno/environments/onprem.bru` (adjust `baseURL`).
