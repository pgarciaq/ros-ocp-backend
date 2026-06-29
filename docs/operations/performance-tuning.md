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

---

## Term Config Cache (`ROS_TERM_CONFIG_CACHE_MAX_ENTRIES`)

The term configuration cache stores per-org recommendation term settings (window
days, min data days, decay half-life) in a bounded LRU with 60-second TTL expiry.

| Variable | Default | Description |
|----------|---------|-------------|
| `ROS_TERM_CONFIG_CACHE_MAX_ENTRIES` | 5 (on-prem) / 1000 (SaaS) | Maximum entries in the term config LRU cache |
| `ONPREM` | `false` | Deployment mode; affects default cache sizes |

### How It Works

Each `LoadTermConfigCached` call checks the LRU cache. On hit (entry exists and
TTL has not expired), the cached terms are returned without a DB query. On miss,
terms are loaded from the database and inserted into the cache.

When the cache reaches its maximum size, the least-recently-used entry is evicted
to make room. TTL expiry runs in a background goroutine — entries older than 60s
are removed regardless of access pattern.

### Mode-Aware Defaults

The cache uses different defaults based on deployment mode:

| Mode | Default Max Entries | Rationale |
|------|---------------------|-----------|
| On-prem (`ONPREM=true`) | 5 | Single-tenant; few orgs/types |
| SaaS (`ONPREM=false`) | 1000 | Multi-tenant; many orgs × recommendation types |

Set `ROS_TERM_CONFIG_CACHE_MAX_ENTRIES` to override the mode default explicitly.

### Prometheus Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `rosocp_term_config_cache_size` | Gauge | Current entries in cache |
| `rosocp_term_config_cache_hits_total` | Counter | Lookups served from cache |
| `rosocp_term_config_cache_misses_total` | Counter | Lookups requiring DB fetch |
| `rosocp_term_config_cache_evictions_total` | Counter | Entries evicted by LRU capacity |

### Tuning Guidance

- **Hit rate below 80%**: Increase `ROS_TERM_CONFIG_CACHE_MAX_ENTRIES` to reduce
  DB load from repeated term lookups.
- **Memory concerns**: Each entry is ~200 bytes (3 terms × struct). Even 1000
  entries use only ~200 KB — negligible for most deployments.
- **After Settings API changes**: The cache is automatically invalidated for the
  affected org+type when term settings are updated via the API.
