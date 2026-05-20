# Operational Runbooks

## Prometheus Metrics Reference

All metrics use the `rosocp_` prefix.

### Pipeline Performance

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `rosocp_recommendation_duration_seconds` | Histogram | `type` | End-to-end duration per recommendation type (`container`, `node`, `gpu`, `pvc`, `namespace`, `snapshot`) |
| `rosocp_pipeline_phase_duration_seconds` | Histogram | `phase` | Per-phase duration (`digest`, `gpu_enrichment`) |
| `rosocp_db_query_duration_seconds` | Histogram | `operation` | Database query latency |

### Throughput

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `rosocp_recommendations_written_total` | Counter | `type` | Recommendations persisted (`container`, `namespace`, `node`, `pvc`, `snapshot`) |
| `rosocp_kafka_messages_processed_total` | Counter | — | Kafka messages consumed successfully |
| `rosocp_recommendation_request_total` | Counter | — | Kruize recommendation requests (legacy path) |
| `rosocp_recommendation_success_total` | Counter | — | Kruize recommendations saved (legacy path) |

### Errors

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `rosocp_ingestion_errors_total` | Counter | `stage` | Ingestion failures by stage: `csv_parse`, `digest`, `recommend`, `write` |
| `rosocp_db_error_total` | Counter | — | Generic database errors |
| `rosocp_partition_missing_error_total` | Counter | `resource_name` | Table partition not found |
| `rosocp_invalid_csv_total` | Counter | — | Invalid container CSVs received |
| `rosocp_invalid_namespace_csv_total` | Counter | — | Invalid namespace CSVs received |
| `rosocp_csv_fetch_error_total` | Counter | — | S3/HTTP CSV download failures |
| `ros_ocp_plugin_hook_errors_total` | Counter | `plugin`, `hook_type` | Plugin ingest hook failures (non-fatal) |
| `rosocp_gpu_unrecognized_model_total` | Counter | `model_string` | GPU model strings not found in catalog |

### Operational Health

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `rosocp_rh_account_created_total` | Counter | — | New tenant accounts created |

---

## Runbook: High Ingestion Error Rate

**Alert condition:** `rate(rosocp_ingestion_errors_total[5m]) > 0.1`

### Diagnosis

1. Check which stage is failing:
   ```promql
   sum by (stage) (rate(rosocp_ingestion_errors_total[5m]))
   ```

2. By stage:
   - **csv_parse**: Malformed CSV from the operator. Check operator version and CSV format.
   - **digest**: Database write failure during sample ingestion. Check PostgreSQL connectivity and disk space.
   - **recommend**: Recommendation engine failure. Check logs for OOM, CPU issues, or malformed data.
   - **write**: Persisting recommendations failed. Check table partitions exist and disk space.

### Resolution

- **csv_parse**: Verify operator is generating properly typed CSVs. Check `manifest.json` has correct file list.
- **digest/write**: Check PostgreSQL health (`pg_stat_activity`, connection pool exhaustion). Verify partition auto-creation is working.
- **recommend**: Check `rosocp_pipeline_phase_duration_seconds{phase="digest"}` for abnormally long durations indicating data volume spikes.

---

## Runbook: Recommendation Duration Spike

**Alert condition:** `histogram_quantile(0.95, rate(rosocp_recommendation_duration_seconds_bucket[5m])) > 60`

### Diagnosis

1. Identify which type is slow:
   ```promql
   histogram_quantile(0.95, sum by (type, le) (rate(rosocp_recommendation_duration_seconds_bucket[5m])))
   ```

2. Check if it's a specific pipeline phase:
   ```promql
   histogram_quantile(0.95, sum by (phase, le) (rate(rosocp_pipeline_phase_duration_seconds_bucket[5m])))
   ```

### Resolution

- **container slow**: Check cluster size (containers per cluster). The streaming pipeline processes in batches of 500; very large clusters may take longer.
- **node slow**: Check if advisory lock contention is occurring (multiple workers processing same cluster simultaneously).
- **digest slow**: Database I/O bottleneck. Check `pg_stat_io` and connection pool size.

---

## Runbook: Partition Missing Errors

**Alert condition:** `rate(rosocp_partition_missing_error_total[1h]) > 0`

### Diagnosis

1. Identify which tables are missing partitions:
   ```promql
   sum by (resource_name) (rate(rosocp_partition_missing_error_total[1h]))
   ```

2. Check partition creation is running:
   ```sql
   SELECT schemaname, tablename FROM pg_tables
   WHERE tablename LIKE '%_202%'
   ORDER BY tablename DESC LIMIT 20;
   ```

### Resolution

- Partitions are auto-created during `EnsureHistoryPartitions` and `EnsureQualityPartitions`. If these fail silently, the downstream writes will hit this error.
- Manually create missing partitions:
  ```sql
  CREATE TABLE IF NOT EXISTS recommendation_history_2026_06
  PARTITION OF recommendation_history
  FOR VALUES FROM ('2026-06-01') TO ('2026-07-01');
  ```

---

## Runbook: GPU Model Unrecognized

**Alert condition:** `rate(rosocp_gpu_unrecognized_model_total[1h]) > 0`

### Diagnosis

1. Check which model strings are unrecognized:
   ```promql
   topk(10, sum by (model_string) (rosocp_gpu_unrecognized_model_total))
   ```

2. This indicates the NVIDIA GPU catalog (`internal/engine/gpu_catalog.yaml`) needs updating.

### Resolution

See [GPU Catalog Maintenance](gpu-catalog.md) for the update procedure.

---

## Runbook: Kafka Consumer Lag

**Alert condition:** Consumer group lag growing over time (monitored externally via Kafka metrics).

### Diagnosis

1. Check processing rate:
   ```promql
   rate(rosocp_kafka_messages_processed_total[5m])
   ```

2. If processing rate is near zero but lag is growing, the consumer may be stuck.

### Resolution

- Check consumer pod health and logs for panics or deadlocks.
- Verify database connectivity (the consumer blocks on DB writes).
- If the consumer is healthy but slow, check `rosocp_recommendation_duration_seconds` for slow recommendations that are back-pressuring consumption.
- Consider scaling workers if sustained lag is due to throughput limits.

---

## Runbook: Database Connection Exhaustion

**Symptom:** Increasing error rates across all metrics, logs showing "connection pool exhausted" or "too many connections".

### Diagnosis

```sql
SELECT count(*), state FROM pg_stat_activity
WHERE datname = 'ros_ocp'
GROUP BY state;
```

### Resolution

- Check for long-running transactions: `SELECT * FROM pg_stat_activity WHERE state = 'active' AND query_start < now() - interval '5 minutes';`
- Kill stuck sessions: `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE ...;`
- Increase `max_connections` or pool size if legitimate load growth.

---

## Runbook: Cost Data Fetch Failures

**Symptom:** Logs showing `cost data fetch failed` warnings. Recommendations still produced but without savings estimates (all savings = $0).

### Diagnosis

- Check Koku API health: `curl http://koku-server:8000/api/cost-management/v1/status/`
- Check `NotifNoCostData` notification appears in API responses (expected behavior when cost model is missing).

### Resolution

- This is **non-fatal** — recommendations are still written, just without dollar savings estimates.
- Verify cost model is assigned to the OCP provider in the Koku UI.
- Trigger cost model recalculation: `curl http://masu-server:5042/api/cost-management/v1/update_cost_model_costs/?provider_uuid=UUID&schema=orgNNNNN`
