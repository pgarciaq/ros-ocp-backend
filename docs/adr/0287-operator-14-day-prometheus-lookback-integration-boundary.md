# ADR-0287: Operator 14-day Prometheus lookback as integration boundary

## Status

Accepted

## Phase

Cross-repo (koku-metrics-operator)

## Context

The koku-metrics-operator queries Prometheus with a 14-day lookback window by default. ROS-OCP processes whatever data arrives — it does not control the lookback. This 14-day boundary affects confidence scoring and idle detection minimum observation ([ADR-0173](0173-tenant-configurable-idle-detection.md)).

Changing operator lookback changes recommendation quality without ROS code changes.

## Decision

ROS-OCP accepts the operator's lookback as a given integration boundary. Idle detection minimum observation days (14) aligns with operator default. Confidence ([ADR-0178](0178-confidence-score-linear-ramp.md)) ramps linearly over the data window. ROS does not request specific lookback from the operator.

## Alternatives Considered

### ROS requests specific window from operator

Coupling; requires operator API change.

### ROS accumulates across uploads beyond operator window

Digests persist across uploads, but initial window still limited by operator lookback.

## Consequences

- Changing operator lookback changes recommendation quality without ROS code changes.
- 14-day idle minimum matches operator default (not coincidence).
- Clusters with shorter Prometheus retention get fewer data days → lower confidence → low-confidence notification.

## Related Decisions

- [ADR-0173](0173-tenant-configurable-idle-detection.md): Idle min observation days.
- [ADR-0178](0178-confidence-score-linear-ramp.md): Confidence linear ramp.
- [ADR-0265](0265-operator-csv-column-contract-optional-columns-partial-upgrade.md): Operator CSV contract.

## References

- [koku-metrics-operator: internal/collector/collector.go](https://github.com/project-koku/koku-metrics-operator)
- [internal/engine/confidence.go](../../internal/engine/confidence.go)
