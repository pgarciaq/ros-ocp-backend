# ADR-0211: Parallel settings domains with domain-specific storage shapes

## Status

Accepted

## Context

Settings cover many recommendation types with different parameter shapes. A single generic settings model would either be too broad or too limiting for validation and API ergonomics.

## Decision

Separate API endpoints and storage for each domain:

| Domain | Endpoint pattern | Storage |
|--------|------------------|---------|
| Thresholds | `/settings/thresholds/{type}` | `recommendation_thresholds` |
| Idle detection | `/idle-detection` | `recommendation_thresholds` |
| Quota / CRQ | `/quota`, `/cluster-quota` | `recommendation_thresholds` |
| VM | `/vm` | `recommendation_thresholds` (nested JSON) |
| Snapshot | `/snapshot` | `recommendation_thresholds` |
| Business hours | `/business-hours` | `business_hour_schedules` |
| Terms | `/settings/terms` | `org_recommendation_terms` |

## Alternatives Considered

### Single generic JSONB settings blob

Hard to validate; no type safety at API boundary.

### GraphQL-style nested settings

Over-engineering for current REST clients.

## Consequences

- Adding a new settings domain requires a new handler and validation rules.
- No universal "get all settings" endpoint.
- PUT semantics vary: thresholds merge partial updates; BH PUT replaces schedule; DELETE behavior differs per domain.

## Related Decisions

- [ADR-0208](0208-settings-scope-org-wide-only-except-business-hours.md): Org-wide scope.
- [ADR-0084](0084-three-tier-settings-precedence.md): Precedence model.

## References

- [internal/api/handlers_threshold_settings.go](../../internal/api/handlers_threshold_settings.go)
- [internal/api/handlers_business_hours.go](../../internal/api/handlers_business_hours.go)
