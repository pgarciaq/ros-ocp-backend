# Adversarial Due Diligence Review — ros-ocp-backend

## Version & Date
Version: 6.0 | Date: 2026-07-04 | Reviewer: AI-assisted (incremental)

**Previous review:** v5.0 (2026-06-11) — 85 findings, all resolved or accepted  
**Scope:** Incremental review of **317 commits** (20,630 insertions across 273 files in security-critical directories) since last audit. Covers new endpoints, SQL patterns, caching layers, error handling, and operational gaps in newly-added code.

---

## Executive Summary

ros-ocp-backend remains in strong shape. The v5.0 audit's 85 findings are still resolved/accepted — no regressions detected. The rapid feature velocity (317 commits adding fleet heatmap, node/VM hourly heatmaps, GPU MIG pagination, replica optimization, category classification, quota headroom trends, business hours overlay, and idle detection) introduced 5 findings in the initial v6.0 review.

**Update (2026-07-04):** Findings #86 (raw DB error leakage), #87 (unbounded heatmap result set), and #88 (silent row scan errors) have been **resolved** via commits on branch `pgarciaq-rosocp-superpowers-phase15`. Finding #90 (no rate limiting) has been **partially resolved** — per-org API rate limiting is now implemented and available behind `ROS_API_RATE_LIMIT_ENABLED`; circuit breakers remain an accepted gap.

No open findings remain. The security posture is production-grade for its deployment model.

---

## Scorecard

| Dimension | Rating | Key gap (since v5.0) |
|-----------|--------|----------------------|
| Security | ★★★★★ | No new auth/injection issues; DB error leakage fixed; SSRF and RBAC remain solid |
| Correctness | ★★★★★ | Heatmap scan errors now reported via meta.warnings + Prometheus counter |
| Auditability | ★★★★☆ | Structured logging good; new handlers use `hlog.Errorf` |
| Operational robustness | ★★★★☆ | Per-org rate limiting implemented; circuit breakers remain a documented gap |
| Performance | ★★★★★ | Fleet heatmap capped with configurable LIMIT; keyset pagination well-applied |
| Design quality | ★★★★★ | Plugin architecture, LRU caches with TTL + metrics, RBAC-scoped cache keys |
| Maintainability | ★★★★★ | 353 test files, 162 ADRs, OpenAPI contract tests, migration linter |
| Governance | ★★★★★ | CHANGELOG discipline, ADR-per-feature, govulncheck in CI |

---

## Findings Status Summary

| # | Title | Severity | Dimension | Status |
|---|-------|----------|-----------|--------|
| 86 | Raw DB error leakage in 5 new handler files | Medium | Security | **Resolved** ([#143](https://github.com/pgarciaq/ros-ocp-backend/issues/143)) |
| 87 | Fleet heatmap returns unbounded result set (no pagination) | Medium | Performance | **Resolved** ([#144](https://github.com/pgarciaq/ros-ocp-backend/issues/144)) |
| 88 | Heatmap row scan errors silently skipped | Low | Correctness | **Resolved** ([#145](https://github.com/pgarciaq/ros-ocp-backend/issues/145)) |
| 89 | CloudWatch credentials in process environment | Low | Security | **Accepted** (unchanged from v5.0 design) |
| 90 | No API rate limiting or circuit breakers | Informational | Operational | **Partially resolved** — rate limiting implemented ([#37](https://github.com/pgarciaq/ros-ocp-backend/issues/37)); circuit breakers remain accepted gap |

---

## Findings Detail

### #86 — Raw DB Error Leakage in New Handler Files

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Dimension** | Security |
| **Status** | **Resolved** |
| **Location** | `internal/api/handlers_node_hourly.go:106`, `internal/api/handlers_vm_hourly.go:119`, `internal/api/handlers_namespace_history.go:45`, `internal/api/handlers_vm_history.go:113`, `internal/api/handlers_gpu_timeslicing_history.go:115` |
| **Description** | Five handler files added since the last audit return `queryErr.Error()` / `listErr.Error()` directly in the 500 response JSON body. This leaks PostgreSQL error messages (including table names, column names, constraint names, and potentially partial query text) to API consumers. |
| **Resolution** | All 5 handlers now log the full error via `hlog.Errorf(...)` and return a generic `"unable to fetch records from database"` message. Implemented in [#143](https://github.com/pgarciaq/ros-ocp-backend/issues/143). |

---

### #87 — Fleet Heatmap Returns Unbounded Result Set

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Dimension** | Performance |
| **Status** | **Resolved** |
| **Location** | `internal/api/handlers_fleet_heatmap.go:149-163` |
| **Description** | `GetFleetHeatmap` queries all node recommendations for an org (scoped by RBAC clusters) with no `LIMIT` clause. For large organizations with hundreds of nodes, this results in unbounded memory allocation and response size. The query has `ORDER BY nr.machineset_name NULLS LAST, nr.node` but no pagination. |
| **Resolution** | Added configurable `ROS_FLEET_HEATMAP_MAX_NODES` (default 1000) with a `LIMIT maxNodes+1` clause. When truncated, `meta.warnings` reports the cap and suggests filtering by cluster. Implemented in [#144](https://github.com/pgarciaq/ros-ocp-backend/issues/144). |

---

### #88 — Heatmap Row Scan Errors Silently Skipped

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Correctness |
| **Status** | **Resolved** |
| **Location** | `internal/api/handlers_fleet_heatmap.go:185-188` |
| **Description** | When `rows.Scan(...)` fails for a heatmap row, the error is logged as a warning and the row is silently skipped (`continue`). This means the API response can have a `meta.count` that doesn't match the actual database count, and data corruption (e.g., a NULL in a NOT NULL-scanned column) would be invisible to the consumer. |
| **Resolution** | Scan errors now increment Prometheus counter `rosocp_fleet_heatmap_scan_errors_total` and report failures in `meta.warnings` (e.g., "2 rows could not be read"). Implemented in [#145](https://github.com/pgarciaq/ros-ocp-backend/issues/145). |

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
| **Status** | **Partially resolved** |
| **Location** | `internal/api/server.go` (middleware chain) |
| **Description** | The API server previously had no per-org or global rate limiting middleware. Outbound HTTP calls to RBAC, Masu (savings/reship), and Kruize have timeouts but no circuit breaker to avoid hammering a failing dependency. |
| **Resolution (rate limiting)** | Per-org token bucket rate limiter implemented using Echo's built-in `RateLimiterMemoryStore`. Configured via `ROS_API_RATE_LIMIT_ENABLED` (default `false`), `ROS_API_RATE_LIMIT_RPM` (default 60), `ROS_API_RATE_LIMIT_BURST` (default 30). Prometheus counter `rosocp_rate_limited_requests_total` tracks denied requests. Implemented in [#37](https://github.com/pgarciaq/ros-ocp-backend/issues/37). |
| **Remaining gap (circuit breakers)** | Outbound calls to RBAC, Masu, and Kruize still lack circuit breaker patterns. The RBAC LRU cache (500 entries, 60s TTL) provides some buffering. Accepted as low priority — gateway provides additional protection in SaaS; on-prem is single-tenant. |
| **Effort** | Remaining: M (circuit breakers, if justified by scale) |

---

## Priority Remediation Order

All actionable findings have been resolved:

| Priority | Finding | Status |
|----------|---------|--------|
| 1 | **#86** — DB error leakage (5 files) | ✅ Resolved |
| 2 | **#87** — Unbounded heatmap result set | ✅ Resolved |
| 3 | **#88** — Silent row scan failures | ✅ Resolved |
| 4 | **#89** — CloudWatch env vars | Accepted (low urgency) |
| 5 | **#90** — Rate limiting / circuit breakers | ✅ Rate limiting resolved; circuit breakers accepted |

---

## Accepted Risks

| Finding | Rationale |
|---------|-----------|
| #89 | Container runs as non-root UID 1001 in minimal image; no proc access by default; CloudWatch is optional |
| #90 (circuit breakers only) | Gateway (3scale/Envoy) provides additional protection in production; DB pool (5 conns) and statement timeouts (25s) provide natural backpressure; RBAC LRU cache buffers RBAC failures |

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
| Resolved | 88 (including #86, #87, #88, and rate limiting portion of #90) |
| Accepted | 3 (#89, #90 circuit breakers, and 1 platform-architecture decision) |
| Open | 0 |
