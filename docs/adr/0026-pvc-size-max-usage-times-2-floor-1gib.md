# ADR-0026: Recommend PVC size as max(usage_max×2, 1 GiB)

## Status

Accepted

## Context

Tight shrink-only sizing leaves no operational headroom for storage growth.

## Decision

Double the observed max usage, floor at 1 GiB.

## Consequences

Generous headroom. Won't recommend tiny PVCs. May over-provision stable storage.

## References

- [internal/engine/pvc_recommend.go](internal/engine/pvc_recommend.go)
