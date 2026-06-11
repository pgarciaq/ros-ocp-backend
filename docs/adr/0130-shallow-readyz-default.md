# ADR-0130: Use shallow /readyz by default; optional deep checks

## Status

Accepted

## Context

Deep dependency checks caused unnecessary pod restarts on transient Kafka blips.

## Decision

Default readiness = DB ping. Opt-in `ROS_READINESS_CHECK_KAFKA`/`ROS_READINESS_CHECK_S3`.

## Alternatives Considered

### Deep readyz checking Kafka + DB + S3
Transient Kafka blips caused restart loops—Kubernetes removed pods from service during brief broker unavailability even though the process could still serve cached API responses.

### No readyz endpoint
Kubernetes cannot health-check; rolling updates rely on liveness only and may route traffic to starting pods.

## Consequences

Stable pod lifecycle by default. Deep checks available for strict environments. Kafka/DB failures surface via metrics and logs, not automatic pod restarts—monitor `rosocp_kafka_consumer_lag` and DB connection errors separately.

## References

- [internal/health/readyz.go](internal/health/readyz.go)
