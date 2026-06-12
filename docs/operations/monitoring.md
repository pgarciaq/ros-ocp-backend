# Monitoring and Observability

Operational guide for ROS-OCP Backend metrics, logging, and alerting. Complements the runbooks in [runbooks.md](runbooks.md) with a complete metric catalog and scrape configuration.

**Last updated:** 2026-06-13

---

## Architecture Overview

ROS-OCP Backend runs as three independent processes (deployments):

| Process | Role | Primary port | Metrics port |
|---------|------|--------------|--------------|
| **API** (`rosocp start api`) | REST API for recommendations and settings | `API_PORT` (default `8000`) | `PROMETHEUS_PORT` (default `5005` local; `9000` in Clowder/cost-onprem) |
| **Processor** (`rosocp start processor`) | Kafka consumer; ingestion and native recommendation pipeline | — | `PROMETHEUS_PORT` |
| **Recommendation poller** (`rosocp start recommendation-poller`) | Legacy Kruize recommendation fetcher | — | `PROMETHEUS_PORT` |

Each process registers Prometheus collectors via `promauto` at startup. Standard Go runtime collectors (`go_*`, `process_*`) are also exposed.

---

## Metrics Endpoints

### API pod

The API runs **two HTTP listeners**:

1. **Application server** (`API_PORT`) — REST routes, probes, and Echo middleware metrics
   - `GET /status` — trivial liveness (returns `{"api-server":"working"}`)
   - `GET /healthz` — deep liveness (goroutine count, GC pause, scheduler responsiveness)
   - `GET /readyz` — readiness (PostgreSQL pool ping; optional Kafka/S3 when `ROS_READINESS_CHECK_*` enabled)
   - `GET /api/cost-management/v1/...` — recommendation API

2. **Metrics server** (`PROMETHEUS_PORT`) — dedicated listener in `internal/api/server.go`
   - `GET /metrics` — Prometheus scrape target (includes API middleware + all app metrics)

Implementation: `internal/api/server.go` starts `metricsEcho` on `PROMETHEUS_PORT` with `echoprometheus.NewHandler()`.

### Processor and recommendation-poller pods

A single HTTP server on `PROMETHEUS_PORT` (`internal/utils/utils.go` → `Start_prometheus_server()`):

| Path | Purpose |
|------|---------|
| `GET /metrics` | Prometheus scrape target |
| `GET /status` | Trivial liveness (`{"status":"ok"}`) |
| `GET /healthz` | Deep liveness (goroutines, GC, scheduler) |
| `GET /readyz` | Readiness (DB ping; optional Kafka/S3) |

Clowder and cost-onprem set `PROMETHEUS_PORT=9000` and use port `9000` for probes on processor/poller. The API deployment must set `PROMETHEUS_PORT` to match the exposed metrics Service port (typically `9000`).

### Local development ports

From `Makefile` / `CONTRIBUTING.md`:

| Process | `PROMETHEUS_PORT` |
|---------|-------------------|
| Processor | `5005` |
| Recommendation poller | `5006` |
| API | `5007` |

---

## Prometheus Metrics Catalog

All application metrics use the `rosocp_` prefix except business-hours reship metrics (`ros_reship_*`, `ros_threshold_*`) and a few legacy counters documented in [Legacy / non-prefixed metrics](#legacy--non-prefixed-metrics).

### Pipeline health and throughput

| Metric | Type | Labels | What it measures |
|--------|------|--------|------------------|
| `rosocp_kafka_messages_processed_total` | Counter | — | Kafka messages committed successfully after processing (processor and poller) |
| `rosocp_kafka_retries_total` | Counter | — | Kafka messages requeued with incremented retry count |
| `rosocp_recommendations_written_total` | Counter | `type` | Recommendations persisted to PostgreSQL. `type`: `container`, `namespace`, `node`, `pvc`. **Note:** `snapshot` is observed for duration but not incremented on write (known gap) |
| `rosocp_recommendation_duration_seconds` | Histogram | `type` | End-to-end recommendation computation time. `type`: `container`, `node`, `gpu`, `namespace`, `pvc`, `snapshot` |
| `rosocp_pipeline_phase_duration_seconds` | Histogram | `phase` | Sub-phase timing within ingestion. Observed phases: `digest`, `gpu_enrichment` |
| `rosocp_rh_account_created_total` | Counter | — | New tenant accounts provisioned on first ingestion |

**Source files:** `internal/metrics/metrics.go`, `internal/services/report_processor.go`

### Error indicators

| Metric | Type | Labels | What it measures |
|--------|------|--------|------------------|
| `rosocp_kafka_dlq_messages_total` | Counter | — | Messages routed to Dead Letter Queue after exhausting retry budget |
| `rosocp_ingestion_errors_total` | Counter | `stage` | Native pipeline failures. `stage`: `csv_parse`, `digest`, `recommend`, `write` |
| `rosocp_invalid_csv_total` | Counter | — | Malformed container ROS CSV |
| `rosocp_invalid_namespace_csv_total` | Counter | — | Malformed namespace ROS CSV |
| `rosocp_invalid_datapoints_total` | Counter | — | Invalid rows within otherwise parseable CSVs |
| `rosocp_csv_fetch_error_total` | Counter | — | S3/HTTP failures downloading CSV payloads |
| `rosocp_db_error_total` | Counter | — | Generic database errors |
| `rosocp_partition_missing_error_total` | Counter | `resource_name` | Writes failed because a monthly partition does not exist (e.g. `workload_metrics`, `historical_recommendation_set`) |
| `rosocp_quality_partition_missing_total` | Counter | — | Quality-metrics write failed due to missing partition |
| `ros_ocp_plugin_hook_errors_total` | Counter | `plugin`, `hook_type` | Non-fatal plugin ingest hook failures (processing continues) |

**Source files:** `internal/services/metrics.go`, `internal/model/metrics.go`, `internal/utils/metrics.go`, `internal/engine/quality.go`

### Legacy Kruize path (poller only)

| Metric | Type | Labels | What it measures |
|--------|------|--------|------------------|
| `rosocp_recommendation_request_total` | Counter | — | Container recommendation requests to Kruize when result not found |
| `rosocp_namespace_recommendation_request_total` | Counter | — | Namespace recommendation requests to Kruize |
| `rosocp_recommendation_success_total` | Counter | — | Container recommendations saved from Kruize |
| `rosocp_namespace_recommendation_success_total` | Counter | — | Namespace recommendations saved from Kruize |
| `rosocp_kruize_api_exception_total` | Counter | `path` | Exceptions calling Kruize HTTP API |
| `rosocp_invalid_recommendation_total` | Counter | — | Invalid container recommendations returned by Kruize |
| `rosocp_invalid_namespace_recommendation_total` | Counter | — | Invalid namespace recommendations from Kruize |
| `kruize_create_experiment_request_total` | Counter | — | Container experiment creation requests |
| `kruize_update_result_request_total` | Counter | — | Container update-result requests |
| `kruize_create_namespace_experiment_request_total` | Counter | — | Namespace experiment creation requests |
| `kruize_update_namespace_result_request_total` | Counter | — | Namespace update-result requests |

**Source files:** `internal/services/metrics.go`, `internal/utils/kruize/metrics.go`

### Threshold and configurability

| Metric | Type | Labels | What it measures |
|--------|------|--------|------------------|
| `ros_threshold_recalculation_total` | Counter | `recommendation_type`, `status` | Async recalculations triggered by Settings API threshold changes. `status`: `success`, `error`, `skipped`. Per-org context in structured logs. |
| `ros_threshold_cache_entries` | Gauge | — | In-memory threshold resolution cache size (per org × recommendation type) |

**Source files:** `internal/engine/threshold_recalculate.go`, `internal/engine/threshold_metrics.go`

Threshold recalculation is gated by `ROS_THRESHOLD_RECALCULATION_ENABLED` (default `true`).

### Business-hours reship

| Metric | Type | Labels | What it measures |
|--------|------|--------|------------------|
| `ros_reship_in_progress` | Gauge | — | Number of masu `reship_ros` calls currently in flight (fleet-wide). Per-org/cluster context in structured logs. |
| `ros_reship_files_processed` | Counter | — | ROS files published to Kafka by successful reships |
| `ros_reship_duration_seconds` | Histogram | — | HTTP duration of masu `reship_ros` calls |
| `ros_reship_failures_total` | Counter | — | Reship attempts that exhausted the consecutive retry budget |
| `ros_reship_provider_resolution_failures_total` | Counter | `reason` | Failures resolving `cluster_uuid` → `provider_uuid`. `reason`: `no_cost_model`, `masu_unavailable`, `not_found`, `timeout` |
| `ros_reship_fallback_forward_only_total` | Counter | — | Clusters transitioned to forward-only BH recommendations after retry exhaustion |

**Source file:** `internal/reship/metrics.go`

Reship poller interval: `ROS_RESHIP_POLLER_INTERVAL_SECS` (default `60`). Max retries: `ROS_RESHIP_MAX_RETRIES` (default `10`).

### API performance (Echo middleware)

Registered with subsystem `rosocp` in `internal/api/server.go`:

| Metric | Type | Labels | What it measures |
|--------|------|--------|------------------|
| `rosocp_requests_total` | Counter | `code`, `method`, `host`, `url` | HTTP requests handled by the API. `url` is the Echo route template (e.g. `/api/cost-management/v1/recommendations/openshift/:recommendation-id`) |
| `rosocp_request_duration_seconds` | Histogram | `code`, `method`, `host`, `url` | Request latency |
| `rosocp_request_size_bytes` | Histogram | `code`, `method`, `host`, `url` | Approximate request body size |
| `rosocp_response_size_bytes` | Histogram | `code`, `method`, `host`, `url` | Response body size |

These metrics appear on the **metrics listener** scrape (`PROMETHEUS_PORT`), not the main API port.

### Database layer

| Metric | Type | Labels | What it measures |
|--------|------|--------|------------------|
| `rosocp_db_query_duration_seconds` | Histogram | `operation` | Labeled DB operation latency. Known `operation` values: `upsert_usage_samples`, `write_recommendations`, `persist_node_recommendations` |

### Connection pool (pgxpool)

GORM and direct pgxpool access share one pool per process. Metrics are point-in-time snapshots from `pool.Stat()` on each Prometheus scrape.

| Metric | Type | What it measures |
|--------|------|------------------|
| `rosocp_db_pool_total_conns` | Gauge | Total connections (acquired + idle + constructing) |
| `rosocp_db_pool_acquired_conns` | Gauge | Connections currently in use |
| `rosocp_db_pool_idle_conns` | Gauge | Idle connections |
| `rosocp_db_pool_max_conns` | Gauge | Configured max connections (`ROS_DB_MAX_CONNS`) |
| `rosocp_db_pool_acquire_count_total` | Counter | Cumulative successful acquires |
| `rosocp_db_pool_acquire_duration_seconds` | Counter | Cumulative time spent acquiring connections |

**Alerting example:**

```promql
rosocp_db_pool_acquired_conns / rosocp_db_pool_max_conns > 0.9
```

### Ingestion streaming

| Metric | Type | Labels | What it measures |
|--------|------|--------|------------------|
| `rosocp_ingest_groups_in_memory` | Gauge | — | Container-day digest groups currently held in memory during streaming CSV ingest |
| `rosocp_ingest_flush_total` | Counter | — | Incremental digest-group flush operations (each flush is its own transaction) |
| `rosocp_ingest_flush_duration_seconds` | Histogram | — | Duration of incremental digest-group flush operations |
| `rosocp_ingest_manifest_id_synthesized_total` | Counter | — | Kafka messages that omitted `metadata.manifest_id` and received a deterministic synthesized manifest ID (`synth-` prefix) for per-file tracking |
| `rosocp_manifest_recommendation_deferred_total` | Counter | — | Recommendation runs deferred for synthesized manifest IDs pending the quiet period (`ROS_SYNTH_MANIFEST_QUIET_PERIOD`) |
| `rosocp_internal_endpoint_calls_total` | Counter | `endpoint`, `sa_name` | Internal platform endpoint invocations (tag sync/status, savings recalc). Target `org_id` is logged per call. |
| `rosocp_analytics_incomplete_total` | Counter | `error_type` | Container ingestion batches where history or quality analytics writes failed in degraded mode. `error_type`: `history` or `quality`. Per-org/cluster context in structured logs. Not incremented in strict mode (`ROS_INGEST_STRICT_ANALYTICS=true`) because the message is retried instead. |

Pool tuning env vars: `ROS_DB_MAX_CONNS` (default `10`), `ROS_DB_ACQUIRE_TIMEOUT_SECS` (default `5`), `ROS_DB_STATEMENT_TIMEOUT` (default `25`), `ROS_DB_INGEST_STATEMENT_TIMEOUT` (default `120`), `ROS_INGEST_FLUSH_BATCH_SIZE` (default `1000`).

### Koku effective-rates cache

In-memory LRU cache for masu `effective_rates` responses (used by savings estimates). Entries expire after 5 minutes; capacity is capped by `ROS_COST_CACHE_MAX_ENTRIES` (default `1000`).

| Metric | Type | What it measures |
|--------|------|------------------|
| `rosocp_cost_cache_size` | Gauge | Current number of cached org×cluster effective-rates entries |
| `rosocp_cost_cache_evictions_total` | Counter | Entries evicted because the cache reached max capacity |

**Source file:** `internal/costdata/provider.go`, `internal/costdata/metrics.go`

### RBAC permission cache

In-memory LRU cache for RBAC permission lookups keyed by `X-Rh-Identity`. TTL is set by `ROS_RBAC_CACHE_TTL` (default **60 seconds**; `0` disables). Capacity is capped by `ROS_RBAC_CACHE_MAX_ENTRIES` (default **500**).

| Metric | Type | What it measures |
|--------|------|------------------|
| `rosocp_rbac_cache_size` | Gauge | Current number of cached RBAC permission entries |
| `rosocp_rbac_cache_evictions_total` | Counter | Entries evicted because the cache reached max capacity |

**Source file:** `internal/api/middleware/rbac_cache.go`

### Fleet summary cache

In-memory LRU cache for `GET /recommendations/openshift/fleet-summary` and default (non-`group_by`) `GET /recommendations/openshift/savings-summary` responses, keyed by org, RBAC scope, and (for savings) `engine`/`term`. TTL is set by `ROS_FLEET_SUMMARY_CACHE_TTL` (default **300 seconds**). Capacity is capped by `ROS_FLEET_SUMMARY_CACHE_CAPACITY` (default **256**) per cache. Explicit invalidation runs on recommendation ingest, threshold/business-hours settings changes, and savings recalculation triggers.

| Metric | Type | What it measures |
|--------|------|------------------|
| `rosocp_fleet_summary_cache_size` | Gauge | Current number of cached fleet summary entries |
| `rosocp_fleet_summary_cache_hits_total` | Counter | Fleet cache lookups that returned a valid entry |
| `rosocp_fleet_summary_cache_misses_total` | Counter | Fleet cache lookups that missed or found an expired entry |
| `rosocp_fleet_summary_cache_evictions_total` | Counter | Fleet entries evicted because the cache reached max capacity |
| `rosocp_fleet_summary_cache_invalidations_total` | Counter | Explicit org-scoped fleet cache invalidations (`InvalidateOrg`) |
| `rosocp_fleet_summary_cache_lazy_expiry_total` | Counter | Fleet entries removed on read because TTL expired |
| `rosocp_savings_summary_cache_size` | Gauge | Current number of cached savings summary entries |
| `rosocp_savings_summary_cache_hits_total` | Counter | Savings summary cache lookups that returned a valid entry |
| `rosocp_savings_summary_cache_misses_total` | Counter | Savings summary cache lookups that missed or found an expired entry |
| `rosocp_savings_summary_cache_evictions_total` | Counter | Savings entries evicted because the cache reached max capacity |
| `rosocp_savings_summary_cache_invalidations_total` | Counter | Explicit org-scoped savings cache invalidations (`InvalidateOrg`) |
| `rosocp_savings_summary_cache_lazy_expiry_total` | Counter | Savings entries removed on read because TTL expired |

**Source file:** `internal/fleetsummary/cache.go`

### Async job coalescing

Per-org single-flight guards prevent duplicate background work when settings or internal triggers fire in rapid succession.

| Metric | Type | Labels | What it measures |
|--------|------|--------|------------------|
| `rosocp_threshold_recalc_coalesced_total` | Counter | `recommendation_type` | Threshold recalc triggers coalesced while a job was in-flight |
| `rosocp_savings_recalc_coalesced_total` | Counter | — | Savings recalc triggers coalesced while a job was in-flight |
| `rosocp_reship_coalesced_total` | Counter | — | Business-hours reship triggers coalesced while a job was in-flight |

**Source files:** `internal/engine/threshold_recalc_guard.go`, `internal/engine/savings_recalc_guard.go`, `internal/reship/trigger_guard.go`

### GPU

| Metric | Type | Labels | What it measures |
|--------|------|--------|------------------|
| `rosocp_gpu_model_unrecognized_total` | Counter | `model_name` | GPU model strings not matched in `internal/engine/gpu_catalog.yaml` |

GPU recommendation duration is recorded under `rosocp_recommendation_duration_seconds{type="gpu"}` (API enrichment path in `internal/api/handlers_node_recs.go`).

### Recommendation quality (processor)

Emitted after each successful `WriteRecommendationQuality` batch during container ingestion. Per-org/cluster aggregates are logged structurally; Prometheus captures fleet-wide distributions.

| Metric | Type | Labels | What it measures |
|--------|------|--------|------------------|
| `ros_recommendation_oom_rate` | Histogram | — | Mean `oom_events_after_rec` per quality batch |
| `ros_recommendation_stability` | Histogram | — | Mean recommendation stability per batch (0–100; 100 = no change vs prior cycle) |
| `ros_recommendation_adoption_rate` | Histogram | — | Mean adoption rate per batch (0–100) |

**Source file:** `internal/engine/quality.go`

The `/recommendations/openshift/quality` API returns `stability_pct` on a **0.0–1.0** scale; Prometheus stability uses 0–100.

### Retention

| Metric | Type | Labels | What it measures |
|--------|------|--------|------------------|
| `rosocp_retention_partitions_dropped_total` | Counter | — | Monthly partitions dropped by the retention sweep |

Retention is controlled by `ROS_RETENTION_MONTHS`, `ROS_HISTORY_RETENTION_DAYS`, `ROS_STALE_CLEANUP_DAYS`, `ROS_SNAPSHOT_INVENTORY_RETENTION_H`. See [retention.md](retention.md).

### Legacy / non-prefixed metrics

Some counters predate the `rosocp_` naming convention and retain their original names:

| Metric | Type | Labels | What it measures |
|--------|------|--------|------------------|
| `ros_ingestion_file_failures_total` | Counter | `report_type`, `error_class` | Permanent per-file ingestion failures recorded in `report_file_status` |
| `ros_savings_recalculation_total` | Counter | `recommendation_type`, `status` | Cost-model-triggered savings-only recalculations. `status`: `success`, `error` |

**Source files:** `internal/services/metrics.go`, `internal/engine/savings_recalculate.go`

---

## Logging

### Library and format

- **Library:** [logrus](https://github.com/sirupsen/logrus) via `internal/logging/logging.go`
- **Production format:** JSON (`logrus.JSONFormatter`) — default when `LogFormater` is not `text`
- **Development format:** Text (`LogFormater=text` in `.env`)
- **Caller reporting:** Enabled (`ReportCaller=true`) — log entries include `func` and `file` fields
- **CloudWatch:** Optional batching hook when `CwAccessKey` is set (Clowder-managed in SaaS)

Every log line includes `service` from `SERVICE_NAME` (e.g. `rosocp-processor`, `ros-api`).

### Key structured fields

| Field | When present | Purpose |
|-------|--------------|---------|
| `service` | Always (root logger) | Deployment identity |
| `org_id` | Pipeline, API, reship | Tenant correlation |
| `cluster_uuid` | Ingestion, engine | Cluster scope |
| `request_id` | Kafka messages, API (`middleware.RequestID`) | End-to-end request tracing |
| `account`, `source_id`, `cluster_alias` | Upload Kafka messages | Cost-mgmt payload metadata |
| `workload_id`, `experiment_name` | Poller Kafka messages | Kruize experiment context |
| `msg` | Reship, threshold recalc | Stable message key for log aggregation |

Helper functions: `logging.Set_request_details()`, `logging.ForOrg()`, `logging.ForRequest()` in `internal/logging/logging.go`.

### Notable log patterns

| Pattern | Level | Meaning |
|---------|-------|---------|
| `Logging initialized` | INFO | Logger ready; includes `service` |
| `Starting prometheus http server` | INFO | Metrics listener started (processor/poller) |
| `unable to read CSV from URL` | ERROR | S3/HTTP fetch failure; check `rosocp_csv_fetch_error_total` |
| `unable to process ... error` | ERROR | CSV parse failure; increments `rosocp_ingestion_errors_total{stage="csv_parse"}` |
| `Kafka message omitted metadata.manifest_id` | WARN | Legacy publisher; synthesized `synth-*` manifest ID for per-file tracking; increments `rosocp_ingest_manifest_id_synthesized_total` |
| `native engine: no recommendations produced` | INFO | Cluster had data but no actionable container recs |
| `native engine: analytics pipeline incomplete` | WARN | History/quality write partial failure; recs still saved |
| `readyz: database ping failed` | WARN | DB unreachable; readiness probe will fail |
| `gpu_metadata: unrecognized GPU model` | WARN | Add model to GPU catalog; see `rosocp_gpu_model_unrecognized_total` |
| `cost data fetch failed` | WARN | Masu `effective_rates` unavailable; savings may be zero |
| `threshold recalculation started/completed/failed` | INFO/WARN | Settings-driven async recalc |
| `reship completed` | INFO | Successful business-hours backfill (`files_processed` field) |
| `reship max retries exceeded` | ERROR | Reship exhausted; may trigger forward-only fallback |
| `provider_uuid resolution failed; reship deferred` | WARN | Cannot map cluster to Koku provider |

---

## Configuration

Environment variables that affect observability:

| Variable | Default | Effect |
|----------|---------|--------|
| `LOG_LEVEL` | `INFO` | `DEBUG`, `INFO`, or `ERROR` |
| `LogFormater` | `text` (local), JSON (Clowder) | Log output format |
| `SERVICE_NAME` | `rosocp` | `service` field on all log lines |
| `PROMETHEUS_PORT` | `5005` (local), `9000` (Clowder) | Metrics (+ probes for processor/poller) |
| `API_PORT` | `8000` | API listener; `/status`, `/healthz`, and `/readyz` for API probes |
| `CW_LOG_STREAM_NAME` | `rosocp` | CloudWatch log stream (when CW credentials configured) |
| `ROS_DB_MAX_CONNS` | `10` | Connection pool size (affects DB latency under load) |
| `ROS_DB_ACQUIRE_TIMEOUT_SECS` | `5` | Pool acquire timeout; `0` = unlimited |
| `ROS_RESHIP_POLLER_INTERVAL_SECS` | `60` | Background reship retry interval |
| `ROS_RESHIP_MAX_RETRIES` | `10` | Consecutive reship failures before giving up |
| `ROS_THRESHOLD_RECALCULATION_ENABLED` | `true` | Gate threshold-change recalc metrics and background work |
| `ROS_BUSINESS_HOURS_ENABLED` | `true` | Enables reship poller and BH metrics on API deployment |
| `KAFKA_AUTO_COMMIT` | `false` | Affects redelivery behavior on crash (see [runbooks.md](runbooks.md)) |

---

## Grafana Dashboard

The **ROSOCP** Grafana dashboard (`dashboards/grafana-dashboard-insights-rosocp-general.configmap.yaml`) provides a single-pane view of pipeline health, engine performance, caches, and infrastructure. It is the primary operational dashboard for on-call and incident response.

### Dashboard overview

| Property | Value |
|----------|-------|
| **ConfigMap name** | `grafana-dashboard-insights-rosocp-general` |
| **Dashboard UID** | `ofxxAX0nk` |
| **Title** | ROSOCP |
| **Auto-import label** | `grafana_dashboard: "true"` |
| **Grafana folder** | `/grafana-dashboard-definitions/Insights` (annotation) |

The dashboard JSON lives under the `ROSOCP.json` key in the ConfigMap. Grafana's **sidecar container** watches for ConfigMaps with the `grafana_dashboard: "true"` label and auto-imports them — no manual JSON upload is required when the chart or platform deploys the ConfigMap.

**Template variables:**

| Variable | Type | Purpose |
|----------|------|---------|
| `datasource` | Prometheus datasource | Application metrics (`rosocp_*`, `ros_*`) scraped from ROS API, processor, and poller pods |
| `DatasourceRDS` | Prometheus datasource | AWS RDS exporter metrics (SaaS only) |
| `namespace` | Custom (`rosocp-stage`, `rosocp-prod`, …) | Scopes Kubernetes infrastructure panels (OOM, restarts, memory) to the target deployment namespace |
| `cloudwatch` | Prometheus datasource | AWS CloudWatch exporter — Kafka consumer lag for `hccm.ros.events` (SaaS only) |

The top panel (`hccm.ros.events`) shows Kafka consumer lag via CloudWatch and is independent of the row sections below.

### Row sections

#### 1. Kafka & Ingestion

Upstream data path health: Kafka consumer lag (CloudWatch), invalid container/namespace CSVs, CSV rows skipped, permanent ingestion file failures (`report_type` / `error_class`), DLQ messages, Kafka retries, messages processed, CSV fetch errors, ingestion errors by `stage`, and ingestion file failures broken down by `report_type`.

#### 2. Recommendations

Legacy Kruize poller throughput: recommendations saved and requested, container and namespace recommendation success ratios, namespace request/success counters, and invalid namespace recommendations.

#### 3. Quality & Stability

Recommendation quality signals: quality partition missing errors, analytics incomplete by `error_type` (`history` / `quality`), and histogram percentiles (p50 / p95) for recommendation stability, adoption rate, and OOM rate after recommendation.

#### 4. Reship

Business-hours backfill: in-progress gauge, files processed, failures, forward-only fallback, provider resolution failures by `reason`, coalesced triggers, and reship duration (p50 / p95).

#### 5. Recalculation

Settings- and cost-model-driven background work: threshold recalculation by `recommendation_type` / `status`, savings recalculation by `recommendation_type` / `status`, and coalesced counters for threshold and savings recalc.

#### 6. Engine Performance

Native engine timing and throughput: DB query latency p95 by `operation`, recommendation duration p95 by `type`, pipeline phase duration p95 by `phase`, and recommendations written by `type`.

#### 7. Streaming Ingest

In-memory digest buffering: groups in memory gauge, ingest flush rate, and ingest flush duration p95.

#### 8. Caches

API response caches: fleet and savings summary cache hit rates, combined cache sizes (fleet, savings, cost, RBAC, threshold), and eviction rates across caches.

#### 9. Data Lifecycle

Retention and manifest handling: retention partitions dropped, manifest recommendation deferred (debounced quiet-period deferrals), and manifest IDs synthesized for legacy Kafka messages.

#### 10. GPU & Plugins

Enrichment gaps: unrecognized GPU models by `model_name`, plugin hook errors by `plugin` / `hook_type`, and invalid data points from malformed CSV rows.

#### 11. API HTTP

REST API health: request latency p95 and request rate by HTTP status code.

#### 12. Infrastructure

Platform and pod health: RH accounts created, DB errors, partition missing errors, RDS free storage (SaaS), internal endpoint calls, OOMKilled containers, container restarts (processor / poller / api / housekeeper), and pod memory working set.

### Key panels during incidents

| Symptom | Panel to check | What to look for |
|---------|----------------|------------------|
| Slow API responses | Engine Performance → DB query latency p95 | Spikes above 1–2s indicate DB contention |
| Slow API responses | Caches → Fleet/savings cache hit rate | Below 50% means cache is not helping |
| Slow API responses | API HTTP → Request latency p95 | Overall p95 above 500ms needs investigation |
| Missing recommendations | Recommendations → success ratio | Below 0.95 indicates failures |
| High memory usage | Streaming Ingest → Groups in memory | Sustained high values indicate backpressure |
| Pod OOMKilled | Infrastructure → OOMKilled + Pod memory | Compare working set to limits |
| Ingestion errors | Kafka & Ingestion → file failures | Check `report_type` and `error_class` for patterns |
| GPU enrichment gaps | GPU & Plugins → Unrecognized GPU models | New GPU models need catalog updates |

### Recommended alert rules

These complement the [general alerting rules](#alerting-recommendations) below and map directly to dashboard panels:

```yaml
# DB query latency too high
- alert: ROSOCPDBQueryLatencyHigh
  expr: histogram_quantile(0.95, sum(rate(rosocp_db_query_duration_seconds_bucket[5m])) by (le, operation)) > 2
  for: 10m
  labels:
    severity: warning

# Recommendation success rate dropping
- alert: ROSOCPRecommendationSuccessLow
  expr: sum(rate(rosocp_recommendation_success_total[5m])) / clamp_min(sum(rate(rosocp_recommendation_request_total[5m])), 1e-9) < 0.9
  for: 15m
  labels:
    severity: warning

# Cache hit rate too low
- alert: ROSOCPCacheHitRateLow
  expr: sum(rate(rosocp_fleet_summary_cache_hits_total[5m])) / clamp_min(sum(rate(rosocp_fleet_summary_cache_hits_total[5m])) + sum(rate(rosocp_fleet_summary_cache_misses_total[5m])), 1e-9) < 0.5
  for: 30m
  labels:
    severity: warning

# Ingest memory pressure
- alert: ROSOCPIngestMemoryPressure
  expr: rosocp_ingest_groups_in_memory > 10000
  for: 5m
  labels:
    severity: warning

# Reship stuck
- alert: ROSOCPReshipStuck
  expr: ros_reship_in_progress > 0
  for: 30m
  labels:
    severity: warning
```

---

## Quick Monitoring Checklist

Copy-paste PromQL for common operational questions.

### Is the processor consuming Kafka messages?

```promql
rate(rosocp_kafka_messages_processed_total[5m])
```

Expect a steady rate during active cluster uploads. Zero for extended periods with known upstream traffic indicates a stuck consumer.

### Are there ingestion errors?

```promql
sum by (stage) (rate(rosocp_ingestion_errors_total[5m]))
```

Breakdown by stage:

```promql
sum by (stage) (increase(rosocp_ingestion_errors_total[1h]))
```

### How fast is recommendation processing?

P95 by type:

```promql
histogram_quantile(0.95, sum by (type, le) (rate(rosocp_recommendation_duration_seconds_bucket[5m])))
```

Pipeline phases:

```promql
histogram_quantile(0.95, sum by (phase, le) (rate(rosocp_pipeline_phase_duration_seconds_bucket[5m])))
```

### Is the database healthy?

Error rate:

```promql
rate(rosocp_db_error_total[5m])
```

Query latency P95:

```promql
histogram_quantile(0.95, sum by (operation, le) (rate(rosocp_db_query_duration_seconds_bucket[5m])))
```

Partition gaps:

```promql
sum by (resource_name) (increase(rosocp_partition_missing_error_total[1h]))
```

### API latency and errors

P95 latency by route:

```promql
histogram_quantile(0.95, sum by (url, le) (rate(rosocp_request_duration_seconds_bucket[5m])))
```

5xx rate:

```promql
sum(rate(rosocp_requests_total{code=~"5.."}[5m]))
/
sum(rate(rosocp_requests_total[5m]))
```

### Is business-hours reship stuck?

In-flight reships (should return to 0 when idle):

```promql
ros_reship_in_progress
```

Stuck reships (non-zero for >30 minutes):

```promql
ros_reship_in_progress > 0
```

Failures and fallbacks:

```promql
increase(ros_reship_failures_total[24h])
increase(ros_reship_fallback_forward_only_total[24h])
```

Provider resolution issues:

```promql
sum by (reason) (rate(ros_reship_provider_resolution_failures_total[1h]))
```

### Retention running?

```promql
increase(rosocp_retention_partitions_dropped_total[24h])
```

---

## Health Endpoints

ROS-OCP Backend exposes three probe endpoints with distinct purposes:

| Endpoint | HTTP codes | Purpose | When to use |
|----------|------------|---------|-------------|
| `GET /status` | 200 | Trivial liveness — confirms the HTTP server is responding | Legacy/simple probes; no runtime diagnostics |
| `GET /healthz` | 200, 503 | Deep liveness — goroutine count, GC pause pressure, scheduler responsiveness | **Kubernetes liveness probes** (default in cost-onprem chart) |
| `GET /readyz` | 200, 503 | Readiness — PostgreSQL ping; optional Kafka/S3 when `ROS_READINESS_CHECK_*` enabled | **Kubernetes readiness probes** |

### `/healthz` response shape

```json
{
  "ok": true,
  "checks": {
    "goroutines": "42",
    "goroutines_status": "ok",
    "heap_alloc_mb": "18",
    "heap_sys_mb": "32",
    "gc_cycles": "15",
    "last_gc_pause_ms": "0.42",
    "gc_status": "ok",
    "scheduler": "ok"
  }
}
```

When `ok` is `false`, the endpoint returns HTTP 503. Kubernetes liveness probes will restart the pod.

### Configuration

| Variable | Default | Purpose |
|----------|---------|---------|
| `ROS_HEALTHZ_MAX_GOROUTINES` | `5000` | Fail `/healthz` when `runtime.NumGoroutine()` exceeds this threshold |
| `ROS_HEALTHZ_MAX_GC_PAUSE_MS` | `100` | Fail `/healthz` when the last GC pause exceeds this many milliseconds |

Tune thresholds for your deployment size. Small dev environments may need higher goroutine limits; latency-sensitive production pods may need lower GC pause limits.

---

## Known Gaps

| Gap | Impact | Workaround |
|-----|--------|------------|
| **Shallow readiness by default** | `/readyz` pings PostgreSQL only unless `ROS_READINESS_CHECK_KAFKA` / `ROS_READINESS_CHECK_S3` are enabled | Enable deep checks on processor/ingestion pods; monitor Kafka lag externally on API pods |
| `rosocp_threshold_recalc_coalesced_total` | Threshold settings PUT coalesced into follow-up recalc | Alert if rate is sustained high (settings churn) |
| **Snapshot write counter missing** | `rosocp_recommendations_written_total{type="snapshot"}` is never incremented despite duration being tracked | Use logs (`native snapshot engine: wrote N`) or DB row counts |
| **No OpenTelemetry tracing** | No distributed trace IDs across Kafka → processor → DB → API | Correlate via `request_id` and `org_id` log fields |
| **Pipeline phase coverage incomplete** | Help text lists `recommend`, `write`, `quality`, `history` phases but only `digest` and `gpu_enrichment` are instrumented | Use `rosocp_recommendation_duration_seconds` for end-to-end timing |
| **Kruize API duration not exported** | Histogram stub commented out in `internal/utils/utils.go` | Use Kruize-side metrics or log timestamps |

---

## Alerting Recommendations

Suggested Prometheus alerting rules (tune thresholds for your environment):

```yaml
groups:
  - name: ros-ocp-backend
    rules:
      - alert: ROSIngestionErrors
        expr: sum(rate(rosocp_ingestion_errors_total[5m])) > 0.1
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: ROS ingestion error rate elevated
          description: "Check stage breakdown: sum by (stage) (rate(rosocp_ingestion_errors_total[5m]))"

      - alert: ROSRecommendationLatencyHigh
        expr: |
          histogram_quantile(0.95, sum by (type, le) (rate(rosocp_recommendation_duration_seconds_bucket[5m]))) > 120
        for: 15m
        labels:
          severity: warning
        annotations:
          summary: ROS recommendation P95 latency > 2 minutes

      - alert: ROSKafkaProcessingStalled
        expr: rate(rosocp_kafka_messages_processed_total[15m]) == 0
        for: 30m
        labels:
          severity: warning
        annotations:
          summary: No Kafka messages processed in 30 minutes (verify upstream traffic)

      - alert: ROSDatabaseErrors
        expr: rate(rosocp_db_error_total[5m]) > 0
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: ROS database errors detected

      - alert: ROSPartitionMissing
        expr: increase(rosocp_partition_missing_error_total[1h]) > 0
        for: 0m
        labels:
          severity: warning
        annotations:
          summary: Missing PostgreSQL partition blocking writes

      - alert: ROSReshipStuck
        expr: ros_reship_in_progress > 0
        for: 30m
        labels:
          severity: warning
        annotations:
          summary: Business-hours reship in progress for >30 minutes

      - alert: ROSReshipFailures
        expr: increase(ros_reship_failures_total[1h]) > 0
        for: 0m
        labels:
          severity: warning
        annotations:
          summary: Business-hours reship retry budget exhausted

      - alert: ROSAPIErrorRate
        expr: |
          sum(rate(rosocp_requests_total{code=~"5.."}[5m]))
          / sum(rate(rosocp_requests_total[5m])) > 0.05
        for: 10m
        labels:
          severity: critical
        annotations:
          summary: ROS API 5xx rate above 5%

      - alert: ROSGPUUnrecognizedModels
        expr: increase(rosocp_gpu_model_unrecognized_total[24h]) > 0
        for: 0m
        labels:
          severity: info
        annotations:
          summary: Unrecognized GPU models detected — update GPU catalog
```

Combine with external alerts on Kafka consumer lag, PostgreSQL connection count, and pod restart rate.

---

## Related Documentation

- [Grafana dashboard ConfigMap](../../dashboards/grafana-dashboard-insights-rosocp-general.configmap.yaml) — dashboard JSON source
- [Operational Runbooks](runbooks.md) — step-by-step incident response
- [Retention Policy](retention.md) — data lifecycle and `rosocp_retention_partitions_dropped_total`
- [GPU Catalog Maintenance](gpu-catalog.md) — resolving `rosocp_gpu_model_unrecognized_total`
- [CONTRIBUTING.md](../../CONTRIBUTING.md) — local metrics ports and adding new metrics
