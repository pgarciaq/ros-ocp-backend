# Changelog

All notable API and behavioral changes to ROS-OCP-Backend are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Fixed

- Fleet summary cache: configurable capacity (`ROS_FLEET_SUMMARY_CACHE_CAPACITY`), Prometheus metrics (hits/misses/evictions/invalidations/lazy expiry), LRU lazy-expiry cleanup via `container/list`, and invalidation on threshold settings, business-hours settings, and savings recalculation triggers (adversarial review findings #65, #66, #69 resolved).
- Defer recommendation engines for synthesized manifest IDs (`synth-*`) until `ROS_SYNTH_MANIFEST_QUIET_PERIOD` (default 30s) expires with no new file registrations; metric `rosocp_manifest_recommendation_deferred_total` (adversarial review finding #61 resolved).
- Single-flight coalescing for savings recalc, reship, and threshold recalc now uses the latest caller parameters on trailing runs (finding #62 resolved).
- Fix IPv6 private address bypass in CSV URL SSRF protection (adversarial review finding #64 resolved).

### Added

- Notification code **77** (`SPARSE_DATA`, INFO): fires when `data_days` is at or below `sparse_data_threshold` (default 2) for container, namespace, node, and PVC recommendations — orthogonal to `LOW_CONFIDENCE` (code 1). Configurable via `sparse_data_threshold` on container/namespace Settings API (`ROS_CONTAINER_SPARSE_DATA_THRESHOLD`, `ROS_NAMESPACE_SPARSE_DATA_THRESHOLD`); migration `000143`.
- Internal endpoint audit logging and metric `rosocp_internal_endpoint_calls_total` (labels `endpoint`, `org_id`, `sa_name`); optional org allowlist via `ROS_INTERNAL_ALLOWED_ORGS` (finding #63 resolved mitigated).
- Advisory CI workflow [`.github/workflows/openapi-changelog-check.yml`](.github/workflows/openapi-changelog-check.yml): warns when API-affecting paths (see [`.github/openapi-paths.txt`](.github/openapi-paths.txt)) change without `openapi.json` updates, or when Go files change without `CHANGELOG.md` updates (finding #53 resolved).
- Advisory CI workflow [`.github/workflows/adr-reminder.yml`](.github/workflows/adr-reminder.yml): reminds authors to review or create ADRs when architectural paths change (see [`.github/architectural-paths.txt`](.github/architectural-paths.txt)) (finding #54 resolved).
- `govulncheck` CI workflow [`.github/workflows/govulncheck.yml`](.github/workflows/govulncheck.yml) on PRs and weekly schedule (finding #60 resolved).

### Changed

- Remove unused `aws-sdk-go` v1 phantom dependency (adversarial review finding #60 resolved as non-issue); migrate S3 readiness check to `aws-sdk-go-v2`.

- Cost-management entitlement middleware on v1 API routes: rejects requests without `entitlements.cost_management.is_entitled=true` unless `DEVELOPMENT=true` (finding #35 resolved).
- Internal tag endpoint bearer auth in db mode via `ROS_INTERNAL_TAGS_AUTH_REQUIRED` (default `true`) (finding #37 resolved).
- API shutdown context for async threshold/savings recalc and masu reship jobs with 30s drain grace (finding #47 resolved).
- In-memory LRU fleet summary cache with `ROS_FLEET_SUMMARY_CACHE_TTL` (default 300s), invalidated on recommendation ingest (finding #52 resolved).
- Serialized Kafka offset commits when `ROS_KAFKA_PARALLEL=true` via `kafka.CommitMessage` mutex (finding #57 resolved).
- Configurable history default date window `ROS_HISTORY_DEFAULT_DAYS` (default 30) when `start_date`/`end_date` omitted (finding #51 resolved).

- Adversarial due diligence review **v2.0** ([`docs/audits/adversarial-review.md`](docs/audits/adversarial-review.md)): fresh audit acknowledging v1.6 remediations (#1–#31) and documenting 29 new findings (#32–#60) across ingestion edge cases, GPU fleet-scale performance, auth hardening gaps, and governance.
- SSRF DNS fail-closed in production: unresolved hostnames block CSV fetch when `DEVELOPMENT=false` (adversarial review finding #34 resolved).
- Per-org single-flight coalescing for savings recalculation and business-hours reship with metrics `rosocp_savings_recalc_coalesced_total` and `rosocp_reship_coalesced_total` (finding #36 resolved).
- Bounded LRU cache for RBAC permissions with `ROS_RBAC_CACHE_MAX_ENTRIES` (default 500) and metrics `rosocp_rbac_cache_size`, `rosocp_rbac_cache_evictions_total` (finding #40 resolved).
- Architecture Decision Records: 162 ADRs in [`docs/adr/`](docs/adr/README.md) with index, covering engine, data model, API, ingestion, plugins, cost, tags, deployment, testing, security, Kafka, and configuration decisions (adversarial review finding #30 resolved).
- Bounded LRU cache for masu effective-rates with `ROS_COST_CACHE_MAX_ENTRIES` (default 1000) and metrics `rosocp_cost_cache_size`, `rosocp_cost_cache_evictions_total` (finding #29 mitigated).
- Architecture doc for deterministic recommendation IDs and org_id detail-query invariant (finding #27 verified).
- Threshold recalculation single-flight coalescing per `(org_id, recommendation_type)` with metric `rosocp_threshold_recalc_coalesced_total` (findings #11, #28 mitigated).
- Optional deep readiness checks: `ROS_READINESS_CHECK_KAFKA`, `ROS_READINESS_CHECK_S3` (default `false`); S3 bucket settings `ROS_READINESS_S3_*` (finding #17 mitigated).
- `ROS_API_MAX_NODE_RESULTS` (default `1000`) hard cap for node utilization and GPU time-slicing list endpoints (finding #22 mitigated).
- Migration CI lint [`scripts/lint-migrations.sh`](../scripts/lint-migrations.sh), [`docs/operations/large-table-migrations.md`](operations/large-table-migrations.md), and [`deploy/migrations/concurrent-index-job.yaml`](../deploy/migrations/concurrent-index-job.yaml) (finding #24 mitigated).
- Configurable strict analytics ingestion mode (`ROS_INGEST_STRICT_ANALYTICS`, default `true`): when enabled, history/quality write failures block recommendation persistence and Kafka offset commit (message retried). Set `false` for degraded mode.
- Security hardening env vars: `DEVELOPMENT`, `ROS_API_MAX_OFFSET`, `ROS_CSV_DENY_PRIVATE_NETWORKS`, `ROS_LOG_POISON_PAYLOAD`, `ROS_HOUSEKEEPER_SHUTDOWN_GRACE_SECS`.
- Startup validation for CSV SSRF allowlist, tag dev token, and tag SA allowlist (`ValidateSecurityConfig`, `ValidateTagAuthConfig`).

### Changed

- History CSV export uses `RECORD_LIMIT_CSV` (default 1000) instead of the paginated list limit (finding #50 resolved).
- Native container detail lookup uses indexed `container_id` only; pre-migration composite-key fallback removed (finding #59 resolved).
- GPU MIG list (`GET /recommendations/openshift/gpu/mig`) uses SQL key pagination (`ListGPUMIGKeysPage`) instead of loading all clusters into memory (finding #48 resolved).
- GPU time-slicing list rejects unsupported `order_by` values and uses SQL triple pagination for JSON and CSV exports; in-memory fleet fallback removed (finding #49 resolved).
- Plugin ingest hook failures set `clusters.ingest_hooks_failed` and expose `ingest_hooks_failed` / `ingest_hooks_failed_at` on container list responses (finding #43 resolved).
- Business-hours settings changes log masu reship cluster count and return a warning when re-ingestion is triggered (finding #46 resolved).
- `ROS_CSV_MAX_BODY_BYTES` default lowered from 512 MiB to 100 MiB (finding #56 resolved).
- CORS middleware restricts origins via `ROS_CORS_ALLOWED_ORIGINS`; production defaults deny cross-origin unless configured (finding #42 resolved).

- `ROS_INGEST_STRICT_ANALYTICS` now defaults to `true` (strict mode). Set `false` explicitly for degraded mode (finding #45 resolved).
- Kafka consumer no longer logs message payload prefixes at DEBUG; metadata only (finding #38 resolved).

- Legacy Kafka messages without `metadata.manifest_id` now receive a deterministic synthesized manifest ID (`synth-` prefix) derived from `(org_id, cluster_uuid, date)` or a payload fingerprint, enabling per-file tracking and recommendation gating. Emits `rosocp_ingest_manifest_id_synthesized_total` and a WARN log when synthesis occurs (adversarial review finding #32 resolved).

- Money formatting (`FormatCentsToAmount`) uses integer cents division instead of float64 to avoid display rounding errors (finding #26 mitigated).
- Container ingestion degraded mode now sets `clusters.analytics_incomplete`, emits structured warnings, and increments `rosocp_analytics_incomplete_total` when history or quality writes fail (adversarial review finding #9 mitigated).
- GORM now shares the pgxpool via `stdlib.OpenDBFromPool`; `ROS_DB_MAX_CONNS` governs all database connections per process (adversarial review finding #7 mitigated).
- ILIKE filter values escape `%`, `_`, and `\` with `ESCAPE '\\'` (finding #13 mitigated).
- CSV URL fetch requires explicit allowlist in non-development mode; private networks denied by default (finding #12 mitigated).
- Poison Kafka message logs redact payload by default; optional `ROS_LOG_POISON_PAYLOAD` for debug preview (finding #20 mitigated).
- Housekeeper handles SIGTERM/SIGINT with configurable grace period (finding #19 mitigated).
- History endpoints enforce `MAXIMUM_COUNT_PER_QUERY_PARAM` on filter params (finding #25 mitigated).
- `offset` query parameter capped at `ROS_API_MAX_OFFSET` (default 10000) (finding #14 mitigated).
- Tag auth: `ROS_TAGS_DEV_TOKEN` blocked outside development; empty SA allowlist blocked in api mode (findings #15, #16 mitigated).
- GPU time-slicing list uses SQL triple pagination (`CountNodeGPUTriples` / `ListNodeGPUTriplesPage`) instead of loading all clusters into memory (finding #22 mitigated).

---

## [2026-06-10]

### Added

- Kafka DLQ (Dead Letter Queue) support: messages that fail processing after 5 retries (configurable via `ROS_KAFKA_MAX_TRANSIENT_RETRIES`) are routed to `hccm.ros.events.dlq` with forensic metadata headers, unblocking the consumer partition.
- New Prometheus metrics: `rosocp_kafka_dlq_messages_total`, `rosocp_kafka_retries_total`
- New configuration: `ROS_KAFKA_MAX_TRANSIENT_RETRIES`, `ROS_KAFKA_DLQ_TOPIC`
- Incremental digest flush during streaming ingest (`ROS_INGEST_FLUSH_BATCH_SIZE`, default 1000) with metrics `rosocp_ingest_groups_in_memory`, `rosocp_ingest_flush_total`, `rosocp_ingest_flush_duration_seconds`
- Ingestion-specific DB statement timeout (`ROS_DB_INGEST_STATEMENT_TIMEOUT`, default 120s) via `SET LOCAL` on batch transactions; API timeout configurable via `ROS_DB_STATEMENT_TIMEOUT` (default 25s)

### Changed

- Kafka commit resilience: consumer commits offsets only after successful processing; transient failures retry with backoff before DLQ routing (findings #1 and #2 from adversarial review).
- Streaming ingest flushes container-day digest groups incrementally instead of holding the full map until EOF (adversarial review finding #8).

### Fixed

- Native list pagination dropping `workload_type` filter on re-join to `org_container_keys`
- `workload_type` and namespace tag list filters on the org-keys pagination path
- Namespace tag filters crashing legacy SQL path when tags enabled
- E2E blockers: terms handler, fleet summary counts, `workload_type` filter
- Large-cluster ingestion no longer hits the 25s global `statement_timeout` on batch upserts (adversarial review finding #21)

---

## [2026-06-01] — Phase 12: API Polish, Savings Unification & Snapshot

### Added

- Unified `MoneyAmount` savings format (`value` + `units`) across all list APIs with `meta.currency` on responses (migrations 000074, 000132–000138)
- `confidence_level` on node, GPU MIG, and GPU time-slicing recommendations (migration 000133)
- Keyset pagination for PVC and snapshot list endpoints with `has_next` / `next_cursor` (migrations 000134, 000139)
- Snapshot summary endpoint with `filter[recommendation_type]`, `group_by[namespace]`, MoneyAmount costs, and CSV export
- Snapshot settings API with `inventory_fresh_hours`, validation, and async recalculation
- `GET /recommendations/openshift/notification-codes` public catalog endpoint
- `GET /recommendations/openshift/machinesets` aggregation endpoint
- `GET /recommendations/openshift/namespaces/{id}/history` namespace recommendation history
- Node detail API with savings recalculation and fleet consolidation notifications
- Namespace quota recommendations extended for storage and pods with `capacity_freed` exposure
- Per-ResourceQuota identity and extended quota resources (storage, pods)
- Cluster-quota savings recalculation; quota and cluster-quota async recalc on settings PUT
- PVC `vm_name` from storage CSV ingestion exposed on PVC recommendation API
- `filter[term]` on container list API; normalized to `short_term` convention across node APIs
- `filter[project]` as canonical namespace filter alias across all ROS list endpoints
- CSV export (`format=csv`) on remaining list endpoints; expanded columns for PVC, VM, and container exports
- Tag filtering on VM, quota, cluster-quota, and history endpoints
- `ROS_TAGS_ENABLED` default changed to `true`
- Batch analytics refactored into explicit history and quality pipeline hooks
- Migration 000140: `report_file_status` for ingestion file tracking

### Changed

- Renamed `SavingsObject` to `MoneyAmount`; fleet savings `by_plugin` values migrated to structured format
- Savings stored as integer cents internally (P1 fixed-point migration path)
- Standardized two-decimal savings display in JSON and CSV (`currency` column alongside numeric value)
- Idle/zombie detection improvements with dual-engine integration tests

### Fixed

- GPU time-slicing pagination limiting expanded recommendations (triple SQL path and standard path)
- Container and namespace keyset pagination overlap producing duplicate rows
- `filter[term]` normalization on PVC list API
- Savings recalculation endpoint `conn busy` error under concurrent load
- Healthy PVC upserts now return empty `notification_codes` array instead of null
- GPU tag filter gating and tag SQL for list queries
- VM notification code descriptions aligned with `mapping.go`
- Migration roundtrip and `ReadOldRecommendations` test failures
- Staleness threshold default to 48h with `ROS_STALE_DATA_THRESHOLD_HOURS` alias
- `filter[stale]` on namespace list with `STALE_DATA` notification on stale rows
- Cluster-quota notification catalog filter codes; object-count wired into CRQ utilization, risk, and blocking

---

## [2026-05-31] — Phase 11: Virtual Machine Recommendations

### Added

- VM recommendations plugin (Preview/Beta): vCPU, memory, and disk right-sizing with notification codes 50–63 (migrations 000089–000107)
- VM monthly savings estimates from Koku `effective_rates`; VM savings in fleet summary with `vm_cost_per_month`
- VM placement recommendations: correlated workload detection, NUMA node memory heuristic, codes 60–63
- VM power-off scheduling recommendation
- VM storage tiering notifications (simplified) and Network QoS notifications (SR-IOV/DPDK suggestions)
- Sequential vs random disk I/O profiling for VMs
- Production-quality vGPU time-slicing recommendations with read-time savings
- VM history endpoint with retention behavior; VM settings API with `settings_locked` and dedicated `/settings/vm` routes
- GPU classification thresholds exposed in VM Settings API; `cpu_adaptive_margin_enabled` setting
- Server-side filters: `is_network_bound`, `guest_os`
- Backward compatibility for partial operator upgrades (GPU columns, `restart_count`, VM preferences)
- Comprehensive notification codes reference (internal + public docs-site)
- Dedicated `/settings/{type}` endpoints; `/settings/thresholds?recommendation_type=` deprecated

### Changed

- n1 network-optimized VM recommendations enabled (active, not deferred)
- vGPU profiles and `gpu_catalog.yaml` MIG profiles aligned with NVIDIA documentation
- OpenAPI spec comprehensively updated for VM, GPU, nodes, quotas, and all settings endpoints

### Fixed

- VM test plan counts and notification matrix coverage
- Unit test failures for partitions, notifications, and migrations
- `TestDefinitionsMatchDB`: sync notification codes 58–59

---

## [2026-05-27] — Phase 10: Quota & ClusterResourceQuota Recommendations

### Added

- Namespace quota recommendation plugin with `GET /recommendations/openshift/quota` list and detail endpoints
- Quota settings API with 3-tier resolution (org → cluster → namespace) at `GET/PUT/DELETE /recommendations/openshift/settings/quota`
- ClusterResourceQuota (CRQ) recommendation plugin with list, detail, and settings endpoints
- CRQ settings API with env-var defaults and 3-tier resolution
- `DetermineCSVType` refactor supporting nise-style filenames; integration test for filename detection
- Comprehensive quota and CRQ unit and integration tests
- OpenAPI spec, API cheatsheet, and Bruno collection entries for quota endpoints

### Fixed

- Quota plugin docs-site alignment, default values, registry messages, redundant hooks
- Quota used columns wired in ingestion with backward-compatible handling for older operator versions

---

## [2026-05-21] — Phase 8–9: Tags, Idle Detection, Business Hours & Performance

### Added

- **Tag filtering**: Koku-aligned `filter[tag:key]` and legacy `tag=key:value` syntax on container, namespace, node, GPU, VM, quota, CRQ, and history list APIs; `meta.warnings` on empty tag-filter results
- Tag sync receiver reading Koku tag tables directly (on-prem) with SA auth, full-replace semantics, and `GET /recommendations/openshift/tags/status` endpoint (migration 000082)
- Fleet savings summary `group_by[tag:key]` for per-tag-value container savings
- `ROS_TAGS_SYNC_MAX_BODY_MIB` env var (replaces undocumented `ROS_TAGS_SYNC_MAX_BODY_BYTES`)
- **Idle/zombie detection**: inline engine classification with DB persistence, settings API, notification codes, and GPU idle/zombie detection (migration 000083)
- Phased plugin execution model (Phase 1 Produce / Phase 2 Enrich / Phase 3 Post-process with barriers)
- **Business hours**: schedule domain logic, org/cluster/namespace settings API (`GET/PUT/DELETE`), dual-stream ingestion and recommendation engine, reship client with single-flight lock and retry poller, IANA timezone support via `tzdata` in container image (migrations 000076–000081)
- Env vars: `ROS_BUSINESS_HOURS_ENABLED`, `ROS_BUSINESS_HOURS_RESHIP_FORWARD_ONLY_FALLBACK`, `ROS_RESHIP_POLLER_INTERVAL_SECS`, `ROS_RESHIP_MAX_RETRIES`, `ROS_RESHIP_CONCURRENCY`
- **Threshold settings API** with per-org TTL cache and async recalculation after changes (migration 000073)
- **Keyset pagination** for container and namespace lists with opaque cursors, `has_next`, `next_cursor`, and `after` query parameter (migration 000079+)
- `org_container_keys` table for efficient list pagination at 200k+ containers per org
- Per-phase Prometheus histograms: `rosocp_pipeline_phase_duration_seconds`
- Counters: `rosocp_recommendations_written_total`, `rosocp_ingestion_errors_total`
- Structured logging with `org_id`, `cluster_uuid`, and `request_id`; Echo RequestID middleware
- Operational runbook (`docs/operations/runbooks.md`)
- MkDocs public documentation site (`docs-site/`) with CI deployment from feature branches
- Per-plugin configurable recommendation terms
- Centralized `internal/config.Config` struct replacing scattered `os.Getenv` calls
- Plugin registry with `ROS_ENABLED_PLUGINS` (fatal if both `kruize` and native plugins enabled)
- Gzip middleware for API responses over 1KB
- RBAC permissions in-memory cache with configurable TTL (`ROS_RBAC_CACHE_TTL`)
- Configurable Kafka consumer worker pool (`ROS_KAFKA_PARALLEL`, `ROS_KAFKA_WORKERS`)
- PostgreSQL `statement_timeout` (25s default) on pool and GORM connections
- pgx pool tuning env vars: `ROS_DB_MAX_CONNS`, `ROS_DB_MIN_CONNS`, `ROS_DB_ACQUIRE_TIMEOUT_SECS`, etc.

### Changed

- Recommendation pipeline refactored to streaming architecture (batch-of-500, O(batch) memory)
- All engine/ingestion packages use centralized `internal/logging` package
- API handlers use consistent `hlog` pattern with org_id + request_id context
- GPU filters pushed to SQL for correct pagination (removed in-memory post-query filtering)
- Fixed-point integer storage for savings (cents), GPU digest metrics (basis points), and node sizing (millicores/KiB)
- P0 query rewrites: filter `org_id` directly instead of `rh_accounts` join
- Performance optimizations: fused weighted percentile pass, parallel GPU/node processing, batched business-hours enrichment, request-scoped enrichment cache, eliminated separate COUNT query in list APIs, streaming CSV ingestion

### Fixed

- GPU pagination returning incomplete pages when filters applied post-query (#496)
- `rosocp_partition__missing_error_total` metric name double-underscore (#329)
- Missing ingestion error counter (#330)
- Engine math edge cases: negative savings, zero-division in margin (#256, #262, #263)
- `workload_type` missing from PKs causing silent data collisions (#346, #349)
- Business hours: namespace prune on PUT, reship clearing, cluster-scoped prune logic
- Threshold settings: `locked_fields` null handling, unknown field validation
- Integer cents and fixed-point math for monetary precision

---

## [2026-05-18] — Phase 6–7: Plugin Architecture & Production Hardening

### Added

- Plugin framework: registry, trait interfaces (Producer/Enrich/Retention/APIProvider), and plugins for container, namespace, GPU, node, PVC, snapshot, quota, cluster-quota, VM, and Kruize legacy marker
- `ROS_ENABLED_PLUGINS` replaces `ROS_USE_NATIVE_ENGINE` boolean
- Streaming recommendation pipeline with per-phase metrics and operational runbook
- GPU catalog extracted to YAML with unrecognized model alerting
- Upgrade runbook for Kruize-to-native migration safety
- Contract test for Koku `effective_rates` API
- `.env` file support via godotenv for local development
- Comprehensive developer guide in `CONTRIBUTING.md`
- Clowdapp database version bumped from 13 to 16

### Changed

- `KAFKA_AUTO_COMMIT` default flipped to `false` (manual commit after processing)
- GPU threshold globals replaced with `GPUThresholds` struct
- Background deletion of Kruize-era tables before cluster CASCADE

### Fixed

- P0 security: IDOR, SSRF hardening, fleet RBAC enforcement
- P0 data safety: snapshot reconcile guard, scoped partition creation
- P0/P1 pipeline reliability: transactions, batching, Kafka commits
- P1 API correctness and silent failures (503 on DB errors)
- P2 audit: cascade delete on source removal, node PK completeness, `/readyz` endpoint, Prometheus metrics, probe config, SQL-level pagination for node/GPU handlers
- Dead code and naming issues (#383, #386, #390, #391, #393, #396–#398)
- Migration safety: regex validation for `cluster_uuid::uuid` cast (000041), deadlock prevention between 000058 and `PersistNodeRecommendations`, safe index rebuild for 000045 on large databases
- Reconciliation audit: 52 unmarked P0–P2 issues resolved; 490-issue audit closed

---

## [2026-05-01] — Phase 6: Native Engine Feature Complete (v1.0)

Initial release of the native Go recommendation engine replacing Kruize for production workloads.

### Added

- **Container recommendations**: decay-weighted CPU/memory percentiles with adaptive margin, dual cost/performance outputs, idle detection, trend slope notifications
- **Namespace recommendations**: aggregate boxplots, P60/P98/P99 memory percentiles, memory trend slope notification (migrations 000031–000036)
- **GPU recommendations**: DCGM PROF_ profiling metrics (SM_ACTIVE, PIPE_TENSOR_ACTIVE, DRAM_ACTIVE), workload classification (idle, underutilized, memory_bound, well_utilized), MIG profile selection, Tier 1/Tier 2 model support (migrations 000042–000045)
- **GPU time-slicing**: node-level time-slicing recommendations with pagination, RBAC, and dollar savings (migration 000044)
- **GPU API restructure**: `/gpu/timeslicing`, `/gpu/mig`, `/gpu/summary`; node CPU/memory at `/nodes`
- **Node utilization**: underutilized, overcommitted, stranded resource detection with EMA-smoothed imbalance score; term-based windowing (short/medium/long)
- **Node right-sizing engine** with configurable thresholds and EMA smoothing (migrations 000052–000054)
- **PVC right-sizing**: oversized, near-full, orphaned, growth trend detection (migration 000048, 000064)
- **Snapshot staleness detection** end-to-end with inventory retention sweep
- **Idle/abandoned workload detection** with full savings estimation
- **Adoption detection** (15% tolerance matching)
- **Stale data detection** and lifecycle management
- **Estimated monthly savings** via Koku `effective_rates` integration including distributed costs
- **Replica count** (`pod_count_min/max/avg`) from operator `workload_pod_count` column (migration 000039)
- **Custom term configuration API**: `GET/PUT/DELETE /recommendations/openshift/settings/terms`
- **Unified settings API** with env-var locking and cost model gap analysis
- **Recommendation history and quality tracking** APIs with CSV export (migrations 000030+)
- **Fleet summary endpoint** with savings aggregation
- **Historical tracking**: partitioned `recommendation_history` and `recommendation_quality` tables with separate retention policies
- **Query-time boxplots** from raw `container_usage_samples` and `namespace_usage_samples`
- **Notification codes**: persisted definitions with API mapping (migration 000027); always returned in list/detail responses
- **OOM feedback**: logarithmic memory bump from `oom_count` CSV column (`ROS_OOM_BASE_BUMP`, `ROS_OOM_MAX_BUMP`)
- **Quality metrics**: stability, adoption, OOM rate, recommendation age
- **Kruize vs Native comparison tool** for quantitative algorithm verification
- **CSV export**, current values, stale detection on all container/namespace list endpoints
- **cluster_uuid TEXT → UUID** migration (000041) fixing join operator errors
- **Limit variation**, integer percentages, whole MiB rounding on recommendations
- **Composite indexes** for native list query performance (migration 000061)
- Container image updated to `ubi10/go-toolset:1.25`

### Changed

- Native list API response aligned with Kruize-compatible JSON shape for UI backward compatibility
- Legacy Kruize fallback removed from native recommendation handlers
- Namespace recommendations enabled by default with Unleash kill switch
- Namespace recommendation feature flag gating removed

### Fixed

- Container memory P60/P98/P99 parity: pipeline now stores all percentiles in `daily_container_digests` (migration 000035)
- Namespace memory trend slope notification (previously discarded by evaluator)
- Phase 6 critical audit: write bugs, integration tests, migration 000034
- GPU ingestion pipeline wired end-to-end (digest upsert, query, API enrichment)
- GPU savings: `$0` vs `null` semantics for well-utilized GPUs; org_id prefix mismatch calling Koku
- Short-term recommendations anchored to latest digest date
- Fleet summary query using `medium` term instead of `medium_term`
- Three correctness bugs in recommendation engine
- Migration renumbering conflicts resolved (000023–000043 range consolidated)

---

## [2026-04-30] — Phase 5: History, Boxplots & Retention

### Added

- Recommendation history tracking in partitioned monthly tables
- Raw `container_usage_samples` table for query-time boxplot assembly (exact five-number summaries via `percentile_cont`)
- Retention sweep for digests, samples, history, and quality tables with configurable periods (`ROS_RETENTION_MONTHS`, `ROS_HISTORY_RETENTION_DAYS`)
- Strongly-typed `DetailResponse` struct replacing raw JSON manipulation for Kruize-compatible UI shape

### Changed

- Native detail response matches Kruize-compatible UI JSON shape

### Fixed

- Test failures from Phase 5 DetailResponse shape change

---

## [2026-04-29] — Phase 4: OOM Feedback & Quality Tracking

### Added

- OOM bump for memory recommendations: `bump = 1 + 0.15 × log₂(1 + oom_count)`, capped at 1.60×
- Recommendation quality writer with 4 metrics: stability, adoption, OOM rate, recommendation age
- Auto-create digest partitions on first write
- E2E test for OOM pipeline (cross-repo: operator `oom_count` column, nise test data, backend parser)
- Safety clamps and tuple filter on quality writer keys including `WorkloadType`

### Changed

- Native CSV parser columns aligned with operator/nise output (`cpu_request`, `mem_request`, etc.)
- Legacy GORM query compatible with native engine rows
- API always returns `notification_codes` and `notifications` arrays (never omitted)

### Fixed

- Pipeline ordering: quality writer runs after recommendation write, skipped on read failure
- Compare tool uses operator column names and includes `oom_count`

---

## [2026-04-25] — Phases 1–3: Native Go Recommendation Engine Foundation

### Added

- Native Go recommendation engine with "read once, compute N terms" architecture
- Daily digest schema: `daily_container_digests`, `daily_namespace_digests` with RANGE monthly partitioning (migrations 000025–000028)
- Test infrastructure: testcontainers PostgreSQL 16, golang-migrate, deterministic fixtures
- CSV parsing with float→int64 conversion, NaN/Inf validation, stable row ordering
- Digest computation pipeline: exact percentiles on ~96 int64 values per day
- Decay-weighted percentile recommendations with adaptive margin and 25mc CPU floor
- Dual cost/performance recommendation outputs per term
- Notification code persistence and mapping (Phase 3)
- API fallback handlers, `container_id` migration, scale benchmarks (Phase 2)
- Exclude and exact filter support for container List API
- Percentage sorting columns for recommendations (RHINENG-20862, RHINENG-25638)
- Kruize vs Native Engine comparison tool and documentation

### Changed

- Go upgraded to 1.25; dependencies updated

---

## [2026-04-20] — Phase 0: Critical Robustness Fixes

### Added

- HTTP client timeout (30s default via `GLOBAL_HTTP_CLIENT_TIMEOUT_SECS`) on all outbound calls including Kruize REST
- Dead-letter handling for poison Kafka messages with max retry count
- Context cancellation checks in long-running CSV processing

### Fixed

- RBAC nil pointer panic when permissions service returns error
- API returning HTTP 200 on database failure (now 503)
- Kafka type assertion panics on unexpected message types
- Kafka subscribe failure silently ignored (now exits consumer)
- Non-deterministic CSV row order from map iteration
- GORM insert errors silently swallowed
- Date parse errors returning zero time instead of error
- Kafka payload logged at Info level (moved to Debug with truncation)
- SendMessage failure not propagated to caller

---

## Migration Reference

Operators upgrading from Kruize or earlier native-engine builds should run all migrations through **000140**. Key migration groups:

| Range | Purpose |
|-------|---------|
| 000025–000028 | Daily digests, notification codes, relational recommendation_sets columns |
| 000030–000036 | Usage samples, namespace percentiles, container memory P60/P98/P99 |
| 000039–000045 | Pod count, UUID types, GPU digests, node name on GPU digests |
| 000048–000064 | PVC and snapshot notification codes, PVC term column |
| 000052–000054 | Node digests and node recommendations |
| 000073–000074 | Threshold settings, savings integer cents |
| 000076–000083 | Business hours schema, tag sync metadata, idle state columns |
| 000089–000107 | VM recommendations, enhancements, savings, power schedule |
| 000110–000140 | Namespace schedule type, node idle state, PVC VM name, keyset indexes, savings cents renames, snapshot costs, report file status |

See `docs/operations/upgrade-runbook.md` for Kruize-to-native migration steps and pre-migration `CONCURRENTLY` index guidance for large production databases.
