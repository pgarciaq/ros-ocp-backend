# robne CLI specification (for greenlight)

**Status:** **Greenlit** (2026-08-16) — implement **Phase 1 only** (child issue).  
**Parent issue:** [#99](https://github.com/pgarciaq/ros-ocp-backend/issues/99)  
**Planned-feature page (public MkDocs):** [docs-site/planned-features/robne-cli.md](../../docs-site/planned-features/robne-cli.md)  
→ [https://pgarciaq.github.io/ros-ocp-backend/planned-features/robne-cli/](https://pgarciaq.github.io/ros-ocp-backend/planned-features/robne-cli/)  
**ADRs:** [ADR-0305](../adr/0305-robne-cli-standalone-binary.md) (standalone binary), [ADR-0303](../adr/0303-library-extraction-librobne.md) (librobne)

This is the review artifact for #99. Child GitHub issues are **proposed below, not filed**, until you approve the tree.

**Documentation surfaces (do not create a second public site now):**

| Surface | Audience | In #99? |
|---------|----------|---------|
| This spec (`docs/plans/`) | Implementers / greenlight | Yes — contract. **Not** in MkDocs nav. |
| [planned-features/robne-cli.md](../../docs-site/planned-features/robne-cli.md) | Public GitHub Pages | **Yes — this is the public page today.** Overlay rules live here so users do not need the spec. |
| `docs-site/features/` (or a CLI getting-started page) | Public, after the binary ships | **Yes, Phase 1 docs** — graduate the planned page the same way Visual Insights moved. Not a separate GitHub issue. |
| Standalone `robne-cli` repo user manual | Public, if/when ADR-0305 splits the repo | Later; until then this docs-site page is enough. |

Do **not** add a second MkDocs entry that duplicates this spec. Keep one public page; keep the spec as the review contract.

---

## 0. Issue tree (recommendation)

**Keep [#99](https://github.com/pgarciaq/ros-ocp-backend/issues/99) as the parent.** Do not grow the #99 body into this spec. The issue stays a short product umbrella; this file is the contract.

Proposed children (file only after greenlight):

| Proposed child | Repo | Scope |
|----------------|------|--------|
| **#99 Phase 1 — recommend** ([#469](https://github.com/pgarciaq/ros-ocp-backend/issues/469)) | `pgarciaq/ros-ocp-backend` | Container path: tarball/dir/CSV in, YAML knobs, `--plugins`, `--now`, `--rate-card`, JSON/CSV/table out. First commits land `librobne/csv` (was [#463](https://github.com/pgarciaq/ros-ocp-backend/issues/463) csv half). Public docs: keep [planned-features/robne-cli.md](../../docs-site/planned-features/robne-cli.md); graduate to Features when the binary ships. |
| **#99 Phase 2 — entities + PostgreSQL** | same | Remaining entity types + write (and optional read) PostgreSQL. `librobne/pgdigest` if shared digest SQL is still needed. |
| **#99 Phase 3 — diff / explain / CI** | same | `robne diff`, `robne explain`, CI helpers. |
| **[#465](https://github.com/pgarciaq/ros-ocp-backend/issues/465) NISE ROS column parity** | `nise` (fix); this fork tracks | Add operator columns NISE omits (see §4). Not a CLI blocker. |
| **[#466](https://github.com/pgarciaq/ros-ocp-backend/issues/466) Koku tarball member names** | `koku` (fix); this fork tracks | Normalize `./` prefixes when matching manifest files (see §8). Not a CLI blocker; CLI still self-normalizes. |

[#463](https://github.com/pgarciaq/ros-ocp-backend/issues/463) stays the I/O-package tracker: **csv rides with Phase 1**; **pgdigest waits for Phase 2**. The operator ([#138](https://github.com/pgarciaq/ros-ocp-backend/issues/138)) must **never** import `csv` or `pgdigest`.

**Never in the CLI (any phase):** Settings API clone, plugin `init()` registry, Masu HTTP, admin env locks, Kafka, FX / `user_currency` caches ([#462](https://github.com/pgarciaq/ros-ocp-backend/issues/462) is independent).

---

## 1. Product shape

`robne` is a **standalone static binary** (ADR-0305), not a ros-ocp-backend subcommand. It imports librobne in-process and computes the same recommendations as the processor.

**Phase 1 command:**

```text
robne recommend \
  --input <dir|file.csv|file.tar.gz> \
  --config robne.yaml \
  --plugins container \
  --format json|csv|table \
  --rate-card card.json \   # optional
  --now RFC3339             # optional; see §3
```

Flags stay few. Engine knobs live in YAML (§2). Cost rates live in the rate-card file (§6).
Sample files live in [`cmd/robne/`](../../cmd/robne/) (`robne.yaml.sample`, `rate-card.json.sample`) — copy next to `main.go` when Phase 1 lands.

**Phase 1 is not the full wishlist.** Container + file I/O + YAML + rate card + `--now` is enough to greenlight coding.

---

## 2. Configuration YAML

This file is **engine configuration only**. It is not the Settings API, not `ROS_*` admin locks, and not a plugin registry.

**Sample:** [`cmd/robne/robne.yaml.sample`](../../cmd/robne/robne.yaml.sample) (copy to `./robne.yaml` or the user file below).

Omitted keys use compiled defaults from `librobne/engine.DefaultEngineConfig` / `DefaultTerms` / `DefaultContainerSizingThresholds` / `DefaultIdleConfig`.

### Search and overlay

**Yes — also read a per-user file.** Same schema as `./robne.yaml`. Paths **1–3 are candidates for a single user file** (first existing wins). **`./robne.yaml` is an extra overlay** after that — same search pattern as the rate card (§6), but **merge units differ** (table below).

**User file** (pick **one**; stop at the first that exists):

| # | Path | When it is considered |
|---|------|------------------------|
| 1 | `$XDG_CONFIG_HOME/robne/robne.yaml` | Only if `XDG_CONFIG_HOME` is set |
| 2 | `~/.config/robne/robne.yaml` | If #1 is missing or `XDG_CONFIG_HOME` is unset |
| 3 | `~/.robne.yaml` | If #1 and #2 are missing |

When `XDG_CONFIG_HOME` is unset, #1 is skipped; #2 is the usual XDG default. Do **not** read #2 and #3 and merge them.

**Then overlay:**

| # | Path | When |
|---|------|------|
| 4 | `./robne.yaml` | If present **and** `--config` was **not** passed |
| — | `--config PATH` | If passed: overlay this file; **skip** #4 (CI must not pick up a laptop cwd YAML) |

**Load order:**

1. Compiled librobne defaults
2. The one user file from #1–#3, if any
3. `./robne.yaml` (#4)
4. `--config PATH` if passed — overlays the user file; skips #4

`--config` does **not** skip the user file. To ignore #1–#3: `ROBNE_NO_USER_CONFIG=1` (or `--no-user-config` if added). Document the env var in `--help`. It does not skip #4 or `--config`.

**CI:** commit a project `robne.yaml` or pass `--config`; do not rely on a developer’s home file.

### Replace semantics (YAML vs rate card)

Both files overlay. They do **not** overlay the same way:

| | `robne.yaml` | `rate-card.json` |
|--|--------------|------------------|
| User paths | `$XDG…/robne/robne.yaml` → `~/.config/robne/robne.yaml` → `~/.robne.yaml` | `$XDG…/robne/rate-card.json` → `~/.config/robne/rate-card.json` → `~/.rate-card.json` |
| Cwd file | `./robne.yaml` | `./rate-card.json` |
| Flag skips cwd | `--config PATH` | `--rate-card PATH` |
| `ROBNE_NO_USER_CONFIG=1` | skips user YAML | skips user JSON (same env) |
| **Merge unit** | **Top-level YAML key** (`sizing`, `idle`, `terms`, …) | **Cluster id** under `clusters` |
| Inside that unit | The later file’s value **replaces the whole key**. No deep-merge of `sizing:` maps. | The later file’s cluster object **replaces that cluster**. No deep-merge of `cpu.by_architecture`. Other cluster ids from the earlier file **remain**. |
| After files | `--plugins` / `--now` override YAML `plugins` / `now` | (no per-cluster flags) |

**YAML example — whole `sizing:` replaced, other keys kept:**

User `~/.config/robne/robne.yaml`:

```yaml
sizing:
  cpu_cost_percentile: 0.60
  min_margin: 1.15
idle:
  enabled: true
```

Project `./robne.yaml`:

```yaml
plugins:
  - container
sizing:
  cpu_cost_percentile: 0.80
```

Effective config: `plugins` = `[container]`; `idle` still from the user file; `sizing` is **only** `cpu_cost_percentile: 0.80` — `min_margin` from the user file is **gone**. To keep `min_margin`, repeat it in the project file.

**Rate-card example — merge by cluster id, replace whole cluster:** see §6. A project file that only lists `cluster-power-prod` leaves the user’s `cluster-arm-gpu` in place, and **replaces** `cluster-power-prod` entirely (including nested `by_architecture`).

```yaml
# robne.yaml — Phase 1 schema
org_id: "1234567"                 # required for PostgreSQL write (Phase 2); optional in Phase 1
cluster_uuid: "local-cluster"     # same

# Clock for decay / staleness. CLI flag --now overrides this.
# If both omitted: max interval_end (else interval_start) across ingested rows.
now: null                         # RFC3339, or omit

plugins:                          # allowlist; flag --plugins overrides
  - container

terms:
  - name: short
    window_days: 1
    min_data_days: 1
    decay_half_life_hours: 0
    replica_target_utilization_pct: 70
  - name: medium
    window_days: 7
    min_data_days: 3
    decay_half_life_hours: 168
    replica_target_utilization_pct: 70
  - name: long
    window_days: 15
    min_data_days: 7
    decay_half_life_hours: 360
    replica_target_utilization_pct: 70

sizing:                           # maps to types.SizingThresholdSettings
  cpu_cost_percentile: 0.60
  cpu_perf_percentile: 0.98
  mem_cost_percentile: 0.95
  mem_perf_percentile: 1.0
  min_margin: 1.15
  max_margin: 1.50
  limit_multiplier: 1.05
  cpu_floor_mc: 25
  mem_floor_kib: 4096
  idle_cpu_threshold_mc: 10
  idle_mem_threshold_kib: 10240
  mem_trend_slope_threshold: 100.0
  low_confidence_threshold: 0.5
  sparse_data_threshold: 2

idle:                             # maps to types.IdleConfig
  enabled: true
  zombie_cpu_p95_mc: 1
  zombie_cpu_peak_mc: 10
  idle_cpu_util_pct: 2
  idle_mem_util_pct: 5
  burst_ratio: 10
  min_observation_days: 14
  exclude_namespaces:
    - kube-system
    - openshift-*
  exclude_workload_types:
    - daemonset

staleness_hours: 48               # EngineConfig.StalenessThreshold

# NOT Phase 1 — reserved so the schema does not churn later
# business_hours:
#   enabled: false
# node: { ... }
# gpu: { ... }
# pvc: { ... }
# vm: { ... }
# quota: { ... }
```

**Validation:** unknown keys are errors (no silent ignore). Percentiles must be in `(0, 1]`. `plugins` must be a known entity name. Empty `terms` is an error (do not silently use defaults if the key is present and empty).

**`--plugins`:** comma-separated allowlist (`container`, later `node`, `namespace`, …). Same idea as “enable recommenders,” **not** `internal/plugins` `init()` registration.

---

## 3. `--now` (engine clock)

**Yes: it anchors “now” so term windows, decay, and staleness are relative to that instant — not the wall clock.**

Librobne does not use `time.Now()` for scoring. It uses `EngineConfig.Now`:

| Mechanism | Meaning relative to `Now` |
|-----------|---------------------------|
| **Term `window_days`** | Only digest hours in `[Now − window, Now]` count for that term (short = last 1 day before `Now`, medium = last 7 days before `Now`, …) |
| **Decay `decay_half_life_hours`** | Exponential weight: hours closer to `Now` count more; older hours in the window count less |
| **Staleness `staleness_hours`** | If the newest ingested point is older than `Now − staleness`, the workload is stale |
| **Idle `min_observation_days`** | Observation length is measured back from `Now` |

Wall-clock `time.Now()` is wrong for a tarball collected last week: on 16 Aug you would look at 9–16 Aug, find no (or sparse) rows, and mark everything stale. Anchoring `Now` at the data’s last `interval_end` (e.g. 7 Aug) makes “medium term” mean 1–7 Aug.

**`--now` is not a row filter.** It does not drop CSV lines. Rows outside a term’s window are simply unused **for that term** (the same as the processor). All rows are still ingested.

Resolution order:

1. `--now` (RFC3339)
2. `now` in YAML (project or user file, after overlay)
3. **Max timestamp in ingested rows** (`interval_end`, else `interval_start`)

If no timestamp can be parsed, exit non-zero with a clear error — do not fall back to wall clock.

---

## 4. Input: NISE vs operator CSVs

**One `--input` path.** Detect by filename pattern (`DetermineCSVType`) **and** header names. Support:

| Source | Typical names |
|--------|----------------|
| NISE (`--write-monthly --ros-ocp-info`) | `ocp_ros_usage.csv`, date/UUID prefixes |
| Operator (including `upload_toggle: false`) | `ros-openshift-container-*.csv` inside the local package tarball |
| Cost-only (NISE without `--ros-ocp-info`, or `cm-openshift-pod-usage`) | **Reject** with an error that names the missing ROS columns |

Input may be a directory, a single CSV, or a `.tar.gz` (operator package or hand-rolled NISE tarball). Strip `./` from tar member names before matching (see §8).

Parser is **header-name based**, so column **order** may differ. Missing optional columns zero-fill.

### Why NISE and the operator can diverge

They are **two generators**, not one schema file:

- Operator header: `koku-metrics-operator/internal/collector/types.go` `rosContainerRow.csvHeader()`
- NISE header: `nise/generators/ocp/ocp_generator.py` `OCP_ROS_USAGE_COLUMN`
- ROS already maps both filename families in `internal/utils/utils.go` `DetermineCSVType`
- ROS contract fixture: `internal/ingestion/csv_contract_test.go` `OperatorRosContainerCSVHeader` (currently **lags** the operator: missing `node_allocatable_gpu_count` and `gpu_uuid`)

**Container ROS columns present in the operator and absent from NISE `OCP_ROS_USAGE_COLUMN` today:**

| Column | Why it matters |
|--------|----------------|
| `node_allocatable_cpu_cores` | Node / packing math (capacity vs allocatable) |
| `node_allocatable_memory_bytes` | Same |
| `node_allocatable_gpu_count` | GPU node inventory |
| `instance_type` | Node / MachineSet context |
| `cpu_throttle_container_min` | Throttle range (NISE has avg/max/sum only; comment cites operator #705) |
| `gpu_uuid` | GPU identity (NISE emits this on **cost** GPU rows, not on `ocp_ros_usage`) |

Shared metric columns (requests, usage, RSS, OOM, accelerator SM/DRAM/tensor, …) match by **name**. Order differs (NISE starts `interval_start`; operator starts `report_period_start`). That is fine.

**Should that be fixed?** **Yes, in NISE.** Tracker: [#465](https://github.com/pgarciaq/ros-ocp-backend/issues/465) (implementation in `project-koku/nise`). Not a Phase 1 CLI blocker. Phase 1 still accepts today’s NISE files; node/GPU quality from NISE stays weaker until parity lands.

**How the inconsistency happened:** not one sloppy native-engine commit. NISE *did* add capacity, machineset, OOM, replicas, and DCGM columns (Apr–Jun 2026). `instance_type` was in NISE-1.1 and never added. Operator then grew allocatable + ROS-container `gpu_uuid` (2026-07-23) with no nise follow-up. `cpu_throttle_container_min` has been on the operator since `#705` (2025-09) and nise only TODOs it on the namespace CSV. Three header copies, no CI lock — full write-up on #465.

**Do not** invent a third header. Operator `csvHeader()` is the contract. NISE should grow toward it. Update `OperatorRosContainerCSVHeader` when the operator set is the source of truth.

NISE pitfalls already documented elsewhere: use `--write-monthly` + `--ros-ocp-info`; do **not** use `--insights-upload` combined `openshift_report.*.csv` files.

---

## 5. Output stores (PostgreSQL in Phase 2; not SQLite)

**Phase 1 does not write a database.** `--format json|csv|table` is stdout (or a file if we add `--output PATH` as a filename, not a DSN). No `postgres://` or `sqlite://` in Phase 1.

**Phase 2** adds **PostgreSQL upsert** only. `--output postgres://…` (libpq URL). The CLI does **not** run migrations. The target database must already be at the product schema.

### SQLite — considered, not Phase 2

Do **not** replace PostgreSQL with SQLite. Do **not** add SQLite alongside PostgreSQL in Phase 2.

They are different products:

| Store | Job |
|-------|-----|
| JSON / CSV / table | Portable artifact (laptop, CI, air-gap). Phase 1 already does this. Phase 3 `diff` compares these files. |
| PostgreSQL | Seed or round-trip a **real ROS database** (then the API/UI). That is why Phase 2 exists. |
| SQLite | A third persist path. Not needed for either job above. |

Reasons SQLite stays out of Phase 2:

1. **Product schema is PostgreSQL** (`JSONB`, PG types, product migrations). A SQLite file that pretends to be `workloads` / `recommendation_sets` is a second schema to maintain and will drift. A CLI-owned SQLite schema is yet another data model.
2. **ADR-0305** wants a static binary with no CGO. `github.com/mattn/go-sqlite3` needs CGO. Pure-Go SQLite exists; it is extra weight for a use case JSON already covers.
3. **Phase 3 `robne diff`** should diff two recommend outputs (JSON), not require a queryable DB.

If a later child issue wants `sqlite://./robne.db` for ad-hoc SQL, that is **Phase 3+ optional**, CLI-owned schema, **in addition to** PostgreSQL — never a substitute for the product upsert.

### Phase 2 PostgreSQL write

Writes follow the processor’s tables, not a parallel CLI schema:

| Phase 2 first | Later with other plugins |
|---------------|--------------------------|
| `workloads` (FK parent) then `recommendation_sets` (unique `(workload_id, container_name)`) | `namespace_recommendation_sets`, node/GPU/PVC/quota/snapshot tables |

`COPY FROM` is the bulk tool where a staging table or unconstrained copy is safe. Because `recommendation_sets` FKs to `workloads`, the implementation is **upsert** (`INSERT … ON CONFLICT`) or `COPY` into staging then upsert — not a blind `COPY` into `recommendation_sets` alone. The planned-feature page previously implied a single `COPY FROM`; this spec corrects that.

`org_id` and `cluster_uuid` come from YAML (required when `--output` is PostgreSQL).

**Out of scope for Phase 2:** Masu HTTP, Kafka, historical rec tables unless a child issue explicitly adds them.

Optional Phase 2 read: `COPY TO` / query digest tables so a laptop can recompute from an existing ROS DB. That is input, not a substitute for CSV ingest.

---

## 6. Rate card (where you deposit $/core-hour)

Deposit rates in a **JSON file** (not engine YAML, not Masu, not the Settings API). Keep money in JSON so there is one unmarshal path; do **not** add `~/.rate-card.yaml`.

**Sample:** [`cmd/robne/rate-card.json.sample`](../../cmd/robne/rate-card.json.sample).

Empty / omitted after search: **sizing only**, no dollar savings (`RateCard.IsEmpty()`). Currency is unset (never default `"USD"`).

### Search and overlay

**Yes — a per-user rate card**, same JSON schema. The numbered paths below are **not** four overlays stacked. **1–3 are candidates for a single user file** (first existing wins). **`./rate-card.json` is an extra overlay** after that.

**User file** (pick **one**; stop at the first that exists):

| # | Path | When it is considered |
|---|------|------------------------|
| 1 | `$XDG_CONFIG_HOME/robne/rate-card.json` | Only if `XDG_CONFIG_HOME` is set |
| 2 | `~/.config/robne/rate-card.json` | If #1 is missing or `XDG_CONFIG_HOME` is unset |
| 3 | `~/.rate-card.json` | If #1 and #2 are missing |

When `XDG_CONFIG_HOME` is unset, #1 is skipped; #2 is the usual XDG default (`~/.config/...`). Do **not** read #2 and #3 and merge them.

**Then overlay:**

| # | Path | When |
|---|------|------|
| 4 | `./rate-card.json` | If present **and** `--rate-card` was **not** passed |
| — | `--rate-card PATH` | If passed: overlay this file; **skip** #4 (CI must not pick up a laptop cwd card) |

**Load order:**

1. Empty card
2. The one user file from #1–#3, if any
3. `./rate-card.json` (#4) — `clusters` **merge by cluster id** (later file replaces that cluster’s entire object; does not deep-merge `cpu.by_architecture` inside a cluster)
4. `--rate-card PATH` if passed — same cluster-id merge onto the user file; skips #4

`ROBNE_NO_USER_CONFIG=1` skips #1–#3 (one switch for engine YAML and money). It does not skip #4 or `--rate-card`.

YAML overlay uses **top-level keys**; rate-card overlay uses **cluster ids**. Comparison and YAML example: §2 *Replace semantics*.

**Rate-card example — merge by cluster id, replace whole cluster:**

User `~/.config/robne/rate-card.json` has `cluster-power-prod` and `cluster-arm-gpu`.
Project `./rate-card.json` has only `cluster-power-prod` with a new `cpu` block.

Effective card: `cluster-arm-gpu` still from the user file; `cluster-power-prod` is **exactly** the project object — nested `by_architecture` / `by_instance_type` from the user file for that cluster are **gone**. To keep a nested key, repeat it in the later file.

**One file must not imply one price for every cluster and every GPU.** An NVIDIA A100 is not an RTX 6000. An IBM POWER core is not an ARM `aarch64` core. Distinct OpenShift clusters have distinct cost models.

### Today’s engine is too flat (CLI must not copy that)

`librobne/types.RateCard` is a **single** CPU / RAM / GPU / storage scalar. SaaS `GetEffectiveRates(org, cluster)` is already **per cluster**, but:

- Container savings uses **Tier B** namespace spend (blended), not per-arch CPU.
- GPU savings (`ApplyGPUSavings`) reads **one** `configured_rates["gpu_cost_per_month"]` and ignores `GPUModelName` for the dollar rate (model is only used for MIG slice math).

The CLI catalog is the place to do this **right**. Resolve to a scalar `RateCard` (or a lookup) **per recommendation row**: cluster → CPU arch / instance type → GPU model.

### File shape

Human dollars; CLI converts once at the boundary (`MicroCentsPerDollar = 100_000_000`).

```json
{
  "currency": "USD",
  "markup_percent": 0,
  "clusters": {
    "cluster-power-prod": {
      "distribution": "cpu",
      "cpu": {
        "default_dollars_per_core_hour": 0.055,
        "by_architecture": {
          "s390x": 0.060
        }
      },
      "memory": { "default_dollars_per_gib_hour": 0.006 },
      "gpu": {
        "default_dollars_per_gpu_month": 0,
        "by_model": {}
      },
      "storage": { "default_dollars_per_gib_month": 0.10 }
    },
    "cluster-arm-gpu": {
      "distribution": "cpu",
      "cpu": {
        "default_dollars_per_core_hour": 0.031,
        "by_architecture": { "arm64": 0.022 }
      },
      "memory": { "default_dollars_per_gib_hour": 0.004 },
      "gpu": {
        "default_dollars_per_gpu_month": 800,
        "by_model": {
          "NVIDIA A100-SXM4-80GB": 2400,
          "NVIDIA RTX 6000 Ada Generation": 900
        }
      },
      "storage": { "default_dollars_per_gib_month": 0.10 }
    }
  }
}
```

| You want | Where in the file |
|----------|-------------------|
| $/core-hour (CPU) | `clusters.<id>.cpu.default_dollars_per_core_hour` **or** `by_architecture` (see lookup — not added together) |
| Optional SKU override | `cpu.by_instance_type` (e.g. `m6g.xlarge`) |
| $/GiB-hour (RAM) | `memory.default_dollars_per_gib_hour` |
| GPU (A100 ≠ RTX 6000) | `gpu.by_model` keyed by `accelerator_model_name` / `GPUModelName`; `default_dollars_per_gpu_month` only if the model is unknown |
| $/GiB-month storage | `storage.default_dollars_per_gib_month` |
| markup on Tier A | top-level `markup_percent` (10 → 1000 bp), unless a cluster overrides |

GPU uses **month** to match Koku `gpu_cost_per_month` / `ApplyGPUSavings`. Optional `dollars_per_gpu_hour` may be accepted and converted with calendar hours (ADR-0326).

### `default_*` vs `by_architecture` / `by_model` (replace, do not add)

**`by_architecture` is not added to `default_dollars_per_core_hour`.** It is a lookup table. For a given row, **one** CPU number wins:

```text
clusters[yaml.cluster_uuid]     # missing cluster → error (no silent global CPU rate)
  1. cpu.by_instance_type[row.instance_type]   if instance_type is non-empty and the key exists
  2. cpu.by_architecture[row.arch]             if arch is non-empty and the key exists
  3. cpu.default_dollars_per_core_hour         if set
  4. else no CPU dollar savings for that row
```

Same pattern for GPU: `by_model[name]` **replaces** `default_dollars_per_gpu_month`; default is only the unknown-model fallback (and only if `> 0`).

Examples:

- Cluster is all `ppc64le`: set `default_dollars_per_core_hour` only. You do **not** need `by_architecture.ppc64le` unless you want it documented twice.
- Mixed `amd64` + `arm64` on one cluster: set `default_*` for the usual arch (or omit it) and `by_architecture.arm64` / `amd64` for the exceptions. A row with `arch=arm64` uses **0.022**, not `0.031+0.022`.
- Duplicate values (`default: 0.055` and `by_architecture.ppc64le: 0.055`) are allowed and redundant.

**Lookup (cluster):** `clusters[cluster_uuid]` from YAML `cluster_uuid` / input. Missing cluster → error. Missing GPU model → `gpu.default_*` if > 0, else skip GPU savings. Missing arch → CPU `default_*` (step 3).

**Data gap:** ROS container CSV has `accelerator_model_name` and (operator) `instance_type`. It does **not** have `node_architecture`. Arch lives in cost `node_labels` (`label_kubernetes_io_arch`). Until operator+NISE emit `node_architecture` on ROS rows, `by_architecture` only applies when that column exists; otherwise use the **cluster** CPU default (put POWER and ARM in **different cluster keys**). Follow-up: add `node_architecture` to operator `csvHeader()` + NISE (can ride with [#465](https://github.com/pgarciaq/ros-ocp-backend/issues/465)).

**No Tier B** in the CLI. No Masu. Flat top-level `cpu_dollars_per_core_hour` (old draft) is **rejected** — force the `clusters` map so a multi-cluster tarball cannot silently share one POWER rate.

Projection hours stay an Apply* argument, not a card field.

---

## 7. Business hours

**Not Phase 1.** Digest BH filtering stays a later YAML block (`business_hours.enabled`). Do not wire `bhschedule` until a child issue says so.

---

## 8. Tarball `./` prefix — should we fix ingest?

**Yes.** Documented workaround (`tar czf … --transform='s|^\./||' .`) is not a substitute for a parser fix.

**What breaks today (koku listener):**

- `extract_tarball_to_directory` returns `TarFile.getnames()` (member strings as archived).
- `get_manifest_member_name` requires the **exact** string `manifest.json`. Members from `tar czf archive.tar.gz .` are typically `./manifest.json` → **“No manifest found in payload.”**
- ROS routing uses `if ros_file in payload_files` (exact match of manifest `resource_optimization_files` vs member names). `ocp_ros_usage.csv` vs `./ocp_ros_usage.csv` → empty `ros_reports` → “No ROS reports to handle in the current payload.”

That is a **koku** bug (`masu/external/kafka_msg_handler.py` + `masu/util/ocp/common.py`). Tracker: [#466](https://github.com/pgarciaq/ros-ocp-backend/issues/466). Fix by normalizing members: strip a leading `./`, compare `posixpath.basename`. Keep the transform workaround in docs until that ships.

**CLI Phase 1** must normalize the same way when it opens a `.tar.gz`, even if koku is unfixed — otherwise `oc cp` of an operator package still works, but a `tar czf .` NISE archive would not.

Do **not** wait on the koku patch to start Phase 1.

---

## 9. Phasing (locked for greenlight)

| Phase | In | Out |
|-------|----|-----|
| **1** | Container ROS CSV (NISE **or** operator tarball/dir); YAML; `--plugins`; `--now`; `--rate-card`; `validate` | JSON / CSV / table |
| **2** | Other entity CSVs; PostgreSQL upsert; optional PG digest read | Same files + postgres URL |
| **3** | `diff`, `explain`, CI exit codes / JUnit | — |

`librobne/csv` is the first code of Phase 1. `pgdigest` is Phase 2.

---

## 10. Greenlight checklist

Reply on #99 (or here) with yes/no:

1. Parent #99 + children as in §0 (file children after this yes).
2. Phase 1 scope as in §9 (container / files / YAML / rate card / `--now` only).
3. YAML schema §2 (unknown keys = error; user overlay; **replace whole top-level keys**, no deep-merge of `sizing:`; `cmd/robne/robne.yaml.sample`). Public overlay docs: planned-features/robne-cli.md.
4. `--now` anchors `EngineConfig.Now` for windows/decay/staleness; never wall clock as silent fallback (§3).
5. NISE column gap: accept today’s files; fix NISE via [#465](https://github.com/pgarciaq/ros-ocp-backend/issues/465) (§4).
6. Phase 1 = JSON/CSV/table only; PostgreSQL upsert in Phase 2; **no SQLite in Phase 2** (§5).
7. Rate card JSON in dollars (§6): **`clusters` map**; overlay **merges by cluster id** (later file replaces that cluster object, not nested maps); `by_architecture` **replaces** `default_*` for that arch (not added); GPU `by_model` same rule. User `~/.config/robne/rate-card.json`. Sample `cmd/robne/rate-card.json.sample`. No `~/.rate-card.yaml`. No global scalar card.
8. Business hours not Phase 1 (§7).
9. Fix koku `./` matching in koku ([#466](https://github.com/pgarciaq/ros-ocp-backend/issues/466)); CLI also normalizes (§8).

No code beyond Phase 1 until a later child is filed. Phase 1 starts after this list was accepted (2026-08-16).
