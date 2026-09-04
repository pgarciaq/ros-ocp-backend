# ADR-0138: Use MkDocs public site separate from internal docs

## Status

Accepted

> **Note:** This ADR documents a straightforward or inherited decision kept for completeness and historical traceability. It does not represent a non-obvious architectural fork.

## Context

Internal docs (phase plans, adversarial audits) shouldn't be customer-visible.

## Decision

Separate `docs-site/` for public MkDocs; `docs/` for internal engineering docs.

## Consequences

Clear public/private boundary. Must sync shared content manually.

## References

- [docs/agents/docs-site-sync.md](../agents/docs-site-sync.md)
