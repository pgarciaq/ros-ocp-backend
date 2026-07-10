# ADR-0091: Incremental digest flush during streaming CSV parse

## Status

Accepted — **Revised**: default changed from 5,000 to `math.MaxInt32` (effectively disabled). See [#264](https://github.com/pgarciaq/ros-ocp-backend/issues/264).

## Context

Holding full cluster-day digest map until EOF was originally thought to cause OOM on large clusters.

## Decision

The `ROS_INGEST_FLUSH_BATCH_SIZE` mechanism exists as a safety net, but the default is now `math.MaxInt32` — effectively disabling intermediate flushes. All digest groups accumulate in memory until EOF and are flushed once.

### Why intermediate flushes are disabled by default

1. **Memory is bounded upstream**: Nise caps files at 100K rows; CMMO splits at 100 MB. In-flight digest accumulator memory is 22–115 MB regardless of cluster size — well within the 1 GiB pod limit.

2. **Intermediate flushes degrade recommendation quality**: The flush-and-clear cycle (`clear(groupedAll); clear(groupedBH)`) combined with blind-overwrite upserts (`ON CONFLICT DO UPDATE SET column = EXCLUDED.column`) means the last flush's percentiles completely overwrite previous values. With interval-first CSV ordering, each flush computes percentiles from only ~1 sample per group — P50 = P60 = P95 = P99 = that single value, `sample_count = 1`. The recommendation engine then generates right-sizing recommendations from these meaningless "percentiles."

3. **Performance improves**: 1 DB round-trip per file instead of 20 (for 10K containers with threshold 5K). The 10K-container benchmark dropped from 3 hours to projected ~20–30 min.

### Previous decision (superseded)

The original decision was to flush every 5,000 groups. This was based on a concern that large clusters could accumulate gigabytes in memory. Analysis showed this was unfounded: upstream file size caps bound memory to ~22–115 MB.

## Alternatives Considered

### Keep threshold at 5,000 (original)
Causes superlinear scaling (24× slowdown for 2.5× more containers) and produces garbage percentiles for clusters with >5K containers. Rejected.

### Raise threshold to 50,000
Eliminates flushes for clusters up to ~25K containers but still triggers quality-degrading flushes for larger clusters. Rejected in favor of `MaxInt32`.

### Merge-on-flush instead of overwrite
Modify the upsert to use `GREATEST(existing, new)` / weighted average instead of blind overwrite, preserving quality across flushes. More complex, not needed if intermediate flushes are disabled. Deferred.

### Spool digests to disk (temp files)
Writing intermediate digest state to PVC would cap RAM usage, but adds I/O latency and cleanup complexity on ephemeral processor pods. Not needed given memory is bounded by upstream file caps.

## Consequences

- Memory bounded by upstream file size caps (~22–115 MB), not by the flush threshold.
- Single DB round-trip per file — maximum performance.
- Percentiles computed from all samples in the file — maximum recommendation quality.
- The `ROS_INGEST_FLUSH_BATCH_SIZE` env var remains available as a safety valve for environments with severely constrained memory.

## References

- [internal/ingestion/pipeline_stream.go](internal/ingestion/pipeline_stream.go)
- [#264: Disable intermediate digest flushes](https://github.com/pgarciaq/ros-ocp-backend/issues/264)
