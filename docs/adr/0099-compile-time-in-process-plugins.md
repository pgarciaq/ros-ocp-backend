# ADR-0099: Use compile-time in-process plugins over gRPC/Wasm/.so

## Status

Accepted

## Context

Dynamic loading adds FIPS compliance complexity, IPC overhead, and debugging difficulty.

## Decision

Compile-time plugins registered via `init()` + blank imports. No IPC, no sidecars.

## Consequences

Zero runtime overhead. All plugins compiled into single binary. Adding plugins requires rebuild.

## References

- [docs/architecture/plugin-architecture.md](docs/architecture/plugin-architecture.md)
- [internal/plugin/plugin.go](internal/plugin/plugin.go)
