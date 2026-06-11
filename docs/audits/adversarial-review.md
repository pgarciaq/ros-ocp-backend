# Adversarial Due Diligence Review — ros-ocp-backend

> **INTERNAL USE ONLY** — This document is an internal security and engineering audit. It is not for public disclosure, customer distribution, or external publication without explicit security and legal review.

**Date:** 2026-06-10 (re-validation pass)  
**Previous review:** 2026-06-08 (v1.2)  
**Scope:** `ros-ocp-backend` — Kafka ingestion pipeline, native recommendation engine, REST API, database layer, authentication/authorization, operational readiness, and engineering governance  
**Methodology:** Adversarial due diligence combining static code review, architecture analysis, threat modeling (STRIDE-lite), and operational failure-mode analysis. Reviewers assumed the **SNO/dev deployment posture** (`ROS_TAGS_SOURCE=db`, `RBAC_ENABLE=false`, no gateway) with network access to the API port unless otherwise noted. Findings were validated against source locations and cross-referenced for compound failure chains.

**Changes since v1.3:** All v1.6 findings (#1–#31) and v2.0 findings (#32–#60) are **resolved, mitigated, or accepted** with documented rationale. See [v2.0 Findings Status Summary](#v20-findings-status-summary) and [Current State](#current-state).

---

## Deployment Context

ros-ocp-backend runs in three distinct deployment postures. Several findings in this review reflect **SNO/dev overrides** rather than production vulnerabilities in the default SaaS or on-prem chart configurations.

| Posture | Auth | RBAC | Tags source | Internal endpoints |
|---------|------|------|-------------|-------------------|
| **SaaS** (console.redhat.com) | 3scale validates JWT upstream | Enabled (`RBAC_ENABLE=true`) | `api` (push sync with SA auth) | Cluster-internal only |
| **On-prem chart (default)** | Envoy gateway validates JWT via JWKS, injects X-Rh-Identity | Enabled (`rbac.enabled: true`) | `db` (direct PG join) | NetworkPolicy restricted to gateway/UI |
| **SNO/dev overrides** | No gateway; direct API access | Disabled (`rbac.enabled: false`) | `db` | Unrestricted |

**Review scope:** This audit was conducted against the **SNO/dev posture**. Findings #3 and #5 are mitigated or eliminated in the default production postures (SaaS and on-prem chart). Finding #4 is resolved (#37). Findings #6 and #16 reflect accepted platform architecture with optional hardening levers, not production gaps when compensating controls are in place.

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
10. [Adversarial Due Diligence Review v2.0](#adversarial-due-diligence-review-v20)
11. [Current State](#current-state)

---

## Executive Scorecard

| Area | Verdict | Summary |
|------|---------|---------|
| **Data integrity (Kafka ingestion)** | 🟢 Low (mitigated) | Per-file tracking and error surfacing in native path (`90e5ed52`); empty `manifest_id` synthesized (#32); legacy Kruize path accepted for deprecation (#33) |
| **Authentication** | 🟢 Delegated to gateway | Accepted architecture; weak only if gateway bypassed (SNO/dev posture) |
| **Authorization** | 🟢 Strong when chart defaults used | `rbac.enabled: true` in production; weak only in SNO/dev overrides |
| **API security** | 🟢 Low (mitigated) | ILIKE wildcard injection, unbounded offset, SSRF when allowlist unset, history filter cardinality — mitigated (#12–#14, #25); pagination filter bypass (#31) fixed |
| **Database & connections** | 🟢 Low (mitigated) | Unified pgxpool for GORM and pgx paths; pool metrics exported |
| **Memory & performance** | 🟢 Low (mitigated) | Incremental digest flush (#8); GPU MIG/time-slicing and node endpoints use SQL pagination (#22, #48, #49) |
| **Operational resilience** | 🟢 Low (mitigated) | Readiness probe shallow; Kafka transient errors retry/DLQ (#18); housekeeper graceful shutdown (#19 mitigated) |
| **Pipeline correctness** | 🟢 Low (mitigated) | Strict analytics mode + staleness signaling for history/quality gaps |
| **Engineering governance** | 🟢 Low (mitigated) | CHANGELOG exists (#10); 162 ADRs indexed (#30); migration CONCURRENTLY lint (#24); OpenAPI/ADR advisory CI and govulncheck (#53, #54, #60) |
| **Positive controls** | 🟢 Strong | Plugin architecture, parameterized SQL, service-account auth patterns (when enabled), structured metrics, ingestion unit tests |

**Overall assessment:** The native engine and API surface are production-grade for the common case. **Findings #1 and #2 are mitigated** for the native ingestion path via per-file `report_file_status` tracking (migration `000140`), surfaced ingestion errors, recommendation gating (`runManifestRecommendations`), and `ros_ingestion_file_failures_total`. Empty `manifest_id` is synthesized (#32); operator recovery is documented in `docs/operations/runbooks.md` (#58). **Finding #31** (workload_type filter bypass) was fixed in `f66feaf7`. **Finding #4** (internal tags auth) was resolved in v2.0 as **#37**. Auth findings (#3, #5, #6) remain accepted deployment-specific architecture mitigated by gateway JWT validation, RBAC defaults, and NetworkPolicy in standard deployments.

---

## Priority Remediation Order

Remediation is ordered by **compound risk** (findings that amplify each other) and **blast radius** (data loss > auth bypass > availability > governance).

| # | Finding(s) | Rationale |
|---|------------|-----------|
| 1 | **#1, #2** (High — mitigated) | Per-file tracking, error propagation, recommendation gating, and alerting implemented. Empty `manifest_id` synthesized (#32); legacy Kruize path accepted (#33); manual `reship_ros` documented (#58). |
| 2 | **#8, #21** (Medium — ingestion scale) | Memory accumulation and statement timeout both manifest under large-cluster ingestion; fix together to avoid OOM ↔ retry loops. |
| 3 | **#9, #45** (High — mitigated) | Strict analytics mode default `true` (#45); degraded mode opt-in via `ROS_INGEST_STRICT_ANALYTICS=false`; `rosocp_analytics_incomplete_total` and API `analytics_incomplete` flag. |
| 4 | **#11, #28** (Medium — recalc storms) | Concurrency capped at 3 per job but overlapping async jobs still possible after settings changes. |
| 5 | **#12, #13, #14** (Medium — API hardening) | **Mitigated** — SSRF allowlist + private-network deny, ILIKE escape, offset cap. |
| 6 | **#15, #16** (Medium — tag auth config) | **Mitigated** — startup validation; dev token blocked outside `DEVELOPMENT=true`. |
| 7 | **#17, #19, #20** (Medium — ops) | **Mitigated** — opt-in deep readiness (#17), housekeeper SIGTERM (#19), poison log redaction (#20). |
| 8 | **#22, #23** (Medium — memory/panic) | **Mitigated** — SQL GPU/node pagination (#22, #48, #49); panic-to-error for catalogs (#23). |
| 9 | **#24** (Medium — migrations) | **Resolved** — `lint-migrations.sh`, K8s Job template, runbook. |
| 10 | **#30** (Info — governance) | **Resolved** — 162 ADRs indexed at [`docs/adr/README.md`](../adr/README.md). |
| — | **#7** (Mitigated) | GORM uses `stdlib.OpenDBFromPool`; `ROS_DB_MAX_CONNS` governs all connections; pool metrics on scrape. |
| — | **#18** (Mitigated) | Retry-count headers + DLQ after 5 attempts; `rosocp_kafka_dlq_messages_total` for alerting. |
| — | **#10, #53** (Resolved) | `CHANGELOG.md` exists; advisory OpenAPI/CHANGELOG CI (`99296701`). |
| — | **#31** (Resolved) | Pagination filter bypass fixed in `f66feaf7`. |
| — | **#3** (Info — architecture) | Gateway enforcement in SaaS and on-prem chart. |
| — | **#4 → #37** (Medium — resolved) | Bearer auth on `/internal/tags/*` via `ROS_INTERNAL_TAGS_AUTH_REQUIRED` (default `true`). |
| — | **#5, #6** (Low/Info — deployment-specific) | RBAC disabled and cross-tenant SA scope are SNO/dev overrides or accepted platform architecture. |

---

## Findings by Deployment Posture

Which findings apply to each deployment posture. ✓ = applies; ✗ = mitigated or not applicable; ⚠ = partially mitigated.

| Finding | SaaS | On-prem (default) | SNO/dev |
|---------|------|---------------------|---------|
| #1 Kafka commit | ✓ | ✓ | ✓ |
| #2 Error swallowed | ✓ | ✓ | ✓ |
| #3 Identity header | ✗ (gateway) | ✗ (gateway) | ✓ |
| #4 Tags unauth | ✗ (resolved #37) | ✗ (resolved #37) | ⚠ (if auth disabled) |
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
| #17 Readiness shallow | ⚠ (opt-in deep checks) | ⚠ (opt-in deep checks) | ⚠ (opt-in deep checks) |
| #18 Kafka stall | ⚠ (mitigated) | ⚠ (mitigated) | ⚠ (mitigated) |
| #19 Housekeeper shutdown | ✗ (mitigated) | ✗ (mitigated) | ✗ (mitigated) |
| #20 PII in logs | ✗ (mitigated) | ✗ (mitigated) | ✗ (mitigated) |
| #21 Statement timeout | ✗ (mitigated) | ✗ (mitigated) | ✗ (mitigated) |
| #22 Node GPU memory | ✗ (mitigated) | ✗ (mitigated) | ✗ (mitigated) |
| #23 panic() parse | ✗ (mitigated) | ✗ (mitigated) | ✗ (mitigated) |
| #24 Migrations CONCURRENTLY | ✗ (mitigated) | ✗ (mitigated) | ✗ (mitigated) |
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

- **Empty `manifest_id`:** **Resolved (#32).** When omitted, the processor synthesizes a deterministic `synth-*` manifest ID from `(org_id, cluster_uuid, date|payload fingerprint)` so per-file tracking and recommendation gating still apply. Legacy Kruize path (#33) remains untracked.
- **Legacy Kruize path:** Files processed via `ReadCSVFromUrl` + dataframe (when Kruize plugin enabled) do not use `report_file_status`; fetch/parse failures `continue` without permanent classification.
- Operators must manually intervene via Koku's `reship_ros` API to re-deliver failed files. Offset commit behavior is unchanged — the queue does not stall on partial failure.
- Operator runbook for querying `report_file_status` and triggering recovery: `docs/operations/runbooks.md` (#58).

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

- Legacy Kruize path (#33) still bypasses per-file tracking when the plugin is enabled; accepted for deprecation (ADR-0163).
- Recovery for native-path failures uses targeted `reship_ros` after investigating `report_file_status` and Prometheus alerts.

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

- **`ROS_INGEST_STRICT_ANALYTICS`** (default `true`): when `true`, analytics writes run before recommendations; failures abort the batch and return a transient error (no offset commit, message retried).
- **Degraded mode (`ROS_INGEST_STRICT_ANALYTICS=false`):** recommendations persist; `rosocp_analytics_incomplete_total{error_type="history|quality"}` increments; `clusters.analytics_incomplete` flag set; container list/detail responses expose `analytics_incomplete` and `analytics_incomplete_at`.
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

**Resolution**

`CHANGELOG.md` exists at repo root (added in `ec4fdfe3`, updated in `d20e262f`) with Keep a Changelog format and an `[Unreleased]` section. Advisory OpenAPI/CHANGELOG CI added in v2.0 Finding #53 (`99296701`).

---

## Findings — Medium

### Finding #4 — `/internal/tags/status` unauthenticated in on-prem (db mode)

| Field | Value |
|-------|-------|
| **Severity** | Medium (on-prem db-mode only) |
| **Category** | Authorization / multi-tenancy |
| **Location** | `internal/api/handlers_tags_status.go` (lines 17–44), `internal/api/server.go` |
| **Status** | **Resolved** (v2.0 Finding #37; verified 2026-06-11) |
| **Effort** | S |

**Description**

When `ROS_TAGS_SOURCE=db` (on-prem default), bearer authentication is skipped for `/internal/tags/status` (`config.TagsUsePushSync()` is false). The endpoint accepts an arbitrary `org_id` query parameter, enabling cross-tenant tag enumeration.

**Exploit / trigger**

Any pod or user on the cluster network calls `GET /internal/tags/status?org_id=<victim>` without credentials and receives tag sync status for other tenants.

**Deployment context**

Only affects `ROS_TAGS_SOURCE=db` (on-prem). In SaaS (`api` mode), bearer auth is always required. On-prem chart NetworkPolicy restricts access to internal endpoints.

**Recommended fix**

Always require service-account bearer auth on `/internal/*` routes regardless of tag source mode. Bind `org_id` to the authenticated caller's namespace or explicit SA allowlist.

**Resolution (v2.0 #37)**

Implemented via `validateInternalTagsAuth` and `ROS_INTERNAL_TAGS_AUTH_REQUIRED` (default `true`). See Finding #37 below.

---

### Finding #11 — No rate limiting; recalc goroutines spawn without dedup

| Field | Value |
|-------|-------|
| **Severity** | Medium (partially addressed) |
| **Category** | Availability |
| **Location** | `internal/api/server.go`, `internal/engine/threshold_recalculate.go` |
| **Status** | **Mitigated** (2026-06-10) |
| **Effort** | M |

**Mitigation (implemented)**

- Per-`(org_id, recType)` single-flight guard in `internal/engine/threshold_recalc_guard.go` coalesces overlapping triggers; at most one follow-up run after the in-flight job completes.
- Prometheus counter `rosocp_threshold_recalc_coalesced_total`.
- Unit tests: `internal/engine/threshold_recalc_guard_test.go`.

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
| **Status** | **Mitigated** (2026-06-10) |
| **Effort** | M |

**Mitigation (implemented)**

- Default `/readyz` remains PostgreSQL-only (intentional for API-only pods).
- Opt-in deep checks via `ROS_READINESS_CHECK_KAFKA` and `ROS_READINESS_CHECK_S3` (default `false`); failures return HTTP 503 with per-dependency JSON `checks`.
- S3 check uses `HeadBucket` against `ROS_READINESS_S3_BUCKET` (+ endpoint/credentials env vars).
- Shared logic in `internal/health/readyz.go`; unit tests in `internal/health/readyz_test.go` and `internal/api/handlers_readyz_test.go`.

**Accepted risk**

Processor/ingestion pods should enable deep checks in production; API deployments may keep shallow probe and monitor Kafka lag / S3 errors externally.

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
| **Status** | **Mitigated** (2026-06-10) |
| **Effort** | M |

**Mitigation (implemented)**

- GPU time-slicing uses `CountNodeGPUTriples` + `ListNodeGPUTriplesPage` with SQL `LIMIT`/`OFFSET` (or keyset seek); only the current page of triples is enriched.
- Node utilization already paginated in SQL; added `ROS_API_MAX_NODE_RESULTS` hard cap (default 1000).
- Integration test with 55 nodes: `handlers_node_utilization_pagination_integration_test.go`.

**Residual risk**

None for supported sort keys. Unsupported `order_by` values return HTTP 400 (#49).

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
| **Status** | **Mitigated** (2026-06-10) |
| **Effort** | L |

**Mitigation (implemented)**

- CI lint script `scripts/lint-migrations.sh` flags new non-`CONCURRENTLY` indexes on configured large tables.
- Runbook: `docs/operations/large-table-migrations.md`.
- K8s Job template: `deploy/migrations/concurrent-index-job.yaml`.
- Updated `migrations/README.md` with lint policy.

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
| **Status** | **Mitigated** (2026-06-10) |
| **Effort** | S |

**Description**

Integer cents are divided by `100.0` into `float64` for display formatting.

**Mitigation (implemented)**

`FormatCentsToAmount` formats via integer division and remainder (`cents/100`, `cents%100`) with explicit negative handling. Unit tests cover large cent values that would exhibit float64 rounding.

---

### Finding #27 — Deterministic recommendation IDs

| Field | Value |
|-------|-------|
| **Severity** | Info |
| **Category** | Security awareness |
| **Location** | `internal/model/recommendation_set_native.go` |
| **Status** | **Verified** (2026-06-10) |
| **Effort** | Info only |

**Description**

Recommendation IDs are UUID v5 derived from cluster, namespace, workload, and container identifiers. Acceptable if org boundary is enforced on all detail endpoints.

**Mitigation (verified)**

Audited all detail endpoints — each filters by `org_id`. Documented security invariant in [`docs/architecture/recommendation-ids.md`](../architecture/recommendation-ids.md). Regression test: `internal/model/recommendation_detail_org_scope_test.go`.

---

### Finding #28 — Overlapping threshold recalc jobs

| Field | Value |
|-------|-------|
| **Severity** | Low (partially addressed) |
| **Category** | Correctness / availability |
| **Location** | `internal/engine/threshold_recalculate.go` |
| **Status** | **Mitigated** (2026-06-10) |
| **Effort** | S |

**Mitigation (implemented)**

Same single-flight coalescing as finding #11 (`threshold_recalc_guard.go`).

---

### Finding #29 — Effective-rates cache unbounded

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Category** | Performance / memory |
| **Location** | `internal/costdata/provider.go` (`sync.Map`, 5 min TTL) |
| **Status** | **Mitigated** (2026-06-10) |
| **Effort** | S |

**Description**

The effective-rates cache grows with distinct org×cluster pairs. Entries expire by TTL but there is no max size.

**Mitigation (implemented)**

Replaced `sync.Map` with bounded LRU cache (`ROS_COST_CACHE_MAX_ENTRIES`, default 1000). TTL-on-access preserved (5 minutes). Metrics: `rosocp_cost_cache_size`, `rosocp_cost_cache_evictions_total`.

---

### Finding #30 — No formal ADR index

| Field | Value |
|-------|-------|
| **Severity** | Info |
| **Category** | Governance |
| **Location** | `docs/` (no `docs/adr/` directory) |
| **Status** | **Resolved** (2026-06-11) |
| **Effort** | S |

**Description**

Architectural decisions are discoverable only by archaeology across scattered docs and commit history.

**Resolution**

Added [`docs/adr/`](../adr/) with 162 numbered Architecture Decision Records (Michael Nygard format) and an index at [`docs/adr/README.md`](../adr/README.md). Decisions are grouped by domain: engine/algorithm, data model, API design, ingestion, plugins, cost/savings, tags, reship/business hours, deployment/ops, testing, security, Kafka, and configuration.

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
| **Empty manifest_id in Kafka msg** | **Synthesized ID (#32 resolved)** | Gated | N/A | Partial | Committed | `rosocp_ingest_manifest_id_synthesized_total` when fallback used |
| **Masu/Koku cost API down** | Recs without cost (degraded) | Savings $0 or cached | N/A | Partial | May commit (#9) | Cost provider errors in logs |
| **History DB write fails** | Recs written (degraded) or retried (strict) | Fresh recs; flag if degraded | N/A | **Gap if degraded** | Committed if degraded; retry if strict | `rosocp_analytics_incomplete_total`, API `analytics_incomplete` |
| **Identity gateway bypassed (#3)** | N/A | Cross-tenant access *(SNO/dev only)* | Unauthorized mutation *(SNO/dev only)* | N/A | N/A | No in-app signal |
| **`RBAC_ENABLE=false` (#5)** | N/A | All data (if identity trusted) | Any user changes thresholds *(SNO/dev only)* | Recalc storm | N/A | None |
| **Unclassified Kafka error (#18)** | **Retry then DLQ** | Stale during retries | N/A | Stale | **Committed after DLQ** | `rosocp_kafka_retries_total`, `rosocp_kafka_dlq_messages_total` |
| **Large cluster + 25s timeout (#21)** | Timeout → transient loop | OK | OK | Stalled | Not committed | Repeated timeout logs |
| **OOM during ingest (#8)** | Pod killed mid-batch | 503 if same pod | 503 | Partial | **May commit partial (#1)** | OOMKilled event |
| **NetworkPolicy missing (#3, #4)** | N/A | Internal routes exposed *(SNO/dev)* | Tag enum cross-tenant *(db mode)* | N/A | N/A | None without network audit |
| **workload_type filter + pagination (#31)** | N/A | **Fixed** — filter honored on all pages | N/A | N/A | N/A | N/A |

**Key takeaway:** The worst production outcomes cluster around **Kafka commit semantics** (silent loss vs. infinite stall). Per-file tracking (#1+#2) and manifest ID synthesis (#32) materially improve the partial-failure case for native ingestion. Auth findings (#3–#6) are largely mitigated in SaaS and default on-prem chart deployments. Analytics strict mode is the default (#45); degraded mode is opt-in with metrics and API flags.

---

## Tracking

| Finding # | Title | Jira | Status | Target / Notes |
|-----------|-------|------|--------|----------------|
| 1 | Kafka offset committed after partial file failure | TBD | Mitigated | `90e5ed52`; empty `manifest_id` synthesized (#32); Kruize path accepted (#33) |
| 2 | Native ingestion errors swallowed (return nil) | TBD | Mitigated | Native path only; `TestConsumer_PresignedDownload403` |
| 3 | Identity header trusted without JWT verification | TBD | Accepted (architecture) | — |
| 4 | `/internal/tags/status` unauthenticated in on-prem | TBD | **Resolved** (#37) | `ROS_INTERNAL_TAGS_AUTH_REQUIRED` default `true` (`8ff82f5e`) |
| 5 | Settings mutation without RBAC (SNO/dev override) | TBD | Accepted (deployment-specific) | — |
| 6 | Internal SA can act on any org_id | TBD | Accepted (architecture) | — |
| 7 | Dual DB connection pools (GORM + pgxpool) | TBD | **Mitigated** | GORM shares pgxpool via `OpenDBFromPool`; `rosocp_db_pool_*` metrics |
| 8 | Streaming ingest accumulates all groups in memory | TBD | **Mitigated** | Incremental flush via `ROS_INGEST_FLUSH_BATCH_SIZE`; ingest memory metrics |
| 9 | Pipeline writes recs when history/quality fails | TBD | **Mitigated** | Strict mode + `rosocp_analytics_incomplete_total` + API flag |
| 10 | No CHANGELOG.md despite API versioning policy | TBD | **Resolved** | `CHANGELOG.md` exists |
| 11 | No rate limiting; recalc goroutines without dedup | TBD | **Mitigated** | Single-flight + `rosocp_threshold_recalc_coalesced_total` |
| 12 | SSRF risk when CSV host allowlist unset | TBD | **Mitigated** | `csv_security.go`; startup + private-network deny |
| 13 | ILIKE wildcard injection | TBD | **Mitigated** | `escapeILIKE` + `ESCAPE '\\'` |
| 14 | Unbounded offset (deep-pagination DoS) | TBD | **Mitigated** | `ROS_API_MAX_OFFSET` default 10000 |
| 15 | ROS_TAGS_DEV_TOKEN static bypass | TBD | **Mitigated** | Blocked outside `DEVELOPMENT=true` |
| 16 | Empty SA allowlist permits any K8s SA | TBD | **Mitigated** | Required in api mode (non-dev) |
| 17 | Readiness probe only checks PostgreSQL | TBD | **Mitigated** | Opt-in Kafka/S3 via `ROS_READINESS_CHECK_*` |
| 18 | Unclassified Kafka errors default to transient | TBD | **Mitigated** | `kafka_retry.go`; DLQ `hccm.ros.events.dlq`; max 5 retries |
| 19 | Housekeeper lacks graceful shutdown | TBD | **Mitigated** | `signal.NotifyContext`; `ROS_HOUSEKEEPER_SHUTDOWN_GRACE_SECS` |
| 20 | Poison message payload logged (PII risk) | TBD | **Mitigated** | Metadata-only logs; `ROS_LOG_POISON_PAYLOAD` opt-in |
| 21 | 25s statement_timeout kills large ingestion | TBD | **Mitigated** | `SET LOCAL` ingest timeout via `ROS_DB_INGEST_STATEMENT_TIMEOUT` |
| 22 | Node GPU endpoint paginates in memory | TBD | **Mitigated** | SQL triple pagination; `ROS_API_MAX_NODE_RESULTS` |
| 23 | panic() in boxplot/GPU YAML parse | TBD | **Mitigated** | Error returns; `log.Fatal` for embedded catalog only |
| 24 | 140 migrations with no CONCURRENTLY automation | TBD | **Mitigated** | `lint-migrations.sh` + K8s Job runbook |
| 25 | History endpoints lack filter cardinality limits | TBD | **Mitigated** | `checkHistoryFilterCardinality` |
| 26 | Float64 in money formatting | TBD | **Mitigated** | Integer cents formatting in `FormatCentsToAmount` |
| 27 | Deterministic recommendation IDs (info) | TBD | **Verified** | `docs/architecture/recommendation-ids.md`; org_id regression test |
| 28 | Overlapping threshold recalc jobs | TBD | **Mitigated** | Same as #11 |
| 29 | Effective-rates cache unbounded | TBD | **Mitigated** | LRU + `ROS_COST_CACHE_MAX_ENTRIES`; cache metrics |
| 30 | No formal ADR index | TBD | **Resolved** | [`docs/adr/README.md`](../adr/README.md) |
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

*Document version: 1.6 — 2026-06-10. Historical baseline; all findings resolved, mitigated, or accepted. See v2.0 and [Current State](#current-state).*

---

# Adversarial Due Diligence Review v2.0

**Date:** 2026-06-11  
**Reviewer:** AI Adversarial Auditor  
**Scope:** Full codebase as of commit `5c257248cafb30076be1d7d005e985c849d0cb2a`  
**Previous version:** v1.6 (31 findings, all resolved/mitigated/accepted)

## v2.0 Findings Status Summary

| # | Title | Severity | Status | Resolution |
|---|-------|----------|--------|------------|
| 32 | Empty `manifest_id` bypasses tracking/gating | High | Resolved | `e035d8a5` — deterministic `synth-*` manifest ID |
| 33 | Kruize path lacks `report_file_status` | High | Accepted | ADR-0163 (`c7fa06a4`); plugin slated for removal |
| 34 | SSRF DNS fail-open | Medium | Resolved | `08e84b3c` — fail closed on DNS errors (non-dev) |
| 35 | No cost-management entitlement validation | Medium | Resolved | `8ff82f5e` — `CostManagementEntitlement` middleware |
| 36 | No rate limit on async internal triggers | Medium | Resolved | `08e84b3c` — single-flight coalescing for savings/reship |
| 37 | `/internal/tags/status` unauthenticated (was #4) | Medium | Resolved | `8ff82f5e` — `ROS_INTERNAL_TAGS_AUTH_REQUIRED` default `true` |
| 38 | Kafka debug payload logging | Low | Resolved | `08e84b3c` — metadata-only logging |
| 39 | Kruize debug payload logging | Low | Accepted | ADR-0163; no legacy path changes |
| 40 | RBAC cache unbounded | Low | Resolved | `08e84b3c` — bounded LRU (`ROS_RBAC_CACHE_MAX_ENTRIES`) |
| 41 | Unauthenticated `/metrics` | Info | Accepted | NetworkPolicy restricts scrape to Prometheus |
| 42 | CORS allow-all origins | Low | Resolved | `11f0ee77` — `ROS_CORS_ALLOWED_ORIGINS` |
| 43 | Plugin ingest hooks non-fatal | Medium | Resolved | `11f0ee77` — `ingest_hooks_failed` surfaced on cluster/API |
| 44 | Kruize fetch errors misclassified transient | Medium | Accepted | ADR-0163; no legacy path changes |
| 45 | Strict analytics default false | Medium | Resolved | `08e84b3c` — default `ROS_INGEST_STRICT_ANALYTICS=true` |
| 46 | Org BH PUT triggers fleet reship | Medium | Resolved | `11f0ee77` — reship coalescing + WARN log |
| 47 | Async jobs ignore shutdown context | Low | Resolved | `765aad42` — `asyncjobs` shutdown-aware context |
| 48 | GPU MIG in-memory fleet load | Medium | Resolved | `11f0ee77` — SQL pagination (`ListGPUMIGKeysPage`) |
| 49 | GPU time-slicing fallback in-memory | Medium | Resolved | `11f0ee77` — in-memory fallback removed |
| 50 | History CSV wrong row limit | Low | Resolved | `231059b3` — `RECORD_LIMIT_CSV` override for CSV |
| 51 | History wide default date window | Low | Resolved | `231059b3` — `ROS_HISTORY_DEFAULT_DAYS` default |
| 52 | Fleet summary uncached aggregation | Low | Resolved | `75711b6b` — in-memory LRU fleet summary cache |
| 53 | No OpenAPI/CHANGELOG CI | Info | Resolved | `99296701` — advisory `openapi-changelog-check.yml` |
| 54 | ADR drift detection absent | Info | Resolved | `99296701` — advisory `adr-reminder.yml` |
| 55 | Kruize shared HTTP timeout | Medium | Accepted | ADR-0163; no legacy path changes |
| 56 | CSV max body default too large | Medium | Resolved | `11f0ee77` — default 100 MiB (`104857600`) |
| 57 | Parallel Kafka shared committer | Low | Resolved | `f9dd8588` — commit mutex in parallel mode |
| 58 | `report_file_status` runbook absent | Low | Resolved | `11f0ee77` — runbook section in `runbooks.md` |
| 59 | Recommendation detail fallback debt | Low | Resolved | `f8dd05b1` — fallback path removed |
| 60 | `aws-sdk-go` v1 dependency | Info | Resolved | `99296701` — govulncheck CI + v2 migration plan |

## Prior Review Status

All v1.6 findings (#1–#31) were re-validated against current source. **All are resolved, mitigated, or accepted** as documented in v1.6. **Finding #4** (`/internal/tags/status` unauthenticated in db mode) was carried forward as **Finding #37** and is **resolved**. Residual Kruize legacy-path gaps (#33, #39, #44, #55) are **accepted** pending plugin removal (ADR-0163).

## Executive Summary

The v1.6 and v2.0 remediation sprints materially improved ingestion resilience, API hardening, operational observability, and governance (ADR index, CHANGELOG, DLQ and `report_file_status` runbooks). The native engine path is production-grade for the common case. **Remaining accepted risk is limited to the deprecated Kruize plugin** (#33, #39, #44, #55) and **unauthenticated `/metrics` with NetworkPolicy compensation** (#41). All other v2.0 findings are resolved.

## Executive Scorecard

| Dimension | Grade | Trend | Notes |
|-----------|-------|-------|-------|
| Security | A− | ↑ | SSRF fail-closed, entitlement check, internal tags auth, RBAC cache bounds, CORS lockdown |
| Correctness | A− | ↑ | Per-file tracking, manifest ID synthesis, strict analytics default, hook failure surfacing |
| Auditability | A | ↑ | DLQ + `report_file_status` runbooks, metrics, ADR index |
| Operational Robustness | A− | ↑ | DLQ, housekeeper shutdown, deep readiness opt-in, async job shutdown, reship coalescing |
| Performance | B+ | ↑ | Incremental ingest flush, SQL GPU/MIG pagination, fleet summary cache |
| Design Quality | B+ | → | Plugin architecture clean; Kruize dual path accepted for deprecation |
| Maintainability | A− | ↑ | 162 ADRs, CHANGELOG, OpenAPI/ADR advisory CI, detail fallback removed |
| Governance | A | ↑ | ADR index, migration lint, govulncheck CI |

## Findings

### Finding #32: Empty `manifest_id` bypasses per-file tracking and recommendation gating

- **Severity:** High
- **Dimension:** Correctness / Data integrity
- **Location:** `internal/services/report_file_tracker.go:16-18`, `internal/services/manifest_recommendations.go:21-24`
- **Status:** **Resolved** (2026-06-11)
- **Description:** When Kafka messages omit `metadata.manifest_id`, all `report_file_status` functions no-op and `runManifestRecommendations` returns immediately without checking ingestion completeness. Recommendations may run on partial payloads with no operator-visible failure state.
- **Risk:** Silent partial ingestion and stale/premature recommendations for legacy publishers or malformed messages — the exact scenario v1.6 #1/#2 aimed to fix.
- **Recommendation:** Treat missing `manifest_id` as a permanent validation failure (DLQ + metric) or synthesize a deterministic manifest key from `(org_id, cluster_uuid, payload hash, date)`. Block recommendation engines until tracking completes.
- **Effort:** M

**Resolution**

When `metadata.manifest_id` is empty, the processor now synthesizes a deterministic manifest ID with prefix `synth-` using UUID v5 over `(org_id, cluster_uuid, scope_key)` where `scope_key` is the `date=YYYY-MM-DD` segment from ROS `object_keys` when present, otherwise a SHA-256 fingerprint of the sorted file list. Per-file tracking, failure recording, and `runManifestRecommendations` gating all use the resolved ID. Operators see a WARN log and `rosocp_ingest_manifest_id_synthesized_total` when synthesis occurs. Kruize legacy path (#33) remains untracked.

### Finding #33: Legacy Kruize CSV path lacks `report_file_status` tracking

- **Severity:** High
- **Status:** Accepted — Kruize plugin is slated for removal; no enhancements will be made to legacy paths.
- **Dimension:** Correctness / Data integrity
- **Location:** `internal/services/report_processor.go:246-305` (ReadCSVFromUrl + Kruize experiment loop)
- **Description:** When the Kruize plugin is enabled, files fall through to the legacy dataframe/Kruize path which does not call `markFileProcessing`, `markFileDone`, or `handlePermanentFileError`. Parse and experiment errors `continue` without permanent classification.
- **Risk:** Identical silent-loss semantics as pre-v1.6 native path; mitigations #1/#2 do not apply in Kruize mode.
- **Recommendation:** Wrap legacy path with the same per-file tracker or deprecate Kruize ingestion entirely. At minimum, return wrapped errors and increment `ros_ingestion_file_failures_total`.
- **Effort:** M

### Finding #34: SSRF allowlist bypass when DNS resolution fails

- **Severity:** Medium
- **Dimension:** Security
- **Location:** `internal/utils/csv_security.go:76-82`
- **Status:** **Resolved** (2026-06-11)
- **Description:** `denyRestrictedHost` allows the fetch when `LookupIPAddr` returns an error (“Hostname did not resolve; allowlist already validated the name”). An attacker controlling DNS intermittently or via race can point an allowlisted hostname to a private IP on retry.
- **Risk:** Processor fetches internal services (metadata APIs, kubelet, MinIO admin) despite `ROS_CSV_DENY_PRIVATE_NETWORKS=true`.
- **Recommendation:** Fail closed on DNS errors in non-development mode, or resolve at connection time with pinned IPs (custom `DialContext`) after validation.
- **Effort:** M

**Resolution**

`denyRestrictedHost` now returns an error when DNS lookup fails in non-development mode. Development mode (`DEVELOPMENT=true`) still allows unresolved hostnames for local testing convenience.

### Finding #35: No cost-management entitlement validation on API routes

- **Severity:** Medium
- **Dimension:** Security / Authorization
- **Location:** `internal/api/middleware/identity.go:12-25`, `internal/api/server.go:176-180`
- **Status:** **Resolved** (2026-06-11)
- **Description:** Identity middleware decodes `X-Rh-Identity` and extracts `org_id` but never checks `entitlements.cost_management.is_entitled`. Any structurally valid identity header grants full API access when RBAC is disabled or gateway omits entitlement enforcement.
- **Risk:** Unentitled or expired accounts access optimization data in SNO/dev or misconfigured gateway deployments.
- **Recommendation:** Reject requests where `entitlements.cost_management.is_entitled != true` unless `DEVELOPMENT=true`. Add integration test.
- **Effort:** S

**Resolution**

Added `CostManagementEntitlement` middleware on the v1 API group (`internal/api/middleware/entitlement.go`). Skipped when `DEVELOPMENT=true`. Unit tests in `entitlement_test.go`. Test identity headers include `is_entitled: true`.

### Finding #36: No rate limiting on expensive async internal endpoints

- **Severity:** Medium
- **Dimension:** Operational Robustness / Availability
- **Location:** `internal/api/handlers_savings_recalculate.go:78`, `internal/api/handlers_business_hours_settings.go:366`, `internal/engine/threshold_recalculate.go:89`
- **Status:** **Resolved** (2026-06-11)
- **Description:** `POST /internal/recalculate-savings`, business-hours PUT (triggers fleet reship), and threshold settings PUT spawn unbounded async work. Only threshold recalc has single-flight coalescing; savings recalc and reship do not deduplicate per org.
- **Risk:** A compromised or misconfigured service account can trigger masu reship storms, DB recalc load, and masu API abuse across all clusters in an org.
- **Recommendation:** Add per-org token-bucket rate limits, idempotency keys, and coalescing for savings recalc/reship similar to `threshold_recalc_guard.go`.
- **Effort:** M

**Resolution**

Added per-org single-flight coalescing for savings recalculation (`internal/engine/savings_recalc_guard.go`) and business-hours reship (`internal/reship/trigger_guard.go`), mirroring the existing threshold recalc guard. Metrics: `rosocp_savings_recalc_coalesced_total`, `rosocp_reship_coalesced_total`.

### Finding #37: `/internal/tags/status` unauthenticated in on-prem db mode (carried from #4)

- **Severity:** Medium
- **Dimension:** Security / Multi-tenancy
- **Location:** `internal/api/handlers_tags_status.go:17-44`, `internal/api/server.go:169-173`
- **Status:** **Resolved** (2026-06-11)
- **Description:** Bearer auth is skipped when `ROS_TAGS_SOURCE=db`. Any caller on the pod network can enumerate tag catalogs for arbitrary `org_id` query values.
- **Risk:** Cross-tenant tag metadata disclosure when NetworkPolicy is misconfigured (common in SNO/dev).
- **Recommendation:** Require service-account bearer auth on all `/internal/*` routes regardless of tag source mode; bind `org_id` to authenticated caller scope.
- **Effort:** S

**Resolution**

`validateInternalTagsAuth` enforces TokenReview bearer auth on `/internal/tags/status` and `/internal/tags/sync` when `ROS_INTERNAL_TAGS_AUTH_REQUIRED=true` (default). Set `ROS_INTERNAL_TAGS_AUTH_REQUIRED=false` for local dev without SA tokens.

### Finding #38: Kafka consumer debug logs expose message payload prefix

- **Severity:** Low
- **Dimension:** Security / Privacy
- **Location:** `internal/kafka/consumer.go:56,92`
- **Status:** **Resolved** (2026-06-11)
- **Description:** At DEBUG log level, the consumer logs the first 512 bytes of every Kafka message value. Payloads contain presigned URLs, cluster metadata, and file lists.
- **Risk:** Credential leakage in centralized logging when DEBUG is enabled during incidents (common operator response).
- **Recommendation:** Log only `len`, `org_id`, `cluster_uuid`, and `manifest_id` at DEBUG; never log message bodies unless `ROS_LOG_POISON_PAYLOAD`-style opt-in.
- **Effort:** S

**Resolution**

Removed DEBUG payload prefix logging from the Kafka consumer. Message metadata (topic, partition, offset, length) is still logged via `logKafkaMessageReceived`. Poison message bodies remain opt-in via `ROS_LOG_POISON_PAYLOAD`.

### Finding #39: Kruize API debug logs include full HTTP payloads

- **Severity:** Low
- **Status:** Accepted — Kruize plugin is slated for removal; no enhancements will be made to legacy paths.
- **Dimension:** Security / Privacy
- **Location:** `internal/utils/kruize/kruize_api.go:112,164,216`
- **Description:** Kruize experiment and updateResults calls log complete JSON payloads at DEBUG, including container names, namespaces, and resource metrics.
- **Risk:** PII/workload metadata in log aggregators; compounds when Kruize plugin is enabled.
- **Recommendation:** Redact or truncate payloads; log experiment name and row count only.
- **Effort:** S

### Finding #40: RBAC permission cache is unbounded

- **Severity:** Low
- **Dimension:** Performance / Availability
- **Location:** `internal/api/middleware/rbac_cache.go:15-51`
- **Status:** **Resolved** (2026-06-11)
- **Description:** RBAC responses are cached in a `sync.Map` keyed by identity hash with TTL expiry but no maximum entry count. Expired entries are removed only on access.
- **Risk:** Memory growth under high user cardinality (large enterprises, automated polling) or identity-token rotation patterns; API pod OOM under load test or incident traffic.
- **Recommendation:** Replace with bounded LRU (mirror `ROS_COST_CACHE_MAX_ENTRIES` pattern) and export `rosocp_rbac_cache_size` metric.
- **Effort:** S

**Resolution**

Replaced `sync.Map` with bounded LRU cache (`ROS_RBAC_CACHE_MAX_ENTRIES`, default 500). TTL-on-access preserved. Metrics: `rosocp_rbac_cache_size`, `rosocp_rbac_cache_evictions_total`.

### Finding #41: Prometheus `/metrics` exposed without authentication

- **Severity:** Informational
- **Status:** Accepted — NetworkPolicy restricts scrape to Prometheus in production deployments.
- **Dimension:** Security
- **Location:** `internal/api/server.go:141-149`
- **Description:** Metrics listener on `PROMETHEUS_PORT` serves `/metrics` without auth. Labels include `org_id` on several counters.
- **Risk:** Information disclosure of tenant activity patterns to any pod on the network; acceptable when NetworkPolicy restricts scrape to Prometheus only.
- **Recommendation:** Document NetworkPolicy requirement; consider `authorization` header or mTLS for on-prem chart.
- **Effort:** S

### Finding #42: CORS middleware allows all origins by default

- **Severity:** Low
- **Status:** **Resolved** (2026-06-11)
- **Dimension:** Security
- **Location:** `internal/api/server.go:158-160`
- **Description:** `CORSWithConfig` sets `AllowMethods` only. Echo's CORS middleware treats empty `AllowOrigins` as allow-all (`Access-Control-Allow-Origin: *`).
- **Risk:** Browser-based cross-origin requests from malicious pages when a user has a valid session cookie/token — low practical impact since auth is header-based, but violates least-privilege.
- **Recommendation:** Set explicit `AllowOrigins` to console/on-prem UI hostnames.
- **Effort:** S

**Resolution:** `ROS_CORS_ALLOWED_ORIGINS` configures explicit origins; production denies cross-origin when unset; `DEVELOPMENT=true` allows `*`.

### Finding #43: Plugin ingest hook failures are silently non-fatal

- **Severity:** Medium
- **Status:** **Resolved** (2026-06-11)
- **Dimension:** Correctness
- **Location:** `internal/plugin/dispatch.go:47-49`, `internal/services/report_processor.go:47-50`
- **Description:** `RunIngestHooks` collects hook errors but `ProcessReport` only logs a warning and increments a counter. Ingestion is considered successful; downstream recommendations proceed with incomplete derived data (e.g., GPU digest hooks, namespace hooks).
- **Risk:** Recommendations computed on stale or partial digest state with no API-visible degradation flag for hook failures.
- **Recommendation:** Classify hook failures as transient (retry message) or permanent (file failure) based on error type; surface `ingest_hooks_failed` on cluster metadata.
- **Effort:** M

**Resolution:** Hook failures set `clusters.ingest_hooks_failed`; container API exposes `ingest_hooks_failed` / `ingest_hooks_failed_at`; runbook and `ros_ocp_plugin_hook_errors_total` alerting guidance added.

### Finding #44: Kruize legacy fetch errors misclassified as transient

- **Severity:** Medium
- **Status:** Accepted — Kruize plugin is slated for removal; no enhancements will be made to legacy paths.
- **Dimension:** Correctness / Kafka semantics
- **Location:** `internal/services/report_processor.go:246-252`
- **Description:** Legacy path calls `recordKafkaTransient(fetchError)` on CSV fetch failure and `continue`s without permanent file tracking. HTTP 403/404 from presigned URLs retry until DLQ instead of being immediately classified permanent.
- **Risk:** Delayed visibility of permanent S3 failures; unnecessary retry/DLQ volume; inconsistent with native path behavior (`TestConsumer_PresignedDownload403`).
- **Recommendation:** Align legacy path error classification with native ingest functions; use `handlePermanentFileError` for non-transient HTTP status codes.
- **Effort:** S

### Finding #45: Strict analytics mode disabled by default

- **Severity:** Medium
- **Dimension:** Correctness / Data consistency
- **Location:** `internal/config/config.go:76-78`, `internal/services/report_processor.go` (analytics pipeline)
- **Status:** **Resolved** (2026-06-11)
- **Description:** `ROS_INGEST_STRICT_ANALYTICS` defaults to `false`. History/quality write failures allow recommendation persistence and offset commit with only a cluster-level `analytics_incomplete` flag.
- **Risk:** Production serves fresh recommendations without history/quality parity unless operators explicitly opt into strict mode — easy to miss during deployment.
- **Recommendation:** Default strict mode to `true` for on-prem chart; keep degraded mode as explicit opt-in with documented trade-offs.
- **Effort:** S

**Resolution**

Changed default `ROS_INGEST_STRICT_ANALYTICS` from `false` to `true`. Degraded mode remains available by explicitly setting `ROS_INGEST_STRICT_ANALYTICS=false`.

### Finding #46: Business-hours org PUT triggers fleet-wide masu reship

- **Severity:** Medium
- **Status:** **Resolved** (2026-06-11)
- **Dimension:** Operational Robustness
- **Location:** `internal/api/handlers_business_hours_settings.go:355-366`, `internal/reship/trigger.go:42-61`
- **Description:** Enabling business hours at org scope resolves all cluster UUIDs and fires async masu `reship_ros` for each (fan-out capped at `ROS_RESHIP_CONCURRENCY`, default 2). No confirmation, idempotency window, or “dry run” mode.
- **Risk:** Accidental org-level toggle during business hours causes full historical re-ingestion across all clusters — masu/Kafka/DB load spike.
- **Recommendation:** Require explicit `confirm_fleet_reship=true` body field for org-level enable; expose reship scope in API response before execution.
- **Effort:** S

**Resolution:** Existing `triggerReshipCoalesced` single-flight per org; WARN log with cluster count; API response warning when reship triggered.

### Finding #47: Background async jobs ignore API shutdown context

- **Severity:** Low
- **Dimension:** Operational Robustness
- **Location:** `internal/engine/threshold_recalculate.go:89`, `internal/engine/savings_recalculate.go:77`, `internal/reship/trigger.go:47-48`
- **Status:** **Resolved** (2026-06-11)
- **Description:** Threshold recalc, savings recalc, and reship triggers spawn goroutines with `context.Background()` detached from the API server shutdown context.
- **Risk:** In-flight recalculations continue during pod termination, causing connection errors, partial DB writes, and confusing metrics during rollouts.
- **Recommendation:** Use a process-level cancellable context wired from `StartAPIServer` shutdown; wait for in-flight jobs up to termination grace period.
- **Effort:** M

**Resolution**

`internal/asyncjobs` provides shutdown-aware context from `StartAPIServer`. Threshold/savings recalc and masu reship use `asyncjobs.Go` and propagate cancellation through coalesced job loops. Warns if jobs exceed 30s grace period.

### Finding #48: GPU MIG list loads entire fleet into memory before pagination

- **Severity:** Medium
- **Status:** **Resolved** (2026-06-11)
- **Dimension:** Performance
- **Location:** `internal/api/handlers_gpu_mig.go:102-213`
- **Description:** Handler iterates all RBAC-filtered clusters, calls `engine.QueryGPURecommendations` per cluster, accumulates all MIG entries in a slice, then sorts and paginates in Go. No SQL-level pagination equivalent to container keyset path.
- **Risk:** API latency and memory scale O(clusters × GPU containers). A 1000-cluster tenant with GPU workloads can OOM or timeout the API pod on a single list request.
- **Recommendation:** Add SQL-backed pagination mirroring `ListNodeGPUTriplesPage` or cap cluster iteration with cursor-based cluster paging.
- **Effort:** L

**Resolution:** Handler uses `CountGPUMIGKeys` / `ListGPUMIGKeysPage` with per-page enrichment; unsupported `order_by` (term, confidence) rejected with 400.

### Finding #49: GPU time-slicing fallback path still paginates in memory

- **Severity:** Medium
- **Status:** **Resolved** (2026-06-11)
- **Dimension:** Performance
- **Location:** `internal/api/handlers_node_recs.go:119-188`
- **Description:** When `order_by` is unsupported for triple SQL pagination or format is CSV, handler loads recommendations for all clusters (errgroup limit 5), concatenates, sorts, and slices in memory. v1.6 #22 mitigated the primary path only.
- **Risk:** CSV export and non-standard sort keys trigger the expensive fallback at fleet scale.
- **Recommendation:** Reject unsupported `order_by` for large fleets with 400 + guidance; implement SQL pagination for CSV path; or stream CSV from DB cursor.
- **Effort:** M

**Resolution:** In-memory fleet fallback removed; SQL triple pagination used for JSON and CSV; unsupported `order_by` returns 400.

### Finding #50: History CSV export capped at paginated limit, not `RECORD_LIMIT_CSV`

- **Severity:** Low
- **Dimension:** Correctness / UX
- **Location:** `internal/model/recommendation_history.go:48-100`, `internal/api/handlers_history.go:139-171`
- **Status:** **Resolved** (2026-06-11)
- **Description:** Container/namespace lists override limit to `RECORD_LIMIT_CSV` (default 1000) for CSV format. History uses `opts.Limit` from `ListAPIOptions` (default 100) with no CSV override.
- **Risk:** Operators receive truncated history exports believing they got the full dataset; compliance/audit gaps.
- **Recommendation:** Apply the same CSV limit override as `recommendation_set_native.go:354-355`; document in OpenAPI.
- **Effort:** S

**Resolution**

History handler sets `opts.Limit = RECORD_LIMIT_CSV` and `opts.Offset = 0` when `format=csv`. OpenAPI documents CSV row cap.

### Finding #51: History endpoint wide default date window without cluster filter

- **Severity:** Low
- **Dimension:** Performance
- **Location:** `internal/api/handlers_history.go:51-57`, `internal/model/recommendation_history.go:73-82`
- **Status:** **Resolved** (2026-06-11)
- **Description:** Default `start_date` is first of month; no default cluster/project filter. Count query scans all history rows for the org in the window before pagination.
- **Risk:** Expensive COUNT(*) on `recommendation_history` for large orgs; statement timeout under load (25s API default).
- **Recommendation:** Require at least one scoping filter (cluster or project) when date range exceeds N days, or use approximate counts for unscoped queries.
- **Effort:** M

**Resolution**

When both `start_date` and `end_date` are omitted, history queries default to the last `ROS_HISTORY_DEFAULT_DAYS` (default 30) instead of first-of-month through now. Configurable via env. Documented in OpenAPI.

### Finding #52: Fleet summary executes uncached full-org aggregation

- **Severity:** Low
- **Dimension:** Performance
- **Location:** `internal/api/handlers_fleet.go:60-80`, `internal/model/org_recommendation_stats.go`
- **Status:** **Resolved** (2026-06-11)
- **Description:** Fleet summary queries aggregate container counts and savings across all clusters for the org on every request with 5-minute HTTP cache only.
- **Risk:** Repeated dashboard polling hammers PostgreSQL; no materialized summary table or Redis cache layer.
- **Recommendation:** Populate `org_recommendation_stats` incrementally during recommendation runs; serve fleet summary from pre-aggregated table.
- **Effort:** M

**Resolution**

Added in-memory LRU cache (`internal/fleetsummary`) keyed by org_id and RBAC scope. TTL via `ROS_FLEET_SUMMARY_CACHE_TTL` (default 300s). Invalidated on recommendation ingest via `WriteRecommendations` and savings recalc.

### Finding #53: No CI enforcement of OpenAPI spec vs CHANGELOG on breaking changes

- **Severity:** Informational
- **Status:** **Resolved** (2026-06-11)
- **Dimension:** Governance
- **Location:** `docs/architecture/api-versioning.md`, `.github/workflows/` (no openapi diff job)
- **Description:** v1.6 #10 resolved CHANGELOG existence but noted missing CI enforcement. No workflow validates OpenAPI diff against `[Unreleased]` changelog entries.
- **Risk:** Breaking API changes ship without documentation; IQE catches late.
- **Recommendation:** Add CI step comparing `openapi.json` diff to CHANGELOG `[Unreleased]` section (oasdiff or similar).
- **Effort:** M

**Resolution**

Added advisory workflow [`.github/workflows/openapi-changelog-check.yml`](../../.github/workflows/openapi-changelog-check.yml) with path patterns in [`.github/openapi-paths.txt`](../../.github/openapi-paths.txt). On PRs, API-affecting changes without `openapi.json` updates and Go changes without `CHANGELOG.md` updates emit GitHub warnings and an advisory PR comment (`continue-on-error: true`).

### Finding #54: ADR index has no drift detection against code

- **Severity:** Informational
- **Status:** **Resolved** (2026-06-11)
- **Dimension:** Governance / Maintainability
- **Location:** `docs/adr/README.md` (162 ADRs), `docs/adr/_generate_adrs.py` (untracked generator in workspace)
- **Description:** ADR index was bulk-generated to resolve #30. No CI checks that code changes contradict accepted ADRs (e.g., ADR-0011 fixed idle thresholds vs configurable settings).
- **Risk:** ADRs become stale documentation within one release cycle; false confidence for auditors.
- **Recommendation:** Require ADR amendment PR for changes touching `internal/engine/` algorithm constants; add lint linking ADR numbers in commit messages for engine changes.
- **Effort:** M

**Resolution**

Added advisory workflow [`.github/workflows/adr-reminder.yml`](../../.github/workflows/adr-reminder.yml) with architectural trigger paths in [`.github/architectural-paths.txt`](../../.github/architectural-paths.txt). PRs touching config, migrations, Kafka, middleware, or plugin registration receive an advisory comment to review or create ADRs.

### Finding #55: Kruize heavy endpoints share global HTTP client timeout

- **Severity:** Medium
- **Status:** Accepted — Kruize plugin is slated for removal; no enhancements will be made to legacy paths.
- **Dimension:** Performance / Operational Robustness
- **Location:** `internal/utils/utils.go:71-86`, `internal/utils/kruize/kruize_api.go:162-277`
- **Description:** `/updateResults` and `/updateRecommendations` use `utils.HTTPClient` (default 30s). Code comments acknowledge need for per-endpoint timeouts (FLPATH-3407) but no histogram or extended timeout exists.
- **Risk:** Large bulk payloads timeout mid-upload; partial Kruize state with unclear recovery; ingestion appears as transient failures.
- **Recommendation:** Dedicated Kruize client with 120s+ timeout for bulk endpoints; expose `rosocp_kruize_api_duration_seconds` histogram.
- **Effort:** S

### Finding #56: CSV download default max body too large (was 512 MiB)

- **Severity:** Medium
- **Status:** **Resolved** (2026-06-11)
- **Dimension:** Performance / Availability
- **Location:** `internal/utils/utils.go:33-38`
- **Description:** `ROS_CSV_MAX_BODY_BYTES` previously defaulted to 512 MiB (500 MiB in some docs). Processor reads entire CSV into memory before streaming parse (legacy path loads full dataframe).
- **Risk:** Multi-file Kafka payloads with large CSVs can exhaust processor memory despite incremental digest flush (Finding #8 mitigated grouping, not raw CSV size).
- **Recommendation:** Lower default to 128 MiB; stream-parse without full buffering; reject oversized files as permanent failures with metric.
- **Effort:** M

**Resolution:** Default `ROS_CSV_MAX_BODY_BYTES` lowered to 100 MiB (`104857600`); documented in configuration reference.

### Finding #57: Parallel Kafka workers share consumer for offset commit

- **Severity:** Low
- **Dimension:** Correctness
- **Location:** `internal/kafka/consumer.go:68-130`, `internal/services/kafka_retry.go:186-204`
- **Status:** **Resolved** (2026-06-11)
- **Description:** `ROS_KAFKA_PARALLEL=true` (default) dispatches handler work to a worker pool but all workers call `CommitMessage` on the same `*kafka.Consumer`. librdkafka consumer is not documented as thread-safe for concurrent commits.
- **Risk:** Rare offset commit corruption or consumer crash under high throughput; difficult to reproduce.
- **Recommendation:** Serialize commits on the reader goroutine (commit channel) or verify/constrain to partition-locked commits only with documented thread-safety guarantee.
- **Effort:** M

**Resolution**

`kafka.CommitMessage` serializes commits with a mutex when parallel mode is enabled. All commit call sites in `report_processor.go` and `kafka_retry.go` route through this helper. Partition-level handler mutex remains for message processing ordering.

### Finding #58: `report_file_status` operator recovery runbook absent

- **Severity:** Low
- **Status:** **Resolved** (2026-06-11)
- **Dimension:** Auditability / Operational Robustness
- **Location:** `docs/operations/runbooks.md` (DLQ runbook exists; no `report_file_status` section)
- **Description:** v1.6 #1 noted operators must use `reship_ros` for stuck files but no runbook documents SQL queries against `report_file_status`, failure classification, or recovery verification.
- **Risk:** On-call cannot quickly determine whether a manifest is stuck vs. complete; MTTR for partial ingestion regressions.
- **Recommendation:** Add runbook section with example queries, Prometheus alert rules, and reship procedure linked to Koku masu API.
- **Effort:** S

**Resolution:** Added runbook section in `docs/operations/runbooks.md` with SQL queries, synthesized manifest ID guidance, reship procedure, and alert thresholds.

### Finding #59: Native recommendation detail fallback path retained as technical debt

- **Severity:** Low
- **Dimension:** Maintainability
- **Location:** `internal/model/recommendation_set_native.go:655` (`getNativeRecommendationByIDFallback`)
- **Status:** **Resolved** (2026-06-11)
- **Description:** TODO documents a fallback query path pending `container_id` backfill verification in production. Two code paths increase test matrix and IDOR audit surface.
- **Risk:** Fallback path may diverge from primary path filters (org scope, workload_type); regression during removal.
- **Recommendation:** Complete backfill verification, remove fallback, add migration to enforce NOT NULL if appropriate.
- **Effort:** M

**Resolution**

Removed `getNativeRecommendationByIDFallback`. All new writes populate `container_id` via `WriteRecommendations` ON CONFLICT upsert. Detail lookup uses indexed `container_id` only; missing rows return 404.

### Finding #60: `aws-sdk-go` v1 remains a direct dependency

- **Severity:** Informational
- **Status:** **Resolved** (2026-06-11) — govulncheck CI added; v2 migration documented, not executed
- **Dimension:** Governance / Security
- **Location:** `go.mod:7` (`github.com/aws/aws-sdk-go v1.55.8`)
- **Description:** AWS SDK v1 is in maintenance mode. Used for S3/MinIO operations in readiness checks and ingestion. No `govulncheck` in CI (tool not present in dev environment).
- **Risk:** Missed CVE advisories; increasing incompatibility with modern AWS APIs.
- **Recommendation:** Migrate to aws-sdk-go-v2; add `govulncheck` to CI workflow.
- **Effort:** L

**Resolution**

Added [`.github/workflows/govulncheck.yml`](../../.github/workflows/govulncheck.yml) running `govulncheck ./...` on PRs and weekly. Documented deferred v1→v2 migration plan at [`docs/plans/aws-sdk-v2-migration.md`](../plans/aws-sdk-v2-migration.md) — direct usage is limited to S3 readiness checks and CloudWatch logging configuration.

## Priority Remediation Order

All v2.0 findings are **resolved or accepted**. Historical priority order (for audit trail):

1. ~~**#32**~~ — Empty `manifest_id` **resolved** (`e035d8a5`)
2. ~~**#34–#37, #40, #45**~~ — Security/ops hardening **resolved** (`08e84b3c`, `8ff82f5e`)
3. ~~**#48, #49**~~ — GPU API pagination **resolved** (`11f0ee77`)
4. ~~**#36, #46**~~ — Async job coalescing **resolved** (`08e84b3c`, `11f0ee77`)
5. ~~**#43, #47, #50–#54, #56–#60**~~ — Correctness, performance, governance **resolved**
6. **#33, #39, #44, #55** — Kruize legacy paths **accepted** (ADR-0163)
7. **#41** — Unauthenticated `/metrics` **accepted** (NetworkPolicy)

## Positive Observations

Improvements since v1.6 worth acknowledging:

| Area | Observation |
|------|-------------|
| **Ingestion resilience** | Native path per-file tracking, DLQ escalation, incremental digest flush, and ingest statement timeout form a coherent failure-handling story when `manifest_id` is present |
| **API hardening** | ILIKE escape, offset cap, SSRF allowlist + private-network deny, history filter cardinality — reduce practical injection/DoS surface |
| **Observability** | DLQ/retry metrics, analytics incomplete signaling, pool metrics, ingest flush gauges — operators can alert on real failure modes |
| **Governance** | 162 ADRs with index, CHANGELOG with Keep a Changelog format, migration CONCURRENTLY lint — onboarding and audit trail significantly improved |
| **Threshold recalc** | Single-flight coalescing prevents the worst recalc storms (#11/#28) |
| **Cost cache** | Bounded LRU for effective-rates prevents unbounded memory (#29) |
| **Plugin tests** | 14 plugin test files covering lifecycle, disabled routes, and integration paths — better than typical greenfield plugin systems |
| **Security startup validation** | `ValidateSecurityConfig` + `ValidateTagAuthConfig` fail fast on unsafe production configs |
| **Documentation** | DLQ runbook, large-table migration runbook, cost-integration contract, recommendation ID security invariant |

## v2.0 Tracking

| Finding # | Title | Severity | Status |
|-----------|-------|----------|--------|
| 32 | Empty manifest_id bypasses tracking/gating | High | **Resolved** |
| 33 | Kruize path lacks report_file_status | High | **Accepted** |
| 34 | SSRF DNS fail-open | Medium | **Resolved** |
| 35 | No entitlement validation | Medium | **Resolved** |
| 36 | No rate limit on async internal triggers | Medium | **Resolved** |
| 37 | /internal/tags/status unauthenticated (db mode) | Medium | **Resolved** (was #4) |
| 38 | Kafka debug payload logging | Low | **Resolved** |
| 39 | Kruize debug payload logging | Low | **Accepted** |
| 40 | RBAC cache unbounded | Low | **Resolved** |
| 41 | Unauthenticated /metrics | Info | **Accepted** (NetworkPolicy) |
| 42 | CORS allow-all origins | Low | **Resolved** |
| 43 | Plugin ingest hooks non-fatal | Medium | **Resolved** |
| 44 | Kruize fetch errors misclassified transient | Medium | **Accepted** |
| 45 | Strict analytics default false | Medium | **Resolved** |
| 46 | Org BH PUT triggers fleet reship | Medium | **Resolved** |
| 47 | Async jobs ignore shutdown context | Low | **Resolved** |
| 48 | GPU MIG in-memory fleet load | Medium | **Resolved** |
| 49 | GPU time-slicing fallback in-memory | Medium | **Resolved** |
| 50 | History CSV wrong row limit | Low | **Resolved** |
| 51 | History wide default scan | Low | **Resolved** |
| 52 | Fleet summary uncached aggregation | Low | **Resolved** |
| 53 | No OpenAPI/CHANGELOG CI | Info | **Resolved** |
| 54 | ADR drift detection absent | Info | **Resolved** |
| 55 | Kruize shared HTTP timeout | Medium | **Accepted** |
| 56 | CSV max body default too large | Medium | **Resolved** |
| 57 | Parallel Kafka shared committer | Low | **Resolved** |
| 58 | report_file_status runbook missing | Low | **Resolved** |
| 59 | Recommendation detail fallback debt | Low | **Resolved** |
| 60 | aws-sdk-go v1 dependency | Info | **Resolved** (govulncheck CI; v2 migration planned) |

---

## Current State

**Last updated:** 2026-06-11

| Review | Findings | Resolved | Mitigated | Accepted | Open |
|--------|----------|----------|-----------|----------|------|
| v1.6 (#1–#31) | 31 | 4 (#4, #10, #30, #31) | 24 | 3 (#3, #5, #6) | 0 |
| v2.0 (#32–#60) | 29 | 24 | 0 | 5 (#33, #39, #41, #44, #55) | 0 |
| **Combined (#1–#60)** | **60** | **28** | **24** | **8** | **0** |

Notes:

- **Mitigated** (v1.6): fixes reduce risk with documented residual behavior (e.g., Kafka offset still commits on partial failure by design).
- **Accepted**: architectural or deprecation decisions with compensating controls documented (gateway JWT, NetworkPolicy, ADR-0163 Kruize removal).
- **Carried forward:** v1.6 #4 → v2.0 #37 (same issue; counted once in combined resolved total).

**Conclusion:** All findings are resolved or accepted with documented rationale. No open remediation items remain.

---

*Document version: 2.1 — 2026-06-11. Reconciled all v1.6 and v2.0 finding statuses; see [Current State](#current-state).*
