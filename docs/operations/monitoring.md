# Monitoring and Observability

Operational guide for ROS-OCP Backend metrics, logging, and alerting. Complements the runbooks in [runbooks.md](runbooks.md) with a complete metric catalog and scrape configuration.

**Last updated:** 2026-05-25

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
   - `GET /status` — liveness (returns `{"api-server":"working"}`)
   - `GET /readyz` — readiness (PostgreSQL pool ping; 503 if DB unavailable)
   - `GET /api/cost-management/v1/...` — recommendation API

2. **Metrics server** (`PROMETHEUS_PORT`) — dedicated listener in `internal/api/server.go`
   - `GET /metrics` — Prometheus scrape target (includes API middleware + all app metrics)

Implementation: `internal/api/server.go` starts `metricsEcho` on `PROMETHEUS_PORT` with `echoprometheus.NewHandler()`.

### Processor and recommendation-poller pods

A single HTTP server on `PROMETHEUS_PORT` (`internal/utils/utils.go` → `Start_prometheus_server()`):

| Path | Purpose |
|------|---------|
| `GET /metrics` | Prometheus scrape target |
| `GET /status` | Liveness (`{"status":"ok"}`) |
| `GET /readyz` | Readiness (DB pool ping) |

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

All application metrics use the `rosocp_` prefix except business-hours reship metrics (`ros_reship_*`, `ros_threshold_*`).

### Pipeline health and throughput

| Metric | Type | Labels | What it measures |
|--------|------|--------|------------------|
| `rosocp_kafka_messages_processed_total` | Counter | — | Kafka messages committed successfully after processing (processor and poller) |
| `rosocp_recommendations_written_total` | Counter | `type` | Recommendations persisted to PostgreSQL. `type`: `container`, `namespace`, `node`, `pvc`. **Note:** `snapshot` is observed for duration but not incremented on write (known gap) |
| `rosocp_recommendation_duration_seconds` | Histogram | `type` | End-to-end recommendation computation time. `type`: `container`, `node`, `gpu`, `namespace`, `pvc`, `snapshot` |
| `rosocp_pipeline_phase_duration_seconds` | Histogram | `phase` | Sub-phase timing within ingestion. Observed phases: `digest`, `gpu_enrichment` |
| `rosocp_rh_account_created_total` | Counter | — | New tenant accounts provisioned on first ingestion |

**Source files:** `internal/metrics/metrics.go`, `internal/services/report_processor.go`

### Error indicators

| Metric | Type | Labels | What it measures |
|--------|------|--------|------------------|
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
| `ros_threshold_recalculation_total` | Counter | `org_id`, `recommendation_type`, `status` | Async recalculations triggered by Settings API threshold changes. `status`: `success`, `error` |
| `ros_threshold_cache_entries` | Gauge | — | In-memory threshold resolution cache size (per org × recommendation type) |

**Source files:** `internal/engine/threshold_recalculate.go`, `internal/engine/threshold_metrics.go`

Threshold recalculation is gated by `ROS_THRESHOLD_RECALCULATION_ENABLED` (default `true`).

### Business-hours reship

| Metric | Type | Labels | What it measures |
|--------|------|--------|------------------|
| `ros_reship_in_progress` | Gauge | `org_id`, `cluster_uuid` | `1` while a masu `reship_ros` call is in flight, `0` otherwise |
| `ros_reship_files_processed` | Counter | `org_id` | ROS files published to Kafka by successful reships |
| `ros_reship_duration_seconds` | Histogram | `org_id` | HTTP duration of masu `reship_ros` calls |
| `ros_reship_failures_total` | Counter | `org_id` | Reship attempts that exhausted the consecutive retry budget |
| `ros_reship_provider_resolution_failures_total` | Counter | `org_id`, `reason` | Failures resolving `cluster_uuid` → `provider_uuid`. `reason`: `no_cost_model`, `masu_unavailable`, `not_found`, `timeout` |
| `ros_reship_fallback_forward_only_total` | Counter | `org_id` | Clusters transitioned to forward-only BH recommendations after retry exhaustion |

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

Pool tuning env vars: `ROS_DB_MAX_CONNS` (default `10`), `ROS_DB_ACQUIRE_TIMEOUT_SECS` (default `5`).

### GPU

| Metric | Type | Labels | What it measures |
|--------|------|--------|------------------|
| `rosocp_gpu_model_unrecognized_total` | Counter | `model_name` | GPU model strings not matched in `internal/engine/gpu_catalog.yaml` |

GPU recommendation duration is recorded under `rosocp_recommendation_duration_seconds{type="gpu"}` (API enrichment path in `internal/api/handlers_node_recs.go`).

### Retention

| Metric | Type | Labels | What it measures |
|--------|------|--------|------------------|
| `rosocp_retention_partitions_dropped_total` | Counter | — | Monthly partitions dropped by the retention sweep |

Retention is controlled by `ROS_RETENTION_MONTHS`, `ROS_HISTORY_RETENTION_DAYS`, `ROS_STALE_ARCHIVE_DAYS`, `ROS_SNAPSHOT_INVENTORY_RETENTION_H`. See [retention.md](retention.md).

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
| `API_PORT` | `8000` | API listener; `/status` and `/readyz` for API probes |
| `CW_LOG_STREAM_NAME` | `rosocp` | CloudWatch log stream (when CW credentials configured) |
| `ROS_DB_MAX_CONNS` | `10` | Connection pool size (affects DB latency under load) |
| `ROS_DB_ACQUIRE_TIMEOUT_SECS` | `5` | Pool acquire timeout; `0` = unlimited |
| `ROS_RESHIP_POLLER_INTERVAL_SECS` | `60` | Background reship retry interval |
| `ROS_RESHIP_MAX_RETRIES` | `10` | Consecutive reship failures before giving up |
| `ROS_THRESHOLD_RECALCULATION_ENABLED` | `true` | Gate threshold-change recalc metrics and background work |
| `ROS_BUSINESS_HOURS_ENABLED` | `true` | Enables reship poller and BH metrics on API deployment |
| `KAFKA_AUTO_COMMIT` | `false` | Affects redelivery behavior on crash (see [runbooks.md](runbooks.md)) |

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

In-flight reships (should return to 0):

```promql
sum(ros_reship_in_progress)
```

Stuck reships (>30 minutes):

```promql
ros_reship_in_progress == 1
```

Failures and fallbacks:

```promql
sum by (org_id) (increase(ros_reship_failures_total[24h]))
sum by (org_id) (increase(ros_reship_fallback_forward_only_total[24h]))
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

## Known Gaps

| Gap | Impact | Workaround |
|-----|--------|------------|
| **No `/healthz`** | Liveness probes use `/status`, which does not detect deadlocks or goroutine leaks | Monitor process restarts; consider external watchdog |
| **No Kafka-aware readiness** | `/readyz` only pings PostgreSQL; pod may be "ready" while Kafka is unreachable | Monitor `rosocp_kafka_messages_processed_total` and consumer lag externally |
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
        expr: ros_reship_in_progress == 1
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

- [Operational Runbooks](runbooks.md) — step-by-step incident response
- [Retention Policy](retention.md) — data lifecycle and `rosocp_retention_partitions_dropped_total`
- [GPU Catalog Maintenance](gpu-catalog.md) — resolving `rosocp_gpu_model_unrecognized_total`
- [CONTRIBUTING.md](../../CONTRIBUTING.md) — local metrics ports and adding new metrics
