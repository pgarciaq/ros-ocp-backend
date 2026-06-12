# ADR-0289: Defer org metadata refresh to end of reconcile cycle

## Status

Accepted

## Phase

Performance / Ingestion

## Context

Streaming container recommendations ([ADR-0171](0171-streaming-recommendation-batches.md))
write results in batches of 500 (`streamBatchSize`). After each batch,
`WriteRecommendations` previously called `RefreshOrgContainerKeys` and
`RefreshOrgRecommendationStats` — two org-scoped queries that each scan all
non-stale `recommendation_sets` rows for the tenant via `DISTINCT ON`.

For a 10k-container org (~20 batches), that meant **~40 full-org scans** per
reconcile cycle. Both refreshes exist to keep list pagination fast
([ADR-0052](0052-org-container-keys-denormalized-index.md),
[ADR-0189](0189-precomputed-org-recommendation-stats.md)), but running them on
every batch amplified write time without improving correctness: intermediate
batches are not visible to API clients until the cycle completes.

The native engine performance audit (P0-3 / M1) identified this as the single
largest database write-amplification bottleneck for large orgs.

## Decision

Defer org metadata refresh to **once per reconcile cycle**:

1. `WriteRecommendations` persists recommendation rows only — no metadata refresh.
2. New `RefreshOrgMetadata` in `internal/engine/recommend_all.go` runs
   `RefreshOrgContainerKeys`, `RefreshOrgRecommendationStats`, and
   `fleetsummary.InvalidateOrg` in sequence.
3. Call sites invoke `RefreshOrgMetadata` once after all streaming batches finish:
   - `runContainerRecommendations` in `internal/services/report_processor.go` (ingest)
   - `recalculateContainerCluster` in `internal/engine/threshold_recalculate.go` (threshold recalc)
4. Single-batch paths (tests, tooling) use `WriteRecommendationsAndRefreshOrg`, which
   wraps `WriteRecommendations` + `RefreshOrgMetadata`.

`MarkAdopted` still calls `RefreshOrgContainerKeys` immediately — adoption is a
point event, not a multi-batch streaming cycle.

## Alternatives Considered

### Refresh after every batch (status quo)

Correct but wasteful: each batch re-scans the entire org even though list API
consumers only need fresh keys/stats after the cycle completes. Rejected after
audit showed 50–90% of recommendation write time spent on redundant refreshes.

### Refresh only `org_recommendation_stats`, skip keys per batch

Halves scan count but still leaves N redundant stats recomputations. Partial fix.

### Incremental delta refresh per batch

Upsert keys for containers in the batch without full-org `DISTINCT ON`. More
complex (must handle deletes, stale transitions, cross-batch ordering) for
marginal gain when a single end-of-cycle refresh is cheap enough.

### No refresh until API request (lazy)

Pushes cost to first list query after ingest; violates pre-computed stats design
([ADR-0189](0189-precomputed-org-recommendation-stats.md)) and causes slow
post-ingest dashboard loads.

## Consequences

- **Performance:** 50–90% reduction in recommendation write time for orgs with
  10k+ containers (one org scan pair instead of ~2× batch count).
- **Staleness during cycle:** `org_container_keys` and `org_recommendation_stats`
  reflect the previous cycle until `RefreshOrgMetadata` completes. Acceptable
  because recommendations are written in bulk and API queries only need current
  metadata after reconciliation finishes; concurrent list requests during an
  active ingest may briefly show pre-cycle counts/keys.
- **Failure semantics:** If `RefreshOrgMetadata` fails after all batches succeed,
  recommendations are persisted but keys/stats are stale until the next successful
  refresh (ingest retry or manual recalc). The error is fatal to the reconcile
  step so the failure is visible in logs/metrics.
- **Fleet cache:** `fleetsummary.InvalidateOrg` moves from per-batch to
  end-of-cycle, reducing cache churn during streaming.

## Related Decisions

- [ADR-0171](0171-streaming-recommendation-batches.md): Streaming batch size and write pattern.
- [ADR-0189](0189-precomputed-org-recommendation-stats.md): Pre-computed list counts.
- [ADR-0255](0255-org-container-keys-refresh-deletes-stale.md): Keys refresh semantics on ingest.
- [ADR-0052](0052-org-container-keys-denormalized-index.md): Denormalized keys table.

## References

- [internal/engine/recommend_all.go](../../internal/engine/recommend_all.go) — `RefreshOrgMetadata`, `WriteRecommendationsAndRefreshOrg`
- [internal/services/report_processor.go](../../internal/services/report_processor.go) — `runContainerRecommendations`
- [internal/engine/threshold_recalculate.go](../../internal/engine/threshold_recalculate.go) — `recalculateContainerCluster`
- [internal/model/org_container_keys.go](../../internal/model/org_container_keys.go)
- [internal/model/org_recommendation_stats.go](../../internal/model/org_recommendation_stats.go)
- [docs/performance/native-engine-audit-2026-06.md](../performance/native-engine-audit-2026-06.md) — P0-3 / M1
