# Plugin Reference

> **Last verified:** 2026-08-05

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
