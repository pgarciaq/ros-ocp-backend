# ADR-0140: Use Kruize vs Native comparison tool for algorithm validation

## Status

Accepted

## Context

"Looks right" sign-off insufficient for replacing production algorithm.

## Decision

Automated comparison tool runs both engines on the same input data, reports differences. Validation methodology: side-by-side comparison on identical digest input, tracking recommendation divergence percentage per resource, and correlating OOM/restart events with undersized Kruize vs Native recommendations.

## Related Decisions

- [ADR-0001](0001-native-engine-over-kruize.md): decision to build native engine validated by this tool before cutover.
- [ADR-0163](0163-deprecate-kruize-plugin.md): deprecation gated on comparison tool passing divergence thresholds.

## Consequences

Quantitative validation. Catches regressions. Requires maintaining comparison tool.

## References

- [docs/kruize-vs-native-comparison.md](docs/kruize-vs-native-comparison.md)
