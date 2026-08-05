# Monitoring and Observability

> **Last verified:** 2026-08-05

This guide helps operators deploy, scrape, and troubleshoot ROS-OCP Backend using Prometheus metrics and structured logs.

ROS-OCP Backend runs as three processes — **API**, **processor**, and **recommendation poller** — each exposing a Prometheus `/metrics` endpoint. The API additionally exposes HTTP request metrics for REST traffic.

---

## What to Scrape

### Deployment layout

| Component | Scrape path | Default metrics port |
|-----------|-------------|----------------------|
| **ROS API** | `/metrics` | `9000` (Helm/Clowder); `5007` local dev |
| **ROS processor** | `/metrics` | `9000` (Helm/Clowder); `5005` local dev |
| **ROS recommendation poller** | `/metrics` | `9000` (Helm/Clowder); `5006` local dev |

Set `PROMETHEUS_PORT` to match the container metrics port exposed by your Service or ServiceMonitor. On Red Hat OpenShift with the cost-onprem chart, all three components use port **9000** with ServiceMonitor resources scraping `/metrics` every 30 seconds (configurable via `monitoring.scrapeInterval`).

### Health and readiness probes

| Endpoint | Port | Component | Purpose |
|----------|------|-----------|---------|
| `/status` | API port (`8000`) or metrics port | All | Trivial liveness — HTTP server is responding |
| `/healthz` | API port or metrics port | All | Deep liveness — goroutine count, GC pause, scheduler responsiveness |
| `/readyz` | API port or metrics port | All | Readiness — PostgreSQL ping; optional Kafka/S3 when `ROS_READINESS_CHECK_*` enabled |
| `/metrics` | `PROMETHEUS_PORT` | All | Prometheus scrape |

Kubernetes liveness probes should target `/healthz` (default in the cost-onprem chart). Use `/status` only when you need a trivial always-200 probe with no runtime diagnostics.

The API runs **two listeners**: the main API port serves REST traffic, `/healthz`, and `/readyz`; a separate metrics listener on `PROMETHEUS_PORT` serves `/metrics` (including API latency histograms).

Processor and poller serve `/metrics`, `/status`, `/healthz`, and `/readyz` on a single metrics port.

---

## Key Metrics

Metrics use the `rosocp_` prefix unless noted. Standard Go runtime metrics (`process_*`, `go_*`) are also exported.

### Pipeline health

| Metric | Type | Labels | What to watch |
|--------|------|--------|---------------|
| `rosocp_kafka_messages_processed_total` | Counter | — | Throughput — should increase when clusters upload data |
| `rosocp_kafka_retries_total` | Counter | — | Messages requeued for retry (precursor to DLQ routing) |
| `rosocp_recommendations_written_total` | Counter | `type` | Recommendations saved (`container`, `namespace`, `node`, `pvc`) |
| `rosocp_recommendation_duration_seconds` | Histogram | `type` | How long each recommendation domain takes |
| `rosocp_pipeline_phase_duration_seconds` | Histogram | `phase` | Per-phase pipeline timing (`download`, `parse_digest`, `write_digests`, `recommend`, `write_recommendations`, `post_process`, `metadata_refresh`) |
| `rosocp_pipeline_total_duration_seconds` | Histogram | `status` | End-to-end manifest processing (`success` or `error`) |
| `rosocp_rh_account_created_total` | Counter | — | New tenant accounts provisioned on first ingestion |
| `rosocp_kafka_consumer_lag` | Gauge | `topic`, `partition` | Per-partition lag (high watermark minus committed offset) for assigned partitions. Stale labels are cleaned on rebalance. |
| `rosocp_kafka_consumer_lag_total` | Gauge | `topic` | Aggregate consumer lag across all assigned partitions. `sum(rosocp_kafka_consumer_lag)` across replicas gives the cluster-wide total. |

**Is it processing?**

```promql
rate(rosocp_kafka_messages_processed_total[5m])
```

**How many recommendations per hour?**

```promql
sum by (type) (increase(rosocp_recommendations_written_total[1h]))
```

### Errors

| Metric | Type | Labels | What to watch |
|--------|------|--------|---------------|
| `rosocp_kafka_dlq_messages_total` | Counter | — | Messages sent to the Dead Letter Queue after max retries |
| `rosocp_ingestion_errors_total` | Counter | `stage` | Pipeline failures: `csv_parse`, `digest`, `recommend`, `write` |
| `rosocp_invalid_csv_total` | Counter | — | Bad container CSV from upstream |
| `rosocp_invalid_namespace_csv_total` | Counter | — | Bad namespace CSV |
| `rosocp_invalid_datapoints_total` | Counter | — | Invalid rows within otherwise parseable CSVs |
| `rosocp_csv_fetch_error_total` | Counter | — | Failed S3/HTTP downloads |
| `rosocp_db_error_total` | Counter | — | Database errors |
| `rosocp_partition_missing_error_total` | Counter | `resource_name` | Missing monthly table partition |
| `rosocp_quality_partition_missing_total` | Counter | — | Quality-metrics write failed due to missing partition |
| `ros_ocp_plugin_hook_errors_total` | Counter | `plugin`, `hook_type` | Non-fatal plugin hook failures |

**Any errors?**

```promql
sum by (stage) (rate(rosocp_ingestion_errors_total[5m]))
```

### API performance

Available on the API metrics scrape only:

| Metric | Type | Labels | What to watch |
|--------|------|--------|---------------|
| `rosocp_requests_total` | Counter | `code`, `method`, `host`, `url` | Request volume and status codes |
| `rosocp_request_duration_seconds` | Histogram | `code`, `method`, `host`, `url` | Latency per route template |
| `rosocp_request_size_bytes` | Histogram | same | Request payload sizes |
| `rosocp_response_size_bytes` | Histogram | same | Response payload sizes |

**P95 API latency by route:**

```promql
histogram_quantile(0.95, sum by (url, le) (rate(rosocp_request_duration_seconds_bucket[5m])))
```

**5xx error ratio:**

```promql
sum(rate(rosocp_requests_total{code=~"5.."}[5m]))
/ sum(rate(rosocp_requests_total[5m]))
```

### Database

| Metric | Type | Labels | What to watch |
|--------|------|--------|---------------|
| `rosocp_db_query_duration_seconds` | Histogram | `operation` | Query latency |

### Connection pool (pgxpool)

GORM and direct pgxpool access share one pool per process. Metrics reflect `pool.Stat()` at scrape time.

| Metric | Type | What it measures |
|--------|------|------------------|
| `rosocp_db_pool_total_conns` | Gauge | Total connections (acquired + idle + constructing) |
| `rosocp_db_pool_acquired_conns` | Gauge | Connections currently in use |
| `rosocp_db_pool_idle_conns` | Gauge | Idle connections |
| `rosocp_db_pool_max_conns` | Gauge | Configured max connections (`ROS_DB_MAX_CONNS`) |
| `rosocp_db_pool_acquire_count_total` | Counter | Cumulative successful acquires |
| `rosocp_db_pool_acquire_duration_seconds` | Counter | Cumulative time spent acquiring connections |

### Ingestion streaming

| Metric | Type | Labels | What to watch |
|--------|------|--------|---------------|
| `rosocp_ingest_groups_in_memory` | Gauge | — | Digest groups buffered during CSV ingest; sustained high values may indicate flush batch size is too large |
| `rosocp_ingest_flush_total` | Counter | — | Incremental digest flush count |
| `rosocp_ingest_flush_duration_seconds` | Histogram | — | Time spent in each incremental flush |
| `rosocp_ingest_manifest_id_synthesized_total` | Counter | — | Legacy Kafka messages without `metadata.manifest_id` that received a synthesized `synth-*` manifest ID for per-file tracking |
| `rosocp_manifest_recommendation_deferred_total` | Counter | — | Recommendation runs deferred for synthesized manifest IDs pending the quiet period |
| `rosocp_internal_endpoint_calls_total` | Counter | `endpoint`, `sa_name` | Internal platform endpoint invocations. Target `org_id` is logged per call. |
| `rosocp_analytics_incomplete_total` | Counter | `error_type` | Analytics write failures during container ingestion. `error_type`: `history` or `quality`. Per-org/cluster context in structured logs. Increments in degraded mode (`ROS_INGEST_STRICT_ANALYTICS=false`); strict mode retries instead. |

### Koku effective-rates cache

| Metric | Type | What to watch |
|--------|------|---------------|
| `rosocp_cost_cache_size` | Gauge | Cached org×cluster effective-rates entries; should stay below `ROS_COST_CACHE_MAX_ENTRIES` |
| `rosocp_cost_cache_removals_total` | Counter | LRU removals (eviction or expiry); sustained growth may mean raising `ROS_COST_CACHE_MAX_ENTRIES` |

**DB healthy?**

```promql
rate(rosocp_db_error_total[5m]) == 0
```

### GPU

| Metric | Type | Labels | What to watch |
|--------|------|--------|---------------|
| `rosocp_gpu_model_unrecognized_total` | Counter | — | GPU models missing from catalog (aggregate count; check logs for `gpu_metadata: unrecognized GPU model` to identify specific models) |

### Business hours reship

When business-hours optimization is enabled, the API runs a background reship poller that backfills historical data via Koku Masu.

| Metric | Type | Labels | What to watch |
|--------|------|--------|---------------|
| `ros_reship_in_progress` | Gauge | — | Concurrent masu reship calls in flight (fleet-wide). Should return to 0 when idle. |
| `ros_reship_files_processed` | Counter | — | Files successfully republished |
| `ros_reship_duration_seconds` | Histogram | — | Masu HTTP call duration |
| `ros_reship_failures_total` | Counter | — | Retry budget exhausted |
| `ros_reship_provider_resolution_failures_total` | Counter | `reason` | Cannot map cluster to provider |
| `ros_reship_fallback_forward_only_total` | Counter | — | Fell back to forward-only recommendations |

**Reship stuck?**

```promql
ros_reship_in_progress > 0
```

(Alert if the gauge stays above 0 for more than 30 minutes.)

### Threshold recalculation

When tenants change recommendation thresholds via the Settings API:

| Metric | Type | Labels | What to watch |
|--------|------|--------|---------------|
| `ros_threshold_recalculation_total` | Counter | `recommendation_type`, `status` | `success`, `error`, or **`skipped`** (cluster hash unchanged) |
| `ros_threshold_cache_entries` | Gauge | — | In-memory threshold resolution cache size |

**Skip behavior:** After a Settings PUT, ROS compares each cluster's stored
threshold hash against the new settings. Unchanged clusters increment
`status=skipped` instead of re-running the engine. Tune parallel fan-out with
`ROS_THRESHOLD_RECALC_CONCURRENCY` (default `3`). Disable recalc entirely with
`ROS_THRESHOLD_RECALCULATION_ENABLED=false`.

**High skip ratio is healthy** — it means redundant work is being avoided:

```promql
sum(rate(ros_threshold_recalculation_total{status="skipped"}[1h]))
/ sum(rate(ros_threshold_recalculation_total[1h]))
```

### Kafka parallel workers

Parallel ingestion is controlled by `ROS_KAFKA_PARALLEL` (default `true`) and
`ROS_KAFKA_WORKERS` (default `3`). There is no dedicated worker-pool gauge;
throughput appears on `rosocp_kafka_messages_processed_total`. The processor
logs batch progress every 100 messages at INFO level.

**Tuning signals:**

- Flat `rate(rosocp_kafka_messages_processed_total[5m])` with rising external
  consumer lag → increase workers or check DB pool limits (`ROS_DB_MAX_CONNS`).
- Rising `rosocp_ingestion_errors_total` after raising workers → pool
  saturation; reduce workers or increase `ROS_DB_MAX_CONNS`.

### RBAC permission cache

The API caches RBAC permission lookups in memory keyed by `X-Rh-Identity`.
TTL is set by `ROS_RBAC_CACHE_TTL` (default **60 seconds**; `0` disables).
Capacity is capped by `ROS_RBAC_CACHE_MAX_ENTRIES` (default **500**).

| Metric | Type | What to watch |
|--------|------|---------------|
| `rosocp_rbac_cache_size` | Gauge | Current cached RBAC entries; should stay below max |
| `rosocp_rbac_cache_removals_total` | Counter | LRU removals (eviction or expiry) |

Observable effects:

- Lower RBAC service request rate at steady API traffic.
- Permission changes may take up to TTL seconds to propagate for cached identities.
- Set `ROS_RBAC_CACHE_TTL=0` temporarily when debugging authorization issues.

### Fleet summary cache

`GET /recommendations/openshift/fleet-summary` and default (non-`group_by`) `GET /recommendations/openshift/savings-summary` responses are cached in memory keyed by org, RBAC scope, and (for savings) `engine`/`term`. TTL: `ROS_FLEET_SUMMARY_CACHE_TTL` (default **300 seconds**). Capacity: `ROS_FLEET_SUMMARY_CACHE_CAPACITY` (default **256**) per cache. The fleet heatmap has its own LRU cache: `ROS_FLEET_HEATMAP_CACHE_CAPACITY` (default **128**; entries are large — ~200 bytes × `ROS_FLEET_HEATMAP_MAX_NODES` each, so 128 × 1000 nodes ≈ 25 MB). All caches are invalidated on recommendation ingest, threshold/business-hours settings changes, and savings recalculation triggers.

| Metric | Type | What to watch |
|--------|------|---------------|
| `rosocp_fleet_summary_cache_size` | Gauge | Current cached fleet summary entries |
| `rosocp_fleet_summary_cache_hits_total` | Counter | Successful fleet cache lookups |
| `rosocp_fleet_summary_cache_misses_total` | Counter | Fleet cache misses and expired entries |
| `rosocp_fleet_summary_cache_removals_total` | Counter | LRU removals (eviction or expiry) for fleet cache |
| `rosocp_fleet_summary_cache_invalidations_total` | Counter | Explicit org invalidations for fleet cache after data mutations |
| `rosocp_savings_summary_cache_size` | Gauge | Current cached savings summary entries |
| `rosocp_savings_summary_cache_hits_total` | Counter | Successful savings summary cache lookups |
| `rosocp_savings_summary_cache_misses_total` | Counter | Savings summary cache misses and expired entries |
| `rosocp_savings_summary_cache_removals_total` | Counter | LRU removals (eviction or expiry) for savings cache |
| `rosocp_savings_summary_cache_invalidations_total` | Counter | Explicit org invalidations for savings cache after data mutations |

### Async job coalescing

Per-org single-flight guards deduplicate rapid savings recalc, reship, and threshold recalc triggers.

| Metric | Type | Labels | What to watch |
|--------|------|--------|---------------|
| `rosocp_threshold_recalc_coalesced_total` | Counter | `recommendation_type` | Settings churn coalesced into follow-up recalc |
| `rosocp_savings_recalc_coalesced_total` | Counter | — | Duplicate savings recalc triggers while in-flight |
| `rosocp_reship_coalesced_total` | Counter | — | Duplicate business-hours reship triggers while in-flight |

### Recommendation quality (processor)

Updated after each container ingestion quality write. Per-org/cluster aggregates are logged structurally.

| Metric | Type | Labels | What to watch |
|--------|------|--------|---------------|
| `ros_recommendation_oom_rate` | Histogram | — | Mean OOM events per quality batch |
| `ros_recommendation_stability` | Histogram | — | Mean stability per batch (0–100; API uses 0.0–1.0) |
| `ros_recommendation_adoption_rate` | Histogram | — | Mean adoption rate per batch (0–100) |

### Retention

| Metric | Type | Labels | What to watch |
|--------|------|--------|---------------|
| `rosocp_retention_partitions_dropped_total` | Counter | — | Partitions dropped by daily sweep |

Controlled by `ROS_RETENTION_MONTHS`, `ROS_SAMPLE_RETENTION_DAYS`, `ROS_HISTORY_RETENTION_DAYS`, and related env vars.

### Legacy Kruize path

If the recommendation poller is deployed (Kruize mode), additional counters track Kruize API calls and invalid responses (`rosocp_kruize_api_exception_total`, `kruize_*_request_total`, etc.). These are zero when running the native Go engine only.

### Legacy / non-prefixed metrics

Some counters predate the `rosocp_` naming convention and retain their original names:

| Metric | Type | Labels | What to watch |
|--------|------|--------|---------------|
| `ros_ingestion_file_failures_total` | Counter | `report_type`, `error_class` | Permanent per-file ingestion failures recorded in `report_file_status` |
| `ros_savings_recalculation_total` | Counter | `recommendation_type`, `status` | Cost-model-triggered savings-only recalculations (`success`, `error`) |

---

## Logging

### Format

- **Production:** JSON to stdout (default in Clowder/OpenShift deployments)
- **Development:** Plain text when `LogFormater=text`
- **Levels:** `DEBUG`, `INFO` (default), `ERROR` via `LOG_LEVEL`

Each log entry includes a `service` field (`ros-api`, `rosocp-processor`, etc.) from `SERVICE_NAME`.

### Fields for correlation

| Field | Use |
|-------|-----|
| `org_id` | Tenant isolation |
| `cluster_uuid` | Target cluster |
| `request_id` | Trace a single upload or API call |
| `msg` | Stable message identifier (reship, threshold recalc) |

### Important log messages

| Message | Severity | Meaning |
|---------|----------|---------|
| `unable to read CSV from URL` | ERROR | Upstream payload download failed |
| `unable to process ... error` | ERROR | CSV parse failure |
| `readyz: database ping failed` | WARN | Database unreachable — readiness fails |
| `native engine: analytics pipeline incomplete` | WARN | History/quality write failed; recommendations still saved |
| `cost data fetch failed` | WARN | Savings estimates unavailable (non-fatal) |
| `gpu_metadata: unrecognized GPU model` | WARN | Update GPU catalog |
| `reship max retries exceeded` | ERROR | Business-hours backfill failed |
| `threshold recalculation failed` | WARN | Settings change recalc error for one cluster |

Aggregate logs by `org_id` and `request_id` in your log platform (Loki, Elasticsearch, CloudWatch).

---

## Configuration

Performance and observability-related environment variables. For the complete
reference (database, Kafka, thresholds, plugins), see
[Configuration Reference](configuration.md).

| Environment variable | Default | Purpose |
|---------------------|---------|---------|
| `LOG_LEVEL` | `INFO` | Verbosity |
| `LogFormater` | JSON in prod, `text` locally | Output format |
| `SERVICE_NAME` | `rosocp` | Log `service` field |
| `PROMETHEUS_PORT` | `5005` / `9000` | Metrics listener port |
| `API_PORT` | `8000` | REST API port |
| `ROS_KAFKA_PARALLEL` | `true` | Parallel Kafka processing |
| `ROS_KAFKA_WORKERS` | `3` | Kafka worker goroutines |
| `KAFKA_LAG_POLL_INTERVAL_SECONDS` | `30` | Polling interval for consumer lag metrics |
| `ROS_RBAC_CACHE_TTL` | `60` | RBAC cache TTL (seconds; `0` = off) |
| `ROS_DB_MAX_CONNS` | `10` | DB pool size |
| `ROS_DB_MIN_CONNS` | `2` | DB pool minimum |
| `ROS_DB_ACQUIRE_TIMEOUT_SECS` | `5` | Pool wait timeout |
| `ROS_THRESHOLD_RECALC_CONCURRENCY` | `3` | Parallel cluster threshold recalc |
| `ROS_RESHIP_CONCURRENCY` | `2` | Parallel masu reship calls |
| `ROS_RESHIP_POLLER_INTERVAL_SECS` | `60` | Reship retry interval |
| `ROS_RESHIP_MAX_RETRIES` | `10` | Max consecutive reship failures |
| `ROS_THRESHOLD_RECALCULATION_ENABLED` | `true` | Threshold-change recalc |
| `ROS_BUSINESS_HOURS_ENABLED` | `true` | Business-hours feature and reship poller |

---

## Grafana Dashboard

The **ROSOCP** dashboard ships with the repository as a Kubernetes ConfigMap (`dashboards/grafana-dashboard-insights-rosocp-general.configmap.yaml`). When deployed via the cost-onprem Helm chart (or any environment with Grafana's dashboard sidecar), it is **auto-imported** — look for a dashboard titled **ROSOCP** (UID `ofxxAX0nk`) in Grafana.

### Accessing the dashboard

**Helm / OpenShift (recommended):** The chart applies the ConfigMap with label `grafana_dashboard: "true"`. Grafana's sidecar watches for this label and imports the dashboard automatically. No manual steps are required if your cluster runs the standard Grafana sidecar pattern.

**Manual import:** Extract the JSON from the `ROSOCP.json` key in `dashboards/grafana-dashboard-insights-rosocp-general.configmap.yaml` and import it through Grafana → Dashboards → Import.

**Prometheus datasource:** Point the `datasource` template variable at the Prometheus instance that scrapes ROS pods. Scrape targets:

| Component | Path | Port |
|-----------|------|------|
| ROS API | `/metrics` | `PROMETHEUS_PORT` (typically `9000`) |
| ROS processor | `/metrics` | `PROMETHEUS_PORT` |
| ROS recommendation poller | `/metrics` | `PROMETHEUS_PORT` |

The cost-onprem chart creates ServiceMonitor resources that scrape `/metrics` every 30 seconds (configurable via `monitoring.scrapeInterval`).

!!! note "SaaS-only panels"
    Panels that use **RDS free storage** (`DatasourceRDS`) or **Kafka consumer lag via CloudWatch** (`cloudwatch` variable) apply to Red Hat SaaS deployments only. On-prem operators can ignore these panels or leave the variables unset — the application-metric rows still work.

!!! note "Kafka lag panels"
    The processor exports `rosocp_kafka_consumer_lag` per assigned partition. For cluster-wide lag in multi-replica deployments, use `sum(rosocp_kafka_consumer_lag)`. In SaaS, external lag from the CloudWatch exporter is also available. On-prem, the application-side lag metric is sufficient for alerting; alternatively configure Strimzi/Kafka metrics for broker-side lag.

### Dashboard sections

The dashboard is organized into row sections. Each row groups related panels:

| Section | What it shows |
|---------|---------------|
| **Kafka & Ingestion** | Consumer lag, CSV validation errors, ingestion file failures by `report_type`/`error_class`, rows skipped, DLQ messages |
| **Recommendations** | Container and namespace recommendation request/success rates and success ratios |
| **Quality & Stability** | Analytics incomplete by `error_type`, recommendation stability/adoption/OOM histograms (p50/p95) |
| **Reship** | In-progress gauge, duration, failures, fallback, provider resolution failures, coalesced |
| **Recalculation** | Threshold/savings recalculation by `recommendation_type`/`status`, coalesced counters |
| **Engine Performance** | DB query latency p95 by `operation`, recommendation duration p95 by `type`, pipeline phase duration p95 by `phase`, recommendations written by `type` |
| **Streaming Ingest** | Groups in memory gauge, flush rate, flush duration p95 |
| **Caches** | Fleet/savings cache hit rates, cache sizes (fleet, savings, cost, RBAC, threshold), eviction rates |
| **Data Lifecycle** | Retention partitions dropped, manifest recommendation deferred (debounced), manifest IDs synthesized |
| **GPU & Plugins** | Unrecognized GPU models by `model_name`, plugin hook errors, invalid data points |
| **API HTTP** | Request latency p95, request rate by status code |
| **Infrastructure** | RH accounts created, DB errors, partition errors, RDS free storage, internal endpoint calls, OOMKilled, container restarts, pod memory |

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

### Example PromQL (custom panels)

If you build your own panels instead of using the shipped dashboard:

**Ingestion throughput (time series)**

```promql
rate(rosocp_kafka_messages_processed_total[5m])
```

**Recommendation latency P95 (multi-line)**

```promql
histogram_quantile(0.95, sum by (type, le) (rate(rosocp_recommendation_duration_seconds_bucket[5m])))
```

**API latency P95 top routes (table)**

```promql
topk(10, histogram_quantile(0.95, sum by (url, le) (rate(rosocp_request_duration_seconds_bucket[5m]))))
```

**Reship in progress (stat — should be 0)**

```promql
ros_reship_in_progress
```

---

## Troubleshooting via Metrics

Use the **ROSOCP** Grafana dashboard sections above as your starting point. The scenarios below map common symptoms to the relevant dashboard rows.

### Symptom: Recommendations all zero

1. Open **Kafka & Ingestion** — check ingestion file failures, errors by stage, and DLQ messages.
2. Verify `rate(rosocp_kafka_messages_processed_total[5m])` is non-zero.
3. Check external Kafka consumer lag if available.
4. Verify `/readyz` returns 200 (database connectivity).

### Symptom: API slow

1. Open **Engine Performance** — DB query latency p95 and recommendation duration p95.
2. Open **Caches** — fleet and savings cache hit rates (below 50% means repeated DB work).
3. Open **API HTTP** — request latency p95 and error rate by status code.

### Symptom: Pods restarting

1. Open **Infrastructure** — OOMKilled containers and pod memory working set.
2. Compare memory working set to pod limits; check container restart rate by component.
3. If OOM-related, also check **Streaming Ingest → Groups in memory** for ingest backpressure.

### Symptom: Costs not updating

1. Open **Reship** — in-progress gauge, failures, provider resolution failures.
2. Open **Recalculation** — savings recalculation by type and status.
3. Look for log lines `cost data fetch failed` if dollar fields are zero (savings estimates depend on Koku Masu).

### Symptom: No new recommendations (detailed)

1. Check `rate(rosocp_kafka_messages_processed_total[5m])` — zero means the processor is not consuming.
2. Check `sum(rosocp_kafka_consumer_lag)` — rising lag with zero throughput confirms a stuck consumer.
3. Check `rosocp_ingestion_errors_total` by stage for parse/digest/write failures.
4. Verify `/readyz` returns 200 (database connectivity).

### Symptom: High latency (detailed)

1. Compare P95 across `rosocp_recommendation_duration_seconds` types to find the slow domain.
2. Check `rosocp_pipeline_phase_duration_seconds` for slow `download`, `parse_digest`, or `recommend` phases.
3. Check `rosocp_db_query_duration_seconds` for database bottlenecks.
4. Review API `rosocp_request_duration_seconds` if UI/API feels slow.

### Symptom: Missing savings estimates

Not directly metric-driven — look for log lines `cost data fetch failed`. Verify `KOKU_MASU_URL` and `ROS_SAVINGS_ESTIMATES_ENABLED=true`. Recommendations still write; only dollar fields are affected.

### Symptom: Business-hours reship not completing

1. `ros_reship_in_progress > 0` for extended period → stuck HTTP call to Masu.
2. `ros_reship_provider_resolution_failures_total` → cluster not mapped to a Koku provider or no cost model.
3. `ros_reship_failures_total` increasing → retries exhausted; check `ros_reship_fallback_forward_only_total`.

### Symptom: GPU recommendations incomplete

1. `rosocp_gpu_model_unrecognized_total` increasing → check logs for `gpu_metadata: unrecognized GPU model` to identify specific model strings, then add model variants to the GPU catalog.
2. Check `rosocp_recommendation_duration_seconds{type="gpu"}` for slow enrichment.

---

## Known Limitations

| Limitation | Operator impact |
|------------|-----------------|
| Readiness is shallow by default | Enable `ROS_READINESS_CHECK_KAFKA` / `ROS_READINESS_CHECK_S3` on processor pods |
| No distributed tracing | Correlate with `request_id` in logs |
| Snapshot write counter missing | Use logs or DB queries for snapshot output volume |

---

## Related Documentation

- [Grafana dashboard source](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/dashboards/grafana-dashboard-insights-rosocp-general.configmap.yaml) — ConfigMap JSON for manual import
- [Configuration Reference](configuration.md)
- [Upgrade Runbook](operations/upgrade-runbook.md)
- [Known Issues](known-issues.md)
- [Configurability Reference](architecture/configurability.md)
- [Business Hours](features/business-hours.md)
