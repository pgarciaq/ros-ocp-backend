# ADR-0066: Use keyset (after cursor) pagination over deep offset

## Status

Accepted

## Context

OFFSET degrades at scale and produces overlapping pages.

## Decision

Keyset pagination with opaque `after` cursor. Cap offset at 10k as interim guard.

## Consequences

Consistent performance at any depth. Requires cursor-aware clients. Legacy offset still works for shallow pages.

## References

- [internal/api/cursor.go](internal/api/cursor.go)
