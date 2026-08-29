# robne CLI specification (for greenlight)

**Status:** **Greenlit** (2026-08-16). Phase 1 ([#469](https://github.com/pgarciaq/ros-ocp-backend/issues/469)), JSON envelope ([#470](https://github.com/pgarciaq/ros-ocp-backend/issues/470)), Phase **2a** ([#471](https://github.com/pgarciaq/ros-ocp-backend/issues/471)), digest INSERT ([#463](https://github.com/pgarciaq/ros-ocp-backend/issues/463)), and digest SELECT ([#474](https://github.com/pgarciaq/ros-ocp-backend/issues/474)) shipped (**container-only** for 463/474). **[#472](https://github.com/pgarciaq/ros-ocp-backend/issues/472) 2b stdout shipped:** namespace + node/GPU + PVC + VM + quota + cluster_quota files → stdout (JSON v2–v8 sibling arrays). Remaining 2b under #472 is none. **[#473](https://github.com/pgarciaq/ros-ocp-backend/issues/473) 2c shipped:** other-entity rec PG upsert (same migrate/ensure as 2a; both GPU tables). **[#481](https://github.com/pgarciaq/ros-ocp-backend/issues/481) shipped:** other-entity digest INSERT (`pgdigest` LWW). **[#482](https://github.com/pgarciaq/ros-ocp-backend/issues/482) shipped:** other-entity Path A SELECT. **[#478](https://github.com/pgarciaq/ros-ocp-backend/issues/478) shipped:** snapshot inventory CSV → stdout (JSON v9 `snapshot_recommendations`). **[#479](https://github.com/pgarciaq/ros-ocp-backend/issues/479) shipped:** YAML `business_hours:` dual digest streams (container + namespace), JSON v10 siblings. Default `--plugins` is **all shipped plugins** unless `--plugins` or YAML `plugins:` is set. Phase 3 `diff` / container `explain` ([#480](https://github.com/pgarciaq/ros-ocp-backend/issues/480)) is **shipped**. Other-entity `explain` ([#490](https://github.com/pgarciaq/ros-ocp-backend/issues/490)) is **shipped**. Binary identity / envelope capability ([#489](https://github.com/pgarciaq/ros-ocp-backend/issues/489)) is **shipped**. **Status:** **Greenlit** (2026-08-16). Phase 1 ([#469](https://github.com/pgarciaq/ros-ocp-backend/issues/469)), JSON envelope ([#470](https://github.com/pgarciaq/ros-ocp-backend/issues/470)), Phase **2a** ([#471](https://github.com/pgarciaq/ros-ocp-backend/issues/471)), digest INSERT ([#463](https://github.com/pgarciaq/ros-ocp-backend/issues/463)), and digest SELECT ([#474](https://github.com/pgarciaq/ros-ocp-backend/issues/474)) shipped (**container-only** for 463/474). **[#472](https://github.com/pgarciaq/ros-ocp-backend/issues/472) 2b stdout shipped:** namespace + node/GPU + PVC + VM + quota + cluster_quota files → stdout (JSON v2–v8 sibling arrays). Remaining 2b under #472 is none. **[#473](https://github.com/pgarciaq/ros-ocp-backend/issues/473) 2c shipped:** other-entity rec PG upsert (same migrate/ensure as 2a; both GPU tables). **[#481](https://github.com/pgarciaq/ros-ocp-backend/issues/481) shipped:** other-entity digest INSERT (`pgdigest` LWW). **[#482](https://github.com/pgarciaq/ros-ocp-backend/issues/482) shipped:** other-entity Path A SELECT. **[#478](https://github.com/pgarciaq/ros-ocp-backend/issues/478) shipped:** snapshot inventory CSV → stdout (JSON v9 `snapshot_recommendations`). **[#479](https://github.com/pgarciaq/ros-ocp-backend/issues/479) shipped:** YAML `business_hours:` dual digest streams (container + namespace), JSON v10 siblings. Default `--plugins` is **all shipped plugins** unless `--plugins` or YAML `plugins:` is set. Phase 3 `diff` / container `explain` ([#480](https://github.com/pgarciaq/ros-ocp-backend/issues/480)) is **shipped**. Other-entity `explain` ([#490](https://github.com/pgarciaq/ros-ocp-backend/issues/490)) is **shipped**. Binary identity / envelope capability ([#489](https://github.com/pgarciaq/ros-ocp-backend/issues/489)) is **shipped**. GPU product API ([#485](https://github.com/pgarciaq/ros-ocp-backend/issues/485)) shipped (no CLI JSON siblings). GPU timeslicing product BH ([#491](https://github.com/pgarciaq/ros-ocp-backend/issues/491)) shipped. VM product BH ([#486](https://github.com/pgarciaq/ros-ocp-backend/issues/486)) shipped (detail thin nest). CLI JSON BH siblings ([#487](https://github.com/pgarciaq/ros-ocp-backend/issues/487)) **shipped** (full CLI DTOs; envelope **11**; product nests stay thin). Overnight Settings PUT ([#488](https://github.com/pgarciaq/ros-ocp-backend/issues/488)) **shipped**. Node product API ([#484](https://github.com/pgarciaq/ros-ocp-backend/issues/484)) shipped.  
**Parent issue:** [#99](https://github.com/pgarciaq/ros-ocp-backend/issues/99)  
**Public MkDocs page:** [docs-site/features/robne-cli.md](../../docs-site/features/robne-cli.md)  
→ [https://pgarciaq.github.io/ros-ocp-backend/features/robne-cli/](https://pgarciaq.github.io/ros-ocp-backend/features/robne-cli/)  
(Old planned-features URL is a bookmark stub.)  
**ADRs:** [ADR-0305](../adr/0305-robne-cli-standalone-binary.md) (standalone binary), [ADR-0303](../adr/0303-library-extraction-librobne.md) (librobne)

This is the review artifact for #99. Child issues live on `pgarciaq/ros-ocp-backend`. Issue **descriptions** must match this spec (rewrite the body if they drift). Do not leave the opposite story in a comment.

**Documentation surfaces (do not create a second public site now):**

| Surface | Audience | In #99? |
|---------|----------|---------|
| This spec (`docs/plans/`) | Implementers / greenlight | Yes — contract. **Not** in MkDocs nav. |
| [features/robne-cli.md](../../docs-site/features/robne-cli.md) | Public GitHub Pages | **Yes — this is the public page.** Overlay rules live here so users do not need the spec. |
| [planned-features/robne-cli.md](../../docs-site/planned-features/robne-cli.md) | Bookmark redirect | Stub only (same pattern as Visual Insights). |
| Standalone `robne-cli` repo user manual | Public, if/when ADR-0305 splits the repo | Later; until then the Features page is enough. |

Do **not** add a second MkDocs entry that duplicates this spec. Keep one public page; keep the spec as the review contract.

---

## 0. Issue tree (recommendation)

**Keep [#99](https://github.com/pgarciaq/ros-ocp-backend/issues/99) as the parent.** Do not grow the #99 body into this spec. The issue stays a short product umbrella; this file is the contract.

Children:

| Child | Repo | Scope |
|-------|------|--------|
| **#99 Phase 1 — recommend** ([#469](https://github.com/pgarciaq/ros-ocp-backend/issues/469)) | `pgarciaq/ros-ocp-backend` | Container path: tarball/dir/CSV in, YAML knobs, `--plugins`, `--now`, `--rate-card`, JSON/CSV/table out. First commits land `librobne/csv` (was [#463](https://github.com/pgarciaq/ros-ocp-backend/issues/463) csv half). Public docs: [features/robne-cli.md](../../docs-site/features/robne-cli.md). **Shipped.** |
| **JSON stdout DTO** ([#470](https://github.com/pgarciaq/ros-ocp-backend/issues/470)) | same | Versioned snake_case envelope; do not tag `ContainerRec`. **Shipped.** Phase 3 `diff` consumes this. |
| **Phase 2a — container PG upsert** ([#471](https://github.com/pgarciaq/ros-ocp-backend/issues/471)) | same | Use case **(c):** embed product migrations, `--apply-schema` on bootstrap/upgrade, ensure cluster `source_id=robne`, refuse foreign `source_id`, native container upsert. **Shipped.** |
| **pgdigest — container digest INSERT** ([#463](https://github.com/pgarciaq/ros-ocp-backend/issues/463)) | same | **(c)** `INSERT` into `daily_container_digests` (`all_hours`). Same CLI-owned DB as 2a. Processor imports the same SQL. **Shipped.** |
| **Phase 2b — other entity CSVs** ([#472](https://github.com/pgarciaq/ros-ocp-backend/issues/472)) | same | **(a)(b)** for node/namespace/GPU/PVC/VM/quota/cluster_quota. Stdout. Independent of 2a. **Shipped** (JSON v2–v8 sibling arrays). Remaining 2b under #472 is none. Snapshot is **not** this issue ([#478](https://github.com/pgarciaq/ros-ocp-backend/issues/478)). Business hours is **not** this issue ([#479](https://github.com/pgarciaq/ros-ocp-backend/issues/479)). |
| **Phase 2c — other entity PG upsert** ([#473](https://github.com/pgarciaq/ros-ocp-backend/issues/473)) | same | **(c)** rec rows for shipped 2b plugins (incl. cluster_quota + both GPU tables). Reuse 2a `migrate.Up()` / ensure cluster. Slim `pgrec.Write*(recs)`; do not wrap GPU query-then-persist. **Shipped.** Not digests ([#481](https://github.com/pgarciaq/ros-ocp-backend/issues/481) / [#482](https://github.com/pgarciaq/ros-ocp-backend/issues/482)). Not snapshot. |
| **Phase 2d — PG digest read** ([#474](https://github.com/pgarciaq/ros-ocp-backend/issues/474)) | same | **(c)** recompute from **this CLI’s** `daily_container_digests`. **Shipped (container-only).** Other entities: [#482](https://github.com/pgarciaq/ros-ocp-backend/issues/482) **shipped.** |
| **Other-entity digest INSERT** ([#481](https://github.com/pgarciaq/ros-ocp-backend/issues/481)) | same | **(c)** extend `pgdigest` for 2b `daily_*` / `gpu_container_digests`. After 473. Not rec upsert. **Shipped.** |
| **Other-entity digest SELECT** ([#482](https://github.com/pgarciaq/ros-ocp-backend/issues/482)) | same | **(c)** Path A for 2b plugins. After 481. Not Helm-stack SELECT. **Shipped.** |
| **Snapshot inventory CSV** ([#478](https://github.com/pgarciaq/ros-ocp-backend/issues/478)) | same | **(a)(b)** `--plugins snapshot` files → stdout (JSON v9). Not 2b. **Shipped.** |
| **Business hours** ([#479](https://github.com/pgarciaq/ros-ocp-backend/issues/479)) | same | YAML `business_hours:` + dual digest streams (container + namespace). Not an entity CSV. Not 2b. **Shipped.** Product node/GPU/timeslicing/VM APIs ([#484](https://github.com/pgarciaq/ros-ocp-backend/issues/484)/[#485](https://github.com/pgarciaq/ros-ocp-backend/issues/485)/[#491](https://github.com/pgarciaq/ros-ocp-backend/issues/491)/[#486](https://github.com/pgarciaq/ros-ocp-backend/issues/486)) ship **thin nested** BH on detail only. CLI JSON node/GPU/timeslicing/VM BH siblings ([#487](https://github.com/pgarciaq/ros-ocp-backend/issues/487)) **shipped** (full DTOs, envelope **11**). YAML `business_hours` + `--plugins node|gpu|vm` is allowed. |
| **Phase 3 — diff / explain / CI** ([#480](https://github.com/pgarciaq/ros-ocp-backend/issues/480)) | same | `robne diff` (two #470 envelopes), container `robne explain` (re-run, not envelope), CI helpers. **Shipped.** Does not need Postgres. Other-entity explain is [#490](https://github.com/pgarciaq/ros-ocp-backend/issues/490) **shipped**. |
| **explain other entities** ([#490](https://github.com/pgarciaq/ros-ocp-backend/issues/490)) | same | Extend `robne explain` for namespace/node/GPU/PVC/VM/quota/cluster_quota/snapshot. Same subcommand. **Shipped.** |
| **binary identity / envelope capability** ([#489](https://github.com/pgarciaq/ros-ocp-backend/issues/489)) | same | `robne version` (no `--version`): git identity + plugin → envelope bump table. JSON `"version"` stays per-run. **Shipped.** |
| **[#465](https://github.com/pgarciaq/ros-ocp-backend/issues/465) NISE ROS column parity** | `nise` (fix); this fork tracks | Add operator columns NISE omits (see §4). Not a CLI blocker. |
| **[#466](https://github.com/pgarciaq/ros-ocp-backend/issues/466) Koku tarball member names** | `koku` (fix); this fork tracks | Normalize `./` prefixes when matching manifest files (see §8). Not a CLI blocker; CLI still self-normalizes. |
| **CLI BH codes 79–82** ([#492](https://github.com/pgarciaq/ros-ocp-backend/issues/492)) | same | `notification_codes` on node/GPU/timeslicing/VM BH JSON siblings only (79–82; all-hours omit the key). Envelope stays **11**. **Shipped.** |

[#463](https://github.com/pgarciaq/ros-ocp-backend/issues/463) is **pgdigest** (digest **INSERT**) — **shipped.** Required for daily incremental (c) (medium/long). Operator ([#138](https://github.com/pgarciaq/ros-ocp-backend/issues/138)) must **never** import `csv`, `pgdigest`, or rec-persist SQL.

**Related, not a #99 child:** ingest parser dedup ([#475](https://github.com/pgarciaq/ros-ocp-backend/issues/475), **shipped** — container `ForEachRow`) and digest-read dedup ([#476](https://github.com/pgarciaq/ros-ocp-backend/issues/476)) are P5 processor hygiene under [#94](https://github.com/pgarciaq/ros-ocp-backend/issues/94). They do not change `bin/robne`.

**Use case → issues:**

| Use case | What it needs | Issues |
|----------|---------------|--------|
| **(a)(b)** container | Files → stdout | [#469](https://github.com/pgarciaq/ros-ocp-backend/issues/469) + [#470](https://github.com/pgarciaq/ros-ocp-backend/issues/470) **shipped** |
| **(a)(b)** other entities | More CSVs → stdout | [#472](https://github.com/pgarciaq/ros-ocp-backend/issues/472) — namespace + node/GPU + PVC + VM + quota + cluster_quota stdout shipped. Snapshot is [#478](https://github.com/pgarciaq/ros-ocp-backend/issues/478). **Can run without 2a.** |
| **(a)(b)** goldens / compare | `diff` on JSON envelopes | [#480](https://github.com/pgarciaq/ros-ocp-backend/issues/480) **shipped** |
| **(a)(b)** explain other entities | Same `explain` subcommand | [#490](https://github.com/pgarciaq/ros-ocp-backend/issues/490) **shipped** |
| **(a)(b)** which binary / envelope capability | `robne version` | [#489](https://github.com/pgarciaq/ros-ocp-backend/issues/489) **shipped** |
| **(c)** schema + keep container recs | Embed migrate, ensure cluster, upsert | [#471](https://github.com/pgarciaq/ros-ocp-backend/issues/471) **shipped** |
| **(c)** keep other entity recs | Same persist, other tables | [#473](https://github.com/pgarciaq/ros-ocp-backend/issues/473) **shipped.** Recs only. Path A SELECT is [#482](https://github.com/pgarciaq/ros-ocp-backend/issues/482) **shipped.** |
| **(c)** keep daily usage (medium/long terms, charts, 2d) | Digest **INSERT** | Containers: [#463](https://github.com/pgarciaq/ros-ocp-backend/issues/463) **shipped.** Other entities: [#481](https://github.com/pgarciaq/ros-ocp-backend/issues/481) **shipped.** Daily operator payloads are ~one day; without stored digests, medium/long windows collapse. Other-entity Path A SELECT is [#482](https://github.com/pgarciaq/ros-ocp-backend/issues/482) **shipped.** |
| **(c)** recompute without re-reading CSV | Digest **SELECT** | Containers: [#474](https://github.com/pgarciaq/ros-ocp-backend/issues/474) **shipped.** Other entities: [#482](https://github.com/pgarciaq/ros-ocp-backend/issues/482) **shipped.** Not “read a Helm ROS DB.” |

**Never in the CLI (any phase):** Settings API clone, plugin `init()` registry, Masu HTTP, admin env locks, Kafka, FX / `user_currency` caches ([#462](https://github.com/pgarciaq/ros-ocp-backend/issues/462) is independent).

---

## 1. Product shape

`robne` is a **standalone static binary** (ADR-0305), not a ros-ocp-backend subcommand. It imports librobne in-process and computes the same recommendations as the processor.

**Three use cases** (this is the product, not “skip Kafka on a running stack”):

| | Who | Postgres? |
|--|-----|-----------|
| **(a) Testing** | Engineer: NISE CSVs → recs, to check a new type or algorithm change | **No.** Stdout JSON/CSV/table. Phase 1. |
| **(b) Support / debug** | Engineer: customer operator payload → recs, to see what the engine says | **No.** Same as (a). Phase 1. |
| **(c) Pedestrian ROS** | Operator: take payloads every day, run `robne`, keep results | **Yes.** `robne` **owns** that database: create schema, upgrade schema when the binary is newer, upsert recs. No git clone, no `rosocp db migrate`, no Helm. |

Container **(a)** and **(b)** (files → stdout) are shipped. Namespace, node, GPU, PVC, VM, quota, and cluster_quota files are a **2b** ([#472](https://github.com/pgarciaq/ros-ocp-backend/issues/472)) slice (**stdout shipped**). **(c)** is why PostgreSQL exists (2a, then pgdigest, 2c, 2d). The old “refuse empty DB / never migrate / already-running stack only” lock **does not serve (c)** and is **withdrawn**.

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
Samples: [`cmd/robne/robne.yaml.sample`](../../cmd/robne/robne.yaml.sample), [`cmd/robne/rate-card.json.sample`](../../cmd/robne/rate-card.json.sample).

Phase 1 (container files → stdout) is **shipped**. Phase **2b** files → stdout is **shipped**. Phase **2c** other-entity rec upsert is **shipped**. Other-entity digest INSERT ([#481](https://github.com/pgarciaq/ros-ocp-backend/issues/481)) is **shipped**. Other-entity Path A SELECT ([#482](https://github.com/pgarciaq/ros-ocp-backend/issues/482)) is **shipped**. Snapshot files → stdout ([#478](https://github.com/pgarciaq/ros-ocp-backend/issues/478)) is **shipped**. Phase 3 `diff` / container `explain` ([#480](https://github.com/pgarciaq/ros-ocp-backend/issues/480)) is **shipped**. Other-entity `explain` ([#490](https://github.com/pgarciaq/ros-ocp-backend/issues/490)) is **shipped**. Binary identity / envelope capability ([#489](https://github.com/pgarciaq/ros-ocp-backend/issues/489)) is **shipped**. Business hours ([#479](https://github.com/pgarciaq/ros-ocp-backend/issues/479)) is **shipped** (container + namespace). Product node API ([#484](https://github.com/pgarciaq/ros-ocp-backend/issues/484)) is **shipped**. CLI JSON node/GPU/timeslicing/VM BH siblings ([#487](https://github.com/pgarciaq/ros-ocp-backend/issues/487)) are **shipped** (full CLI DTOs; envelope **11**; product nests stay thin). Overnight Settings PUT ([#488](https://github.com/pgarciaq/ros-ocp-backend/issues/488)) is **shipped**.

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
idle:
  enabled: true
```

Project `./robne.yaml`:

```yaml
plugins:
  - container
sizing:
  cpu_cost_percentile: 0.80
  # …repeat every sizing field (see cmd/robne/robne.yaml.sample)
```

Effective: `plugins` = `[container]`; `idle` still from the user file; `sizing` is **exactly** the project block (`min_margin` from the user file is **gone** unless repeated).

A later file that lists `sizing:` with only `cpu_cost_percentile` is an **error**. Omit `sizing:` entirely to keep compiled (or earlier-file) defaults. Do not deep-merge.

**Rate-card example — merge by cluster id, replace whole cluster:** see §6. A project file that only lists `cluster-power-prod` leaves the user’s `cluster-arm-gpu` in place, and **replaces** `cluster-power-prod` entirely (including nested `by_architecture`).

```yaml
# robne.yaml — Phase 1 schema
org_id: "1234567"                 # required for PostgreSQL write (Phase 2a); optional in Phase 1
cluster_uuid: "local-cluster"     # any string for stdout (a)(b). When --output postgres:// it MUST be an RFC 4122 UUID (product column is UUID). Hard error otherwise — do not invent a UUID from this string.

# Clock for decay / staleness only. CLI flag --now overrides this.
# Term windows stay on the latest digest day (same as the processor).
# If both omitted: max interval_end (else interval_start) across ingested rows.
now: null                         # RFC3339, or omit

# Omit plugins: for all shipped plugins. A present empty plugins: is an error.
# Flag --plugins or this key (non-empty) is an explicit list: missing input errors.
# plugins:
#   - container
#   - namespace                   # NISE/operator namespace CSVs → stdout; JSON version 2 sibling array
#   - node                        # from container ROS rows; JSON version 3 sibling array
#   - gpu                         # from container ROS rows; JSON version 4 sibling arrays (MIG + timeslicing)
#   - pvc                         # storage CSV → stdout; JSON version 5 sibling array; YAML pvc: stays reserved
#   - vm                          # VM usage CSV → stdout; JSON version 6 sibling array; YAML vm: stays reserved; optional pvc/gpu companions degrade
#   - quota
#   - cluster_quota
#   - snapshot                    # inventory CSV → stdout; JSON version 9; YAML snapshot: stays reserved

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

# Cluster-wide business hours (not Settings JSON nesting; not an entity CSV).
# Omit the key for compiled default off. Overlay replaces the whole key.
# Overnight (end_time < start_time) is allowed. Equal start/end is invalid.
business_hours:
  enabled: true
  timezone: Europe/Madrid
  days: [monday, tuesday, wednesday, thursday, friday]
  start_time: "09:00"
  end_time: "20:00"
  # off_hours_weight: 0          # omit → 0 (same as Settings PUT)

# Reserved until a PR parses a dedicated settings schema for that block.
# Plugins are unlocked (omit plugins: for all shipped, or list them explicitly).
# A present block with enabled: true is an error until then.
# node: { ... }
# gpu: { ... }
# pvc: { ... }
# vm: { ... }
# quota: { ... }
# cluster_quota: { ... }
# snapshot: { ... }
```

**Validation:** unknown keys are errors (no silent ignore). Percentiles must be in `(0, 1]`. `plugins` must be a known entity name. Empty `terms` is an error (do not silently use defaults if the key is present and empty). Empty `plugins:` (key present) is an error; omit the key to enable all shipped plugins.

**`--plugins`:** comma-separated allowlist (`container`, `namespace`, `node`, `gpu`, `pvc`, `vm`, `quota`, `cluster_quota`, `snapshot`). Same idea as “enable recommenders,” **not** `internal/plugins` `init()` registration. **Default (no flag, no YAML `plugins:`)** is **all shipped plugins**. Implicit default **skips** a plugin when its dedicated CSV is missing (files) or its own digest table SELECT is empty (Path A). GPU additionally requires at least one container ROS row with `accelerator_model_name`. Snapshot additionally requires a classified snapshot **filename** (header-only inventory still counts; empty rows emit an empty sibling). An **explicit** list (`--plugins` or YAML `plugins:`) **errors** when that plugin’s input is missing. Path A plus **explicit** `snapshot` is a **hard error** (no snapshot digest table; implicit default drops snapshot). `--format csv` / `table` stay one entity per stream; a container ROS file also enables `node`, so pin `--plugins container` for those formats. Node and GPU still read **container ROS CSV** (no new file family). PVC reads a **storage** file family (`ros-openshift-storage-*`, `ocp_storage_usage`, `cm-openshift-storage-usage` classified **before** blanket `cm-openshift-*` → cost-only). VM reads **usage** (`ros-openshift-vm-usage-*`, `ocp_ros_vm_usage` classified **before** `ocp_ros_usage`) plus optional companions (`ros-openshift-vm-pvc-*` / `ocp_ros_vm_pvc`, `ros-openshift-vm-gpu-device-*` / `ocp_ros_vm_gpu_device`; missing/malformed companions degrade). Quota reads the **same namespace ROS CSV** as `--plugins namespace` (optional `quota_name` / `resource_quota_name`; named-quota `*_namespace_sum` columns are ResourceQuota hard). Cluster quota reads a **dedicated CRQ** file family (`ros-openshift-cluster-quota-*`, `ocp_ros_cluster_quota` classified **before** namespace). Snapshot reads **inventory** (`ros-openshift-snapshot-*`, `ocp_snapshot_inventory`, `cm-openshift-snapshot-inventory` classified **before** blanket `cm-openshift-*` → cost-only). Collapse hourly inventory to the latest row per `(namespace, snapshot_name)` by `interval_end`, then `interval_start`, then `creation_timestamp` before `ClassifySnapshotInventory`. YAML `node:` / `gpu:` / `pvc:` / `vm:` / `quota:` / `cluster_quota:` / `snapshot:` stay reserved (compiled defaults; quota/CRQ headroom 11000 BP; snapshot uses `DefaultSnapshotSettings` + CLI `now`, not product `InventoryFreshHours` or DB reconcile). Do **not** treat GPU timeslicing as a model for quota: namespace ResourceQuota (`quota`) and ClusterResourceQuota (`cluster_quota`) are separate plugins. Do **not** json-tag `snapshot.SnapshotRec`. Unmatched filenames are `KindUnknown` (not `KindOther`).

---

## 3. `--now` (decay / staleness clock)

**Keep the flag.** Do not remove it. It is the decay/staleness clock, not the term-window anchor.

Librobne already has **two clocks** (same as the processor). Phase 1 must match that, not invent a CLI-only “slide the windows with `--now`” behavior.

| Mechanism | Anchor | Role of `EngineConfig.Now` |
|-----------|--------|----------------------------|
| **Term `window_days`** | Each container’s **latest digest day** (`WindowBounds(digests, latest.BucketDate, windowDays)`) | **None.** Short = last 1 calendar day of *that container’s data*, medium = last 7 days of that data, … — ending at the latest bucket, not at `Now`. |
| **Decay `decay_half_life_hours`** | Age = `Now − row.BucketDate` | Hours closer to `Now` weigh more. |
| **Staleness `staleness_hours`** | `Now − ClusterLastReported` (CLI sets last-reported to max `interval_end`) | If the cluster is older than the threshold relative to `Now`, the rec is stale. |
| **Idle `min_observation_days`** | Count of digest **rows in the idle window** (that window also ends at latest digest day) | **None.** Not measured back from `Now`. |

Default `Now` is **max `interval_end`** in the files. Then all three practical clocks line up: windows end at the last data day, decay treats that day as age 0, staleness does not fire. That is the “score last week’s tarball as if the cluster is current” case.

Pass `--now` (or YAML `now`) only when you want a **different freshness clock**:

| You pass | Windows | Decay / staleness |
|----------|---------|-------------------|
| *(omit — default)* | Last N days of data | Data is “fresh” as of the last row |
| `--now` = last `interval_end` | Same | Same (explicit pin for CI) |
| `--now` = wall clock, data is a week old | Still last N days of *data* | Rows look old; likely **stale**. This is what the processor would say if that tarball arrived today. |

Removing `--now` would leave only the default (max `interval_end`) **or** a silent `time.Now()`. Silent wall clock is forbidden. Default-only would drop the “pin the clock” / “score as of today” cases. The flag is cheap; the weirdness is only if docs claim it slides windows. They must not.

**`--now` is not a row filter.** It does not drop CSV lines. Rows outside a term’s window (relative to latest digest day) are unused **for that term**. All rows are still ingested.

Resolution order:

1. `--now` (RFC3339)
2. `now` in YAML (project or user file, after overlay)
3. **Max timestamp in ingested rows** (`interval_end`, else `interval_start`)

If no timestamp can be parsed, exit non-zero with a clear error — do not fall back to wall clock.

Do **not** change `WindowBounds` to use `Now` in the CLI only. That would diverge from the processor. Sliding windows off `Now` is an engine change (processor + CLI together), not a Phase 1 follow-up.

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

Bad numeric or timestamp **data rows** are skipped (stderr: `skipped N unparseable rows`). The CLI continues if any rows remain. If every data row in a ROS file is unparseable, that is an error. Structural CSV errors (broken quoting) still fail immediately.

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

### One cluster per `--input`

Phase 1 errors if parsed rows contain more than one distinct `cluster_id` / `cluster_uuid`.

**NISE does not put multiple clusters in one CSV.** `nise report ocp --ocp-cluster-id ID` is one cluster. YAML `generators:` are nodes / namespaces / pods of that cluster, not extra cluster ids. `OCP_ROS_USAGE_COLUMN` has **no `cluster_id` column**; the id is in the filename (`{Month}-{Year}-{cluster_id}-ocp_ros_usage.csv`).

**Operator ROS container CSV also has no `cluster_id` column** (`rosContainerRow.csvHeader()`). Cluster identity lives in the package / manifest.

So for both real generators today, `UniqueClusterIDs` is empty and YAML `cluster_uuid` is the id for the whole `--input`. **Do not concatenate two NISE runs (two `--ocp-cluster-id`s) into one directory** — the CLI cannot tell them apart and will score them as one cluster. If a `cluster_id` column lands later, mixed files in one `--input` become a hard error.

---

## 5. Output stores (PostgreSQL in 2a; not SQLite)

**Phase 1 does not write a database.** `--format json|csv|table` is stdout (or a file if we add `--output PATH` as a filename, not a DSN). No `postgres://` or `sqlite://` in Phase 1.

### JSON stdout (frozen — [#470](https://github.com/pgarciaq/ros-ocp-backend/issues/470))

`--format json` is a **versioned envelope**, not a bare array and not PascalCase `ContainerRec` fields. CSV headers and JSON row keys are the same snake_case set. Phase 3 `robne diff` consumes this envelope. Envelope `"version"` is **per-run** (max plugin sibling this invocation; YAML `business_hours.enabled` bumps to **10**). It does **not** identify the binary. `robne version` ([#489](https://github.com/pgarciaq/ros-ocp-backend/issues/489) **shipped**) prints binary identity (`make robne` injects `git describe --always --dirty`; `go test` / `go build` stay `devel`) plus the plugin → envelope bump table from the same consts `envelopeVersion()` uses. `business_hours` in that table is the YAML bump, not a `--plugins` name. Default `--plugins` (all shipped) is already v9 (v10 with BH). Do **not** add `--version`.

```json
{
  "version": 1,
  "cluster_id": "cluster-a",
  "now": "2026-08-01T02:00:00Z",
  "skipped_rows": 0,
  "recommendations": [
    {
      "namespace": "app",
      "workload": "api",
      "workload_type": "deployment",
      "container_name": "api",
      "term": "short",
      "engine": "cost",
      "rec_cpu_request_mc": 58,
      "rec_cpu_limit_mc": 61,
      "rec_mem_request_kib": 58880,
      "rec_mem_limit_kib": 61824,
      "current_cpu_request_mc": 200,
      "current_mem_request_kib": 102400,
      "estimated_savings_cents": null,
      "stale": false,
      "idle_state": "active",
      "category": "oversized"
    }
  ]
}
```

| Rule | Detail |
|------|--------|
| Envelope | `version` (int: `1` container-only, `2` namespace, `3` node, `4` gpu, `5` pvc, `6` vm, `7` quota, `8` cluster_quota, `9` snapshot — **max** of enabled plugins), `cluster_id`, `now` (RFC3339 UTC), `skipped_rows`, `recommendations` (always an array, never `null`; **container rows only**) |
| Rows | CLI-owned DTO in `cmd/robne` (`containerOut`). Same keys as CSV. No explanation factors, trend slopes, or `float32` confidence. |
| Savings | `estimated_savings_cents` is JSON `null` when unset (not omitted, not `0`) |
| Engine type | Do **not** add `json` tags on `librobne/types.ContainerRec` |
| CI | Pin `--now`. Numeric golden: `cmd/robne/testdata/golden_short_cost.json` (`rec_cpu_request_mc` / `rec_mem_request_kib` only) |

```bash
jq '.recommendations[] | select(.term=="short" and .engine=="cost") | .rec_cpu_request_mc'
```

The `58` / `58880` request values are the numeric golden (`cmd/robne/testdata/golden_short_cost.json`). Other fields in the example match that same one-day fixture but are not frozen.

**`robne diff` / `robne explain` (frozen — [#480](https://github.com/pgarciaq/ros-ocp-backend/issues/480)).** `recommend` JSON is the **list** (what to apply). It does **not** carry explanation factors, trend slopes, or `float32` confidence — same split as the product API list vs `?include=explanation` on detail. CSV headers stay that subset. Phase 3 `diff` consumes this envelope; putting `Expl` on `containerOut` would fail goldens on every rationale-only algorithm tweak.

```text
robne diff LEFT.json RIGHT.json
```

Two #470 envelopes on disk. No Postgres, no re-run. Exit **0** identical, **1** recs or metadata differ (`cluster_id`, `now`, `skipped_rows`), **2** unreadable JSON / version mismatch / duplicate row keys. Version mismatch is a hard error (do not silently ignore sibling arrays). Empty sibling vs missing key is a delta. Diff every `*_recommendations` array present in either file, keyed by persist identity (not array index). Stdout is a short text report. Keep `golden_short_cost.json`; CI also checks `cmd/robne/testdata/golden_envelope_v1.json` (one short/cost container row). Do not `diff` explain JSON (floats).

```text
robne explain --input <dir|file.csv|file.tar.gz|postgres://> \
  [--plugins ENTITY] \
  …identity flags for that entity… \
  [--schedule all_hours|business_hours] \
  [--config] [--now] [--rate-card] [--no-user-config]
```

Re-runs the same engine path as `recommend` (files or Path A). Does **not** read a recommend envelope (hard error). Does **not** SELECT stored `expl_*`. `--output` / `--apply-schema` / `--format` are not flags. Missing match is a hard error. Stdout is one snake_case JSON object (CLI-owned `*ExplainOut`: identity + same rec numbers as the matching `*Out` + expl fields). CLI names stay `*_bp` / `*_mc` / `*_kib` (not API millicores). Omit `float32` confidence. Do not json-tag engine `Expl` structs. Do not golden full explain JSON (floats). PVC also copies `usage_ratio` and `growth_bytes_per_day` from the rec (not only `PVCExplanationFactors`).

**One entity type per run ([#490](https://github.com/pgarciaq/ros-ocp-backend/issues/490) shipped).** `--plugins` is **exactly one** name. Omit it for **container**. Two or more names is a hard error. YAML `plugins:` does **not** select the type (unlike `recommend`). No `--entity` flag. Inapplicable identity flags are hard errors (validated in `runExplain`, not cobra `MarkFlagRequired`). `--schedule` defaults to `all_hours`; `business_hours` requires YAML `business_hours.enabled` and is **container, namespace, node, gpu, and vm** (timeslicing uses the gpu plugin). PVC/quota/cluster_quota/snapshot hard-error. Snapshot + Path A stays the existing hard error.

| `--plugins` | Required | Optional |
|---|---|---|
| `container` (default) | `--namespace` `--workload` `--container` `--term` `--engine` | `--workload-type` `--schedule` |
| `namespace` | `--namespace` `--term` `--engine` | `--schedule` |
| `node` | `--node` `--term` `--engine` | — |
| `gpu` MIG | `--namespace` `--workload` `--container` `--term` | `--gpu-model` if not unique |
| `gpu` timeslicing | `--node` `--gpu-model` `--term` | — |
| `pvc` | `--namespace` `--pvc` `--term` | — |
| `vm` | `--namespace` `--vm-name` `--term` `--engine` | — |
| `quota` | `--namespace` `--quota-name` | `--recommendation-type` if not unique |
| `cluster_quota` | `--cluster-quota-name` | `--recommendation-type` if not unique |
| `snapshot` | `--namespace` `--snapshot-name` | — |

GPU infers kind from flags (no `--kind`): `--container` → MIG; `--node` without `--container` → timeslicing; both set → error. VM overlay terms are `short_term` / `medium_term` / `long_term` (`VMTermWindowsFromConfig`), not `short`. PVC/node/container/namespace use `short`. Ambiguous container match: pass `--workload-type`. Ambiguous GPU MIG: pass `--gpu-model`. Ambiguous quota/CRQ: pass `--recommendation-type`.

**Namespace sibling ([#472](https://github.com/pgarciaq/ros-ocp-backend/issues/472) slice, [ADR-0336](../adr/0336-robne-json-entity-sibling-arrays.md)):** when `--plugins` includes `namespace`, `version` is at least **2** and the envelope adds `namespace_recommendations` (always an array, never `null`). `recommendations` stays **container-only**. Container-only runs stay `version` **1** with no sibling key (`--plugins container`). `--format csv` / `table` are one entity per stream; mixing any of container/namespace/node/gpu/pvc/vm/quota/cluster_quota/snapshot requires JSON. Do **not** add `json` tags on `namespace.NamespaceRec`. Namespace recs reuse container YAML `terms` / `sizing` (no reserved `namespace:` block). `--output postgres://` upserts recs for shipped 2b plugins ([#473](https://github.com/pgarciaq/ros-ocp-backend/issues/473)). `--input postgres://` SELECTs stored other-entity days for listed plugins ([#482](https://github.com/pgarciaq/ros-ocp-backend/issues/482)).

**Node / GPU siblings (same #472 slice):** `--plugins node` bumps `version` to at least **3** and adds `node_recommendations`. `--plugins gpu` bumps `version` to **4** and adds `gpu_recommendations` (per-container MIG) **and** `gpu_timeslicing_recommendations` (per node×model×term; JSON-only). Both read container ROS rows (optional allocatable/DCGM columns). Missing allocatable uses 0.93× capacity; missing GPU model produces no GPU recs. YAML `node:` / `gpu:` stay reserved. Do **not** add `json` tags on `node.Rec` or `gpu.GPURec`.

**PVC sibling (same #472 slice):** `--plugins pvc` bumps `version` to at least **5** and adds `pvc_recommendations` (always an array, never `null`). One rec per PVC×term (no `engine` column). Missing optional columns (`vm_name`, usage, request) do not fail; empty PVC name rows are dropped. Explicit `--plugins pvc` with no storage CSV is an error; implicit default skips a missing storage file. Do not call `ApplyPVCSavings` (unset savings stay JSON `null`). YAML `pvc:` stays reserved. Do **not** add `json` tags on `pvc.PVCRec`.

**VM sibling (same #472 slice):** `--plugins vm` bumps `version` to at least **6** and adds `vm_recommendations` (always an array, never `null`). One rec per VM×term×engine (`cost`, `performance`). Overlay container `terms` apply via `VMTermWindowsFromConfig` until YAML `vm:` unlocks (CLI short term is 1 day / min 1, not product VM 7/15/30). Missing companion files degrade; malformed companions in a dir/tarball are skipped; explicit `--plugins vm` with no **usage** CSV is an error (implicit default skips). Empty `vm_name` / `namespace` rows are skipped. Timeslicing is **on the VM row** (`recommended_time_slice_count`), not a second sibling (GPU’s node×model timeslicing array is a different grain). Do not call product VM savings (unset `estimated_savings_cents` stays JSON `null`). YAML `vm:` stays reserved. Do **not** add CLI `json` tags on `vm.VMRecommendation` (existing GPU field tags in librobne stay). Cluster quota is **8**. Snapshot is **9** ([#478](https://github.com/pgarciaq/ros-ocp-backend/issues/478), shipped).

**Quota sibling (same #472 slice):** `--plugins quota` bumps `version` to at least **7** and adds `quota_recommendations` (always an array, never `null`). No new Kind — same namespace ROS CSV as `--plugins namespace`. Rows without `quota_name` (alias `resource_quota_name`) are skipped as snapshots (empty sibling, not an error). Explicit `--plugins quota` with no namespace CSV is an error; implicit default skips. One rec per namespace×quota_name (no term/engine). Named-quota operator `*_namespace_sum` columns are ResourceQuota hard. If container ROS is in the load, compute in-memory container recs (term `medium`, engine `cost`) for `ContainerQuotaAggregate` even when `--plugins container` is off; missing container ROS yields zero aggregates. Compiled defaults: 10% headroom = 11000 BP, high 9000, medium 7000, empty currency (`DefaultQuotaRecConfig`). Do not call `ApplyQuotaSavings` (unset savings stay JSON `null`). YAML `quota:` stays reserved. Do **not** add `json` tags on `quota.QuotaRec`. `--output postgres://` upserts quota recs ([#473](https://github.com/pgarciaq/ros-ocp-backend/issues/473)). `--input postgres://` SELECTs `daily_namespace_quota_digests` (and nested container days for aggregates) ([#482](https://github.com/pgarciaq/ros-ocp-backend/issues/482)).

**Cluster quota sibling (same #472 slice):** `--plugins cluster_quota` bumps `version` to at least **8** and adds `cluster_quota_recommendations` (always an array, never `null`). Dedicated Kind (`KindClusterQuota`): classify `ros-openshift-cluster-quota-` and `ocp_ros_cluster_quota` **before** namespace. Explicit `--plugins cluster_quota` with no CRQ CSV is an error; implicit default skips a missing CRQ file. Days with no hard limits emit an empty sibling (not an error). Empty `namespaces` on the snapshot sums **all** in-memory namespace quota recs (product `QueryNamespaceQuotaAggregateForNamespaces`); a non-empty comma-separated list filters by membership. Two ResourceQuotas in one namespace both count. Missing NS/container ROS still emits CRQ recs from used vs hard (zero nsAggs). Nested chain: if container ROS is present, compute in-memory container recs (`medium`/`cost`); if namespace ROS is present, compute quota recs; sum into `NamespaceQuotaClusterAggregate` per CRQ; then `RecommendClusterQuotas`. Attach quota before cluster_quota so `--plugins quota` can reuse `QuotaRecs`. Do **not** emit `quota_recommendations` unless `--plugins quota` is on. DTO (`clusterQuotaOut`) matches `quotaOut` grain: hard + recommended CPU millicores, memory/storage **bytes**, pods; omit used, utilization, notification codes, capacity freed, object_count, Expl. Do not convert CRQ memory to KiB. Do not call `ApplyClusterQuotaSavings` (unset savings stay JSON `null`). YAML `cluster_quota:` stays reserved (`enabled: true` is still a Phase-1 error). Do **not** add `json` tags on `quota.ClusterQuotaRec`. `--output postgres://` upserts cluster_quota recs ([#473](https://github.com/pgarciaq/ros-ocp-backend/issues/473)). `--input postgres://` SELECTs `daily_cluster_quota_digests` and reconstructs the nested quota/container chain from stored days ([#482](https://github.com/pgarciaq/ros-ocp-backend/issues/482)). Snapshot is [#478](https://github.com/pgarciaq/ros-ocp-backend/issues/478) (shipped; not 2b).

**Snapshot sibling ([#478](https://github.com/pgarciaq/ros-ocp-backend/issues/478), [ADR-0336](../adr/0336-robne-json-entity-sibling-arrays.md)):** `--plugins snapshot` (or implicit default when a snapshot file is classified) bumps `version` to **9** and adds `snapshot_recommendations` (always an array, never `null`; empty `notification_codes` is `[]` not `null`). Parse in `librobne/csv` (`KindSnapshot`); do **not** wrap `internal/ingestion.ParseSnapshotRows`. Collapse hourly rows before classify. Classify with `snapshot.DefaultSnapshotSettings()` and CLI `now` (`--now` / YAML `now` / max interval — never wall clock). Do not apply product `InventoryFreshHours` or DB reconcile. Rate card unused; cost is compiled `CostPerGiBMonth` (0.05) on `SnapshotRec.EstimatedCostCents`. Explicit missing snapshot CSV is an error; empty inventory after parse is an empty sibling. Files-only for snapshot: `--output postgres://` must **not** write `snapshot_recommendation_sets` or `snapshot_inventory`. Path A does not SELECT snapshot; explicit `--plugins snapshot` with `--input postgres://` is a hard error. YAML `snapshot:` stays reserved. Do **not** add `json` tags on `snapshot.SnapshotRec`. DTO (`snapshotOut`): namespace, snapshot_name, source_pvc_name, volume_snapshot_class, storage_class, creation_timestamp (RFC3339), restore_size_bytes, age_days, source_pvc_exists, restored_pvc_count, managed_by, recommendation_type, estimated_cost_cents, notification_codes.

```json
{
  "version": 2,
  "cluster_id": "cluster-a",
  "now": "2026-08-01T02:00:00Z",
  "skipped_rows": 0,
  "recommendations": [],
  "namespace_recommendations": [
    { "namespace": "app", "term": "short", "engine": "cost", "rec_cpu_request_mc": 100 }
  ]
}
```

### Phase 2 split (do not implement as one issue)

| Slice | Issue | What |
|-------|-------|------|
| **2a** | [#471](https://github.com/pgarciaq/ros-ocp-backend/issues/471) | **(c)** embed migrate + ensure cluster + container upsert. **Shipped.** |
| **2b** | [#472](https://github.com/pgarciaq/ros-ocp-backend/issues/472) | **(a)(b)** other entity CSVs → stdout. **Shipped.** Snapshot is [#478](https://github.com/pgarciaq/ros-ocp-backend/issues/478) (**shipped**). Business hours is [#479](https://github.com/pgarciaq/ros-ocp-backend/issues/479) (**shipped**). |
| **2c** | [#473](https://github.com/pgarciaq/ros-ocp-backend/issues/473) | **(c)** other entity **rec** upsert (same migrate/ensure as 2a). **Shipped.** Not digests. |
| **2d** | [#474](https://github.com/pgarciaq/ros-ocp-backend/issues/474) | **(c)** `SELECT` **container** digests this CLI wrote. **Shipped.** |
| **2b digest INSERT** | [#481](https://github.com/pgarciaq/ros-ocp-backend/issues/481) | **(c)** other-entity digest INSERT. After 473. **Shipped.** |
| **2b digest SELECT** | [#482](https://github.com/pgarciaq/ros-ocp-backend/issues/482) | **(c)** Path A for 2b plugins. After 481. **Shipped.** |

**2d shipped (containers):** files plus `--output postgres://` INSERT today’s `all_hours` **container** digests, commit, then SELECT `[end − MaxWindowDays(terms), end]` (`end` is `--now` if set, else `max(bucket_date)` — never wall clock, never `ROS_MAX_LOOKBACK_DAYS`), then `RecommendWorkloads` and rec upsert. `--input postgres://` (or `postgresql://`) is recompute: same SELECT, stdout, optional rec upsert if `--output` is the **same** database; `--apply-schema` is a hard error. `validate` stays files-only. Empty SELECT is a CLI error. Processor timeout/cap stay in the wrapper. `#476` can later point `QueryContainerDigests*` at the same `pgdigest` Read.

**481 shipped (other-entity INSERT):** files plus `--output postgres://` also INSERT today’s other-entity daily rows via slim LWW `pgdigest.Write*` (namespace usage, node, GPU container, PVC, VM + `vm_gpu_device_digests`, namespace quota, cluster quota). Same CLI-owned DB as 463. Persist whenever the load already built those days (not plugin-gated); rec upsert stays plugin-gated ([#473](https://github.com/pgarciaq/ros-ocp-backend/issues/473)). Quota/CRQ persist every `report_date`, not `Latest*Snapshots`. `CREATE TABLE … PARTITION OF` only for RANGE parents (`daily_namespace_digests`, `daily_node_digests`, `gpu_container_digests`, `daily_pvc_digests`); heap tables (`daily_namespace_quota_digests`, `daily_cluster_quota_digests`, `daily_vm_digests`) are not partitioned. Do not copy ingest `GREATEST`/`LEAST` (processor stream merge stays in `internal/ingestion`). Path B still computes other-entity recs from files.

**482 shipped (other-entity Path A SELECT):** `--input postgres://` SELECTs stored other-entity days via slim `pgdigest.Read*` (not product `Query*`). Window is `[end − MaxWindowDays(terms), end]` (`end` is `--now` if set, else `MaxAnyDigestDate` across this identity’s digest tables — never wall clock). Quota/CRQ recs reduce to latest day in that window (`Latest*FromDaily`; CRQ still requires hard limits). Nested chain matches files: quota/CRQ SELECT supporting container and quota days even when those plugins are off, then emit siblings only for listed plugins. Empty own-table SELECT for an **explicit** plugin is a CLI error; implicit default **skips** empty tables. Empty nested inputs yield zero aggregates, not an error. `--apply-schema` stays a hard error. Same CLI-owned DB; `--output` the same URL upserts recs only (no digest INSERT on Path A). Not Helm-stack SELECT. Snapshot has **no** Path A SELECT ([#478](https://github.com/pgarciaq/ros-ocp-backend/issues/478): explicit `--plugins snapshot` is a hard error; implicit default drops snapshot). Not business hours. YAML `node:` / `gpu:` / `pvc:` / `vm:` / `quota:` / `cluster_quota:` / `snapshot:` stay reserved.

`--output postgres://…` is **2a** (scheme must be `postgres` or `postgresql`; refuse a path that only *looks* like a DSN). Also read `PGHOST` / `PGPORT` / `PGUSER` / `PGPASSWORD` / `PGDATABASE` / `PGSSLMODE` and/or `--pg-url-file PATH` so the password is not required on argv.

**`--apply-schema`:** required when `migrate.Up()` would apply **at least one** version (empty DB → bootstrap, or DB version **<** binary → upgrade). Daily cron at head does **not** pass it (`Up()` is a no-op). Missing flag in those situations is an error that names both versions (or “empty”) and tells the user to pass `--apply-schema` on a **dedicated** database. No `--force` / `--i-own-this-database` on every recommend.

**Who `--output postgres://` is for:** use case **(c)** only — a pedestrian database **this CLI owns**. Not (a)/(b). Not “the Helm chart already migrated production.” Do not point `--output` at a live cost-onprem Postgres the operator also migrates.

**How users get the schema:** `go:embed` of `migrations/` inside `bin/robne`. The binary **is** the delivery. Not git clone. Not a second `CREATE TABLE` dump.

**2a success / no CLI UI:** rows in the CLI-owned database. Inspect with `psql` (or keep using `--format json` on stdout). There is **no** robne CLI web UI in this epic. A UI for **robne-operator** is [#138](https://github.com/pgarciaq/ros-ocp-backend/issues/138) (separate issue).

### Persist: extract once — do not copy SQL, do not move Echo plugins into librobne

Copying `WriteRecommendations` into `cmd/robne` **is** duplication and **will** drift. **Extracting** it is the opposite: **one** `INSERT … ON CONFLICT`, two importers (processor + CLI).

Extract the native rec upsert, `RefreshOrgMetadata`, and `MarkUnreportedContainersStale` (processor marks ghosts `stale=true`; do **not** invent a CLI-only `DELETE`) into an optional I/O package — same idea as `librobne/csv`. Operator ([#138](https://github.com/pgarciaq/ros-ocp-backend/issues/138)) must not import it.

`cmd/robne` **can** import `internal/` (same Go module) but must **not** import the engine god-package: `internal/engine` pulls Prometheus metrics, cluster cache, fleet heatmap, logging, and `internal/model`. A slim persist package is the ADR-0305 shape.

Do **not** “fix” remaining I/O pain by moving the **product plugin architecture** into librobne core. In this repo that means `CSVIngestor` / `APIProvider` / `RetentionProvider`, Echo routes, `init()` registry, and Kafka dispatch (`internal/plugins`, `internal/plugin`). Those are product I/O, not compute. Engines (`RecommendWorkloads`, node/GPU/PVC/…) are **already** in librobne. Putting Echo/Kafka/GORM/`CSVIngestor` into librobne **core** is the failure mode ADR-0303 / blueprint §3 already named: the operator image would drag pgx + central schema + HTTP it must never run.

Right shape: optional I/O packages beside core (`librobne/csv` today; rec-persist and later `pgdigest` the same way). CLI + processor import them; operator imports **neither**. `--plugins` on the CLI stays a recommender allowlist, not a second `init()` registry.

`pgx` is pure Go (`CGO_ENABLED=0` still works). Do not use CGO `lib/pq` in the CLI.

### Schema — embed migrations, bootstrap, upgrade; never downgrade

Use case **(c)** has no `rosocp` / chart to apply DDL. `robne` embeds the **same** `migrations/` tree the processor uses (`go:embed` + golang-migrate `iofs`). One tree.

| Situation | `--apply-schema` | 2a behavior |
|-----------|------------------|-------------|
| Empty Postgres (no `schema_migrations`) | **Required** | Bootstrap: `migrate.Up()` from 0, then upsert. Without the flag: error (do not create tables). |
| `dirty = true` | — | Error. Do not write. |
| DB version **<** this binary’s version | **Required** | Upgrade: `migrate.Up()` to the embedded head, then upsert. Without the flag: error; name both versions. |
| DB version **=** this binary | Not required | Upsert. `Up()` at head is a no-op. Daily cron. |
| DB version **>** this binary | — | Error. Name both numbers. Do **not** `migrate.Down()`. Install a matching or newer `robne`. |

**Wrong-database seatbelts:**

1. Dedicated database. Do not `--output` at live Helm/cost-onprem Postgres.
2. `--apply-schema` for bootstrap/upgrade (above) so a typo DSN to an **empty** catalog does not create ROS tables unless asked.
3. If `clusters` already has any row whose `source_id` is **not** exactly `robne`: **refuse** (looks like Sources/Helm). No `--force` in v1. CLI-created rows always use `source_id = 'robne'` (not the cluster UUID — that would break this check).

### `clusters` row — ensure from YAML (case c has no Sources)

Case **(c)** never runs Kafka/Sources. **2a inserts** `rh_accounts` (from YAML `org_id`) and `clusters` (from YAML `cluster_uuid`) if missing. `source_id` is always the string `robne`. `last_reported`: now. Do not invent a random `source_id` each run (unique key would fork rows). Do not use the cluster UUID as `source_id` (Helm-detection below keys on `source_id = 'robne'`).

**`cluster_uuid` on PG write:** YAML value must be an RFC 4122 UUID (migration `000041`). Phase 1 stdout may use `local-cluster`. `--output postgres://` with a non-UUID is a **hard error**. Do **not** hash or name-uuid the string (silent identity mismatch with the operator’s real cluster UUID).

If a row already exists for that `cluster_uuid` **and** `source_id = 'robne'`, use it. If **any** `clusters` row has `source_id <> 'robne'`: refuse the whole `--output` (full stack). Do not overwrite a Sources `source_id`.

### Entity CSVs vs ingest ([#475](https://github.com/pgarciaq/ros-ocp-backend/issues/475))

`librobne/csv` parses **container** ROS, **namespace** ROS (`KindNamespace`: NISE `*ocp_ros_namespace_usage.csv`, operator `ros-openshift-namespace-*.csv`), **storage** (`KindStorage`: `ros-openshift-storage-*`, `ocp_storage_usage`, `cm-openshift-storage-usage` before blanket `cm-openshift-*` → cost-only), **VM** (`KindVM` / `KindVMPVC` / `KindVMGPU`: `ros-openshift-vm-usage-*` / `ocp_ros_vm_usage`, optional `ros-openshift-vm-pvc-*` / `ocp_ros_vm_pvc`, `ros-openshift-vm-gpu-device-*` / `ocp_ros_vm_gpu_device`), **cluster quota** (`KindClusterQuota`: `ros-openshift-cluster-quota-*`, `ocp_ros_cluster_quota` classified **before** namespace), and **snapshot inventory** (`KindSnapshot`: `ros-openshift-snapshot-*`, `ocp_snapshot_inventory`, `cm-openshift-snapshot-inventory` classified **before** blanket `cm-openshift-*`). Classify VM usage **before** `ocp_ros_usage` (`ocp_ros_vm_usage` contains that substring). Classify namespace **before** `ocp_ros_usage` so the substring does not steal namespace files. Container ROS optional columns feed in-memory node and GPU daily aggregation (`DailyNodeDigests`, `DailyGPUDigests`). Storage rows feed `DailyPVCDigests` (byte-seconds → bytes, same as ingest `ComputePVCDigests`). VM usage + companions feed `DailyVMDigests` (companions attach onto existing usage days only). Namespace ROS optional quota columns feed `DailyNamespaceQuotaDigests` (one row per namespace×quota×day; day key is interval **end**; `LatestNamespaceQuotaSnapshots` wraps that). Cluster-quota rows feed `DailyClusterQuotaDigests` (includes days without hard limits; `LatestClusterQuotaSnapshots` still filters `HasHardLimits()`, daily max, last non-empty namespaces, sort by name). Snapshot rows feed `LatestSnapshotInventory` (hourly collapse) then `ClassifySnapshotInventory`. `internal/ingestion` already parses those families. Remaining **2b** under [#472](https://github.com/pgarciaq/ros-ocp-backend/issues/472) is none. Do **not** rewrite ingest as a 2a/2b/pgdigest side quest. Container ingest streams through `librobne/csv.ForEachRow` ([#475](https://github.com/pgarciaq/ros-ocp-backend/issues/475)); other-entity ingest parsers are still duplicated. Operator never imports `csv`.

### Reserved YAML keys until a settings schema lands

`node`, `gpu`, `pvc`, `vm`, `quota`, `cluster_quota`, `snapshot` error if `enabled: true` (`reservedYAMLKeys`). **Keep those YAML-block errors** even after `--plugins` unlocks that entity (compiled defaults; overlay container `terms` for VM; snapshot uses `DefaultSnapshotSettings`). Unlock YAML **per entity in the same PR** that parses a dedicated settings schema. Namespace has **no** reserved `namespace:` YAML block — it reuses container `sizing` / `terms`. `--plugins namespace`, `node`, `gpu`, `pvc`, `vm`, `quota`, `cluster_quota`, and `snapshot` are unlocked. `business_hours:` is **not** a `--plugins` name; the typed YAML block is unlocked ([#479](https://github.com/pgarciaq/ros-ocp-backend/issues/479)).

Same-cluster tarball with many file types is OK in 2b; do **not** concatenate two NISE `--ocp-cluster-id` runs (Phase 1 one-cluster error stays).

### Native write path (not Kruize `workloads` FK)

Writes follow **today’s processor**, not `migrations/000004`:

| | Native (do this) | Kruize-era (do not rebuild) |
|--|------------------|------------------------------|
| Parent row | None. Identity is denormalized on the rec row. | Insert `workloads` first (`workload_id` FK) |
| Conflict key | `(org_id, cluster_uuid, namespace, workload, workload_type, container_name, term, engine)` | `(workload_id, container_name)` |
| API | `LEFT JOIN workloads` / `clusters` with `COALESCE` to denormalized columns | Require the join |

Persist the **full** `ContainerRec` (explanations, variations, replica fields). Stdout JSON stays the [#470](https://github.com/pgarciaq/ros-ocp-backend/issues/470) subset. Do not upsert the DTO.

`org_id` and `cluster_uuid` come from YAML (required when `--output` is PostgreSQL). `cluster_uuid` must parse as UUID for that path.

**Out of scope for 2a:** CLI UI (none; operator UI is [#138](https://github.com/pgarciaq/ros-ocp-backend/issues/138)), Masu HTTP, Kafka, historical rec tables, other entity CSVs ([#472](https://github.com/pgarciaq/ros-ocp-backend/issues/472)). Digest **SELECT** is [#474](https://github.com/pgarciaq/ros-ocp-backend/issues/474) **shipped**. Digest **INSERT** (`pgdigest`, [#463](https://github.com/pgarciaq/ros-ocp-backend/issues/463)) **shipped** after 2a. Other-entity digest INSERT ([#481](https://github.com/pgarciaq/ros-ocp-backend/issues/481)) **shipped**. Daily operator payloads are typically ~one day of CSV; without stored digests, medium/long terms silently match short until this INSERT (and 2d SELECT). A second payload for the same container-day is **last-write-wins** (`ON CONFLICT DO UPDATE`), same as the processor — not a merge of partial hours. Other-entity days use the same LWW writers (not ingest `GREATEST`/`LEAST`). `--output` writes today’s digests, SELECTs the term window (2d, containers), then upserts recs; either failure fails the command. CLI writes `schedule_type=all_hours` always. When YAML `business_hours.enabled` is true, it also writes `business_hours` container and namespace digest rows, and namespace recs for both streams. Container recs stay all_hours only (`recommendation_sets` has no `schedule_type`).

SQLite stays out (see below).

### SQLite — considered, not Phase 2

Do **not** replace PostgreSQL with SQLite. Do **not** add SQLite alongside PostgreSQL in Phase 2.

They are different products:

| Store | Job |
|-------|-----|
| JSON / CSV / table | Portable artifact (laptop, CI, air-gap). Phase 1 already does this. Phase 3 `diff` compares these files. |
| PostgreSQL | Use case **(c):** a database **this CLI owns** (schema in the binary, daily recs). Not “seed Helm production.” |
| SQLite | A third persist path. Not needed for either job above. |

Reasons SQLite stays out of Phase 2:

1. **Product schema is PostgreSQL** (`JSONB`, PG types, product migrations). A SQLite file that pretends to be `recommendation_sets` is a second schema. A CLI-owned SQLite schema is yet another data model.
2. **ADR-0305** wants a static binary with no CGO. `github.com/mattn/go-sqlite3` needs CGO. Pure-Go SQLite exists; it is extra weight for a use case JSON already covers.
3. **Phase 3 `robne diff`** should diff two recommend outputs (JSON), not require a queryable DB.

If a later child issue wants `sqlite://./robne.db` for ad-hoc SQL, that is **Phase 3+ optional**, CLI-owned schema, **in addition to** PostgreSQL — never a substitute for the product upsert.

Blind `COPY FROM` into `recommendation_sets` is still wrong (conflict keys, `updated_at`, metadata refresh). 2a is `INSERT … ON CONFLICT`. Staging `COPY` then upsert can wait until a child issue measures bulk load.

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

**Shipped** ([#479](https://github.com/pgarciaq/ros-ocp-backend/issues/479)). Not Phase 1. Not [#472](https://github.com/pgarciaq/ros-ocp-backend/issues/472). Not a `--plugins` name. Not an entity CSV.

YAML `business_hours:` is cluster-wide (no org/cluster/namespace inheritance). The CLI does **not** read or write `business_hours_schedules`, does not reship, and does not use Unleash. Omit the key → compiled default **off** (existing goldens stay valid). Overlay: later file **replaces the whole `business_hours:` key**. Unknown nested keys error. Days are lowercase monday–sunday. Invalid IANA timezone is an error (not silent UTC). `enabled` is required if the block is present; `enabled: false` with other fields is valid (parse, do not filter). Equal start/end is invalid (zero-width). Overnight (`end_time < start_time`) is **allowed** in CLI YAML and Settings PUT ([#488](https://github.com/pgarciaq/ros-ocp-backend/issues/488) **shipped**). Eval is `InBusinessHours`. `off_hours_weight` omit → 0.

**Dual digest streams (do not simplify):** always unweighted `all_hours`. When BH is enabled, a second `schedule_type=business_hours` stream uses `ScheduleWeight(interval_start)` + `digest.ComputeWeightedDigest` (weight `≤ 0` drops the sample). This is **not** a query-time filter on stored `all_hours` (ADR-0127). `--now` is decay/staleness only; classification is sample `interval_start` in the YAML IANA zone against the YAML window. Empty BH after weighting → empty JSON siblings, **not** a missing-plugin error. Skip days with no retained samples.

Eval lives in **`librobne/bhschedule`**. `internal/bhschedule` keeps SQL/cache/prune and type-aliases `Schedule`. `cmd/robne` imports `librobne/bhschedule` only. `librobne/csv` takes a weight callback; it must **not** import `bhschedule`. `DailyDigests(rows)` stays unweighted.

**Stdout:** BH off → unchanged envelope (version still driven by plugins, including 9 for snapshot). BH on → version **10** for container/namespace siblings; version **11** when `--plugins` includes node, gpu, or vm (or implicit default keeps node). Siblings `business_hours_recommendations` and `business_hours_namespace_recommendations` (always arrays, never `null`). Node/GPU/timeslicing/VM BH keys (`business_hours_node_recommendations`, `business_hours_gpu_recommendations`, `business_hours_gpu_timeslicing_recommendations`, `business_hours_vm_recommendations`) are **full CLI DTOs** (same shape as all-hours siblings) — **not** the product thin nest. BH rows with sizing set `notification_codes` to **79** / **80** / **81** / **82** only ([#492](https://github.com/pgarciaq/ros-ocp-backend/issues/492) **shipped**); all-hours siblings omit the key; do not copy the engine catalog. Omit a key when that plugin is off. Empty after weighting is still `[]`, never copy all-hours into a BH key. Omit all BH keys when BH is off. Existing `recommendations` / `namespace_recommendations` stay **all_hours**. `csv` / `table`: **hard error** when `business_hours.enabled` (one entity **and** one schedule stream; JSON only).

**This PR (#479) is container and namespace only.** Node/GPU/VM CLI JSON BH is [#487](https://github.com/pgarciaq/ros-ocp-backend/issues/487) (**shipped**: full siblings, envelope 11). Product APIs keep thin detail nests. PVC/snapshot/quota stay out. Quota/CRQ still use **all_hours** container days.

**Postgres:** files → `--output postgres://` dual-writes container and namespace digests (`all_hours` and `business_hours`). [#487](https://github.com/pgarciaq/ros-ocp-backend/issues/487) also dual-writes **node / GPU container / VM** BH digests (`Write*WithSchedule`). Do **not** upsert BH recs onto tables without `schedule_type` (node/GPU/VM rec tables stay all_hours). Namespace BH rec overwrite of all-hours rows is **removed**. Container recs persist all_hours only. Path A SELECTs matching stream(s) and never re-filters stored `all_hours`. Path A + YAML `enabled: true` + **explicit** `--plugins node|gpu|vm` + **no** BH digest rows → **hard error**. Implicit default with empty BH node/gpu/vm digests emits `[]` and keeps all-hours recs. Container/namespace Path A empty BH is always a hard error when that plugin is on. Path B empty BH SELECT is OK (files semantics, including weekend + explicit node). Path B re-SELECTs BH container days after persist so medium/long include prior days.

---

## 8. Tarball `./` prefix — should we fix ingest?

**Yes.** Documented workaround (`tar czf … --transform='s|^\./||' .`) is not a substitute for a parser fix.

**What breaks today (koku listener):**

- `extract_tarball_to_directory` returns `TarFile.getnames()` (member strings as archived).
- `get_manifest_member_name` requires the **exact** string `manifest.json`. Members from `tar czf archive.tar.gz .` are typically `./manifest.json` → **“No manifest found in payload.”**
- ROS routing uses `if ros_file in payload_files` (exact match of manifest `resource_optimization_files` vs member names). `ocp_ros_usage.csv` vs `./ocp_ros_usage.csv` → empty `ros_reports` → “No ROS reports to handle in the current payload.”

That is a **koku** bug (`masu/external/kafka_msg_handler.py` + `masu/util/ocp/common.py`). Tracker: [#466](https://github.com/pgarciaq/ros-ocp-backend/issues/466). Fix by normalizing members: strip a leading `./`, compare `posixpath.basename`. Keep the transform workaround in docs until that ships.

**CLI Phase 1** already normalizes tar members the same way, even if koku is unfixed — otherwise `oc cp` of an operator package still works, but a `tar czf .` NISE archive would not.

The koku patch ([#466](https://github.com/pgarciaq/ros-ocp-backend/issues/466)) is independent of the CLI.

---

## 9. Phasing (locked for greenlight)

| Phase | In | Out |
|-------|----|-----|
| **1** | Container ROS CSV (NISE **or** operator tarball/dir); YAML; `--plugins`; `--now`; `--rate-card`; `validate` | JSON / CSV / table |
| **2a** ([#471](https://github.com/pgarciaq/ros-ocp-backend/issues/471)) | Same files | `--output postgres://…` — `--apply-schema` on bootstrap/upgrade, ensure cluster `source_id=robne`, container upsert |
| **2b** ([#472](https://github.com/pgarciaq/ros-ocp-backend/issues/472)) | Other entity CSVs | JSON / CSV / table envelopes. **Shipped.** |
| **2c** ([#473](https://github.com/pgarciaq/ros-ocp-backend/issues/473)) | 2b files | Other entity **rec** PG upsert. **Shipped.** |
| **pgdigest** ([#463](https://github.com/pgarciaq/ros-ocp-backend/issues/463)) | Same files as 2a | Container digest **INSERT**. **Shipped.** Needed for daily incremental payloads (medium/long). |
| **2d** ([#474](https://github.com/pgarciaq/ros-ocp-backend/issues/474)) | This CLI’s `daily_container_digests` | Stdout and/or 2a upsert. **Shipped (container-only).** |
| **2b digest INSERT** ([#481](https://github.com/pgarciaq/ros-ocp-backend/issues/481)) | 2b files | Other-entity digest INSERT (`pgdigest` extend). After 473. **Shipped.** |
| **2b digest SELECT** ([#482](https://github.com/pgarciaq/ros-ocp-backend/issues/482)) | This CLI’s other-entity digest tables | Path A for 2b plugins. After 481. **Shipped.** |
| **snapshot** ([#478](https://github.com/pgarciaq/ros-ocp-backend/issues/478)) | Snapshot inventory CSV | JSON / CSV / table. Not 2b. **Shipped.** |
| **business hours** ([#479](https://github.com/pgarciaq/ros-ocp-backend/issues/479)) | YAML `business_hours:` | Dual digest streams (container + namespace). Not an entity CSV. **Shipped.** |
| **3** ([#480](https://github.com/pgarciaq/ros-ocp-backend/issues/480)) | Two JSON envelopes (from (a)(b) or (c)); same `--input` as recommend for `explain` | `diff`, container `explain`, CI. **Shipped.** |
| **3+** ([#490](https://github.com/pgarciaq/ros-ocp-backend/issues/490)) | Same as `explain` | Other-entity `explain` DTOs / selectors. **Shipped.** |
| **identity** ([#489](https://github.com/pgarciaq/ros-ocp-backend/issues/489)) | none | `robne version`: binary identity + envelope capability table. **Shipped.** |

`librobne/csv` landed in Phase 1. **pgdigest** INSERT is [#463](https://github.com/pgarciaq/ros-ocp-backend/issues/463), **shipped (containers).** Digest **SELECT** (recommend path) is 2d ([#474](https://github.com/pgarciaq/ros-ocp-backend/issues/474)), **shipped (containers).** Other-entity digest INSERT is [#481](https://github.com/pgarciaq/ros-ocp-backend/issues/481), **shipped.** Other-entity SELECT/Path A is [#482](https://github.com/pgarciaq/ros-ocp-backend/issues/482), **shipped.** Ingest parser dedup ([#475](https://github.com/pgarciaq/ros-ocp-backend/issues/475), **shipped** — container `ForEachRow`; other-entity ingest parsers still duplicated) and `QueryContainerDigests*` dedup ([#476](https://github.com/pgarciaq/ros-ocp-backend/issues/476)) are P5 under [#94](https://github.com/pgarciaq/ros-ocp-backend/issues/94), not CLI children.

---

## 10. Greenlight checklist

**Accepted 2026-08-16.** Children in §0 are already filed. This list is the lock, not a prompt to file more issues.

1. Parent #99 + children as in §0.
2. Phase 1 scope as in §9 (container / files / YAML / rate card / `--now` only) — **shipped**.
3. YAML schema §2 (unknown keys = error; user overlay; **replace whole top-level keys**, no deep-merge of `sizing:`; `cmd/robne/robne.yaml.sample`). Public overlay docs: features/robne-cli.md.
4. `--now` is the decay/staleness clock (`EngineConfig.Now`); term windows stay anchored at latest digest day (same as the processor). Never wall clock as silent fallback (§3).
5. NISE column gap: accept today’s files; fix NISE via [#465](https://github.com/pgarciaq/ros-ocp-backend/issues/465) (§4).
6. Phase 1 = JSON/CSV/table **(a)(b)**. PostgreSQL is **2a** ([#471](https://github.com/pgarciaq/ros-ocp-backend/issues/471)) for **(c)**: embed `migrations/`, `--apply-schema` for bootstrap/upgrade, never Down, ensure cluster `source_id=robne`, refuse foreign `source_id`, native upsert. **No SQLite. No CLI UI.** Digest **INSERT** is pgdigest ([#463](https://github.com/pgarciaq/ros-ocp-backend/issues/463)) **shipped (containers)** plus other-entity INSERT ([#481](https://github.com/pgarciaq/ros-ocp-backend/issues/481)) **shipped**. Digest **SELECT** is **2d** ([#474](https://github.com/pgarciaq/ros-ocp-backend/issues/474)) **shipped (containers)** plus other-entity Path A ([#482](https://github.com/pgarciaq/ros-ocp-backend/issues/482)) **shipped**.
7. Rate card JSON in dollars (§6): **`clusters` map**; overlay **merges by cluster id** (later file replaces that cluster object, not nested maps); `by_architecture` **replaces** `default_*` for that arch (not added); GPU `by_model` same rule. User `~/.config/robne/rate-card.json`. Sample `cmd/robne/rate-card.json.sample`. No `~/.rate-card.yaml`. No global scalar card.
8. Business hours not Phase 1 (§7). Child: [#479](https://github.com/pgarciaq/ros-ocp-backend/issues/479) **shipped** (container + namespace YAML + dual streams). Namespace plugin does **not** by itself unlock `business_hours:` — the typed YAML block does.
9. Fix koku `./` matching in koku ([#466](https://github.com/pgarciaq/ros-ocp-backend/issues/466)); CLI already normalizes (§8).

No code beyond the current child until that issue is the active sprint. Remaining **2b** under [#472](https://github.com/pgarciaq/ros-ocp-backend/issues/472) is none. Other-entity rec upsert ([#473](https://github.com/pgarciaq/ros-ocp-backend/issues/473)) is **shipped**. Other-entity digest INSERT ([#481](https://github.com/pgarciaq/ros-ocp-backend/issues/481)) is **shipped**. Other-entity Path A SELECT ([#482](https://github.com/pgarciaq/ros-ocp-backend/issues/482)) is **shipped**. Snapshot files → stdout ([#478](https://github.com/pgarciaq/ros-ocp-backend/issues/478)) is **shipped**. Business hours ([#479](https://github.com/pgarciaq/ros-ocp-backend/issues/479)) is **shipped**. Phase 3 ([#480](https://github.com/pgarciaq/ros-ocp-backend/issues/480)) is **shipped**. Other-entity `explain` ([#490](https://github.com/pgarciaq/ros-ocp-backend/issues/490)) is **shipped**. Binary identity / envelope capability ([#489](https://github.com/pgarciaq/ros-ocp-backend/issues/489)) is **shipped**. GPU product API ([#485](https://github.com/pgarciaq/ros-ocp-backend/issues/485)) shipped (thin detail nest). GPU timeslicing product BH ([#491](https://github.com/pgarciaq/ros-ocp-backend/issues/491)) shipped (detail nest only). VM product BH ([#486](https://github.com/pgarciaq/ros-ocp-backend/issues/486)) shipped (detail thin nest only). CLI JSON BH siblings ([#487](https://github.com/pgarciaq/ros-ocp-backend/issues/487)) **shipped** (full DTOs, envelope **11**). CLI BH codes 79–82 ([#492](https://github.com/pgarciaq/ros-ocp-backend/issues/492)) **shipped**. Overnight Settings PUT ([#488](https://github.com/pgarciaq/ros-ocp-backend/issues/488)) **shipped**. Node product API ([#484](https://github.com/pgarciaq/ros-ocp-backend/issues/484)) shipped.
