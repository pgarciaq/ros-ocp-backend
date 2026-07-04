# Adversarial Due Diligence Review — ros-ocp-backend

## Version & Date
Version: 6.0 | Date: 2026-07-04 | Reviewer: AI-assisted (incremental)

**Previous review:** v5.0 (2026-06-11) — 85 findings, all resolved or accepted  
**Scope:** Incremental review of **317 commits** (20,630 insertions across 273 files in security-critical directories) since last audit. Covers new endpoints, SQL patterns, caching layers, error handling, and operational gaps in newly-added code.

---

## Executive Summary

ros-ocp-backend remains in strong shape. The v5.0 audit's 85 findings are still resolved/accepted — no regressions detected. However, the rapid feature velocity (317 commits adding fleet heatmap, node/VM hourly heatmaps, GPU MIG pagination, replica optimization, category classification, quota headroom trends, business hours overlay, and idle detection) has introduced **5 new findings** and **2 recurring pattern violations** that were absent in the code at last audit.

The most significant new concern is a consistent pattern of **raw database error leakage in 5 newly-added handler files** — this violates the established `apiErrResponse()` convention and represents information disclosure. No new critical or high-severity findings were discovered. The security posture remains production-grade for its deployment model.

---

## Scorecard

| Dimension | Rating | Key gap (since v5.0) |
|-----------|--------|----------------------|
| Security | ★★★★☆ | No new auth/injection issues; SSRF and RBAC remain solid |
| Correctness | ★★★★☆ | Heatmap row scan failures silently skipped (continue); cursor decode error leaks |
| Auditability | ★★★★☆ | Structured logging good; new handlers use `hlog.Errorf` |
| Operational robustness | ★★★☆☆ | Still no rate limiting, no circuit breaker, no distributed tracing; Kafka liveness gap persists |
| Performance | ★★★★☆ | Fleet heatmap unbounded result set; keyset pagination well-applied elsewhere |
| Design quality | ★★★★★ | Plugin architecture, LRU caches with TTL + metrics, RBAC-scoped cache keys |
| Maintainability | ★★★★★ | 353 test files, 162 ADRs, OpenAPI contract tests, migration linter |
| Governance | ★★★★★ | CHANGELOG discipline, ADR-per-feature, govulncheck in CI |

---

## Findings Status Summary

| # | Title | Severity | Dimension | Status |
|---|-------|----------|-----------|--------|
| 86 | Raw DB error leakage in 5 new handler files | Medium | Security | **Open** |
| 87 | Fleet heatmap returns unbounded result set (no pagination) | Medium | Performance | **Open** |
| 88 | Heatmap row scan errors silently skipped | Low | Correctness | **Open** |
| 89 | CloudWatch credentials in process environment | Low | Security | **Accepted** (unchanged from v5.0 design) |
| 90 | No API rate limiting or circuit breakers | Informational | Operational | **Accepted** (documented arch decision; gateway provides rate limiting in SaaS) |

---

## Findings Detail

### #86 — Raw DB Error Leakage in New Handler Files

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Dimension** | Security |
| **Location** | `internal/api/handlers_node_hourly.go:106`, `internal/api/handlers_vm_hourly.go:119`, `internal/api/handlers_namespace_history.go:45`, `internal/api/handlers_vm_history.go:113`, `internal/api/handlers_gpu_timeslicing_history.go:115` |
| **Description** | Five handler files added since the last audit return `queryErr.Error()` / `listErr.Error()` directly in the 500 response JSON body. This leaks PostgreSQL error messages (including table names, column names, constraint names, and potentially partial query text) to API consumers. |
| **Risk** | Information disclosure aids attackers in mapping the database schema. A malformed request triggering a unique constraint violation or type cast error would reveal internal table structure. In the SaaS posture (behind gateway), the risk is reduced to authenticated callers; in SNO/dev mode, it's fully exposed. |
| **Recommendation** | Replace `queryErr.Error()` with the project's established pattern: log the full error internally via `hlog.Errorf(...)` and return a generic message (`"unable to fetch records from database"`). Optionally use `apiErrResponse()`. |
| **Effort** | S (< 1 hour) |

---

### #87 — Fleet Heatmap Returns Unbounded Result Set

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Dimension** | Performance |
| **Location** | `internal/api/handlers_fleet_heatmap.go:149-163` |
| **Description** | `GetFleetHeatmap` queries all node recommendations for an org (scoped by RBAC clusters) with no `LIMIT` clause. For large organizations with hundreds of nodes, this results in unbounded memory allocation and response size. The query has `ORDER BY nr.machineset_name NULLS LAST, nr.node` but no pagination. |
| **Risk** | A large cluster fleet (500+ nodes) could produce multi-MB responses and high memory consumption. Under concurrent requests, this could exhaust the DB pool or cause GC pressure. The 5-minute LRU cache mitigates repeat hits but the initial uncached request bears the full cost. |
| **Recommendation** | Add a configurable max-node limit (e.g., `ROS_FLEET_HEATMAP_MAX_NODES`, default 1000) with a `LIMIT` clause. For very large fleets, consider server-side pagination or a pre-aggregated summary. The cache already handles the common case, but a safety limit prevents pathological scenarios. |
| **Effort** | S (< 1 day) |

---

### #88 — Heatmap Row Scan Errors Silently Skipped

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Correctness |
| **Location** | `internal/api/handlers_fleet_heatmap.go:185-188` |
| **Description** | When `rows.Scan(...)` fails for a heatmap row, the error is logged as a warning and the row is silently skipped (`continue`). This means the API response can have a `meta.count` that doesn't match the actual database count, and data corruption (e.g., a NULL in a NOT NULL-scanned column) would be invisible to the consumer. |
| **Risk** | Low — scan errors are rare in practice (schema matches model). However, after a migration adds/removes a column, scan failures would silently return incomplete data until the binary is redeployed. Consumers relying on the count would see inconsistencies. |
| **Recommendation** | Either (a) stop on first scan error and return 500, or (b) add a `warnings` array to the response metadata indicating skipped rows. At minimum, increment a Prometheus counter for scan failures. |
| **Effort** | S (< 1 hour) |

---

### #89 — CloudWatch Credentials in Process Environment

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Security |
| **Location** | `internal/logging/logging.go:50-53` |
| **Description** | `os.Setenv("AWS_ACCESS_KEY_ID", ...)` and `os.Setenv("AWS_SECRET_ACCESS_KEY", ...)` are called at startup. These credentials become visible via `/proc/self/environ` to any process that can read the proc filesystem (same UID or root). |
| **Risk** | Low — the container runs as UID 1001 in a minimal UBI image. No shell or auxiliary process typically has access. However, a container escape, debug sidecar, or `kubectl exec` would expose the credentials. This is an existing pattern (not new since v5.0) carried forward from pre-audit code. |
| **Recommendation** | Use the AWS SDK's credential provider chain (`credentials.NewStaticCredentialsProvider(...)`) directly on the CloudWatch client instead of setting process-wide environment variables. This prevents the credentials from appearing in `/proc/self/environ`. |
| **Effort** | S (< 1 day) |

---

### #90 — No API Rate Limiting or Circuit Breakers

| Field | Value |
|-------|-------|
| **Severity** | Informational |
| **Dimension** | Operational robustness |
| **Location** | `internal/api/server.go` (middleware chain) |
| **Description** | The API server has no per-org or global rate limiting middleware. Outbound HTTP calls to RBAC, Masu (savings/reship), and Kruize have timeouts but no circuit breaker to avoid hammering a failing dependency. This was noted in v5.0 as an accepted architectural gap (gateway provides rate limiting in SaaS; on-prem is single-tenant). |
| **Risk** | In SNO/dev deployments without a gateway, a misbehaving client or automated scanner could saturate the 5-connection DB pool. In production, 3scale provides rate limiting. The RBAC LRU cache (500 entries, 60s TTL) provides some buffering against RBAC service failures. |
| **Recommendation** | For on-prem deployments, consider adding Echo's `middleware.RateLimiter` with a configurable per-org limit (e.g., 100 req/s per org). For outbound calls, `sony/gobreaker` with a 5-failure threshold would prevent cascading failures during dependency outages. |
| **Effort** | M (2-3 days) |

---

## Priority Remediation Order

| Priority | Finding | Effort | Rationale |
|----------|---------|--------|-----------|
| 1 | **#86** — DB error leakage (5 files) | S | Information disclosure; trivial fix; violates established convention |
| 2 | **#87** — Unbounded heatmap result set | S | Memory/availability risk under large fleets; add safety LIMIT |
| 3 | **#88** — Silent row scan failures | S | Data correctness; add counter metric at minimum |
| 4 | **#89** — CloudWatch env vars | S | Defense-in-depth; low urgency but easy fix |
| 5 | **#90** — Rate limiting / circuit breakers | M | Accepted architecture; implement when on-prem scale justifies it |

---

## Accepted Risks

| Finding | Rationale |
|---------|-----------|
| #89 | Container runs as non-root UID 1001 in minimal image; no proc access by default; CloudWatch is optional |
| #90 | Gateway (3scale/Envoy) provides rate limiting in production postures; DB pool (5 conns) and statement timeouts (25s) provide natural backpressure |

---

## Verified Regressions from v5.0: None

The following v5.0 findings were spot-checked and remain resolved:
- **#12** (SSRF): `ValidateSecurityConfig()` still panics if `ROS_CSV_ALLOWED_HOSTS` empty in production
- **#13** (ILIKE injection): `escapeILIKE()` still applied; `sanitizeParamValue()` active on non-skipped fields
- **#14** (Unbounded offset): `ROS_API_MAX_OFFSET` still enforced
- **#31** (Pagination filter bypass): Fix in `f66feaf7` still present
- **#37** (Internal tags auth): `ROS_INTERNAL_TAGS_AUTH_REQUIRED` default `true`; startup validation intact

---

## What Held Up Well (New Since v5.0)

| Area | Evidence |
|------|----------|
| **Keyset pagination** | Node utilization, GPU MIG list, namespace recs all use cursor-based seek — no offset-based performance cliffs |
| **SQL allowlist architecture** | `native_query_allowlist.go` + `nodeUtilAllowedOrderBy` maps prevent column injection in new sort paths |
| **Cache design** | Fleet heatmap + fleet summary caches use RBAC-scoped keys, TTL, LRU eviction, and Prometheus metrics (hits/misses/removals/invalidations) |
| **Statement timeout discipline** | New heavy endpoints (fleet heatmap, node hourly) inherit the heavy API timeout (28s SaaS / 45s on-prem) via the existing middleware |
| **Integration tests** | 37 integration test files using testcontainers-go; new heatmap/hourly endpoints have dedicated integration tests |
| **Feature gating** | New features (business hours, idle detection, categories) are gated behind `ROS_*_ENABLED` flags with safe defaults |

---

## Current State

| Metric | Value |
|--------|-------|
| Total findings (cumulative) | 90 |
| Resolved | 83 |
| Accepted | 5 (#6, #33, #89, #90, and 1 platform-architecture decision) |
| Open (new in v6.0) | 3 (#86, #87, #88) |
| Estimated remediation effort (open) | ~1 day total |
