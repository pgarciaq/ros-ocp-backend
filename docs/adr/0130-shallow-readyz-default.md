# ADR-0130: Use shallow /readyz by default; optional deep checks

## Status

Accepted

## Context

Deep dependency checks caused unnecessary pod restarts on transient Kafka blips.

## Decision

Default readiness = DB ping. Opt-in `ROS_READINESS_CHECK_KAFKA`/`ROS_READINESS_CHECK_S3`.

## Consequences

Stable pod lifecycle by default. Deep checks available for strict environments.

## References

- [internal/health/readyz.go](internal/health/readyz.go)
