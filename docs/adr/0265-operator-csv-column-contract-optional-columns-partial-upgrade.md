# ADR-0265: Operator CSV column contract — optional columns and partial-upgrade tolerance

## Status

Accepted

## Phase

4–11

## Context

The koku-metrics-operator evolves independently of ros-ocp-backend. New operator versions add CSV columns (GPU metrics, quota `used`, VM fields). ROS must handle both old and new operator versions simultaneously within the same org during rolling cluster upgrades.

Strict schema validation would fail ingest for clusters running older operator versions.

## Decision

CSV parsing treats new columns as optional — missing columns get zero/nil defaults. Feature detection via column presence (e.g., no `quota_used` columns → skip quota recommendations for that cluster).

Contract tests (`internal/services/csv_contract_test.go`) validate minimum required columns. Nise test data generation must match current operator output.

## Alternatives Considered

### Strict schema validation

Breaks on operator version skew within one org.

### Versioned CSV format header

Over-engineering for the change frequency.

### Protobuf binary format

Breaks existing operator tar.gz pipeline.

## Consequences

- Partial-cluster upgrades work (some clusters old operator, some new).
- Missing columns degrade gracefully (features disabled, not errors).
- Adding required columns is a breaking change requiring coordinated operator+backend release.
- Contract tests gate CI against operator header drift.

## Related Decisions

- [ADR-0142](0142-csv-contract-test-operator-columns.md): CSV contract tests.
- [ADR-0269](0269-testcontainers-over-docker-compose-test-isolation.md): Test infrastructure.
- [ADR-0088](0088-kafka-s3-pipeline-both-modes.md): Kafka ingest pipeline.
- [ADR-0287](0287-operator-14-day-prometheus-lookback-integration-boundary.md): Operator lookback boundary.

## References

- [internal/services/csv_contract_test.go](../../internal/services/csv_contract_test.go)
- [internal/services/csv_parser.go](../../internal/services/csv_parser.go)
