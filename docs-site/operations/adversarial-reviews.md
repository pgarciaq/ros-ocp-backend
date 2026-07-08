# Adversarial Reviews

## What Are Adversarial Reviews?

Adversarial reviews are structured security and engineering audits conducted against
ros-ocp-backend using a "red team" mindset. Unlike standard code review, adversarial
reviews assume the reviewer has attacker-level access, seek compound failure chains
across subsystems, and explicitly validate fixes against all deployment postures.

The goal is not to find trivial bugs but to surface systemic risks: data integrity
failures under partial outages, authentication bypasses in non-standard deployments,
memory exhaustion under adversarial workloads, and governance gaps that compound over time.

### Methodology

Each review cycle combines four analysis techniques:

- **Static code review** — source-level inspection of handlers, middleware, SQL queries, and configuration parsing
- **Architecture analysis** — threat modeling (STRIDE-lite) across Kafka ingestion, API, and database layers
- **Operational failure-mode analysis** — what happens when services degrade, misconfigure, or restart under load
- **Cross-system integration checks** — NetworkPolicy gaps, environment variable wiring across Helm charts, cross-service authentication

Findings are classified by severity (High / Medium / Low / Info), tracked through
remediation, and verified at the code level before closure.

---

## Deployment Context

Findings are assessed against three distinct deployment postures. Many issues only
apply when gateway or RBAC compensating controls are absent.

| Posture | Authentication | RBAC | Risk Profile |
|---------|---------------|------|--------------|
| **SaaS** (console.redhat.com) | 3scale validates JWT upstream | Enabled | Lowest — gateway + platform controls |
| **On-prem chart** (default) | Envoy gateway validates JWT via JWKS | Enabled | Low — NetworkPolicy + chart defaults |
| **SNO/dev overrides** | No gateway; direct API access | Disabled | Highest — no compensating controls |

The review scope targets the highest-risk posture (SNO/dev) to identify the worst-case
attack surface. Findings that are eliminated by standard deployment controls are
noted as such.

---

## Review Cycles

| Review | Date | Scope | Findings | Status |
|--------|------|-------|----------|--------|
| v1.0 | 2026-06-10 | Full engine + API + ingestion + auth | 31 | All closed |
| v2.0 | 2026-06-11 | Internal auth, tag wiring, GPU memory, governance | 29 | All closed |
| v3.0 | 2026-06-11 | Regression assessment, cache interactions, coalescing | 16 | All closed |
| v4.0 | 2026-06-11 | Post-completion validation, debouncer lifecycle | 9 | All closed |
| v5.0 | 2026-06-11 | Cumulative integration validation | 0 new | Clean |

---

## Executive Scorecard

| Area | Rating | Summary |
|------|--------|---------|
| **Data integrity** | A | Per-file tracking, strict analytics mode, error propagation, manifest ID synthesis |
| **Authentication** | A− | Gateway-delegated; internal endpoints SA-authenticated with bearer validation |
| **Authorization** | A | RBAC enabled by default; cluster/node scoping; entitlement middleware |
| **API security** | A | SSRF allowlist + IPv6 deny, ILIKE escape, offset cap, statement timeout, CORS lockdown |
| **Database & connections** | A | Unified pgxpool, bounded caches, pool metrics, statement timeouts |
| **Memory & performance** | A− | Streaming ingest, SQL pagination, bounded batches, fleet/savings caches |
| **Operational resilience** | A | Graceful shutdown, DLQ, readiness probes, debouncer lifecycle |
| **Engineering governance** | A | 163 ADRs, CHANGELOG, migration lint CI, govulncheck, OpenAPI advisory CI |

---

## All Findings by Severity

### High Severity (8 findings)

#### #1 — Kafka offset committed after partial file failure

| | |
|---|---|
| **Category** | Data integrity / Kafka consumer semantics |
| **Status** | **Mitigated** |

The report processing loop commits Kafka offsets after iterating files in a multi-file payload. When a single file fails permanently, the offset is still committed (by design, to avoid blocking the consumer group), previously causing silent data loss with no recovery path.

**Resolution:** Per-file `report_file_status` tracking table records state per `(manifest_id, filename)`. Failed files are explicitly recorded; recommendation engines are gated until all expected files complete. A Prometheus counter (`ros_ingestion_file_failures_total`) enables operator alerting. Recovery uses Koku's `reship_ros` API.

---

#### #2 — Native ingestion errors swallowed

| | |
|---|---|
| **Category** | Data integrity / error propagation |
| **Status** | **Mitigated** |

Non-transient ingestion errors (S3 403, fetch failures, parse errors) were logged but functions returned success. The caller never learned the file had failed, compounding Finding #1.

**Resolution:** Native ingest functions now return wrapped errors for permanent failures. The processor classifies errors as transient or permanent, records permanent failures in the tracking table, and surfaces them via structured logging and metrics.

---

#### #7 — Dual database connection pools

| | |
|---|---|
| **Category** | Performance / reliability |
| **Status** | **Mitigated** |

Two independent database connection pools existed (GORM and pgxpool) without coordinated limits. Under load, combined usage could exceed PostgreSQL `max_connections`.

**Resolution:** GORM now wraps the shared pgxpool via `stdlib.OpenDBFromPool`. A single pool governs all connections, with Prometheus pool metrics exported on scrape.

---

#### #8 — Streaming ingest accumulates all groups in memory

| | |
|---|---|
| **Category** | Performance / availability |
| **Status** | **Mitigated** |

Despite streaming CSV parsing, all container-day digest groups were held in memory until EOF. Large clusters (10k+ containers × 14 days) could trigger OOMKill mid-ingestion.

**Resolution:** Streaming ingest now flushes digest groups incrementally at a configurable batch size (default 5000 groups). Each flush runs in its own transaction. Prometheus gauges track in-memory group count and flush operations.

---

#### #9 — Pipeline writes recommendations on analytics failure

| | |
|---|---|
| **Category** | Data consistency |
| **Status** | **Mitigated** |

Recommendations were persisted even when history or quality metric writes failed, serving fresh recommendations without corresponding analytics data.

**Resolution:** Strict analytics mode (default enabled) aborts the batch on analytics write failure — no offset commit, message retried. A degraded-mode option exists with cluster-level flags and Prometheus counters for operator visibility.

---

#### #10 — No CHANGELOG despite API versioning policy

| | |
|---|---|
| **Category** | Governance / API contract |
| **Status** | **Resolved** |

The project had an API versioning policy document but no CHANGELOG tracking actual changes.

**Resolution:** `CHANGELOG.md` added with Keep a Changelog format. Advisory CI workflow warns when API-affecting PRs lack changelog entries.

---

#### #32 — Empty manifest_id bypasses per-file tracking

| | |
|---|---|
| **Category** | Correctness / data integrity |
| **Status** | **Resolved** |

When Kafka messages omit `manifest_id`, all per-file tracking functions no-op and recommendation gating returns immediately — the exact scenario Findings #1/#2 aimed to fix.

**Resolution:** The processor synthesizes a deterministic manifest ID (UUID v5 over org, cluster, and scope key) when the field is empty. Per-file tracking, failure recording, and recommendation gating all use the resolved ID. Operators see a warning log and Prometheus counter when synthesis occurs.

---

#### #33 — Legacy Kruize path lacks per-file tracking

| | |
|---|---|
| **Category** | Correctness / data integrity |
| **Status** | **Accepted** |

The deprecated Kruize plugin path does not use `report_file_status` tracking. Parse and experiment errors continue without permanent classification.

**Rationale:** The Kruize plugin is slated for removal (documented in ADR-0163). No enhancements will be made to legacy paths.

---

### Medium Severity (33 findings)

#### #4 — Internal tags endpoint unauthenticated

| | |
|---|---|
| **Category** | Authorization / multi-tenancy |
| **Status** | **Resolved** (see #37) |

In on-prem `db` mode, bearer authentication was skipped for internal tag endpoints. Any pod on the cluster network could enumerate tags for arbitrary tenants.

**Resolution:** Bearer auth via service-account TokenReview is now required on all internal routes by default, regardless of tag source mode.

---

#### #11 — Recalculation storms from unbounded goroutines

| | |
|---|---|
| **Category** | Availability |
| **Status** | **Mitigated** |

Settings changes triggered unbounded parallel recalculation goroutines without deduplication, causing CPU/memory spikes and connection exhaustion.

**Resolution:** Per-org single-flight coalescing guard ensures at most one in-flight recalculation per type, with one pending follow-up that uses the latest parameters.

---

#### #12 — SSRF risk when CSV host allowlist unset

| | |
|---|---|
| **Category** | Security / SSRF |
| **Status** | **Mitigated** |

When the CSV host allowlist was empty, any URL in a Kafka message could be fetched by the processor, enabling internal network scanning.

**Resolution:** Non-development mode fails at startup if the allowlist is empty. Private-network deny (RFC1918, link-local, loopback) blocks fetches regardless of allowlist. DNS hostnames are resolved before fetch.

---

#### #13 — ILIKE wildcard injection

| | |
|---|---|
| **Category** | Security / query manipulation |
| **Status** | **Mitigated** |

Filter values containing `%` and `_` were passed directly to ILIKE clauses, enabling query manipulation and performance DoS.

**Resolution:** An `escapeILIKE()` utility escapes special characters in all user-facing ILIKE filters with proper escape clause syntax.

---

#### #14 — Unbounded offset pagination DoS

| | |
|---|---|
| **Category** | Availability / DoS |
| **Status** | **Mitigated** |

Requests with extremely large offsets (`offset=999999999`) forced full sequential scans, enabling trivial DoS.

**Resolution:** Offset cap (default 10,000) returns HTTP 400 with guidance directing callers to keyset pagination.

---

#### #15 — Development token accepted in production

| | |
|---|---|
| **Category** | Authentication |
| **Status** | **Mitigated** |

A static development token could bypass authentication if the development flag leaked to production deployments.

**Resolution:** Startup fails if the dev token is configured and `DEVELOPMENT` is not explicitly `true`.

---

#### #16 — Empty service account allowlist permits all callers

| | |
|---|---|
| **Category** | Authentication |
| **Status** | **Mitigated** |

An empty SA allowlist accepted any Kubernetes service account token, enabling cross-tenant invocation of internal endpoints.

**Resolution:** Startup fails when the allowlist is empty in production mode. Runtime default-deny for empty allowlist outside development.

---

#### #17 — Readiness probe only checks PostgreSQL

| | |
|---|---|
| **Category** | Operational readiness |
| **Status** | **Mitigated** |

The readiness probe only verified PostgreSQL connectivity, masking Kafka or S3 failures from the orchestrator.

**Resolution:** Opt-in deep checks for Kafka and S3 via configuration. Failures return HTTP 503 with per-dependency JSON diagnostics. API-only pods retain the shallow probe by design.

---

#### #18 — Unclassified Kafka errors cause consumer stall

| | |
|---|---|
| **Category** | Kafka consumer semantics |
| **Status** | **Mitigated** |

Unknown error types defaulted to "transient," preventing offset commit. Without a retry budget, the consumer could redeliver the same message indefinitely.

**Resolution:** Retry-count tracking via Kafka headers with configurable maximum (default 5). After exhausting retries, messages route to a dead-letter queue with forensic metadata. Prometheus metrics enable alerting on retry storms and DLQ volume.

---

#### #19 — Housekeeper lacks graceful shutdown

| | |
|---|---|
| **Category** | Operational resilience |
| **Status** | **Mitigated** |

The housekeeper process did not handle SIGTERM, potentially leaving cleanup operations in an inconsistent state.

**Resolution:** Signal notification context wired through the housekeeper lifecycle. In-flight operations respect cancellation with a configurable grace period (default 30s).

---

#### #20 — Poison message payload logged (PII risk)

| | |
|---|---|
| **Category** | Privacy / logging |
| **Status** | **Mitigated** |

Failed Kafka messages had their full payload logged, potentially exposing workload metadata in centralized log aggregation.

**Resolution:** Poison message logging now emits metadata only (request ID, org, cluster, error class, payload size). Full payload preview is opt-in via configuration.

---

#### #21 — Statement timeout kills large ingestion

| | |
|---|---|
| **Category** | Data integrity / performance |
| **Status** | **Mitigated** |

A global 25-second statement timeout applied to all connections. Large ingestion batch inserts exceeded this limit, causing infinite retry loops.

**Resolution:** API connections keep the standard timeout. Ingestion transactions set a per-transaction override (default 120s) that resets automatically on completion.

---

#### #22 — Node/GPU endpoint paginates in memory

| | |
|---|---|
| **Category** | Performance / scalability |
| **Status** | **Mitigated** |

GPU and node endpoints loaded entire result sets into memory before pagination, risking OOM on large GPU fleets.

**Resolution:** SQL-backed pagination for GPU MIG, time-slicing, and node endpoints. Only the current page is loaded and enriched. Hard cap on results prevents unbounded memory.

---

#### #23 — panic() in boxplot/GPU YAML parse

| | |
|---|---|
| **Category** | Reliability |
| **Status** | **Mitigated** |

Parsing functions called `panic()` on invalid input, crashing the entire process instead of returning an error.

**Resolution:** Functions return `(result, error)` instead of panicking. `log.Fatal` is reserved for corrupt compile-time embedded data only.

---

#### #24 — Migrations lack CONCURRENTLY automation

| | |
|---|---|
| **Category** | Operations / deploy safety |
| **Status** | **Mitigated** |

140+ migration files with no automation to detect non-concurrent index creation on large tables, risking production locks.

**Resolution:** CI lint script flags new non-concurrent indexes on configured large tables. Runbook and Kubernetes Job template provided for safe large-table migrations.

---

#### #31 — Pagination filter bypass

| | |
|---|---|
| **Category** | Correctness / filter bypass |
| **Status** | **Resolved** |

Keyset pagination for container lists did not include `workload_type` in the page join keys. Filtering by one workload type could return rows for other types on subsequent pages.

**Resolution:** `workload_type` added to DISTINCT ON, page join keys, cursor tie-breakers, and detail query re-filter. Regression test added.

---

#### #34 — SSRF allowlist bypass on DNS failure

| | |
|---|---|
| **Category** | Security |
| **Status** | **Resolved** |

The SSRF deny function allowed the fetch when DNS resolution failed, enabling attackers controlling DNS intermittently to redirect to private IPs.

**Resolution:** DNS lookup failures now return an error in non-development mode (fail closed).

---

#### #35 — No cost-management entitlement validation

| | |
|---|---|
| **Category** | Security / authorization |
| **Status** | **Resolved** |

Identity middleware decoded the identity header but never checked `entitlements.cost_management.is_entitled`. Unentitled accounts could access optimization data.

**Resolution:** Entitlement middleware on the v1 API group rejects requests without valid cost-management entitlement. Skipped in development mode.

---

#### #36 — No rate limit on async internal triggers

| | |
|---|---|
| **Category** | Operational robustness / availability |
| **Status** | **Resolved** |

Expensive internal endpoints (savings recalculation, fleet reship) spawned unbounded async work without deduplication per org.

**Resolution:** Per-org single-flight coalescing for savings recalculation and business-hours reship, mirroring the threshold recalc guard pattern. Prometheus metrics track coalesced triggers.

---

#### #37 — Internal tags endpoints unauthenticated in db mode

| | |
|---|---|
| **Category** | Security / multi-tenancy |
| **Status** | **Resolved** |

Bearer auth was skipped when tags used direct database joins. Any caller on the pod network could enumerate tag catalogs for arbitrary tenants.

**Resolution:** TokenReview bearer auth enforced on all internal tag routes by default. Configurable opt-out for local development without SA tokens.

---

#### #43 — Plugin ingest hook failures silently non-fatal

| | |
|---|---|
| **Category** | Correctness |
| **Status** | **Resolved** |

Plugin hook errors were collected but ingestion was considered successful. Downstream recommendations proceeded with incomplete derived data.

**Resolution:** Hook failures set a cluster-level flag exposed via API. Prometheus metric and runbook alerting guidance added.

---

#### #44 — Kruize fetch errors misclassified as transient

| | |
|---|---|
| **Category** | Correctness / Kafka semantics |
| **Status** | **Accepted** |

The legacy Kruize path classifies HTTP 403/404 fetch failures as transient, retrying until DLQ instead of immediate permanent classification.

**Rationale:** Kruize plugin is slated for removal (ADR-0163). No legacy path changes.

---

#### #45 — Strict analytics mode disabled by default

| | |
|---|---|
| **Category** | Correctness / data consistency |
| **Status** | **Resolved** |

Strict analytics mode defaulted to `false`, allowing recommendations to persist without history/quality parity unless operators explicitly opted in.

**Resolution:** Default changed to `true`. Degraded mode remains available as explicit opt-in with documented trade-offs.

---

#### #46 — Business-hours toggle triggers fleet-wide reship

| | |
|---|---|
| **Category** | Operational robustness |
| **Status** | **Resolved** |

Enabling business hours at org scope fired async re-ingestion across all clusters with no confirmation or idempotency window.

**Resolution:** Single-flight coalescing per org with warning log showing cluster count. API response includes warning when reship is triggered.

---

#### #48 — GPU MIG list loads entire fleet into memory

| | |
|---|---|
| **Category** | Performance |
| **Status** | **Resolved** |

The GPU MIG handler loaded recommendations from all clusters into a slice before pagination in Go. A large GPU tenant could OOM the API pod.

**Resolution:** SQL-backed count and page queries with per-page enrichment. Unsupported sort keys return HTTP 400.

---

#### #49 — GPU time-slicing fallback paginates in memory

| | |
|---|---|
| **Category** | Performance |
| **Status** | **Resolved** |

A fallback path for unsupported sort keys loaded all recommendations from all clusters before in-memory sort and slice.

**Resolution:** In-memory fallback removed entirely. SQL pagination used for all formats. Unsupported sort keys return HTTP 400.

---

#### #55 — Kruize endpoints share global HTTP timeout

| | |
|---|---|
| **Category** | Performance / operational robustness |
| **Status** | **Accepted** |

Kruize bulk upload endpoints used a shared 30-second HTTP client timeout insufficient for large payloads.

**Rationale:** Kruize plugin is slated for removal (ADR-0163). No legacy path changes.

---

#### #56 — CSV download max body too large

| | |
|---|---|
| **Category** | Performance / availability |
| **Status** | **Resolved** |

The default maximum CSV body size was 512 MiB. Multi-file payloads with large CSVs could exhaust processor memory despite streaming parse.

**Resolution:** Default lowered to 100 MiB. Documented in configuration reference.

---

#### #61 — Synthesized manifest triggers premature recommendations

| | |
|---|---|
| **Category** | Correctness / data integrity |
| **Status** | **Resolved** |

When multiple Kafka messages arrive on the same day for the same cluster (sharing a synthesized manifest ID), the first message to complete triggers recommendations before later messages arrive.

**Resolution:** Synthesized manifest IDs defer recommendation engines until a configurable quiet period (default 30s) expires with no new file registrations. Each file registration resets the timer.

---

#### #62 — Single-flight coalescing replays stale parameters

| | |
|---|---|
| **Category** | Correctness / operational robustness |
| **Status** | **Resolved** |

Coalescing guards re-executed trailing iterations with the original parameters from the first trigger, not the latest caller's parameters.

**Resolution:** Flight structs store the latest caller parameters atomically under mutex. Trailing iterations read latest values before execution.

---

#### #63 — Internal endpoints accept arbitrary org_id

| | |
|---|---|
| **Category** | Security / multi-tenancy |
| **Status** | **Resolved (mitigated)** |

Authenticated internal callers can supply any `org_id` without tenant binding. TokenReview validates SA identity but does not map to permitted orgs.

**Resolution:** Cross-tenant internal calls are intentional for platform services. Defense-in-depth: structured audit logging, Prometheus metrics per endpoint/org/SA, optional org allowlist restriction, and documented design decision.

---

#### #64 — IPv6 addresses bypass private-network SSRF deny

| | |
|---|---|
| **Category** | Security |
| **Status** | **Resolved** |

The private-network check only handled IPv4, allowing IPv6 ULA and link-local addresses to bypass the SSRF deny.

**Resolution:** IP restriction uses Go's `IsPrivate()`, `IsLoopback()`, `IsLinkLocalUnicast()`, and `IsLinkLocalMulticast()` for both IPv4 and IPv6.

---

#### #78 — Async recalc invalidates cache at start, not completion

| | |
|---|---|
| **Category** | Correctness |
| **Status** | **Resolved** |

Cache invalidation ran before launching background recalculation. During the recalc window, concurrent reads could repopulate caches with stale data.

**Resolution:** Post-completion invalidation added in coalesced guard loops. Pre-trigger invalidation retained for pessimistic double-invalidation pattern.

---

### Low Severity (23 findings)

#### #5 — Settings mutation without RBAC

| | |
|---|---|
| **Category** | Authorization / integrity |
| **Status** | **Accepted** |

When RBAC is disabled, any authenticated user can mutate optimization thresholds. Default chart sets RBAC enabled.

**Rationale:** Accepted for SNO/dev posture. Production deployments have RBAC enabled by default.

---

#### #25 — History endpoints lack filter cardinality limits

| | |
|---|---|
| **Category** | Availability |
| **Status** | **Mitigated** |

History endpoints accepted unbounded filter values without cardinality checks, enabling expensive queries.

**Resolution:** Cardinality check applied to all filter parameters matching the pattern used by main list handlers.

---

#### #26 — Float64 in money formatting

| | |
|---|---|
| **Category** | Correctness |
| **Status** | **Mitigated** |

Integer cents were divided into float64 for display, risking rounding errors on large values.

**Resolution:** Formatting uses integer division and remainder with explicit negative handling. Unit tests cover large cent values that would exhibit float64 rounding.

---

#### #28 — Overlapping threshold recalculation jobs

| | |
|---|---|
| **Category** | Correctness / availability |
| **Status** | **Mitigated** |

Multiple threshold recalculation jobs could overlap, causing redundant work and resource contention.

**Resolution:** Same single-flight coalescing guard as Finding #11.

---

#### #29 — Effective-rates cache unbounded

| | |
|---|---|
| **Category** | Performance / memory |
| **Status** | **Mitigated** |

The cost rates cache grew unbounded with distinct org×cluster pairs, risking OOM under high tenant cardinality.

**Resolution:** Replaced with bounded LRU cache (configurable max entries, default 1000). TTL-on-access preserved. Cache size and eviction metrics exported.

---

#### #38 — Kafka consumer debug logs expose payload prefix

| | |
|---|---|
| **Category** | Security / privacy |
| **Status** | **Resolved** |

At DEBUG level, the consumer logged the first 512 bytes of every message including presigned URLs and cluster metadata.

**Resolution:** Removed payload prefix logging. Message metadata (topic, partition, offset, length) retained. Poison message bodies remain opt-in only.

---

#### #40 — RBAC permission cache unbounded

| | |
|---|---|
| **Category** | Performance / availability |
| **Status** | **Resolved** |

RBAC responses cached in a map with TTL expiry but no maximum size, growing unbounded under high user cardinality.

**Resolution:** Replaced with bounded LRU cache (configurable max entries, default 500). Cache size and eviction metrics exported.

---

#### #42 — CORS middleware allows all origins

| | |
|---|---|
| **Category** | Security |
| **Status** | **Resolved** |

Empty CORS origin configuration resulted in `Access-Control-Allow-Origin: *`, violating least-privilege.

**Resolution:** Explicit origins configured via environment. Production denies cross-origin when unset. Development mode allows all origins.

---

#### #47 — Background async jobs ignore shutdown context

| | |
|---|---|
| **Category** | Operational robustness |
| **Status** | **Resolved** |

Recalculation and reship goroutines used `context.Background()` detached from API server shutdown, causing connection errors during pod termination.

**Resolution:** Shutdown-aware context package wired from API server. All async jobs propagate cancellation. Warns if jobs exceed grace period.

---

#### #50 — History CSV export capped at paginated limit

| | |
|---|---|
| **Category** | Correctness / UX |
| **Status** | **Resolved** |

History CSV export used the standard paginated limit (100 rows) instead of the CSV-specific limit (1000 rows), truncating exports.

**Resolution:** History handler applies the CSV record limit for CSV format responses. Documented in OpenAPI.

---

#### #51 — History default date window too wide

| | |
|---|---|
| **Category** | Performance |
| **Status** | **Resolved** |

Default history queries scanned from first-of-month without requiring scoping filters, causing expensive count queries on large orgs.

**Resolution:** Default window limited to a configurable number of recent days (default 30). Documented in OpenAPI.

---

#### #52 — Fleet summary uncached aggregation

| | |
|---|---|
| **Category** | Performance |
| **Status** | **Resolved** |

Fleet summary executed full-org aggregation on every request with only HTTP-level caching.

**Resolution:** In-memory LRU cache keyed by org and RBAC scope. TTL-based expiry with invalidation on recommendation ingest and savings recalculation.

---

#### #57 — Parallel Kafka workers share consumer for commits

| | |
|---|---|
| **Category** | Correctness |
| **Status** | **Resolved** |

Parallel Kafka workers called `CommitMessage` on the same consumer without synchronization. librdkafka is not documented as thread-safe for concurrent commits.

**Resolution:** Commits serialized via mutex when parallel mode is enabled.

---

#### #58 — Report file status operator runbook absent

| | |
|---|---|
| **Category** | Auditability / operational robustness |
| **Status** | **Resolved** |

No runbook documented how to query stuck manifests, classify failures, or trigger recovery.

**Resolution:** Runbook section added with SQL queries, synthesized manifest ID guidance, reship procedure, and alert thresholds.

---

#### #59 — Recommendation detail fallback path retained

| | |
|---|---|
| **Category** | Maintainability |
| **Status** | **Resolved** |

A legacy fallback query path increased the test matrix and audit surface for the recommendation detail endpoint.

**Resolution:** Fallback path removed. All lookups use the indexed primary path. Missing rows return 404.

---

#### #65 — Fleet cache misses retention and cleanup mutations

| | |
|---|---|
| **Category** | Correctness |
| **Status** | **Resolved** |

Fleet summary cache did not invalidate on retention sweeps or Sources destroy cleanup, briefly showing inflated counts.

**Resolution:** Cache invalidation wired into retention purge (using per-org RETURNING clause) and Sources cleanup paths.

---

#### #66 — Fleet cache lacks metrics and configurable capacity

| | |
|---|---|
| **Category** | Operational robustness |
| **Status** | **Resolved** |

Fleet summary cache had hardcoded capacity with no Prometheus metrics, inconsistent with other LRU caches.

**Resolution:** Configurable max entries, plus size/evictions/invalidation metrics matching the pattern of RBAC and cost caches.

---

#### #67 — Startup validation omits internal auth and CORS

| | |
|---|---|
| **Category** | Security / governance |
| **Status** | **Resolved** |

Security startup validation did not check for disabled internal tags auth or empty CORS origins in production.

**Resolution:** Extended validation to warn on unsafe production configurations. Warnings are non-fatal by design for operator flexibility.

---

#### #68 — Savings-summary endpoint uncached

| | |
|---|---|
| **Category** | Performance |
| **Status** | **Resolved** |

Fleet summary was cached but savings-summary (a heavier query used by the dashboard) was not.

**Resolution:** Savings-summary cache added with same LRU+TTL pattern. Correctly bypassed for group-by variants that cannot be cached.

---

#### #69 — Fleet cache LRU order slice leaks on lazy expiry

| | |
|---|---|
| **Category** | Performance |
| **Status** | **Resolved** |

Expired entries were removed from the cache map but not from the order tracking, causing the order slice to grow over time.

**Resolution:** Expired keys properly removed from both map and order structures.

---

#### #77 — Retention and Sources cleanup omit cache invalidation

| | |
|---|---|
| **Category** | Correctness |
| **Status** | **Resolved** |

Cache invalidation was wired for recalc and settings changes but not for retention purges or cluster removal.

**Resolution:** Per-org invalidation added after retention deletes and Sources cleanup completion.

---

#### #79 — Manifest debouncer lacks shutdown integration

| | |
|---|---|
| **Category** | Operational robustness |
| **Status** | **Resolved** |

The synthesized-manifest quiet-period debouncer used `context.Background()` with no shutdown hook, allowing timers to fire after DB pool draining begins.

**Resolution:** Debouncer initialized with cancellable context, registered in shutdown path. Deferred runs use cancellable context from the Kafka handler.

---

#### #80 — Debouncer timer Stop() race can double-fire

| | |
|---|---|
| **Category** | Correctness |
| **Status** | **Resolved** |

Per Go semantics, `timer.Stop()` can return false if the timer already expired, allowing both the old callback and a new one to fire simultaneously.

**Resolution:** Generation counter in the timer scheduler. Superseded callbacks exit early even when `Stop()` returns false.

---

### Informational (21 findings)

#### #3 — Identity header trusted without JWT verification

| | |
|---|---|
| **Category** | Authentication |
| **Status** | **Accepted** |

The middleware decodes and trusts `X-Rh-Identity` without verifying JWT signatures. By design, the upstream gateway validates JWT and injects the trusted header.

**Rationale:** Standard Red Hat platform architecture. Not a required fix when gateway and NetworkPolicy are correctly deployed.

---

#### #6 — Internal service account can act on any org_id

| | |
|---|---|
| **Category** | Authorization / multi-tenancy |
| **Status** | **Accepted** |

Bearer token authentication validates the caller is a known service account but does not bind `org_id` to the caller's identity.

**Rationale:** By design for cross-tenant platform services (Koku/Masu acting on behalf of tenants).

---

#### #27 — Deterministic recommendation IDs

| | |
|---|---|
| **Category** | Security awareness |
| **Status** | **Verified** |

Recommendation IDs are UUID v5 derived from container identity. Acceptable when org boundary is enforced on all detail endpoints.

**Resolution:** Audited all detail endpoints — each filters by org_id. Security invariant documented and regression test added.

---

#### #30 — No formal ADR index

| | |
|---|---|
| **Category** | Governance |
| **Status** | **Resolved** |

Architectural decisions were discoverable only by archaeology across scattered docs and commit history.

**Resolution:** 163 numbered Architecture Decision Records added with index, grouped by domain.

---

#### #39 — Kruize API debug logs include full payloads

| | |
|---|---|
| **Category** | Security / privacy |
| **Status** | **Accepted** |

Legacy Kruize API calls log complete JSON payloads at DEBUG level including container names and resource metrics.

**Rationale:** Kruize plugin is slated for removal (ADR-0163). No legacy path changes.

---

#### #41 — Prometheus /metrics exposed without authentication

| | |
|---|---|
| **Category** | Security |
| **Status** | **Accepted** |

The metrics endpoint serves without auth. Labels include org_id on several counters.

**Rationale:** NetworkPolicy restricts scrape to Prometheus in production deployments. Acceptable information disclosure within the monitoring boundary.

---

#### #53 — No OpenAPI/CHANGELOG CI enforcement

| | |
|---|---|
| **Category** | Governance |
| **Status** | **Resolved** |

No workflow validated that API-affecting changes updated the OpenAPI spec or CHANGELOG.

**Resolution:** Advisory CI workflow compares API-affecting file changes against OpenAPI and CHANGELOG updates, emitting warnings on PRs.

---

#### #54 — ADR drift detection absent

| | |
|---|---|
| **Category** | Governance / maintainability |
| **Status** | **Resolved** |

No CI verified that code changes contradicting accepted ADRs triggered review.

**Resolution:** Advisory workflow triggers on architectural file changes, reminding PR authors to review or create ADRs.

---

#### #60 — aws-sdk-go v1 dependency

| | |
|---|---|
| **Category** | Governance / security |
| **Status** | **Resolved** |

AWS SDK v1 (maintenance mode) appeared as a direct dependency with no vulnerability scanning in CI.

**Resolution:** `govulncheck` CI workflow added. The direct dependency was a phantom (transitively required only); removed via `go mod tidy` after migrating to aws-sdk-go-v2.

---

#### #70 — OpenAPI spec omits entitlement requirement

| | |
|---|---|
| **Category** | Governance |
| **Status** | **Resolved** |

Entitlement middleware enforces access but the OpenAPI spec did not document this requirement.

**Resolution:** Reusable `ForbiddenEntitlement` response component added and referenced from all v1 paths. Info description updated with entitlement requirement.

---

#### #71 — CI governance workflows advisory-only with path drift

| | |
|---|---|
| **Category** | Governance |
| **Status** | **Resolved** |

Governance workflows used `continue-on-error: true` and path filters drifted from documentation files.

**Resolution:** Path filter files updated with broader globs and maintenance comments. Workflow path filters synced.

---

#### #72 — govulncheck uses unpinned version

| | |
|---|---|
| **Category** | Governance |
| **Status** | **Resolved** |

CI installed `govulncheck@latest` on each run, causing non-reproducible results.

**Resolution:** Version pinned in workflow with reproducibility comment.

---

#### #73 — Reference routes bypass identity/entitlement

| | |
|---|---|
| **Category** | Security |
| **Status** | **Accepted** |

Notification codes catalog and OpenAPI spec endpoints register before auth middleware — no authentication required.

**Rationale:** Static reference data acceptable for UI bootstrap. Gateway controls restrict external exposure.

---

#### #74 — No ADR cross-references in Go source

| | |
|---|---|
| **Category** | Maintainability |
| **Status** | **Resolved** |

163 ADRs existed but no Go source file referenced them, allowing drift without awareness.

**Resolution:** `// ADR-NNNN` comments added at key architectural decision points across database, plugin, Kafka, config, middleware, reship, and async job packages.

---

#### #75 — SSRF DNS validation TOCTOU residual

| | |
|---|---|
| **Category** | Security |
| **Status** | **Accepted** |

DNS rebinding between validation time and connection time could theoretically bypass SSRF deny.

**Rationale:** Practical risk is minimal with controlled allowlists and fail-closed DNS. Full mitigation (pinned dial) is available for high-threat deployments.

---

#### #76 — Fleet cache timing side channel

| | |
|---|---|
| **Category** | Security |
| **Status** | **Accepted** |

Cache hit vs miss latency reveals recent ingest/recalc timing to authenticated callers.

**Rationale:** Caller is already entitled and org-scoped. No cross-tenant information leakage. Constant-time padding not warranted.

---

#### #81 — Savings cache observability asymmetry

| | |
|---|---|
| **Category** | Operational robustness |
| **Status** | **Resolved** |

Savings cache exposed only hit/miss metrics while fleet cache had full observability (size, evictions, invalidations).

**Resolution:** Full Prometheus metric suite added to savings cache mirroring fleet cache patterns.

---

#### #82 — CI path manifest files not consumed by workflows

| | |
|---|---|
| **Category** | Governance |
| **Status** | **Resolved** |

Path filter documentation files existed but workflows hardcoded their own globs, causing drift.

**Resolution:** Missing paths added and workflow filters synced with documentation files.

---

#### #83 — OpenAPI 403 responses inconsistently documented

| | |
|---|---|
| **Category** | Governance |
| **Status** | **Resolved** |

Some paths referenced the reusable `ForbiddenEntitlement` component while others used inline descriptions.

**Resolution:** Standardized across all applicable paths.

---

#### #84 — ADR cross-references missing on new modules

| | |
|---|---|
| **Category** | Maintainability |
| **Status** | **Resolved** |

Modules added during the v3.0 hardening sprint lacked the `// ADR-NNNN` comments added in Finding #74.

**Resolution:** ADR comments added referencing relevant decisions (manifest synthesis, fleet cache, coalescing pattern).

---

#### #85 — Production config misconfiguration remains warn-only

| | |
|---|---|
| **Category** | Security / governance |
| **Status** | **Accepted** |

Startup validation for internal auth and CORS configuration emits warnings but does not block startup in production.

**Rationale:** Accepted as operator-flexibility trade-off. NetworkPolicy and SA allowlist remain primary controls. Optional strict mode available for hardened deployments.

---

## What Held Up Well

Positive findings from the review — areas that demonstrate mature engineering practices
and reduce compound risk.

| Area | Observation |
|------|-------------|
| **Plugin architecture** | Recommendation types are modular with explicit enable/disable; disabled routes return 404. Clean separation supports incremental hardening per plugin. |
| **Parameterized SQL** | All list and detail queries use bound parameters via pgxpool/GORM. No string-concatenated user input in SQL text observed in reviewed paths. |
| **Kafka error classification** | Structured transient vs. permanent taxonomy with retry/DLQ escalation prevents infinite partition stall. |
| **Per-file ingestion tracking** | `report_file_status` with unit tests provides operational visibility for partial manifest failures. |
| **Manifest-gated recommendations** | Recommendation engines deferred until all expected files reach `done` — prevents stale/partial results from incomplete ingestion. |
| **Service-account auth pattern** | Kubernetes TokenReview integration for internal endpoints correctly implemented with allowlist validation. |
| **Structured metrics** | Ingestion and processing paths increment Prometheus counters/histograms enabling alerting on real failure modes. |
| **Native engine test coverage** | Unit and integration tests for core recommendation math, term windows, idle detection, and keyset pagination. |
| **Keyset pagination correctness** | Includes `workload_type` in DISTINCT ON and cursor tie-breakers; regression tested. |
| **API versioning documentation** | Coherent deprecation policy with CHANGELOG tracking releases. |
| **Multi-tenancy schema isolation** | Tenant data scoped by `org_id` in all handlers; RBAC integration for SaaS mode. |
| **Streaming CSV parser** | Pipeline streams file content rather than loading entire CSVs into memory. |
| **Single-flight coalescing** | Consistent pattern across savings, threshold, and reship guards prevents worst-case recalculation storms. |
| **Bounded LRU caches** | Cost, RBAC, fleet, and savings caches all use the same bounded LRU+TTL pattern with Prometheus metrics. |
| **Security startup validation** | `ValidateSecurityConfig` fails fast on unsafe production configurations. |
| **ADR corpus** | 163 Architecture Decision Records with index — exceptional for project maturity. |
| **Shutdown integration** | Graceful shutdown wired through API server, Kafka consumer, housekeeper, async jobs, and debouncer lifecycle. |
| **DLQ and recovery** | Dead-letter queue with forensic metadata headers; documented operator recovery procedures. |

---

## Summary Scorecard

| Metric | Value |
|--------|-------|
| **Total findings** | 85 |
| **Resolved** | 46 |
| **Mitigated** | 24 |
| **Accepted (with rationale)** | 12 |
| **Open** | **0** |
| **Review cycles** | 5 |
| **Critical/High findings** | 8 (all closed) |
| **Medium findings** | 33 (all closed) |
| **Low findings** | 23 (all closed) |
| **Informational findings** | 21 (all closed) |

All 85 findings across five review rounds are resolved, mitigated, or accepted with
documented rationale. Zero open remediation items remain. The accepted findings
relate to deprecated code paths slated for removal (Kruize plugin), intentional
platform architecture (cross-tenant service accounts, gateway-delegated auth), and
theoretical risks with minimal practical impact under standard deployment postures.
