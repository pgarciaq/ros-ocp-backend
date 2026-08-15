# robne CLI — Standalone Offline/Batch Recommendations

!!! warning "Status: Phase 1 in progress"
    Parent issue: [#99](https://github.com/pgarciaq/ros-ocp-backend/issues/99).
    Contract: [`docs/plans/robne-cli-spec.md`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/plans/robne-cli-spec.md)
    (greenlit). Phase 2/3 are still planned.

!!! info "Quick Facts (planned)"
    **Tool:** `robne` — standalone CLI binary (ADR-0305)  
    **Library:** librobne — same algorithms as ros-ocp-backend and robne-operator  
    **Input:** NISE ROS CSVs, koku-metrics-operator package tarball/dir, later PostgreSQL  
    **Output:** JSON, CSV, table to stdout (Phase 1); PostgreSQL upsert (Phase 2)  
    **Config:** user file + cwd overlay — YAML replaces top-level keys; rate card merges by cluster id ([overlay](#config-overlay-yaml-and-rate-card))  
    **Infrastructure:** None — no Kafka, no API server, no Masu, no Settings API

The **reviewable contract** lives in
[`docs/plans/robne-cli-spec.md`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/plans/robne-cli-spec.md)
(not on this MkDocs nav). **This page is the public website page for the CLI**
([#99](https://github.com/pgarciaq/ros-ocp-backend/issues/99)). Overlay rules
below are the user manual; when Phase 1 ships, this page graduates to
[Features](../features/index.md) (same as Visual Insights). Do not add a second
MkDocs CLI page that duplicates the spec.

---

## What it does

The `robne` CLI reads metric data from local files (and later a database), computes
recommendations using librobne, and writes JSON, CSV, or a terminal table on stdout
(Phase 1). Phase 2 can upsert into product PostgreSQL tables. It is a
zero-infrastructure tool for development, testing, air-gapped operator packages
(`upload_toggle: false` + `oc cp`), and CI.

---

## Use cases

- **Development and testing:** compute recommendations locally without the full stack
- **CI/CD validation:** run against golden datasets to detect recommendation regressions
- **Offline analysis:** NISE CSVs or operator tarballs from restricted-network clusters
- **Data exploration:** experiment with engine configuration (percentiles, margins, terms)
- **Bulk seeding (Phase 2):** upsert recommendations into an existing ROS schema
- **Migration validation:** compare engine versions on the same input (`robne diff`, Phase 3)

---

## Planned subcommands

| Subcommand | Phase | Purpose |
|------------|-------|---------|
| `robne recommend` | 1 | Compute recommendations from input data |
| `robne validate` | 1 | Validate input format without computing |
| `robne diff` | 3 | Compare two recommendation sets |
| `robne explain` | 3 | Show explanation factors for a workload |

---

## Example usage

```bash
# Phase 1: directory of NISE or operator ROS CSVs
robne recommend --input /path/to/csvs/ --config robne.yaml --format json

# Operator package tarball (restricted network: oc cp the local package)
robne recommend --input ./metrics.tar.gz --plugins container --format table

# Optional clock and rate card (see spec §§3 and 6)
robne recommend --input ./csvs/ --now 2026-08-01T00:00:00Z \
  --rate-card card.json --format json

# Phase 2: upsert into an existing ROS database (migrations already applied)
robne recommend --input ./csvs/ --config robne.yaml \
  --output postgres://localhost:5432/ros?sslmode=disable
```

Flags stay few: `--input`, `--config`, `--plugins`, `--format`, `--rate-card`, `--now`,
and later `--output`. Percentiles, terms, decay, idle, and floors live in **YAML**, not
`--cpu-percentile` flags.

---

## Configuration YAML

Engine knobs are a YAML file. **Sample:** [`cmd/robne/robne.yaml.sample`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/cmd/robne/robne.yaml.sample).

This file is **not** the Settings API and **not** `ROS_*` admin locks. `--plugins` is an
allowlist of recommenders (`container`, later `node`, …), not `internal/plugins` registration.
Unknown keys are errors. Omitted keys use librobne compiled defaults.

**Business hours** are reserved in the schema and **not Phase 1**.

How files stack (replace vs merge): [Config overlay](#config-overlay-yaml-and-rate-card).

---

## `--now` (engine clock)

`--now` **anchors “now”** so term windows, decay weighting, and staleness are relative
to that instant, not the laptop’s wall clock. A tarball from last week is scored as
if today were the last `interval_end` in the files (unless you pass `--now` or YAML
`now`). It does **not** drop rows.

1. `--now` (RFC3339)
2. `now` in YAML
3. Max `interval_end` (else `interval_start`) in ingested rows

If no timestamp can be parsed, the CLI exits with an error. It does not silently use
`time.Now()`. Spec §3.

---

## Input formats

| Format | Source | Phase |
|--------|--------|-------|
| Directory or `.csv` | NISE `--write-monthly --ros-ocp-info`, or unpacked operator CSVs | 1 |
| `.tar.gz` | Operator local package (`upload_toggle: false`) or a NISE tarball | 1 |
| PostgreSQL digest tables | Existing ROS DB (`COPY TO` / query) | 2 |
| Prometheus JSON | Export from PromQL | 2 (not Phase 1) |

One `--input` path. Detect by filename (`DetermineCSVType`: `ocp_ros_usage` and
`ros-openshift-container-`) **and** header names.

**Cost-only files** (`cm-openshift-pod-usage`, NISE without `--ros-ocp-info`) are
rejected with an error that names the missing ROS columns.

**NISE vs operator headers** can diverge by column (order is fine; missing names
zero-fill). Operator `csvHeader()` is the contract. NISE should grow toward it
([#465](https://github.com/pgarciaq/ros-ocp-backend/issues/465), not a Phase 1
CLI blocker). Details: spec §4.

**Tarball `./` prefix:** `tar czf archive.tar.gz .` stores `./ocp_ros_usage.csv`.
The CLI strips `./` before matching. The koku listener still does exact-string
match — tracker [#466](https://github.com/pgarciaq/ros-ocp-backend/issues/466);
keep `--transform='s|^\./||'` until koku ships. Spec §8.

---

## Output formats

| Format | Target | Phase |
|--------|--------|-------|
| JSON | stdout, structured | 1 |
| CSV | stdout, spreadsheet | 1 |
| Table | stdout, terminal | 1 |
| PostgreSQL | Product tables (`workloads` then `recommendation_sets`, then other entity tables) | **2** (not Phase 1) |
| SQLite | — | **Not Phase 2.** Spec §5 (JSON is the local artifact; PG is product upsert). |

Phase 2 PostgreSQL write is an **upsert** into the product schema (migrations must
already have been applied). It is not a blind `COPY FROM` into `recommendation_sets`
alone — that table FKs to `workloads`. Spec §5.

---

## Rate card (dollars)

Savings need a JSON rate card. **Sample:** [`cmd/robne/rate-card.json.sample`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/cmd/robne/rate-card.json.sample).
Omit every file for sizing-only output. Money stays JSON (not `~/.rate-card.yaml`).

How files stack (merge by cluster id): [Config overlay](#config-overlay-yaml-and-rate-card).

**Rates are per cluster, per CPU architecture, and per GPU model.**
`by_architecture` **replaces** `default_dollars_per_core_hour` for that arch — it is
**not** added to the default. Same for `gpu.by_model` vs `default_dollars_per_gpu_month`.
Lookup per row: instance type → architecture → default. Missing cluster → error.

| Deposit | JSON location |
|---------|----------------|
| $/core-hour (CPU) | `clusters.<uuid>.cpu.default_dollars_per_core_hour`; exceptions in `by_architecture` |
| $/GiB-hour (RAM) | `clusters.<uuid>.memory.default_dollars_per_gib_hour` |
| GPU | `clusters.<uuid>.gpu.by_model`; monthly default only as unknown-model fallback |
| $/GiB-month storage | `clusters.<uuid>.storage.default_dollars_per_gib_month` |

A flat top-level `cpu_dollars_per_core_hour` is rejected. The CLI converts to integer
micro-cents (`$1` = `100_000_000`) at the boundary. No Masu HTTP. Full schema: spec §6.

`librobne` `RateCard` is still a single scalar today; the CLI catalog **resolves per row**.
Backend GPU savings also uses one `gpu_cost_per_month` — do not copy that into the CLI.

---

## Config overlay (YAML and rate card)

Robne reads **at most one user file** plus **at most one project/flag file** per kind.
It does **not** stack `$XDG…`, `~/.config/…`, and `~/.*` as three overlays.

### Shared search

**User file** — first existing wins:

| Kind | 1 (if `XDG_CONFIG_HOME` set) | 2 | 3 |
|------|------------------------------|---|---|
| Engine YAML | `$XDG_CONFIG_HOME/robne/robne.yaml` | `~/.config/robne/robne.yaml` | `~/.robne.yaml` |
| Rate card | `$XDG_CONFIG_HOME/robne/rate-card.json` | `~/.config/robne/rate-card.json` | `~/.rate-card.json` |

**Then overlay** (skipped when the matching flag is passed, so CI does not pick up a laptop cwd file):

| Kind | Cwd file | Flag that skips cwd |
|------|----------|---------------------|
| Engine YAML | `./robne.yaml` | `--config PATH` |
| Rate card | `./rate-card.json` | `--rate-card PATH` |

`ROBNE_NO_USER_CONFIG=1` skips the user YAML **and** the user rate card. It does **not** skip cwd files or `--config` / `--rate-card`.

`--config` / `--rate-card` still overlay the user file unless that env is set.

After YAML files load, `--plugins` and `--now` override `plugins` and `now`.

### Replace vs merge (they differ)

| | `robne.yaml` | `rate-card.json` |
|--|--------------|------------------|
| **Merge unit** | Top-level key (`sizing`, `idle`, `terms`, …) | Cluster id under `clusters` |
| Inside that unit | Later value **replaces the whole key**. No deep-merge of `sizing:` | Later **cluster object replaces that cluster**. No deep-merge of `cpu.by_architecture` |
| Keys only in the earlier file | Kept, if the later file omitted that top-level key | Other cluster ids **remain** |

**YAML — whole `sizing:` replaced:**

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

Effective: `plugins` = `[container]`; `idle.enabled` still `true`; `sizing` is **only**
`cpu_cost_percentile: 0.80`. User `min_margin` is **gone**. Repeat it in the project
file if you still want it.

**Rate card — merge by cluster id, replace whole cluster:**

User card has `cluster-power-prod` and `cluster-arm-gpu`. Project `./rate-card.json`
has only `cluster-power-prod`. Effective: `cluster-arm-gpu` still from the user file;
`cluster-power-prod` is **exactly** the project object (nested `by_architecture` from
the user file for that cluster is **gone**).

**Inside one cluster, `by_architecture` is a lookup**, not an add-on: an `arm64` row
with default `0.031` and `by_architecture.arm64: 0.022` uses **0.022**, not `0.053`.
Spec §§2 and 6.

**CI:** commit `./robne.yaml` / `./rate-card.json` in the repo, or pass `--config` /
`--rate-card`. Do not rely on a developer’s home files.

---

## Architecture

```
Input Sources              CLI                          Output Targets
┌──────────────┐     ┌─────────────┐              ┌──────────────┐
│ NISE CSVs    │────▶│             │──────────────▶│ JSON / CSV   │  Phase 1 stdout
│ Operator tgz │────▶│  robne CLI  │──────────────▶│ Table (tty)  │
│ PostgreSQL   │────▶│  (librobne) │──────────────▶│ PostgreSQL   │  Phase 2 upsert
└──────────────┘     └─────────────┘              └──────────────┘
```

---

## Entity type coverage

All types supported by librobne, enabled via `--plugins` / YAML `plugins`:

- Container (**Phase 1**)
- Node, VM, GPU (MIG + time-slicing), PVC, namespace quota, cluster quota, snapshot (**Phase 2**)

Node/GPU still need **container ROS CSV** as well as their own files (hooks on container ingest).

---

## Phasing

| Phase | Scope |
|-------|-------|
| **Phase 1** | Container from NISE **or** operator tarball/dir → JSON/CSV/table. YAML, `--plugins`, `--now`, `--rate-card`, `validate`. `librobne/csv` lands here. |
| **Phase 2** | Other entity types, PostgreSQL upsert (and optional digest read), `pgdigest` if needed |
| **Phase 3** | Diff, explain, CI helpers |

---

## Relationship to other planned features

- **Depends on** librobne (ADR-0303, issue #94 — extract complete)
- **Standalone binary**, not a subcommand of ros-ocp-backend (ADR-0305)
- **CSV helpers:** [#463](https://github.com/pgarciaq/ros-ocp-backend/issues/463) csv half rides with Phase 1; pgdigest with Phase 2. Operator must never import those packages.
- **Complements [Local Mode](local-mode.md)** — CLI = offline/batch; operator = real-time on-cluster
- **Complements ros-ocp-backend** — CLI = no infrastructure; backend = full pipeline

---

## Prerequisites

- librobne library extraction (issue #94) — done
- Greenlight of [`docs/plans/robne-cli-spec.md`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/plans/robne-cli-spec.md) — **done** (2026-08-16)

---

## Limitations (planned)

- No real-time collection — static snapshots only
- No API server — use ros-ocp-backend or robne-operator for API access
- No Masu, Kafka, Settings API, admin locks, or FX caches
- No cost/savings without a rate card
- No tag enrichment or cloud tag correlation (central-only)
- Business hours not in Phase 1
- No CLI-side fix for NISE missing operator columns ([#465](https://github.com/pgarciaq/ros-ocp-backend/issues/465))
