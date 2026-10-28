# Native Recommendation Worker Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let ROS persist CSV-derived recommendation inputs and a durable job, acknowledge the upload event, and generate native recommendations in a separately scalable worker.

**Architecture:** Keep per-file CSV download, parsing, digest calculation, and PostgreSQL persistence in the upload processor. Replace inline native recommendation execution with a PostgreSQL-backed manifest job; a new `rosocp start native-recommendation-worker` process claims jobs, reads the persisted digests, and executes the existing manifest recommendation path. Use the same binary/image, bounded worker concurrency, per-cluster serialization, and retry-safe writes.

**Tech Stack:** Go 1.26.9, Cobra, pgx/pgxpool, PostgreSQL 16, golang-migrate, Prometheus, ClowdApp.

**Spec:** `docs/superpowers/specs/2026-10-28-native-recommendation-worker-design.md`

**Issue:** [#708](https://github.com/pgarciaq/ros-ocp-backend/issues/708)

**Status:** Draft for review. No implementation, commit, or push is authorized by this plan.

## Global Constraints

- “Moving CSV download, parsing, or digest calculation out of the upload consumer” is a non-goal; “This proposal isolates recommendation execution only.”
- “Do not copy presigned URLs, CSV contents, or the full upload Kafka payload into the job row.”
- “The initial queue is PostgreSQL-backed directly” and “Do not add a native-recommendation Kafka topic in the first version.”
- “Commit the upload Kafka event only after that job write succeeds.”
- “For publisher-supplied manifest IDs, completeness is determined by manifest metadata (expected file count)” and synthesized IDs retain the quiet-period behavior, durably.
- “Reprocessing must be idempotent.” All recommendation and history side effects must be safe to repeat for the same job.
- “The worker must not run two recommendation jobs for the same cluster concurrently unless the engine/write paths are shown safe for that case.” Do not coalesce or skip jobs in this version.
- “No numeric throughput improvement or worker-concurrency default is asserted until a baseline is collected. Accuracy must remain unchanged.”
- Bound worker concurrency and account for `replicas × ROS_DB_MAX_CONNS` across all ROS deployments.
- No new image is needed. The processor and worker use the same `rosocp` binary/image with different start commands.
- Initial deployment work targets Clowder/SaaS. Do not modify the deprecated `cost-onprem-chart`; identify the supported on-prem deployment surface before enabling this worker there.
- Do not commit or push implementation work without explicit owner instruction.
- Regression tests for correctness/data-loss risks must fail pre-fix for the intended mechanism; follow `docs/agents/adversarial-fixtures.md`.

## Proposed Calls for Review

These are plan recommendations, not previously approved product requirements:

- Keep the async mode disabled by default (`ROS_NATIVE_RECOMMENDATIONS_ASYNC=false`) for staged rollout; never run inline and async recommendations for the same manifest.
- Serialize same-cluster jobs FIFO; run jobs for different clusters in parallel only within configured bounds.
- Preserve current `synth-*` quiet-period behavior and make it durable; use a generation counter so a file arrival during a running job schedules a follow-up run rather than being lost.
- Proposed worker settings: one in-flight job per pod, a 1-second empty-queue poll, a 120-second lease renewed every 30 seconds, six total attempts (initial plus five retries), and exponential retry backoff capped at five minutes. Measure idle polling/lease behavior before rollout; these defaults remain reviewable.
- Retain completed/failed job metadata for 30 days; never expire pending/running jobs. Confirm this retention call before rollout.
- Do not set a recommendation-freshness SLO before collecting the existing phase/lag baseline; present a target proposal for owner review before production rollout.

## Test Plan Overview

| Area | Proof |
|---|---|
| Job store and migration | PostgreSQL integration tests for dedupe, state transitions, due times, lease recovery, cluster serialization, retention, and migration up/down |
| Upload handoff | Service tests prove complete manifests persist a job before upload acknowledgement and incomplete manifests do not schedule work |
| Synthesized manifests | Restart and generation tests prove the quiet-period due time survives process loss and late file activity schedules another run |
| Worker/retries | Tests cover claim, heartbeat, shutdown, retry/backoff, terminal failure, manual requeue, and duplicate delivery |
| Recommendation idempotency | Integration tests replay the same job across all append-only history writers and prove no duplicate side effects |
| Output parity | Same CSV fixtures/configuration through inline characterization and async worker produce equivalent recommendation/API data |
| Deployment/observability | ClowdApp template validation, bounded-label metric tests, docs lint/drift, and a canary checklist |
| Performance | Compare upload phase duration/consumer lag and recommendation wait/duration, CPU, and PostgreSQL saturation; no numeric benefit asserted until baseline review |

`testutil.SetupTestDB` skips PostgreSQL tests under `-short`; run the targeted integration tests without `-short` in an environment with Docker/testcontainers. `make test-short` remains the fast unit/regression check.

## Review Focus

1. **Digest/job/offset crash window:** digest rows may be committed before the job row; upload replay must safely finish scheduling without losing or double-ingesting data.
2. **Recommendation write/job-completion crash window:** a worker may have committed some/all recommendation outputs before it can mark the job complete; append-only histories must not duplicate on retry.
3. **Synthesized manifest activity during a run:** a later file must reset durable quiet time and must not be swallowed when the current generation completes.
4. **Two workers claiming the same cluster:** job-row locks alone are insufficient if different manifests for one cluster are claimed concurrently; the database must enforce the cluster-level single-flight rule.
5. **Tenant/identity collisions:** identical manifest strings in different organizations or clusters must not deduplicate across tenants; job key and tests must vary those dimensions.

---

### Task 1: Add the durable recommendation-job schema and store

**Files:**
- Create: `migrations/000216_native_recommendation_jobs.up.sql`
- Create: `migrations/000216_native_recommendation_jobs.down.sql`
- Create: `internal/model/native_recommendation_job.go`
- Create: `internal/model/native_recommendation_job_test.go`
- Modify: `scripts/lint_migrations_test.go` only if the existing sequence/lint contract requires a fixture update

**Interfaces:**
- `RecommendationJobStore` owns all SQL for queue state transitions and terminal-row cleanup.
- `RecommendationJobRequest` contains `OrgID`, `ClusterUUID`, `ManifestID`, `AvailableAt` (UTC), and `Reschedule` (true only for `synth-*` activity); it contains no URLs, CSV bytes, or Kafka payload.
- `RecommendationJob` exposes the persisted ID, dedupe key, state, generation, attempt count, due time, lease owner, and lease expiry.
- Implement `EnqueueOrReschedule(ctx, request)`, `ClaimNext(ctx, workerID, now, leaseDuration)`, `RenewLease(ctx, id, workerID, generation, leaseUntil)`, `Complete(ctx, id, workerID, generation)`, `Retry(ctx, id, workerID, generation, availableAt, cause)`, `Fail(ctx, id, workerID, generation, cause)`, and `Requeue(ctx, id)`.

- [ ] **Step 1: Write PostgreSQL tests for the job lifecycle**

  Add `TestRecommendationJobStore_EnqueueIdempotentByTenantClusterManifest`, `TestRecommendationJobStore_ClaimSkipsFutureJobs`, `TestRecommendationJobStore_ClaimSerializesClusterAcrossWorkers`, `TestRecommendationJobStore_ReclaimsExpiredLease`, `TestRecommendationJobStore_RescheduleDuringRunKeepsNewGeneration`, and `TestRecommendationJobStore_PruneOnlyTerminalRows`. Assert same manifest text with different org/cluster creates distinct rows and stale owner/generation updates affect no row.

- [ ] **Step 2: Run the targeted tests and confirm the intended failures**

  Run: `go test ./internal/model -run 'TestRecommendationJob' -count=1 -v`

  Expected: FAIL because the job model/store and table do not exist. For behavioral regressions, keep the failure tied to duplicate/claim/lease semantics, not fixture ordering.

- [ ] **Step 3: Add migration 000216 and its reverse**

  Create `native_recommendation_jobs` with a surrogate key; `org_id`, `cluster_uuid`, `manifest_id`; constrained `pending|running|completed|failed` state; `available_at`; `generation` and `claimed_generation`; `attempt_count`; `lease_owner`/`lease_until`; `created_at`, `updated_at`, `completed_at`; and bounded `last_error`. Add a unique natural key on `(org_id, cluster_uuid, manifest_id)`, a partial ready-job index, a partial unique index on `(org_id, cluster_uuid)` where `status='running'`, and a terminal-retention index. Add reverse migration that removes only objects introduced by 000216.

- [ ] **Step 4: Implement the store transitions**

  Use short transactions and `FOR UPDATE SKIP LOCKED` for claims. Do not hold a claim transaction open while recommendations execute. A synthesized-manifest reschedule increments `generation` and updates `available_at`; completion of an older claimed generation returns the row to pending when a newer generation exists. Enforce the partial unique active-cluster constraint and handle its conflict as “try another job,” not as a lost job. Delete only completed/failed rows older than 30 days in bounded batches; leave pending/running rows untouched.

- [ ] **Step 5: Run tests and migration checks**

  Run: `go test ./internal/model -run 'TestRecommendationJob' -count=1 -v` and `go test ./scripts -run TestLintMigrations -count=1 -v`

  Expected: PASS; verify up/down against a disposable PostgreSQL 16 database and verify the claimed-row query uses the ready index with `EXPLAIN` on representative seeded rows.

### Task 2: Make recommendation side effects idempotent for a job retry

**Files:**
- Modify/test: `internal/services/manifest_recommendations.go`, `internal/services/report_processor_analytics_test.go`
- Modify/test: `internal/engine/container/history.go`, `internal/engine/history_test.go`
- Modify/test: `librobne/pgrec/vm.go`, VM history tests under `internal/engine/vm*history*test.go`
- Modify/test: `internal/engine/quota/quota_rec_history.go`, `internal/engine/quota/cluster_quota_rec_history.go`
- Modify/test: `librobne/pgrec/gpu_timeslicing.go`, `internal/engine/gpu_timeslicing_history_test.go`
- Sync: `vendor/github.com/redhatinsights/ros-ocp-backend/librobne/pgrec/vm.go` and `vendor/github.com/redhatinsights/ros-ocp-backend/librobne/pgrec/gpu_timeslicing.go`
- Create: `migrations/000217_native_recommendation_history_job_id.up.sql` and `.down.sql`
- Modify/test as needed: `internal/services/manifest_recommendations.go`, `internal/engine/vm/vm_runner.go`, `internal/engine/quota_run.go`, `internal/engine/cluster_quota_run.go`, and `internal/engine/gpu/timeslicing_persist.go`

**Interfaces:**
- Recommendation execution receives the claimed `jobID`/generation as a run identity; non-job callers, including `cmd/robne`, continue to use their existing behavior through compatibility wrappers.
- Existing recommendation outputs remain keyed as today. Job identity is only for retry idempotency and does not become an API field.

- [ ] **Step 1: Add a failing replay characterization test**

  Add `TestRunManifestRecommendations_RetrySameJobDoesNotDuplicateHistory`. With a PostgreSQL fixture covering container, VM, quota, cluster-quota, and GPU time-slicing histories, execute the same logical recommendation job twice and assert live recommendation values and history row counts/contents are unchanged after the second run. Use distinct entity names and values so `MAX`, `DISTINCT`, or key collisions cannot hide duplicates.

- [ ] **Step 2: Run the targeted test and verify it fails for duplicate history rows**

  Run: `go test ./internal/services ./internal/engine -run 'Test.*Recommendation.*Replay|Test.*History.*Job' -count=1 -v`, then `go test -C librobne ./pgrec -run 'Test.*History.*Job' -count=1 -v`

  Expected: the current append-only history paths fail the replay assertion; inspect the failure to confirm it identifies duplicate history writes rather than a missing fixture.

- [ ] **Step 3: Add idempotency keys to append-only history writes**

  Container `recommendation_history` already upserts on its stable daily identity; preserve that. In migration 000217 add nullable `recommendation_job_id BIGINT` to `vm_recommendation_history`, `quota_recommendation_history`, `cluster_quota_recommendation_history`, and `node_gpu_timeslicing_recommendation_history`. Add partial unique indexes for non-NULL job IDs using each table's natural entity key: VM `(job_id, org_id, cluster_id, vm_name, namespace, term, engine)`; quota `(job_id, org_id, cluster_uuid, namespace, quota_name, resource)`; cluster quota `(job_id, org_id, cluster_uuid, cluster_quota_name, resource)`; time-slicing `(job_id, org_id, cluster_uuid, node_name, gpu_model, term)`. Pass job identity to worker-only writer entrypoints and make same-job conflicts update the existing snapshot. Keep existing exported pgrec APIs as non-job wrappers for `cmd/robne`/other callers; do not add a foreign key from retained history to expiring job metadata. After local librobne changes, run `go mod vendor` and inspect the generated vendor diff; do not edit generated copies manually. Scan the full manifest dispatch tree before finalizing migration 000217 so no other append-only side effect is missed.

- [ ] **Step 4: Re-run replay tests and mutation-check the regression**

  Run the targeted test green, then temporarily remove the job-identity conflict handling and verify the test fails specifically with duplicate rows; restore the fix and rerun. Record the evidence in the test/commit notes when implementation is authorized.

### Task 3: Enqueue only after durable manifest completion

**Files:**
- Create: `internal/services/native_recommendation_jobs.go`
- Modify: `internal/services/report_processor.go`
- Modify: `internal/services/report_file_tracker.go`
- Modify/test: `internal/services/report_file_tracker_test.go`
- Modify: `internal/model/report_file_status.go`
- Modify: `internal/services/manifest_recommendations.go`
- Modify: `internal/services/manifest_recommendation_debouncer.go`
- Modify/test: `internal/services/manifest_recommendations_test.go`
- Modify/test: `internal/services/manifest_recommendation_debouncer_test.go`
- Modify/test: `internal/services/report_processor_test.go`
- Modify/test: `internal/model/report_file_status_test.go` only for manifest contracts not already covered

**Interfaces:**
- `scheduleNativeRecommendationJob(ctx, pool, kafkaMsg)` checks the current manifest gate and calls `RecommendationJobStore.EnqueueOrReschedule`.
- A real manifest is due immediately only when all expected files are `done`.
- A synthesized manifest is scheduled at `now + ROS_SYNTH_MANIFEST_QUIET_PERIOD`; later file activity updates its generation/due time.
- The worker will call the existing engine dispatch through a small metadata type containing only org, cluster, and manifest identity.

- [ ] **Step 1: Write tests proving real manifests are queued, not executed inline**

  Add `TestScheduleNativeRecommendationJob_RequiresCompleteManifest`, `TestScheduleNativeRecommendationJob_DeduplicatesReplay`, `TestScheduleNativeRecommendationJob_DoesNotRunEngineInline`, and a model test proving `MarkReportFileDone` errors on DB failure or zero affected expected rows. Assert incomplete/pending/failed file status produces no runnable job; all-done status produces one due job; duplicate delivery does not create a second active real-manifest job; and the engine hook has not run at the upload scheduling boundary.

- [ ] **Step 2: Run the targeted service tests and verify failure**

  Run: `go test ./internal/services ./internal/model -run 'Test.*Manifest.*(Job|Complete|Incomplete)' -count=1 -v`

  Expected: FAIL because the current path invokes the engine or has no durable job state.

- [ ] **Step 3: Replace inline scheduling with durable enqueue in async mode**

  Add the queue adapter and preserve `IsManifestIngestionComplete`/`CompletedReportTypes` semantics. On enqueue failure, return the error to `ProcessReport` so its existing transient retry/DLQ behavior applies; do not log-and-commit an event whose durable trigger failed.

- [ ] **Step 4: Propagate report-file completion failures**

  Change `model.MarkReportFileDone` to report an update that affected no expected row; return errors from `markFileDone` and propagate them through every caller in `parallel_ingest.go` and the VM-file batch path. Classify these persistence failures as transient so upload replay reuses the already-persisted digest upserts and retries the status/job transition. Use a caller grep to prove all 21 current `markFileDone` call sites handle the returned error. Do not alter the existing permanent-file-failure policy.

- [ ] **Step 5: Replace the synthesized in-memory timer with persisted due-time updates in async mode**

  Persist each synthetic manifest generation/due time before the upload event can be committed. Remove the timer-only path from async mode while retaining the inline path behind the rollout switch. A new activity during an active generation must cause a pending follow-up job when that generation completes.

- [ ] **Step 6: Prove the upload acknowledgement boundary**

  Extract a testable processing/ack decision seam around `ProcessReport` if needed. Verify the upload event is eligible for commit only after digest/file status and job row writes succeed; inject queue-store failure and prove the message is sent through the existing transient retry path. Verify replay skips files already marked done and still schedules the missing job.

- [ ] **Step 7: Run targeted tests**

  Run: `go test ./internal/services ./internal/model -run 'Test.*(ProcessReport|Manifest|RecommendationJob)' -count=1 -v`

  Expected: PASS with output parity hooks and no inline engine call in async mode.

### Task 4: Implement the native recommendation worker process

**Files:**
- Create: `internal/services/native_recommendation_worker.go`
- Modify: `cmd/start.go`
- Modify: `internal/config/config.go`
- Modify/test: `internal/config/config_validation_test.go` and relevant config tests
- Create: `internal/services/native_recommendation_worker_test.go`
- Modify/test: `internal/services/manifest_recommendations_test.go`

**Interfaces:**
- `services.RunNativeRecommendationWorker(ctx, pool, workerConfig, runner)` owns polling, claims, lease renewal, retry/failure transitions, and graceful shutdown.
- `runNativeRecommendationJob(ctx, pool, job)` builds the manifest-scoped request, passes the `job.ID` to idempotent history writers, and invokes the existing recommendation engines/report-type dispatch.
- `rosocp start native-recommendation-worker` initializes the same runtime settings as `processor`, starts the metrics server, and runs the worker until SIGTERM.

- [ ] **Step 1: Add worker-loop tests with an injected runner/store**

  Add `TestNativeRecommendationWorker_CompletesReadyJob`, `TestNativeRecommendationWorker_SkipsFutureJob`, `TestNativeRecommendationWorker_RetriesTransientFailure`, `TestNativeRecommendationWorker_FailsAfterMaxAttempts`, `TestNativeRecommendationWorker_ReclaimsExpiredLease`, `TestNativeRecommendationWorker_RenewsLease`, `TestNativeRecommendationWorker_SerializesSameCluster`, `TestNativeRecommendationWorker_RunsDifferentClustersWithinLimit`, and `TestNativeRecommendationWorker_StopsOnShutdown`. Inject the engine runner and clock/store so the tests are deterministic.

- [ ] **Step 2: Run tests to confirm the worker behavior is absent**

  Run: `go test ./internal/services -run 'TestNativeRecommendationWorker' -count=1 -v`

  Expected: FAIL because the worker loop and command do not exist.

- [ ] **Step 3: Implement bounded claim/execute/complete loop**

  Use the store from Task 1. Keep claim and heartbeat transactions short; do not hold a database transaction over recommendation computation. Serialize by the database-enforced active-cluster constraint. Classify errors using `isTransientKafkaProcessingError`; persist retry time/error or terminal failure rather than dropping the job. Count the first claim as attempt 1; after six total attempts, mark failed. Schedule retries with exponential backoff starting at one second and capped at 300 seconds. Renew the 120-second lease every 30 seconds. Do not retry permanent input/constraint failures indefinitely.

- [ ] **Step 4: Implement the native command and shutdown behavior**

  Register `native-recommendation-worker` in `cmd/start.go`; initialize GPU/VM defaults and plugin registry exactly as the processor does; start Prometheus; on cancellation stop claiming, finish/cancel in-flight work within configured shutdown grace, and leave unfinished work lease-recoverable. Run bounded cleanup of terminal job rows older than 30 days at startup and daily; never purge pending/running rows.

- [ ] **Step 5: Run targeted unit and PostgreSQL integration tests**

  Run: `go test -short ./internal/services -run 'TestNativeRecommendationWorker' -count=1 -v`, then `go test ./internal/services -run 'TestNativeRecommendationWorker' -count=1 -v` with testcontainers available.

  Expected: short tests pass; PostgreSQL lifecycle/concurrency tests pass without skipped DB tests.

### Task 5: Add async-mode configuration and SaaS deployment wiring

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_validation.go`
- Modify/test: `internal/config/config_validation_test.go`
- Modify: `cmd/start.go`
- Modify: `clowdapp.yaml`
- Modify: `docs/operations/monitoring.md`
- Modify: `docs/operations/runbooks.md`
- Modify: `docs-site/monitoring.md` if the public process/deployment contract changes

**Interfaces:**
- `ROS_NATIVE_RECOMMENDATIONS_ASYNC` defaults to `false`; false preserves existing inline behavior, true persists a job and suppresses inline execution.
- Proposed worker envs/defaults: `ROS_NATIVE_RECOMMENDATION_WORKER_CONCURRENCY=1`, `ROS_NATIVE_RECOMMENDATION_POLL_INTERVAL_SECS=1`, `ROS_NATIVE_RECOMMENDATION_LEASE_SECS=120`, `ROS_NATIVE_RECOMMENDATION_MAX_ATTEMPTS=6` (including the initial attempt), and `ROS_NATIVE_RECOMMENDATION_JOB_RETENTION_DAYS=30`; lease heartbeat is 30 seconds and retry backoff is exponential, capped at 300 seconds.
- The new ClowdApp deployment uses `${IMAGE}:${IMAGE_TAG}` and starts `./rosocp start native-recommendation-worker`; its replica count and resource requests/limits are independently configurable from processor and legacy poller counts.
- Initial rollout is Clowder/SaaS. Do not add a topic or modify the deprecated `cost-onprem-chart`.

- [ ] **Step 1: Test configuration defaults and mode validation**

  Add `TestConfig_NativeRecommendationsAsyncDefaultsInline`, `TestConfig_NativeRecommendationsAsyncEnablesJobs`, and table tests for zero/negative worker concurrency, poll interval, lease, max attempts, and retention. Verify pool-budget warnings account for worker concurrency where configuration permits.

- [ ] **Step 2: Run config tests and confirm the async switch is not present**

  Run: `go test ./internal/config -run 'Test.*NativeRecommendation' -count=1 -v`

  Expected: FAIL before config is added.

- [ ] **Step 3: Add the async-mode switch and bounded worker settings**

  Add the environment fields/defaults and validation. Keep one in-flight job per pod; set deployment replicas independently rather than increasing per-pod concurrency before measuring PostgreSQL headroom. Validate positive poll/lease/attempt/retention values and ensure the heartbeat interval is shorter than the lease.

- [ ] **Step 4: Add the ClowdApp deployment role**

  Add a `native-recommendation-worker` deployment plus `NATIVE_RECOMMENDATION_WORKER_REPLICA_COUNT` and worker CPU/memory request/limit parameters in `clowdapp.yaml`; initialize worker resources to current processor defaults but keep them independently overridable. Use existing image/probe/metrics conventions and pass the async/worker settings. It must not subscribe to `RECOMMENDATION_TOPIC` or replace the Kruize poller.

- [ ] **Step 5: Verify deployment-template and docs consistency**

  Run the repository's YAML/template lint and `make docs-lint docs-drift`. Confirm no documentation calls the legacy recommendation-poller the native worker. Confirm the supported on-prem source of truth before on-prem rollout; stop for owner input rather than editing the deprecated chart.

### Task 6: Add job observability and operations procedures

**Files:**
- Modify: `internal/metrics/metrics.go`
- Modify/test: `internal/metrics/metrics_test.go`
- Modify: `docs/operations/monitoring.md`
- Modify: `docs/operations/runbooks.md`
- Modify: `docs-site/monitoring.md` for customer/operator-visible monitoring contract
- Optional after the convention survey: `dashboards/grafana-dashboard-insights-rosocp-general.configmap.yaml`

- [ ] **Step 1: Add tests for bounded-label job metrics**

  Add `TestRecommendationJobMetrics_BoundedLabels` and metric exposition assertions for queue depth by bounded state, oldest ready-job age, job execution duration, retries, and terminal failures. Assert metric labels never contain org, cluster, manifest, or job IDs.

- [ ] **Step 2: Add metrics and instrument enqueue/claim/complete/retry/fail**

  Use stable low-cardinality labels only. Preserve current pipeline phase metrics while adding a distinct job-wait/job-execution view so upload and recommendation latency are not conflated.

- [ ] **Step 3: Document the worker and recovery procedures**

  Update process-role tables, scrape/health endpoints, DB connection budgeting, pending/failed job diagnosis, safe manual requeue, synthesized-manifest behavior, and rollback/drain order. Keep internal details in `docs/`; mirror customer-relevant monitoring facts to `docs-site/` with adapted links and required Last verified stamp.

### Task 7: Validate rollout, output parity, and performance

**Files:**
- Modify: `docs/superpowers/specs/2026-10-28-native-recommendation-worker-design.md` to close reviewed decisions before implementation release
- Modify: `docs/testing/validating-native-engine.md` for SaaS worker topology and E2E steps
- Test: `internal/services`, `internal/model`, `internal/engine`, and `librobne/pgrec` integration suites

- [ ] **Step 1: Capture the inline baseline before enabling async mode**

  Record the current `rosocp_pipeline_phase_duration_seconds{phase="recommend"}`, `rosocp_pipeline_total_duration_seconds`, `rosocp_recommendation_duration_seconds`, `rosocp_kafka_consumer_lag_total`, processor CPU/memory, and PostgreSQL pool/query saturation for representative small and large manifests. Do not set an SLO from a single run.

- [ ] **Step 2: Run output-parity fixtures through inline and async paths**

  Use identical CSV/digest inputs and plugin/config settings; compare persisted recommendation sets and histories. Cover all enabled report types and multi-file manifest ordering.

- [ ] **Step 3: Exercise restart/retry failure matrix against PostgreSQL**

  Inject failures after digest commit/before job enqueue; after enqueue/before upload offset commit; after claim; during engine execution; after history writes/before job completion; and during synthetic quiet period. Verify recovery, no lost jobs, no duplicate history, and no partial-manifest recommendation.

- [ ] **Step 4: Deploy a canary with async mode disabled, then enable for a bounded cohort**

  Start the worker deployment first with no eligible jobs; verify health/metrics. Enable async enqueueing only after all processor replicas run the compatible build. Monitor job backlog age and upload Kafka lag; ensure inline and async paths never both run for one manifest.

- [ ] **Step 5: Compare performance and recommendation freshness**

  Compare the same workload mix against Step 1. Report ingestion latency/throughput, queue wait, recommendation completion age, CPU, and PostgreSQL saturation. No numeric improvement is a release claim until the owner accepts the baseline-derived target. If PostgreSQL saturation worsens, stop and present the tradeoff; do not tune concurrency or weaken output correctness without approval.

## Final verification commands

Run before implementation completion/release:

```bash
make test-short
make test                 # Full Postgres/testcontainers suite when available
make lint
make build                # CGO_ENABLED=1; processor and worker share rosocp
make docs-lint docs-drift
```

Run focused PostgreSQL tests without `-short` for job claims, leases, history replay, and worker restart cases. Record explicit skips/failures; do not report a short test as proof of database integration behavior.
