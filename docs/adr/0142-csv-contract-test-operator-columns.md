# ADR-0142: Use CSV contract test tied to operator column headers

## Status

Accepted

## Context

Operator column changes would be discovered only in production ingest failures.

## Decision

Contract test validates parser expectations against known operator CSV headers.

## Consequences

Breaking operator changes caught pre-merge. Must update when operator legitimately evolves.

## References

- [internal/ingestion/csv_contract_test.go](internal/ingestion/csv_contract_test.go)
