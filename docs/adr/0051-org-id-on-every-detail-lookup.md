# ADR-0051: Require org_id on every detail lookup despite deterministic IDs

## Status

Accepted

## Context

UUID v5 from cluster+workload identity is not tenant-scoped; same topology across tenants would IDOR.

## Decision

Every detail query must filter org_id from identity header.

## Consequences

IDOR-proof. Slightly more complex query. Can't share recommendation URLs across orgs.

## References

- [internal/model/recommendation_detail_org_scope_test.go](internal/model/recommendation_detail_org_scope_test.go)
