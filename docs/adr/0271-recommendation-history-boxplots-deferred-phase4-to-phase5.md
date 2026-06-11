# ADR-0271: Recommendation history and boxplots deferred from phase 4 to phase 5

## Status

Accepted

## Phase

4–5

## Context

Phase 4 scope was OOM detection + quality feedback. History (daily recommendation snapshots) and boxplots (statistical visualization) required partitioned tables and raw sample storage decisions.

Shipping everything in phase 4 would have delayed OOM detection delivery.

## Decision

Explicitly defer history + boxplots to phase 5. Phase 4 ships quality metrics and OOM notification only. Rationale: partitioned history tables require migration complexity; boxplots need sample storage decisions settled first.

## Alternatives Considered

### Ship everything in phase 4

Too large; delays OOM detection delivery.

### Never ship history

Product requirement for trend visibility; rejected.

## Consequences

- Phase 4 delivered faster with focused OOM scope.
- Phase 5 added `recommendation_history_daily` (partitioned) and boxplot endpoints.
- Clear scope boundaries between phases documented in phase notes.

## Related Decisions

- [ADR-0060](0060-separate-recommendation-history.md): Separate recommendation history.
- [ADR-0055](0055-query-time-boxplots-from-samples.md): Query-time boxplots.
- [ADR-0179](0179-stability-score-formula.md): Quality/stability scoring.

## References

- [migrations/000044_recommendation_history_daily.up.sql](../../migrations/000044_recommendation_history_daily.up.sql)
- [internal/api/handlers_boxplot.go](../../internal/api/handlers_boxplot.go)
