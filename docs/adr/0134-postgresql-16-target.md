# ADR-0134: Use PostgreSQL 16 target

## Status

Accepted

## Context

PG13 is EOL; PG17 not yet Red Hat certified for OCP.

## Decision

Target PG16 for all development and testing.

## Consequences

Access to PG16 features (better partitioning, MERGE). Must test against 16 specifically.

## References

- [docs/archive/requirements.md](docs/archive/requirements.md)
