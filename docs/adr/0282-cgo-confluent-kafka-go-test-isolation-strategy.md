# ADR-0282: CGO dependency via confluent-kafka-go — test isolation strategy

## Status

Accepted

## Phase

0

## Context

`confluent-kafka-go` requires CGO (links librdkafka). This affects build time, cross-compilation, and test isolation. Pure-Go alternatives existed at decision time but lacked feature parity (consumer groups, exactly-once semantics).

FIPS builds require CGO anyway (BoringCrypto).

## Decision

Accept CGO dependency. Structure packages so pure-Go tests avoid Kafka imports (test packages do not import `internal/kafka/`). Integration tests that need Kafka use build tags or separate test binaries. FIPS builds use CGO anyway.

## Alternatives Considered

### segmentio/kafka-go (pure Go)

Missing features at decision time.

### franz-go

Newer, less proven in production at decision time.

### Sarama

Deprecated; no longer maintained actively.

## Consequences

- Cross-compilation requires C toolchain.
- Test binaries without Kafka imports compile faster.
- aarch64 builds need native compilation (no cross-compile from amd64).
- Downstream FIPS requirement aligns with CGO acceptance.

## Related Decisions

- [ADR-0200](0200-kafka-consumer-session-tuning.md): Kafka session tuning.
- [ADR-0089](0089-manual-kafka-commit-after-success.md): Manual Kafka commit.
- [ADR-0266](0266-go-language-choice-inherited-kafka-integration-service.md): Go language choice.

## References

- [internal/kafka/consumer.go](../../internal/kafka/consumer.go)
- [Makefile](../../Makefile)
