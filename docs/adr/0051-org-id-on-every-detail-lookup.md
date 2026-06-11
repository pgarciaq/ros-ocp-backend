# ADR-0051: Require org_id on every detail lookup despite deterministic IDs

## Status

Accepted

## Context

UUID v5 from cluster+workload identity is not tenant-scoped; same topology across tenants would IDOR.

## Decision

Every detail query must filter org_id from identity header.

## Alternatives Considered

### Schema-per-tenant (django-tenants style)
Koku uses isolated PostgreSQL schemas per org (`org1234567`), which makes accidental cross-tenant reads impossible at the SQL layer. ROS shares Koku's database but uses a single `public` schema with `org_id` columns—schema-per-tenant would require separate migration pipelines, connection routing, and double the partition-management code already duplicated between Koku and ROS.

### Include org_id in UUID v5 namespace
Embedding org_id in the deterministic ID would make IDs globally unique without query filters, but would break stable URLs when a cluster moves between orgs (rare but supported via Sources events) and expose org identifiers in bookmarkable URLs.

### Rely on gateway JWT validation only
Trusting that only authenticated callers reach ROS ignores IDOR: any user who obtains or guesses a recommendation UUID from another tenant's cluster topology (identical namespace/pod names) could read foreign data if detail queries keyed only on `id`.

## Consequences

IDOR-proof. Slightly more complex query. Can't share recommendation URLs across orgs.

## References

- [internal/model/recommendation_detail_org_scope_test.go](internal/model/recommendation_detail_org_scope_test.go)
