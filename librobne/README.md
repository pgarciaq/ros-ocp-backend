# librobne

In-process, statically linked recommendation engine for OpenShift resource
optimization. Nested module of [ros-ocp-backend](https://github.com/redhatinsights/ros-ocp-backend).

Import path: `github.com/redhatinsights/ros-ocp-backend/librobne/...`

The parent module has `replace => ./librobne`. After editing this tree, run
`go mod vendor` in the parent so `vendor/` stays in sync (Go vendor mode copies
the nested module). Core has **no** pgx, Echo, Kafka, or GORM. Consumers scan
or parse into `types.DigestRow` / `types.KeyedDigest` and call
`engine.RecommendWorkloads`. **Zero convert loops.**

| Package | What |
|---------|------|
| `types` | Canonical container digest/rec types, RateCard, idle, notifications |
| `digest` | `ComputeDigest` / `ComputeWeightedDigest` (exact sort, nearest-lower-rank) |
| `engine` | `RecommendWorkloads` (no pool) |
| `container` | CPU/memory recommend, notifications, replica helper, `ApplySavingsEstimates` |
| `savings` | Re-export of `ApplySavingsEstimates` |

`Apply*` is a **separate** call after emit. Empty RateCard does not invent `"USD"`.

Node/VM/GPU/PVC/quota/namespace/snapshot stay in ros-ocp-backend until P4+.
Optional `csv` / `pgdigest` are P5.
