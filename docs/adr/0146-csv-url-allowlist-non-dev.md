# ADR-0146: Require explicit CSV URL allowlist in non-dev

## Status

Accepted

## Context

Open fetch-any-URL from Kafka metadata is unsafe.

## Decision

CSV download URLs must match configured allowlist domains/prefixes.

## Alternatives Considered

### Trust Kafka message source (platform-ingested URLs only)
Assuming all URLs originate from Koku's trusted S3 presigning pipeline ignores compromised upstream publishers or misconfigured test fixtures that inject arbitrary URLs into `hccm.ros.events` topic metadata.

### Deny all external URLs; require ROS-local S3 credentials
ROS fetching directly from S3 with its own IAM role eliminates URL injection, but duplicates credential management already centralized in Koku/Masu and breaks the existing presigned-URL contract that keeps ROS stateless regarding cloud credentials.

### Wildcard allowlist (`*.amazonaws.com`)
Broad domain wildcards reduce configuration toil, but permit presigned URLs pointing at attacker-controlled buckets in the same DNS suffix; explicit host allowlist (`ROS_CSV_ALLOWED_HOSTS`) ties fetches to known MinIO/S3 endpoints per deployment.

## Consequences

Only known S3 endpoints allowed. Must configure allowlist per deployment.

## References

- [internal/utils/csv_security.go](internal/utils/csv_security.go)
