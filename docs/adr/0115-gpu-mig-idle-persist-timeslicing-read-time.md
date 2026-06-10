# ADR-0115: Persist GPU MIG/idle savings; compute time-slicing at read time

## Status

Accepted

## Context

Time-slicing savings depend on live candidate sets that change between ingests.

## Decision

MIG-profile and idle-GPU savings persisted; time-slicing computed on API read.

## Consequences

Persisted savings stable. Time-slicing always fresh. Fleet summary excludes read-time GPU.

## References

- [internal/api/gpu_enrichment.go](internal/api/gpu_enrichment.go)
