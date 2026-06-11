# ADR-0066: Use keyset (after cursor) pagination over deep offset

## Status

Accepted

## Context

OFFSET degrades at scale and produces overlapping pages.

## Decision

Keyset pagination with opaque `after` cursor. Cap offset at 10k as interim guard.

## Alternatives Considered

### Offset/limit pagination only
Standard `OFFSET`/`LIMIT` is simpler for clients and works for shallow pages, but PostgreSQL must scan and discard all skipped rows—list queries against `org_container_keys` with 200k+ rows per org degrade linearly and produce duplicate/missing rows when data mutates between page fetches.

### Cursor pagination on primary key alone
A monotonic `id` cursor avoids deep offsets but breaks when users sort by cost, namespace, or notification codes; the list API exposes multiple sort orders, so the cursor must encode the full sort key tuple used in `internal/api/cursor.go`.

### Search-after with Elasticsearch
Offloading list queries to Elasticsearch would scale full-text and faceted search, but adds a third datastore to operate in on-prem deployments, duplicates denormalized recommendation state, and is unnecessary when a purpose-built `org_container_keys` index table already satisfies filter/sort/paginate in PostgreSQL.

## Consequences

Consistent performance at any depth. Requires cursor-aware clients. Legacy offset still works for shallow pages.

## References

- [internal/api/cursor.go](internal/api/cursor.go)
