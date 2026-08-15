# Savings Estimations

Internal reference for the savings estimation subsystem.

## Overview

ROS computes per-workload monthly savings estimates by comparing current resource allocations against recommended values, multiplied by cost rates from Koku Masu `effective_rates`.

## Key Files

- `internal/engine/savings.go` — Container savings logic
- `internal/engine/node/savings.go` — Node savings
- `internal/engine/pvc/pvc_savings.go` — PVC savings (including orphaned)
- `internal/engine/vm/vm_savings.go` — VM savings
- `internal/engine/gpu/query.go` — GPU MIG/idle savings persistence (`estimated_gpu_savings_cents`)
- `internal/engine/gpu/recommender.go` — GPU savings computation (fallback when not persisted)
- `internal/engine/savings_recalculate.go` — Cost-model-triggered savings refresh
- `internal/api/handlers_savings_summary.go` — Fleet savings-summary endpoint
- `internal/api/handlers_savings_recalculate.go` — Internal recalculate endpoint
- `internal/money/format.go` — `MoneyAmount` API shape
- `internal/config/config.go` — Kill-switch (`ROS_SAVINGS_ESTIMATES_ENABLED`)

## Architecture

See [cost-integration.md](../architecture/cost-integration.md) for formulas, rate resolution chain, fleet aggregation, and recalculation design.

## Public Documentation

See [docs-site/features/savings-estimations.md](../../docs-site/features/savings-estimations.md) for the user-facing feature page.

## Plugin coverage

| Plugin | Storage | API field | Fleet `savings-summary` |
|--------|---------|-----------|-------------------------|
| Container | `estimated_savings_cents` | `estimated_monthly_savings` (`MoneyAmount`) | `by_plugin.container` |
| Node | `estimated_savings_cents` | `estimated_monthly_savings` per engine | `by_plugin.node` |
| PVC | `estimated_savings_cents` | `estimated_monthly_savings` | `by_plugin.pvc` |
| VM | `estimated_savings_cents` | `estimated_monthly_savings` | `by_plugin.vm` |
| Snapshot | `estimated_cost_cents` | `estimated_monthly_cost` | `by_plugin.snapshot` |
| GPU MIG/idle | `estimated_gpu_savings_cents` | `estimated_monthly_gpu_savings` | Excluded (`by_plugin.gpu` = 0) |
| GPU time-slicing | (read-time) | `estimated_monthly_timeslicing_savings` | Excluded |
| Quota / cluster-quota | `estimated_savings_cents` | `estimated_savings` | Excluded (avoid double-count) |

## Kill-Switch

Set `ROS_SAVINGS_ESTIMATES_ENABLED=false` to disable savings computation. When disabled:

- Masu cost ingestion is skipped
- **Container / node / PVC:** savings fields return `$0` (numeric zero in cents)
- **VM:** savings field returns `null` (not computed at all)
- **GPU MIG/idle:** `estimated_monthly_gpu_savings` omitted; time-slicing savings omitted
- **Snapshot:** static/org-configured rate only (no Masu effective-rates default)

## Future Work

- Billing-derived snapshot costs ([COST-7523](https://redhat.atlassian.net/browse/COST-7523))
- Savings trends/time-series API (historical progression from `recommendation_history`)
- Fleet rollup of persisted GPU savings (`by_plugin.gpu`) once aggregation cost is acceptable
