# ADR-0024: Use external YAML GPU catalog over hardcoded model tables

## Status

Accepted

## Context

GPU models and MIG profiles evolve; recompilation for catalog updates is expensive.

## Decision

Load GPU catalog from embedded YAML at startup. Ops can update profiles without code changes.

## Alternatives Considered

### Hardcoded Go map
Every new GPU model or MIG profile requires a code change, review, and redeploy—unacceptable given quarterly NVIDIA SKU releases.

### External API lookup at runtime
Adds network dependency and latency on every classification; on-prem clusters may lack outbound access. Startup failure if catalog service is unreachable.

### Database table per GPU model
Each new SKU needs a migration and DBA coordination; slower turnaround than shipping an updated YAML in the container image.

## Consequences

Catalog updates don't need code review. Must validate YAML at startup. Embedded in binary via `embed`.

## References

- [internal/engine/gpu_catalog.yaml](internal/engine/gpu_catalog.yaml)
- [docs/architecture/gpu-catalogs.md](docs/architecture/gpu-catalogs.md)
