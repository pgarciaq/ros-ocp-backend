# Configuration Reference

Complete environment variable reference for ROS-OCP Backend. Values are loaded
via [Viper](https://github.com/spf13/viper) from process environment (and
`.env` in local development). In Red Hat OpenShift / Clowder deployments, many
connection settings are injected automatically from the platform.

For algorithm-specific thresholds (container percentiles, GPU classification,
snapshot staleness, etc.), see [Configurability Reference](../architecture/configurability.md).
This document focuses on **platform wiring**, **performance tuning**, and
**operational controls**.

**Last updated:** 2026-06-10

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
| `ROS_RBAC_CACHE_MAX_ENTRIES` | `500` | Max entries in the in-memory RBAC permission LRU cache. Evicts oldest entries when full. |
| `ROS_THRESHOLD_RECALC_CONCURRENCY` | `3` | Max parallel clusters during async threshold recalculation after Settings API PUT. |
| `ROS_DB_MIN_CONNS` | `2` | pgxpool minimum idle connections kept open. |
| `ROS_DB_MAX_CONN_LIFETIME` | `30` (minutes) | Maximum lifetime of a pooled connection before recycle. |
| `ROS_DB_MAX_CONN_IDLE_TIME` | `5` (minutes) | Maximum idle time before a connection is closed. |
| `ROS_DB_STATEMENT_CACHE_MODE` | `describe` | pgx prepared-statement cache mode (`describe`, `prepare`, or `describe_exec`). `describe` avoids server-side prepare overhead for ad-hoc queries. |
| `ROS_RESHIP_CONCURRENCY` | `2` | Parallel masu `reship_ros` calls per org during business-hours backfill. Coordinate with masu rate limits when raising. |

Related database pool settings (pre-existing, often tuned together):

| Variable | Default | Purpose |
|----------|---------|---------|
| `ROS_DB_MAX_CONNS` | `10` | Maximum pgxpool connections per process (API, processor, poller each have their own pool). GORM and pgxpool share this single pool via `stdlib.OpenDBFromPool`. |
| `ROS_DB_ACQUIRE_TIMEOUT_SECS` | `5` | Max wait when acquiring a connection from the pool. `0` = unlimited wait. |
| `ROS_DB_STATEMENT_TIMEOUT` | `25` (seconds) | Session-level statement timeout applied on pool connect (`AfterConnect`) for API and GORM paths. |
| `ROS_DB_INGEST_STATEMENT_TIMEOUT` | `120` (seconds) | Per-transaction `SET LOCAL` timeout for ingestion batch writes (samples, digests, GPU/node). |
| `ROS_INGEST_FLUSH_BATCH_SIZE` | `1000` | Max container-day digest groups held in memory before an incremental flush during streaming ingest. |
| `ROS_INGEST_STRICT_ANALYTICS` | `true` | When `true` (default), history and quality writes must succeed before recommendations are persisted; analytics failures return a transient ingestion error and the Kafka message is retried. Set `false` for degraded mode: recommendations are written first and analytics gaps are flagged via metrics and API fields. |

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
| `ROS_API_MAX_OFFSET` | `10000` | Max `offset` query parameter; returns HTTP 400 above this (use keyset pagination for deeper pages). |
| `ROS_API_MAX_NODE_RESULTS` | `1000` | Hard cap on rows returned per request by node utilization and GPU time-slicing list endpoints. |
| `ROS_READINESS_CHECK_KAFKA` | `false` | When `true`, `/readyz` verifies Kafka broker metadata (processor/poller). |
| `ROS_READINESS_CHECK_S3` | `false` | When `true`, `/readyz` HEAD-checks the configured S3 bucket (ingestion). |
| `ROS_READINESS_S3_BUCKET` | (empty) | Bucket name for S3 readiness check (required when `ROS_READINESS_CHECK_S3=true`). |
| `ROS_READINESS_S3_ENDPOINT` | (empty) | S3/MinIO endpoint URL (path-style). |
| `ROS_READINESS_S3_ACCESS_KEY` | (empty) | S3 access key for readiness HEAD request. |
| `ROS_READINESS_S3_SECRET_KEY` | (empty) | S3 secret key for readiness HEAD request. |
| `ROS_READINESS_S3_REGION` | `us-east-1` | AWS region for S3 client. |
| `DEVELOPMENT` | `false` | When `true`, relaxes certain security checks for local dev (empty CSV allowlist, tag dev token). **Never set in production.** |
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

### Kafka resilience

| Variable | Default | Purpose |
|----------|---------|---------|
| `ROS_KAFKA_MAX_TRANSIENT_RETRIES` | `5` | Maximum number of times a message is requeued before being routed to the Dead Letter Queue. Applies to unclassified transient errors only. Set higher for environments with flaky S3/DB connectivity. |
| `ROS_KAFKA_DLQ_TOPIC` | `hccm.ros.events.dlq` | Kafka topic for dead-lettered messages. Must exist (auto-created by Strimzi if `auto.create.topics.enable` is true, or declare as KafkaTopic CR). |

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
| `ROS_COST_CACHE_MAX_ENTRIES` | `1000` | Max entries in the in-memory LRU cache for masu `effective_rates` responses (5-minute TTL per entry). |
| `ROS_SAVINGS_ESTIMATES_ENABLED` | `true` | Fetch effective rates from masu for dollar savings fields. |
| `ROS_RESHIP_POLLER_INTERVAL_SECS` | `60` | Background reship retry interval (seconds). |
| `ROS_RESHIP_MAX_RETRIES` | `10` | Consecutive reship failures before marking exhausted. |
| `ROS_BUSINESS_HOURS_RESHIP_FORWARD_ONLY_FALLBACK` | `false` | After max retries, fall back to forward-only BH recommendations. |

See **Performance Tuning** for `ROS_RESHIP_CONCURRENCY`.

---

## Kruize (Legacy)

| Variable | Default | Purpose |
|----------|---------|---------|
| `KRUIZE_URL` | `http://localhost:8080` | Kruize HTTP endpoint (defaults from host/port when unset). |
| `KRUIZE_HOST` | `localhost` | Kruize hostname (used to build default `KRUIZE_URL`). |
| `KRUIZE_PORT` | `8080` | Kruize port (used to build default `KRUIZE_URL`). |
| `KRUIZE_WAIT_TIME` | `30` | Seconds to wait for Kruize experiment results. |
| `KRUIZE_MAX_BULK_CHUNK_SIZE` | `100` | Max experiments per bulk API call. |
| `KRUIZE_PERFORMANCE_PROFILE_VERSION` | `v2.0` | Performance profile version sent to Kruize. |
| `ROS_USE_NATIVE_ENGINE` | `true` | **Deprecated.** Use `ROS_ENABLED_PLUGINS=kruize` instead. When `false`, forces Kruize-only mode. |

---

## Feature Flags and Plugins

Recommendation domains are toggled at runtime via two environment variables read in
[`internal/plugin/registry.go`](../../internal/plugin/registry.go) (not fields on the
central `Config` struct). See [Environment variables outside Config](#environment-variables-outside-config).

### Plugin enablement

| Variable | Default | Purpose |
|----------|---------|---------|
| `ROS_ENABLED_PLUGINS` | (empty) | Comma-separated **allowlist**. When empty, all native plugins run. When non-empty, **only** listed plugins run. |
| `ROS_DISABLED_PLUGINS` | (empty) | Comma-separated **denylist**. Applied only when the allowlist is **empty**: subtracts plugins from the default set. Ignored when `ROS_ENABLED_PLUGINS` is set. |
| `ROS_USE_NATIVE_ENGINE` | `true` | **Deprecated.** Use `ROS_ENABLED_PLUGINS=kruize` for legacy Kruize-only mode. |

**Available plugins:** `container`, `namespace`, `node`, `gpu`, `pvc`, `snapshot`, `kruize`

- **`kruize`** is mutually exclusive with native plugins. When enabled, only Kruize runs.
- **`kruize` plus native plugins** in `ROS_ENABLED_PLUGINS` causes a **fatal startup error** (process exits).
- With both allowlist and denylist unset, **`kruize` is off** unless explicitly allowlisted.

**Disable namespace recommendations** (either approach):

```bash
# Denylist (default allowlist = all native plugins)
ROS_DISABLED_PLUGINS=namespace

# Allowlist (omit namespace)
ROS_ENABLED_PLUGINS=container,gpu,node,pvc,snapshot
```

| Variable | Default | Purpose |
|----------|---------|---------|
| `ROS_BUSINESS_HOURS_ENABLED` | `true` | Business-hours routes, ingestion dual-stream, reship poller. |
| `ROS_THRESHOLD_RECALCULATION_ENABLED` | `true` | Async recalc when tenant threshold settings change. |

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
| `ROS_HISTORY_RETENTION_DAYS` | `90` | Recommendation history snapshot retention (days). |
| `ROS_STALENESS_THRESHOLD_HOURS` | `48` | Hours without cluster report before recommendations marked stale. |
| `ROS_STALE_CLEANUP_DAYS` | `30` | Delete stale recommendations older than N days. (`ROS_STALE_ARCHIVE_DAYS` deprecated alias) |
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
| `ROS_TAGS_ENABLED` | `true` | Master switch for tag list filters (and push API when source=api); cost-onprem chart default. |
| `ROS_TAGS_SOURCE` | `db` | `db` = direct Koku PostgreSQL reads; `api` = push into `resolved_tags`. |
| `ROS_TAGS_ALLOWED_SERVICE_ACCOUNTS` | (empty) | Comma-separated Kubernetes ServiceAccount names allowed to call the push API (api source only). **Required non-empty in production when `ROS_TAGS_SOURCE=api`.** |
| `ROS_TAGS_DEV_TOKEN` | (empty) | Dev-only bearer token fallback for push auth (api source). **Blocked at startup when `DEVELOPMENT` is not `true`.** |
| `ROS_TAGS_SYNC_MAX_BODY_MIB` | `10` | Max request body size (MiB) for `POST /internal/tags/sync` (api source). |
| `ROS_INTERNAL_TAGS_AUTH_REQUIRED` | `true` | Require bearer TokenReview auth on `/internal/tags/*` regardless of `ROS_TAGS_SOURCE`. Set `false` for local dev without SA tokens. |
| `ROS_INTERNAL_ALLOWED_ORGS` | (empty) | Optional comma-separated org IDs that internal endpoints may target. Empty allows all orgs (default for backward compatibility). |
| `ROS_SYNTH_MANIFEST_QUIET_PERIOD` | `30` | Seconds to defer recommendation engines after synthesized manifest (`synth-*`) ingestion completes; timer resets on each new file registration for that manifest. |
| `ROS_HISTORY_DEFAULT_DAYS` | `30` | Default lookback window for history endpoints when `start_date` and `end_date` are both omitted. |
| `ROS_FLEET_SUMMARY_CACHE_TTL` | `300` | In-memory fleet summary cache TTL in seconds (LRU, invalidated on recommendation ingest). |

Koku pushes resolved namespace tags via `POST /api/cost-management/v1/internal/tags/sync`
using `Authorization: Bearer <service-account-token>`. Sync freshness is available at
`GET /api/cost-management/v1/internal/tags/status?org_id=<org_id>`.

**Authentication:** Kubernetes ServiceAccount token validation via TokenReview API.
See [tag-sync-auth.md](tag-sync-auth.md) for current auth and the planned **mTLS** upgrade.

**Data flow and lifecycle:** [tag-sync.md](tag-sync.md)

List filtering supports Koku syntax `?filter[tag:key]=value1,value2` (OR within a key,
AND across keys). Fleet savings summary supports `?group_by[tag:key]=*` for per-tag-value
container savings aggregation. Empty tag-filtered lists may include `meta.warnings`.
With `ROS_TAGS_SOURCE=db`, startup verifies `reporting_enabledtagkeys` is reachable.
See [features/tag-filtering.md](../features/tag-filtering.md).

---

## CSV Download Security (Kafka ingestion)

Presigned CSV URLs in Kafka messages are validated before fetch.

| Variable | Default | Purpose |
|----------|---------|---------|
| `ROS_CSV_ALLOWED_HOSTS` | (empty) | Comma-separated hostname allowlist for CSV URLs. **Required in non-development mode** (startup fails if empty). |
| `ROS_CSV_DENY_PRIVATE_NETWORKS` | `true` | Deny private, link-local, loopback, and `localhost` targets (IPv4 and IPv6) even when allowlisted. Set `false` only for local httptest. |
| `ROS_CSV_MAX_BODY_BYTES` | `104857600` (100 MiB) | Max CSV download size (bytes). Lower if processor memory is constrained. |
| `ROS_CORS_ALLOWED_ORIGINS` | (empty) | Comma-separated browser origins allowed for CORS. Empty + `DEVELOPMENT=true` allows `*`. Empty in production denies cross-origin requests. |
| `ROS_CSV_DOWNLOAD_TIMEOUT_SECS` | `120` | CSV fetch timeout. |

Startup validation: `ValidateSecurityConfig()` in `internal/config/security.go` (called from service startup, not config load).

---

## Housekeeper & Logging

| Variable | Default | Purpose |
|----------|---------|---------|
| `ROS_HOUSEKEEPER_SHUTDOWN_GRACE_SECS` | `30` | Grace period (seconds) when housekeeper receives SIGTERM/SIGINT mid-cleanup. |
| `ROS_LOG_POISON_PAYLOAD` | `false` | When `true`, log first 256 bytes of permanently failed Kafka payloads (debug only). Default logs metadata only; full payload is on the DLQ topic. |

---

## OOM Feedback

| Variable | Default | Purpose |
|----------|---------|---------|
| `ROS_OOM_BASE_BUMP` | `0.15` | Log-scaling factor for post-OOM memory bump. |
| `ROS_OOM_MAX_BUMP` | `1.60` | Maximum memory bump multiplier after OOM kills. |

---

## Idle / Zombie Detection

| Variable | Default | Purpose |
|----------|---------|---------|
| `ROS_IDLE_DETECTION_ENABLED` | `true` | Master switch for inline idle/zombie classification. |
| `ROS_IDLE_ZOMBIE_CPU_MILLICORES` | `1` | P95 CPU (millicores) below this → zombie candidate. |
| `ROS_IDLE_ZOMBIE_PEAK_MILLICORES` | `10` | Peak CPU below this confirms zombie. |
| `ROS_IDLE_CPU_UTILIZATION_PCT` | `2` | P95 CPU as % of request; below → idle. |
| `ROS_IDLE_MEMORY_UTILIZATION_PCT` | `5` | P95 memory as % of request; below → idle. |
| `ROS_IDLE_BURST_RATIO` | `10` | `peak/P95` above this → bursty (stay active). |
| `ROS_IDLE_MIN_OBSERVATION_DAYS` | `14` | Minimum digest days before classifying. |
| `ROS_IDLE_EXCLUDE_NAMESPACES` | `kube-system,openshift-*` | Comma-separated namespace globs to skip. |
| `ROS_IDLE_EXCLUDE_WORKLOAD_TYPES` | `DaemonSet` | Comma-separated owner kinds to skip. |
| `ROS_IDLE_GPU_SM_ACTIVE_BP` | `500` | GPU `sm_active` P95 below this (basis points) → idle. |
| `ROS_IDLE_GPU_DRAM_ACTIVE_BP` | `500` | GPU `dram_active` P95 below this (basis points) → idle. |

Tenant Settings API overrides for these thresholds are planned; env + defaults apply today.
See [idle-detection.md](../features/idle-detection.md).

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

Node idle/zombie classification (`classification.idle_state` on
`GET .../recommendations/openshift/nodes`; migration **000111**):

| Variable | Default | Purpose |
|----------|---------|---------|
| `ROS_NODE_ZOMBIE_CPU_MC` | `200` | CPU P95 (millicores) below this with few pods → `zombie`. |
| `ROS_NODE_ZOMBIE_MAX_PODS` | `5` | Max running pods for zombie classification. |
| `ROS_NODE_IDLE_CPU_UTIL_PCT` | `10` | CPU utilization % of allocatable below this → `idle` candidate. |
| `ROS_NODE_IDLE_MEM_UTIL_PCT` | `10` | Memory utilization % of allocatable below this → `idle` candidate. |
| `ROS_NODE_IDLE_MAX_PODS` | `10` | Max running pods for idle classification. |

See [`ClassifyNodeIdleState`](../../internal/engine/recommend_nodes.go) and
[Node recommendations](../../docs-site/features/node-recommendations.md).

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

### ResourceQuota

`ROS_QUOTA_HEADROOM_PERCENT` (default `10`), `ROS_QUOTA_HIGH_RISK_THRESHOLD_PERCENT` (default `90`),
`ROS_QUOTA_MEDIUM_RISK_THRESHOLD_PERCENT` (default `70`). Per-org overrides:
`GET/PUT/DELETE .../settings/quota`. See
[quota-recommendations.md](../features/quota-recommendations.md).

### ClusterResourceQuota

`ROS_CLUSTER_QUOTA_HEADROOM_PERCENT` (default `10`),
`ROS_CLUSTER_QUOTA_HIGH_RISK_THRESHOLD_PERCENT` (default `90`),
`ROS_CLUSTER_QUOTA_MEDIUM_RISK_THRESHOLD_PERCENT` (default `70`). Per-org overrides:
`GET/PUT/DELETE .../settings/cluster-quota`. API list:
`GET .../recommendations/openshift/cluster-quota/`. Requires `cluster-quota` in
`ROS_ENABLED_PLUGINS`. See
[cluster-resource-quota.md](../features/cluster-resource-quota.md).

### Snapshot

`ROS_SNAPSHOT_ORPHAN_AGE_DAYS`, `ROS_SNAPSHOT_NEVER_RESTORED_DAYS`,
`ROS_SNAPSHOT_STALE_DAYS`, `ROS_SNAPSHOT_REDUNDANT_THRESHOLD`,
`ROS_SNAPSHOT_COST_PER_GIB_MONTH`, `ROS_SNAPSHOT_INVENTORY_FRESH_HOURS`,
`ROS_SNAPSHOT_INVENTORY_RETENTION_HOURS`, `ROS_SNAPSHOT_STALE_GRACE_HOURS`

### Term windows (per plugin)

Read via viper in [`internal/config/env.go`](../../internal/config/env.go) and applied in
[`internal/engine/term_config.go`](../../internal/engine/term_config.go). Format:
`ROS_TERMS_<PLUGIN>_<TERM>_<FIELD>` where `<PLUGIN>` is the
recommendation type (`CONTAINER`, `NAMESPACE`, `NODE`, `GPU`, `PVC`, `SNAPSHOT`),
`<TERM>` is `SHORT`, `MEDIUM`, or `LONG`, and `<FIELD>` is one of:

| Field suffix | Type | Example |
|--------------|------|---------|
| `WINDOW_DAYS` | int | `ROS_TERMS_CONTAINER_LONG_WINDOW_DAYS=45` |
| `MIN_DATA_DAYS` | int | `ROS_TERMS_CONTAINER_MEDIUM_MIN_DATA_DAYS=3` |
| `DECAY_HALFLIFE_HOURS` | float | `ROS_TERMS_CONTAINER_MEDIUM_DECAY_HALFLIFE_HOURS=168` |

When set, the env var **locks** that term field platform-wide (tenant Settings API cannot override).

---

## Centralized configuration

All production environment variables are loaded through
[`internal/config/config.go`](../../internal/config/config.go) (viper + `Config` struct).
Per-plugin term overrides use dynamic keys (`ROS_TERMS_<PLUGIN>_<TERM>_<FIELD>`) read via
[`config.EnvString`](../../internal/config/env.go) — see [Term windows](#term-windows-per-plugin).

Plugin fields: `EnabledPlugins` (`ROS_ENABLED_PLUGINS`), `DisabledPlugins` (`ROS_DISABLED_PLUGINS`).
CSV download: `CSVMaxBodyBytes`, `CSVDownloadTimeoutSecs`, `CSVAllowedHosts`, `CSVDenyPrivateNetworks`.
Security startup: `Development`, `APIMaxOffset`, `LogPoisonPayload`, `HousekeeperShutdownGraceSecs`.
Kubernetes tag sync auth: `KubernetesSATokenPath`, `KubernetesTokenReviewURL`.

`KRUIZE_HOST` and `KRUIZE_PORT` are viper-only keys used to build the default `KRUIZE_URL`.

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
2. `ROS_RBAC_CACHE_MAX_ENTRIES=500` caps memory; watch `rosocp_rbac_cache_size` and `rosocp_rbac_cache_evictions_total`.
3. Lower TTL (e.g. `30`) if permission changes must propagate faster.
4. Set TTL `0` only for debugging — every request hits RBAC.

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
