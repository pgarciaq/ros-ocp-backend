# Configuration Reference

Complete environment variable reference for ROS-OCP Backend. Values are loaded
via [Viper](https://github.com/spf13/viper) from process environment (and
`.env` in local development). In Red Hat OpenShift / Clowder deployments, many
connection settings are injected automatically from the platform.

For algorithm-specific thresholds (container percentiles, GPU classification,
snapshot staleness, etc.), see [Configurability Reference](../architecture/configurability.md).
This document focuses on **platform wiring**, **performance tuning**, and
**operational controls**.

**Last updated:** 2026-05-25

---

## Configuration Precedence (Thresholds)

Recommendation thresholds follow a three-tier model documented in
[configurability.md](../architecture/configurability.md):

1. **Admin env var** (`ROS_*`) — locks the field platform-wide
2. **Tenant Settings API** — per-`org_id` overrides when not locked
3. **Compiled default** — hardcoded fallback in engine/plugin code

Performance env vars in this document are **platform-wide only** (not
tenant-configurable).

---

## Performance Tuning (Batches 1–3)

These variables were added or formalized during the native-engine performance
optimization work (typed columns, parallel ingestion, connection pooling, RBAC
caching, threshold recalc fan-out, reship concurrency).

| Variable | Default | Purpose |
|----------|---------|---------|
| `ROS_KAFKA_PARALLEL` | `true` | Enable parallel Kafka message processing. When `true` and `ROS_KAFKA_WORKERS` > 1, messages are dispatched to a worker pool. Partition-level mutexes preserve ordering within a partition. |
| `ROS_KAFKA_WORKERS` | `3` | Number of Kafka worker goroutines when parallel mode is enabled. Increase for CPU-bound clusters with low per-message I/O; decrease if DB pool is saturated. |
| `ROS_RBAC_CACHE_TTL` | `60` (seconds) | In-memory TTL for RBAC permission lookups keyed by `X-Rh-Identity`. Set `0` to disable caching (every API request hits RBAC). |
| `ROS_THRESHOLD_RECALC_CONCURRENCY` | `3` | Max parallel clusters during async threshold recalculation after Settings API PUT. |
| `ROS_DB_MIN_CONNS` | `2` | pgxpool minimum idle connections kept open. |
| `ROS_DB_MAX_CONN_LIFETIME` | `30` (minutes) | Maximum lifetime of a pooled connection before recycle. |
| `ROS_DB_MAX_CONN_IDLE_TIME` | `5` (minutes) | Maximum idle time before a connection is closed. |
| `ROS_DB_STATEMENT_CACHE_MODE` | `describe` | pgx prepared-statement cache mode (`describe`, `prepare`, or `describe_exec`). `describe` avoids server-side prepare overhead for ad-hoc queries. |
| `ROS_RESHIP_CONCURRENCY` | `2` | Parallel masu `reship_ros` calls per org during business-hours backfill. Coordinate with masu rate limits when raising. |

Related database pool settings (pre-existing, often tuned together):

| Variable | Default | Purpose |
|----------|---------|---------|
| `ROS_DB_MAX_CONNS` | `10` | pgxpool maximum connections per process (API, processor, poller each have their own pool). |
| `ROS_DB_ACQUIRE_TIMEOUT_SECS` | `5` | Max wait when acquiring a connection from the pool. `0` = unlimited wait. |

---

## Application

| Variable | Default | Purpose |
|----------|---------|---------|
| `SERVICE_NAME` | `rosocp` | Service identifier in structured logs (`service` field). |
| `LOG_LEVEL` | `INFO` | Log verbosity: `DEBUG`, `INFO`, `ERROR`. |
| `LogFormater` | `text` (local), JSON (Clowder) | Log output format. |
| `API_PORT` | `8000` | REST API listener port. |
| `PROMETHEUS_PORT` | `5005` (local), `9000` (Clowder) | Metrics and probe port (processor/poller); API also runs a separate metrics listener on this port. |
| `READ_HEADER_TIMEOUT` | `15` (seconds) | HTTP read-header timeout for API server. |
| `RECORD_LIMIT_CSV` | `1000` | Max CSV rows per export batch. |
| `CSV_STREAM_INTERVAL` | `100` | Rows between CSV stream flush intervals. |
| `MAXIMUM_COUNT_PER_QUERY_PARAM` | `5` | Max values allowed per repeated query parameter. |
| `GLOBAL_HTTP_CLIENT_TIMEOUT_SECS` | `30` | Default timeout for outbound HTTP clients (RBAC, masu, sources). |
| `RECOMMENDATION_POLL_INTERVAL_HOURS` | `24` | Legacy Kruize poller interval (hours). |
| `DATA_RETENTION_PERIOD` | `15` | Legacy data retention period (days). |

---

## Database

Connection parameters. In Clowder, these are set from the platform database binding.

| Variable | Default (local) | Purpose |
|----------|-----------------|---------|
| `DB_HOST` | `localhost` | PostgreSQL host. |
| `DB_PORT` | `15432` | PostgreSQL port. |
| `DB_NAME` | `postgres` | Database name. |
| `DB_USER` | `postgres` | Database user. |
| `DB_PASSWORD` | `postgres` | Database password. |
| `DB_SSL` | `disable` | SSL mode (`disable`, `require`, `verify-full`, etc.). |
| `DB_CA_CERT` | (empty) | Path to CA certificate for TLS verification. |

See **Performance Tuning** above for `ROS_DB_*` pool variables.

---

## Kafka

| Variable | Default (local) | Purpose |
|----------|-----------------|---------|
| `KAFKA_BOOTSTRAP_SERVERS` | `localhost:29092` | Broker list (comma-separated). |
| `KAFKA_CONSUMER_GROUP_ID` | `ros-ocp` | Consumer group for processor and poller. |
| `KAFKA_AUTO_COMMIT` | `false` | Auto-commit offsets. `false` = manual commit after successful processing (recommended). |
| `UPLOAD_TOPIC` | `hccm.ros.events` | Topic for cluster upload events (processor). |
| `RECOMMENDATION_TOPIC` | `rosocp.kruize.recommendations` | Topic for Kruize recommendation requests (legacy poller). |
| `SOURCES_EVENT_TOPIC` | `platform.sources.event-stream` | Platform Sources lifecycle events. |

SASL/TLS variables (`KafkaUsername`, `KafkaPassword`, `KafkaSASLMechanism`,
`KafkaSecurityProtocol`, `KafkaCA`) are injected by Clowder when Kafka auth is
enabled — not set manually in production.

See **Performance Tuning** for `ROS_KAFKA_PARALLEL` and `ROS_KAFKA_WORKERS`.

---

## RBAC

| Variable | Default (local) | Purpose |
|----------|---------|---------|
| `RBAC_ENABLE` | `false` (local), `true` (Clowder) | Enable RBAC middleware on API routes. |
| `RBACHost` | `localhost` | RBAC service hostname. |
| `RBACPort` | `9080` | RBAC service port. |
| `RBACProtocol` | `http` | RBAC URL scheme. |

See **Performance Tuning** for `ROS_RBAC_CACHE_TTL`.

---

## Koku / Masu Integration

| Variable | Default | Purpose |
|----------|---------|---------|
| `KOKU_MASU_URL` | (empty) | Base URL for Koku masu API (savings estimates, business-hours reship). |
| `ROS_SAVINGS_ESTIMATES_ENABLED` | `true` | Fetch effective rates from masu for dollar savings fields. |
| `ROS_RESHIP_POLLER_INTERVAL_SECS` | `60` | Background reship retry interval (seconds). |
| `ROS_RESHIP_MAX_RETRIES` | `10` | Consecutive reship failures before marking exhausted. |
| `ROS_BUSINESS_HOURS_RESHIP_FORWARD_ONLY_FALLBACK` | `false` | After max retries, fall back to forward-only BH recommendations. |

See **Performance Tuning** for `ROS_RESHIP_CONCURRENCY`.

---

## Kruize (Legacy)

| Variable | Default | Purpose |
|----------|---------|---------|
| `KRUIZE_URL` | `http://localhost:8080` | Kruize HTTP endpoint. |
| `KRUIZE_WAIT_TIME` | `30` | Seconds to wait for Kruize experiment results. |
| `KRUIZE_MAX_BULK_CHUNK_SIZE` | `100` | Max experiments per bulk API call. |
| `KRUIZE_PERFORMANCE_PROFILE_VERSION` | `v2.0` | Performance profile version sent to Kruize. |
| `ROS_USE_NATIVE_ENGINE` | `true` | **Deprecated.** Use `ROS_ENABLED_PLUGINS=kruize` instead. When `false`, forces Kruize-only mode. |

---

## Feature Flags and Plugins

| Variable | Default | Purpose |
|----------|---------|---------|
| `ROS_ENABLED_PLUGINS` | (empty = all) | Comma-separated allowlist: `container`, `namespace`, `node`, `gpu`, `pvc`, `snapshot`, `kruize`. |
| `ROS_DISABLED_PLUGINS` | (empty) | Comma-separated blocklist (applied after allowlist). |
| `ROS_BUSINESS_HOURS_ENABLED` | `true` | Business-hours routes, ingestion dual-stream, reship poller. |
| `ROS_THRESHOLD_RECALCULATION_ENABLED` | `true` | Async recalc when tenant threshold settings change. |
| `DISABLE_NAMESPACE_RECOMMENDATION` | `false` | Legacy flag to disable namespace recommendations. |

Unleash (feature flags) — configured by Clowder in SaaS; local defaults:

| Variable | Default | Purpose |
|----------|---------|---------|
| `UnleashClientAccessToken` | `rosocp:dev.token` | Unleash API token. |
| `UnleashHostname` | `0.0.0.0` | Unleash host. |
| `UnleashPort` | `3063` | Unleash port. |
| `UnleashScheme` | `http` | Unleash URL scheme. |

---

## Retention and Staleness

| Variable | Default | Purpose |
|----------|---------|---------|
| `ROS_RETENTION_MONTHS` | `6` | Monthly digest partition retention. |
| `ROS_HISTORY_RETENTION_DAYS` | `90` | Historical recommendation archive retention. |
| `ROS_STALENESS_THRESHOLD_HOURS` | `72` | Hours without cluster report before recommendations marked stale. |
| `ROS_STALE_ARCHIVE_DAYS` | `30` | Delete stale recommendations older than N days. |
| `ROS_MAX_LOOKBACK_DAYS` | `90` | Max digest lookback for recommendation queries. |

---

## Sources API

| Variable | Default | Purpose |
|----------|---------|---------|
| `SOURCES_API_BASE_URL` | `http://127.0.0.1:8002` | Platform Sources API base URL. |
| `SOURCES_API_PREFIX` | `/api/sources/v3.1` | Sources API path prefix. |

---

## CloudWatch (SaaS)

| Variable | Default | Purpose |
|----------|---------|---------|
| `CW_LOG_STREAM_NAME` | `rosocp` | CloudWatch log stream name. |
| `CwLogGroup`, `CwRegion`, `CwAccessKey`, `CwSecretKey` | (Clowder) | CloudWatch credentials and destination. |

---

## Tag Sync

Resolved OpenShift tags from Koku are stored on `org_container_keys.resolved_tags` and
exposed for list filtering when tag sync is enabled.

| Variable | Default | Purpose |
|----------|---------|---------|
| `ROS_TAGS_ENABLED` | `false` | Master switch for tag sync push API and tag list filters. When `false`, the push endpoint returns 404 and tag query params are ignored. |
| `ROS_TAGS_ALLOWED_SERVICE_ACCOUNTS` | (empty) | Comma-separated Kubernetes ServiceAccount names allowed to call the push API. Empty accepts any authenticated cluster SA. |
| `ROS_TAGS_DEV_TOKEN` | (empty) | Dev-only bearer token fallback; logged as a warning when used. |

Koku pushes resolved namespace tags via `POST /api/cost-management/v1/internal/tags/sync`
using `Authorization: Bearer <service-account-token>`. Sync freshness is available at
`GET /api/cost-management/v1/internal/tags/status?org_id=<org_id>`.

**Authentication:** Kubernetes ServiceAccount token validation via TokenReview API.
See [tag-sync-auth.md](tag-sync-auth.md) for current auth and the planned **mTLS** upgrade.

**Data flow and lifecycle:** [tag-sync.md](tag-sync.md)

List filtering supports Koku syntax `?filter[tag:key]=value1,value2` (OR within a key,
AND across keys). See [features/tag-filtering.md](../features/tag-filtering.md).

---

## OOM Feedback

| Variable | Default | Purpose |
|----------|---------|---------|
| `ROS_OOM_BASE_BUMP` | `0.15` | Log-scaling factor for post-OOM memory bump. |
| `ROS_OOM_MAX_BUMP` | `1.60` | Maximum memory bump multiplier after OOM kills. |

---

## Engine Thresholds (Summary)

Platform-wide defaults for sizing and classification. Each can lock the
corresponding Settings API field when set. Full descriptions and tenant
override behavior: [configurability.md](../architecture/configurability.md).

### Container

`ROS_CONTAINER_CPU_COST_PERCENTILE`, `ROS_CONTAINER_CPU_PERF_PERCENTILE`,
`ROS_CONTAINER_MEM_COST_PERCENTILE`, `ROS_CONTAINER_MEM_PERF_PERCENTILE`,
`ROS_CONTAINER_MIN_MARGIN`, `ROS_CONTAINER_MAX_MARGIN`,
`ROS_CONTAINER_LIMIT_MULTIPLIER`, `ROS_CONTAINER_CPU_FLOOR_MC`,
`ROS_CONTAINER_IDLE_CPU_THRESHOLD_MC`, `ROS_CONTAINER_IDLE_MEM_THRESHOLD_KIB`,
`ROS_CONTAINER_MEM_TREND_SLOPE_THRESHOLD`, `ROS_CONTAINER_LOW_CONFIDENCE_THRESHOLD`

### Namespace

`ROS_NAMESPACE_*` — same shape as container (see configurability doc).

### Node

`ROS_NODE_UNDERUTIL_THRESHOLD`, `ROS_NODE_OVERCOMMIT_THRESHOLD`,
`ROS_NODE_ALLOCATABLE_FACTOR`, `ROS_NODE_STRANDED_IMBALANCE_THRESHOLD`,
`ROS_NODE_EMA_ALPHA`, `ROS_NODE_COST_TARGET_UTILIZATION`,
`ROS_NODE_PERF_TARGET_UTILIZATION`,
`ROS_NODE_PERF_CONSOLIDATION_HEADROOM_MULTIPLIER`, `ROS_NODE_TREND_MIN_DAYS`

### GPU

`ROS_GPU_IDLE_THRESHOLD`, `ROS_GPU_UNDERUTILIZED_SM_THRESHOLD`,
`ROS_GPU_UNDERUTILIZED_TENSOR_THRESHOLD`, `ROS_GPU_MEMBOUND_DRAM_THRESHOLD`,
`ROS_GPU_MEMBOUND_TENSOR_THRESHOLD`, `ROS_GPU_FB_HEADROOM_FACTOR`,
`ROS_GPU_COMPUTE_BOUND_DRAM_THRESHOLD`, `ROS_GPU_MIG_FB_PERCENTILE`,
`ROS_GPU_CONFIDENCE_DAYS_TIER1/2/3`, `ROS_GPU_SPIKE_RATIO_THRESHOLD`,
`ROS_GPU_SPIKE_CONFIDENCE_PENALTY`, `ROS_GPU_NO_PROFILING_CONFIDENCE_FACTOR`,
`ROS_GPU_TIMESLICING_*`, `ROS_GPU_NODE_FRESHNESS_DAYS`

### PVC

`ROS_PVC_OVERSIZED_THRESHOLD`, `ROS_PVC_NEAR_FULL_THRESHOLD`,
`ROS_PVC_MIN_TREND_DAYS`, `ROS_PVC_RECOMMENDED_SIZE_MULTIPLIER`,
`ROS_PVC_MIN_RECOMMENDED_GIB`, `ROS_PVC_DAYS_TO_FULL_ALERT`

### Snapshot

`ROS_SNAPSHOT_ORPHAN_AGE_DAYS`, `ROS_SNAPSHOT_NEVER_RESTORED_DAYS`,
`ROS_SNAPSHOT_STALE_DAYS`, `ROS_SNAPSHOT_REDUNDANT_THRESHOLD`,
`ROS_SNAPSHOT_COST_PER_GIB_MONTH`, `ROS_SNAPSHOT_INVENTORY_FRESH_HOURS`,
`ROS_SNAPSHOT_INVENTORY_RETENTION_HOURS`, `ROS_SNAPSHOT_STALE_GRACE_HOURS`

### Term windows (per plugin)

Format: `ROS_TERMS_<PLUGIN>_<TERM>_<FIELD>` where `<TERM>` is `SHORT`, `MEDIUM`,
or `LONG` and `<FIELD>` is `WINDOW_DAYS`, `MIN_DATA_DAYS`, or
`DECAY_HALFLIFE_HOURS`. Example: `ROS_TERMS_CONTAINER_LONG_WINDOW_DAYS=45`.

Implemented in [`term_config.go`](../../internal/engine/term_config.go).

---

## Tuning Guidelines

### Processor throughput

1. Start with defaults (`ROS_KAFKA_PARALLEL=true`, `ROS_KAFKA_WORKERS=3`).
2. If CPU is underutilized and `rate(rosocp_kafka_messages_processed_total[5m])`
   is below expected upstream volume, increase `ROS_KAFKA_WORKERS` (watch DB
   pool saturation via `rosocp_db_error_total` and query latency).
3. Ensure `ROS_DB_MAX_CONNS` ≥ workers + headroom for concurrent writes.

### API latency under RBAC load

1. Default `ROS_RBAC_CACHE_TTL=60` reduces RBAC HTTP round-trips per identity.
2. Lower TTL (e.g. `30`) if permission changes must propagate faster.
3. Set `0` only for debugging — every request hits RBAC.

### Threshold recalculation

1. `ROS_THRESHOLD_RECALC_CONCURRENCY` controls parallel cluster fan-out.
2. Clusters with unchanged threshold hash are skipped (`status=skipped` on
   `ros_threshold_recalculation_total`).
3. Disable entirely with `ROS_THRESHOLD_RECALCULATION_ENABLED=false` on
   constrained environments.

### Business-hours reship

1. `ROS_RESHIP_CONCURRENCY=2` balances masu load vs backfill speed.
2. Increase only after confirming masu can handle parallel `reship_ros` calls.

---

## Source

All defaults and validation logic: [`internal/config/config.go`](../../internal/config/config.go).

## Related Documentation

| Document | Scope |
|----------|-------|
| [Configurability Reference](../architecture/configurability.md) | Threshold semantics and Settings API |
| [Monitoring](monitoring.md) | Metrics tied to these settings |
| [Retention](retention.md) | Data lifecycle env vars |
| [RBAC](rbac.md) | Authorization configuration |
