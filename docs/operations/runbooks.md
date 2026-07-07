# Operational Runbooks

## Prometheus Metrics Reference

All metrics use the `rosocp_` prefix.

### Pipeline Performance

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `rosocp_recommendation_duration_seconds` | Histogram | `type` | End-to-end duration per recommendation type (`container`, `node`, `gpu`, `pvc`, `namespace`, `snapshot`) |
| `rosocp_pipeline_phase_duration_seconds` | Histogram | `phase` | Per-phase duration (`download`, `parse_digest`, `write_digests`, `recommend`, `write_recommendations`, `post_process`, `metadata_refresh`) |
| `rosocp_pipeline_total_duration_seconds` | Histogram | `status` | End-to-end manifest processing (`success` or `error`) |
| `rosocp_db_query_duration_seconds` | Histogram | `operation` | Database query latency |

### Throughput

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `rosocp_recommendations_written_total` | Counter | `type` | Recommendations persisted (`container`, `namespace`, `node`, `pvc`, `snapshot`) |
| `rosocp_kafka_messages_processed_total` | Counter | — | Kafka messages consumed successfully |
| `rosocp_kafka_retries_total` | Counter | — | Kafka messages requeued with incremented retry count |
| `rosocp_recommendation_request_total` | Counter | — | Kruize recommendation requests (legacy path) |
| `rosocp_recommendation_success_total` | Counter | — | Kruize recommendations saved (legacy path) |

### Errors

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `rosocp_kafka_dlq_messages_total` | Counter | — | Messages routed to Dead Letter Queue after exhausting retry budget |
| `rosocp_ingestion_errors_total` | Counter | `stage` | Ingestion failures by stage: `csv_parse`, `digest`, `recommend`, `write` |
| `rosocp_db_error_total` | Counter | — | Generic database errors |
| `rosocp_partition_missing_error_total` | Counter | `resource_name` | Table partition not found |
| `rosocp_invalid_csv_total` | Counter | — | Invalid container CSVs received |
| `rosocp_invalid_namespace_csv_total` | Counter | — | Invalid namespace CSVs received |
| `rosocp_csv_fetch_error_total` | Counter | — | S3/HTTP CSV download failures |
| `ros_ocp_plugin_hook_errors_total` | Counter | `plugin`, `hook_type` | Plugin ingest hook failures (non-fatal) |
| `rosocp_gpu_model_unrecognized_total` | Counter | — | GPU model strings not found in catalog (aggregate count; check logs for specific models) |

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
- **recommend**: Check `rosocp_pipeline_phase_duration_seconds{phase="parse_digest"}` for abnormally long durations indicating data volume spikes.

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

## Runbook: Analytics-Degraded Pipeline State

**Alert condition:** `increase(rosocp_analytics_incomplete_total[1h]) > 0` or container list responses include `"analytics_incomplete": true`

### Symptoms

- Fresh container recommendations are available via the API, but `/history` or `/quality` data is missing or stale for the affected cluster.
- Processor logs contain `analytics pipeline incomplete (history and/or quality)`.
- `clusters.analytics_incomplete` is `true` for the cluster (see diagnosis below).

### Diagnosis

1. Check which analytics path failed:
   ```promql
   sum by (org_id, cluster_uuid, error_type) (increase(rosocp_analytics_incomplete_total[1h]))
   ```

2. Confirm cluster flag in PostgreSQL:
   ```sql
   SELECT ra.org_id, c.cluster_uuid, c.analytics_incomplete, c.analytics_incomplete_at
   FROM clusters c
   JOIN rh_accounts ra ON ra.id = c.tenant_id
   WHERE c.analytics_incomplete = true;
   ```

3. Inspect processor logs for the cluster UUID — look for `writing recommendation history failed` or `writing quality metrics failed`.

4. Common root causes: missing monthly partition (`rosocp_quality_partition_missing_total`), ingest statement timeout, transient DB connectivity.

### Resolution

**Default (strict mode, `ROS_INGEST_STRICT_ANALYTICS=true`):**

- The Kafka offset was not committed; the message will retry automatically. Fix the root cause (partitions, DB load, timeout) and allow the consumer to catch up.

**Degraded mode (`ROS_INGEST_STRICT_ANALYTICS=false`):**

- Recommendations were intentionally persisted. Re-trigger ingestion for the cluster (Kafka message replay or `reship_ros`) once the underlying DB issue is fixed. A successful run clears `analytics_incomplete`.

**Operational hardening:**

- Enable strict mode in environments where analytics/history parity is required for compliance dashboards.
- Monitor `rosocp_analytics_incomplete_total` alongside `rosocp_partition_missing_error_total`.

---

## Runbook: Plugin Ingest Hook Failures

**Alert condition:** `increase(ros_ocp_plugin_hook_errors_total[1h]) > 0` or container list responses include `"ingest_hooks_failed": true`

### Symptoms

- Container CSV ingestion succeeded but GPU/node digest hooks failed (logged as `IngestHook <name> failed (non-fatal)`).
- `clusters.ingest_hooks_failed` is `true` for the cluster.
- Recommendations may proceed with stale or partial GPU/node digest data.

### Diagnosis

1. Check hook failure rate by plugin:
   ```promql
   sum by (plugin, hook_type) (increase(ros_ocp_plugin_hook_errors_total[1h]))
   ```

2. Confirm cluster flag in PostgreSQL:
   ```sql
   SELECT ra.org_id, c.cluster_uuid, c.ingest_hooks_failed, c.ingest_hooks_failed_at
   FROM clusters c
   JOIN rh_accounts ra ON ra.id = c.tenant_id
   WHERE c.ingest_hooks_failed = true;
   ```

3. Inspect processor logs for the cluster UUID — look for `IngestHook` warnings with org and cluster context.

### Resolution

- Fix the underlying hook failure (DB write errors, schema mismatch, plugin misconfiguration).
- Re-trigger ingestion for the cluster via Kafka replay or Koku masu `reship_ros`. A successful ingest with all hooks passing clears `ingest_hooks_failed`.

**Operational hardening:**

- Monitor `ros_ocp_plugin_hook_errors_total` alongside `rosocp_ingestion_errors_total`.
- Treat sustained hook failures like analytics degradation: investigate before trusting GPU/node recommendations for affected clusters.

---

## Runbook: Partial Manifest Ingestion (`report_file_status`)

**Alert condition:** `increase(ros_ingestion_file_failures_total[1h]) > 0`, recommendations missing for expected workloads, or `increase(rosocp_ingest_manifest_id_synthesized_total[1h]) > 0`

### Symptoms

- Kafka payload partially processed: some CSV files ingested, others permanently failed.
- Recommendation engines gated until all expected files reach `done` state.
- Legacy messages without `metadata.manifest_id` receive synthesized IDs (`synth-*` prefix).

### Diagnosis

1. List incomplete or failed manifests:
   ```sql
   SELECT manifest_id, filename, status, error_class, error_message, updated_at
   FROM report_file_status
   WHERE status IN ('failed', 'processing', 'pending')
   ORDER BY updated_at DESC
   LIMIT 50;
   ```

2. Check manifest completion for a specific manifest:
   ```sql
   SELECT status, count(*) FROM report_file_status
   WHERE manifest_id = '<manifest-id>'
   GROUP BY status;
   ```

3. Identify synthesized manifest IDs (legacy Kafka messages):
   ```sql
   SELECT manifest_id, count(*) FROM report_file_status
   WHERE manifest_id LIKE 'synth-%'
   GROUP BY manifest_id
   ORDER BY max(updated_at) DESC;
   ```

4. Prometheus:
   ```promql
   sum by (org_id, cluster_id, report_type, error_class) (increase(ros_ingestion_file_failures_total[1h]))
   increase(rosocp_ingest_manifest_id_synthesized_total[1h])
   ```

### Resolution

1. **Investigate failed files** — check `error_class` and `error_message` in `report_file_status` (S3 403/404, corrupt CSV, parse errors).

2. **Fix root cause** (object storage access, operator CSV format, missing file in payload).

3. **Re-deliver failed files** via Koku masu reship (preferred):
   ```bash
   curl -s "http://masu-server:5042/api/cost-management/v1/reship_ros/?manifest_id=<MANIFEST_ID>"
   ```

4. **Verify recovery:**
   ```sql
   SELECT status, count(*) FROM report_file_status
   WHERE manifest_id = '<manifest-id>'
   GROUP BY status;
   ```
   All expected files should be `done`; recommendations should resume on the next successful ingest pass.

5. **Manual status reset (last resort):** If a file was incorrectly marked `failed` and you have confirmed data is valid after a fix, delete the stale row and re-trigger reship:
   ```sql
   DELETE FROM report_file_status
   WHERE manifest_id = '<manifest-id>' AND filename = '<filename>' AND status = 'failed';
   ```

### Notes

- Kafka offsets commit after partial failure by design — the queue does not stall on one bad file.
- Synthesized manifest IDs (`synth-*`) are deterministic from `(org_id, cluster_uuid, date|payload fingerprint)` and behave like real manifest IDs for gating.
- See also [Runbook: Kafka Dead Letter Queue (DLQ) Messages](#runbook-kafka-dead-letter-queue-dlq-messages) for message-level failures.

---

## Runbook: GPU Model Unrecognized

**Alert condition:** `rate(rosocp_gpu_model_unrecognized_total[1h]) > 0`

### Diagnosis

1. Check which model strings are unrecognized by searching application logs:
   ```bash
   # Kubernetes
   kubectl logs -l app.kubernetes.io/component=ros-processor --all-containers | grep "gpu_metadata: unrecognized GPU model"
   # Or in centralized logging (Loki, CloudWatch, etc.):
   # filter: "gpu_metadata: unrecognized GPU model"
   ```
   Each log line includes a `gpu_model` field with the exact DCGM-reported string.
   The warning is emitted **once per model string per process lifetime** — if you
   don't see recent entries, the model was logged after a previous restart.

2. This indicates the NVIDIA GPU catalog (`internal/engine/gpu_catalog.yaml`) needs updating.

### Resolution

See [GPU Catalog Maintenance](gpu-catalog.md) for the update procedure and
[GPU Catalogs — Data Sources and Validation](../architecture/gpu-catalogs.md) for NVIDIA references.

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

## Runbook: Kafka Dead Letter Queue (DLQ) Messages

**Alert condition:** `rate(rosocp_kafka_dlq_messages_total[5m]) > 0`

### Diagnosis

1. Check DLQ message rate:
   ```promql
   rate(rosocp_kafka_dlq_messages_total[5m])
   ```

2. Check retry rate (precursor to DLQ):
   ```promql
   rate(rosocp_kafka_retries_total[5m])
   ```

3. Inspect DLQ messages for failure reasons:
   ```bash
   kubectl exec -n kafka cost-onprem-kafka-broker-0 -- \
     bin/kafka-console-consumer.sh \
     --bootstrap-server localhost:9092 \
     --topic hccm.ros.events.dlq \
     --from-beginning --max-messages 5 \
     --property print.headers=true
   ```

4. Key headers on DLQ messages:
   - `X-Original-Topic` — source topic
   - `X-Original-Partition` — source partition
   - `X-Failure-Reason` — error that caused permanent failure
   - `X-Failed-At` — UTC timestamp of DLQ routing
   - `X-Retry-Count` — number of retries attempted (should equal max)

### Resolution

1. **Identify root cause** from `X-Failure-Reason` header:
   - Database errors → check PostgreSQL health
   - S3/MinIO errors → check object storage connectivity
   - Parse errors → check for corrupt CSV data from operator
   - Unknown errors → check processor logs around `X-Failed-At` timestamp

2. **Fix the root cause** before replaying messages.

3. **Replay DLQ messages** after fix:
   ```bash
   # Option A: Use reship_ros endpoint (preferred)
   curl -s "http://masu-server:5042/api/cost-management/v1/reship_ros/?manifest_id=<ID>"
   
   # Option B: Manual replay from DLQ topic
   # Produce DLQ messages back to hccm.ros.events (strip X-Retry-Count header)
   ```

4. **Verify recovery:**
   ```promql
   rate(rosocp_kafka_messages_processed_total[5m])  # Should resume normal rate
   rate(rosocp_kafka_dlq_messages_total[5m])        # Should drop to zero
   ```

### Configuration

| Variable | Default | Purpose |
|----------|---------|---------|
| `ROS_KAFKA_MAX_TRANSIENT_RETRIES` | `5` | Attempts before routing to DLQ |
| `ROS_KAFKA_DLQ_TOPIC` | `hccm.ros.events.dlq` | Dead Letter Queue topic name |

### Notes

- DLQ messages are preserved with 30-day retention (configured on the KafkaTopic CR).
- The retry mechanism uses Kafka message headers (`X-Retry-Count`), which survive pod restarts and consumer rebalances.
- If the DLQ produce itself fails, the offset is NOT committed — natural Kafka redeliver continues (infinite retry as fallback).

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

**Symptom:** Logs showing `cost data fetch failed` warnings. Recommendations still produced but without savings estimates (savings = `$0` or omitted).

**Affected plugins:** Container, node, and PVC append `NotifNoCostData` (code **25**) when Masu data is missing. GPU/time-slicing API enrichment omits dollar fields without code 25. Snapshot recoverable cost skips the dynamic effective-rates default only.

### Diagnosis

- Check whether savings are intentionally disabled: `ROS_SAVINGS_ESTIMATES_ENABLED=false` skips all Masu calls by design (see [cost-integration.md](../architecture/cost-integration.md)).
- Check `KOKU_MASU_URL` is set on **both** ros-processor and ros-api.
- Check Koku/Masu health: `curl http://koku-server:8000/api/cost-management/v1/status/`
- Verify migrations **000070**–**000072** applied if node/PVC savings columns or nested node engines are missing:
  ```sql
  SELECT column_name FROM information_schema.columns
  WHERE table_name IN ('node_recommendations', 'pvc_recommendation_sets')
    AND column_name = 'estimated_savings_cents';

  SELECT column_name FROM information_schema.columns
  WHERE table_name = 'node_recommendations'
    AND column_name IN ('engine', 'recommended_cpu_cores', 'recommended_memory_gib', 'node_count_reduction');
  ```
- Check API responses for `NotifNoCostData` (code 25) on container detail, `GET .../nodes`, and `GET .../pvcs`.

### Resolution

- **Non-fatal** — recommendations are still written; only dollar fields are affected.
- Set `ROS_SAVINGS_ESTIMATES_ENABLED=true` and configure `KOKU_MASU_URL` if disabled.
- Verify an OCP cost model is assigned to the provider in the Koku UI (storage rates required for PVC/snapshot dynamic defaults).
- For OCP-on-cloud clusters, confirm both OCP and cloud sources are ingested and correlated (`infrastructure_cost` in `effective_rates` namespace aggregates).
- Trigger cost model recalculation: `curl http://masu-server:5042/api/cost-management/v1/update_cost_model_costs/?provider_uuid=UUID&schema=orgNNNNN`
- Wait for the next ingestion cycle (container/node/PVC) or re-query GPU endpoints after Masu recovery.

See [cost-integration.md](../architecture/cost-integration.md) for formulas, plugin matrix, and [upgrade-runbook.md](../upgrade-runbook.md) for migrations **000070**–**000072**.

---

## Runbook: CSV URL SSRF Protection

Presigned CSV URLs in Kafka messages are validated in `internal/utils/csv_security.go` before the processor downloads them.

### Controls

| Control | Variable | Behavior |
|---------|----------|----------|
| Host allowlist | `ROS_CSV_ALLOWED_HOSTS` | Required in production; only listed hostnames may appear in CSV URLs. |
| Private-network deny | `ROS_CSV_DENY_PRIVATE_NETWORKS` (default `true`) | Resolves the hostname and rejects loopback, RFC1918/private, and link-local addresses in **both IPv4 and IPv6** (including `::1`, ULA `fc00::/7`, link-local `fe80::/10`). |
| DNS fail-closed | (implicit when `DEVELOPMENT=false`) | Unresolved hostnames block fetch in production. |

### Diagnosis

Processor logs or DLQ messages containing `restricted address` or `resolves to restricted address`:

1. Confirm the URL hostname is in `ROS_CSV_ALLOWED_HOSTS`.
2. Check whether DNS for that hostname resolves to a private or link-local address (IPv4 or IPv6).
3. Verify `ROS_CSV_DENY_PRIVATE_NETWORKS` is not disabled in production.

### Resolution

- Use a public object-storage endpoint hostname that resolves to routable addresses.
- For legitimate internal MinIO/NooBaa URLs, ensure the hostname resolves to addresses outside private/link-local ranges, or use development mode only for local testing.
- See [configuration.md](configuration.md#csv-download-security-kafka-ingestion) and ADR-0145.
