# ADR-0148: Redact Kafka poison payloads in logs by default

## Status

Accepted

## Context

Poison messages may contain PII or secrets.

## Decision

Log metadata only; payload logged only when `ROS_LOG_POISON_PAYLOAD=true` (truncated).

## Consequences

PII protection by default. Opt-in for debugging. Truncation limits exposure.

## References

- [internal/services/poison_message_log.go](internal/services/poison_message_log.go)
