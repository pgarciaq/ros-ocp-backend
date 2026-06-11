# ADR-0078: Use nested node list with medium-term cost row for shared classification

## Status

Accepted

## Context

Flattening six rows per node (3 terms × 2 engines) into six API objects is confusing.

## Decision

Nest node recommendations; use medium-term cost row for top-level classification display.

## Alternatives Considered

### Flat six-row-per-node list
Three terms × two engines = six API objects per node; pagination and sorting become unusable at fleet scale.

### Single row with nested JSON for all terms
Cannot sort or filter list queries by medium-term savings—clients must fetch all rows and sort in memory.

### Separate endpoints per term
Triple API round-trips to assemble one node view; koku-ui overview pages would fan out excessively.

## Consequences

Cleaner API. Medium-term cost as default view. Client can drill into other terms.

## References

- [internal/api/handlers_node_recs.go](internal/api/handlers_node_recs.go)
