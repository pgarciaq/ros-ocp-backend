# ADR-0124: Trigger Koku reship_ros to rebuild BH digests from S3

## Status

Accepted

## Context

Re-collecting from cluster for historical schedule changes is impractical.

## Decision

Koku's `reship_ros` re-processes S3 CSVs through ROS with new BH schedule.

## Alternatives Considered

### Re-query Prometheus/operator for historical metrics
Re-collecting raw metrics from the cluster for past dates is impossible—Prometheus retention (typically 15–30 days) cannot reconstruct months of hourly samples needed to recompute business-hours digests after a schedule change.

### Store dual digest streams from day one without reship
Maintaining `all_hours` and `business_hours` digests during initial ingest avoids backfill, but customers frequently change BH schedules after deployment; without reship, historical BH windows would permanently reflect the old schedule unless every schedule edit triggered full forward re-ingest from the operator.

### ROS-local S3 re-read without Koku orchestration
ROS could fetch CSVs directly from S3 using stored object keys, but Koku owns manifest metadata, S3 credentials, and the `reship_ros` Masu endpoint that coordinates partition-safe re-processing across the existing Kafka pipeline—duplicating that orchestration in ROS would fork the ingestion contract.

## Consequences

Historical BH data rebuilt without operator involvement. Depends on S3 retention.

## References

- [internal/reship/client.go](internal/reship/client.go)
