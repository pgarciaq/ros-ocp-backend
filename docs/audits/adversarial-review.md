# Adversarial Due Diligence Review — ros-ocp-backend

> **INTERNAL USE ONLY** — This document is an internal security and engineering audit. It is not for public disclosure, customer distribution, or external publication without explicit security and legal review.

**Date:** 2026-06-10 (re-validation pass)  
**Previous review:** 2026-06-08 (v1.2)  
**Scope:** `ros-ocp-backend` — Kafka ingestion pipeline, native recommendation engine, REST API, database layer, authentication/authorization, operational readiness, and engineering governance  
**Methodology:** Adversarial due diligence combining static code review, architecture analysis, threat modeling (STRIDE-lite), and operational failure-mode analysis. Reviewers assumed the **SNO/dev deployment posture** (`ROS_TAGS_SOURCE=db`, `RBAC_ENABLE=false`, no gateway) with network access to the API port unless otherwise noted. Findings were validated against source locations and cross-referenced for compound failure chains.

**Changes since v1.3:** Implemented adversarial review findings **#12–#16, #19, #20, #23, #25** — CSV SSRF hardening, ILIKE wildcard escaping, offset cap, tag auth startup validation, housekeeper graceful shutdown, poison-message log redaction, panic-to-error for embedded catalogs, history filter cardinality limits. Prior: Finding #18 mitigation — Kafka retry-count headers, DLQ escalation after 5 transient retries, and `rosocp_kafka_dlq_messages_total` / `rosocp_kafka_retries_total` metrics (`internal/services/kafka_retry.go`).

---

## Deployment Context

ros-ocp-backend runs in three distinct deployment postures. Several findings in this review reflect **SNO/dev overrides** rather than production vulnerabilities in the default SaaS or on-prem chart configurations.

| Posture | Auth | RBAC | Tags source | Internal endpoints |
|---------|------|------|-------------|-------------------|
| **SaaS** (console.redhat.com) | 3scale validates JWT upstream | Enabled (`RBAC_ENABLE=true`) | `api` (push sync with SA auth) | Cluster-internal only |
| **On-prem chart (default)** | Envoy gateway validates JWT via JWKS, injects X-Rh-Identity | Enabled (`rbac.enabled: true`) | `db` (direct PG join) | NetworkPolicy restricted to gateway/UI |
| **SNO/dev overrides** | No gateway; direct API access | Disabled (`rbac.enabled: false`) | `db` | Unrestricted |

**Review scope:** This audit was conducted against the **SNO/dev posture**. Findings #3, #4, and #5 are mitigated or eliminated in the default production postures (SaaS and on-prem chart). Findings #6 and #16 reflect accepted platform architecture with optional hardening levers, not production gaps when compensating controls are in place.

---

## Table of Contents

1. [Executive Scorecard](#executive-scorecard)
2. [Priority Remediation Order](#priority-remediation-order)
3. [Findings by Deployment Posture](#findings-by-deployment-posture)
4. [Findings — High](#findings--high)
5. [Findings — Medium](#findings--medium)
6. [Findings — Low / Info](#findings--low--info)
7. [What Held Up Well](#what-held-up-well)
8. [Cross-Cutting Failure Scenario Matrix](#cross-cutting-failure-scenario-matrix)
9. [Tracking](#tracking)

---

## Executive Scorecard

| Area | Verdict | Summary |
|------|---------|---------|
| **Data integrity (Kafka ingestion)** | 🟠 High (mitigated) | Per-file tracking and error surfacing implemented in native path (`90e5ed52`); gaps remain for empty `manifest_id` and legacy Kruize path |
| **Authentication** | 🟢 Delegated to gateway | Accepted architecture; weak only if gateway bypassed (SNO/dev posture) |
| **Authorization** | 🟢 Strong when chart defaults used | `rbac.enabled: true` in production; weak only in SNO/dev overrides |
| **API security** | 🟢 Low (mitigated) | ILIKE wildcard injection, unbounded offset, SSRF when allowlist unset, history filter cardinality — mitigated (#12–#14, #25); pagination filter bypass (#31) fixed |
| **Database & connections** | 🟢 Low (mitigated) | Unified pgxpool for GORM and pgx paths; pool metrics exported |
| **Memory & performance** | 🟠 Medium | Streaming ingest holds full grouped map; node GPU endpoints paginate in memory |
| **Operational resilience** | 🟢 Low (mitigated) | Readiness probe shallow; Kafka transient errors retry/DLQ (#18); housekeeper graceful shutdown (#19 mitigated) |
| **Pipeline correctness** | 🟢 Low (mitigated) | Strict analytics mode + staleness signaling for history/quality gaps |
| **Engineering governance** | 🟡 Low–Medium | CHANGELOG exists; no ADR index; 140 migrations without CONCURRENTLY automation |
| **Positive controls** | 🟢 Strong | Plugin architecture, parameterized SQL, service-account auth patterns (when enabled), structured metrics, ingestion unit tests |

**Overall assessment:** The native engine and API surface are functionally mature. **Findings #1 and #2 are mitigated** for the native ingestion path via per-file `report_file_status` tracking (migration `000140`), surfaced ingestion errors, recommendation gating (`runManifestRecommendations`), and `ros_ingestion_file_failures_total` — matching Koku's `CostUsageReportStatus` pattern. Residual risk: operators must run `reship_ros` for stuck manifests; runbook for `report_file_status` recovery is not yet documented. **Finding #31** (workload_type filter bypass in keyset pagination) was discovered and fixed in `f66feaf7`. Auth findings (#3, #5, #6) remain deployment-specific shortcuts mitigated by gateway JWT validation, RBAC defaults, and NetworkPolicy in standard deployments.

---

## Priority Remediation Order

Remediation is ordered by **compound risk** (findings that amplify each other) and **blast radius** (data loss > auth bypass > availability > governance).

| # | Finding(s) | Rationale |
|---|------------|-----------|
| 1 | **#1, #2** (High — mitigated) | Per-file tracking, error propagation, recommendation gating, and alerting implemented. Residual: empty `manifest_id`, legacy Kruize path, manual `reship_ros` for failed files. |
| 2 | **#8, #21** (Medium — ingestion scale) | Memory accumulation and statement timeout both manifest under large-cluster ingestion; fix together to avoid OOM ↔ retry loops. |
| 3 | **#9** (High — mitigated) | Strict analytics mode, `rosocp_analytics_incomplete_total`, and API `analytics_incomplete` cluster flag. Default remains degraded-compatible. |
| 4 | **#11, #28** (Medium — recalc storms) | Concurrency capped at 3 per job but overlapping async jobs still possible after settings changes. |
| 5 | **#12, #13, #14** (Medium — API hardening) | **Mitigated** — SSRF allowlist + private-network deny, ILIKE escape, offset cap. |
| 6 | **#15, #16** (Medium — tag auth config) | **Mitigated** — startup validation; dev token blocked outside `DEVELOPMENT=true`. |
| 7 | **#17, #19, #20** (Medium — ops) | **Partially mitigated** — housekeeper SIGTERM (#19), poison log redaction (#20); readiness depth (#17) open. |
| 8 | **#22, #23** (Medium — memory/panic) | **Partially mitigated** — panic-to-error for catalogs (#23); node GPU in-memory pagination (#22) open. |
| 9 | **#24** (Medium — migrations) | CONCURRENTLY automation — plan for next large-table index. |
| 10 | **#30** (Info — governance) | ADR index — process debt, not incident drivers. |
| — | **#7** (Mitigated) | GORM uses `stdlib.OpenDBFromPool`; `ROS_DB_MAX_CONNS` governs all connections; pool metrics on scrape. |
| — | **#18** (Mitigated) | Retry-count headers + DLQ after 5 attempts; `rosocp_kafka_dlq_messages_total` for alerting. |
| — | **#10** (Resolved) | `CHANGELOG.md` exists at repo root. Optional: enforce OpenAPI diff in CI. |
| — | **#31** (Resolved) | Pagination filter bypass fixed in `f66feaf7`. |
| — | **#3** (Info — architecture) | Gateway enforcement in SaaS and on-prem chart. |
| — | **#4** (Medium — hardening) | Authenticate `/internal/*` in db mode — NetworkPolicy mitigates in default on-prem chart. |
| — | **#5, #6** (Low/Info — deployment-specific) | RBAC disabled and cross-tenant SA scope are SNO/dev overrides or accepted platform architecture. |

---

## Findings by Deployment Posture

Which findings apply to each deployment posture. ✓ = applies; ✗ = mitigated or not applicable; ⚠ = partially mitigated.

| Finding | SaaS | On-prem (default) | SNO/dev |
|---------|------|---------------------|---------|
| #1 Kafka commit | ✓ | ✓ | ✓ |
| #2 Error swallowed | ✓ | ✓ | ✓ |
| #3 Identity header | ✗ (gateway) | ✗ (gateway) | ✓ |
| #4 Tags unauth | ✗ (api mode) | ⚠ (db mode, NetworkPolicy) | ✓ |
| #5 No RBAC | ✗ (enabled) | ✗ (enabled) | ✓ |
| #6 SA any org | ✗ (by design) | ✗ (by design) | ✗ (by design) |
| #7 Dual pools | ✗ (mitigated) | ✗ (mitigated) | ✗ (mitigated) |
| #8 Memory grouped map | ✗ (mitigated) | ✗ (mitigated) | ✗ (mitigated) |
| #9 Pipeline degraded | ✗ (mitigated) | ✗ (mitigated) | ✗ (mitigated) |
| #10 No CHANGELOG | ✗ (exists) | ✗ (exists) | ✗ (exists) |
| #11 Recalc storms | ⚠ (concurrency cap) | ⚠ | ⚠ |
| #12 SSRF allowlist | ✗ (mitigated) | ✗ (mitigated) | ⚠ (dev allows empty) |
| #13 ILIKE injection | ✗ (mitigated) | ✗ (mitigated) | ✗ (mitigated) |
| #14 Deep pagination | ✗ (mitigated) | ✗ (mitigated) | ✗ (mitigated) |
| #15 Dev token | ✗ (blocked prod) | ✗ (blocked prod) | ⚠ (if configured + DEVELOPMENT) |
| #16 Empty SA allowlist | ✗ (blocked api mode) | ✗ (blocked api mode) | ⚠ (dev warning) |
| #17 Readiness shallow | ✓ | ✓ | ✓ |
| #18 Kafka stall | ⚠ (mitigated) | ⚠ (mitigated) | ⚠ (mitigated) |
| #19 Housekeeper shutdown | ✗ (mitigated) | ✗ (mitigated) | ✗ (mitigated) |
| #20 PII in logs | ✗ (mitigated) | ✗ (mitigated) | ✗ (mitigated) |
| #21 Statement timeout | ✗ (mitigated) | ✗ (mitigated) | ✗ (mitigated) |
| #22 Node GPU memory | ✓ | ✓ | ✓ |
| #23 panic() parse | ✗ (mitigated) | ✗ (mitigated) | ✗ (mitigated) |
| #24 Migrations CONCURRENTLY | ✓ | ✓ | ✓ |
| #25–#30 Low/Info | ⚠ (#25 mitigated) | ⚠ (#25 mitigated) | ⚠ (#25 mitigated) |
| #31 Pagination filter bypass | ✗ (fixed) | ✗ (fixed) | ✗ (fixed) |

---

## Findings — High

### Finding #1 — Kafka offset committed after partial file failure

| Field | Value |
|-------|-------|
| **Severity** | High (mitigated) |
| **Category** | Data integrity / Kafka consumer semantics |
| **Location** | `internal/services/report_processor.go`, `internal/model/report_file_status.go` |
| **Status** | **Mitigated** (verified 2026-06-10, commit `90e5ed52`) |
| **Effort** | M |

**Description**

The report processing loop iterates over files in a multi-file Kafka payload. When a single file fails permanently, the Kafka offset is still committed (by design, to avoid blocking the consumer group). Previously this caused silent data loss with no recovery path.

**Exploit / trigger**

Not attacker-driven. Any permanent S3/MinIO glitch, corrupt CSV, or missing object key on **one file** in a multi-file payload could permanently drop that file's data without operator visibility.

**Mitigation (implemented — verified in code)**

- `report_file_status` table (migration `000140`) tracks per-file state (`pending`, `processing`, `done`, `failed`) keyed by `(manifest_id, filename)`.
- Kafka messages carry `manifest_id` and `expected_files` (from Koku `ROSReportShipper`).
- Failed files recorded via `handlePermanentFileError` → `MarkReportFileFailed`; idempotent re-delivery skips files already `done` via `shouldSkipProcessedFile`.
- Recommendation engines **gated** in `runManifestRecommendations` until `IsManifestIngestionComplete` returns true.
- `ros_ingestion_file_failures_total` Prometheus counter with labels `org_id`, `cluster_id`, `report_type`, `error_class`.
- Unit tests in `internal/model/report_file_status_test.go` cover lifecycle and failed-file blocking.

**Residual risk / gaps**

- **Empty `manifest_id`:** When `manifestIDFromMsg` returns empty, all tracking is skipped (`ensureManifestExpectations`, `markFileProcessing`, `markFileDone` no-op). Legacy or malformed Kafka messages bypass per-file tracking entirely.
- **Legacy Kruize path:** Files processed via `ReadCSVFromUrl` + dataframe (when Kruize plugin enabled) do not use `report_file_status`; fetch/parse failures `continue` without permanent classification.
- Operators must manually intervene via Koku's `reship_ros` API to re-deliver failed files. Offset commit behavior is unchanged — the queue does not stall on partial failure.
- Operator runbook for querying `report_file_status` and triggering recovery is not yet in `docs/operations/runbooks.md`.

---

### Finding #2 — Native ingestion errors swallowed (return nil)

| Field | Value |
|-------|-------|
| **Severity** | High (mitigated) |
| **Category** | Data integrity / error propagation |
| **Location** | `internal/services/report_processor.go` — native ingest functions |
| **Status** | **Mitigated** (native path; verified 2026-06-10) |
| **Effort** | S |

**Description**

Non-transient ingestion errors (S3 403, fetch failures, parse errors) were logged and metrics incremented, but functions returned `nil` (success). The caller never learned the file failed.

**Exploit / trigger**

Compounded Finding #1: permanent fetch/parse failures appeared successful, preventing failure tracking and recommendation gating.

**Mitigation (implemented — verified in code)**

- Native ingest functions (`processContainerCSVIngest`, `processNamespaceCSVIngest`, etc.) return wrapped errors for permanent failures (e.g., `return fmt.Errorf("fetch container CSV: %w", err)`).
- `ProcessReport` classifies errors via `isTransientKafkaProcessingError`; permanent errors call `handlePermanentFileError` and `continue` to next file.
- Structured error logging via `recordFileFailure` with `org_id`, `cluster_uuid`, `report_type`, `error_class`.
- Unit test `TestConsumer_PresignedDownload403` asserts non-nil return on S3 403 and zero digest rows written.

**Residual risk**

- Same gaps as Finding #1: empty `manifest_id` and legacy Kruize path still swallow or misclassify failures.
- Recovery requires targeted `reship_ros` after investigating `report_file_status` and Prometheus alerts.

---

### Finding #7 — Dual DB connection pools (GORM + pgxpool)

| Field | Value |
|-------|-------|
| **Severity** | High |
| **Category** | Performance / reliability |
| **Location** | `internal/db/db.go`, `internal/metrics/pool_collector.go` |
| **Status** | **Mitigated** (verified 2026-06-10) |
| **Effort** | L |

**Description**

Two independent database connection pools existed: GORM (via `database/sql` defaults — no `SetMaxOpenConns`/`SetMaxIdleConns` configured) and pgxpool (tuned via `ROS_DB_MAX_CONNS`). Under load, one pool could exhaust connections while the other remained idle, or combined usage could exceed PostgreSQL `max_connections`.

**Mitigation**

GORM now wraps the shared pgxpool via `stdlib.OpenDBFromPool`. `GetDB()` initializes `GetPool()` first; `database/sql` idle/open limits are set to zero so pgxpool governs all connections. Prometheus pool metrics (`rosocp_db_pool_*`) are exported on scrape via a custom collector reading `pool.Stat()`.

**Residual risk**

Each process (API, processor, poller) still has its own pool instance — coordinate `ROS_DB_MAX_CONNS` × replica count against PostgreSQL `max_connections`.

---

### Finding #8 — Streaming ingest accumulates all groups in memory

| Field | Value |
|-------|-------|
| **Severity** | High (mitigated) |
| **Category** | Performance / availability |
| **Location** | `internal/ingestion/pipeline_stream.go` — incremental digest flush |
| **Status** | **Mitigated** (verified 2026-06-10) |
| **Effort** | M |

**Description**

Despite streaming CSV parsing, all container-day digest groups were held in a `groupedAll` map until EOF. Large clusters (10k containers × 14 days) could accumulate gigabytes in memory before flush, risking OOMKill mid-ingestion.

**Mitigation**

Streaming ingest now flushes digest groups incrementally when the in-memory group count reaches `ROS_INGEST_FLUSH_BATCH_SIZE` (default 1000). Each flush runs in its own transaction; maps are cleared after flush. Prometheus gauges/counters track in-memory group count and flush operations (`rosocp_ingest_groups_in_memory`, `rosocp_ingest_flush_total`, `rosocp_ingest_flush_duration_seconds`). Small payloads below the batch threshold retain the prior flush-at-EOF behavior.

---

### Finding #9 — Pipeline writes recommendations when history/quality fails

| Field | Value |
|-------|-------|
| **Severity** | High → **Mitigated** |
| **Category** | Data consistency |
| **Location** | `internal/services/report_processor.go`, `internal/engine/analytics_pipeline.go` |
| **Status** | **Mitigated** (verified 2026-06-10) |
| **Effort** | M |

**Description**

Container recommendations are persisted and Kafka offsets committed even when history or quality metric writes fail. The API serves fresh recommendations without corresponding analytics history.

**Mitigation**

- **`ROS_INGEST_STRICT_ANALYTICS`** (default `false`): when `true`, analytics writes run before recommendations; failures abort the batch and return a transient error (no offset commit, message retried).
- **Degraded mode (default):** recommendations persist; `rosocp_analytics_incomplete_total{error_type="history|quality"}` increments; `clusters.analytics_incomplete` flag set; container list/detail responses expose `analytics_incomplete` and `analytics_incomplete_at`.
- Runbook: [Analytics-Degraded Pipeline State](../operations/runbooks.md#runbook-analytics-degraded-pipeline-state).

---

### Finding #10 — No CHANGELOG.md despite API versioning policy

| Field | Value |
|-------|-------|
| **Severity** | High → **Resolved** |
| **Category** | Governance / API contract |
| **Location** | `CHANGELOG.md` (repo root), `docs/architecture/api-versioning.md` |
| **Status** | **Resolved** (verified 2026-06-10) |
| **Effort** | S |

**Description**

The API versioning policy documents breaking-change procedures referencing `CHANGELOG.md`. The v1.2 audit reported the file missing; **`CHANGELOG.md` now exists** at repo root (added in `ec4fdfe3`, updated in `d20e262f`) with Keep a Changelog format and an `[Unreleased]` section.

**Residual gap**

No CI enforcement of OpenAPI spec diff against changelog entries on breaking changes. Recommended as follow-up hardening.

---

## Findings — Medium

### Finding #4 — `/internal/tags/status` unauthenticated in on-prem (db mode)

| Field | Value |
|-------|-------|
| **Severity** | Medium (on-prem db-mode only) |
| **Category** | Authorization / multi-tenancy |
| **Location** | `internal/api/handlers_tags_status.go` (lines 17–44), `internal/api/server.go` |
| **Status** | **Open** (verified 2026-06-10) |
| **Effort** | S |

**Description**

When `ROS_TAGS_SOURCE=db` (on-prem default), bearer authentication is skipped for `/internal/tags/status` (`config.TagsUsePushSync()` is false). The endpoint accepts an arbitrary `org_id` query parameter, enabling cross-tenant tag enumeration.

**Exploit / trigger**

Any pod or user on the cluster network calls `GET /internal/tags/status?org_id=<victim>` without credentials and receives tag sync status for other tenants.

**Deployment context**

Only affects `ROS_TAGS_SOURCE=db` (on-prem). In SaaS (`api` mode), bearer auth is always required. On-prem chart NetworkPolicy restricts access to internal endpoints.

**Recommended fix**

Always require service-account bearer auth on `/internal/*` routes regardless of tag source mode. Bind `org_id` to the authenticated caller's namespace or explicit SA allowlist.

---

### Finding #11 — No rate limiting; recalc goroutines spawn without dedup

| Field | Value |
|-------|-------|
| **Severity** | Medium (partially addressed) |
| **Category** | Availability |
| **Location** | `internal/api/server.go`, `internal/engine/threshold_recalculate.go` |
| **Status** | **Partially addressed** (verified 2026-06-10) |
| **Effort** | M |

**Description**

Settings `PUT` handlers spawn async goroutines for threshold recalculation with no deduplication or rate limiting.

**Current state**

- `RecalculateThresholdsForOrg` uses a semaphore (`thresholdRecalcConcurrency()`, default 3) to cap concurrent cluster recalcs within a single job.
- Hash-based skip (`shouldSkipClusterThresholdRecalc`) avoids redundant work when settings unchanged.
- **`TriggerThresholdRecalculationAsync` still launches a new goroutine per PUT** with no per-`(org_id, recType)` single-flight guard — rapid settings changes spawn overlapping jobs.

**Recommended fix**

Per-org mutex or job queue with coalescing of duplicate in-flight jobs. Reject or queue when recalc already running for `(org_id, recType)`.

---

### Finding #12 — SSRF risk when CSV host allowlist unset

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Category** | Security / SSRF |
| **Location** | `internal/utils/csv_security.go` (`validateCSVDownloadURL`) |
| **Status** | **Mitigated** (2026-06-10) |
| **Effort** | S |

**Description**

When `ROS_CSV_ALLOWED_HOSTS` is empty, any URL in a Kafka message payload could be fetched by the processor (only scheme and host presence were validated).

**Mitigation (implemented)**

- Non-development mode: startup fails if `ROS_CSV_ALLOWED_HOSTS` is empty (`ValidateSecurityConfig`); runtime fetches blocked with clear error.
- Development mode (`DEVELOPMENT=true`): empty allowlist logs a one-time warning and allows fetches (backwards compatible for local httptest).
- `ROS_CSV_DENY_PRIVATE_NETWORKS` (default `true`): always blocks RFC1918, link-local, loopback, and `localhost`; resolves hostnames before fetch.
- Unit tests: `internal/utils/csv_security_test.go`.

---

### Finding #13 — ILIKE wildcard injection

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Category** | Authorization bypass |
| **Location** | `internal/api/common.go`, `internal/api/utils.go` |
| **Status** | **Mitigated** (2026-06-10) |
| **Effort** | S |

**Mitigation (implemented)**

- `escapeILIKE()` escapes `\`, `%`, `_` in filter operands; ILIKE clauses use `ESCAPE '\\'`.
- Applied in `buildModeClause`, `parseClusterParams`, and `gpu_model` filter in `handlers.go`.
- Unit tests: `internal/api/ilike_escape_test.go`.

---

### Finding #14 — Unbounded offset (deep-pagination DoS)

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Category** | Availability / DoS |
| **Location** | `internal/api/listoptions/list_options.go` |
| **Status** | **Mitigated** (2026-06-10) |
| **Effort** | S |

**Mitigation (implemented)**

- `ROS_API_MAX_OFFSET` (default `10000`): returns HTTP 400 when exceeded with message directing callers to keyset pagination.
- Unit tests: `internal/api/listoptions/list_options_test.go`.

---

### Finding #15 — ROS_TAGS_DEV_TOKEN static bypass

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Category** | Authentication |
| **Location** | `internal/tags/auth_config.go`, `internal/tags/startup.go` |
| **Status** | **Mitigated** (2026-06-10) |
| **Effort** | S |

**Mitigation (implemented)**

- Startup fails if `ROS_TAGS_DEV_TOKEN` is set and `DEVELOPMENT` is not `true`.
- Development mode logs prominent warning when dev token is active.
- Unit tests: `internal/tags/auth_config_test.go`.

---

### Finding #16 — Empty SA allowlist permits any K8s service account

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Category** | Authentication |
| **Location** | `internal/tags/auth_config.go`, `internal/tags/auth.go` |
| **Status** | **Mitigated** (2026-06-10) |
| **Effort** | S |

**Mitigation (implemented)**

- Startup fails when `ROS_TAGS_SOURCE=api`, allowlist empty, and not in development mode.
- Runtime default-deny for empty allowlist outside development.
- Development mode logs warning when allowlist is empty.
- Unit tests: `internal/tags/auth_config_test.go`.

---

### Finding #17 — Readiness probe only checks PostgreSQL

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Category** | Operational readiness |
| **Location** | `internal/db/db.go`, `internal/utils/utils.go` (`/readyz`) |
| **Status** | **Open** (verified 2026-06-10) |
| **Effort** | M |

**Description**

The `/readyz` endpoint verifies PostgreSQL connectivity only. Kafka, S3/MinIO, and Masu/Koku reachability are not checked.

**Recommended fix**

Add optional dependency health checks (configurable per deployment mode). Document accepted risk with external lag alerts if shallow probe is intentional.

---

### Finding #18 — Unclassified Kafka errors default to transient → consumer stall

| Field | Value |
|-------|-------|
| **Severity** | Medium (mitigated) |
| **Category** | Kafka consumer semantics |
| **Location** | `internal/services/kafka_processing_errors.go`, `internal/services/kafka_retry.go`, `internal/services/report_processor.go` |
| **Status** | **Mitigated** (2026-06-10) |
| **Effort** | M |

**Description**

Unknown error types are classified as transient (`return true` at line 67 of `kafka_processing_errors.go`), preventing offset commit. Without a retry budget, the consumer could redeliver the same message indefinitely with no progress.

**Mitigation (implemented)**

- **Retry-count tracking via Kafka headers:** Each requeue increments an `X-Retry-Count` header on the reproduced message. State is carried on the record itself (survives pod restarts and consumer rebalances).
- **Max 5 retries before DLQ escalation:** After `ROS_KAFKA_MAX_TRANSIENT_RETRIES` (default 5) attempts, the original message is routed to `hccm.ros.events.dlq` (`ROS_KAFKA_DLQ_TOPIC`) with forensic metadata headers (`X-Original-Topic`, `X-Original-Partition`, `X-Failure-Reason`, `X-Failed-At`). The source offset is then committed to unblock the partition.
- **Prometheus metrics:** `rosocp_kafka_dlq_messages_total` and `rosocp_kafka_retries_total` enable alerting on retry storms and DLQ volume.
- **Operational recovery:** After fixing the root cause, operators can replay messages from the DLQ topic. For production, declare `hccm.ros.events.dlq` as a Strimzi `KafkaTopic` CR (auto-create is enabled in dev but explicit CR is recommended).

**Residual risk**

Unclassified errors still retry up to the configured limit before DLQ. Operators should monitor DLQ growth and tune `isTransientKafkaProcessingError` for known permanent error patterns.

**Recommended fix (original)**

After N retries, invert default for unclassified errors to permanent/poison with DLQ. Add `rosocp_kafka_unclassified_error_total` metric for alerting.

---

### Finding #19 — Housekeeper lacks graceful shutdown

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Category** | Operational resilience |
| **Location** | `cmd/start.go`, `internal/services/housekeeper/` |
| **Status** | **Mitigated** (2026-06-10) |
| **Effort** | S |

**Mitigation (implemented)**

- Housekeeper subcommand wires `signal.NotifyContext` (SIGTERM/SIGINT) like processor and API.
- Context passed to sources listener and partition cleaner; in-flight cleanup respects cancellation with `ROS_HOUSEKEEPER_SHUTDOWN_GRACE_SECS` (default 30) when interrupted mid-work.
- Unit tests: `internal/services/housekeeper/shutdown_test.go`.

---

### Finding #20 — Poison message payload logged (PII risk)

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Category** | Privacy / logging |
| **Location** | `internal/services/poison_message_log.go`, `internal/services/report_processor.go` |
| **Status** | **Mitigated** (2026-06-10) |
| **Effort** | S |

**Mitigation (implemented)**

- `logPoisonMessage()` logs metadata only: `request_id`, `org_id`, `cluster_uuid`, `error_class`, `payload_size_bytes`; references DLQ topic for full payload recovery.
- `ROS_LOG_POISON_PAYLOAD` (default `false`): when `true`, logs first 256 bytes as `payload_preview` for debugging.
- Unit tests: `internal/services/poison_message_log_test.go`.

---

### Finding #21 — 25s statement_timeout kills large ingestion

| Field | Value |
|-------|-------|
| **Severity** | Medium (mitigated) |
| **Category** | Data integrity / performance |
| **Location** | `internal/db/db.go`, `internal/db/statement_timeout.go` |
| **Status** | **Mitigated** (verified 2026-06-10) |
| **Effort** | S |

**Description**

A global 25-second `statement_timeout` applied to both GORM and pgxpool connections via `AfterConnect`. Large ingestion batch inserts/upserts could exceed this limit, causing timeout errors classified as transient and retried indefinitely.

**Mitigation**

API connections keep `ROS_DB_STATEMENT_TIMEOUT` (default 25s). Ingestion batch transactions call `SET LOCAL statement_timeout` using `ROS_DB_INGEST_STATEMENT_TIMEOUT` (default 120s) at transaction start; the override resets automatically when the transaction ends.

---

### Finding #22 — Node GPU endpoint paginates in memory

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Category** | Performance / scalability |
| **Location** | `internal/api/handlers_node_recs.go`, `internal/api/handlers_node_utilization.go` |
| **Status** | **Open** (verified 2026-06-10) |
| **Effort** | M |

**Description**

Node and GPU recommendation endpoints load all matching results into memory, compute recommendations, then paginate in Go.

**Recommended fix**

Push pagination and filtering into SQL. Limit cluster fan-out per request. Add integration tests at 1k+ node scale.

---

### Finding #23 — panic() in boxplot/GPU YAML parse

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Category** | Reliability |
| **Location** | `internal/model/boxplot.go`, `internal/engine/vgpu_profiles.go`, `internal/engine/gpu_metadata.go` |
| **Status** | **Mitigated** (2026-06-10) |
| **Effort** | S |

**Mitigation (implemented)**

- `BucketGranularity.sql()` returns `(string, error)` instead of panicking on unknown values.
- Embedded GPU catalog YAML loaders return errors; `init()` uses `log.Fatal` for corrupt compile-time data only.
- Unit tests: `internal/model/boxplot_granularity_test.go`.

---

### Finding #24 — 140 migrations with no CONCURRENTLY automation

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Category** | Operations / deploy safety |
| **Location** | `migrations/` (140 `.up.sql` files), `migrations/README.md` |
| **Status** | **Open** (verified 2026-06-10; count updated from 134+) |
| **Effort** | L |

**Description**

golang-migrate wraps migrations in transactions, making `CREATE INDEX CONCURRENTLY` impossible within standard migration files. Latest migration: `000140_report_file_status`.

**Recommended fix**

CI check flagging new indexes on large tables. Automate CONCURRENTLY index creation via separate Kubernetes Job documented in upgrade runbook.

---

### Finding #31 — Native list pagination dropped workload_type filter (NEW)

| Field | Value |
|-------|-------|
| **Severity** | Medium (was open; **fixed** 2026-06-10) |
| **Category** | Correctness / filter bypass |
| **Location** | `internal/model/container_recommendation_pagination.go`, `internal/model/recommendation_set_native.go`, `internal/api/cursor.go` |
| **Status** | **Resolved** (commit `f66feaf7`) |
| **Effort** | S |

**Description**

Keyset pagination for native container lists used `DISTINCT ON` and page join keys on `(cluster_uuid, namespace, workload, container_name)` **without `workload_type`**. When multiple workload types shared the same container identity (e.g., a Deployment and StatefulSet named `api/main`), filtering by `workload_type=deployment` could return rows for other types after the page re-join.

**Exploit / trigger**

User filters `filter[workload_type]=deployment` on a paginated list; page 2+ returns StatefulSet rows sharing the same namespace/workload/container identity.

**Fix (verified)**

- `workload_type` added to `DISTINCT ON`, page join keys, cursor tie-breakers, and detail query re-filter.
- Regression test `TestGetNativeRecommendations_WorkloadTypeFilter` in `recommendation_set_keyset_test.go`.

**Regression check**

No new performance concerns observed — additional column in tie-breaker is indexed via existing list indexes.

---

## Findings — Low / Info

### Finding #3 — Identity header trusted without JWT verification

| Field | Value |
|-------|-------|
| **Severity** | Info (accepted architecture) |
| **Category** | Authentication |
| **Location** | `internal/api/middleware/identity.go` (lines 12–25) |
| **Status** | **Accepted** (unchanged) |
| **Effort** | M (optional defense-in-depth) |

**Description**

The middleware base64-decodes the `X-Rh-Identity` header and trusts its contents without verifying JWT signatures, expiry, issuer, or entitlements.

**Deployment context**

By design, upstream gateway validates JWT and injects trusted X-Rh-Identity. Not a required fix when gateway and NetworkPolicy are correctly deployed.

---

### Finding #5 — Settings mutation without RBAC (SNO/dev override)

| Field | Value |
|-------|-------|
| **Severity** | Low (deployment-specific) |
| **Category** | Authorization / integrity |
| **Location** | `internal/api/settings_rbac.go`, `internal/config/config.go` |
| **Status** | **Accepted** (unchanged) |
| **Effort** | S |

**Description**

When `RBAC_ENABLE=false`, any authenticated user can `PUT` optimization thresholds. Default cost-onprem chart sets `rbac.enabled: true`.

---

### Finding #6 — Internal service account can act on any org_id

| Field | Value |
|-------|-------|
| **Severity** | Info (accepted architecture) |
| **Category** | Authorization / multi-tenancy |
| **Location** | `internal/api/handlers_savings_recalculate.go`, `internal/api/handlers_tags_sync.go` |
| **Status** | **Accepted** (unchanged) |
| **Effort** | M (optional hardening) |

**Description**

Bearer token authentication validates the caller is a Kubernetes service account, but `org_id` in the request body is not bound to the caller's identity. By design for cross-tenant platform SAs.

---

### Finding #25 — History endpoints lack filter cardinality limits

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Category** | Availability |
| **Location** | `internal/api/handlers_history.go` |
| **Status** | **Mitigated** (2026-06-10) |
| **Effort** | S |

**Mitigation (implemented)**

- `checkHistoryFilterCardinality()` applies `MAXIMUM_COUNT_PER_QUERY_PARAM` to cluster, project, workload, container, term, and engine filters (same pattern as main list handlers).
- Unit tests: `internal/api/handlers_history_filter_test.go`.

---

### Finding #26 — Float64 in money formatting

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Category** | Correctness |
| **Location** | `internal/money/format.go` (lines 16–18) |
| **Status** | **Open** (verified 2026-06-10) |
| **Effort** | S |

**Description**

Integer cents are divided by `100.0` into `float64` for display formatting.

**Recommended fix**

Use integer-only formatting (cents → dollars via div/mod) or a decimal library.

---

### Finding #27 — Deterministic recommendation IDs

| Field | Value |
|-------|-------|
| **Severity** | Info |
| **Category** | Security awareness |
| **Location** | `internal/model/recommendation_set_native.go` |
| **Status** | **Open** (info only; unchanged) |
| **Effort** | Info only |

**Description**

Recommendation IDs are UUID v5 derived from cluster, namespace, workload, and container identifiers. Acceptable if org boundary is enforced on all detail endpoints.

---

### Finding #28 — Overlapping threshold recalc jobs

| Field | Value |
|-------|-------|
| **Severity** | Low (partially addressed) |
| **Category** | Correctness / availability |
| **Location** | `internal/engine/threshold_recalculate.go` |
| **Status** | **Partially addressed** (verified 2026-06-10) |
| **Effort** | S |

**Description**

Rapid successive settings `PUT` requests launch concurrent recalc goroutines.

**Current state**

Hash-based cluster skip reduces redundant work within overlapping jobs, but no single-flight guard prevents multiple concurrent `RecalculateThresholdsForOrg` runs for the same `(org_id, recType)`.

**Recommended fix**

Per-`(org_id, recType)` single-flight guard. Coalesce or cancel superseded jobs.

---

### Finding #29 — Effective-rates cache unbounded

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Category** | Performance / memory |
| **Location** | `internal/costdata/provider.go` (`sync.Map`, 5 min TTL) |
| **Status** | **Open** (verified 2026-06-10) |
| **Effort** | S |

**Description**

The effective-rates cache grows with distinct org×cluster pairs. Entries expire by TTL but there is no max size.

**Recommended fix**

Replace with LRU cache capped at configurable max entries. Export cache size metric.

---

### Finding #30 — No formal ADR index

| Field | Value |
|-------|-------|
| **Severity** | Info |
| **Category** | Governance |
| **Location** | `docs/` (no `docs/adr/` directory) |
| **Status** | **Open** (verified 2026-06-10) |
| **Effort** | S |

**Description**

Architectural decisions are discoverable only by archaeology across scattered docs and commit history.

**Recommended fix**

Add `docs/adr/` with numbered Architecture Decision Records.

---

## What Held Up Well

Positive findings from the review — areas that demonstrate mature engineering and reduce compound risk.

| Area | Observation |
|------|-------------|
| **Plugin architecture** | Recommendation types are modular with explicit enable/disable; disabled routes return 404. Clean separation supports incremental hardening per plugin. |
| **Parameterized SQL** | List and detail queries use bound parameters via pgxpool/GORM; no string-concatenated user input in SQL text observed in reviewed paths. |
| **Kafka error classification framework** | `kafka_processing_errors.go` provides a structured transient vs. permanent taxonomy; retry/DLQ escalation in `kafka_retry.go` prevents infinite partition stall (#18 mitigated). |
| **Per-file ingestion tracking** | `report_file_status` + `ReportFileTracker` with unit tests (`report_file_status_test.go`, `TestConsumer_PresignedDownload403`) — operational visibility for partial manifest failures. |
| **Manifest-gated recommendations** | `runManifestRecommendations` defers engine execution until all expected files reach `done` — prevents stale/partial recs from incomplete ingestion. |
| **Service-account auth pattern** | Kubernetes TokenReview integration for internal endpoints is correctly implemented when allowlists are configured (Findings #15–#16 are config gaps, not design gaps). |
| **Structured metrics** | Ingestion and processing paths increment Prometheus counters/histograms; `ros_ingestion_file_failures_total` enables alerting on per-file failures. |
| **Native engine test coverage** | Unit and integration tests exist for core recommendation math, term windows, idle detection, and keyset pagination — reduces regression risk during remediation. |
| **Keyset pagination correctness** | Container list pagination includes `workload_type` in DISTINCT ON and cursor tie-breakers after `f66feaf7`; regression test added. |
| **API versioning documentation** | `docs/architecture/api-versioning.md` defines a coherent deprecation policy; `CHANGELOG.md` now tracks releases. |
| **Multi-tenancy schema isolation** | Tenant data scoped by `org_id` in queries when handlers apply filters correctly; RBAC integration path exists for SaaS mode. |
| **Streaming CSV parser** | Ingestion pipeline streams file content rather than loading entire CSVs — memory issue is in grouping layer (Finding #8), not parse layer. |
| **Upgrade runbook** | `docs/upgrade-runbook.md` documents manual migration steps for large-table operations. |
| **Koku integration contract** | `docs/architecture/cost-integration.md` documents `effective_rates`, `reship_ros`, tags, and ingestion contracts (added `98d403dd`). |

---

## Cross-Cutting Failure Scenario Matrix

What happens when a dependency fails or is misconfigured. Rows are independent scenarios; cells describe user-visible and operational impact.

| Scenario | Ingestion | API reads | API writes | Analytics | Consumer offset | Operator signal |
|----------|-----------|-----------|------------|-----------|-----------------|-----------------|
| **PostgreSQL down** | Fails (transient) | 503 /readyz fails | 503 | Blocked | Not committed (retry) | `/readyz` fails; pod not ready |
| **Kafka down** | Stalled | Unaffected | Unaffected | Stale | N/A | Consumer lag alert (external) |
| **S3/MinIO down (one file in payload)** | **Tracked failure (#1+#2 mitigated)** | Stale recs (gated) | N/A | Stale | **Committed** | `ros_ingestion_file_failures_total` + `report_file_status.failed` |
| **S3/MinIO down (all files)** | Fails | Stale | N/A | Stale | Depends on error class | Log + metric |
| **Empty manifest_id in Kafka msg** | **No per-file tracking** | May run recs early | N/A | Partial | Committed | No `report_file_status` rows |
| **Masu/Koku cost API down** | Recs without cost (degraded) | Savings $0 or cached | N/A | Partial | May commit (#9) | Cost provider errors in logs |
| **History DB write fails** | Recs written (degraded) or retried (strict) | Fresh recs; flag if degraded | N/A | **Gap if degraded** | Committed if degraded; retry if strict | `rosocp_analytics_incomplete_total`, API `analytics_incomplete` |
| **Identity gateway bypassed (#3)** | N/A | Cross-tenant access *(SNO/dev only)* | Unauthorized mutation *(SNO/dev only)* | N/A | N/A | No in-app signal |
| **`RBAC_ENABLE=false` (#5)** | N/A | All data (if identity trusted) | Any user changes thresholds *(SNO/dev only)* | Recalc storm | N/A | None |
| **Unclassified Kafka error (#18)** | **Retry then DLQ** | Stale during retries | N/A | Stale | **Committed after DLQ** | `rosocp_kafka_retries_total`, `rosocp_kafka_dlq_messages_total` |
| **Large cluster + 25s timeout (#21)** | Timeout → transient loop | OK | OK | Stalled | Not committed | Repeated timeout logs |
| **OOM during ingest (#8)** | Pod killed mid-batch | 503 if same pod | 503 | Partial | **May commit partial (#1)** | OOMKilled event |
| **NetworkPolicy missing (#3, #4)** | N/A | Internal routes exposed *(SNO/dev)* | Tag enum cross-tenant *(db mode)* | N/A | N/A | None without network audit |
| **workload_type filter + pagination (#31)** | N/A | **Fixed** — filter honored on all pages | N/A | N/A | N/A | N/A |

**Key takeaway:** The worst production outcomes cluster around **Kafka commit semantics** (silent loss vs. infinite stall). Per-file tracking (#1+#2) materially improves the partial-failure case for native ingestion when `manifest_id` is present. Auth findings (#3–#6) are largely mitigated in SaaS and default on-prem chart deployments. Analytics degradation (#9) is now observable via metrics and API flags; strict mode available for environments requiring history/quality parity.

---

## Tracking

| Finding # | Title | Jira | Status | Target / Notes |
|-----------|-------|------|--------|----------------|
| 1 | Kafka offset committed after partial file failure | TBD | Mitigated | `90e5ed52`; gaps: empty manifest_id, Kruize path |
| 2 | Native ingestion errors swallowed (return nil) | TBD | Mitigated | Native path only; `TestConsumer_PresignedDownload403` |
| 3 | Identity header trusted without JWT verification | TBD | Accepted (architecture) | — |
| 4 | `/internal/tags/status` unauthenticated in on-prem | TBD | Open | db-mode hardening |
| 5 | Settings mutation without RBAC (SNO/dev override) | TBD | Accepted (deployment-specific) | — |
| 6 | Internal SA can act on any org_id | TBD | Accepted (architecture) | — |
| 7 | Dual DB connection pools (GORM + pgxpool) | TBD | **Mitigated** | GORM shares pgxpool via `OpenDBFromPool`; `rosocp_db_pool_*` metrics |
| 8 | Streaming ingest accumulates all groups in memory | TBD | **Mitigated** | Incremental flush via `ROS_INGEST_FLUSH_BATCH_SIZE`; ingest memory metrics |
| 9 | Pipeline writes recs when history/quality fails | TBD | **Mitigated** | Strict mode + `rosocp_analytics_incomplete_total` + API flag |
| 10 | No CHANGELOG.md despite API versioning policy | TBD | **Resolved** | `CHANGELOG.md` exists |
| 11 | No rate limiting; recalc goroutines without dedup | TBD | Partially addressed | Semaphore cap=3; no single-flight |
| 12 | SSRF risk when CSV host allowlist unset | TBD | **Mitigated** | `csv_security.go`; startup + private-network deny |
| 13 | ILIKE wildcard injection | TBD | **Mitigated** | `escapeILIKE` + `ESCAPE '\\'` |
| 14 | Unbounded offset (deep-pagination DoS) | TBD | **Mitigated** | `ROS_API_MAX_OFFSET` default 10000 |
| 15 | ROS_TAGS_DEV_TOKEN static bypass | TBD | **Mitigated** | Blocked outside `DEVELOPMENT=true` |
| 16 | Empty SA allowlist permits any K8s SA | TBD | **Mitigated** | Required in api mode (non-dev) |
| 17 | Readiness probe only checks PostgreSQL | TBD | Open | — |
| 18 | Unclassified Kafka errors default to transient | TBD | **Mitigated** | `kafka_retry.go`; DLQ `hccm.ros.events.dlq`; max 5 retries |
| 19 | Housekeeper lacks graceful shutdown | TBD | **Mitigated** | `signal.NotifyContext`; `ROS_HOUSEKEEPER_SHUTDOWN_GRACE_SECS` |
| 20 | Poison message payload logged (PII risk) | TBD | **Mitigated** | Metadata-only logs; `ROS_LOG_POISON_PAYLOAD` opt-in |
| 21 | 25s statement_timeout kills large ingestion | TBD | **Mitigated** | `SET LOCAL` ingest timeout via `ROS_DB_INGEST_STATEMENT_TIMEOUT` |
| 22 | Node GPU endpoint paginates in memory | TBD | Open | — |
| 23 | panic() in boxplot/GPU YAML parse | TBD | **Mitigated** | Error returns; `log.Fatal` for embedded catalog only |
| 24 | 140 migrations with no CONCURRENTLY automation | TBD | Open | Was 134+; now 140 |
| 25 | History endpoints lack filter cardinality limits | TBD | **Mitigated** | `checkHistoryFilterCardinality` |
| 26 | Float64 in money formatting | TBD | Open | — |
| 27 | Deterministic recommendation IDs (info) | TBD | Open (info) | — |
| 28 | Overlapping threshold recalc jobs | TBD | Partially addressed | Hash skip; no single-flight |
| 29 | Effective-rates cache unbounded | TBD | Open | — |
| 30 | No formal ADR index | TBD | Open | — |
| 31 | Native list pagination dropped workload_type filter | TBD | **Resolved** | `f66feaf7` |

---

## Commits Reviewed (2026-06-08 → 2026-06-10)

| Commit | Summary | Audit impact |
|--------|---------|--------------|
| `b9d2ec35` | Initial adversarial review document | Baseline v1.0 |
| `2df9ee38` | Reclassify findings by deployment posture | v1.2 scorecard/remediation |
| `98d403dd` | Koku integration contract docs | Positive: cost-integration.md |
| `90e5ed52` | Kafka commit resilience (#1, #2) | Mitigated; migration 000140 |
| `f66feaf7` | Fix workload_type pagination filter | New #31, resolved |

---

*Document version: 1.6 — 2026-06-10. Mitigated Finding #9 (analytics strict mode + staleness signaling).*
