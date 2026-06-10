# ADR-0106: Use GPU as APIEnricher on container responses

## Status

Accepted

## Context

Separate mandatory second API call for GPU fields on every container view adds latency.

## Decision

GPU plugin registers as APIEnricher; container responses enriched in-process.

## Consequences

Single API call returns GPU data. GPU plugin enriches only when enabled. No extra round-trip.

## References

- [internal/plugins/gpu/plugin.go](internal/plugins/gpu/plugin.go)
