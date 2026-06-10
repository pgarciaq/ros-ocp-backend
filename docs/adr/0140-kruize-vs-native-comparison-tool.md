# ADR-0140: Use Kruize vs Native comparison tool for algorithm validation

## Status

Accepted

## Context

"Looks right" sign-off insufficient for replacing production algorithm.

## Decision

Automated comparison tool runs both engines on same data, reports differences.

## Consequences

Quantitative validation. Catches regressions. Requires maintaining comparison tool.

## References

- [docs/kruize-vs-native-comparison.md](docs/kruize-vs-native-comparison.md)
