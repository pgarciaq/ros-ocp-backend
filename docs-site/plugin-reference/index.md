# Plugin Reference

> **Last verified:** 2026-09-20

This section documents each recommendation plugin: endpoints, settings, savings behavior,
and links to feature docs.

**All pages under `plugin-reference/` are hand-maintained** in `docs-site/plugin-reference/`
and committed to git. Edit them directly — do **not** expect `make docs-build` or CI to
regenerate them from Go source.

For product behavior (capabilities, algorithms, configuration), prefer:

- [Features](../features/index.md)
- [Configurability Reference](../architecture/configurability.md)
- [Recommendation Engines](../architecture/recommendation-engines.md)

Plugin-reference pages link into those docs and into `internal/plugins/*/`.

**Scaffold a new plugin:** `make new-plugin NAME=...` (see
[Local Development — Adding a plugin](../development.md#adding-a-plugin) and
[`cmd/newplugin`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/cmd/newplugin)).

**Trait catalog (what each trait means):** [Plugin Traits](../architecture/plugin-traits.md) — role, when to use,
who implements it. The [matrix](#trait-matrix) below is a checklist only. Go
signatures and design notes: [Plugin Architecture §4](../architecture/plugin-architecture.md#4-plugin-interfaces-trait-based).

## Refresh path

| Action | Command / path |
|--------|----------------|
| Edit a plugin page | Change `docs-site/plugin-reference/<name>.md` and commit |
| Preview the site | `make docs-serve` (does **not** overwrite plugin-ref) |
| Assemble known-issues / CONTRIBUTING copies | `make docs-generate` (CI runs this before Pages build) |
| Optional gomarkdoc dump (maintainers only) | `DOC_GENERATE_GOMARKDOC=1 make docs-generate` — **overwrites** curated `plugin.md`, `kruize.md`, and `example.md`; review the diff carefully |

`scripts/generate-docs.sh` no longer copies `docs/architecture/` or `docs/operations/` over
`docs-site/` — those trees are curated under `docs-site/`.

## Package Hierarchy

```
internal/plugin/          ← Trait interfaces and registry
internal/plugins/         ← Aggregator (imports all production plugins)
internal/plugins/
├── container/            ← CPU/memory recommendations
├── gpu/                  ← GPU utilization and MIG/time-slicing
├── node/                 ← Node sizing and utilization
├── pvc/                  ← PVC storage right-sizing
├── quota/                ← ResourceQuota hard-limit recommendations
├── cluster-quota/        ← ClusterResourceQuota team-pool recommendations
├── namespace/            ← Namespace usage-based sizing
├── snapshot/             ← VolumeSnapshot staleness
├── vm/                   ← OpenShift Virtualization VM right-sizing
├── kruize/               ← Legacy engine (mutual-exclusive)
└── example/              ← Authoring template for new plugins
```

## Trait Matrix

See also the author-facing [Plugin Traits](../architecture/plugin-traits.md) catalog for explanations of each
interface.

| Plugin | CSVIngestor | IngestHook | APIProvider | APIEnricher | RetentionProvider | TermProvider |
|--------|:-----------:|:----------:|:-----------:|:-----------:|:-----------------:|:------------:|
| container | ✓ | | | | ✓ | ✓ (max 90d) |
| gpu | | ✓ | ✓ | ✓ | ✓ | ✓ (max 90d) |
| node | | ✓ | ✓ | | ✓ | ✓ (max 90d) |
| pvc | ✓ | | ✓ | | ✓ | ✓ (max 365d) |
| quota | | | ✓ | | ✓ | |
| cluster-quota | ✓ | | ✓ | | ✓ | |
| namespace | ✓ | | ✓ | | ✓ | ✓ (max 90d) |
| snapshot | ✓ | | ✓ | | | |
| vm | ✓ | | ✓ | | ✓ | ✓ (max 90d) |
| kruize | | | | | | |

## Plugin dependencies

What each plugin needs from other plugins, by level (**CSVs → digests → recs** — each dependency names its level). Verified against `internal/plugins/*/plugin.go` (hooks), generation SQL, and serving handlers.

| Plugin | Requires | Level | If requirement missing |
|---|---|---|---|
| container | — | — | — (foundation) |
| namespace | — | — | — |
| node | **container** | CSVs: node hook derives node digests from container rows (`node/plugin.go`) | node digests go stale (recs not needed) |
| vm | — | — | — |
| pvc / snapshot / cluster-quota | — | — | — |
| quota | **container** | recs: aggregates sum `recommendation_sets` (`engine/quota/recommend_quota.go`) | soft: aggregates read as zeros (documented one-cycle lag) |
| gpu | **container** | CSVs (hook) + recs (MIG history, `has_gpu` marking) | soft: core GPU recs unaffected; history + flags degrade (degradation must be user-visible via notification code, not logs-only) |
| business-hours (each variant) | its own entity (see [BH digest sources](business-hours.md#per-entity-digest-sources)) | digests of that entity + shared schedules | that variant is dead; others unaffected |

### Enablement drag (transitive closure)

Read as "enabling X must also enable…". Container digestion is always-on (core fallback), so CSV-level drags hold today by accident; rec-level drags need the container plugin actually generating.

- `gpu` ⇒ **container** (CSVs + recs) · `quota` ⇒ **container** (recs) · `node` ⇒ **container** (CSVs only)
- `business-hours` ⇒ whichever entity's BH you want (container-BH→`container`, VM-BH→`vm`, node-BH→`node`, ns-BH→`namespace`, GPU-BH→`gpu`) + BH schedules
- `container`, `namespace`, `vm`, `pvc`, `snapshot`, `cluster-quota` ⇒ nothing
- **Misconfiguration:** any `ROS_ENABLED_PLUGINS` allowlist with `gpu`, `quota`, or `node` but without `container` is silently degraded. Decided: **no auto-drag** — the allowlist is explicit intent, and container enablement has visible scope consequences (recs served, telemetry volume). Violations fail fast at startup (kruize precedent), naming the fix. The `robne` CLI already enforces this shape (`requireExplicitFilePlugins` errors on explicit selection, prunes silently on auto-detection); the server startup validation must mirror it.
- **Per-object label gating, per-entity verdicts.** Namespace scope is the default collection-scoping unit, not the only conceivable one:
  - *VMs, PVCs, snapshots: coherent but unbuilt.* Independent units — excluding one doesn't falsify others' recs. VMs/PVCs would need per-entity label joins (~dozens of queries each); snapshots only a label selector on the existing API list call (cheapest of the three). All await demand, with documented semantics.
  - *GPUs: split.* Frame-buffer series already follow the namespace label (HCP branch included). Per-GPU labels are impossible — GPUs aren't Kubernetes objects, nothing carries the label. VM-GPU mapping follows VM policy.
  - *Nodes: declined (math).* Capacity recs over partial-cluster data are wrong recs, not fewer recs. Whole-cluster on/off is the only sound control.
  - *Cluster objects: impossible (structure).* No namespace or object label can scope cluster-level queries.
  - Same key on admin-owned vs team-owned objects would need per-entity documented semantics — a shared unlabeled meaning is not assumed.

## Controlplane guardrail profile (W1)

Management control-plane rows (HCP namespaces) take stricter floors than generic app containers, on cost and perf engines alike. Detect-and-route: every row still flows; floors only ever raise recommendations, never lower or exclude them.

| | Generic | Controlplane guardrail |
|---|---|---|
| CPU floor | 25m (`ROS_CONTAINER_CPU_FLOOR_MC`) | `max(100m, 70% of current request)` |
| Memory floor | 4MiB (`ROS_CONTAINER_MEM_FLOOR_KIB`) | `max(128MiB, 70% of current request)` |
| Replica recs | optimized (min 2 deploy / 1 StatefulSet) | suppressed for HCP groups (operators own CP topology) |

Algorithm: build the known-HCP set once per run (payload manifest topology facts; empty means off) → per container group, route by namespace membership → override floors from the **window-median** request P50 (median resists single-bucket redeploy drops; the 1-row short window falls back to the absolute leg) → skip replica optimization for guardrailed groups.

Magic numbers, explained: **70%** fires only on gross over-request (never aggressive downsize); **100m/128MiB** sit above generic noise but below any real CP need, governing ceremonial or blank requests; **25m/4MiB** generic exists to block absurd near-zero app recs. Payload manifest whose `cluster_id` mismatches the CSV cluster fails fast (mixed payload dir). Implementation: `librobne/hcp` (rules, pin, floors) + hook in `librobne/engine` + `EngineConfig.HCPNamespaces`; CLI wired from manifest (#584 Path 1), server path tracked in #590.

## Term Defaults

| Plugin | Short | Medium | Long | Max |
|--------|-------|--------|------|-----|
| container | 1d window / 1d min | 7d / 3d min | 15d / 7d min | 90d |
| gpu | 1d / 1d | 7d / 3d | 15d / 7d | 90d |
| node | 1d / 1d | 7d / 3d | 15d / 7d | 90d |
| namespace | 1d / 1d | 7d / 3d | 15d / 7d | 90d |
| pvc | 7d / 3d | 30d / 14d | 90d / 30d | 365d |
| vm | 7d / 3d | 15d / 7d | 30d / 15d | 90d |

## Browsing

Use the sidebar to navigate to individual plugin documentation. Each page includes package
metadata, endpoints, key filters, notification codes, savings behavior, and links to feature
and architecture docs.

| Plugin / feature | Page |
|------------------|------|
| plugin (interfaces) | [plugin](plugin.md) |
| container | [container](container.md) |
| node | [node](node.md) |
| pvc | [pvc](pvc.md) |
| gpu | [gpu](gpu.md) |
| namespace | [namespace](namespace.md) |
| quota | [quota](quota.md) |
| cluster-quota | [cluster-quota](cluster-quota.md) |
| snapshot | [snapshot](snapshot.md) |
| vm | [vm](vm.md) |
| kruize (legacy) | [kruize](kruize.md) |
| example (template) | [example](example.md) |
| business-hours | [business-hours](business-hours.md) (cross-cutting enrichment, not a plugin) |
| idle-detection | [idle-detection](idle-detection.md) |

## Query parameters

List endpoints use Koku-aligned bracket notation (`filter[project]`, `order_by[field]`).
See [Query Parameters](query-parameters.md) for the full bracket-syntax reference.
