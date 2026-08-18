# Notification codes API

> **Last verified:** 2026-08-17

`GET /api/cost-management/v1/recommendations/openshift/notification-codes`

Returns the machine-readable catalog of all notification codes (severity, name, description). The response is built from in-memory Go definitions that mirror the `notification_code_definitions` database table — no database query per request.

## Authentication

No `x-rh-identity` header is required (reference data).

## Query parameters

| Parameter | Description |
|-----------|-------------|
| `filter[plugin]` | Optional. Limit to codes used by a plugin: `container`, `namespace`, `node`, `gpu`, `pvc`, `snapshot`, `vm`, `quota`, `cluster-quota`. |

## Response

```json
{
  "meta": { "count": 80 },
  "data": [
    {
      "code": 2,
      "name": "STALE_DATA",
      "severity": "WARNING",
      "description": "No new metrics data received for more than 48 hours"
    },
    {
      "code": 77,
      "name": "SPARSE_DATA",
      "severity": "INFO",
      "description": "Recommendation based on limited data; accuracy improves with more observation time"
    },
    {
      "code": 79,
      "name": "NODE_BH_NOT_PEAK_SAFE",
      "severity": "WARNING",
      "description": "Business-hours node sizing is not peak-safe — overnight spikes outside the cluster schedule are excluded"
    },
    {
      "code": 80,
      "name": "GPU_BH_OFFICE_WINDOW",
      "severity": "WARNING",
      "description": "Business-hours GPU sizing uses the namespace office window — overnight training and off-hours bursts are excluded"
    },
    {
      "code": 81,
      "name": "GPU_TS_BH_CLUSTER_WINDOW",
      "severity": "WARNING",
      "description": "Business-hours GPU time-slicing uses the cluster office window — overnight training and off-hours bursts are excluded"
    }
  ]
}
```

Entries are sorted by `code` ascending.

## Related documentation

- [Notification codes (human-readable catalog)](../architecture/notification-codes.md)
- [Snapshot staleness](../features/snapshot-staleness.md) — VolumeSnapshot staleness detection (codes 31–35)
