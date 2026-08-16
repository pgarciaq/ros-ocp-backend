# robne CLI — Standalone Offline/Batch Recommendations

!!! success "Status: Phase 1, 2a, and pgdigest shipped"
    Parent issue: [#99](https://github.com/pgarciaq/ros-ocp-backend/issues/99).
    Implementation: [#469](https://github.com/pgarciaq/ros-ocp-backend/issues/469),
    [#471](https://github.com/pgarciaq/ros-ocp-backend/issues/471),
    [#463](https://github.com/pgarciaq/ros-ocp-backend/issues/463).
    Contract: [`docs/plans/robne-cli-spec.md`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/plans/robne-cli-spec.md)
    (not on this MkDocs nav). Build: `make robne` or `make build-all` → `bin/robne`.
    **Next:** digest SELECT ([#474](https://github.com/pgarciaq/ros-ocp-backend/issues/474)) or other-entity CSVs ([#472](https://github.com/pgarciaq/ros-ocp-backend/issues/472)).
    2b–2d remainder and Phase 3 (`diff` / `explain`) are still planned. The old [planned-features URL](../planned-features/robne-cli.md) is a
    bookmark stub.

!!! info "Quick Facts"
    **Tool:** `robne` — standalone CLI binary (ADR-0305)  
    **Library:** librobne — same algorithms as ros-ocp-backend and robne-operator  
    **Input:** NISE ROS CSVs, koku-metrics-operator package tarball/dir; later this CLI’s digest tables (2d, after pgdigest)  
    **Output:** JSON, CSV, table to stdout (Phase 1); PostgreSQL upsert of recs + container digests (Phase **2a** + [#463](https://github.com/pgarciaq/ros-ocp-backend/issues/463), then 2c)  
    **Config:** user file + cwd overlay — YAML replaces top-level keys; rate card merges by cluster id ([overlay](#config-overlay-yaml-and-rate-card))  
    **Infrastructure:** None — no Kafka, no API server, no Masu, no Settings API

The **reviewable contract** lives in
[`docs/plans/robne-cli-spec.md`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/plans/robne-cli-spec.md)
(not on this MkDocs nav). **This page is the public website page for the CLI**
([#99](https://github.com/pgarciaq/ros-ocp-backend/issues/99)). Overlay rules
below are the user manual. Do not add a second MkDocs CLI page that duplicates
the spec.

---

## What it does

The `robne` CLI reads metric data from local files (and later a database), computes
recommendations using librobne, and writes JSON, CSV, or a terminal table on stdout
**(a)(b)**. Phase **2a** plus pgdigest is **(c)**: a Postgres this CLI owns (embed
schema, upgrade with the binary, upsert container recs and `all_hours` digests). It is a
zero-infrastructure tool for development, testing, air-gapped operator packages
(`upload_toggle: false` + `oc cp`), and CI.

---

## Use cases

- **(a) Testing:** NISE CSVs → stdout recs, to check a new type or algorithm (no Postgres)
- **(b) Support / debug:** customer operator payload → stdout recs (no Postgres; same as (a))
- **(c) Pedestrian ROS:** daily payloads → `robne` → Postgres this CLI owns (embed migrations, upgrade when the binary is newer). Container recs (**2a**) and digest INSERT ([#463](https://github.com/pgarciaq/ros-ocp-backend/issues/463)) are shipped; completeness still needs other entity upsert (**2c**) and recompute from this CLI’s digests (**2d**). Not “seed a live Helm ROS.”
- **CI / goldens:** pin `--now`, diff JSON (`robne diff`, Phase 3)

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
make robne   # writes bin/robne

# Phase 1: directory of NISE or operator ROS CSVs
./bin/robne recommend --input /path/to/csvs/ --config robne.yaml --format json

# Operator package tarball (restricted network: oc cp the local package)
robne recommend --input ./metrics.tar.gz --plugins container --format table

# Optional decay/staleness clock and rate card (see spec §3 — --now does not slide term windows)
robne recommend --input ./csvs/ --now 2026-08-01T00:00:00Z \
  --rate-card card.json --format json

# Phase 2a first run (empty dedicated DB): --apply-schema bootstraps. Daily cron omits it.
robne recommend --input ./csvs/ --config robne.yaml \
  --output postgres://localhost:5432/robne?sslmode=disable \
  --apply-schema
# Password: PG* env or --pg-url-file. Dedicated DB named for this CLI — not Helm. Spec §5.
```

Flags stay few: `--input`, `--config`, `--plugins`, `--format`, `--rate-card`, `--now`,
`--no-user-config` (same as `ROBNE_NO_USER_CONFIG=1`), and later `--output` /
`--pg-url-file` / `--apply-schema` (bootstrap or upgrade only).

---

## Configuration YAML

Engine knobs are a YAML file. **Sample:** [`cmd/robne/robne.yaml.sample`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/cmd/robne/robne.yaml.sample).

This file is **not** the Settings API and **not** `ROS_*` admin locks. `--plugins` is an
allowlist of recommenders (`container`, later `node`, …), not `internal/plugins` registration.
Unknown keys are errors. Omitted keys use librobne compiled defaults.

**Business hours** and other entity YAML blocks (`node:`, `gpu:`, …) stay errors until
Phase **2b** unlocks that plugin in the same PR that parses its CSV.

How files stack (replace vs merge): [Config overlay](#config-overlay-yaml-and-rate-card).

---

## `--now` (decay / staleness clock)

`--now` does **not** slide term windows.

The processor already uses two clocks; the CLI matches that:

| Mechanism | Anchor |
|-----------|--------|
| **Term windows** (`short` / `medium` / `long`) | Each container’s **latest digest day** — last 1 / 7 / 15 days of *that container’s data* |
| **Decay weighting and staleness** | `EngineConfig.Now` (`--now`, YAML `now`, or max `interval_end`) |

Default `Now` is the last `interval_end` in the files. Then the clocks agree: a tarball
from last week is scored as if the cluster is still current. Pass `--now` only to **pin**
that instant (CI) or to ask “what would the processor say if this data arrived *today*?”
(wall-clock `--now` with old data → heavy decay and likely stale). It does **not** drop
rows.

1. `--now` (RFC3339)
2. `now` in YAML
3. Max `interval_end` (else `interval_start`) in ingested rows

If no timestamp can be parsed, the CLI exits with an error. It does not silently use
`time.Now()`. Spec §3.

## Shell completion

Cobra adds a `completion` subcommand by default:

```bash
./bin/robne completion bash   # also: zsh, fish, powershell
```

It prints a script to stdout. Source it or install it in the usual completion directory.

---

## Input formats

| Format | Source | Phase |
|--------|--------|-------|
| Directory or `.csv` | NISE `--write-monthly --ros-ocp-info`, or unpacked operator CSVs | 1 |
| `.tar.gz` | Operator local package (`upload_toggle: false`) or a NISE tarball | 1 |
| PostgreSQL digest tables | This CLI’s `daily_*_digests` (after digest INSERT) | **2d** ([#474](https://github.com/pgarciaq/ros-ocp-backend/issues/474)) |
| Prometheus JSON | Export from PromQL | Later (not 2a) |

One `--input` path. Detect by filename (`DetermineCSVType`: `ocp_ros_usage` and
`ros-openshift-container-`) **and** header names.

**Cost-only files** (`cm-openshift-pod-usage`, NISE without `--ros-ocp-info`) are
rejected with an error that names the missing ROS columns.

Bad numeric or timestamp rows are skipped (`skipped N unparseable rows` on stderr).
The command continues if any rows remain. If every data row in a ROS file is
unparseable, that is an error.

**NISE vs operator headers** can diverge by column (order is fine; missing names
zero-fill). Operator `csvHeader()` is the contract. NISE should grow toward it
([#465](https://github.com/pgarciaq/ros-ocp-backend/issues/465), not a Phase 1
CLI blocker). Details: spec §4.

**One cluster per `--input`.** NISE `nise report ocp --ocp-cluster-id` is one cluster
per invocation (YAML `generators:` are workloads on that cluster, not extra clusters).
Neither NISE `OCP_ROS_USAGE_COLUMN` nor the operator ROS container header currently
includes `cluster_id`; YAML `cluster_uuid` applies to the whole input. Do not dump two
NISE runs into one directory — the CLI cannot tell them apart. If rows ever contain
more than one `cluster_id`, Phase 1 errors.

**Tarball `./` prefix:** `tar czf archive.tar.gz .` stores `./ocp_ros_usage.csv`.
The CLI strips `./` before matching. The koku listener still does exact-string
match — tracker [#466](https://github.com/pgarciaq/ros-ocp-backend/issues/466);
keep `--transform='s|^\./||'` until koku ships. Spec §8.

---

## Output formats

| Format | Target | Phase |
|--------|--------|-------|
| JSON | stdout, versioned envelope | 1 |
| CSV | stdout, spreadsheet (same snake_case row keys as JSON) | 1 |
| Table | stdout, terminal | 1 |
| PostgreSQL | Product `recommendation_sets` (native denormalized keys; no `workloads` INSERT) | **2a** ([#471](https://github.com/pgarciaq/ros-ocp-backend/issues/471)); other entity tables in **2c** |
| SQLite | — | **Not 2a.** Spec §5 (JSON is the local artifact; PG is product upsert). |

**JSON contract ([#470](https://github.com/pgarciaq/ros-ocp-backend/issues/470)):** an object, not a bare array:

```json
{
  "version": 1,
  "cluster_id": "cluster-a",
  "now": "2026-08-01T02:00:00Z",
  "skipped_rows": 0,
  "recommendations": [ { "namespace": "app", "term": "short", "engine": "cost", "rec_cpu_request_mc": 58, "estimated_savings_cents": null } ]
}
```

Row keys match the CSV header. Missing savings are JSON `null` (not omitted). Pin `--now` in CI. Phase 3 `robne diff` diffs this envelope. Spec §5.

```bash
jq '.recommendations[] | select(.term=="short" and .engine=="cost") | .rec_cpu_request_mc'
```

Phase **2a** PostgreSQL is use case **(c)**: this binary **embeds** product `migrations/`.
`--apply-schema` on bootstrap or upgrade (never `Down()`). Daily upsert at head
needs no extra flag. Ensures `rh_accounts` / `clusters` with `source_id=robne`;
**refuses** if any cluster has another `source_id` (Helm/Sources). Dedicated
database. No CLI UI — inspect with `psql`. Spec §5.

Digest **INSERT** (pgdigest, [#463](https://github.com/pgarciaq/ros-ocp-backend/issues/463))
**shipped:** `--output` upserts `all_hours` container digests (last-write-wins)
before recs. Daily operator payloads are ~one day of CSV; stored digests keep
history for 2d. Digest **SELECT** is 2d. The next `recommend` still uses `--input`
until then.

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
**`by_architecture` stays.** Do not remove it. ROS container CSV has no `node_architecture`
column yet, so the lookup falls through to `default_*` until operator/NISE emit the
column ([#465](https://github.com/pgarciaq/ros-ocp-backend/issues/465)). Mixed POWER/ARM
today: two cluster keys. When the column exists, one cluster + `by_architecture` works.

**GPU rates** on the container plugin path are stored on the resolved card but
`ApplySavingsEstimates` for containers does not use GPU. That is correct for Phase 1
(container CPU/RAM only). Do not imply container savings include GPU. Keep resolving
GPU onto the card for Phase **2b** GPU plugins.

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
  # full block from cmd/robne/robne.yaml.sample (cpu_cost_percentile: 0.60, min_margin: 1.15, …)
idle:
  enabled: true
```

Project `./robne.yaml`:

```yaml
plugins:
  - container
sizing:
  cpu_cost_percentile: 0.80
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
```

Effective: `plugins` = `[container]`; `idle.enabled` still `true`; `sizing` is **exactly**
the project block. User `min_margin` is **gone** unless you repeat it.

!!! warning "Incomplete `sizing:` block"
    Because replace is whole-key, a project file that lists only one percentile is an
    **error** (it would otherwise zero `min_margin`, floors, and the rest). Copy the
    sample’s full `sizing:` block, or omit the `sizing:` key entirely to keep compiled
    defaults. Do not rely on a partial overlay.

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
│ PostgreSQL   │────▶│  (librobne) │──────────────▶│ PostgreSQL   │  2a upsert / 2d read
└──────────────┘     └─────────────┘              └──────────────┘
```

---

## Entity type coverage

All types supported by librobne, enabled via `--plugins` / YAML `plugins`:

- Container (**Phase 1**)
- Node, VM, GPU (MIG + time-slicing), PVC, namespace quota, cluster quota, snapshot (**Phase 2b** files → stdout; **2c** PG)

Node/GPU still need **container ROS CSV** as well as their own files (hooks on container ingest).

---

## Phasing

| Phase | Scope |
|-------|-------|
| **Phase 1** | Container from NISE **or** operator tarball/dir → JSON/CSV/table. YAML, `--plugins`, `--now`, `--rate-card`, `validate`. `librobne/csv` lands here. **Shipped.** |
| **Phase 2a** | Use case (c): embed migrations, `migrate.Up()`, ensure cluster, container upsert ([#471](https://github.com/pgarciaq/ros-ocp-backend/issues/471)). **Shipped.** |
| **pgdigest** | Container digest INSERT into this CLI’s DB ([#463](https://github.com/pgarciaq/ros-ocp-backend/issues/463)). **Shipped.** |
| **Phase 2b** | Other entity CSVs → stdout envelopes ([#472](https://github.com/pgarciaq/ros-ocp-backend/issues/472)) |
| **Phase 2c** | Other entity PG upsert ([#473](https://github.com/pgarciaq/ros-ocp-backend/issues/473)) |
| **Phase 2d** | Recompute from **this CLI’s** digest tables ([#474](https://github.com/pgarciaq/ros-ocp-backend/issues/474)) |
| **Phase 3** | Diff, explain, CI helpers |

---

## Relationship to other planned features

- **Depends on** librobne (ADR-0303, issue #94 — extract complete)
- **Standalone binary**, not a subcommand of ros-ocp-backend (ADR-0305)
- **CSV helpers:** [#463](https://github.com/pgarciaq/ros-ocp-backend/issues/463) csv half rode with Phase 1; **pgdigest** (digest INSERT) **shipped**. Operator must never import those packages or rec-persist SQL.
- **Complements [Local Mode](../planned-features/local-mode.md)** — CLI = offline/batch; operator = real-time on-cluster
- **Complements ros-ocp-backend** — CLI = no infrastructure; backend = full pipeline

---

## Prerequisites

- librobne library extraction (issue #94) — done
- Greenlight of [`docs/plans/robne-cli-spec.md`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/plans/robne-cli-spec.md) — **done** (2026-08-16)

---

## Limitations

- No real-time collection — static snapshots only
- No API server — use ros-ocp-backend or robne-operator for API access
- No Masu, Kafka, Settings API, admin locks, or FX caches
- No cost/savings without a rate card
- No tag enrichment or cloud tag correlation (central-only)
- Business hours and other entity YAML blocks not until **2b**
- No CLI-side fix for NISE missing operator columns ([#465](https://github.com/pgarciaq/ros-ocp-backend/issues/465))
