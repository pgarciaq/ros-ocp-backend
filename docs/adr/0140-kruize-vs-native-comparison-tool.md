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

## Scale Benchmark CLI (`cmd/bench`)

[`cmd/bench/main.go`](../../cmd/bench/main.go) provides a scale benchmark CLI distinct from the Kruize vs Native comparison tool:

- Generates synthetic orgs at configurable scale (100–100k containers)
- Measures recommendation throughput, memory footprint, and latency percentiles
- Used pre-merge to validate streaming batch changes ([ADR-0171](0171-streaming-recommendation-batches.md))

The comparison tool validates algorithm correctness against Kruize; the bench CLI validates performance regressions at fleet scale.

## References

- [docs/kruize-vs-native-comparison.md](../kruize-vs-native-comparison.md)
- [cmd/bench/main.go](../../cmd/bench/main.go)
