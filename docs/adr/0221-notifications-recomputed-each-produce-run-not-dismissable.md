# ADR-0221: Notifications are recomputed each produce run (not dismissable entities)

## Status

Accepted

## Context

Many systems treat notifications as persistent objects that users can dismiss. ROS-OCP notifications represent current system state derived from recommendation logic, not user-facing alerts with lifecycle.

## Decision

Notification codes are recomputed every produce run and stored as `SMALLINT[]` on `recommendation_sets`. No dismiss API exists.

Codes reflect current state: if a workload is no longer idle, code 5 disappears on next produce. Enrichment (human-readable messages, severity, links) happens at read time via `internal/notifications/mapping.go`.

## Alternatives Considered

### Persistent notification entities

Requires dismiss state, lifecycle management, and garbage collection.

### Hybrid (some dismissable)

Complex dual model with unclear product semantics.

## Consequences

- UI cannot "dismiss" a notification — it reappears on next ingest if still applicable.
- Product requests for dismiss require architectural change.
- Notifications are cheap (no separate table, no state machine).
- Stale notifications disappear naturally when recommendations update.

## Related Decisions

- [ADR-0038](0038-notification-code-bitmap-1-63.md): Notification code bitmap.
- [ADR-0039](0039-notification-codes-smallint-array.md): SMALLINT[] storage.
- [ADR-0222](0222-notification-dual-source-db-seed-and-go-definitions.md): Dual source catalog.

## References

- [internal/notifications/mapping.go](../../internal/notifications/mapping.go)
- [internal/engine/recommend_container.go](../../internal/engine/recommend_container.go)
