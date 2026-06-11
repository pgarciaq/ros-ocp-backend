# ADR-0198: GPU time-slicing notification code 36 and savings formula

## Status

Accepted

## Context

GPU time-slicing identifies GPUs that could be shared among multiple workloads. The notification code must fit the existing notification system ([ADR-0038](0038-notification-code-bitmap-1-63.md)) while extending beyond the original contiguous 1–35 range.

## Decision

Time-slicing emits `NotifGPUTimeSharingCandidate` (**code 36**) — outside the original contiguous 1–35 range.

**Savings formula:**

```
gpuMonthlyRate × (1 − 1/replicas) × candidates
```

where `replicas` is the recommended sharing factor.

## Consequences

- Notification code catalog is no longer a dense contiguous range (codes 36, 64, 67–69, 74, 76 exist beyond original 35).
- Plugin filters must account for non-contiguous codes.
- Savings assumes linear cost sharing (not accounting for contention overhead).

## Alternatives Considered

### Reuse existing code range

Would require renumbering existing codes and breaking clients. Rejected.

### Contention-adjusted savings

Requires workload interference modeling not available in v1. Deferred.

## Related Decisions

- [ADR-0038](0038-notification-code-bitmap-1-63.md): Notification code bitmap baseline.
- [ADR-0020](0020-p98-fb-times-1-20-mig-profile.md): GPU recommendation context.
- [ADR-0022](0022-timeslicing-replicas-clamp-2-8-majority-rule.md): Replica clamping rules.

## References

- [internal/engine/gpu_timeslicing.go](../../internal/engine/gpu_timeslicing.go)
- [internal/engine/notifications.go](../../internal/engine/notifications.go)
