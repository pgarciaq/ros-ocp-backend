# ADR-0068: Use filter[project] as canonical namespace alias

## Status

Accepted

## Context

Inconsistent `namespace` vs `project` parameter names across ROS/Koku confused users.

## Decision

Accept both; canonicalize to `project` in API (matching OCP terminology).

## Consequences

User-friendly. Backward compatible. Internal code normalizes early.

## References

- [openapi.json](openapi.json)
- [CHANGELOG](CHANGELOG)
