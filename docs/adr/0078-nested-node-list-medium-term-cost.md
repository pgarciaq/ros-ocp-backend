# ADR-0078: Use nested node list with medium-term cost row for shared classification

## Status

Accepted

## Context

Flattening six rows per node (3 terms × 2 engines) into six API objects is confusing.

## Decision

Nest node recommendations; use medium-term cost row for top-level classification display.

## Consequences

Cleaner API. Medium-term cost as default view. Client can drill into other terms.

## References

- [internal/api/handlers_node_recs.go](internal/api/handlers_node_recs.go)
