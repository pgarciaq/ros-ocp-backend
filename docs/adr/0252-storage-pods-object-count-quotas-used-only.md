# ADR-0252: Storage/pods/object-count quotas use used metrics only (not container sums)

## Status

Accepted

## Context

[ADR-0251](0251-quota-max-used-container-rec-sum.md) defines `max(quota_used, container_rec_sum)` for CPU and memory quota utilization. ResourceQuota also tracks **storage**, **pods**, and **object counts** (e.g., `count/configmaps`).

Container recommendation rows expose CPU/memory request/limit sizing — not analogous per-workload storage or pod-count targets suitable for summing into quota hard-limit comparisons.

## Decision

For **storage**, **pods**, and **object-count** quota dimensions, utilization uses **only actual `used` metrics** from the operator quota report. Container recommendation sums are **not** incorporated.

CPU/memory retain the max(used, sum) rule from [ADR-0251](0251-quota-max-used-container-rec-sum.md).

## Alternatives Considered

### Sum PVC recommendations into storage quota used

PVC recs are separate plugin ([ADR-0025](0025-pvc-thresholds-20-oversized-85-near-full.md)); double-counting and scope mismatch with ResourceQuota storage keys.

### Zero storage quota recs when no container signal

Loses valid FinOps signal from actual storage quota pressure.

### Unified max() for all resource types

Semantically wrong for non-container-scoped limits.

## Consequences

- Storage quota recommendations reflect observed quota usage, not projected PVC right-sizing.
- Pod count quotas ignore hypothetical pod scaling from CPU/memory recs.
- Documentation must split CPU/memory vs other quota resource behavior.

## Related Decisions

- [ADR-0251](0251-quota-max-used-container-rec-sum.md): CPU/memory max(used, sum).
- [ADR-0029](0029-quota-headroom-10-percent-70-90-risk-bands.md): Risk bands apply to all types with appropriate used signal.
- [ADR-0030](0030-quota-after-container-crq-after-namespace.md): Quota produce ordering.

## References

- [internal/plugins/quota/produce.go](../../internal/plugins/quota/produce.go)
- [internal/plugins/quota/classify.go](../../internal/plugins/quota/classify.go)
