# ADR-0138: Use MkDocs public site separate from internal docs

## Status

Accepted

## Context

Internal docs (phase plans, adversarial audits) shouldn't be customer-visible.

## Decision

Separate `docs-site/` for public MkDocs; `docs/` for internal engineering docs.

## Consequences

Clear public/private boundary. Must sync shared content manually.

## References

- [.cursor/rules/docs-site-sync.mdc](.cursor/rules/docs-site-sync.mdc)
