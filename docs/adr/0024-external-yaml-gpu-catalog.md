# ADR-0024: Use external YAML GPU catalog over hardcoded model tables

## Status

Accepted

## Context

GPU models and MIG profiles evolve; recompilation for catalog updates is expensive.

## Decision

Load GPU catalog from embedded YAML at startup. Ops can update profiles without code changes.

## Consequences

Catalog updates don't need code review. Must validate YAML at startup. Embedded in binary via `embed`.

## References

- [internal/engine/gpu_catalog.yaml](internal/engine/gpu_catalog.yaml)
- [docs/architecture/gpu-catalogs.md](docs/architecture/gpu-catalogs.md)
