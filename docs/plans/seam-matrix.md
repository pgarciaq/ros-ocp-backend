# Cross-Repo Seam Matrix (ROS Data Contracts)

Source: [#468](https://github.com/pgarciaq/ros-ocp-backend/issues/468) (spike,
filled 2026-08-16 from then-current trees).

**Refreshed 2026-09-06:** container, namespace, and cluster-quota header cells
re-verified by column-set diff against live trees (results inline); Koku
rates/money/FX cells re-verified against live Koku tree; NetworkPolicy ingress
re-verified against the chart tree. Cells marked accordingly; everything else
retains its 2026-08-16 status. Verify any cell against the live tree before
trusting it; the trackers below own current state.

**Status:** snapshot. Cells name the source of truth, the CI lock (if any),
and the tracker. Do not fix sibling-repo bugs from this page — link or file
them in their own issues. Verify a cell against the live tree before trusting
it; the trackers below own current state.

Time box respected: this page is transcription, not a re-audit and not a
second CSV-implementation epic. Do not pause #99 Phase 1 for it. Do not
re-audit `librobne/gpu` et al. from here.

Legend: **SoT** = source of truth file. **CI** = automated lock that fails
when that SoT drifts. **Tracker** = issue or `ok`.

Parser note: ROS `buildColumnIndex` is **header-name based** (column order
does not matter; missing → zero). Drift is silent unless a contract test
lists the names.

## ROS container CSV headers

**Closed 2026-09-06** (verified by column-set diff, all 65 columns identical
both directions): operator `rosContainerRow.csvHeader()`
(`koku-metrics-operator/internal/collector/types.go`) ==
NISE `OCP_ROS_USAGE_COLUMN`
(`nise/nise/generators/ocp/ocp_generator.py`) == ROS fixture
`OperatorRosContainerCSVHeader` (now in `librobne/csv/csv_contract_test.go`;
moved from `internal/ingestion/` during the librobne extract). [#465](https://github.com/pgarciaq/ros-ocp-backend/issues/465)
is closed. The table below is the historical (pre-fix) record.

| Side | SoT | CI lock | Tracker |
|------|-----|---------|---------|
| **Operator (producer)** | `koku-metrics-operator/internal/collector/types.go` `rosContainerRow.csvHeader()` — includes `node_allocatable_cpu_cores`, `node_allocatable_memory_bytes`, `node_allocatable_gpu_count`, `instance_type`, `gpu_uuid`, `cpu_throttle_container_min` | Operator `types_test.go` asserts those names exist **in the operator repo only** | ok (producer) |
| **NISE** | `nise/nise/generators/ocp/ocp_generator.py` `OCP_ROS_USAGE_COLUMN` — **missing** `instance_type`, all three `node_allocatable_*`, `gpu_uuid`, `cpu_throttle_container_min` (has `cpu_throttle_container_avg/max/sum` only) | `tests/test_ocp_generator.py` locks **NISE's own tuple**, not operator parity | [#465](https://github.com/pgarciaq/ros-ocp-backend/issues/465) |
| **Koku listener / Masu** | Pass-through of tar members listed in `manifest.resource_optimization_files` | n/a | n/a |
| **ROS parser** | Claims SoT is operator `csvHeader()` via `internal/ingestion/csv_contract_test.go` `OperatorRosContainerCSVHeader` | Test exists **but the fixture lags the operator**: no `node_allocatable_gpu_count`, no `gpu_uuid` (allocatable CPU/mem and `instance_type` are in the fixture) | Stale copy: [#438](https://github.com/pgarciaq/ros-ocp-backend/issues/438) (lock) + [#465](https://github.com/pgarciaq/ros-ocp-backend/issues/465) (NISE). **No CI compares the three copies to each other.** |

Three header lists, zero cross-repo job. That **was** the container seam
(pre-fix record above).

## ROS namespace CSV headers

**Partially closed 2026-09-06** (verified by column-set diff, 36 columns
identical both directions): operator `rosNamespaceRow.csvHeader()` == NISE
`OCP_ROS_NAMESPACE_USAGE_COLUMN`. ROS still has **no**
`OperatorRosNamespaceCSVHeader` fixture — that remainder stays with [#438](https://github.com/pgarciaq/ros-ocp-backend/issues/438).

| Side | SoT | CI lock | Tracker |
|------|-----|---------|---------|
| **Operator** | `types.go` `rosNamespaceRow.csvHeader()` | Operator-local tests | ok (producer) |
| **NISE** | `OCP_ROS_NAMESPACE_USAGE_COLUMN` (includes `cpu_throttle_namespace_min`) | NISE-local `test_ocp_generator.py` | column-identical to operator per 2026-09-06 diff |
| **Koku** | pass-through | n/a | n/a |
| **ROS** | **No** `OperatorRosNamespaceCSVHeader` in `csv_contract_test.go` | **no** | [#438](https://github.com/pgarciaq/ros-ocp-backend/issues/438) — do not file a fourth issue until #438's "required vs optional per report type" is done |

## Other ROS / cost CSVs (PVC, GPU cost, VM, snapshot, CRQ)

**Cluster quota closed 2026-09-06** (verified by column-set diff, 20 columns
identical all three ways): operator `rosClusterQuotaRow.csvHeader()` == NISE
`OCP_ROS_CLUSTER_QUOTA_COLUMN` == ROS `OperatorRosClusterQuotaCSVHeader`
(`internal/ingestion/csv_contract_test.go`).

Note: the operator has since reorganized per-type rows out of `types.go`
(e.g. `snapshotRow` now lives in `snapshot_types.go`) — file paths in the
table below are stale even where the headers themselves were not re-verified.
Line-by-line PVC / VM / snapshot / GPU-device vs NISE remains #438 work,
not a new spike.

| Side | SoT | CI lock | Tracker |
|------|-----|---------|---------|
| **Operator** | `csvHeader()` on `storageRow`, `nvidiaGpuRow`, `rosVMRow`, `rosVMPVCRow`, `rosVMGPUDeviceRow`, `snapshotRow`, `rosClusterQuotaRow`, plus cost `podRow`/`nodeRow`/`namespaceRow` | Operator expected_reports fixtures | ok (producer, many files) |
| **NISE** | ROS: usage + namespace + cluster quota. Cost: pod/node/storage/GPU/VM files (GPU uuid on **cost** GPU CSV, not ROS container) | NISE-local | [#465](https://github.com/pgarciaq/ros-ocp-backend/issues/465) is **container** ROS; other types not inventoried column-by-column here |
| **Koku** | Cost CSVs → koku pipeline; ROS files → shipper if names match manifest | [#466](https://github.com/pgarciaq/ros-ocp-backend/issues/466) for member names | #466 |
| **ROS** | CRQ: `OperatorRosClusterQuotaCSVHeader` in `csv_contract_test.go`. PVC/VM/snapshot/GPU-device: **no** copied header fixture in that file | CRQ: yes (copy). Others: **no** | [#438](https://github.com/pgarciaq/ros-ocp-backend/issues/438) |

**Not filled in #468:** line-by-line PVC / VM / snapshot / GPU-device vs NISE. That is #438 work, not a new spike.

## `manifest.json` filenames vs tar member names

**Re-verified 2026-09-06:** [#466](https://github.com/pgarciaq/ros-ocp-backend/issues/466)
is closed; the table below is the historical record of the `./`-prefix problem.

| Side | SoT | CI lock | Tracker |
|------|-----|---------|---------|
| **Operator** | Packaging typically **no** `./` prefix (production uploads often work) | operator packaging tests | ok enough |
| **NISE / hand tar** | `tar czf archive.tar.gz .` → `./manifest.json`, `./ocp_ros_usage.csv` | none | workaround `--transform='s\|^\./\|\|'` |
| **Koku listener** | `TarFile.getnames()` **exact** equality vs manifest lists; schema **forbids** `./` in filenames | koku tests do not cover `./` members today | [#466](https://github.com/pgarciaq/ros-ocp-backend/issues/466) |
| **ROS / CLI** | Processor never sees members koku dropped. CLI Phase 1 **must** strip `./` itself | **no** CLI yet | [#99](https://github.com/pgarciaq/ros-ocp-backend/issues/99) spec §8; not a Phase 1 blocker |

## Cost model `rates[]` (tiered + tag_rates)

**Re-verified 2026-09-06 against the live Koku tree:** Masu
`_parse_rates_list` still keeps `tiered_rates[0]` only with no `tag_rates`
handling; aggregates still lack a `cost_model_gpu_cost` SUM; money values
still go through `float()`; [#467](https://github.com/pgarciaq/ros-ocp-backend/issues/467),
[#461](https://github.com/pgarciaq/ros-ocp-backend/issues/461), and [#462](https://github.com/pgarciaq/ros-ocp-backend/issues/462)
are all still open. Cells below hold.

| Side | SoT | CI lock | Tracker |
|------|-----|---------|---------|
| **Operator / NISE** | n/a | n/a | n/a |
| **Koku config** | `GET /cost-models/{uuid}/` `CostModelSerializer` — full `tiered_rates` **or** `tag_rates`. Application: `CostModelDBAccessor` (`metric_to_tag_params_map`, `price_list_effective_on`) | koku cost_models tests | ok (config + apply) |
| **Masu `effective_rates`** | **Does not use the accessor.** `_parse_rates_list` keeps `tiered_rates[0]` only; **drops `tag_rates`**. GPU UI, `project_per_month` (tag-only), tagged node/VM/storage all become `$0` or a scalar | `test_effective_rates.py` is structural; **no** tag_rates assertion | [#467](https://github.com/pgarciaq/ros-ocp-backend/issues/467) |
| **ROS mapper** | `configured_rates` → `RatePair` `{infra, suppl}` only (`internal/costdata/provider.go`) | `provider_contract_test.go` fixture is the **flattened** shape (no `tag_rates`, no GPU models) | #467 |

Public cost-models is **not** the ROS hop (identity/RBAC). Do not fold aggregates into it (#467 contract).

## `GET …/effective_rates/` payload (aggregates)

| Side | SoT | CI lock | Tracker |
|------|-----|---------|---------|
| **Koku** | `koku/masu/api/effective_rates.py` — `namespace_aggregates` SQL (`Pod`+`GPU`, CPU/mem costs, infra+markup, distributed). **Does not** `SUM(cost_model_gpu_cost)` | `test_effective_rates.py` key set **without** `cost_model_gpu_cost` | [#467](https://github.com/pgarciaq/ros-ocp-backend/issues/467) |
| **ROS** | `ClusterCostData.Namespaces` / ADR-0229 `ClusterCostDataToRateCard` (Tier B; does **not** copy `ConfiguredRates`) | mapper tests | ok for CPU/mem path; GPU spend column missing on wire |

## Money encoding

| Side | SoT | CI lock | Tracker |
|------|-----|---------|---------|
| **Koku** | Stores `Decimal`; HTTP `float()` | tests accept floats | [#461](https://github.com/pgarciaq/ros-ocp-backend/issues/461) |
| **ROS** | `float64` then `math.Round` → micro-cents | savings tests in µ¢ after convert | #461 |

## FX / `user_currency`

| Side | SoT | CI lock | Tracker |
|------|-----|---------|---------|
| **Koku** | Separate Masu `exchange_rate` / `user_currency` (ratio as string) | koku tests | [#462](https://github.com/pgarciaq/ros-ocp-backend/issues/462) |
| **ROS** | `ParseFloat`; USD fallback when currency empty | currency integration tests document the fallback | #462. **Never in CLI** (#99 spec) |

## Identity / NetworkPolicy (Masu)

| Side | SoT | CI lock | Tracker |
|------|-----|---------|---------|
| **Koku** | `effective_rates` `AllowAny` (internal) | n/a | by design |
| **Chart** | `cost-onprem/.../masu-networkpolicy.yaml` — ingress from `cost-worker`, `ros-processor`, `cost-management-api`, **`ros-api`** (adversarial "ros-api omitted" is **fixed in the chart tree**; re-verified present 2026-09-06) | still unverified whether a Helm test **asserts** `ros-api` is in the `from` list (chart tests found are e2e-style; no helm unit test located) | **ok** if NP tests exist; otherwise a chart test gap — **do not file until someone greps helm tests** (not a ROS engine bug) |

## Still empty / out of this fill

- Line-by-line PVC / VM / snapshot / GPU-device vs NISE — **#438**, not a new issue.
- Helm test for `ros-api` on Masu NP — optional chart follow-up; not filed.
- **Closed since the 2026-08-16 fill and reflected above:** container headers
  (#465), cluster-quota headers, tar `./` handling (#466).
- **No new unnamed-gap issues** from this fill. Every broken cell already has #438 / #461 / #462 / #465 / #466 / #467 / #99.

## Not this page

| Issue | What it already is |
|-------|-------------------|
| #438 | **Implement** operator↔robne CSV freeze (fixtures + CI). Do the CSV *lock* there, not here. |
| #428 | Rebrand: do not rename Cost Management wires. |
| #461 #462 #465 #466 #467 | Known seam **bugs**. Sequence/fix them; do not duplicate the write-up here. |
