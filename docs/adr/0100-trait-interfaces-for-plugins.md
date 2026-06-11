# ADR-0100: Use trait interfaces (CSVIngestor, IngestHook, APIProvider, …)

## Status

Accepted

## Context

Fat Plugin interface would force empty method implementations on every domain.

## Decision

Fine-grained trait interfaces; plugins implement only what they need.

## Alternatives Considered

### Single fat Plugin interface
One interface with `Ingest()`, `Produce()`, `Enrich()`, `Optimize()`, `APIRoutes()`, etc. would simplify the registry, but forces every plugin to stub 10+ no-op methods—GPU plugin would carry empty PVC/quota/VM methods, and compile-time interface satisfaction errors would not catch missing implementations until runtime.

### Plugin inheritance hierarchy (base + overrides)
Embedding a `BasePlugin` struct with default no-ops reduces boilerplate, but Go composition makes it easy to accidentally override the wrong method or miss a new trait when the base struct gains a method—explicit interface checks at registration time are clearer.

### One plugin per binary with no shared traits
Fully independent plugin binaries would avoid trait design entirely, but contradicts ADR-0099's in-process model and prevents cross-plugin orchestration (container produce → GPU enrich → quota optimize ordering in `0103`).

## Consequences

Clean separation. Type-safe dispatch. Registry inspects implemented traits at registration.

## References

- [internal/plugin/plugin.go](internal/plugin/plugin.go)
