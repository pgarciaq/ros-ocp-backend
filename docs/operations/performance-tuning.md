# Performance Tuning

## Parallel CSV Download (`ROS_MANIFEST_DOWNLOAD_WORKERS`)

When a Kafka message arrives containing a manifest with N CSV files, the
processor downloads and ingests them concurrently using a bounded worker pool.

| Variable | Default | Description |
|----------|---------|-------------|
| `ROS_MANIFEST_DOWNLOAD_WORKERS` | 2 | Maximum concurrent file downloads per manifest |

### How It Works

Each Kafka consumer goroutine (controlled by `ROS_KAFKA_WORKERS`) processes one
manifest at a time. Within that manifest, up to `ROS_MANIFEST_DOWNLOAD_WORKERS`
files are downloaded and ingested in parallel using an `errgroup` with a
concurrency limit.

Wall-clock improvement: sequential time is `N × avg_download_time`; parallel
time is approximately `max(download_times)`.

### Interaction with Connection Pool

Each concurrent file download may hold a database connection during ingest.
The worst-case concurrent connection usage is:

```
max_connections_needed = ROS_MANIFEST_DOWNLOAD_WORKERS × ROS_KAFKA_WORKERS
```

The startup validator emits a WARNING when:

```
ROS_MANIFEST_DOWNLOAD_WORKERS × ROS_KAFKA_WORKERS > ROS_DB_MAX_CONNS - 2
```

The 2-connection reserve accounts for health checks and recommendation queries
that run outside the ingest path.

### Recommended Values

| Deployment Size | `WORKERS` | `KAFKA_WORKERS` | `DB_MAX_CONNS` | Notes |
|----------------|-----------|-----------------|----------------|-------|
| Small (dev/test) | 2 | 3 | 5 | Default; warning emitted but safe |
| Medium (single pod) | 3 | 3 | 12 | 9 ingest + 3 reserve |
| Large (multi-pod) | 4 | 5 | 25 | 20 ingest + 5 reserve |

### Memory Implications

Each concurrent file download streams CSV rows through the parser, accumulating
digest groups in memory. With `W` workers, peak memory is approximately
`W × (rows_per_file × row_size)`. For typical OCP manifests (5–10 files,
~100K rows each), 2–3 workers add negligible overhead.

### Error Handling

- **Transient errors** (DB timeout, connection refused, 503): Cancel all
  in-flight downloads for the manifest and return the error to Kafka for retry.
- **Permanent errors** (constraint violation): Record per-file failure without
  cancelling sibling downloads; other files continue processing.
- **Unknown errors**: Classified as transient by default (conservative approach
  to avoid data loss).

### Troubleshooting

| Symptom | Likely Cause | Fix |
|---------|--------------|-----|
| Startup warning about pool exhaustion | Workers × Kafka > MaxConns - 2 | Increase `ROS_DB_MAX_CONNS` or reduce workers |
| Frequent connection pool timeouts | Too many concurrent workers | Reduce `ROS_MANIFEST_DOWNLOAD_WORKERS` |
| Slow ingestion despite high worker count | Network bottleneck or DB contention | Profile with `rosocp_ingest_phase_seconds` metrics |
| Single file failures retrying entire manifest | Error classified as transient | Check logs for the specific error; only DB/network errors should trigger retry |
