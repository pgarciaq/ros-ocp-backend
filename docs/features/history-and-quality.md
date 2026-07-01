# Recommendation History & Quality

Internal reference for container recommendation history snapshots and post-recommendation quality metrics.

## Overview

| Area | Fleet API | Detail-only history |
|------|-----------|---------------------|
| **Containers** | `GET .../history`, `GET .../quality` | — |
| **Namespace / VM / quota / CRQ** | No fleet history API | Quota and cluster-quota embed `history[]` in detail responses |
| **Node / PVC / GPU** | Not implemented | PVC detail has usage time-series, not recommendation snapshots |

Quality metrics (stability, adoption, OOM-after-rec, recommendation age) are **container-only**.

## Key files

| Component | Path |
|-----------|------|
| History writer | `internal/engine/history.go` |
| Quality writer | `internal/engine/quality.go` |
| History API | `internal/api/handlers_history.go` |
| Quality API | `internal/api/handlers_quality.go` |
| Retention sweep | `internal/engine/retention.go` (`StartRetentionTicker`) |
| Config defaults | `internal/config/config.go` |

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `ROS_HISTORY_RETENTION_DAYS` | 90 | Monthly partition retention for `recommendation_history` and `recommendation_quality` |
| `ROS_STALE_CLEANUP_DAYS` | 30 | Delete stale `recommendation_sets` after this many days (`ROS_STALE_ARCHIVE_DAYS` deprecated alias) |
| `ROS_STALENESS_THRESHOLD_HOURS` | 48 | Mark recommendations stale when cluster stops reporting |

See [retention.md](../operations/retention.md) for sweep behavior and table list.

## API and metrics

- Fleet history: `GET /api/cost-management/v1/recommendations/openshift/history`
- Fleet quality: `GET /api/cost-management/v1/recommendations/openshift/quality`
- Default `filter[engine]` when omitted: **cost**
- API quality scales: `stability_pct` **0.0–1.0**; Prometheus gauges **0–100**
- `term` filter values: `short_term`, `medium_term`, `long_term` (canonical form per ADR-0069; raw DB values `short`/`medium`/`long` also accepted via `NormalizeRecommendationTermFilter`)
- Response includes 21 `expl_*` explanation columns (nullable, omitted when nil): data days, decay half-life, CPU/memory cost/perf percentiles, usage stats (p50/p95/mean), adaptive margins, trend slopes, OOM count/bump, floor flags, idle state

Design detail: [quality-metrics.md](../design/quality-metrics.md).

## Future work

- `data_coverage_pct` on quality rows
- Archive stale `recommendation_sets` to `recommendation_history` before deletion (today cleanup is delete-only)
- Per-plugin quality and GPU recommendation history

## Public documentation

See [docs-site/features/history-and-quality.md](../../docs-site/features/history-and-quality.md) for the customer-facing feature page.
