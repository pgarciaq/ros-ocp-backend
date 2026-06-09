# Adversarial Due Diligence Review — ros-ocp-backend

> **INTERNAL USE ONLY** — This document is an internal security and engineering audit. It is not for public disclosure, customer distribution, or external publication without explicit security and legal review.

**Date:** 2026-06-08  
**Scope:** `ros-ocp-backend` — Kafka ingestion pipeline, native recommendation engine, REST API, database layer, authentication/authorization, operational readiness, and engineering governance  
**Methodology:** Adversarial due diligence combining static code review, architecture analysis, threat modeling (STRIDE-lite), and operational failure-mode analysis. Reviewers assumed the **SNO/dev deployment posture** (`ROS_TAGS_SOURCE=db`, `RBAC_ENABLE=false`, no gateway) with network access to the API port unless otherwise noted. Findings were validated against source locations and cross-referenced for compound failure chains.

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
| **Data integrity (Kafka ingestion)** | 🟠 High (mitigated) | Per-file tracking and error surfacing implemented; manual reship still required for recovery |
| **Authentication** | 🟢 Delegated to gateway | Accepted architecture; weak only if gateway bypassed (SNO/dev posture) |
| **Authorization** | 🟢 Strong when chart defaults used | `rbac.enabled: true` in production; weak only in SNO/dev overrides |
| **API security** | 🟠 Medium–High | ILIKE injection, unbounded offset, SSRF when allowlist unset |
| **Database & connections** | 🟠 Medium | Dual connection pools; statement timeout conflicts with large ingestion |
| **Memory & performance** | 🟠 Medium | Streaming ingest holds full grouped map; node GPU endpoints paginate in memory |
| **Operational resilience** | 🟠 Medium | Readiness probe shallow; unclassified Kafka errors stall consumer; no graceful housekeeper shutdown |
| **Pipeline correctness** | 🟠 Medium | Recommendations persist when history/quality writes fail |
| **Engineering governance** | 🟡 Low–Medium | No CHANGELOG; no ADR index; 134+ migrations without CONCURRENTLY automation |
| **Positive controls** | 🟢 Strong | Plugin architecture, parameterized SQL, service-account auth patterns (when enabled), structured metrics |

**Overall assessment:** The native engine and API surface are functionally mature. **Findings #1 and #2 are mitigated** via per-file `report_file_status` tracking, surfaced ingestion errors, recommendation gating, and a `ros_ingestion_file_failures_total` Prometheus alert — matching the platform pattern used by Koku's `CostUsageReportStatus`. Residual risk: operators must run `reship_ros` for stuck manifests with failed files. Several auth findings (#3, #5, #6) are **deployment-specific shortcuts** in the SNO/dev posture and are mitigated by gateway JWT validation, RBAC defaults, and NetworkPolicy in standard SaaS and on-prem chart deployments.

---

## Priority Remediation Order

Remediation is ordered by **compound risk** (findings that amplify each other) and **blast radius** (data loss > auth bypass > availability > governance).

| # | Finding(s) | Rationale |
|---|------------|-----------|
| 1 | **#1, #2** (High — mitigated) | Per-file tracking, error propagation, recommendation gating, and alerting implemented. Residual: manual `reship_ros` for failed files. |
| 2 | **#18** (Medium — Kafka stall) | Unclassified errors cause infinite redelivery — consumer group makes no progress; pairs with #1 for opposite failure mode (stall vs. skip). |
| 3 | **#8, #21** (Medium — ingestion scale) | Memory accumulation and statement timeout both manifest under large-cluster ingestion; fix together to avoid OOM ↔ retry loops. |
| 4 | **#9** (High — pipeline degraded) | Fresh recommendations without analytics misleads operators and fleet metrics; add strict mode or staleness signaling. |
| 5 | **#7** (High — dual pools) | Connection exhaustion is silent until cascade failure; large effort but prevents production incidents under load. |
| 6 | **#11, #28** (Medium — recalc storms) | Unbounded goroutines and overlapping threshold jobs threaten availability after settings changes. |
| 7 | **#12, #13, #14** (Medium — API hardening) | SSRF, ILIKE wildcard injection, deep-pagination DoS — quick wins. |
| 8 | **#15, #16** (Medium — tag auth config) | Dev token bypass and empty SA allowlist are configuration footguns; primary risk when `api` tag mode is used without explicit SA allowlist. |
| 9 | **#17, #19, #20** (Medium — ops) | Readiness depth, graceful shutdown, PII in poison logs. |
| 10 | **#22, #23** (Medium — memory/panic) | Node GPU in-memory pagination; panic on parse failures. |
| 11 | **#24** (Medium — migrations) | CONCURRENTLY automation — plan for next large-table index. |
| 12 | **#10, #30** (High/Low — governance) | CHANGELOG and ADR index — process debt, not incident drivers. |
| 13 | **#25–#29** (Low/Info) | Cardinality limits, float formatting, cache bounds, deterministic IDs — address opportunistically. |
| — | **#3** (Info — architecture) | Gateway enforcement is already in place in SaaS and on-prem chart. Document as architecture requirement; optional in-app JWT validation is defense-in-depth only. |
| — | **#4** (Medium — hardening) | Authenticate `/internal/*` in db mode — hardening nice-to-have; NetworkPolicy mitigates in default on-prem chart. |
| — | **#5, #6** (Low/Info — deployment-specific) | RBAC disabled and cross-tenant SA scope are SNO/dev overrides or accepted platform architecture, not production gaps. |

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
| #7 Dual pools | ✓ | ✓ | ✓ |
| #8 Memory grouped map | ✓ | ✓ | ✓ |
| #9 Pipeline degraded | ✓ | ✓ | ✓ |
| #10 No CHANGELOG | ✓ | ✓ | ✓ |
| #11 Recalc storms | ✓ | ✓ | ✓ |
| #12 SSRF allowlist | ✓ | ✓ | ✓ |
| #13 ILIKE injection | ✓ | ✓ | ✓ |
| #14 Deep pagination | ✓ | ✓ | ✓ |
| #15 Dev token | ✗ (not set) | ✗ (not set) | ⚠ (if configured) |
| #16 Empty SA allowlist | ⚠ (if unset) | ⚠ (api mode only) | ⚠ (if configured) |
| #17 Readiness shallow | ✓ | ✓ | ✓ |
| #18 Kafka stall | ✓ | ✓ | ✓ |
| #19 Housekeeper shutdown | ✓ | ✓ | ✓ |
| #20 PII in logs | ✓ | ✓ | ✓ |
| #21 Statement timeout | ✓ | ✓ | ✓ |
| #22 Node GPU memory | ✓ | ✓ | ✓ |
| #23 panic() parse | ✓ | ✓ | ✓ |
| #24 Migrations CONCURRENTLY | ✓ | ✓ | ✓ |
| #25–#30 Low/Info | ✓ | ✓ | ✓ |

---

## Findings — High

### Finding #1 — Kafka offset committed after partial file failure

| Field | Value |
|-------|-------|
| **Severity** | High (mitigated) |
| **Category** | Data integrity / Kafka consumer semantics |
| **Location** | `internal/services/report_processor.go`, `internal/model/report_file_status.go` |
| **Effort** | M |

**Description**

The report processing loop iterates over files in a multi-file Kafka payload. When a single file fails permanently, the Kafka offset is still committed (by design, to avoid blocking the consumer group). Previously this caused silent data loss with no recovery path.

**Exploit / trigger**

Not attacker-driven. Any permanent S3/MinIO glitch, corrupt CSV, or missing object key on **one file** in a multi-file payload could permanently drop that file's data without operator visibility.

**Mitigation (implemented)**

- Added `report_file_status` table tracking per-file state (`pending`, `processing`, `done`, `failed`) keyed by `(manifest_id, filename)`.
- Kafka messages now carry `manifest_id` and `expected_files` (from Koku `ROSReportShipper`) so ros-ocp-backend knows the full expected file set.
- Failed files are recorded with `status=failed` and `error_message`; idempotent re-delivery skips files already marked `done`.
- Recommendation engines are **gated** until all expected manifest files reach `done` — processed data is kept, recommendations wait for complete ingestion.
- Matches the platform pattern used by Koku's `CostUsageReportStatus` per-file tracking.
- `ros_ingestion_file_failures_total` Prometheus counter (labels: `org_id`, `cluster_id`, `report_type`, `error_class`) surfaces failures immediately; cost-onprem chart includes an alerting rule.

**Residual risk**

Operators must manually intervene via Koku's `reship_ros` API (with optional `manifest_id` filter) to re-deliver failed files. Offset commit behavior is unchanged — the queue does not stall on partial failure.

---

### Finding #2 — Native ingestion errors swallowed (return nil)

| Field | Value |
|-------|-------|
| **Severity** | High (mitigated) |
| **Category** | Data integrity / error propagation |
| **Location** | `internal/services/report_processor.go` — native ingest functions |
| **Effort** | S |

**Description**

Non-transient ingestion errors (S3 403, fetch failures, parse errors) were logged and metrics incremented, but functions returned `nil` (success). The caller never learned the file failed.

**Exploit / trigger**

Compounded Finding #1: permanent fetch/parse failures appeared successful, preventing failure tracking and recommendation gating.

**Mitigation (implemented)**

- All native ingest functions (`processContainerCSVIngest`, `processNamespaceCSVIngest`, etc.) now return wrapped errors for permanent failures.
- `ProcessReport` classifies errors as transient (blocks offset commit) vs permanent (records `report_file_status.failed`, increments `ros_ingestion_file_failures_total`, continues to next file).
- Structured error logging on file failure includes `org_id`, `cluster_uuid`, `report_type`, and `error_class`.
- Unit tests assert non-nil return on S3 403 and HTTP failures.

**Residual risk**

Same as Finding #1: recovery requires targeted `reship_ros` after investigating `report_file_status` and Prometheus alerts.

---

### Finding #7 — Dual DB connection pools (GORM + pgxpool)

| Field | Value |
|-------|-------|
| **Severity** | High |
| **Category** | Performance / reliability |
| **Location** | `internal/db/db.go` |
| **Effort** | L |

**Description**

Two independent database connection pools exist: GORM (via `database/sql` defaults) and pgxpool (tuned via `ROS_DB_MAX_CONNS`). Under load, one pool can exhaust connections while the other remains idle, or combined usage can exceed PostgreSQL `max_connections`.

**Exploit / trigger**

Production traffic spike or parallel ingestion + API queries cause silent connection wait timeouts, 500 errors, or ingestion stalls with no clear single-pool metric.

**Recommended fix**

Migrate remaining GORM code paths to pgxpool, or configure `sql.DB` `SetMaxOpenConns` / `SetMaxIdleConns` from the same config source. Export per-pool connection metrics and alert on saturation.

---

### Finding #8 — Streaming ingest accumulates all groups in memory

| Field | Value |
|-------|-------|
| **Severity** | High |
| **Category** | Performance / availability |
| **Location** | `internal/ingestion/pipeline_stream.go` — `groupedAll` map (lines 135–221) |
| **Effort** | M |

**Description**

Despite streaming CSV parsing, all container-day digest groups are held in a `groupedAll` map until EOF. Large clusters (10k containers × 14 days) can accumulate gigabytes in memory before flush, risking OOMKill mid-ingestion.

**Exploit / trigger**

Operational — large fleet report payload processed on a pod with constrained memory limits. OOMKill loses in-flight work; combined with Finding #1 may commit offset after partial processing.

**Recommended fix**

Flush digest groups incrementally by month or fixed batch size. Do not retain the full grouped map through EOF. Add memory usage metrics during ingestion.

---

### Finding #9 — Pipeline writes recommendations when history/quality fails

| Field | Value |
|-------|-------|
| **Severity** | High |
| **Category** | Data consistency |
| **Location** | `internal/services/report_processor.go` — `pipelineDegraded` (lines 589–625) |
| **Effort** | M |

**Description**

Container recommendations are persisted and Kafka offsets committed even when history or quality metric writes fail. The API serves fresh recommendations without corresponding analytics history.

**Exploit / trigger**

Operational — transient DB error or timeout on history/quality tables during otherwise successful ingestion. Fleet savings summaries and quality dashboards diverge from recommendation data without surfacing error state to API consumers.

**Recommended fix**

Add configurable strict mode that blocks recommendation commit on analytics failure. Alternatively expose `rosocp_analytics_incomplete_total` metric and an API staleness flag on affected clusters. Document degraded-mode behavior in operations runbooks.

---

### Finding #10 — No CHANGELOG.md despite API versioning policy

| Field | Value |
|-------|-------|
| **Severity** | High |
| **Category** | Governance / API contract |
| **Location** | `docs/architecture/api-versioning.md` (references CHANGELOG) vs. repo root (missing) |
| **Effort** | S |

**Description**

The API versioning policy documents breaking-change procedures referencing `CHANGELOG.md`, but no such file exists. Breaking API changes ship without a documented deprecation trail.

**Exploit / trigger**

Not security — integration breakage. UI and IQE tests fail silently in staging; customers on on-prem miss upgrade notes for behavior changes.

**Recommended fix**

Add `CHANGELOG.md` at repo root. Enforce OpenAPI spec diff in CI with failure on removed or renamed fields without changelog entry.

---

## Findings — Medium

### Finding #4 — `/internal/tags/status` unauthenticated in on-prem (db mode)

| Field | Value |
|-------|-------|
| **Severity** | Medium (on-prem db-mode only) |
| **Category** | Authorization / multi-tenancy |
| **Location** | `internal/api/handlers_tags_status.go` (lines 17–44), `internal/api/server.go` (lines 169–174) |
| **Effort** | S |

**Description**

When `ROS_TAGS_SOURCE=db` (on-prem default), bearer authentication is skipped for `/internal/tags/status`. The endpoint accepts an arbitrary `org_id` query parameter, enabling cross-tenant tag enumeration.

**Exploit / trigger**

Any pod or user on the cluster network calls `GET /internal/tags/status?org_id=<victim>` without credentials and receives tag sync status for other tenants.

**Deployment context**

Only affects `ROS_TAGS_SOURCE=db` (on-prem). In SaaS (`api` mode), bearer auth is always required. On-prem chart NetworkPolicy restricts access to internal endpoints. Low blast radius but should still be hardened.

**Recommended fix**

Always require service-account bearer auth on `/internal/*` routes regardless of tag source mode. Bind `org_id` to the authenticated caller's namespace or explicit SA allowlist.

---

### Finding #11 — No rate limiting; recalc goroutines spawn without dedup

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Category** | Availability |
| **Location** | `internal/api/server.go`, `internal/engine/threshold_recalculate.go` (lines 64–79) |
| **Effort** | M |

**Description**

Settings `PUT` handlers spawn unbounded async goroutines for threshold recalculation with no deduplication or rate limiting.

**Exploit / trigger**

Rapid or automated settings changes (or malicious client) launch hundreds of concurrent recalc jobs, exhausting DB connections and CPU.

**Recommended fix**

Per-org mutex or job queue with coalescing of duplicate in-flight jobs. Reject or queue when recalc already running for `(org_id, recType)`.

---

### Finding #12 — SSRF risk when CSV host allowlist unset

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Category** | Security / SSRF |
| **Location** | `internal/utils/utils.go` (lines 59–83) |
| **Effort** | S |

**Description**

When `ROS_CSV_ALLOWED_HOSTS` is empty, any URL in a Kafka message payload can be fetched by the processor.

**Exploit / trigger**

Compromised Kafka producer or poison message with `file://` or internal metadata URL causes server-side fetch of cloud metadata, internal services, or RFC1918 addresses.

**Recommended fix**

Require explicit allowlist in all environments. When empty, block all fetches or deny RFC1918/link-local ranges. Fail startup if allowlist unset in non-development mode.

---

### Finding #13 — ILIKE wildcard injection

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Category** | Authorization bypass |
| **Location** | `internal/api/common.go` (lines 36–37), `internal/api/utils.go` (lines 262–264) |
| **Effort** | S |

**Description**

Filter values containing `%` or `_` are passed unescaped to ILIKE queries, matching all rows instead of the intended literal substring.

**Exploit / trigger**

User with limited RBAC scope passes `filter[namespace]=%` and receives rows across all namespaces, bypassing authorization-by-filter intent.

**Recommended fix**

Escape `%` and `_` in ILIKE operands (e.g., `escape '\'` clause). Add tests for wildcard characters in filter values.

---

### Finding #14 — Unbounded offset (deep-pagination DoS)

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Category** | Availability / DoS |
| **Location** | `internal/api/listoptions/list_options.go` (lines 137–144) |
| **Effort** | S |

**Description**

The `offset` query parameter accepts any non-negative integer with no upper bound. Requests like `?limit=1000&offset=999999999` force PostgreSQL to skip millions of rows.

**Exploit / trigger**

Authenticated or unauthenticated client (depending on route) sends deep-offset requests, causing long-running queries and connection pool exhaustion.

**Recommended fix**

Cap offset (e.g., 10,000) with clear 400 response, or require keyset/cursor pagination for pages beyond the cap.

---

### Finding #15 — ROS_TAGS_DEV_TOKEN static bypass

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Category** | Authentication |
| **Location** | `internal/tags/auth.go` (lines 52–56) |
| **Effort** | S |

**Description**

When `ROS_TAGS_DEV_TOKEN` is set, it bypasses Kubernetes TokenReview entirely, accepting a static shared secret.

**Exploit / trigger**

Misconfiguration in production leaves a known static token granting full tag-sync API access.

**Deployment context**

Not set in any production values (chart default is empty string). Risk only if accidentally configured. Startup guard recommended as defense-in-depth.

**Recommended fix**

Fail startup if `ROS_TAGS_DEV_TOKEN` is set when `DEVELOPMENT` is not `true`. Log prominent warning in development mode.

---

### Finding #16 — Empty SA allowlist permits any K8s service account

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Category** | Authentication |
| **Location** | `internal/tags/auth.go` (lines 129–132) |
| **Effort** | S |

**Description**

When `ROS_TAGS_ALLOWED_SERVICE_ACCOUNTS` is empty, any service account passing TokenReview is accepted.

**Exploit / trigger**

Any compromised pod in the cluster can call tag-sync internal APIs.

**Deployment context**

Chart default is empty (permissive). SaaS guidance sets explicit allowlist. Primary risk is on-prem deployments that use `api` tag mode without configuring allowed SAs.

**Recommended fix**

Default-deny: require explicit non-empty allowlist in production. Fail startup validation if allowlist empty outside development.

---

### Finding #17 — Readiness probe only checks PostgreSQL

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Category** | Operational readiness |
| **Location** | `internal/db/db.go`, `internal/utils/utils.go` (`/readyz`) |
| **Effort** | M |

**Description**

The `/readyz` endpoint verifies PostgreSQL connectivity only. Kafka, S3/MinIO, and Masu/Koku reachability are not checked. Pods are marked ready while unable to process messages.

**Exploit / trigger**

Kafka broker restart or S3 outage — Kubernetes continues routing work to "ready" pods that immediately fail or stall on consume.

**Recommended fix**

Add optional dependency health checks (configurable per deployment mode). Document accepted risk with external lag alerts if shallow probe is intentional.

---

### Finding #18 — Unclassified Kafka errors default to transient → consumer stall

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Category** | Kafka consumer semantics |
| **Location** | `internal/services/kafka_processing_errors.go` |
| **Effort** | M |

**Description**

Unknown error types are classified as transient, preventing offset commit. The consumer redelivers the same message indefinitely with no progress.

**Exploit / trigger**

New error class (e.g., unexpected DB constraint, novel S3 error code) causes infinite redelivery loop for the partition. Consumer lag grows unbounded; no alert distinguishes stall from backpressure.

**Recommended fix**

After N retries, invert default for unclassified errors to permanent/poison with DLQ. Add `rosocp_kafka_unclassified_error_total` metric for alerting.

---

### Finding #19 — Housekeeper lacks graceful shutdown

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Category** | Operational resilience |
| **Location** | `cmd/start.go` (housekeeper subcommand) |
| **Effort** | S |

**Description**

The housekeeper process does not wire `signal.NotifyContext` or graceful consumer close. Pod termination interrupts in-flight retention or cleanup work.

**Exploit / trigger**

Rolling deploy or node drain during housekeeper run leaves partial deletes, open transactions, or inconsistent retention state.

**Recommended fix**

Wire SIGTERM/SIGINT handling with configurable grace period and consumer/worker drain before exit.

---

### Finding #20 — Poison message payload logged (PII risk)

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Category** | Privacy / logging |
| **Location** | `internal/services/report_processor.go` (`commitOnPermanentFailure`) |
| **Effort** | S |

**Description**

Up to 64 KB of raw Kafka payload is logged for debugging when permanently failing a message. Payloads may contain cluster metadata, namespace names, workload identifiers, and resource usage (PII/operational sensitivity).

**Exploit / trigger**

Operational — log aggregation systems retain sensitive workload data. Compliance review flags excessive data in logs.

**Recommended fix**

Log only `request_id`, `org_id`, `cluster_uuid`, and error class. Store full payload in a restricted dead-letter store with retention policy.

---

### Finding #21 — 25s statement_timeout kills large ingestion

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Category** | Data integrity / performance |
| **Location** | `internal/db/db.go` (`setStatementTimeout`) |
| **Effort** | S |

**Description**

A global 25-second `statement_timeout` applies to ingestion batch inserts/upserts. Large clusters may exceed this limit.

**Exploit / trigger**

Operational — ingestion batch times out, error classified as transient (Finding #18), infinite retry loop with no data progress.

**Recommended fix**

Session-level timeout override for processor database role, or raise timeout for ingestion-specific connections. Document recommended timeout values by cluster size tier.

---

### Finding #22 — Node GPU endpoint paginates in memory

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Category** | Performance / scalability |
| **Location** | `internal/api/handlers_node_recs.go`, `internal/api/handlers_node_utilization.go` |
| **Effort** | M |

**Description**

Node and GPU recommendation endpoints load all matching results into memory, compute recommendations, then paginate in Go.

**Exploit / trigger**

Large fleet queries return high memory usage per request; concurrent API calls risk OOM or latency spikes.

**Recommended fix**

Push pagination and filtering into SQL. Limit cluster fan-out per request. Add integration tests at 1k+ node scale.

---

### Finding #23 — panic() in boxplot/GPU YAML parse

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Category** | Reliability |
| **Location** | `internal/model/boxplot.go` (line 58), `internal/engine/vgpu_profiles.go` |
| **Effort** | S |

**Description**

Unhandled enum values or YAML parse failures call `panic()`, crashing the process at runtime rather than returning a controlled error.

**Exploit / trigger**

Malformed config map, unexpected DB enum value, or corrupt GPU profile YAML causes pod crash loop.

**Recommended fix**

Return errors to callers. Validate GPU profiles and enum mappings at startup with non-fatal degraded mode (skip affected feature, log alert).

---

### Finding #24 — 134+ migrations with no CONCURRENTLY automation

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Category** | Operations / deploy safety |
| **Location** | `migrations/`, `migrations/README.md` |
| **Effort** | L |

**Description**

golang-migrate wraps migrations in transactions, making `CREATE INDEX CONCURRENTLY` impossible within standard migration files. Large production tables require manual pre-deploy index creation.

**Exploit / trigger**

Automated deploy on large tenant adds blocking index, locking recommendation tables for minutes and stalling ingestion and API.

**Recommended fix**

CI check flagging new indexes on large tables. Automate CONCURRENTLY index creation via separate Kubernetes Job documented in upgrade runbook.

---

## Findings — Low / Info

### Finding #3 — Identity header trusted without JWT verification

| Field | Value |
|-------|-------|
| **Severity** | Info (accepted architecture) |
| **Category** | Authentication |
| **Location** | `internal/api/middleware/identity.go` (lines 12–25) |
| **Effort** | M (optional defense-in-depth) |

**Description**

The middleware base64-decodes the `X-Rh-Identity` header and trusts its contents without verifying JWT signatures, expiry, issuer, or entitlements. Any client that can reach the API port directly can impersonate any organization.

**Exploit / trigger**

Attacker with network access to the ROS API port crafts a base64 JSON identity with arbitrary `org_id` and admin flags. Full cross-tenant data access **only if no gateway validates tokens upstream** (SNO/dev posture).

**Deployment context**

By design, this service relies on an upstream gateway (3scale/Envoy/Keycloak) to validate JWT and inject trusted X-Rh-Identity. The ROS API must never be exposed directly to untrusted networks. NetworkPolicy enforces this in the cost-onprem chart. This matches the pattern used by all Insights platform services.

**Recommended fix (optional defense-in-depth)**

Validate JWT signature and claims when `RBAC_ENABLE` or a new `IDENTITY_VALIDATION_ENABLE` flag is set; alternatively enforce strict NetworkPolicy restricting API port to gateway pods only. Not a required fix when gateway and NetworkPolicy are correctly deployed.

---

### Finding #5 — Settings mutation without RBAC (SNO/dev override)

| Field | Value |
|-------|-------|
| **Severity** | Low (deployment-specific) |
| **Category** | Authorization / integrity |
| **Location** | `internal/api/settings_rbac.go` (lines 14–16), `internal/config/config.go` (line 503) |
| **Effort** | S |

**Description**

When `RBAC_ENABLE=false`, any authenticated user can `PUT` optimization thresholds, triggering cluster-wide recalculation jobs. No `settings.write` permission check is enforced.

**Exploit / trigger**

Compromised or over-privileged user account changes thresholds for all recommendation types, causing incorrect recommendations fleet-wide and spawning expensive recalc goroutines (availability impact). **Only exploitable when RBAC is disabled.**

**Deployment context**

Default cost-onprem chart sets `rbac.enabled: true`. Only the SNO aarch64 dev cluster disables RBAC due to insights-rbac being amd64-only. Not a production vulnerability in standard deployments.

**Recommended fix**

Require `settings.write` permission even when RBAC is disabled, or document and enforce a single-admin deployment constraint with network-level access control. Fail startup in production if `RBAC_ENABLE=false` without explicit `ROS_ACCEPT_INSECURE_RBAC=true` acknowledgment.

---

### Finding #6 — Internal service account can act on any org_id

| Field | Value |
|-------|-------|
| **Severity** | Info (accepted architecture) |
| **Category** | Authorization / multi-tenancy |
| **Location** | `internal/api/handlers_savings_recalculate.go` (lines 61–78), `internal/api/handlers_tags_sync.go` (lines 37–52) |
| **Effort** | M (optional hardening) |

**Description**

Bearer token authentication validates the caller is a Kubernetes service account, but `org_id` in the request body is not bound to the caller's identity, namespace, or audience. Any allowed SA can trigger recalculation or tag sync for any organization.

**Exploit / trigger**

Compromised listener or masu pod (or any SA passing TokenReview) sends recalc/tag-sync requests targeting victim `org_id` values.

**Deployment context**

By design, platform service accounts (koku-worker, masu) operate cross-tenant. Internal endpoints are not user-facing and are restricted by NetworkPolicy + SA allowlist. Hardening lever is Finding #16 (explicit SA allowlist).

**Recommended fix (optional hardening)**

Restrict allowed service accounts per operation type. Validate `org_id` against token namespace, audience, or a configured SA→org mapping. Reject mismatched org claims.

---

### Finding #25 — History endpoints lack filter cardinality limits

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Category** | Availability |
| **Location** | `internal/api/handlers_history.go` (lines 64–80) |
| **Effort** | S |

**Description**

History handlers do not apply `MaxCountPerQueryParam` checks. Hundreds of filter values produce large `IN` lists and slow queries.

**Exploit / trigger**

Client sends many repeated filter params; query plan degrades, connection held for extended period.

**Recommended fix**

Reuse cardinality checks from main list handlers. Return 400 when count exceeds limit.

---

### Finding #26 — Float64 in money formatting

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Category** | Correctness |
| **Location** | `internal/money/format.go` (lines 16–18) |
| **Effort** | S |

**Description**

Integer cents are divided by `100.0` into `float64` for display formatting. Theoretical precision loss for extreme values (very large fleet savings aggregates).

**Exploit / trigger**

Edge case only — multi-trillion cent values could display incorrectly. Not a practical attack vector.

**Recommended fix**

Use integer-only formatting (cents → dollars via div/mod) or a decimal library.

---

### Finding #27 — Deterministic recommendation IDs

| Field | Value |
|-------|-------|
| **Severity** | Info |
| **Category** | Security awareness |
| **Location** | `internal/model/recommendation_set_native.go` (lines 617–678) |
| **Effort** | Info only |

**Description**

Recommendation IDs are UUID v5 derived from cluster, namespace, workload, and container identifiers. IDs are guessable if metadata is known.

**Exploit / trigger**

Acceptable if org boundary is enforced on all detail endpoints. IDs must not be treated as secret capability tokens.

**Recommended fix**

Document in API guide that recommendation IDs are deterministic identifiers, not secrets. Ensure all detail routes enforce org-scoped authorization.

---

### Finding #28 — Overlapping threshold recalc jobs

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Category** | Correctness / availability |
| **Location** | `internal/engine/threshold_recalculate.go` (lines 64–79) |
| **Effort** | S |

**Description**

Rapid successive settings `PUT` requests launch concurrent recalc goroutines with interleaved writes to recommendation tables.

**Exploit / trigger**

Automated threshold tuning or flaky client causes duplicate work and transient inconsistent recommendation snapshots.

**Recommended fix**

Per-`(org_id, recType)` single-flight guard. Coalesce or cancel superseded jobs.

---

### Finding #29 — Effective-rates cache unbounded

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Category** | Performance / memory |
| **Location** | `internal/costdata/provider.go` (`sync.Map`, 5 min TTL) |
| **Effort** | S |

**Description**

The effective-rates cache grows with distinct org×cluster pairs. Entries expire by TTL but there is no max size; long-lived pods serving many tenants exhibit slow memory growth.

**Exploit / trigger**

Multi-tenant SaaS-style deployment or frequent cluster onboarding on a single processor pod.

**Recommended fix**

Replace with LRU cache capped at configurable max entries. Export cache size metric.

---

### Finding #30 — No formal ADR index

| Field | Value |
|-------|-------|
| **Severity** | Info |
| **Category** | Governance |
| **Location** | `docs/` (no `docs/adr/` directory) |
| **Effort** | S |

**Description**

Architectural decisions (native engine migration, plugin system, Kruize deprecation) are discoverable only by archaeology across scattered docs and commit history.

**Exploit / trigger**

Not operational — onboarding friction and repeated design debates.

**Recommended fix**

Add `docs/adr/` with numbered Architecture Decision Records. Index key past decisions retroactively.

---

## What Held Up Well

Positive findings from the review — areas that demonstrate mature engineering and reduce compound risk.

| Area | Observation |
|------|-------------|
| **Plugin architecture** | Recommendation types are modular with explicit enable/disable; disabled routes return 404. Clean separation supports incremental hardening per plugin. |
| **Parameterized SQL** | List and detail queries use bound parameters via pgxpool/GORM; no string-concatenated user input in SQL text observed in reviewed paths. |
| **Kafka error classification framework** | `kafka_processing_errors.go` provides a structured transient vs. permanent taxonomy — foundation is sound; defaults need tuning (Finding #18). |
| **Service-account auth pattern** | Kubernetes TokenReview integration for internal endpoints is correctly implemented when allowlists are configured (Findings #15–#16 are config gaps, not design gaps). |
| **Structured metrics** | Ingestion and processing paths increment Prometheus counters/histograms, supporting operability once alerting rules are wired. |
| **Native engine test coverage** | Unit and integration tests exist for core recommendation math, term windows, and idle detection — reduces regression risk during remediation. |
| **API versioning documentation** | `docs/architecture/api-versioning.md` defines a coherent deprecation policy (execution gap: CHANGELOG missing, Finding #10). |
| **Multi-tenancy schema isolation** | Tenant data scoped by `org_id` in queries when handlers apply filters correctly; RBAC integration path exists for SaaS mode. |
| **Streaming CSV parser** | Ingestion pipeline streams file content rather than loading entire CSVs — memory issue is in grouping layer (Finding #8), not parse layer. |
| **Upgrade runbook** | `docs/upgrade-runbook.md` documents manual migration steps for large-table operations — honest operational documentation. |

---

## Cross-Cutting Failure Scenario Matrix

What happens when a dependency fails or is misconfigured. Rows are independent scenarios; cells describe user-visible and operational impact.

| Scenario | Ingestion | API reads | API writes | Analytics | Consumer offset | Operator signal |
|----------|-----------|-----------|------------|-----------|-----------------|-----------------|
| **PostgreSQL down** | Fails (transient) | 503 /readyz fails | 503 | Blocked | Not committed (retry) | `/readyz` fails; pod not ready |
| **Kafka down** | Stalled | Unaffected | Unaffected | Stale | N/A | Consumer lag alert (external) |
| **S3/MinIO down (one file in payload)** | **Tracked failure (#1+#2 mitigated)** | Stale recs (gated) | N/A | Stale | **Committed** | `ros_ingestion_file_failures_total` alert + `report_file_status.failed` |
| **S3/MinIO down (all files)** | Fails | Stale | N/A | Stale | Depends on error class | Log + metric |
| **Masu/Koku cost API down** | Recs without cost (degraded) | Savings $0 or cached | N/A | Partial | May commit (#9) | Cost provider errors in logs |
| **History DB write fails** | Recs written (#9) | Fresh recs, no history | N/A | **Gap** | Committed | No API staleness flag |
| **Identity gateway bypassed (#3)** | N/A | Cross-tenant access *(SNO/dev only)* | Unauthorized mutation *(SNO/dev only)* | N/A | N/A | No in-app signal |
| **`RBAC_ENABLE=false` (#5)** | N/A | All data (if identity trusted) | Any user changes thresholds *(SNO/dev only)* | Recalc storm | N/A | None |
| **Unclassified Kafka error (#18)** | **Infinite retry** | Stale | N/A | Stale | **Never committed** | Lag grows; partition stuck |
| **Large cluster + 25s timeout (#21)** | Timeout → transient loop | OK | OK | Stalled | Not committed | Repeated timeout logs |
| **OOM during ingest (#8)** | Pod killed mid-batch | 503 if same pod | 503 | Partial | **May commit partial (#1)** | OOMKilled event |
| **NetworkPolicy missing (#3, #4)** | N/A | Internal routes exposed *(SNO/dev)* | Tag enum cross-tenant *(db mode)* | N/A | N/A | None without network audit |

**Key takeaway:** The worst production outcomes cluster around **Kafka commit semantics** (silent loss vs. infinite stall). Auth findings (#3–#6) are largely mitigated in SaaS and default on-prem chart deployments by gateway JWT validation, RBAC defaults, and NetworkPolicy; they remain relevant only in SNO/dev overrides or as optional hardening targets. Dependency failure modes are otherwise reasonably observable except analytics degradation (Finding #9).

---

## Tracking

| Finding # | Title | Jira | Status | Target |
|-----------|-------|------|--------|--------|
| 1 | Kafka offset committed after partial file failure | TBD | Mitigated | per-file tracking + gating |
| 2 | Native ingestion errors swallowed (return nil) | TBD | Mitigated | error propagation + metrics |
| 3 | Identity header trusted without JWT verification | TBD | Accepted (architecture) | — |
| 4 | `/internal/tags/status` unauthenticated in on-prem | TBD | Open (db-mode hardening) | — |
| 5 | Settings mutation without RBAC (SNO/dev override) | TBD | Accepted (deployment-specific) | — |
| 6 | Internal SA can act on any org_id | TBD | Accepted (architecture) | — |
| 7 | Dual DB connection pools (GORM + pgxpool) | TBD | Open | — |
| 8 | Streaming ingest accumulates all groups in memory | TBD | Open | — |
| 9 | Pipeline writes recs when history/quality fails | TBD | Open | — |
| 10 | No CHANGELOG.md despite API versioning policy | TBD | Open | — |
| 11 | No rate limiting; recalc goroutines without dedup | TBD | Open | — |
| 12 | SSRF risk when CSV host allowlist unset | TBD | Open | — |
| 13 | ILIKE wildcard injection | TBD | Open | — |
| 14 | Unbounded offset (deep-pagination DoS) | TBD | Open | — |
| 15 | ROS_TAGS_DEV_TOKEN static bypass | TBD | Open | — |
| 16 | Empty SA allowlist permits any K8s SA | TBD | Open | — |
| 17 | Readiness probe only checks PostgreSQL | TBD | Open | — |
| 18 | Unclassified Kafka errors default to transient | TBD | Open | — |
| 19 | Housekeeper lacks graceful shutdown | TBD | Open | — |
| 20 | Poison message payload logged (PII risk) | TBD | Open | — |
| 21 | 25s statement_timeout kills large ingestion | TBD | Open | — |
| 22 | Node GPU endpoint paginates in memory | TBD | Open | — |
| 23 | panic() in boxplot/GPU YAML parse | TBD | Open | — |
| 24 | 134+ migrations with no CONCURRENTLY automation | TBD | Open | — |
| 25 | History endpoints lack filter cardinality limits | TBD | Open | — |
| 26 | Float64 in money formatting | TBD | Open | — |
| 27 | Deterministic recommendation IDs (info) | TBD | Open | — |
| 28 | Overlapping threshold recalc jobs | TBD | Open | — |
| 29 | Effective-rates cache unbounded | TBD | Open | — |
| 30 | No formal ADR index | TBD | Open | — |

---

*Document version: 1.2 — 2026-06-08. Mitigated findings #1 and #2 (reclassified Critical → High). Next review recommended after operator runbook for `reship_ros` recovery is documented.*
