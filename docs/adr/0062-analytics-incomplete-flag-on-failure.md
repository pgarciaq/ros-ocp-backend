# ADR-0062: Mark clusters analytics_incomplete when history/quality fails

## Status

Accepted

## Context

Silent partial success hides data-quality gaps from API consumers.

## Decision

Set `clusters.analytics_incomplete` flag and timestamp on history/quality write failures.

## Alternatives Considered

### Fail-closed ingest (abort entire manifest on analytics failure)
Rolling back recommendations when history/quality writes fail preserves consistency, but a transient PG blip would discard successfully computed sizing for thousands of containers—operators preferred serving stale recommendations over a total ingest failure.

### Silent partial success (no flag, no error surfacing)
Continuing without marking degradation keeps the API green, but users would see quality scores and adoption metrics computed on incomplete history, producing confident-looking recommendations built on missing data.

### Retry-until-success with blocking ingest
Infinite retry on analytics failure eventually succeeds, but blocks the Kafka consumer partition for minutes, triggering consumer lag alerts and delaying downstream tag sync; the flag surfaces degradation immediately while recommendations remain available.

## Consequences

API consumers see degraded state. Operational alerting possible. Recommendations still served.

## References

- [internal/engine/cluster_analytics.go](internal/engine/cluster_analytics.go)
