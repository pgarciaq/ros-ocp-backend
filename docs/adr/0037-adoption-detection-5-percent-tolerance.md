# ADR-0037: Use adoption detection at 5% request tolerance

## Status

Accepted

## Context

Minor spec drift from auto-scaling or rounding shouldn't block adoption credit.

## Decision

Mark recommendation as "adopted" if current request is within 5% of recommended value.

## Consequences

Realistic adoption tracking. Tolerates minor drift. May false-positive on coincidental matches.

## References

- [internal/engine/adoption.go](internal/engine/adoption.go)
