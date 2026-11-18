# ADR-0359: Kafka offsets settle after processing, and a DLQ disposition is terminal

## Status

Accepted (implements [#893](https://github.com/pgarciaq/ros-ocp-backend/issues/893))

## Phase

Deployment / Ops — Kafka consumption semantics and failure visibility

## Context

`ros-processor` consumes `hccm.ros.events`, downloads the CSVs the message
references, and upserts digests. Its commit policy decides whether a report
survives a failure.

The product default was already `KAFKA_AUTO_COMMIT=false`
(`internal/config/config.go:737`), and `ros-recommendation-poller` runs that way.
Both deployment surfaces overrode the *processor* to `true`
(`cost-onprem-chart` `values.yaml:198`, `koku-service-operator`
`internal/resources/ros.go:362`).

In auto-commit mode nothing sets `enable.auto.offset.store` or
`auto.commit.interval.ms` (`internal/kafka/consumer.go:261-296`), so librdkafka
defaults apply: an offset is *stored* when `Poll` returns and committed every
5 s. The dispatcher reads ahead and buffers up to `partitionQueueSize = 16`
messages per partition before the handler runs
(`internal/kafka/consumer.go:212-221`). A message queued or mid-pipeline is
therefore acknowledged about 5 s after being read, whatever its outcome. The
handler tail could not compensate: the retry/DLQ ladder was gated behind
`!KafkaAutoCommit` (`report_processor.go:270`), so in that mode
`handleKafkaTransientError` was unreachable.

The consequence, observed on the lab SNO on 2026-11-18: a database restart during
a re-ingest dropped 15 of 36 accepted `ocp_ros_usage` files. No retry, no DLQ
entry, no `report_file_status` row, no log line naming a file — found only by
diffing the fixture on disk against the status table. A lost file also leaves the
previous day's digest cells in place, so the result read as a fixture defect.

Two further gaps were independent of the flag:

- `hccm.ros.events.dlq` exists (12 partitions, 30-day retention) with **no
  consumer group**, and the route that *succeeds* — the message reaching the
  topic — did not mark the files. `markDLQFilesFailed` was called only on the
  drop/fallback paths (`kafka_retry.go:253, 281, 316`), so a dead-lettered file
  kept a non-terminal row forever.
- `MarkReportFileFailed` is UPDATE-only (`internal/model/report_file_status.go:101`)
  and ignores the affected-row count. The October shape fails *before*
  `ensureManifestExpectations` (`report_processor.go:244`), so no rows existed
  and the UPDATE was a silent no-op.

Upstream koku solved the same class of defect on its own ingress side — durable
staging before the Kafka confirm (`COST-8282`: `IngressStagingPayload`, state
`pending → processing → processed → failed`) — and its `COST-8164`
`IngressDeadLetterQueue` is a park-and-skip switch gated on an Unleash flag. Both
change *when koku emits* `hccm.ros.events`; neither changes who consumes it.
`ros-ocp-backend` is 1955 commits ahead of `origin/main` with no upstream work on
`kafka_retry.go`, `consumer.go` or `report_processor.go`, so no ROS-side fix
arrives via a rebase.

## Decision

1. **Manual commit owns the processor's offsets.** `KAFKA_AUTO_COMMIT=false` is
   the contract, not a preference: a message is committed only after its pipeline
   completed, or after the ladder settled it (retry produced / DLQ produced /
   loud-drop committed). The product default stays `false`, and both deployment
   surfaces must follow it for `ros-processor`.
2. **A DLQ disposition is terminal for the files it carries.** On the successful
   route each file is recorded `failed` with
   `error_message = "dlq: <error>"`; on a drop (`dlq-drop`) with
   `"dlq-drop: <error>"`, because the payload never reached the topic and a
   reship — not a topic read — is the recovery. Both increment
   `ros_ingestion_file_failures_total{error_class="dlq"}`; the routed/dropped
   split is carried by `rosocp_kafka_dlq_messages_total` versus
   `rosocp_kafka_dlq_failed_total`.
3. **Expectations are registered before the terminal mark.** The disposition
   path calls `EnsureReportFileExpectations` first, so a message that died before
   the file loop still leaves a row. Without it "terminal and visible" is false
   exactly in the failure mode that motivated the issue.
4. **The settlement gate is a named seam.** `settleKafkaMessage` takes the
   committer and producer as interfaces; `ProcessReport` adapts
   `kafka.MessageHandler`'s concrete consumer, keeping a nil consumer nil rather
   than wrapping a nil pointer. The retry/DLQ producer is resolved lazily so a
   successful message never constructs one.
5. **Auto-commit remains an explicit opt-out with pinned behavior.** It is not
   improved here: it still has no ladder and still acks on poll. A test pins that
   behavior so changing it is a decision rather than an accident.
6. **Flipping the value requires a recreate.** During a rolling update, old
   auto-commit pods and new manual-commit pods share the `ros-processor` group
   and the old ones ack offsets the new ones are still processing.

Delayed redrive — the ladder spans seconds, not an outage, because
`produceRetryImpl` re-produces immediately — stays with #806. The contiguous
commit watermark (zero loss during a full broker outage) stays with #870.

## Alternatives Considered

| Alternative | Why not |
|---|---|
| Route transient errors through the ladder *even when* auto-commit is on (the issue's option 1) | Fixes only the failure branch. Auto-commit still acks messages that are queued or mid-pipeline, which is the larger and less observable half of the loss window. Retained as a possible belt-and-braces later, not the fix. |
| Leave DLQ'd files `pending` and build a drain first | Reproduces invisibility: nothing consumes the topic, so "pending" would mean "forgotten". A drain is an operator procedure and does not need to precede the truthful status. |
| Port koku's `IngressDeadLetterQueue` | Park-and-skip behind an Unleash flag. This deployment runs no Unleash (`cost-onprem` has no release, masu no `UNLEASH*` env), so it could not be switched on, and its semantics (hold every message for a schema) are not the failure mode here. |
| Adopt koku's durable inbound staging (COST-8282 shape) now | The right long-term shape and already the direction upstream converged on, but it is a durable inbox — #870's size. This ADR is a step toward it, not a competing design. |
| Add retry backoff here | Owned by #806, which is evidence-gated on measured orphan DLQ volume. Absorbing it here would claim outage coverage this change does not deliver. |

## Consequences

- A DB hiccup during ingest no longer discards accepted uploads silently: the
  message is retried, dead-lettered, or recorded as a terminal `failed` row with
  the affected filenames.
- Operators gain a queryable blast radius (`error_message LIKE 'dlq%'`) and a
  disposition that says whether a replay or a reship is required.
- Replay remains bounded by object lifetime: the message carries URLs, so a
  replayed message re-downloads from the object store. Replay after the
  quarantine object expires needs a reship; the runbook states this rather than
  promising indefinite replay.
- Unclassified errors remain transient by design
  (`kafka_processing_errors.go:75`), so a permanently bad payload burns 5 retries
  plus 3 DLQ attempts before the terminal row. Accepted: the loud direction is
  the safe one, and tightening the classifier is its own change.
- The ladder absorbs seconds, not outages; #806 and #870 remain the paths to
  longer windows and the zero-loss watermark.

## Verification

| Behavior | Test | Mutation that must fail it |
|---|---|---|
| The successful DLQ route leaves a terminal `failed` row | `TestHandleKafkaTransientError_SuccessfulDLQRouteMarksFilesFailed` | removing the `markDLQFilesFailed` call from the routed path (row stays `pending`) |
| A message that died before the file loop still gets a row | `TestHandleKafkaTransientError_SuccessfulDLQRouteCreatesMissingStatusRow` | removing the `EnsureReportFileExpectations` call (no row at all) |
| The routed/dropped dispositions are distinguishable | the `error_message` assertions in both tests above | dropping the disposition prefix |
| Manual commit commits success | `TestSettleKafkaMessage_ManualCommitOnSuccessCommits` | inverting the `!cfg.KafkaAutoCommit` guard |
| Manual commit requeues then commits a transient error | `TestSettleKafkaMessage_ManualCommitOnTransientErrorRequeuesAndCommits` | same |
| Auto-commit has no ladder (pinned opt-out) | `TestSettleKafkaMessage_AutoCommitTransientErrorLeavesNoLadder` | routing auto-commit failures through the ladder without a recorded decision |
| The handler reaches the ladder end to end | `TestProcessReport_TransientFailureDrivesRetryLadder` | gating the ladder behind `!KafkaAutoCommit` on a nil-safe path |
| A nil committer is safe | `TestSettleKafkaMessage_NilCommitterDoesNotPanic` | wrapping the nil `*kafka.Consumer` in a non-nil interface |
| The orphan branch changes mode with the flag | `TestProcessVMFileBatch_OrphanTerminalFailureUnderAutoCommit` (pre-existing) | removing the auto-commit branch at `parallel_ingest.go:314` |

Live verification is a forced transient failure on a small ingest — retry, DLQ
route, and a `failed` row with the `dlq` disposition — not a full re-ingest.

## Cross-Repo Dependencies

- `cost-onprem-chart` `cost-onprem/values.yaml:198` — `ros.processor.kafkaAutoCommit`
  must be `false`; the deployed value is the source of truth on the cluster.
- `koku-service-operator` `internal/resources/ros.go:362` — the newer deployment
  surface hardcodes `true` for `ros-processor` and needs the same flip.
- koku upstream: `COST-8164` (ingress DLQ, park-and-skip) and `COST-8282`
  (durable inbound staging) are recorded as prior art and direction. Neither
  delivers this fix, and both are pre-staging in our deployed
  `branch-202611181014`, so a koku bump that crosses `COST-8282` changes when ROS
  events are emitted and must be verified separately.

## Related Decisions

- ADR-0154 — partition-scoped worker pool, including the #803 update that made
  commit fallbacks a loud loss with an alert.
- ADR-0155 — `X-Retry-Count` on requeue (the ladder's state).
- ADR-0050 — synthesized manifest IDs, the fallback identity the DLQ marking uses
  when a legacy message omits `metadata.manifest_id`.
- #806 — delayed redrive (outage absorption).
- #870 — contiguous commit watermark (zero loss on broker outage).
