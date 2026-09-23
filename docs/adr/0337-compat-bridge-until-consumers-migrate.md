# ADR-0337: compat bridge — synthesize Kruize-shaped serving until consumers migrate

## Status

Accepted

## Phase

Kruize decommission ([#65](https://github.com/pgarciaq/ros-ocp-backend/issues/65))

## Context

Kruize decommissions Q3 2027, gated on SaaS native-engine deployment.
Three consumers parse the Kruize (compat) response shape: koku-ui-ros
legacy paths, the RHDH cost-management plugin (generated client, ~4-month
ship train with old/new coexistence), and IQE suites (one of which
hard-asserts `recommendation_terms` + non-empty `plots_data`).

On native-written data the compat path serves hollow `{}` recs and
sextuples every container (63 containers → 378 rows live), because the
native writer upserts six typed (term × engine) rows with no JSON blob
while compat reads one blob per container
([#599](https://github.com/pgarciaq/ros-ocp-backend/issues/599)).

Two options were evaluated: synthesize the Kruize shape at read time
(option 2, backend-only) or migrate every consumer to native shapes
(option 3, cross-repo). RHDH's ship train decides the economics: the
backend must serve the Kruize shape for 4+ months regardless, so a
translator with that lifespan is a bridge, not throwaway — while option 3
alone leaves all current consumers on hollow rows for months.

## Decision

Build the bridge now (backend-only, containers-first with namespace on
consumer proof — [#599](https://github.com/pgarciaq/ros-ocp-backend/issues/599)):
collapse to one row per container carrying a full 3-term × 2-engine blob
on the short/cost row (legacy-exact counts), synthesize `current` +
per-term `config` from sibling typed rows, notifications via the
established `MapToKruizeFormat` precedent, variation via stored-pcts
injection, sibling batch fetch (no N+1). Migrate consumers at their own
cadence in parallel (koku-ui phases, RHDH regeneration after native
`openapi.json` schemas land). Delete the translator when the last old
consumer ages out ([#604](https://github.com/pgarciaq/ros-ocp-backend/issues/604)).

Plots are omitted everywhere (true box-plots unrecoverable from
percentile-band digests; fabricated quartiles would render as zeros).
No writer/blob-backfill changes. No consumer changes in the bridge.

## Consequences

### Positive

- All current consumers work against native data immediately, with zero
  coordination and legacy-exact counts.
- Consumer migrations proceed without a pain window pressuring them.
- Deletion clock is explicit (last-old-RHDH expiry), not drift.

### Negative

- Translator maintenance until deletion (bounded, single package).
- Fidelity gaps vs Kruize-origin values (duration computation, unit
  round-trips, no plots) — documented, not hidden.

### Neutral

- Count semantics change 6×→1× needs UI pagination re-verification
  (legacy-correct, but verify).
- URL collapse ([`#603`](https://github.com/pgarciaq/ros-ocp-backend/issues/603))
  is independent and follows consumer migration.

## References

- [#599](https://github.com/pgarciaq/ros-ocp-backend/issues/599) (options, work lists, tracking)
- [#65](https://github.com/pgarciaq/ros-ocp-backend/issues/65) (Issue 1: engine-presence removal)
- [#604](https://github.com/pgarciaq/ros-ocp-backend/issues/604) (Issue 2: compat removal)
- [Legacy-to-Native Engine Migration Guide](../../docs-site/architecture/native-migration.md)
