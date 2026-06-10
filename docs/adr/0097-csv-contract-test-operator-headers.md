# ADR-0097: Use CSV contract test against operator headers

## Status

Accepted

## Context

Operator column changes would silently break parser without early detection.

## Decision

Contract test validates parser against known operator CSV column headers.

## Consequences

Breaking changes caught in CI. Must update test when operator legitimately changes columns.

## References

- [internal/ingestion/csv_contract_test.go](internal/ingestion/csv_contract_test.go)
