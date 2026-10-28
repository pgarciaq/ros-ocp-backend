# Native recommendation worker decoupling — design draft

Date: 2026-10-28
Status: Draft for owner review; no implementation approval implied.
Issue: [#708](https://github.com/pgarciaq/ros-ocp-backend/issues/708)

## Intent and why

Prevent native recommendation computation from occupying the ROS upload
consumer after report data has been durably ingested. The upload path should be
able to finish its responsibility—download the individual ROS CSVs, parse and
digest their contents, persist the recommendation inputs, and record durable
recommendation work—without waiting for the recommendation engines to finish.

This is a processing-boundary change, not an engine rewrite. It aims to isolate
upload throughput from recommendation CPU and database-read work while
preserving the current recommendation inputs, algorithms, manifest-completeness
rules, and outputs.

## Current behavior

The normal native path is:

```text
koku-metrics-operator tarball
  → Koku receives/extracts the payload and selects ROS CSV files
  → Koku uploads individual CSV objects to S3 and publishes their URLs in Kafka
  → ROS consumes the Kafka event and downloads each CSV
  → ROS parses rows, computes digests, and persists samples/digests in PostgreSQL
  → after the manifest is complete, ROS runs native recommendation engines
  → ROS commits the upload Kafka message
```

ROS receives individual file URLs, not the operator's full tarball. Native
CSV ingestion already computes and persists the data the engines consume; it
does not put raw CSV blobs in PostgreSQL. `ProcessReport` calls
`scheduleManifestRecommendations` before committing the upload event. For a
publisher-supplied manifest ID, that call can execute native recommendations
inline in the same upload handler. The `recommendation-poller` command is the
legacy Kruize result-fetching path, not an isolated native recommendation
worker.

Manifest tracking is per file in `report_file_status`. The engine runs only
after all expected files are marked done. Legacy messages with synthesized
`synth-*` manifest IDs use a resettable in-memory quiet-period timer; that timer
is not durable across a process restart.

## Goals

- Separate native recommendation execution from the upload-message handler.
- Acknowledge an upload event only after its digests and recommendation job
  trigger are durable.
- Preserve current digest values, recommendation algorithms, plugin selection,
  manifest completeness behavior, and public API results.
- Make recommendation work recoverable after process/pod restart, with visible
  retry and terminal-failure states.
- Run the native recommendation worker as a separately scalable process while
  reusing the existing `rosocp` binary and container image.
- Bound worker concurrency and coordinate its PostgreSQL connection use with
  the processor, API, and other deployments.
- Measure ingestion and recommendation latency separately; do not claim a
  throughput gain without production-like measurements.

## Non-goals

- Persisting raw CSV blobs in PostgreSQL.
- Moving CSV download, parsing, or digest calculation out of the upload
  consumer. This proposal isolates recommendation execution only.
- Changing digest schemas, recommendation math, thresholds, recommendation
  APIs, or CSV contracts with Koku/KMO.
- Adding Trino to ROS or changing Koku's cost-processing path.
- Reusing or changing the legacy Kruize `recommendation-poller` behavior.
- Coalescing multiple manifests into one recommendation run in the first
  implementation. That may save work but changes scheduling semantics and
  needs its own evidence and approval.
- Increasing concurrency or trading recommendation freshness/accuracy for
  throughput without measured evidence and explicit approval.

## Proposed architecture

### Upload processor

1. Keep the existing CSV download, parse/digest, persistence, and per-file
   status updates.
2. Preserve the current manifest-completeness gate. Do not enqueue work for an
   incomplete manifest.
3. Once the manifest is eligible for recommendations, insert or update a
   durable native-recommendation job in PostgreSQL.
4. Commit the upload Kafka event only after that job write succeeds. A failure
   to persist the job is a transient upload-processing failure: the upload
   event must remain retryable, and ingestion upserts/file status must remain
   safe on replay.
5. Do not run native recommendation engines inline in async mode.

The job should contain only the metadata needed to locate and process the
persisted inputs (at minimum organization, cluster, and manifest identity).
Do not copy presigned URLs, CSV contents, or the full upload Kafka payload into
the job row. The worker should derive completed report types from durable
manifest/file-status data, as the current manifest runner does.

### Durable job table (proposed)

Add a small PostgreSQL table, tentatively `native_recommendation_jobs`, as the
queue and source of truth. The table shape below is conceptual; names and types
are not locked:

| Field | Purpose |
|---|---|
| `id` | Surrogate primary key for efficient claiming and logs |
| `org_id`, `cluster_uuid`, `manifest_id` | Tenant/cluster scope and dedupe key |
| `status` | Constrained state such as pending, running, completed, or failed |
| `available_at` | Earliest claim time; also supports synthesized-manifest quiet periods |
| `attempt_count` | Retry accounting |
| `lease_until` | Recovery of work abandoned by a terminated worker |
| `created_at`, `updated_at`, `completed_at` | Operational lifecycle timestamps |
| `last_error` | Bounded diagnostic detail for retries/terminal failure |

Proposed schema properties:

- Unique key on the manifest identity (likely `(org_id, cluster_uuid,
  manifest_id)`; verify against existing manifest identity guarantees before
  implementation).
- Check constraints for valid job states and non-negative attempt counts.
- A partial claim index over ready, non-terminal jobs ordered by
  `available_at`/creation time; avoid indexing every field.
- A deliberate cleanup/retention policy for completed and failed jobs. Do not
  delete pending/running work as part of routine cleanup.
- No FK to individual `report_file_status` rows: a job is manifest-scoped,
  references multiple file rows, and must have an independently managed
  lifecycle. Re-evaluate if implementation finds a suitable manifest parent.

The initial queue is PostgreSQL-backed directly, rather than “write DB, then
best-effort publish Kafka.” This keeps job state, quiet-period scheduling,
leases, retries, and completion in one durable store. The upload Kafka event is
not committed until the job row is durable, so a crash before job creation
causes the upload event to be replayed.

### Kafka as an alternative

Kafka is a valid dispatch mechanism here: AMQ Streams is already part of both
SaaS and on-prem deployments, and a dedicated recommendation topic would allow
worker scaling from consumer lag without polling the job table. However, a
Kafka-only queue is a weaker fit for the job lifecycle in this proposal:

- Kafka does not provide native delayed delivery for the synthesized-manifest
  quiet period. Implementing that durably would require a scheduler, delayed
  topic scheme, or PostgreSQL state.
- Delivery is at-least-once across the PostgreSQL digest writes and Kafka
  publish. The upload consumer can publish and wait for delivery before
  committing its source offset; a crash after publish but before source-offset
  commit can still produce a duplicate job event, so workers need a stable job
  key and retry-safe writes.
- A recommendation topic needs its own retry/DLQ behavior, topic configuration,
  ACLs, retention, partition/key strategy, and operational replay procedure.
  Existing upload-topic retry handling is not automatically suitable for
  recommendation jobs.
- A database job ledger would still be useful for completion state, retries,
  terminal failures, and operator replay. If a delayed database job is later
  published to Kafka, that DB-to-Kafka handoff needs an outbox or equivalent
  recovery mechanism.

**Owner decision (2026-10-28):** use the PostgreSQL job table as the first
implementation's queue and source of truth. Do not add a native-recommendation
Kafka topic in the first version. Reconsider Kafka dispatch only if measurements
show that PostgreSQL queue polling or worker scaling is a real constraint; if
reconsidered, retain durable deduplication/job state and define the
database-to-Kafka recovery boundary.

### Native recommendation worker

Add a new Cobra command, provisionally:

```text
rosocp start native-recommendation-worker
```

The command runs in a dedicated deployment/pod using the same binary and image
as the processor, with separate resource requests/limits and independently
configured replica count. This is a new native worker role; the existing
`recommendation-poller` remains unchanged and Kruize-only.

Workers claim ready jobs in a short database transaction (for example,
`FOR UPDATE SKIP LOCKED`), mark the job running with a lease, commit the claim,
then release the claim transaction before doing expensive recommendation work.
The engine reads persisted digests and writes recommendations using the
existing native engine paths. A worker crash leaves an expiring lease; another
worker can reclaim the job after that lease expires.

The worker must not run two recommendation jobs for the same cluster
concurrently unless the engine/write paths are shown safe for that case.
Cross-replica cluster serialization/lease mechanics are an implementation
decision that must be resolved before coding. Jobs for different clusters may
run concurrently within a configured, measured bound.

### Retry, idempotency, and completion

- Successful computation marks the job completed only after all required
  recommendation writes and required post-processing succeed.
- Retryable failures return the job to a future available time with an
  incremented attempt count and bounded backoff.
- After a configured retry limit, mark the job failed, retain diagnostic
  context, and expose an operator-visible alert/replay path. Do not silently
  discard it. The exact retry limit and backoff remain open.
- Reprocessing must be idempotent. Before implementation, audit every engine
  writer and history/analytics side effect for the crash window “recommendation
  writes committed, job completion not committed.” Existing upserts alone do
  not prove that append-only history writes are duplicate-safe.
- A manual requeue operation or documented administrative procedure must be
  able to safely return failed jobs to pending without duplicating or losing
  recommendation history.

### Manifest completion and synthesized IDs

- Publisher-supplied manifest IDs: enqueue only after existing expected-file
  tracking says every expected file is done. Preserve current behavior for
  incomplete or failed files.
- Synthesized `synth-*` IDs: replace the process-local quiet-period timer with
  durable `available_at` scheduling. Every new file/activity for the same
  synthetic manifest resets the due time. A worker restart must not lose the
  scheduled recommendation.
- Ensure duplicate Kafka deliveries cannot create duplicate active jobs.
- Preserve current report-type selection from completed file statuses.
- Add a recovery check for a complete manifest with no corresponding job, to
  cover a crash between per-file completion and job insertion. The upload
  message is currently not committed until its handler returns, which provides
  a replay path, but the recovery check also protects synthesized/debounced
  and operational repair cases.

## Acknowledgement and failure boundaries

The required durability order is:

```text
CSV-derived samples/digests committed
  → file/manifest state records completion
  → recommendation job committed
  → upload Kafka event may be committed
```

Recommendation execution may happen after upload acknowledgement because the
database job is durable. If digest persistence or job persistence fails, the
upload event must not be treated as fully accepted. If recommendation
execution fails, the persisted inputs and job remain available for retry; the
upload event does not need to be downloaded again.

This design does not make CSV download/digest ingestion independent of
PostgreSQL availability, and does not protect against the upstream Koku/Kafka
event never being produced. It also does not move CSV parsing/digest CPU off the
upload processor.

## Performance and capacity considerations

Expected benefit: the upload consumer no longer waits for native recommendation
CPU, digest-table reads, recommendation writes, and post-processing. This can
reduce upload-handler tail latency and head-of-line blocking when recommendation
work is the dominant processor cost.

Expected costs/limits:

- Total recommendation computation is unchanged; there is additional job-row
  insertion, claim/update traffic, and worker polling/metrics.
- The processor and recommendation worker still use the same PostgreSQL
  service. If database saturation is the bottleneck, splitting processes alone
  may not improve throughput and can increase connection/query contention.
- A separate deployment has a separate pool; account for
  `replicas × ROS_DB_MAX_CONNS` across processor, recommendation worker, API,
  and housekeeper when sizing PostgreSQL.
- Recommendation visibility becomes asynchronous. API consumers may observe
  the previous recommendation set between digest persistence and job
  completion; the acceptable lag/SLO needs an owner decision.
- Do not coalesce or skip runs in the initial change to claim a performance
  gain; measure before considering that optimization.

Before/after evaluation should compare production-like manifests and report at
least:

- Upload processing p50/p95/p99 and messages/second.
- Upload-topic consumer lag and age of the oldest unprocessed event.
- Recommendation job wait time, execution duration, retries, and oldest
  pending-job age.
- Processor and worker CPU/memory, PostgreSQL connection acquisition, query
  latency, saturation, and lock/wait indicators.
- Recommendation output parity and completion lag.

Existing metrics include `rosocp_pipeline_phase_duration_seconds`,
`rosocp_pipeline_total_duration_seconds`,
`rosocp_recommendation_duration_seconds`,
`rosocp_db_query_duration_seconds`, and Kafka consumer lag. Add bounded-label
metrics for job queue depth/age, job processing duration, retries, and terminal
failures; never use org, cluster, or manifest IDs as metric labels.

No numeric throughput improvement or worker-concurrency default is asserted
until a baseline is collected. Accuracy must remain unchanged.

## Deployment and rollout

- Reuse the existing `rosocp` binary and image; add a new start command and a
  ClowdApp deployment role with independent replicas/resources.
- Confirm the supported on-prem deployment owner/source of truth before
  implementation; do not assume the deprecated `cost-onprem-chart` is the
  forward deployment surface.
- Use a rollout mode that guarantees exactly one native recommendation path is
  active per manifest: inline or asynchronous, never both. The mechanism
  (temporary config gate versus staged release wiring) is open.
- Roll out schema before code that writes jobs; deploy the worker before
  enabling async enqueueing; then canary the async mode and verify input/job
  durability and recommendation parity.
- Define rollback behavior for pending/running jobs before rollout. A rollback
  must not silently strand jobs or run both inline and async recommendations.
- Leave the legacy Kruize poller and its topic/behavior unchanged.

## Acceptance criteria

1. For the same persisted input data and configuration, native recommendation
   outputs match the existing inline path for all enabled engines/plugins.
2. Upload processing can finish and commit after digest persistence and durable
   job creation, without waiting for recommendation computation.
3. No job is runnable before all manifest files required by current behavior are
   done; multiple-file manifests do not produce partial-data recommendations.
4. Duplicate upload deliveries create no duplicate active job and do not lose
   file data.
5. Restart tests cover: before job creation, after job creation/before upload
   commit, after claim, during computation, after recommendation writes/before
   marking completed, and synthesized quiet-period scheduling.
6. Retryable failures remain recoverable; exhausted jobs are visible and
   replayable.
7. Same-cluster recommendation execution is serialized or proven safe under
   concurrency; unrelated clusters can use bounded parallel capacity.
8. PostgreSQL connection budgets include all worker replicas and existing
   deployments.
9. Performance evaluation demonstrates the impact on upload latency/lag and
   recommendation freshness without output/accuracy regression.
10. SaaS deployment, on-prem deployment ownership, monitoring/runbooks, and
    cross-repository CSV/Kafka contracts are checked before release.

## Open decisions for owner review

1. **Failure policy:** retry count/backoff, terminal failure alert, and manual
   replay procedure.
2. **Ordering and concurrency:** one active run per cluster; whether jobs are
   FIFO and whether any later manifest may supersede an earlier one. The draft
   assumes no coalescing until separately approved.
3. **Freshness SLO:** acceptable time from digest persistence to recommendation
   availability, plus backlog alert threshold.
4. **Rollout switch and rollback:** exact async-mode control and operational
   procedure for jobs already pending during rollback.
5. **Deployment coverage:** Clowder/SaaS and the supported on-prem deployment
   surface for the new process.
6. **Job retention:** how long completed/failed job metadata remains available
   for diagnosis and replay.

## References checked

- `internal/services/report_processor.go` — upload processing and
  recommendation call before upload offset commit.
- `internal/services/parallel_ingest.go` — per-file download/digest path and
  file completion tracking.
- `internal/ingestion/pipeline.go` — CSV parsing and digest persistence.
- `internal/services/manifest_recommendations.go` — manifest completeness and
  native engine dispatch.
- `internal/services/manifest_recommendation_debouncer.go` — current
  in-memory synthesized-manifest quiet period.
- `internal/model/report_file_status.go` — expected files, completion checks,
  and completed report types.
- `cmd/start.go` and `clowdapp.yaml` — existing process roles and shared image
  deployment pattern.
- `docs/adr/0165-defer-recommendations-for-synthesized-manifests.md`,
  `docs/adr/0166-report-file-status-manifest-gating.md`, and
  `docs/adr/0088-kafka-s3-pipeline-both-modes.md` — existing decisions to
  preserve or explicitly supersede.
- `docs/operations/monitoring.md` and `internal/metrics/metrics.go` — current
  process roles and performance metrics.
