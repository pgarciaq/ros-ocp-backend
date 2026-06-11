# ADR-0212: Threshold validation via allowlist and cross-field rules

## Status

Accepted

## Context

Settings PUT bodies must be validated to prevent invalid configurations — for example, `min_margin > max_margin` or percentile targets outside 0–100. Unvalidated settings would produce nonsensical recommendations.

## Decision

PUT validation in `threshold_settings_validation.go`:

1. Unknown keys rejected (allowlist per domain).
2. Cross-field rules enforced (e.g., `cost_percentile ≤ perf_percentile`, headroom bounds).
3. Partial PUT merges submitted fields with current resolved values before validation.

Invalid requests return 400 with field-specific error messages.

## Alternatives Considered

### Accept any JSON

Garbage in, garbage out — unacceptable for production recommendations.

### Full PUT only (no merge)

Poor UX for single-field changes.

## Consequences

- Adding new settings fields requires allowlist update.
- Validation runs against merged state, not just submitted delta.
- 400 responses may reference fields the user did not submit (merge + cross-field check).

## Related Decisions

- [ADR-0208](0208-settings-scope-org-wide-only-except-business-hours.md): Settings scope.
- [ADR-0169](0169-allowlisted-native-sql-query-fragments.md): Allowlist pattern elsewhere.

## References

- [internal/api/threshold_settings_validation.go](../../internal/api/threshold_settings_validation.go)
