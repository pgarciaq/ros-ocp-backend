# ADR-0222: Notification dual source of truth — DB seed and Go definitions map

## Status

Accepted

## Context

Notification codes need both a DB-seeded catalog for API discovery endpoints and Go constants for compile-time evaluation in engine code. A single source cannot serve both runtime catalog and compile-time safety.

## Decision

- `notification_code_definitions` table (migration-seeded) provides the API catalog.
- `internal/notifications/mapping.go` defines the Go `Definitions` map with messages, severity, and plugin association.
- A contract test enforces sync between the two.

New codes require both a migration INSERT and a Go map entry.

## Alternatives Considered

### DB-only

Cannot use codes as compile-time constants in engine.

### Go-only

Requires code deploy for catalog changes; no SQL-backed discovery.

### Code generation from DB

Adds build complexity and migration ordering constraints.

## Consequences

- Adding a notification code is a two-file change plus migration.
- Contract test catches drift between DB and Go.
- DB catalog is source for `GET /notification-codes`; Go map is source for engine evaluation and read-time enrichment.

## Related Decisions

- [ADR-0038](0038-notification-code-bitmap-1-63.md): Code numbering.
- [ADR-0077](0077-notification-codes-catalog-endpoint.md): Catalog endpoint.
- [ADR-0223](0223-plugin-filtered-notification-catalog-subsets.md): Plugin-filtered subsets.

## References

- [internal/notifications/mapping.go](../../internal/notifications/mapping.go)
- [internal/notifications/contract_test.go](../../internal/notifications/contract_test.go)
