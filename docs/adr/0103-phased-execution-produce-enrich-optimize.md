# ADR-0103: Use phased execution (Produce/Enrich/Optimize) with priority ordering

## Status

Accepted

## Context

Cross-plugin ordering matters (namespace after container; quota after namespace).

## Decision

Three phases with priority within each; barriers between phases.

## Consequences

Correct dependency ordering. Phases can't parallelize internally. Clear execution model.

## References

- [internal/plugin/phases.go](internal/plugin/phases.go)
