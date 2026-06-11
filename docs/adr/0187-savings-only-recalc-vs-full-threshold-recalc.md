# ADR-0187: Savings-only recalc vs full threshold recalc

## Status

Accepted

## Context

Two events trigger recalculation with different scope:

- **Threshold/settings changes** require full engine re-run (sizing algorithms, idle classification)
- **Cost model changes from Koku** require only savings recomputation from existing recommendations

Running full engine recalc on every rate update would delay webhook acknowledgment and waste compute.

## Decision

Provide separate entry points:

| Trigger | Path | Scope |
|---------|------|-------|
| Settings / threshold change | Full threshold recalc ([ADR-0086](0086-single-flight-threshold-recalc.md)) | Re-runs sizing and idle paths |
| Koku cost model update | `POST /internal/recalculate-savings` (SA auth, 202 async) | Re-applies persisted savings using new Masu rates only |

Savings recalc uses org-level single-flight with trailing-params pattern ([ADR-0125](0125-single-flight-trailing-reship.md)). Fleet and savings caches invalidate after both recalc types.

## Alternatives Considered

### Always full recalc on rate change

Wasteful when recommendations are unchanged—only dollar values differ.

### Synchronous savings recalc in Koku webhook

Blocks Masu callback response and risks timeout under load.

## Consequences

- Cost model updates complete in seconds, not minutes.
- Savings recalc does not change recommendation targets—only `estimated_savings_cents` / waste columns.
- Operators must distinguish missing recs (threshold) vs stale dollars (savings recalc) when debugging.

## Related Decisions

- [ADR-0086](0086-single-flight-threshold-recalc.md): Full threshold recalc coalescing.
- [ADR-0125](0125-single-flight-trailing-reship.md): Trailing-params single-flight pattern.
- [ADR-0182](0182-monthly-savings-730-hours.md): Rate derivation for savings.

## References

- [internal/api/handlers_savings_recalculate.go](../../internal/api/handlers_savings_recalculate.go)
- [internal/engine/savings_recalc_guard.go](../../internal/engine/savings_recalc_guard.go)
- [internal/engine/savings_recalculate.go](../../internal/engine/savings_recalculate.go)
