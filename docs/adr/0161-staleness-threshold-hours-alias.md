# ADR-0161: Use ROS_STALENESS_THRESHOLD_HOURS=48 with alias

## Status

Accepted

## Context

Inconsistent env names between docs and chart caused configuration confusion.

## Decision

Canonical name with documented alias for backward compatibility.

## Consequences

Both names work. Docs point to canonical. Alias deprecated.

## References

- [CHANGELOG](CHANGELOG)

## Status Update (2026-06)

[ADR-0224](0224-stale-marking-precedence-last-reported-at-overrides-digest-age.md) documents the full staleness precedence rule: `clusters.last_reported_at` within the staleness window overrides individual digest bucket age. This supports reship scenarios where actively-reporting clusters receive historical data.
