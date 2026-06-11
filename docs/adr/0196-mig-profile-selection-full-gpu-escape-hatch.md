# ADR-0196: MIG profile selection full_gpu escape hatch

## Status

Accepted

## Context

GPU MIG recommendations select the smallest MIG slice that satisfies observed framebuffer usage + 20% headroom ([ADR-0020](0020-p98-fb-times-1-20-mig-profile.md)). Observed usage may exceed the largest available MIG profile in the catalog. Recommending the largest slice in that case is misleading.

## Decision

When observed FB × 1.2 exceeds the largest catalog profile, recommend `"full_gpu"` rather than the largest MIG slice. The engine can also recommend downsizing to the smallest sufficient profile when the current allocation is oversized.

The `"full_gpu"` string is consumed by API responses and potentially by operators for scheduling. It is a sentinel value (not a catalog entry).

**Savings:** `full_gpu` → 0 (no reduction). Downsizing savings = `(current_profile_cost - recommended_profile_cost)`.

## Consequences

- `"full_gpu"` is a sentinel value (not a catalog entry).
- Savings for full_gpu = 0 (no reduction).
- Downsizing savings = (current_profile_cost - recommended_profile_cost).

## Alternatives Considered

### Recommend largest profile always

Misleading when workload needs full GPU. Rejected.

### No recommendation when exceeding catalog

Loses the signal that the workload cannot fit any MIG slice. Rejected.

## Related Decisions

- [ADR-0020](0020-p98-fb-times-1-20-mig-profile.md): P98 FB × 1.20 headroom for MIG profile selection.
- [ADR-0021](0021-exclude-idle-memory-bound-mig-from-timeslicing.md): MIG workload exclusions from time-slicing.

## References

- [internal/engine/vm_mig_optimal.go](../../internal/engine/vm_mig_optimal.go)
