# ADR-0094: Use split transactions above 50k rows per phase

## Status

Accepted

## Context

One multi-hour transaction holds locks and risks timeout.

## Decision

Split into sub-transactions at 50k row boundaries within each ingest phase.

## Alternatives Considered

### Single mega-transaction per ingest phase
One transaction spanning an entire manifest minimizes commit overhead, but multi-hour locks on digest partitions exceed the 120s `statement_timeout`, block concurrent API reads, and risk WAL bloat large enough to exhaust disk on failed rollbacks.

### Per-file transactions (one commit per CSV)
Committing after each operator CSV minimizes lock duration, but a typical manifest contains dozens of files—excessive commit frequency slows ingest 3–5× in benchmarks and leaves cross-file digest state inconsistent mid-manifest.

### No split (rely on chunking alone)
ADR-0093's batch flush (now 2000 statements, increased from 500) bounds statement count but not transaction duration; without a 50k row split in `pipeline.go`, a single open transaction still accumulates row locks until the final batch flush completes.

## Consequences

Shorter lock hold times. Partial progress on failure. Must handle idempotent retries.

## References

- [internal/ingestion/pipeline.go](internal/ingestion/pipeline.go)
