# ADR-0053: Split list query: keys table for identity, detail table for rec state

## Status

Accepted

## Context

Single-table pagination dropped filters on re-join for fields like workload_type.

## Decision

Two-phase query: filter/paginate on keys table, then join detail for recommendation data.

## Consequences

Correct filtering. Two queries per list call. Consistent pagination semantics.

## References

- [internal/model/native_list_keys.go](internal/model/native_list_keys.go)
