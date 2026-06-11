# ADR-0013: Classify idle inline during container produce, not as separate plugin

## Status

Accepted

## Context

Separate idle plugin would require second DB pass and couldn't guarantee ordering before namespace rollup.

## Decision

Idle classification runs inline during container Produce phase.

## Alternatives Considered

### Separate idle-detection plugin
Requires a second DB pass and creates an ordering dependency: namespace rollup must see idle flags before aggregation. Rejected because plugin ordering cannot guarantee produce-before-enrich sequencing without fragile registry priorities.

### Post-hoc batch reclassification
Nightly batch jobs leave recommendations stale until the next run; namespace savings roll up incorrect totals intraday. Rejected because UI and fleet summaries would show active containers that are already idle in live telemetry.

## Related Decisions

- [ADR-0103](0103-phased-execution-produce-enrich-optimize.md): phase ordering requires classification during Produce, before Enrich rollup.
- [ADR-0012](0012-three-state-idle-zombie-active.md): three-state model applied inline here.

## Consequences

Single pass. Guaranteed ordering. Can't disable idle detection independently of container plugin.

## References

- [internal/engine/recommend_all.go](internal/engine/recommend_all.go)
