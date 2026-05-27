# Idle / Zombie Detection

Classifies containers (and GPUs) as **active**, **idle** (low utilization vs requests), or
**zombie** (near-zero usage). Surfaces **terminate** guidance and **monthly waste** separate
from rightsizing **savings**.

## API quick reference

| Capability | Example |
|------------|---------|
| Filter list | `GET .../recommendations/openshift?filter[idle_state]=zombie,idle` |
| Waste by state | `GET .../savings-summary?group_by[idle_state]=*` |
| Settings | `GET/PUT .../settings/idle-detection` |

## Savings vs waste (UI)

- **`estimated_monthly_savings`** — resize opportunity (active workloads only in list API).
- **`estimated_monthly_waste`** — full cost recoverable by terminating idle/zombie workloads.
- **Never add** savings + waste in one total.
- When `idle_recommendation.action` is `terminate`, show **waste** as the primary dollar metric.

## Staleness

`idle_duration_days` is computed at the last recommendation run, not live. Treat as
accurate ±1 day relative to ingestion cadence.

## Configuration

Admin: `ROS_IDLE_*` env vars (see [Configuration](../configuration.md)). Tenants may
override via the idle-detection Settings API unless fields appear in `locked_fields`.

Full design: [Idle / Zombie Detection (internal)](../../docs/features/idle-detection.md).
