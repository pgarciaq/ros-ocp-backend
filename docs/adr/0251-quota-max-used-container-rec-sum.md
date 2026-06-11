# ADR-0251: Quota recommendations use max(quota_used, container_rec_sum) utilization signal

## Status

Accepted

## Context

Quota recommendations ([ADR-0028](0028-quota-engine-container-cost-medium-term.md)) compare ResourceQuota hard limits against utilization to suggest headroom adjustments ([ADR-0029](0029-quota-headroom-10-percent-70-90-risk-bands.md)). Container recommendations size workloads to medium_term cost engine rows ([ADR-0030](0030-quota-after-container-crq-after-namespace.md)).

If quota `used` metrics lag behind recommended container sizes, quota math underestimates future demand — recommending limits that fit today's usage but not post-rightsizing footprint.

## Decision

For **CPU and memory** quota recommendations, utilization signal is:

```
utilization_basis = max(quota_used_from_report, sum(container_medium_term_cost_recommendations))
```

Compare `utilization_basis` against hard limits when computing headroom and risk bands. Ensures quotas accommodate recommended sizes even when current `used` is lower than the sum of container right-sizing targets.

Storage, pods, and object-count quotas are handled separately ([ADR-0252](0252-storage-pods-object-count-quotas-used-only.md)).

## Alternatives Considered

### quota_used only

Under-provisions quota after container recs applied — namespace throttling risk.

### container sum only

Ignores burst usage above recommendations before adoption.

### max of long_term and medium_term sums

Over-aggressive; medium_term is the quota engine aggregate per [ADR-0028](0028-quota-engine-container-cost-medium-term.md).

## Consequences

- Quota notifications may fire earlier when container recs exceed current quota used.
- Produce ordering must run quota after container ([ADR-0030](0030-quota-after-container-crq-after-namespace.md)).
- Operators see quota recs aligned with adoption path, not just historical usage.

## Related Decisions

- [ADR-0028](0028-quota-engine-container-cost-medium-term.md): Quota engine aggregates.
- [ADR-0029](0029-quota-headroom-10-percent-70-90-risk-bands.md): Headroom and risk bands.
- [ADR-0252](0252-storage-pods-object-count-quotas-used-only.md): Non-CPU/memory quota rules.

## References

- [internal/plugins/quota/produce.go](../../internal/plugins/quota/produce.go)
- [docs/features/quota-recommendations.md](../features/quota-recommendations.md)
