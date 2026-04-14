# Phase 5 Implementation Notes

## Storage Volume: `container_usage_samples`

Every 15-minute CSV row becomes a row in `container_usage_samples`. Volume estimates:

| Containers | Rows/day | Rows/month | Approx size/month |
|---|---|---|---|
| 1,000 | 96K | 2.9M | ~200 MB |
| 10,000 | 960K | 29M | ~2 GB |
| 100,000 | 9.6M | 290M | ~20 GB |

The retention sweep (`ROS_RETENTION_MONTHS`, default 6) drops old monthly partitions
automatically. The primary key `(org_id, cluster_uuid, namespace, workload, container_name, sample_time)`
ensures efficient lookups for boxplot queries scoped to a single container.

### Potential optimization (deferred)

If storage becomes a concern at scale, consider pre-aggregating raw samples into
hourly five-number summaries (a separate `hourly_container_stats` table) and dropping
the raw samples after aggregation. This trades exact boxplot accuracy for ~24x storage
reduction. The current design prioritizes accuracy — `percentile_cont()` on raw samples
produces exact boxplots, whereas pre-aggregated hourly stats would require approximate
re-aggregation for daily buckets.

## Query Overhead: `enrichNativeDetail`

Each detail API request triggers 4 additional PostgreSQL queries:

1. `AssembleBoxplots` for `short_term` (~24h of samples)
2. `AssembleBoxplots` for `medium_term` (~7d of samples)
3. `AssembleBoxplots` for `long_term` (~15d of samples)
4. `MonitoringEndTime` (MAX on indexed column)

All queries are scoped to a single container via the composite primary key, so they
hit the index directly. Expected latency: < 10ms each on warm cache.

### Potential optimization (deferred)

These 4 queries could be consolidated into a single query using CTEs or a
server-side function. This would reduce round-trips from 4 to 1 per detail
request. Worth implementing if monitoring shows latency issues under concurrent
load, but not needed for current scale.

## IQE Test Design: Raw JSON Deserialization

The IQE boxplot tests in `test_boxplots.py` use `get_recommendation_by_id_without_preload_content`
(raw HTTP response) and parse JSON manually, rather than using the auto-generated SDK client types.

**Reason:** The SDK types are generated from an OpenAPI spec that describes the Kruize
response shape. The native engine's `DetailResponse` has a compatible shape but different
type mappings. Using raw JSON avoids SDK deserialization errors for fields the SDK doesn't
know about.

**Long-term fix:** When the native engine becomes the default, update the OpenAPI spec
to describe the native response shape and regenerate the SDK client types. At that point,
the tests can switch back to typed deserialization.
