# ADR-0259: Replace Kruize experiment lifecycle with synchronous ingest-time engine

## Status

Accepted

## Phase

0–1

## Context

The Kruize architecture used an async loop: Kafka message → `createExperiment` API call → `updateResults` with metrics → poll `rosocp.kruize.recommendations` topic → store results in PostgreSQL.

This async loop introduced latency (minutes between ingest and readable recommendations), complexity (poll-retry, experiment state management), and hard coupling to Kruize container availability.

## Decision

Native engine runs synchronously during ingest: Kafka message → digest aggregation → produce recommendations → persist to PostgreSQL. No experiment creation, no polling topic, no external recommendation service dependency.

The ingest goroutine produces recommendations before committing the Kafka offset ([ADR-0089](0089-manual-kafka-commit-after-success.md)).

## Alternatives Considered

### Keep async poll against Kruize topic

Preserves latency and complexity; rejected for production native path.

### Async with callback webhook

Still requires Kruize dependency and callback infrastructure.

### Shadow mode dual-write

Designed but explicitly rejected ([ADR-0262](0262-shadow-mode-native-engine-explicitly-rejected.md)).

## Consequences

- Recommendations available immediately after ingest (seconds vs minutes).
- No dependency on external Kruize container for recommendation generation.
- Removes `rosocp.kruize.recommendations` topic from native architecture.
- Kruize plugin retains legacy path ([ADR-0163](0163-kruize-deprecation-path.md)).

## Related Decisions

- [ADR-0001](0001-native-engine-over-kruize.md): Native engine over Kruize.
- [ADR-0163](0163-kruize-deprecation-path.md): Kruize deprecation path.
- [ADR-0088](0088-kafka-s3-pipeline-both-modes.md): Kafka + S3 pipeline.
- [ADR-0262](0262-shadow-mode-native-engine-explicitly-rejected.md): Shadow mode rejected.

## References

- [internal/processor/ingest.go](../../internal/processor/ingest.go)
- [internal/engine/produce.go](../../internal/engine/produce.go)
