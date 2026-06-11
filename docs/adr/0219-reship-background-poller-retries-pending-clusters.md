# ADR-0219: Reship background poller retries pending clusters

## Status

Accepted

## Context

Masu reship calls can fail transiently. Pending clusters must be retried until successful or max retries exhausted. The trigger guard alone cannot recover clusters stuck after a failed batch.

## Decision

Background poller (`ReshipPollerIntervalSecs`) scans for pending-marker stubs older than threshold. Retries up to `ReshipMaxRetries` times with metrics on exhaustion. Clears pending status on success to avoid triple Masu calls.

Poller is independent of the trigger guard.

## Alternatives Considered

### No retry

Stuck clusters never reship; BH digests remain incomplete.

### Exponential backoff per cluster

Complex for a periodic background sweep.

### Event-driven retry

Requires reliable event source from Masu completion.

## Consequences

- Stuck pending state eventually surfaces via metrics/alerting.
- Poller adds periodic DB queries.
- Max retries prevent infinite retry loops on permanently broken clusters.

## Related Decisions

- [ADR-0218](0218-org-level-reship-single-flight-trailing-batch-coalescing.md): Trigger guard.
- [ADR-0124](0124-koku-reship-ros-rebuild-bh.md): Koku reship integration.
- [ADR-0126](0126-forward-only-fallback-reship-failure.md): Forward-only fallback after max retries.

## References

- [internal/reship/poller.go](../../internal/reship/poller.go)
