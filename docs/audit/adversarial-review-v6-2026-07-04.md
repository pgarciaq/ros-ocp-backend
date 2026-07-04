# Adversarial Due Diligence Review — ros-ocp-backend

## Version & Date
Version: 6.1 | Date: 2026-07-04 | Reviewer: AI-assisted (incremental)

**Previous review:** v6.0 (2026-07-04) — findings #86–#90, all resolved or accepted  
**Scope:** Fresh deep pass covering rate limiter implementation, SQL injection surface, cache integrity, concurrency, Kafka ingestion pipeline, database operations, S3/object storage, graceful shutdown, and business hours logic. Includes both API/middleware and infrastructure/ingestion dimensions.

---

## Executive Summary

The v6.0 remediation sprint successfully resolved all 5 initial findings (#86–#90). A subsequent deep pass on infrastructure, ingestion, and caching layers — areas not covered in the v6.0 review — surfaced **11 new findings** (#91–#101).

The most significant finding is **#95 (HIGH)**: Kafka consumer worker goroutines lack `recover()`, meaning a panic in any message handler kills the consumer or leaks a WaitGroup counter that blocks graceful shutdown. The remaining findings are defense-in-depth improvements (4 MEDIUM correctness/reliability issues, 2 MEDIUM concurrency/DoS, 1 MEDIUM SSRF, and 4 LOW operational gaps).

No **Critical** findings. No cross-org data leakage. No SQL injection. Authentication and RBAC remain solid. The codebase's security posture remains strong for its deployment model.

---

## Scorecard

| Dimension | Rating | Key gap (since v6.0) |
|-----------|--------|----------------------|
| Security | ★★★★★ | S3 readiness endpoint lacks SSRF validation (#99); rate limiter IP fallback spoofable (#93) — both low-risk in containerized deployment |
| Correctness | ★★★★☆ | Fleet heatmap cache ignores cluster filter (#91); overnight business hours broken (#101) |
| Auditability | ★★★★☆ | Unchanged — structured logging good |
| Operational robustness | ★★★★☆ | Kafka panic recovery missing (#95); HTTP timeouts incomplete (#100) |
| Performance | ★★★★★ | No new issues; keyset pagination and LRU caches well-applied |
| Design quality | ★★★★★ | Plugin architecture, bounded channels, configurable limits |
| Maintainability | ★★★★★ | 353 test files, 162 ADRs, OpenAPI contract tests |
| Governance | ★★★★★ | CHANGELOG discipline, ADR-per-feature, govulncheck in CI |

---

## Findings Status Summary

| # | Title | Severity | Dimension | Status |
|---|-------|----------|-----------|--------|
| 86 | Raw DB error leakage in 5 new handler files | Medium | Security | **Resolved** ([#143](https://github.com/pgarciaq/ros-ocp-backend/issues/143)) |
| 87 | Fleet heatmap returns unbounded result set | Medium | Performance | **Resolved** ([#144](https://github.com/pgarciaq/ros-ocp-backend/issues/144)) |
| 88 | Heatmap row scan errors silently skipped | Low | Correctness | **Resolved** ([#145](https://github.com/pgarciaq/ros-ocp-backend/issues/145)) |
| 89 | CloudWatch credentials in process environment | Low | Security | **Resolved** ([#146](https://github.com/pgarciaq/ros-ocp-backend/issues/146)) |
| 90 | No API rate limiting or circuit breakers | Informational | Operational | **Resolved** ([#37](https://github.com/pgarciaq/ros-ocp-backend/issues/37)) |
| 91 | Fleet heatmap cache key excludes `clusterFilter` | Medium | Correctness | **Resolved** ([#148](https://github.com/pgarciaq/ros-ocp-backend/issues/148)) |
| 92 | Unvalidated `term` parameter in fleet heatmap | Low | Correctness | **Open** |
| 93 | Rate limiter IP fallback spoofable via X-Forwarded-For | Low | Security | **Open** |
| 94 | In-memory rate limiter per-replica | Low | Operational | **Accepted** |
| 95 | No panic recovery in Kafka worker goroutines | High | Reliability | **Resolved** ([#147](https://github.com/pgarciaq/ros-ocp-backend/issues/147)) |
| 96 | No length bound on Files/Object_keys slices in KafkaMsg | Medium | DoS | **Resolved** ([#151](https://github.com/pgarciaq/ros-ocp-backend/issues/151)) |
| 97 | DB/Pool singletons initialized without sync.Once | Medium | Concurrency | **Resolved** ([#150](https://github.com/pgarciaq/ros-ocp-backend/issues/150)) |
| 98 | context.Background() in ingest path — cancellation not propagated | Medium | Reliability | **Open** |
| 99 | S3 readiness endpoint not validated against SSRF allowlist | Medium | Security | **Resolved** ([#152](https://github.com/pgarciaq/ros-ocp-backend/issues/152)) |
| 100 | HTTP server missing ReadTimeout/WriteTimeout/IdleTimeout | Low | DoS | **Open** |
| 101 | InBusinessHours does not handle overnight schedules | Medium | Correctness | **Resolved** ([#149](https://github.com/pgarciaq/ros-ocp-backend/issues/149)) |

---

## Findings Detail

### #86–#90 (Resolved/Accepted)

See v6.0 report sections below for historical record.

<details>
<summary>v6.0 findings (click to expand)</summary>

#### #86 — Raw DB Error Leakage in New Handler Files

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Status** | **Resolved** |
| **Location** | `internal/api/handlers_node_hourly.go:106`, `handlers_vm_hourly.go:119`, `handlers_namespace_history.go:45`, `handlers_vm_history.go:113`, `handlers_gpu_timeslicing_history.go:115` |
| **Resolution** | All 5 handlers now return generic `"unable to fetch records from database"`. [#143](https://github.com/pgarciaq/ros-ocp-backend/issues/143) |

#### #87 — Fleet Heatmap Returns Unbounded Result Set

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Status** | **Resolved** |
| **Location** | `internal/api/handlers_fleet_heatmap.go:149-163` |
| **Resolution** | Added `ROS_FLEET_HEATMAP_MAX_NODES` (default 1000) with LIMIT + truncation warning. [#144](https://github.com/pgarciaq/ros-ocp-backend/issues/144) |

#### #88 — Heatmap Row Scan Errors Silently Skipped

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Status** | **Resolved** |
| **Location** | `internal/api/handlers_fleet_heatmap.go:185-188` |
| **Resolution** | Prometheus counter + meta.warnings. [#145](https://github.com/pgarciaq/ros-ocp-backend/issues/145) |

#### #89 — CloudWatch Credentials in Process Environment

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Status** | **Resolved** |
| **Location** | `internal/logging/logging.go:49-53` |
| **Resolution** | Replaced `os.Setenv` with `credentials.NewStaticCredentials` via `*aws.Config`. [#146](https://github.com/pgarciaq/ros-ocp-backend/issues/146) |

#### #90 — No API Rate Limiting or Circuit Breakers

| Field | Value |
|-------|-------|
| **Severity** | Informational |
| **Status** | **Resolved** |
| **Resolution** | Per-org token bucket implemented. Circuit breakers evaluated and accepted as unnecessary — existing protections (pgxpool limits, statement timeouts, readiness probes, gateway circuit breaking) provide equivalent coverage. [#37](https://github.com/pgarciaq/ros-ocp-backend/issues/37) |

</details>

---

### #91 — Fleet Heatmap Cache Key Excludes `clusterFilter`

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Dimension** | Correctness |
| **Status** | Open |
| **Location** | `internal/api/handlers_fleet_heatmap.go:129-132, 276` |
| **Description** | The cache key includes `orgID`, `rbacScoped`, `userPerms`, `metric`, `term`, `engine` — but NOT the `filter[cluster]` query parameter. When a request includes a cluster filter, the filtered result is cached under a key that doesn't include the filter. Subsequent requests without a filter (or with a different filter) receive the wrong cached data. |
| **Risk** | Intra-org data inconsistency. User A filters by `prod-cluster` → result cached → User B requests all clusters → gets only `prod-cluster`'s nodes. No cross-org leak (orgID is in key), but silently wrong data and wrong `meta.count`. |
| **Recommendation** | Include `clusterFilter` in the cache key, or bypass the cache entirely when `clusterFilter != ""`. |
| **Effort** | S (< 1 hour) |

---

### #92 — Unvalidated `term` Parameter in Fleet Heatmap

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Correctness / DoS |
| **Status** | Open |
| **Location** | `internal/api/handlers_fleet_heatmap.go:110-113` |
| **Description** | `engine` and `metric` are validated against allowlists, but `term` is not. An arbitrary `term` value is included in the LRU cache key, reflected in the response `meta.term`, and passed as a parameterized SQL value (no injection). Compare with `handlers_savings_summary.go:112-113` which validates `term` against `short|medium|long`. |
| **Risk** | Cache pollution: hundreds of distinct `term` values evict legitimate entries from the 256-entry LRU. Invalid terms return empty results silently instead of a 400. |
| **Recommendation** | Add `term` allowlist validation: `if term != "short" && term != "medium" && term != "long" { return 400 }`. |
| **Effort** | S (< 30 minutes) |

---

### #93 — Rate Limiter IP Fallback Spoofable via X-Forwarded-For

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Security |
| **Status** | Open |
| **Location** | `internal/api/middleware/rate_limiter.go:50`, `internal/api/server.go` |
| **Description** | When identity has an empty `OrgID`, the rate limiter falls back to `c.RealIP()`. Echo's `RealIP()` with no explicit `IPExtractor` reads `X-Real-IP` then `X-Forwarded-For`. Both can be set by the caller if not stripped by upstream proxy. |
| **Risk** | Low — production proxy controls identity header and X-Forwarded-For. In dev/staging without proxy, attacker can rotate spoofed IPs to bypass rate limit, or exhaust another IP's bucket. |
| **Recommendation** | (1) Reject identities with empty `OrgID` at the Identity middleware layer (return 401). (2) Configure `app.IPExtractor` explicitly. |
| **Effort** | S (< 1 hour) |

---

### #94 — In-Memory Rate Limiter Per-Replica

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Operational |
| **Status** | Open |
| **Location** | `internal/api/middleware/rate_limiter.go:36-42` |
| **Description** | `RateLimiterMemoryStore` is per-process. With N replicas, effective limit is `N × RateLimitRPM`. Disabled by default (`ROS_API_RATE_LIMIT_ENABLED=false`). |
| **Risk** | Known limitation. At typical scale (2-5 replicas), effective cap is 120-300 RPM instead of 60. Gateway (3scale) provides the hard enforcement. |
| **Recommendation** | Document as known limitation. Replace with Redis-backed limiter only if hard enforcement is required. |
| **Effort** | M (if Redis-backed), or accept |

---

### #95 — No Panic Recovery in Kafka Worker Goroutines

| Field | Value |
|-------|-------|
| **Severity** | High |
| **Dimension** | Reliability / Availability |
| **Status** | Open |
| **Location** | `internal/kafka/consumer.go:34-40` |
| **Description** | `wrapHandlerWithInFlight` calls `handler(ctx, msg, consumer)` with no surrounding `recover()`. If the handler panics (nil dereference in CSV parser, type assertion failure, OOB slice access), the goroutine crashes without recovery. In sequential mode, this kills the consumer loop. In parallel mode (`KafkaWorkers > 1`), it leaks `inFlight.Done()`, causing `drainInFlightHandlers` to hang for the full `ShutdownTimeoutSecs`. |
| **Risk** | A single malformed message can take down the entire Kafka consumer, stopping all data ingestion for the pod. The WaitGroup leak blocks graceful shutdown, requiring SIGKILL. |
| **Recommendation** | Add `defer func() { if r := recover(); r != nil { log.Errorf("panic in message handler: %v\n%s", r, debug.Stack()) } }()` inside `wrapHandlerWithInFlight`, after `defer inFlight.Done()`. |
| **Effort** | S (< 1 hour) |

---

### #96 — No Length Bound on Files/Object_keys Slices in KafkaMsg

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Dimension** | DoS / Resource Exhaustion |
| **Status** | Open |
| **Location** | `internal/types/kafkaMsg.go:25` |
| **Description** | `Files []string validate:"required"` and `Object_keys` have no `max=` validator tag. A malformed Kafka message with 100K entries causes `parallelIngestFiles` to iterate over all elements, consuming CPU and memory proportional to list length. |
| **Risk** | Kafka message size limits (~1 MB default) provide an implicit upper bound, but application-layer validation is missing. A carefully crafted message with many short paths could carry thousands of entries within 1 MB. |
| **Recommendation** | Add `validate:"required,max=1000"` (or domain-appropriate maximum) to `Files` and `Object_keys` fields. |
| **Effort** | S (< 30 minutes) |

---

### #97 — DB/Pool Singletons Initialized Without sync.Once

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Dimension** | Concurrency / Correctness |
| **Status** | Open |
| **Location** | `internal/db/db.go` — `GetDB()` and `GetPool()` |
| **Description** | `if DB == nil { initDB() }` is a plain pointer read without synchronization. If two goroutines race to call `GetDB()` simultaneously during startup (multiple Kafka workers processing first message concurrently), `initDB()` can be called twice. The second call reassigns `DB`, leaving the first caller with a stale pointer. |
| **Risk** | Low in practice (workers start serially in most deployments), but violates Go concurrency safety guarantees. Under test parallelism, this manifests as flaky failures. |
| **Recommendation** | Use `sync.Once` for both `GetDB()` and `GetPool()`. |
| **Effort** | S (< 30 minutes) |

---

### #98 — context.Background() in Ingest Path — Cancellation Not Propagated

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Dimension** | Reliability / Resource Leak |
| **Status** | Open |
| **Location** | `internal/services/report_processor.go:222, 254, 406, 491, 583, 641, 663, 692, 761, 790` |
| **Description** | Every file-level ingest call uses `context.Background()`. The parent Kafka handler receives a `ctx` cancelled on SIGTERM, but ingest functions ignore cancellation. A large CSV mid-ingestion continues running against a shutting-down connection pool, blocking graceful shutdown for minutes. |
| **Risk** | Pod shutdown exceeds `terminationGracePeriodSeconds`, triggering SIGKILL and potential data inconsistency (half-written transactions). |
| **Recommendation** | Thread the parent `ctx` through each `process*CSVIngest` call and downstream `pgx` transaction calls. |
| **Effort** | M (touches 10+ call sites across the ingest pipeline) |

---

### #99 — S3 Readiness Endpoint Not Validated Against SSRF Allowlist

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Dimension** | Security (SSRF) |
| **Status** | Open |
| **Location** | `internal/health/readyz.go` — `checkS3` function |
| **Description** | `ReadinessS3Endpoint` from config is passed as `o.BaseEndpoint` with no URL validation or private-network denial. The CSV download path has robust SSRF mitigation (`validateCSVDownloadURL`, `denyRestrictedHost`), but the S3 health check has none. |
| **Risk** | An operator misconfiguring `ROS_READINESS_S3_ENDPOINT=http://169.254.169.254/latest/meta-data/` would cause the readiness probe to hit the EC2 metadata endpoint. Limited risk in containers (no IAM role on most pods), but deviates from the "validate all configurable endpoints" principle. |
| **Recommendation** | Add startup validation that `ROS_READINESS_S3_ENDPOINT` is a valid http/https URL and passes `denyRestrictedHost()`. |
| **Effort** | S (< 1 hour) |

---

### #100 — HTTP Server Missing ReadTimeout/WriteTimeout/IdleTimeout

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | DoS / Resource Exhaustion |
| **Status** | Open |
| **Location** | `internal/api/server.go:299-302` |
| **Description** | The `http.Server` sets only `ReadHeaderTimeout`. Without `ReadTimeout`, slow-loris attacks can hold connections after header parsing. Without `WriteTimeout` and `IdleTimeout`, slow clients and idle keep-alive connections accumulate file descriptors. |
| **Risk** | Under high load (misconfigured scraper, DDoS), file descriptor exhaustion. Gateway/Envoy typically provides these timeouts in production. |
| **Recommendation** | Add `ReadTimeout: 60s`, `WriteTimeout: 60s`, `IdleTimeout: 120s`. |
| **Effort** | S (< 30 minutes) |

---

### #101 — InBusinessHours Does Not Handle Overnight Schedules

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Dimension** | Business Logic Correctness |
| **Status** | Open |
| **Location** | `internal/bhschedule/schedule.go:123` |
| **Description** | `return localMin >= startMin && localMin < endMin` is correct for same-day schedules (09:00–17:00), but if an administrator configures an overnight schedule (22:00–06:00), then `startMin=1320 > endMin=360`, and the condition is never true. All data is classified as "outside business hours," producing incorrect savings calculations. |
| **Risk** | Silent data misclassification for organizations in timezone offsets or with shift-work schedules (manufacturing, 24/7 ops). Savings estimates will be overstated because "business hours" usage appears to be zero. |
| **Recommendation** | Add wrap-around logic: `if startMin <= endMin { return localMin >= startMin && localMin < endMin } else { return localMin >= startMin || localMin < endMin }`. Also add config validation warning when `endMin < startMin`. |
| **Effort** | S (< 1 hour) |

---

## Priority Remediation Order

| Priority | Finding | Severity | Effort | Rationale |
|----------|---------|----------|--------|-----------|
| 1 | **#95** — Kafka panic recovery | High | S | Single panic kills ingestion for the pod |
| 2 | **#91** — Heatmap cache key missing clusterFilter | Medium | S | Silently wrong data for filtered requests |
| 3 | **#101** — Overnight business hours | Medium | S | Silent data misclassification |
| 4 | **#97** — DB singletons without sync.Once | Medium | S | Concurrency safety violation |
| 5 | **#96** — KafkaMsg Files unbounded | Medium | S | Defense-in-depth against oversized messages |
| 6 | **#99** — S3 readiness SSRF | Medium | S | Consistency with existing SSRF defenses |
| 7 | **#98** — context.Background() in ingest | Medium | M | Graceful shutdown reliability |
| 8 | **#92** — Unvalidated term parameter | Low | S | Cache pollution prevention |
| 9 | **#100** — HTTP server timeouts | Low | S | Slow-loris defense |
| 10 | **#93** — Rate limiter IP spoofing | Low | S | Defense-in-depth |
| 11 | **#94** — Per-replica rate limiter | Low | Accept | Known limitation; gateway enforces |

---

## Accepted Risks

| Finding | Rationale |
|---------|-----------|
| #94 (per-replica limiter) | Disabled by default; gateway provides hard enforcement in production; current implementation is appropriate for dev/staging best-effort |

---

## Verified: No Regressions from v6.0

- **#86 fix held:** Spot-checked `handlers_fleet_heatmap.go`, `handlers_node_hourly.go`, `handlers_vm_hourly.go` — all return generic error messages ✅
- **#87 fix held:** `LIMIT` clause present in fleet heatmap query ✅
- **#88 fix held:** Prometheus counter and meta.warnings on scan errors ✅
- **#89 fix held:** No `os.Setenv` for AWS credentials ✅
- **#90 fix held:** Rate limiter middleware wired in server.go ✅

## Verified: No SQL Injection

All user inputs in `handlers_fleet_heatmap.go`, `handlers_node_hourly.go`, `handlers_vm_hourly.go`, and `handlers_fleet.go` flow into parameterized `pool.Query($1, $2, ...)` calls. No string concatenation in SQL. `native_query_allowlist.go` + `nodeUtilAllowedOrderBy` maps prevent column injection in sort paths. ✅

## Verified: No Cross-Org Data Leakage

`orgID` and `SHA256(userPerms)` are included in all cache keys. `InvalidateOrg` uses prefix matching. Cache issue #91 is intra-org only. ✅

---

## What Held Up Well

| Area | Evidence |
|------|----------|
| **SSRF defenses (CSV path)** | `validateCSVDownloadURL`, `denyRestrictedHost`, `ROS_CSV_ALLOWED_HOSTS` all intact |
| **Kafka backpressure** | Bounded channel (`workers*2`), WaitGroup drain, `ManifestDownloadWorkers` limit |
| **Data validation** | `json.Valid` + `json.Unmarshal` + `validator.Struct` before any ingestion |
| **Poison message handling** | `commitOnPermanentFailure` ACKs only structural failures; transient errors redeliver |
| **Transaction safety** | Single-tx ingest with `ON CONFLICT DO UPDATE SET`; `pgx.Batch` bounded to 500 |
| **Auth on internal endpoints** | `authenticateInternalCaller()` + `validateInternalOrgTarget()` + K8s TokenReview |
| **Dependency hygiene** | No known actionable CVEs in go.mod; `golang.org/x/net`, `golang.org/x/crypto`, Echo all current |

---

## Current State

| Metric | Value |
|--------|-------|
| Total findings (cumulative) | 101 |
| Resolved | 90 (#1–#85 from prior reviews, #86–#89 from v6.0, #90) |
| Partially resolved | 0 |
| Accepted | 1 (#94 per-replica limiter) |
| Open | 4 (#92, #93, #98, #100) |
