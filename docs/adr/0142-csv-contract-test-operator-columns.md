# ADR-0142: Use CSV contract test tied to operator column headers

## Status

Accepted

## Context

Operator column changes would be discovered only in production ingest failures. The koku-metrics-operator CSV schema evolves across releases; the parser must stay aligned with known column headers (incorporates former ADR-0097).

## Decision

Contract test validates parser expectations against known operator CSV headers in `internal/ingestion/csv_contract_test.go`. The test asserts that expected column names for each report type (`ocp_pod_usage`, `ocp_ros_usage`, etc.) match what the ingestion parser requires.

## Consequences

Breaking operator changes caught pre-merge in CI. Must update the contract test when the operator legitimately adds, renames, or removes columns.

## Related Decisions

- [ADR-0265](0265-operator-csv-column-contract-optional-columns-partial-upgrade.md): Optional columns and partial-upgrade tolerance.
- [ADR-0095](0095-csv-type-longest-prefix-first.md): CSV type detection.

## References

- [internal/ingestion/csv_contract_test.go](../../internal/ingestion/csv_contract_test.go)
