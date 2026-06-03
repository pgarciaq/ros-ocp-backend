# Savings Estimations

Internal reference for the savings estimation subsystem.

## Overview

ROS computes per-workload monthly savings estimates by comparing current resource allocations against recommended values, multiplied by cost rates from Koku.

## Key Files

- `internal/engine/savings.go` — Container savings logic
- `internal/engine/node_savings.go` — Node savings
- `internal/engine/pvc_savings.go` — PVC savings (including orphaned)
- `internal/engine/vm_savings.go` — VM savings
- `internal/engine/gpu_recommender.go` — GPU savings (read-time)
- `internal/api/handlers_savings_summary.go` — Fleet savings-summary endpoint
- `internal/config/config.go` — Kill-switch (`ROS_SAVINGS_ESTIMATES_ENABLED`)

## Architecture

See [cost-integration.md](../architecture/cost-integration.md) for formulas, rate resolution chain, and fleet aggregation design.

## Public Documentation

See [docs-site/features/savings-estimations.md](../../docs-site/features/savings-estimations.md) for the user-facing feature page.

## Kill-Switch

Set `ROS_SAVINGS_ESTIMATES_ENABLED=false` to disable savings computation. When disabled:

- Masu cost ingestion is skipped
- **Container / node / PVC:** savings fields return `$0` (numeric zero in cents)
- **VM:** savings field returns `null` (not computed at all)
- **GPU:** savings are read-time only and omitted from the response (same as when enabled — GPU savings are never persisted)

## Future Work

- Billing-derived snapshot costs ([COST-7523](https://redhat.atlassian.net/browse/COST-7523))
- Savings trends/time-series API (historical progression from `recommendation_history`)
