# #756 Kafka Partition-Ordered Queues — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Within each partition, handler execution and commits preserve offset order — actually delivering ADR-0154's stated contract — while cross-partition parallelism scales with traffic, bounded by the DB-connection budget.

**Architecture:** Replace the shared `jobs` channel + per-partition mutex with per-partition ordered queues: a dispatcher goroutine reads messages and routes each to its partition's channel; one goroutine per partition consumes its channel serially; a weighted semaphore (sized from the DB-connection budget) caps concurrent handlers across partitions. Commits stay where they are (the handler's tail) — with ordered execution, they're contiguous by construction, making the global `parallelCommitMu` unnecessary on the parallel path.

**Tech Stack:** Go, confluent-kafka-go/v2 (incl. the mock cluster the audit used), pgx, testcontainers, testify.

**Spec:** [GH #756](https://github.com/pgarciaq/ros-ocp-backend/issues/756) (V15-06, P2) + its handoff comment (reader-assigned ordered partition queues with cross-partition parallelism) + owner decision 2026-11-04 (Option A, the performance-optimal design, with the DB-connection-bounded semaphore) + session recon.

**Recon-verified facts this plan argues from:**

- Zero drift on all four named files since baseline `3061a76f`.
- `consumeMessagesParallelUntilCancelled` (`consumer.go:138-201`): workers dequeue from shared `jobs` chan FIFO, *then* compete for `partitionLocks` — exclusion without order. Worker capacity wasted on lock-blocking under a hot partition (3 workers → ≈1 effective).
- `CommitMessage` (`commit.go:18-30`): the global `parallelCommitMu` serializes calls only; committing offset N acknowledges all ≤ N (the audit's mock-broker proof).
- Three consumer paths (`consumer.go:100-106`): `KafkaParallel && KafkaWorkers > 1` → parallel; else sequential. Plus the handler's `KafkaAutoCommit` branch (`report_processor.go:269-278`). The fix targets the parallel path only.
- ADR-0154 (`docs/adr/0154-partition-scoped-worker-pool.md`): "Parallelism across partitions. Ordering within partition." — the contract this fix actually delivers.
- Config constraint (`config.go:19`): `ManifestDownloadWorkers × KafkaWorkers ≤ DBMaxConns − 2` — each handler holds a DB connection; the semaphore must respect the same budget.
- The panic-recovery path (`consumer.go:172-181`) commits-on-skip after a handler panic — must survive the redesign (a panicked message is "done" for ordering purposes).
- Ingestion upserts (`ON CONFLICT DO UPDATE` across `internal/ingestion/`) are replacement writes — ordered execution makes them fresh-after-stale by construction.

**Readiness-pass amendments (2026-11-04, verified at HEAD `5bcac5f9` — zero drift on all four files):**

- **Drain contract:** the current reader goroutine's `defer close(jobs)` means buffered messages ARE processed on shutdown (workers drain via `for msg := range jobs`). The new design must match: dispatcher closes each partition channel on exit; partition goroutines drain them before the WaitGroup releases. `StartConsumer`'s "drains in-flight handlers" doc comment stays true.
- **`parallelCommitMu` removal evidence:** all three commit call sites — `wrapHandlerWithInFlight`'s panic path (consumer.go:65), the worker panic recovery (consumer.go:170), and `kafka_retry.go:255`'s handler tail — execute inside the handler's goroutine, which in the new design is the partition goroutine. Per-partition commits are serial by construction; cross-partition concurrency hits librdkafka's thread-safe commit. Removal gated on `make test` (RACE=1) green.
- **Two-tier RED (test-design ruling):** within a partition the current mutex serializes execution; the filed reordering lives in the dequeue→lock-acquisition window (unfair mutex + scheduler) — probabilistic, not deterministic. Tier 1: a stress-flavored trace test (~200 messages, one partition, contending workers) asserting offset-sorted execution — near-certain RED on the baseline with the failure message naming the inversion; deterministic-by-construction GREEN on the fix. Tier 2: deterministic unit tests of the new dispatcher itself. If Tier 1 won't reproduce on the baseline after honest attempts, stop and report — fallback is a documented observed-trace RED (run N times, show the inversions), never a weakened assertion.
- **Test infrastructure:** `kafka.NewMockCluster` verified available in the vendored confluent-kafka-go/v2, but with zero existing usage in this repo — the broker-watermark test builds new harness. `golang.org/x/sync v0.23.0` available for `semaphore.Weighted`.
- **Working-tree hygiene:** a parallel session's edits are present (Makefile, README.md, docs-lint.yml, docs/architecture/*.md) — #756 commits stage only their own files.

## Global Constraints

- Production change to the consumer's concurrency core — commits wait for explicit owner go-ahead.
- **Preserve ADR-0154's contract** — parallelism across partitions, ordering within; the sequential and autocommit paths unchanged.
- **The DB-connection budget** — the semaphore cap; no unbounded per-partition goroutines holding connections. Default cap: the existing `KafkaWorkers` value (semantically repurposed from "worker count" to "max concurrent partition handlers"); the config comment updated.
- Panic-recovery commit-on-skip preserved; shutdown/drain must wait for in-flight partition goroutines (no message loss on SIGTERM).
- No drive-by refactors: the reader loop, the handler signature, `wrapHandlerWithInFlight`, and the metrics stay; only the dispatch/execution structure changes.
- The close criteria's warning: "do not claim mutual exclusion proves FIFO ordering or fix the issue with a global commit mutex alone" — the regression must assert **execution order** and **broker watermark**, not lock behavior.
- Push target: fork (`pgarciaq`), branch `pgarciaq-rosocp-superpowers-phase17`.

## Review Focus

1. **Execution order within a partition** — a blocked/slow lower offset must not let a higher offset run first. → Task 2 trace assertions.
2. **Broker watermark** — the mock-cluster committed offset never acknowledges an unprocessed message (the audit's proof reversed). → Task 2 mock-broker test.
3. **Cross-partition parallelism** — two partitions' handlers run concurrently (the throughput win; ADR-0154's other half). → Task 2 concurrency test.
4. **The semaphore** — more partitions than the cap: queues back up, no deadlock, ordering preserved per partition. → Task 2 cap test.
5. **Shutdown/drain** — in-flight messages complete (or the drain policy is explicit) before the consumer exits. → Task 2 shutdown test.
6. **Replacement-write order** — same-partition messages execute in offset order, so upserts are fresh-after-stale. → Task 2 (asserted via execution trace; the write-level proof is #758's territory, cross-referenced).

---

### Task 1: The partition dispatcher (self-contained TDD)

**Files:**
- Modify: `internal/kafka/consumer.go` — replace `consumeMessagesParallelUntilCancelled`'s body
- Test: `internal/kafka/consumer_partition_order_test.go` (new)

**Interfaces:**
- Produces: `consumeMessagesPartitionOrdered(ctx, reader, consumer, handler, log, maxConcurrent int)` — the new parallel path. Internal structure: a `partitionDispatcher` goroutine (reads `reader.ReadMessage`, routes by `partitionLockKey(msg.TopicPartition)` to a per-partition `chan *kafka.Message` with a small buffer), per-partition goroutines (serial consume from their channel, acquire the semaphore around `handler()`), and a `sync.WaitGroup` covering all partition goroutines for drain.

- [ ] **Step 1: Write the failing tests** (Task 2's suite, below — written first, RED against the current implementation).
- [ ] **Step 2: Run → RED** — the ordering tests fail on the shared-channel design (the audit's `trace=[6 7 8 1 2 9]` mechanism).
- [ ] **Step 3: Implement** the dispatcher + per-partition goroutines + semaphore; `consumeMessagesUntilCancelled` routes to it when `KafkaParallel && KafkaWorkers > 1` (same gate); the old `consumeMessagesParallelUntilCancelled` is replaced (not kept alongside).
- [ ] **Step 4: Run → GREEN.**

### Task 2: The regression suite

**Files:**
- Create: `internal/kafka/consumer_partition_order_test.go`

**Interfaces:**
- Consumes: a `kafkaReader` fake yielding controlled offsets (the audit's handoff: "feed increasing offsets for one partition"); the librdkafka mock cluster (`kafka.NewMockCluster`) for broker-watermark assertions; a blockable handler (a channel-gated `func`) for the blocked-lower-offset case.

- [ ] **Step 1: Write the tests (two tiers — see the readiness amendment):**
  - **Tier 1 — stress trace (baseline RED):** one partition, ~200 offsets, workers contending, a handler recording execution order with jittered microsecond delays. Assert the trace is offset-sorted. Near-certain RED on the shared-channel design; deterministic GREEN on the fix. If not reproducible on baseline after honest attempts: stop and report (documented observed-trace RED is the fallback, never a weakened assertion).
  - **Tier 2 — deterministic dispatcher units (GREEN-only, post-fix):** routing by `partitionLockKey`, per-partition FIFO, semaphore cap, drain.
  - **Blocked lower offset:** the handler blocks on offset 3 (channel gate); offsets 4-9 are dispatched; assert none execute until 3 releases, then the full trace is ordered.
  - **Broker watermark:** with the mock cluster, process offsets 0-2 where 0-1 are blocked; assert `Committed` for the partition never returns a next-offset > 0 while 0-1 are unprocessed (the audit's proof, reversed).
  - **Cross-partition concurrency:** two partitions, both handlers in-flight simultaneously (assert an overlap: both entered, neither finished).
  - **Semaphore cap:** 5 partitions, cap 2; assert at most 2 handlers in-flight at once; all traces per-partition ordered; no deadlock (test completes).
  - **Shutdown/drain:** cancel ctx with a handler in-flight and messages in a partition queue; assert the in-flight handler completes and buffered messages are processed (matching the `close(jobs)` drain contract).
  - **Panic recovery:** a handler panics on one offset; assert the commit-on-skip fires and the partition continues with the next offset in order.
- [ ] **Step 2: RED on baseline** for every ordering/watermark case; controls (cross-partition concurrency) pass on both.
- [ ] **Step 3: Ledger the RED output.**

### Task 3: Wire the config + clean up

**Files:**
- Modify: `internal/config/config.go` (`KafkaWorkers` doc comment: "max concurrent partition handlers"), `internal/kafka/commit.go` (the `parallelCommitMu` becomes unnecessary on the ordered path — remove or scope it, with the ADR-0154 comment updated)

- [ ] **Step 1:** Config comment update; `parallelCommitMu` removal (ordered execution makes commits per-partition serial by construction — but verify the sequential path doesn't need it either, since it's single-threaded).
- [ ] **Step 2: `make lint`** + the full `internal/kafka` package.
- [ ] **Step 3: Commit** (after owner go-ahead): `fix(#756): partition-ordered queues — FIFO execution per partition, cross-partition parallelism bounded by the DB budget`

### Task 4: Mutation checks + full verification

- [ ] **Step 1: Mutations** — (a) remove the semaphore: the cap test fails; (b) re-introduce the shared channel for one partition's dispatch: the order trace fails. Restore.
- [ ] **Step 2: Full suite** — `make test` → PASS (headline claim).
- [ ] **Step 3: ADR-0154 amendment** — the ADR's Decision section gets a correction note: the per-partition *mutex* delivered exclusion, not order; the ordered-queue dispatcher delivers the stated contract (file an amendment or a superseding note per the ADR conventions).
- [ ] **Step 4: Commit any strengthening** (after owner go-ahead).

### Task 5: Close-out

- [ ] **Step 1: Comment on #756** — the design decision (Option A over B/C, with the performance rationale: lock-blocking elimination, partition-scaled concurrency), the mutation results, the adversarial answer ("how could these tests pass while the bug lives?" — the trace and watermark tests fail on the baseline with the exact reordering mechanism; the mock-broker watermark can't be fooled by a commit mutex).
- [ ] **Step 2: Close with the completion-report table** — verified vs asserted (CI; the topic's actual partition count as a deployment note; the throughput gain as asserted-not-benchmarked unless the lab measures it; the #758 replacement-write interplay cross-referenced).

---

## Self-review notes

- **Spec coverage:** criterion 1 (offset order within partition, concurrency across) → Tasks 1-2; criterion 2 (blocked/failed earlier offset not skipped; broker watermark verified) → the blocked-offset + mock-cluster tests; criterion 3 (shutdown/drain, replay, stale-after-fresh) → the drain test + the trace-order assertion (the write-level proof cross-referenced to #758); criterion 4 (no mutex-as-FIFO claim) → the watermark test explicitly bypasses the commit-mutex question; criterion 5 (RED on baseline, right reason) → Task 2 RED; criteria 6-8 → Tasks 4-5.
- **Proportion:** one function replaced, one config comment, one mutex removed, one ADR amendment, one test file. The concurrency core is the risk — hence the mutation pair and the drain test.
