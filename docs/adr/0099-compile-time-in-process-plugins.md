# ADR-0099: Use compile-time in-process plugins over gRPC/Wasm/.so

## Status

Accepted

## Context

Dynamic loading adds FIPS compliance complexity, IPC overhead, and debugging difficulty.

## Decision

Compile-time plugins registered via `init()` + blank imports. No IPC, no sidecars.

## Alternatives Considered

### gRPC plugin sidecars
HashiCorp-style plugin over gRPC enables third-party extensions without recompilation, but adds IPC serialization on every ingest hook and API enrichment call, requires separate FIPS-compliant images per plugin, and complicates debugging across process boundaries in cost-onprem's single-namespace deployment.

### WebAssembly (Wasm) runtime plugins
Wasm sandboxing would allow safer third-party code, but Go↔Wasm boundary overhead on hot ingest paths is significant, FIPS certification for Wasm runtimes is immature, and no existing ROS plugins need runtime loading from untrusted sources.

### `.so` dynamic loading via plugin.Open
Go's plugin package supports `.so` hot-loading on Linux, but breaks on FIPS-enabled builds (CGO required), is unsupported on macOS dev machines, and produces opaque stack traces when plugins crash—compile-time registration via blank imports gives zero overhead with full debugger support.

## Consequences

Zero runtime overhead. All plugins compiled into single binary. Adding plugins requires rebuild.

## References

- [docs/architecture/plugin-architecture.md](docs/architecture/plugin-architecture.md)
- [internal/plugin/plugin.go](internal/plugin/plugin.go)
