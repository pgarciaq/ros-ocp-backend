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
| `/readyz` | API port or metrics port | All | Readiness — PostgreSQL pool responds to ping |
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
| `rosocp_recommendations_written_total` | Counter | `type` | Recommendations saved (`container`, `namespace`, `node`, `pvc`) |
| `rosocp_recommendation_duration_seconds` | Histogram | `type` | How long each recommendation domain takes |
| `rosocp_pipeline_phase_duration_seconds` | Histogram | `phase` | Ingestion sub-phases (`digest`, `gpu_enrichment`) |

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
| `rosocp_ingestion_errors_total` | Counter | `stage` | Pipeline failures: `csv_parse`, `digest`, `recommend`, `write` |
| `rosocp_invalid_csv_total` | Counter | — | Bad container CSV from upstream |
| `rosocp_invalid_namespace_csv_total` | Counter | — | Bad namespace CSV |
| `rosocp_csv_fetch_error_total` | Counter | — | Failed S3/HTTP downloads |
| `rosocp_db_error_total` | Counter | — | Database errors |
| `rosocp_partition_missing_error_total` | Counter | `resource_name` | Missing monthly table partition |
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
| `rosocp_db_error_total` | Counter | — | Connection or query failures |

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
| `ros_threshold_recalculation_total` | Counter | `org_id`, `recommendation_type`, `status` | `success` vs `error` |
| `ros_threshold_cache_entries` | Gauge | — | In-memory cache size |

### Retention

| Metric | Type | Labels | What to watch |
|--------|------|--------|---------------|
| `rosocp_retention_partitions_dropped_total` | Counter | — | Partitions dropped by daily sweep |

Controlled by `ROS_RETENTION_MONTHS`, `ROS_HISTORY_RETENTION_DAYS`, and related env vars.

### Legacy Kruize path

If the recommendation poller is deployed (Kruize mode), additional counters track Kruize API calls and invalid responses (`rosocp_kruize_api_exception_total`, `kruize_*_request_total`, etc.). These are zero when running the native Go engine only.

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

| Environment variable | Default | Purpose |
|---------------------|---------|---------|
| `LOG_LEVEL` | `INFO` | Verbosity |
| `LogFormater` | JSON in prod, `text` locally | Output format |
| `SERVICE_NAME` | `rosocp` | Log `service` field |
| `PROMETHEUS_PORT` | `5005` / `9000` | Metrics listener port |
| `API_PORT` | `8000` | REST API port |
| `ROS_DB_MAX_CONNS` | `10` | DB pool size |
| `ROS_DB_ACQUIRE_TIMEOUT_SECS` | `5` | Pool wait timeout |
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
| Readiness is DB-only | Pod may be "ready" while Kafka is down — monitor consumer lag separately |
| No distributed tracing | Correlate with `request_id` in logs |
| Snapshot write counter missing | Use logs or DB queries for snapshot output volume |

---

## Related Documentation

- [Upgrade Runbook](operations/upgrade-runbook.md)
- [Known Issues](known-issues.md)
- [Configurability Reference](architecture/configurability.md)
- [Business Hours](features/business-hours.md)
