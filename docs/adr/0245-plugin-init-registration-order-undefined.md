# ADR-0245: Plugin init() registration order intentionally undefined

## Status

Accepted

## Context

Compile-time plugins ([ADR-0099](0099-compile-time-in-process-plugins.md)) register via `init()` functions in plugin packages imported from `main`. Go does not define `init()` order across packages — only dependency order within a single package.

Some contributors assume registration order affects CSV routing or API route precedence. That assumption leads to fragile `_ " import` ordering hacks.

## Decision

**`init()` registration order is intentionally undefined** and must not be relied upon.

Safety properties that make undefined order acceptable:

1. **CSV types are disjoint** — verified at boot; collision is fatal ([ADR-0246](0246-boot-fatal-csv-type-collision.md)).
2. **API routes are path-based** — Echo matches longest path; no registration-order dependence ([ADR-0105](0105-container-handlers-in-core.md)).
3. **Produce/ingest execution** sorts enabled plugins by phase, priority, and name at **runtime** ([ADR-0103](0103-phased-execution-produce-enrich-optimize.md)).

Plugin authors must not depend on import order in `cmd/*/main.go`.

## Alternatives Considered

### Explicit ordered Register() call in main

Verbose maintenance list duplicates `init()` discovery; rejected for compile-time plugin ergonomics.

### Sort init() by package name in tooling

Non-standard Go; not enforceable by compiler.

### Single plugin package monolith

Defeats modular extraction goal ([ADR-0144](0144-colocated-domain-tests.md)).

## Consequences

- Code review rejects "import X before Y" comments unless required for side-effect registration only.
- Runtime sort keys (phase, priority, name) are the documented ordering contract.
- Tests must not assume registry slice order matches import order.

## Related Decisions

- [ADR-0099](0099-compile-time-in-process-plugins.md): Compile-time in-process plugins.
- [ADR-0103](0103-phased-execution-produce-enrich-optimize.md): Phased execution ordering.
- [ADR-0246](0246-boot-fatal-csv-type-collision.md): Boot fatal on CSV collision.

## References

- [internal/plugins/registry.go](../../internal/plugins/registry.go)
- [cmd/processor/main.go](../../cmd/processor/main.go)
