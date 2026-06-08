# Adversarial Due Diligence Review — ros-ocp-backend

> **INTERNAL USE ONLY** — This document is an internal security and engineering audit. It is not for public disclosure, customer distribution, or external publication without explicit security and legal review.

**Date:** 2026-06-08  
**Scope:** `ros-ocp-backend` — Kafka ingestion pipeline, native recommendation engine, REST API, database layer, authentication/authorization, operational readiness, and engineering governance  
**Methodology:** Adversarial due diligence combining static code review, architecture analysis, threat modeling (STRIDE-lite), and operational failure-mode analysis. Reviewers assumed a production on-prem deployment (`ROS_TAGS_SOURCE=db`, `RBAC_ENABLE=false`) with network access to the API port unless otherwise noted. Findings were validated against source locations and cross-referenced for compound failure chains.

---

## Table of Contents

1. [Executive Scorecard](#executive-scorecard)
2. [Priority Remediation Order](#priority-remediation-order)
3. [Findings — Critical](#findings--critical)
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
| **Data integrity (Kafka ingestion)** | 🔴 Critical | Offset commits after partial file failure; native ingestion errors swallowed |
| **Authentication & authorization** | 🔴 High risk | Identity header unverified; internal endpoints under-protected on-prem |
| **API security** | 🟠 Medium–High | ILIKE injection, unbounded offset, SSRF when allowlist unset |
| **Database & connections** | 🟠 Medium | Dual connection pools; statement timeout conflicts with large ingestion |
| **Memory & performance** | 🟠 Medium | Streaming ingest holds full grouped map; node GPU endpoints paginate in memory |
| **Operational resilience** | 🟠 Medium | Readiness probe shallow; unclassified Kafka errors stall consumer; no graceful housekeeper shutdown |
| **Pipeline correctness** | 🟠 Medium | Recommendations persist when history/quality writes fail |
| **Engineering governance** | 🟡 Low–Medium | No CHANGELOG; no ADR index; 134+ migrations without CONCURRENTLY automation |
| **Positive controls** | 🟢 Strong | Plugin architecture, parameterized SQL, service-account auth patterns (when enabled), structured metrics |

**Overall assessment:** The native engine and API surface are functionally mature, but **two Critical data-integrity defects in the Kafka commit path** create silent, permanent data loss under routine operational failures. Security posture depends heavily on perimeter controls (gateway, network policy) rather than defense-in-depth within the service. Remediation of Critical and High findings should precede broad production hardening claims.

---

## Priority Remediation Order

Remediation is ordered by **compound risk** (findings that amplify each other) and **blast radius** (data loss > auth bypass > availability > governance).

| # | Finding(s) | Rationale |
|---|------------|-----------|
| 1 | **#1, #2** (Critical) | Kafka offset commit after partial failure + swallowed ingestion errors form a compound silent data-loss chain. Any fix to commit logic is ineffective until native processors propagate errors. |
| 2 | **#4, #5, #6** (High — auth) | On-prem defaults expose internal tag enumeration, unauthenticated settings mutation, and cross-tenant SA actions. Low effort, high impact. |
| 3 | **#3** (High — identity) | Defense-in-depth JWT validation or strict network policy; document as architecture requirement regardless. |
| 4 | **#18** (Medium — Kafka stall) | Unclassified errors cause infinite redelivery — consumer group makes no progress; pairs with #1 for opposite failure mode (stall vs. skip). |
| 5 | **#8, #21** (Medium — ingestion scale) | Memory accumulation and statement timeout both manifest under large-cluster ingestion; fix together to avoid OOM ↔ retry loops. |
| 6 | **#9** (High — pipeline degraded) | Fresh recommendations without analytics misleads operators and fleet metrics; add strict mode or staleness signaling. |
| 7 | **#7** (High — dual pools) | Connection exhaustion is silent until cascade failure; large effort but prevents production incidents under load. |
| 8 | **#11, #28** (Medium — recalc storms) | Unbounded goroutines and overlapping threshold jobs threaten availability after settings changes. |
| 9 | **#12, #13, #14** (Medium — API hardening) | SSRF, ILIKE wildcard injection, deep-pagination DoS — quick wins. |
| 10 | **#15, #16** (Medium — tag auth) | Dev token bypass and empty SA allowlist are configuration footguns in production. |
| 11 | **#17, #19, #20** (Medium — ops) | Readiness depth, graceful shutdown, PII in poison logs. |
| 12 | **#22, #23** (Medium — memory/panic) | Node GPU in-memory pagination; panic on parse failures. |
| 13 | **#24** (Medium — migrations) | CONCURRENTLY automation — plan for next large-table index. |
| 14 | **#10, #30** (High/Low — governance) | CHANGELOG and ADR index — process debt, not incident drivers. |
| 15 | **#25–#29** (Low/Info) | Cardinality limits, float formatting, cache bounds, deterministic IDs — address opportunistically. |

---

## Findings — Critical

### Finding #1 — Kafka offset committed after partial file failure

| Field | Value |
|-------|-------|
| **Severity** | Critical |
| **Category** | Data integrity / Kafka consumer semantics |
| **Location** | `internal/services/report_processor.go` (lines 483–495) |
| **Effort** | M |

**Description**

The report processing loop iterates over files in a multi-file Kafka payload. When a single file fails, the loop sets `reportProcessingFailed = true` but `continue`s to the next file. After the loop completes, the Kafka offset is committed unless `kafkaTransientErr` is set. Permanent file failures (S3 404, CSV parse errors, schema mismatches) therefore commit the offset without retrying the failed file.

**Exploit / trigger**

Not attacker-driven. Any transient or permanent S3/MinIO glitch, corrupt CSV, or missing object key on **one file** in a multi-file payload permanently drops that file's data. The consumer advances; the file is never redelivered. Operators see a successful consume with no alert for the skipped file.

**Recommended fix**

Do not commit the offset unless all files succeeded or were explicitly classified as poison (dead-letter). Treat `reportProcessingFailed` as a commit blocker (same as transient), or implement per-file idempotency with a dead-letter queue for poison messages. Add a metric `rosocp_ingestion_file_failed_total` labeled by failure class.

---

### Finding #2 — Native ingestion errors swallowed (return nil)

| Field | Value |
|-------|-------|
| **Severity** | Critical |
| **Category** | Data integrity / error propagation |
| **Location** | `internal/services/report_processor.go` — `processContainerCSVNative` and siblings (lines 510–513, 525–528) |
| **Effort** | S |

**Description**

Non-transient ingestion errors (S3 403, fetch failures, parse errors) are logged and metrics incremented, but the functions return `nil` (success). The caller's file-processing loop never learns the file failed, so `reportProcessingFailed` may remain false even when data was not ingested.

**Exploit / trigger**

Compounds Finding #1: even if commit logic were fixed to check `reportProcessingFailed`, this bug prevents the flag from being set. Any S3 permission error or fetch failure silently succeeds from the caller's perspective.

**Recommended fix**

Return wrapped errors for all ingestion failures unless explicitly classified as poison (e.g., unrecoverable schema violation after N retries). Let the outer loop set `reportProcessingFailed` and drive commit/retry semantics. Add unit tests asserting non-nil return on S3 403/404.

---

## Findings — High

### Finding #3 — Identity header trusted without JWT verification

| Field | Value |
|-------|-------|
| **Severity** | High |
| **Category** | Authentication |
| **Location** | `internal/api/middleware/identity.go` (lines 12–25) |
| **Effort** | M |

**Description**

The middleware base64-decodes the `X-Rh-Identity` header and trusts its contents without verifying JWT signatures, expiry, issuer, or entitlements. Any client that can reach the API port can impersonate any organization.

**Exploit / trigger**

Attacker with network access to the ROS API port crafts a base64 JSON identity with arbitrary `org_id` and admin flags. Full cross-tenant data access if no gateway validates tokens upstream.

**Mitigation (architecture)**

Production deployments must place an identity-validating gateway in front (3scale, oauth2-proxy, OpenShift route with JWT validation). Document as a hard architecture requirement.

**Recommended fix**

Defense-in-depth: validate JWT signature and claims when `RBAC_ENABLE` or a new `IDENTITY_VALIDATION_ENABLE` flag is set; alternatively enforce strict NetworkPolicy restricting API port to gateway pods only.

---

### Finding #4 — `/internal/tags/status` unauthenticated in on-prem (db mode)

| Field | Value |
|-------|-------|
| **Severity** | High |
| **Category** | Authorization / multi-tenancy |
| **Location** | `internal/api/handlers_tags_status.go` (lines 17–44), `internal/api/server.go` (lines 169–174) |
| **Effort** | S |

**Description**

When `ROS_TAGS_SOURCE=db` (on-prem default), bearer authentication is skipped for `/internal/tags/status`. The endpoint accepts an arbitrary `org_id` query parameter, enabling cross-tenant tag enumeration.

**Exploit / trigger**

Any pod or user on the cluster network calls `GET /internal/tags/status?org_id=<victim>` without credentials and receives tag sync status for other tenants.

**Recommended fix**

Always require service-account bearer auth on `/internal/*` routes regardless of tag source mode. Bind `org_id` to the authenticated caller's namespace or explicit SA allowlist.

---

### Finding #5 — Settings mutation without RBAC (on-prem default)

| Field | Value |
|-------|-------|
| **Severity** | High |
| **Category** | Authorization / integrity |
| **Location** | `internal/api/settings_rbac.go` (lines 14–16), `internal/config/config.go` (line 503) |
| **Effort** | S |

**Description**

When `RBAC_ENABLE=false` (on-prem default), any authenticated user can `PUT` optimization thresholds, triggering cluster-wide recalculation jobs. No `settings.write` permission check is enforced.

**Exploit / trigger**

Compromised or over-privileged user account changes thresholds for all recommendation types, causing incorrect recommendations fleet-wide and spawning expensive recalc goroutines (availability impact).

**Recommended fix**

Require `settings.write` permission even when RBAC is disabled, or document and enforce a single-admin deployment constraint with network-level access control. Fail startup in production if `RBAC_ENABLE=false` without explicit `ROS_ACCEPT_INSECURE_RBAC=true` acknowledgment.

---

### Finding #6 — Internal service account can act on any org_id

| Field | Value |
|-------|-------|
| **Severity** | High |
| **Category** | Authorization / multi-tenancy |
| **Location** | `internal/api/handlers_savings_recalculate.go` (lines 61–78), `internal/api/handlers_tags_sync.go` (lines 37–52) |
| **Effort** | M |

**Description**

Bearer token authentication validates the caller is a Kubernetes service account, but `org_id` in the request body is not bound to the caller's identity, namespace, or audience. Any allowed SA can trigger recalculation or tag sync for any organization.

**Exploit / trigger**

Compromised listener or masu pod (or any SA passing TokenReview) sends recalc/tag-sync requests targeting victim `org_id` values.

**Recommended fix**

Restrict allowed service accounts per operation type. Validate `org_id` against token namespace, audience, or a configured SA→org mapping. Reject mismatched org claims.

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
| **S3/MinIO down (one file in payload)** | **Silent skip (Critical #1+#2)** | Stale recs | N/A | Stale | **Committed** | Metric increment only; easy to miss |
| **S3/MinIO down (all files)** | Fails | Stale | N/A | Stale | Depends on error class | Log + metric |
| **Masu/Koku cost API down** | Recs without cost (degraded) | Savings $0 or cached | N/A | Partial | May commit (#9) | Cost provider errors in logs |
| **History DB write fails** | Recs written (#9) | Fresh recs, no history | N/A | **Gap** | Committed | No API staleness flag |
| **Identity gateway bypassed (#3)** | N/A | **Cross-tenant access** | **Unauthorized mutation** | N/A | N/A | No in-app signal |
| **`RBAC_ENABLE=false` (#5)** | N/A | All data (if identity trusted) | **Any user changes thresholds** | Recalc storm | N/A | None |
| **Unclassified Kafka error (#18)** | **Infinite retry** | Stale | N/A | Stale | **Never committed** | Lag grows; partition stuck |
| **Large cluster + 25s timeout (#21)** | Timeout → transient loop | OK | OK | Stalled | Not committed | Repeated timeout logs |
| **OOM during ingest (#8)** | Pod killed mid-batch | 503 if same pod | 503 | Partial | **May commit partial (#1)** | OOMKilled event |
| **NetworkPolicy missing (#3, #4)** | N/A | Internal routes exposed | Tag enum cross-tenant | N/A | N/A | None without network audit |

**Key takeaway:** The worst production outcomes cluster around **Kafka commit semantics** (silent loss vs. infinite stall) and **auth configuration on on-prem defaults** (trust perimeter, not service). Dependency failure modes are otherwise reasonably observable except analytics degradation (Finding #9).

---

## Tracking

| Finding # | Title | Jira | Status | Target |
|-----------|-------|------|--------|--------|
| 1 | Kafka offset committed after partial file failure | TBD | Open | — |
| 2 | Native ingestion errors swallowed (return nil) | TBD | Open | — |
| 3 | Identity header trusted without JWT verification | TBD | Open | — |
| 4 | `/internal/tags/status` unauthenticated in on-prem | TBD | Open | — |
| 5 | Settings mutation without RBAC (on-prem default) | TBD | Open | — |
| 6 | Internal SA can act on any org_id | TBD | Open | — |
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

*Document version: 1.0 — 2026-06-08. Next review recommended after Critical and High findings are remediated or explicitly accepted with compensating controls documented.*
