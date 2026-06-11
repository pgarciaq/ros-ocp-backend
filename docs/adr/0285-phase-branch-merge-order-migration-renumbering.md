# ADR-0285: Phase branch merge order and migration renumbering

## Status

Accepted

## Phase

4–6

## Context

Multiple developers work on different phases concurrently. Migrations have sequential numbers enforced by golang-migrate. Merging phases out of order causes migration number collisions and broken deploy ordering.

COST-5691 tracks the migration dependency graph across phases.

## Decision

Sequential phase merges (phase0 → main, then phase1 → main, etc.). Migration renumbering at merge time: phase branch uses temporary high numbers; PR to main renumbers to next available. Tooling: `scripts/renumber-migrations.sh`.

## Alternatives Considered

### Timestamp-based migrations

golang-migrate does not support well.

### Big-bang merge of all phases

Conflict hell and untestable intermediate states.

### Independent migration sequences per phase

Breaks golang-migrate ordering guarantees.

## Consequences

- Merge order is strict (no out-of-order phase merges).
- Renumbering creates commit churn but ensures clean sequential history on main.
- Feature branches must rebase after upstream phase merges.

## Related Decisions

- [ADR-0063](0063-centralized-migrations-with-plugin-headers.md): Centralized migrations.
- [ADR-0236](0236-large-table-index-strategy-concurrently-pre-step.md): Large-table index strategy.
- [ADR-0137](0137-migration-lint-concurrently-template.md): Migration lint.

## References

- [scripts/renumber-migrations.sh](../../scripts/renumber-migrations.sh)
- [migrations/](../../migrations/)
