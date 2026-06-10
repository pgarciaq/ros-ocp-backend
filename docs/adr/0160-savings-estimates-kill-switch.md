# ADR-0160: Use ROS_SAVINGS_ESTIMATES_ENABLED global kill-switch

## Status

Accepted

## Context

Air-gapped deployments may not have Masu access for cost data.

## Decision

Global switch disables all savings computation when Masu unavailable.

## Consequences

Recommendations work without Masu. No savings displayed. Clean degradation.

## References

- [internal/config/config.go](internal/config/config.go)
