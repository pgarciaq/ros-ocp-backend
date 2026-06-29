# ROS-OCP-Backend — Performance & Scalability Analysis

**Date:** 2026-06-16  
**Scope:** `ros-ocp-backend` only (Go service: Kafka processor, native recommendation engine, REST API, PostgreSQL)  
**Branch reviewed:** `pgarciaq-rosocp-superpowers-phase13`  
**Modes:** On-prem (cost-onprem Helm, native engine, shared PostgreSQL) vs SaaS (Clowder, RBAC, ingress ~30s budget)

All configuration defaults cited below come from `internal/config/config.go` unless noted. Code paths are in this repository; deployment wiring for `GOMEMLIMIT` and PostgreSQL sizing lives in the cost-onprem chart / Clowder AppConfig, not in the Go binary itself.

---

## Executive Summary

ROS-OCP-Backend is a **well-instrumented, PostgreSQL-bound Go service** whose performance story centers on three pipelines:

1. **Kafka ingest → daily digests** (CSV download, streaming parse, `pgx.Batch` upserts)
2. **Native recommendation engine** (streaming digest reads, integer math, batched writes)
3. **REST API** (keyset pagination via `org_container_keys`, bounded LRU caches, heavy-query timeouts)

The rewrite from Kruize to the native engine delivered real wins: percentiles computed at **ingest** time (`internal/ingestion/digest.go`), recommendations streamed in batches of **500 containers** (`RecommendWorkloadsStreaming` in `internal/engine/recommend_all.go`), and list APIs routed through **`org_container_keys`** instead of `DISTINCT ON` over `recommendation_sets` for the default path.

**Honest assessment:** Production readiness for large fleets (10k+ containers per cluster, multi-cluster orgs) is **good for the container hot path**, **weaker for namespace lists and cross-cutting PostgreSQL headroom**, and **absent for API rate limiting**. The service will scale **vertically** (bigger processor pod, tuned PostgreSQL) and **horizontally** (more Kafka partitions + processor replicas) before it needs architectural rewrites.

**Primary cliffs:**

| Cliff | Trigger | Symptom |
|-------|---------|---------|
| **PostgreSQL connection budget** | `ROS_DB_MAX_CONNS=5` × (API + processor + housekeeper replicas) > `max_connections` | `rosocp_db_pool_acquire_duration_seconds` rises; acquire timeouts after 5s |
| **Processor wall time** | Large manifest: many CSVs × (download + digest + recommend + VM/GPU/node plugins) | Kafka consumer lag; single partition processed serially despite 3 workers |
| **Namespace / stale list path** | `filter[stale]=true` or namespace pagination | `DISTINCT ON` over large `recommendation_sets` / `namespace_recommendation_sets` |
| **Savings recalc storm** | Koku cost-model change for org with many clusters | Batched UPDATEs (fixed in phase13) still fan out per cluster × rec type |
| **Memory on huge CSV** | Container CSV >25k rows without incremental flush | Falls off single-transaction fast path; higher peak RSS during grouping |

**What is genuinely well-engineered:**

- Unified **pgxpool** shared by GORM and raw pgx (`internal/db/db.go`, ADR-0128)
- **Manual Kafka commit** with transient retry + DLQ (`KAFKA_AUTO_COMMIT=false`, `internal/services/kafka_retry.go`)
- **Partition-scoped parallelism** with serialized commits (`internal/kafka/consumer.go`, ADR-0154)
- **Statement timeouts** split by path: API 25s, ingest 120s `SET LOCAL`, heavy API 45s (`internal/db/statement_timeout.go`)
- **Bounded LRU caches** with Prometheus metrics (RBAC, fleet summary, savings summary, cost rates)
- **Coalesced async recalc** (`threshold_recalc_guard.go`, `savings_recalc_guard.go`) capped at 3 concurrent clusters
- **SSRF-hardened CSV download** (`internal/utils/csv_security.go`, ADR-0146)
- **Prometheus pipeline phase histograms** without tenant labels (`internal/metrics/metrics.go`, ADR-0243)

---

## Architecture Overview (Performance Lens)

```
OpenShift cluster
    → Ingress (presigned URL in Kafka payload)
        → ros-ocp processor (cmd/start processor)
            → utils.ReadCSVBodyFromUrl (SSRF + 500MiB cap)
            → ingestion.ProcessCSVToDigests / plugin CSV ingestors
                → container_usage_samples (partitioned, optional retention 45d)
                → daily_container_digests (partitioned, 6mo default)
            → services.runManifestRecommendations (after all files complete)
                → engine.RecommendWorkloadsStreaming → WriteContainerRecBatch
                → engine.RefreshOrgMetadata (org_container_keys, org_recommendation_stats)
                → parallel: GPU classify + node recs (errgroup)
                → quota / cluster-quota / PVC / VM / snapshot plugins
        → PostgreSQL commit → kafka.CommitMessage

UI / IQE
    → ros-ocp api (cmd/start api)
        → middleware: Identity → Entitlement → RBAC (cached)
        → model.getNativeRecommendationsFromOrgKeys (default list)
        → enrichment_cache (Masu rates per request)
        → fleetsummary LRU (5m TTL)
```

**Process split (each is a separate Deployment in production):**

| Command | Role | Hot resources |
|---------|------|---------------|
| `rosocp start processor` | Kafka consumer + ingest + recommend | CPU, DB writes, network (CSV) |
| `rosocp start api` | REST + `/metrics` on `PROMETHEUS_PORT` | DB reads, JSON encode, RBAC HTTP |
| `rosocp start recommendation-poller` | Kruize path only (`RECOMMENDATION_TOPIC`) | Kruize HTTP |
| `rosocp start housekeeper` | Partition retention, Sources cleanup | DB DDL/DML bursts |

Native engine is default (`ROS_USE_NATIVE_ENGINE=true`, Kruize gated by `ROS_ENABLED_PLUGINS=kruize`). On-prem deployments should run **without** Kruize for best performance.

---

## A. Database Layer

### pgxpool configuration

Defined in `internal/config/config.go` and applied in `internal/db/initPool()`:

| Setting | Env var | Default | Notes |
|---------|---------|---------|-------|
| Max connections | `ROS_DB_MAX_CONNS` | **5** | Legacy alias `DB_POOL_SIZE`. Comment in code: coordinate × replica count vs PostgreSQL `max_connections`. |
| Min connections | `ROS_DB_MIN_CONNS` | **2** | Keeps warm connections; increases baseline PG usage. |
| Max lifetime | `ROS_DB_MAX_CONN_LIFETIME` | **30 min** | Rotates connections; good for RDS failover. |
| Max idle time | `ROS_DB_MAX_CONN_IDLE_TIME` | **5 min** | |
| Acquire timeout | `ROS_DB_ACQUIRE_TIMEOUT_SECS` | **5s** | `ContextWithAcquireTimeout()` adds deadline when parent ctx has none. |
| Statement cache | `ROS_DB_STATEMENT_CACHE_MODE` | **`describe`** | Maps to `pgx.QueryExecModeCacheDescribe`. |

GORM uses `stdlib.OpenDBFromPool(pool)` with `SetMaxOpenConns(0)` — it does **not** create a second pool (`internal/db/initDB()`).

**AfterConnect hook** sets session `statement_timeout` on every new connection to `APIStatementTimeoutMS()` (default 25s from `ROS_DB_STATEMENT_TIMEOUT`).

**Observability:** `internal/metrics/pool_collector.go` exports `rosocp_db_pool_{total,acquired,idle,max}_conns` and acquire counters.

### Statement timeouts (three tiers)

| Tier | Function | Default | Used by |
|------|----------|---------|---------|
| API / GORM | `APIStatementTimeoutMS()` | 25000 ms | All pooled connections via `AfterConnect` |
| Heavy API | `HeavyAPIStatementTimeoutMS()` | 45000 ms | Savings summary, large fleet aggregates (`WithHeavyGORMStatementTimeout`) |
| Ingest | `IngestStatementTimeoutSecs()` | 120 s | `SET LOCAL` inside ingest transactions only |

SaaS ingress/gateway budget is ~30s — operators should set `ROS_HEAVY_API_STATEMENT_TIMEOUT_MS=28000` (documented in `statement_timeout.go`).

Cancellations increment `ros_api_statement_timeout_cancellations_total`.

### Query patterns and indexes

**Strengths:**

- **Default container list** uses `org_container_keys` + keyset seek on `idx_ock_org_sorted` (`internal/model/recommendation_set_native.go` → `getNativeRecommendationsFromOrgKeys`). Avoids joining all term/engine rows for pagination.
- **Recommendation writes** use `pgx.Batch` in chunks of **500** (`maxPgxBatchQueue` in `recommend_all.go` and `pipeline.go`).
- **Digest reads for recommend** use a single ordered query with partition pruning on `bucket_date` (`RecommendWorkloadsStreaming` SQL).
- **Keyset indexes** added in migrations `000078`, `000134`, `000139` for container, quota, VM, snapshot lists.

**Weaknesses / cliffs:**

- **`getNativeRecommendationsDistinct`** still used when `filter[stale]=true` — `DISTINCT ON (rs.cluster_uuid, rs.namespace, …)` over `recommendation_sets`. Expensive at scale.
- **Namespace lists** now route through `org_namespace_keys` for non-stale queries (migration 000153), matching the container keys pattern. `DISTINCT ON` fallback remains for `filter[stale]=only` queries.
- **Tag filters** force joins through `org_container_keys.resolved_tags` (GIN index `idx_ock_tags` exists, but JSONB containment still adds cost).
- **No `COPY FROM`** anywhere in `internal/` — all bulk loads are batched INSERT/UPSERT. Correct for moderate batch sizes; slower than COPY for massive backfills.

### N+1 and fan-out

| Path | Pattern | Severity |
|------|---------|----------|
| Container list enrichment | Request-scoped `enrichment_cache.go` dedupes Masu calls | ✅ Fixed |
| GPU list enrichment | Page-scoped `QueryGPURecommendationsForContainers` (phase13) | ✅ Fixed |
| Savings recalc | `pgx.Batch` chunk 500 per rec type (phase13) | ✅ Fixed |
| Tag sync | Single `unnest` UPDATE (phase13) | ✅ Fixed |
| Threshold recalc | `RecalculateThresholdsForOrg` loops clusters with concurrency 3 | ⚠️ Bounded but long for 50+ clusters |
| Manifest recommend | Sequential plugin runs after container recs | ⚠️ VM/PVC/snapshot add wall time |

### Partition design

Monthly RANGE partitions on `usage_start` / `bucket_date` for:

- `container_usage_samples` (retention `ROS_SAMPLE_RETENTION_DAYS=45`)
- `daily_container_digests`, `daily_namespace_digests`, GPU/node digests
- `recommendation_history`, `recommendation_quality`

Startup pre-creates current + next month (`EnsureIngestPartitionsAtStartup`, `EnsureRecommendationPartitionsAtStartup`). Historical ingest creates partitions on demand (`EnsureSamplePartitions`, `EnsureDigestPartitions`).

Sample partitions get aggressive autovacuum reloptions: `autovacuum_vacuum_scale_factor=0.05`, `fillfactor=85` (`pipeline_stream.go`).

### VACUUM / write pressure

High churn tables:

- `container_usage_samples` — hourly upserts during ingest; 45-day retention drop via housekeeper
- `recommendation_sets` — upsert every manifest per container × term × engine
- `recommendation_history` — append-only analytics (90-day retention)

Ingest uses **multi-row transactions**; a single large CSV can hold a transaction open for up to **120s**, blocking autovacuum on touched partitions.

**Risk:** On-prem demo PostgreSQL (512Mi, `work_mem=4MB` in chart defaults) will spill sorts to disk on digest aggregation and list queries. ROS does not set `work_mem` — DBA must tune the shared Koku+ROS instance.

### Connection contention

During manifest processing, one processor holds connections for:

1. Ingest transactions (samples + digests)
2. Streaming recommend read cursor
3. Batch write transaction(s)
4. `RefreshOrgMetadata` (refresh materialized keys)
5. Parallel GPU + node errgroup goroutines

With `ROS_DB_MAX_CONNS=5`, overlapping phases can exhaust the pool — especially if API pods on the same database spike during ingest.

**Rule of thumb:** `ROS_DB_MAX_CONNS × (ros-api replicas + ros-processor replicas + housekeeper) ≤ 0.7 × PostgreSQL max_connections`, leaving headroom for Koku masu/API on shared on-prem DB.

---

## B. Kafka Consumer

### Configuration

| Setting | Default | Source |
|---------|---------|--------|
| Consumer group | `ros-ocp` | `KAFKA_CONSUMER_GROUP_ID` |
| Auto commit | **false** | `KAFKA_AUTO_COMMIT` |
| Parallel workers | **3** | `ROS_KAFKA_WORKERS` when `ROS_KAFKA_PARALLEL=true` |
| Session timeout | 120000 ms | Hardcoded in `StartConsumer` |
| Heartbeat interval | 30000 ms | Hardcoded |
| Max transient retries | 5 | `ROS_KAFKA_MAX_TRANSIENT_RETRIES` |
| DLQ topic | `hccm.ros.events.dlq` | `ROS_KAFKA_DLQ_TOPIC` |
| Shutdown drain | 30s | `ROS_SHUTDOWN_TIMEOUT_SECONDS` |

### Commit strategy

**At-least-once** with manual commit after successful processing (`ProcessReport` lines 601–614):

- Poison messages (invalid JSON, validation failure): commit immediately to skip
- Transient errors (DB, context cancel): **reproduce to same topic** with `X-Retry-Count` header, then commit original offset
- After max retries: produce to **DLQ**, commit
- Success: `kafka.CommitMessage` (serialized by `parallelCommitMu` when parallel mode on)

This avoids stuck partitions on bad data while allowing retry on infra failures. Duplicate processing is possible if commit fails after work — idempotent upserts mitigate.

### Parallelism model

`consumeMessagesParallelUntilCancelled`:

- One goroutine reads from Kafka → buffered channel (`workers*2`)
- N worker goroutines process messages
- **Per-partition mutex** ensures ordering within a partition (ADR-0154)
- **Cross-partition** messages run concurrently (up to 3)

**Implication:** Throughput scales with **Kafka partition count × processor replicas**, not worker count alone. If the topic has 1 partition, workers provide no parallelism.

### Backpressure

- No explicit pause/resume API
- Slow handlers block the partition lock → consumer lag grows
- Job channel buffer = `workers*2` — reader blocks on full buffer only briefly
- **No built-in lag metric** — operators must use Kafka broker JMX / Strimzi lag alerts

### Message processing budget

`ProcessReport` wraps the full pipeline in `rosocp_pipeline_total_duration_seconds`. Phases tracked:

`download` → `parse_digest` → `write_digests` → `recommend` → `write_recommendations` → `post_process` → `metadata_refresh`

Large clusters: recommend phase dominates (CPU + DB read). VM recommendations deferred to manifest completion (`runManifestRecommendations`) — good for correctness, adds tail latency.

**Synth manifest debouncing:** Synthesized manifest IDs defer recommendations by `ROS_SYNTH_MANIFEST_QUIET_PERIOD=30s` (`manifest_recommendation_debouncer.go`), coalescing rapid file arrivals.

---

## C. Recommendation Engine

### Memory model

`RecommendWorkloadsStreaming` holds:

- Current container's digest slice (≤90 days × 1 row/day)
- Batch buffer: `streamBatchSize=500` containers × up to **6 recs/container** (3 terms × 2 engines) ≈ 3000 `ContainerRec` structs before flush

Peak memory **O(500 × terms × engines)**, not O(all containers). `RecommendAllWorkloads` wrapper defeats this — production uses streaming emit callback only.

### CPU hotspots

| Operation | Location | Cost |
|-----------|----------|------|
| Percentile sort at ingest | `ComputeDigest` — `slices.Sort` O(n log n) per digest group | Paid once at ingest, not recommend |
| Weighted percentiles | `ComputeContainerDigestWeighted` + `sync.Pool` scratch | Optimized for BH weighting |
| Decay weights | `decay_table.go` lookup | Avoids `math.Exp` on hot path |
| CPU+memory recommend | `RecommendCPUAndMemory` fused pass | Single walk over window rows |
| Window slice | `windowBounds` binary search | Zero-copy subslice |
| Idle classification | `ClassifyIdleState` per container | Extra pass over idle window rows |
| Node consolidation | `recommend_nodes.go` | EMA + percentile over node digests |

### Goroutines

- Post-container: `errgroup` for GPU classify + node recs (2 parallel DB-heavy tasks)
- Threshold/savings recalc: worker pool size `thresholdRecalcConcurrency()` default **3**
- Kafka consumer workers: **3**
- Async jobs: `asyncjobs.Init(ctx, 30s)` on API startup for background tasks

No global worker pool for recommendations — each manifest runs synchronously in the Kafka handler (until commit).

### Batch tuning

| Constant | Value | File |
|----------|-------|------|
| `streamBatchSize` | 500 | `recommend_all.go` |
| `maxPgxBatchQueue` | 500 | `recommend_all.go`, `pipeline.go` |
| `ingestSingleTxRowThreshold` | 25000 | `pipeline.go` |
| `ingestSingleTxGroupThreshold` | 5000 | `pipeline.go` |
| `streamSampleFlushRows` | 1000 | `pipeline_stream.go` |
| `ROS_INGEST_FLUSH_BATCH_SIZE` | 1000 | config default |

Single-transaction ingest fast path (`commitIngestInSingleTx`) avoids multiple round-trips for small CSVs. Above thresholds, phases commit separately.

### Read-once-compute-N-terms

For each container, the engine:

1. Loads all digest rows once (ordered query)
2. For each term config (`LoadTermConfigCached`, 60s TTL): slices window via `windowBounds`
3. For each engine profile (`cost`, `performance`): runs fused CPU+memory recommend
4. Emits to batch writer

Typical: 3 terms × 2 engines = **6 recommendation rows per container**. Notification evaluation is inline (`EvaluateNotificationsWithThresholds`).

### GC pressure

- `ContainerRec` structs allocated per term×engine — unavoidable
- `sync.Pool` for weighted digest scratch buffers (`digest.go`)
- Integer `int64` data plane throughout (`DigestRow` in engine types)
- Large CSV parsing allocates `MetricRow` slices — streaming parser reduces peak vs loading entire file

---

## D. API Layer

### Server stack (`internal/api/server.go`)

- **Echo** + `echoprometheus` middleware (route template as `url` label — bounded cardinality)
- **Gzip** level 5, min 1024 bytes
- **No HTTP/2 push, no response cache** at HTTP layer (app-level LRU only)
- Separate Echo instance for Prometheus on `PROMETHEUS_PORT` (default 5005)
- `READ_HEADER_TIMEOUT` default 15s (config)

### Request path latency breakdown (typical container list)

1. Identity parse (once, context) — microseconds
2. RBAC HTTP call — **0–60ms** (cached 60s in `rbac_cache.go`)
3. Count query on `org_container_keys` — milliseconds to seconds (org size)
4. Page query + join to `recommendation_sets` for sort column — depends on limit (default pagination limit from list options)
5. Enrichment (BH, GPU if enabled) — **page-scoped** queries
6. JSON marshal — `encoding/json` (benchmarked <10% gain vs jsoniter, deliberately kept)

Heavy endpoints use `WithHeavyGORMStatementTimeout` / `WithHeavyStatementTimeout`:

- `handlers_savings_summary.go`
- Large fleet container lists (`recommendation_set_native.go`)

### Pagination

- **Keyset / cursor** pagination with tuple tie-breakers (`pagination_keyset.go`, ADR-0190)
- **Offset cap:** `ROS_API_MAX_OFFSET=10000` — prevents deep offset scans
- Default container path: **`org_container_keys`** — designed for keyset
- Namespace path: still **DISTINCT ON** — offset/keyset less efficient

### Serialization

Slim list DTOs (`internal/model/list_response.go`) reduce JSON size. Plot data on detail endpoints uses digest-based boxplots (`boxplot.go`, ADR-0292) — no raw sample reads.

### Middleware cost

| Middleware | Cost | Notes |
|------------|------|-------|
| Identity | Low | Base64 decode + JSON |
| Entitlement | Low | Context check |
| RBAC | Medium | HTTP to RBAC service; LRU cache 500 entries, 60s TTL |
| Gzip | CPU on large responses | |

### Rate limiting

**None implemented.** A malicious or buggy client can hammer list endpoints and exhaust PostgreSQL connections on API pods. Mitigation is external (OpenShift route rate limits, API gateway).

### Connection keep-alive

API uses PostgreSQL pool only (no outbound pool for most read paths except RBAC/Masu on enrichment). `httpclient.SharedTransport()` reused for outbound calls.

---

## E. Caching

| Cache | Location | Max entries | TTL | Invalidation |
|-------|----------|-------------|-----|--------------|
| RBAC permissions | `middleware/rbac_cache.go` | 500 (`ROS_RBAC_CACHE_MAX_ENTRIES`) | 60s | Expiry only |
| Fleet summary | `fleetsummary/cache.go` | 256 (`ROS_FLEET_SUMMARY_CACHE_CAPACITY`) | 300s | `InvalidateOrg` on rec refresh |
| Savings summary | same package | 256 | 300s | `InvalidateOrg` |
| Cost effective rates | `costdata/lru_cache.go` | 1000 (`ROS_COST_CACHE_MAX_ENTRIES`) | (per-entry) | Prefix delete on org |
| Term config | `engine/term_config.go` | **Unbounded map** | 60s | `InvalidateTermCache` on settings PUT |

**Singleflight / coalescing:**

- `threshold_recalc_guard.go` — per `(org_id, rec_type)` mutex + pending flag
- `savings_recalc_guard.go` — same pattern
- Not using `golang.org/x/sync/singleflight` package; custom implementation

**Request-scoped cache:** `enrichment_cache.go` dedupes Masu `GetEffectiveRates` within one HTTP request.

**Hit ratio monitoring:** Prometheus counters `rosocp_fleet_summary_cache_{hits,misses}_total`, `rosocp_savings_summary_cache_*`, `rosocp_rbac_cache_size`, `rosocp_cost_cache_*`.

**Risk:** Term config cache grows with unique `(org, rec_type)` pairs — bounded by tenant count, not LRU-evicted. Long-lived API pods serving thousands of tenants could accumulate entries (each small).

---

## F. CSV Ingestion

### Download path

`utils.ReadCSVBodyFromUrl` → `getCSVHTTPResponse`:

- **SSRF:** host allowlist `ROS_CSV_ALLOWED_HOSTS` (fail-closed in prod), private network deny with DNS resolution (2s timeout)
- **Size cap:** `ROS_CSV_MAX_BODY_BYTES=524288000` (500 MiB)
- **Timeout:** `ROS_CSV_DOWNLOAD_TIMEOUT_SECS=120`
- **Redirects disabled**
- **Shared transport:** `MaxIdleConns=100`, `MaxIdleConnsPerHost=10`, idle 90s

### Parse performance

- Standard library `encoding/csv` via ingestion parsers
- **Streaming path:** `ProcessCSVToDigestsStream` groups rows by digest key in memory; flushes when `ROS_INGEST_FLUSH_BATCH_SIZE` groups accumulated
- **Percentiles at ingest:** `ComputeContainerDigestWeighted` — sorting pooled buffers
- Legacy Kruize path still uses `gota/dataframe` for some workloads — heavier; disabled when native engine owns ingest

### Batch writes

All via `pgx.Batch` queue capped at 500 statements per round trip. Ingest transactions set `SET LOCAL statement_timeout` to 120s.

**No COPY FROM** — opportunity for future optimization on sample bulk load.

### Memory pressure

`rosocp_ingest_groups_in_memory` gauge tracks digest groups during streaming. Incremental flush prevents unbounded growth except for pathological single-key streams.

---

## G. Memory Management

### Runtime configuration

- `import _ "go.uber.org/automaxprocs"` in `cmd/start.go` — GOMAXPROCS from cgroup quota
- **`GOMEMLIMIT`:** not set in Dockerfile; injected by Helm (`ros.goMemLimit` ~90% of container memory limit per `docs-site/configuration.md`)
- **`GOGC`:** not tuned in code; default 100

### Dockerfile

Multi-stage build: `go build -ldflags="-s -w"` → ~52 MiB binary (per native-engine-audit-v2). Runtime: `ubi9/ubi-minimal`, USER 1001, no shell debugging tools.

### Health checks

`/healthz` (`internal/health/healthz.go`):

- Goroutine count vs `ROS_HEALTHZ_MAX_GOROUTINES=5000`
- Last GC pause vs `ROS_HEALTHZ_MAX_GC_PAUSE_MS=100`
- Scheduler canary (500ms)

`/readyz`: DB ping; optional Kafka/S3 when `ROS_READINESS_CHECK_KAFKA/S3=true` (off by default for API-only).

### Leak risks

- `termConfigCache` map without eviction (low severity)
- Synth manifest debouncer entries cleaned on fire/shutdown
- Kafka consumer goroutines drained on SIGTERM with timeout

### Object pooling

- `weightedDigestScratchPool` in digest computation
- `fieldBufferPool`, `weightBufferPool` in digest.go

---

## H. Concurrency Model

| Mechanism | Usage |
|-----------|-------|
| `sync.Mutex` | RBAC/fleet/cost LRU caches; partition locks in Kafka consumer; debouncer state |
| `sync.Map` | Partition locks, synth debouncers, threshold recalc flights |
| `sync.WaitGroup` | Kafka in-flight handler drain; parallel consumer workers |
| `errgroup` | GPU + node post-processing; some test helpers |
| `context.Context` | Propagated from `signal.NotifyContext` on startup; ingest checks `ctx.Err()` |
| Channel buffer | Kafka jobs chan `workers*2` |

**Cancellation:** Processor respects SIGTERM — stops reading, drains handlers up to `ROS_SHUTDOWN_TIMEOUT_SECONDS`, then closes consumer (30s grace for `consumer.Close()`).

**Contention points:**

- `parallelCommitMu` — serializes all Kafka commits in parallel mode
- Per-partition mutex — serializes manifest processing per partition
- PostgreSQL row-level locks on hot upsert keys during concurrent recommends (same cluster, different manifests rare)

---

## Scaling Characteristics

### What scales linearly

- **Kafka partitions × processor replicas** (consumer group rebalancing)
- **API replicas** for read traffic (stateless; pool per pod)
- **PostgreSQL read IOPS** for list queries with proper indexes and `work_mem`

### What hits cliffs

| Scale trigger | First failure mode |
|---------------|-------------------|
| 1 partition, high ingest rate | Single-threaded partition processing |
| 5 conn × 20 API pods = 100 PG conns | `too many connections` on shared on-prem DB |
| 20k containers, 3 terms, 2 engines | Recommend phase 60–120s+; Kafka lag |
| Namespace list at 5k namespaces | DISTINCT ON sort spill |
| Cost model change, 30 clusters | Savings recalc minutes-long |
| CSV 500 MiB | Download memory if not streamed; timeout at 120s |

### Horizontal scaling limits

- **Processor:** Must increase Kafka topic partitions before adding processor pods
- **API:** Limited by PostgreSQL connection budget and shared cache coldness per pod
- **Database:** Single ROS database (not sharded by org); all tenants share one PostgreSQL instance on-prem

---

## On-Prem vs SaaS (ROS-specific)

| Aspect | On-prem | SaaS |
|--------|---------|------|
| Engine | Native default; Kruize disabled | Native default; Kruize legacy tenants |
| Config source | Helm env vars | Clowder AppConfig |
| RBAC | Often disabled locally (`RBAC_ENABLE=false`) | Enabled; cache critical |
| PostgreSQL | Shared with Koku on one instance | Dedicated ROS RDS; still connection-limited |
| Ingress timeout | Route/ingress configurable | ~30s gateway → tune heavy API timeout 28s |
| CSV URLs | MinIO internal URLs on allowlist | S3 presigned URLs |
| Savings / Masu | `KOKU_MASU_URL` to internal masu | Same pattern, higher latency variance |
| Metrics | Prometheus scrape `:5005/metrics` | Clowder Prometheus port |
| `GOMEMLIMIT` | Helm `ros.goMemLimit` | Clowder memory limit injection |

---

## Priority Recommendations

### P0 — Do before large production fleets

| ID | Recommendation | Effort | Evidence |
|----|----------------|--------|----------|
| P0-1 | Size PostgreSQL using Medium/Large profile (≥4Gi, `work_mem≥16MB`, `max_connections≥200`) on shared on-prem node | S (ops) | List + digest queries spill at 4MB `work_mem`; chart defaults are demo-only |
| P0-2 | Set `GOMEMLIMIT≈0.9×` container memory limit on **all** ROS deployments | S (ops) | Documented in `docs-site/configuration.md`; prevents OOMKill during recommend spikes |
| P0-3 | Budget `ROS_DB_MAX_CONNS × replicas` ≤ 70% of PG `max_connections` | S (ops) | `defaultDBMaxConns=5` in `config.go`; pool metrics available |
| P0-4 | Ensure Kafka topic `hccm.ros.events` has **≥ processor replica count** partitions | S (ops) | Parallel workers useless with 1 partition |

### P1 — High impact code/ops

| ID | Recommendation | Effort | Evidence |
|----|----------------|--------|----------|
| P1-1 | ~~Add **`org_namespace_keys`** materialized table mirroring container pattern~~ **DONE** (migration 000153) | L (5d) | `namespace_recommendation_set_native.go` DISTINCT ON |
| P1-2 | Expose **Kafka consumer lag** metric or document Strimzi alert | S (1d) | No lag metric in `internal/metrics` |
| P1-3 | Set `ROS_HEAVY_API_STATEMENT_TIMEOUT_MS=28000` in SaaS | S (ops) | `HeavyAPIStatementTimeoutMS` comment |
| P1-4 | Raise processor CPU limit before memory for large clusters | S (ops) | Recommend phase is CPU-bound integer math |
| P1-5 | Bound **`termConfigCache`** with LRU or periodic sweep | S (2d) | Unbounded map in `term_config.go` |

### P2 — Optimization backlog

| ID | Recommendation | Effort | Evidence |
|----|----------------|--------|----------|
| P2-1 | **`COPY FROM`** for `container_usage_samples` bulk insert | M (3d) | No CopyFrom in `internal/`; pgx supports it |
| P2-2 | API **rate limiting** middleware (per org_id token bucket) | M (3d) | No rate limit in `server.go` |
| P2-3 | PGO build in CI | L | Deferred in native-engine-audit; binary 52MiB |
| P2-4 | Separate read replica for API Deployment | L (ops) | All reads hit primary today |
| P2-5 | Parallel manifest file download (bounded errgroup) | M | Sequential file loop in `ProcessReport` |

---

## SLI / SLO Recommendations

| SLI | Measurement | Suggested target (on-prem Medium) | Source |
|-----|-------------|-----------------------------------|--------|
| Ingest success rate | `rosocp_pipeline_total_duration_seconds{status="success"}` / total | ≥99% | `metrics.go` |
| Ingest latency (p95) | Pipeline histogram | <10 min per manifest (5k containers) | Phase histograms |
| Recommend latency (p95) | `rosocp_recommendation_duration_seconds{type="container"}` | <120s per cluster | Engine metrics |
| API availability | `/readyz` success | 99.9% | K8s probes |
| API latency (p95) | `rosocp_echo_request_duration_seconds` | List <2s; detail <1s | Echo prometheus |
| DB pool health | `rosocp_db_pool_acquired_conns / rosocp_db_pool_max_conns` | <80% sustained | `pool_collector.go` |
| Statement timeout rate | `ros_api_statement_timeout_cancellations_total` | <0.1% of queries | `statement_timeout.go` |
| Kafka lag | External broker metric | <1000 messages/processor | Not native — add alert |
| Cache effectiveness | `fleet_summary_cache_hits / (hits+misses)` | >60% after warm-up | `fleetsummary/cache.go` |

**Error budget consumers:** Kafka DLQ depth (`rosocp_kafka_dlq_messages_total`), analytics incomplete flag (`analytics_incomplete` on API when `ROS_INGEST_STRICT_ANALYTICS=false`).

---

## Risk Register

| ID | Risk | Likelihood | Impact | Mitigation |
|----|------|------------|--------|------------|
| R1 | Shared on-prem PostgreSQL exhausted by Koku + ROS connections | High | Outage | Connection budgeting; PgBouncer; separate ROS DB |
| R2 | Single Kafka partition limits ingest throughput | Med | Lag | Increase partitions before scaling processors |
| R3 | ~~Namespace DISTINCT ON degrades UI namespace tab~~ **Mitigated** by P1-1 | Med | Timeouts | P1-1 org_namespace_keys (implemented) |
| R4 | Large CSV + strict analytics blocks commit on history failure | Low | Lag | `ROS_INGEST_STRICT_ANALYTICS=false` for degraded mode |
| R5 | No API rate limit → DB exhaustion | Med | API outage | Ingress rate limit; P2-2 |
| R6 | Term config cache unbounded growth | Low | Slow memory creep | P1-5 |
| R7 | SaaS ingress timeout on savings summary | Med | 504 errors | `ROS_HEAVY_API_STATEMENT_TIMEOUT_MS=28000` |
| R8 | Savings recalc during business hours | Med | DB spike | Coalescing exists; schedule off-peak masu webhooks |
| R9 | Poison messages misclassified as transient | Low | DLQ noise | Monitor DLQ; tune `isTransientKafkaProcessingError` |
| R10 | `filter[stale]=true` on large org | Med | List timeout | Document as admin-only; add index covering stale+sort |

---

## Related Internal Documents

- [`docs/performance/native-engine-audit-v2-2026-06.md`](../performance/native-engine-audit-v2-2026-06.md) — Phase13 regression audit
- [`docs/operations/configuration.md`](../operations/configuration.md) — Full env var reference
- [`docs-site/operations/performance-engineering-guide.md`](../../docs-site/operations/performance-engineering-guide.md) — Operator-facing tuning guide

---

*This audit is based on static code review of `ros-ocp-backend` at HEAD on branch `pgarciaq-rosocp-superpowers-phase13`. Validate SLO targets with production Prometheus data before contractual commitments.*
