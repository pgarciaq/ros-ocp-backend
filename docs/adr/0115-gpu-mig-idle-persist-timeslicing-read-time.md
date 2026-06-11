# ADR-0115: Persist GPU MIG/idle savings; compute time-slicing at read time

## Status

Accepted

## Context

Time-slicing savings depend on live candidate sets that change between ingests.

## Decision

MIG-profile and idle-GPU savings persisted; time-slicing computed on API read.

## Alternatives Considered

### Persist all GPU savings including time-slicing at ingest
Writing time-slicing savings during ingest would make fleet totals cheap to aggregate, but candidate sets (which GPUs are idle, memory-bound, or already MIG-partitioned) change between ingests—persisted values go stale within hours and misreport savings until the next full cluster ingest.

### Compute all GPU enrichment at read time
Evaluating MIG profiles, idle detection, and time-slicing on every list/detail request keeps data fresh but multiplies CPU cost on fleet-wide GPU list endpoints; persisting stable MIG/idle results while computing volatile time-slicing splits the cost/freshness trade-off appropriately.

### Separate GPU savings API excluded from main list
A dedicated `/gpu/savings` endpoint would isolate read-time cost, but the UI expects GPU rows inline with container recommendations; excluding time-slicing from fleet summary totals (ADR-0071) achieves the same accounting without a parallel API surface.

## Consequences

Persisted savings stable. Time-slicing always fresh. Fleet summary excludes read-time GPU.

## References

- [internal/api/gpu_enrichment.go](internal/api/gpu_enrichment.go)
