# Plugin Reference

This section contains auto-generated API documentation from Go source code doc comments.
It is regenerated on every push to `main` — the source of truth is always the code itself.

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
| quota | | ✓ | ✓ | | ✓ | |
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

Use the sidebar to navigate to individual plugin documentation. Each page includes:

- Package overview and domain description
- Ingestion details (what CSV types, what data)
- Recommendation algorithm summary
- Default term configuration and rationale
- Trait interface implementations

Hand-maintained API reference pages (settings, filters, notification codes):

| Plugin | Page |
|--------|------|
| container | [container](container.md) |
| cluster-quota | [cluster-quota](cluster-quota.md) |
| node | [node](node.md) |
| quota | [quota](quota.md) |

## Query parameters

List endpoints use Koku-aligned bracket notation (`filter[project]`, `order_by[field]`).
See [Query Parameters](query-parameters.md) for the full bracket-syntax reference.
