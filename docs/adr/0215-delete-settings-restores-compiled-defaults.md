# ADR-0215: DELETE settings restores compiled defaults (not empty/disabled)

## Status

Accepted

## Context

Settings DELETE must have clear semantics. Operators need to know whether DELETE disables a feature or resets configuration to platform defaults.

## Decision

DELETE on threshold, quota, snapshot, idle, and VM settings removes the DB row. Effective config reverts to compiled defaults plus env locks. The feature remains active with default values.

Business hours DELETE has different semantics: removes scoped schedule rows (org/cluster/namespace), potentially disabling BH for that scope.

## Alternatives Considered

### DELETE disables feature

Conflates configuration reset with feature toggling (plugins control enablement).

### PUT with explicit "disabled" flag

Cleaner semantics but more verbose for operators.

## Consequences

- DELETE ≠ "disable feature" for thresholds.
- Cannot "turn off" quota recommendations via DELETE — must disable the plugin.
- BH DELETE at org scope effectively disables BH for the org.

## Related Decisions

- [ADR-0208](0208-settings-scope-org-wide-only-except-business-hours.md): Org-wide scope.
- [ADR-0211](0211-parallel-settings-domains-domain-specific-storage-shapes.md): Domain-specific DELETE behavior.

## References

- [internal/api/handlers_threshold_settings.go](../../internal/api/handlers_threshold_settings.go)
- [internal/api/handlers_business_hours.go](../../internal/api/handlers_business_hours.go)
