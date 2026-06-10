# ADR-0143: Use dry-run SQL tests asserting org_id on detail queries

## Status

Accepted

## Context

IDOR regression possible after optimization removes filters.

## Decision

Test asserts every native detail query includes org_id filter in generated SQL.

## Consequences

IDOR prevention verified automatically. Catches accidental filter removal.

## References

- [internal/model/recommendation_detail_org_scope_test.go](internal/model/recommendation_detail_org_scope_test.go)
