# ADR-0197: VM sub-features — guest-agent confidence, power schedule, storage tiering

## Status

Accepted

## Context

VM recommendations go beyond CPU/memory sizing. Three VM-specific subsystems provide additional optimization signals: guest-agent data quality, idle power scheduling, and storage I/O patterns.

## Decision

Three VM sub-features:

### Confidence

High/moderate/low from guest-agent sample ratio (samples/hour) + minimum samples/day threshold. Without guest agent, confidence is `"none"`. Independent of container confidence model ([ADR-0178](0178-container-confidence-data-days-over-window.md)).

### Power schedule (code 64)

≥70% idle days with some active days → schedule power-off candidate. Pure zombie (100% idle) is not a power-schedule candidate (it's a terminate candidate). Notification only — not a sizing change.

### Storage tiering (codes 67–69)

Notification-only I/O pattern hints (random vs sequential, throughput tiers). No savings computation in v1 — informational guidance only.

## Consequences

- VM confidence is independent of container confidence model.
- Power schedule is a notification, not a sizing change.
- Storage tiering is advisory-only in v1 (no dollar impact).

## Alternatives Considered

### Unified confidence model

Doesn't capture guest-agent data quality differences. Rejected.

### Auto-shutdown

Too risky without human confirmation. Rejected.

### Storage savings

Requires pricing model not yet available. Deferred.

## Related Decisions

- [ADR-0033](0033-vm-p95-p99-whole-units-downsize-hysteresis.md): VM CPU/memory sizing baseline.
- [ADR-0178](0178-container-confidence-data-days-over-window.md): Container confidence variant.

## References

- [internal/engine/vm_recommender.go](../../internal/engine/vm_recommender.go)
- [internal/engine/vm_power_schedule.go](../../internal/engine/vm_power_schedule.go)
- [internal/engine/vm_storage_tiering.go](../../internal/engine/vm_storage_tiering.go)
