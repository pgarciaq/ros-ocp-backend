# ADR-0158: Use ROS_ENABLED_PLUGINS allowlist OR ROS_DISABLED_PLUGINS blocklist

## Status

Accepted

## Context

Rebuilding binary per deployment variant is too expensive.

## Decision

Runtime enable/disable via environment variables. Both allowlist and blocklist supported (not both simultaneously).

## Alternatives Considered

### Separate binaries per plugin combination
Building `ros-api-gpu`, `ros-api-no-gpu`, etc. guarantees dead-code elimination, but explodes CI matrix and Helm chart variants—cost-onprem already builds six aarch64 images proactively; doubling images per plugin toggle is unsustainable.

### Feature flags service (Unleash)
Unleash integration would allow per-org plugin toggles without redeploy, but ROS on-prem shares Unleash with Koku optionally and plugin enablement is a deployment-time concern (GPU clusters vs non-GPU), not a per-tenant runtime flag.

### Compile-time `#ifdef`-style build tags only
Go build tags could exclude plugins at compile time without runtime checks, but operators enabling GPU after deploy would require a full image rebuild and rollout; env-var toggles with startup validation (`0157`) let the same image serve GPU and non-GPU clusters.

## Consequences

Flexible deployment configuration. Must validate at startup. One of the two must be used, not both.

## References

- [docs/architecture/plugin-architecture.md](docs/architecture/plugin-architecture.md)
