# Monitoring and Observability

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
| `/status` | API port (`8000`) or metrics port | All | Liveness — process is running |
| `/readyz` | API port or metrics port | All | Readiness — PostgreSQL ping; optional Kafka/S3 when `ROS_READINESS_CHECK_*` enabled |
| `/metrics` | `PROMETHEUS_PORT` | All | Prometheus scrape |

!!! note "No `/healthz` endpoint"
    Kubernetes liveness probes should target `/status`. There is no dedicated `/healthz` endpoint with deadlock or goroutine checks.

The API runs **two listeners**: the main API port serves REST traffic and `/readyz`; a separate metrics listener on `PROMETHEUS_PORT` serves `/metrics` (including API latency histograms).

Processor and poller serve `/metrics`, `/status`, and `/readyz` on a single metrics port.

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
| `rosocp_pipeline_phase_duration_seconds` | Histogram | `phase` | Ingestion sub-phases (`digest`, `gpu_enrichment`) |
| `rosocp_rh_account_created_total` | Counter | — | New tenant accounts provisioned on first ingestion |

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
| `rosocp_internal_endpoint_calls_total` | Counter | `endpoint`, `org_id`, `sa_name` | Internal platform endpoint invocations for cross-tenant audit |
| `rosocp_analytics_incomplete_total` | Counter | `org_id`, `cluster_uuid`, `error_type` | Analytics write failures during container ingestion. `error_type`: `history` or `quality`. Increments in degraded mode (`ROS_INGEST_STRICT_ANALYTICS=false`); strict mode retries instead. |

### Koku effective-rates cache

| Metric | Type | What to watch |
|--------|------|---------------|
| `rosocp_cost_cache_size` | Gauge | Cached org×cluster effective-rates entries; should stay below `ROS_COST_CACHE_MAX_ENTRIES` |
| `rosocp_cost_cache_evictions_total` | Counter | LRU evictions when cache is full; sustained growth may mean raising `ROS_COST_CACHE_MAX_ENTRIES` |

**DB healthy?**

```promql
rate(rosocp_db_error_total[5m]) == 0
```

### GPU

| Metric | Type | Labels | What to watch |
|--------|------|--------|---------------|
| `rosocp_gpu_model_unrecognized_total` | Counter | `model_name` | GPU models missing from catalog |

### Business hours reship

When business-hours optimization is enabled, the API runs a background reship poller that backfills historical data via Koku Masu.

| Metric | Type | Labels | What to watch |
|--------|------|--------|---------------|
| `ros_reship_in_progress` | Gauge | `org_id`, `cluster_uuid` | Should return to 0 after each reship |
| `ros_reship_files_processed` | Counter | `org_id` | Files successfully republished |
| `ros_reship_duration_seconds` | Histogram | `org_id` | Masu HTTP call duration |
| `ros_reship_failures_total` | Counter | `org_id` | Retry budget exhausted |
| `ros_reship_provider_resolution_failures_total` | Counter | `org_id`, `reason` | Cannot map cluster to provider |
| `ros_reship_fallback_forward_only_total` | Counter | `org_id` | Fell back to forward-only recommendations |

**Reship stuck?**

```promql
ros_reship_in_progress == 1
```

(Alert if any gauge stays at 1 for more than 30 minutes.)

### Threshold recalculation

When tenants change recommendation thresholds via the Settings API:

| Metric | Type | Labels | What to watch |
|--------|------|--------|---------------|
| `ros_threshold_recalculation_total` | Counter | `org_id`, `recommendation_type`, `status` | `success`, `error`, or **`skipped`** (cluster hash unchanged) |
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
| `rosocp_rbac_cache_evictions_total` | Counter | LRU evictions when cache is full |

Observable effects:

- Lower RBAC service request rate at steady API traffic.
- Permission changes may take up to TTL seconds to propagate for cached identities.
- Set `ROS_RBAC_CACHE_TTL=0` temporarily when debugging authorization issues.

### Fleet summary cache

`GET /recommendations/openshift/fleet-summary` responses are cached in memory keyed by org and RBAC scope. TTL: `ROS_FLEET_SUMMARY_CACHE_TTL` (default **300 seconds**). Capacity: `ROS_FLEET_SUMMARY_CACHE_CAPACITY` (default **256**). The cache is invalidated on recommendation ingest, threshold/business-hours settings changes, and savings recalculation triggers.

| Metric | Type | What to watch |
|--------|------|---------------|
| `rosocp_fleet_summary_cache_size` | Gauge | Current cached fleet summary entries |
| `rosocp_fleet_summary_cache_hits_total` | Counter | Successful cache lookups |
| `rosocp_fleet_summary_cache_misses_total` | Counter | Misses and expired entries |
| `rosocp_fleet_summary_cache_evictions_total` | Counter | LRU evictions when cache is full |
| `rosocp_fleet_summary_cache_invalidations_total` | Counter | Explicit org invalidations after data mutations |
| `rosocp_fleet_summary_cache_lazy_expiry_total` | Counter | TTL expiry removals on read |

### Async job coalescing

Per-org single-flight guards deduplicate rapid savings recalc, reship, and threshold recalc triggers.

| Metric | Type | Labels | What to watch |
|--------|------|--------|---------------|
| `rosocp_threshold_recalc_coalesced_total` | Counter | `org_id`, `recommendation_type` | Settings churn coalesced into follow-up recalc |
| `rosocp_savings_recalc_coalesced_total` | Counter | `org_id` | Duplicate savings recalc triggers while in-flight |
| `rosocp_reship_coalesced_total` | Counter | `org_id` | Duplicate business-hours reship triggers while in-flight |

### Recommendation quality (processor)

Updated after each container ingestion quality write. Label `cluster_id` is the cluster UUID.

| Metric | Type | Labels | What to watch |
|--------|------|--------|---------------|
| `ros_recommendation_oom_rate` | Gauge | `org_id`, `cluster_id` | Mean OOM events per quality row in the last batch |
| `ros_recommendation_stability` | Gauge | `org_id`, `cluster_id` | Mean stability (0–100 on gauge; API uses 0.0–1.0) |
| `ros_recommendation_adoption_rate` | Gauge | `org_id`, `cluster_id` | Percent of batch rows with adoption detected |

### Retention

| Metric | Type | Labels | What to watch |
|--------|------|--------|---------------|
| `rosocp_retention_partitions_dropped_total` | Counter | — | Partitions dropped by daily sweep |

Controlled by `ROS_RETENTION_MONTHS`, `ROS_HISTORY_RETENTION_DAYS`, and related env vars.

### Legacy Kruize path

If the recommendation poller is deployed (Kruize mode), additional counters track Kruize API calls and invalid responses (`rosocp_kruize_api_exception_total`, `kruize_*_request_total`, etc.). These are zero when running the native Go engine only.

### Legacy / non-prefixed metrics

Some counters predate the `rosocp_` naming convention and retain their original names:

| Metric | Type | Labels | What to watch |
|--------|------|--------|---------------|
| `ros_ingestion_file_failures_total` | Counter | `org_id`, `cluster_id`, `report_type`, `error_class` | Permanent per-file ingestion failures recorded in `report_file_status` |
| `ros_savings_recalculation_total` | Counter | `org_id`, `recommendation_type`, `status` | Cost-model-triggered savings-only recalculations (`success`, `error`) |

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

## Grafana Dashboard Queries

Example panels for a ROS overview dashboard:

**Ingestion throughput (time series)**

```promql
rate(rosocp_kafka_messages_processed_total[5m])
```

**Ingestion errors by stage (stacked bar)**

```promql
sum by (stage) (increase(rosocp_ingestion_errors_total[1h]))
```

**Recommendation latency P95 (heatmap or multi-line)**

```promql
histogram_quantile(0.95, sum by (type, le) (rate(rosocp_recommendation_duration_seconds_bucket[5m])))
```

**Recommendations written (stat panel)**

```promql
sum(increase(rosocp_recommendations_written_total[24h]))
```

**API latency P95 top routes (table)**

```promql
topk(10, histogram_quantile(0.95, sum by (url, le) (rate(rosocp_request_duration_seconds_bucket[5m]))))
```

**Reship in progress (stat — should be 0)**

```promql
sum(ros_reship_in_progress)
```

---

## Troubleshooting via Metrics

### Symptom: No new recommendations

1. Check `rate(rosocp_kafka_messages_processed_total[5m])` — zero means the processor is not consuming.
2. Check Kafka consumer lag externally (not exported by ROS).
3. Check `rosocp_ingestion_errors_total` by stage for parse/digest/write failures.
4. Verify `/readyz` returns 200 (database connectivity).

### Symptom: High latency

1. Compare P95 across `rosocp_recommendation_duration_seconds` types to find the slow domain.
2. Check `rosocp_pipeline_phase_duration_seconds` for slow digest or GPU enrichment.
3. Check `rosocp_db_query_duration_seconds` for database bottlenecks.
4. Review API `rosocp_request_duration_seconds` if UI/API feels slow.

### Symptom: Missing savings estimates

Not directly metric-driven — look for log lines `cost data fetch failed`. Verify `KOKU_MASU_URL` and `ROS_SAVINGS_ESTIMATES_ENABLED=true`. Recommendations still write; only dollar fields are affected.

### Symptom: Business-hours reship not completing

1. `ros_reship_in_progress == 1` for extended period → stuck HTTP call to Masu.
2. `ros_reship_provider_resolution_failures_total` → cluster not mapped to a Koku provider or no cost model.
3. `ros_reship_failures_total` increasing → retries exhausted; check `ros_reship_fallback_forward_only_total`.

### Symptom: GPU recommendations incomplete

1. `rosocp_gpu_model_unrecognized_total` increasing → add model variants to the GPU catalog.
2. Check `rosocp_recommendation_duration_seconds{type="gpu"}` for slow enrichment.

---

## Known Limitations

| Limitation | Operator impact |
|------------|-----------------|
| No `/healthz` | Use `/status` for liveness; no deadlock detection |
| Readiness is shallow by default | Enable `ROS_READINESS_CHECK_KAFKA` / `ROS_READINESS_CHECK_S3` on processor pods |
| No distributed tracing | Correlate with `request_id` in logs |
| Snapshot write counter missing | Use logs or DB queries for snapshot output volume |

---

## Related Documentation

- [Configuration Reference](configuration.md)
- [Upgrade Runbook](operations/upgrade-runbook.md)
- [Known Issues](known-issues.md)
- [Configurability Reference](architecture/configurability.md)
- [Business Hours](features/business-hours.md)
