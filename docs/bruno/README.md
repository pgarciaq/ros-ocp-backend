# Bruno API collections

ROS and Cost Management HTTP examples for manual testing live in the sibling
**[costmgmt-api-cheatsheet](https://github.com/project-koku/costmgmt-api-cheatsheet)**
repository under `bruno/Optimizations/`.

Node-related requests (2026):

| Request | Path |
|---------|------|
| Node utilization | `GET .../recommendations/openshift/nodes` |
| MachineSet recommendations | `GET .../recommendations/openshift/machinesets` |
| Node utilization - filters | List with `filter[stranded_resource]`, `filter[machineset_name]`, etc. |
| Node utilization detail | `filter[node]` + `filter[cluster]` + `limit=1` |
| Node utilization CSV export | `?format=csv` |
| PUT Settings Thresholds - Node | Idle/zombie and pod headroom fields |
| Fleet savings summary - term | `?term=short\|medium\|long` |
| POST internal recalculate-savings | Service-account token |

Open the collection in Bruno with environment `bruno/environments/onprem.bru` (adjust `baseURL`).
