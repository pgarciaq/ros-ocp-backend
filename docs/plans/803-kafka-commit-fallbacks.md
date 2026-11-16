# #803 — Kafka Commit Fallbacks: Loud Loss Instead of Silent Loss; the Rebalance Window Proven Benign — Implementation Plan

> **For agentic workers:** superpowers:test-driven-development governs every step.
> Owner decisions 2026-11-16 (the assessment this tracker item gated on):
> **Finding 1 — option A**: the two produce-failure fallbacks commit with
> the DLQ-exhaustion disposition (alert + failed-files marker + commit),
> converting silent loss into loud loss. Option B (the contiguous commit
> watermark — zero-loss-ever) **filed as #870, not implemented** — the
> owner's assessment: probably not required for ROS OCP.
> **Finding 2 — prove-benign**: a write-level replay test demonstrating
> convergence, plus the documented dismissal; no fence/drain machinery.

**Repo:** ros-ocp-backend, branch `pgarciaq-rosocp-superpowers-phase17`.
Tracker: #857 (section 1, the last open ingestion item).

## The verified mechanism

**Finding 1:** `handleKafkaTransientError` has exactly two paths that
return without committing, both relying on a "will redeliver naturally"
that Kafka's cumulative commits defeat (the next message's handler-tail
commit acks the skipped offset — the report is silently lost):

1. `kafka_retry.go:267` — DLQ produce failed AND DLQ-redelivery produce
   failed → return uncommitted.
2. `kafka_retry.go:298` — retry-topic produce failed → return uncommitted.

The recon's key context: the transient-retry path already has the correct
produce-a-redelivery-then-commit pattern for the success case, and the
DLQ-*exhaustion* path (`:249-256`) already commits with the loud-loss
disposition (alert metric + `markDLQFilesFailed` + commit). The fix aligns
the two fallbacks with that existing disposition.

**Finding 2:** no rebalance callback exists (confluent-kafka-go emits
`RevokedPartitions` events; nothing consumes them). Buffered messages for
a revoked partition complete while the new owner replays the range — the
exposure is a transient inconsistent read that ADR-0154's amendment
*asserts* converges ("ingestion upserts are idempotent replacement
writes; a replayed report converges to the same row"). The disposition
turns that assertion into evidence.

## Mechanism (the fix)

**Finding 1:** in both fallback paths, replace the uncommitted return
with the DLQ-exhaustion disposition:

- increment `metrics.KafkaDLQFailedTotal`,
- `markDLQFilesFailed(log, msg, err)`,
- commit (best-effort, logging failures as the exhaustion path does).

The comment replacing "will redeliver naturally" records WHY: an
uncommitted return is not a redelivery guarantee under cumulative
commits — committing with an alert is strictly better than silent loss,
and a full broker outage fails commits too (in which case redelivery
genuinely happens).

**Finding 2:** no production change. A write-level replay test
demonstrates convergence: the same report payload written by two
interleaved "consumers" (the replay window's shape — the old owner's
in-flight write landing after the new owner's replayed write, and vice
versa) produces identical rows. If the test fails to converge, that is a
real finding and the fence becomes warranted — the test decides.

## Tests

1. **The RED (finding 1, the close criterion's shape):** using the
   `kafka.NewMockCluster` harness (`consumer_partition_order_test.go`'s
   conventions, minding its quirks — `Committed` takes `timeoutMs int`,
   `mc.CreateTopic` not AdminClient, produce needs a delivery channel):
   a message whose processing takes the retry-exhausted path with all
   produces failing, followed by a successful message — assert the
   **actual broker committed watermark** still covers the failed offset.
   **Baseline: the failed offset is acked by the later commit — RED.**
2. **The loud-loss assertions:** the fallback fires
   `KafkaDLQFailedTotal` and marks the files failed (the alert surface).
3. **The replay-convergence test (finding 2):** two interleaved writers
   over the same report rows — both interleavings converge to identical
   rows. Plus a comment tying it to the ADR-0154 amendment's claim.
4. **The #756 guarantees hold:** the existing
   `consumer_partition_order_test.go` suite passes unchanged.

## Mutations run

- **M1** (one fallback reverted to uncommitted-return): the
  DLQ-redelivery-failure RED fails on both the commit and the alert
  assertions.
- **M2** (the alert/marker dropped from the retry-produce fallback,
  commit kept): the alert assertion fails while the commit assertion
  holds — the tests discriminate between the two halves of the
  disposition.

## Honest notes

- **The close criterion's watermark wording was option-B-shaped**
  ("must NOT result in the failed offset being acknowledged" — the
  watermark remedy's end state). Under the owner's option-A decision
  the desired end state is the opposite: the offset IS acknowledged,
  deliberately, by the fallback's own commit with the alert. The
  discriminating assertions are therefore the fallback's own commit
  call plus the alert surface (the fake-harness RED), with the
  MockCluster composition test proving the fallback's commit and the
  next message's commit compose through the real queue machinery
  (broker watermark = 2 after offsets 0 and 1 — Kafka's
  committed-offset convention is next-to-read, learned live).
- **The two evolved tests pin the bug on baseline by name** (the
  stash-rerun RED: "an uncommitted return is silently acked by the
  next message's cumulative commit").

## Fresh-context review — the production code clean; five findings in the evidence layer, all fixed

The reviewer verified the fallback disposition's consistency with the
exhaustion path (ordering, reason strings, nil-consumer guards, no
double-commit with the handler tail), the metric registry's soundness,
the M1 mutation, the replay-test reset soundness, and the
blocking-commit semantics in the vendored confluent-kafka-go source.
Five findings:

- **P2** (the "second rebalance interleaving" test was a duplicate of
  the first wearing a different comment — the code ran `F,S,F` while
  the comment described `S,F,S,F`; the ADR and CHANGELOG cited "both
  interleavings" as verified): **fixed** — the second test now executes
  its comment's genuinely distinct sequence (the new owner replays
  both in order, then the old owner's late stale write lands after,
  then the catch-up), verified to converge; the evidence claims are
  now true. This was exactly the asserted-vs-verified failure mode the
  completion-report rules exist to catch.
- **P3** (the composition test passed on the pre-fix behavior — the
  final watermark cannot distinguish the fallback's commit from the
  next message's cumulative commit): **fixed** — the test's comment now
  states its honest scope (commits succeed, order holds, no deadlock;
  the unit RED pins the fallback's own commit) and the offset
  convention confusion is corrected.
- **P3** (`markDLQFilesFailed` unpinned for both fallbacks — a marker
  regression would ship green): **fixed** — two DB-backed marker tests
  (one per fallback, payloads carrying Files, modeled on the
  exhaustion test).
- **P3** (the broker-outage parenthetical overstated — commits BLOCK
  during an outage, then succeed or fail-after-rebalance): **fixed** in
  both code comments, the CHANGELOG, and the ADR (the vendored-source
  verification: `CommitMessage` is a synchronous coordinator
  round-trip, no local success path).
- **P3** (the metric's help text said "after exhausting DLQ delivery
  attempts" — stale for the retry-produce trigger; the monitoring row
  read as a three-way conjunction): **fixed** — the help text and the
  row now name both triggers precisely.

## Deliverables (final)

- The two fallback fixes (`internal/services/kafka_retry.go`).
- Tests: the evolved fallback pair (RED on baseline), the
  MockCluster composition test
  (`internal/kafka/commit_fallback_compose_test.go`), the
  replay-convergence pair
  (`internal/ingestion/replay_convergence_test.go` — both rebalance
  interleavings, the transient regression asserted mid-flight).
- ADR-0154 dated clarification (the commit-site enumeration completed,
  the A-over-B decision with the #870 pointer, the rebalance claim's
  evidence).
- docs-site/monitoring.md: the `rosocp_kafka_dlq_failed_total` row
  (previously absent) + Last verified bump.
- CHANGELOG entry; this plan doc; completion report per
  properly-implement-feature.
- **#870** — the filed zero-loss alternative (the owner's framing:
  not currently required for ROS OCP).
