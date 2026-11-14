# #804 — Kafka Batch-Start Data Race — Implementation Plan

> **For agentic workers:** superpowers:test-driven-development governs every step.
> Owner go-ahead 2026-11-14: the #857 campaign starts here (the smallest
> ingestion item; warms the context #822/#836/#803 need).

**Repo:** ros-ocp-backend, branch `pgarciaq-rosocp-superpowers-phase17`,
post `586331f4`. Tracker: #857 (queue item 1).

## Problem (the issue's mechanism, recon-verified)

`logKafkaMessageReceived` (`internal/kafka/consumer.go:333`): `msgCount`
is atomic — exactly one goroutine per multiple of 100 — but consecutive
multiples' sampling blocks can overlap across the per-partition
goroutines (#756 made the counters shared): goroutine n=200 can enter
the block before n=100's finishes, so the `time.Since(*batchStart)`
read and `*batchStart = time.Now()` write overlap — a data race per
the Go memory model. Severity P3, observability only (a torn elapsed
in the "Processed N kafka messages" INFO line; a `-race` failure under
the right shape). Production traffic hits it routinely (the #756
regression suite is single-partition by design, so it never flakes
there).

## Mechanism

`atomic.Pointer[time.Time]` for the batch start — lock-free, race-free;
the sampling block reads via `Load`, writes via `Store`. Both consumers
(sequential, partitioned) switch to the pointer type; nil-safe read.

## Tests

- **RED (race-detector-gated):** a test running `logKafkaMessageReceived`
  from 8 goroutines × 300 messages (24 sampling events across
  goroutines) — under `-race` the OLD code trips the detector (the
  detector flags unsynchronized cross-goroutine access regardless of
  wall-clock overlap). Gated on the race detector via build tags
  (skip politely when `-race` is off — `make test` doesn't default to
  it; the verification and mutation runs use `-race` explicitly).
- **Mutation:** revert to the raw `*time.Time` → the race test fails
  under `-race`.

## Non-goals

#822/#836/#803 (the rest of the ingestion cluster); the sampling
interval; the log line format.
