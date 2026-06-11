# ADR-0174: Fleet summary counts idle via notification codes, not idle_state column

## Status

Accepted

## Context

Fleet summary must count active, idle, and abandoned containers across an org. The `idle_state` column is set by the authoritative classifier ([ADR-0172](0172-dual-path-idle-classification.md)) but not all containers have been through that path—legacy inline classification may omit or differ on `idle_state`.

Notification codes are always present on recommendation rows ([ADR-0038](0038-notification-code-bitmap-1-63.md), [ADR-0039](0039-notification-codes-smallint-array.md)).

## Decision

`GET /fleet-summary` classifies containers using notification code membership:

- Idle: `notification_codes @> ARRAY[5]`
- Zombie / abandoned: `notification_codes @> ARRAY[8]`

Do not use the `idle_state` column for fleet counts during the dual-path migration.

## Alternatives Considered

### Use idle_state column

Inconsistent counts for partially-classified clusters where legacy path did not populate `idle_state`.

### Dual-count and reconcile

Adds complexity for a temporary migration state without improving user-facing accuracy.

## Consequences

- Fleet counts may diverge from `idle_state`-based queries until all containers are authoritatively classified.
- Once migration completes, counts will converge; an explicit TODO remains to migrate fleet counting to `idle_state`.
- UI and API consumers must not assume fleet idle counts match raw `idle_state` aggregates today.

## Related Decisions

- [ADR-0172](0172-dual-path-idle-classification.md): Dual-path idle classification.
- [ADR-0038](0038-notification-code-bitmap-1-63.md): Notification code semantics.

## References

- [internal/api/handlers_fleet.go](../../internal/api/handlers_fleet.go)
- [internal/model/idle_api.go](../../internal/model/idle_api.go)
- [internal/engine/detect_idle.go](../../internal/engine/detect_idle.go)
