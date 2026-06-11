# ADR-0178: Container confidence is dataDays/windowDays (unlike GPU tiers)

## Status

Accepted

## Context

Different resource types need different confidence models. Container, namespace, node, and PVC workloads have relatively stable utilization profiles—more days of data linearly improves confidence. GPUs have spike-prone workloads where burst patterns affect reliability non-linearly ([ADR-0023](0023-gpu-confidence-data-volume-burst-penalty.md)). VMs depend on guest-agent sample availability ([ADR-0033](0033-vm-p95-p99-whole-units-downsize-hysteresis.md)).

## Decision

Use resource-specific confidence formulas:

| Resource | Formula |
|----------|---------|
| Container / namespace / node / PVC | `min(1.0, dataDays / windowDays)` |
| GPU | Tiered day buckets + spike penalty (ADR-0023) |
| VM | Guest-agent sample ratio |

Low-confidence notification (code 3) fires when confidence falls below the configured threshold.

## Alternatives Considered

### Unified formula across all resources

Cannot account for resource-specific data quality and burst behavior.

### No confidence scoring

Users cannot assess recommendation reliability or filter low-quality results.

## Consequences

- Engineers porting GPU confidence patterns to containers would produce wrong notification thresholds.
- Each recommendation plugin must document its confidence model.
- Confidence is computed per term/engine row, not aggregated across engines.

## Related Decisions

- [ADR-0023](0023-gpu-confidence-data-volume-burst-penalty.md): GPU confidence tiers.
- [ADR-0033](0033-vm-p95-p99-whole-units-downsize-hysteresis.md): VM sizing and confidence.

## References

- [internal/engine/recommend_all.go](../../internal/engine/recommend_all.go)
- [internal/engine/gpu_recommender.go](../../internal/engine/gpu_recommender.go)
- [internal/engine/vm_recommender.go](../../internal/engine/vm_recommender.go)
