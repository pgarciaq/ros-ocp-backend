# ADR-0124: Trigger Koku reship_ros to rebuild BH digests from S3

## Status

Accepted

## Context

Re-collecting from cluster for historical schedule changes is impractical.

## Decision

Koku's `reship_ros` re-processes S3 CSVs through ROS with new BH schedule.

## Consequences

Historical BH data rebuilt without operator involvement. Depends on S3 retention.

## References

- [internal/reship/client.go](internal/reship/client.go)
