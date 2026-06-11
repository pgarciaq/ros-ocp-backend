# ADR-0066: Use keyset (after cursor) pagination over deep offset

## Status

Accepted

## Context

OFFSET degrades at scale and produces overlapping pages.

## Decision

Keyset pagination with opaque `after` cursor. Cap offset at 10k as interim guard.

Cursors are **base64url-encoded JSON** of sort key values — opaque to clients but debuggable by operators; not encrypted because sort keys contain no secret data (incorporates former ADR-0067). The server validates decoded cursor tuples on every request.

## Alternatives Considered

### Offset/limit pagination only
Standard `OFFSET`/`LIMIT` is simpler for clients and works for shallow pages, but PostgreSQL must scan and discard all skipped rows—list queries against `org_container_keys` with 200k+ rows per org degrade linearly and produce duplicate/missing rows when data mutates between page fetches.

### Cursor pagination on primary key alone
A monotonic `id` cursor avoids deep offsets but breaks when users sort by cost, namespace, or notification codes; the list API exposes multiple sort orders, so the cursor must encode the full sort key tuple used in `internal/api/cursor.go`.

### Search-after with Elasticsearch
Offloading list queries to Elasticsearch would scale full-text and faceted search, but adds a third datastore to operate in on-prem deployments, duplicates denormalized recommendation state, and is unnecessary when a purpose-built `org_container_keys` index table already satisfies filter/sort/paginate in PostgreSQL.

### Encrypted or signed cursors
Tamper-proof cursors add crypto overhead; server-side validation of sort keys is sufficient because cursors carry no authorization data.

## Consequences

Consistent performance at any depth. Requires cursor-aware clients. Legacy offset still works for shallow pages. Cursors are not tamper-proof (acceptable — server validates).

## Related Decisions

- [ADR-0190](0190-keyset-cursor-tie-breaker-tuples-per-resource-type.md): Tie-breaker tuples per resource type.
- [ADR-0250](0250-pagination-meta-contract.md): Pagination meta contract.

## References

- [internal/api/cursor.go](../../internal/api/cursor.go)
