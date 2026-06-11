# ADR-0134: Use PostgreSQL 16 target

## Status

Accepted

> **Note:** This ADR documents a straightforward or inherited decision kept for completeness and historical traceability. It does not represent a non-obvious architectural fork.

## Context

PG13 is EOL; PG17 not yet Red Hat certified for OCP.

## Decision

Target PG16 for all development and testing.

## Consequences

Access to PG16 features (better partitioning, MERGE). Must test against 16 specifically.

## References

- [docs/archive/requirements.md](docs/archive/requirements.md)
