# ADR-0046: Use BIGINT for all metric columns end-to-end

## Status

Accepted

## Context

Mixed INT/BIGINT caused driver narrowing bugs and two mental models.

## Decision

Uniform BIGINT. Column suffixes convey units (_mc, _kib, _bps), not SQL types.

## Consequences

Consistent. No narrowing. Slightly more storage than INT for small values.

## References

- [docs/architecture/database-conventions.md](docs/architecture/database-conventions.md)
