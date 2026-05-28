# ResourceQuota Recommendations

Right-size Kubernetes **ResourceQuota** hard limits per namespace by comparing
configured limits and observed usage against aggregated container recommendation totals.

**Status:** Shipped. Enable with the native plugin set (`quota` is on by default).

**API:** `GET /api/cost-management/v1/recommendations/openshift/quota/`

**Related:** [Namespace recommendations](namespace-recommendations.md) size ideal namespace
totals from usage digests — distinct from tuning existing ResourceQuota objects.

---

## How it works (high level)

1. The metrics operator reports namespace-level quota **hard** and optional **used** values.
2. ROS ingests them into daily namespace digests and sums container rightsizing recommendations.
3. For each namespace with hard limits, ROS recommends adjusted limits with headroom and
   classifies the namespace as **tighten**, **raise**, or **optimal**.
4. **Tighten** rows can include estimated monthly savings when cost integration is enabled.

**ClusterResourceQuota** (OpenShift multi-namespace quotas) is not supported yet.

---

## Recommendation types and risk

| `recommendation_type` | Meaning |
|----------------------|---------|
| `tighten` | Recommended hard limits are below current hard — reclaim stranded quota |
| `raise` | Usage or recommendation totals are near the hard limit — scaling risk |
| `optimal` | Quota is reasonably aligned with workload needs |

| `risk_level` | Typical utilization (max across CPU/memory request/limit) |
|--------------|-----------------------------------------------------------|
| `high` | ≥ high-risk threshold (default 80% of hard) |
| `medium` | ≥ medium threshold (default 60%) |
| `low` | Below medium but non-zero |
| `none` | No utilization signal |

Utilization uses the **greater** of quota **used** metrics and container recommendation sums
vs hard limits.

---

## Configuration

| Variable | Default | Purpose |
|----------|---------|---------|
| `ROS_QUOTA_HEADROOM_PERCENT` | `20` | Margin on recommended hard values (20 → 120% of container rec sums) |
| `ROS_QUOTA_HIGH_RISK_THRESHOLD_PERCENT` | `80` | Triggers `raise` and `high` risk |
| `ROS_QUOTA_MEDIUM_RISK_THRESHOLD_PERCENT` | `60` | `medium` risk band |

Disable the feature: `ROS_DISABLED_PLUGINS=quota` or omit `quota` from `ROS_ENABLED_PLUGINS`
(list endpoint returns 404).

See [Configuration](../configuration.md#resourcequota-recommendations) for deployment notes.

---

## One-cycle lag

Container recommendation sums are read from the database after the container plugin runs.
When a report bundle includes both container and namespace CSVs, quota runs twice in one
cycle (after container processing and after namespace ingest). If only namespace data
arrives in a cycle, quota uses container recommendations from the **previous** cycle.

---

## Example response

```json
{
  "meta": {
    "count": 1,
    "limit": 100,
    "offset": 0,
    "currency": "USD"
  },
  "links": { "first": "/api/cost-management/v1/recommendations/openshift/quota/?limit=100&offset=0", "last": "...", "next": null, "previous": null },
  "data": [
    {
      "cluster_uuid": "550e8400-e29b-41d4-a716-446655440001",
      "namespace": "production",
      "recommendation_type": "tighten",
      "risk_level": "low",
      "quota_hard": {
        "cpu_request_millicores": 100000,
        "memory_request_bytes": 107374182400
      },
      "quota_used": {
        "cpu_request_millicores": 25000
      },
      "quota_recommended": {
        "cpu_request_millicores": 36000,
        "memory_request_bytes": 45097156608
      },
      "utilization": {
        "cpu_request_percent": 25.0
      },
      "capacity_freed": {
        "cpu_millicores": 64000,
        "memory_bytes": 62277025792
      },
      "estimated_savings": {
        "value": 142.50,
        "units": "USD",
        "currency": "USD"
      },
      "last_observed_at": "2026-05-28T12:00:00Z"
    }
  ]
}
```

---

## Query parameters

| Parameter | Example | Description |
|-----------|---------|-------------|
| `filter[cluster]` | UUID | Limit to one cluster |
| `filter[project]` | `production` | Limit to one namespace |
| `filter[recommendation_type]` | `tighten,raise` | Filter by type |
| `filter[risk_level]` | `high,medium` | Filter by risk |
| `group_by[cluster]` | — | Aggregate rows per cluster |
| `group_by[project]` | — | Aggregate rows per namespace |

Full schema: [OpenAPI specification](../openapi.md) and [`openapi.json`](../../openapi.json).

---

## Operator data

Hard limits (required): `cpu_request_namespace_sum`, `cpu_limit_namespace_sum`,
`memory_request_namespace_sum`, `memory_limit_namespace_sum`.

Used values (optional, backward compatible): `cpu_request_namespace_used`,
`cpu_limit_namespace_used`, `memory_request_namespace_used`, `memory_limit_namespace_used`.

Older operators without `*_namespace_used` columns still work; utilization falls back to
container recommendation sums where used metrics are absent.
