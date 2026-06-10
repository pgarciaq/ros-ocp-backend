# ADR-0155: Use retry counter via X-Retry-Count header on requeue

## Status

Accepted

## Context

Stateless retries can't distinguish first failure from chronic poison.

## Decision

Increment `X-Retry-Count` header on each requeue to source topic.

## Consequences

Visible retry count. DLQ trigger after max. Header survives topic requeue.

## References

- [internal/services/kafka_retry.go](internal/services/kafka_retry.go)
