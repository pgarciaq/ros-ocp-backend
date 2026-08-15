# librobne

In-process, statically linked recommendation engine for OpenShift resource
optimization. Nested module of [ros-ocp-backend](https://github.com/redhatinsights/ros-ocp-backend).

Import path: `github.com/redhatinsights/ros-ocp-backend/librobne/...`

The parent module has `replace => ./librobne`. After editing this tree, run
`go mod vendor` in the parent so `vendor/` stays in sync (Go vendor mode copies
the nested module). Core has **no** pgx, Echo, Kafka, or GORM. Consumers scan
or parse into canonical types and call `Recommend*` with no pool. **Zero
convert loops.**

| Package | What |
|---------|------|
| `types` | Canonical container digest/rec types, RateCard, idle, notifications |
| `digest` | `ComputeDigest` / `ComputeWeightedDigest` (exact sort, nearest-lower-rank) |
| `engine` | `RecommendWorkloads` (no pool) |
| `container` | CPU/memory recommend, notifications, replica helper, `ApplySavingsEstimates` |
| `savings` | Re-export of `ApplySavingsEstimates` |
| `namespace` | `RecommendNamespaces` |
| `snapshot` | `ClassifySnapshotInventory` |
| `node` | `RecommendNodes` |
| `gpu` | MIG classify/select, timeslicing `WithSettings`, embedded catalogs |
| `vm` | `RecommendVM` |
| `pvc` | `RecommendPVCs` / `ComputePVCRecommendation` |
| `quota` | Namespace and cluster quota `Recommend*` / `Compute*` |

`Apply*` is a **separate** call after emit. Empty RateCard does not invent `"USD"`.
Quota currency is deposited on `QuotaRecConfig` by the product.

Optional `csv` / `pgdigest` are P5.
