# ADR-0092: Use separate ingest statement timeout (120s) via SET LOCAL

## Status

Accepted

## Context

Global 25s statement_timeout kills large batch upserts; removing it exposes API to runaway queries.

## Decision

Ingestion transactions use `SET LOCAL statement_timeout = '120s'`; API keeps 25s default.

## Alternatives Considered

### Raise global statement_timeout to 120s
A cluster-wide 120s timeout would fix ingest batch upserts, but exposes list/detail API endpoints to runaway queries from malformed filters—a single missing index could hold connections for two minutes across all API pods.

### Remove statement_timeout entirely
Unlimited query time lets large ingests complete, but removes the primary defense against accidental full-table scans in ad-hoc SQL or buggy ORM queries; production incidents from unbounded queries are harder to diagnose than timeout errors.

### Split ingest to a dedicated database role with role-level timeout
PostgreSQL role settings could scope timeout by connection user, but ROS uses one pool for both API and processor in many deployments; `SET LOCAL` inside ingest transactions scopes the override to the current transaction without affecting concurrent API queries on the same connection from the pool.

## Consequences

Large ingests complete. API still protected. Session-scoped, no global side effects.

## References

- [internal/db/statement_timeout.go](internal/db/statement_timeout.go)
