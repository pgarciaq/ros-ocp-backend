# ADR-0029: Use 10% headroom and 70/90% risk bands for quota/CRQ

## Status

Accepted

## Context

Tight-to-used limits ignore burst and scheduling slack.

## Decision

Add 10% headroom to recommended quota; flag risk at 70% (warning) and 90% (critical) utilization.

## Consequences

Leaves scheduling slack. Two-tier alerting for quota pressure.

## References

- [internal/engine/quota_settings.go](internal/engine/quota_settings.go)
