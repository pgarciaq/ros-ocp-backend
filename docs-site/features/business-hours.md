# Business Hours

Business Hours adds schedule-aware CPU and memory sizing to **container** and **namespace** recommendations. Workloads that spike during business hours but share nodes with overnight batch jobs get a second sizing perspective based on in-window usage only.

## How it works

1. An administrator configures a weekly schedule (timezone, days, start/end time) at org, cluster, or namespace scope.
2. During ingestion, samples are filtered into parallel `all_hours` and `business_hours` digest streams.
3. After historical reship completes, recommendation detail and list responses include a nested `business_hours` block alongside the existing all-hours engines.

Savings estimates always use the **all_hours** perspective. Business hours affects sizing only, not dollar savings.

## Settings API

Base path: `/api/cost-management/v1/recommendations/openshift/settings/business-hours`

| Scope | Methods | Path suffix |
|-------|---------|-------------|
| Org default | GET, PUT, DELETE | `/settings/business-hours` |
| Cluster override | GET, PUT, DELETE | `/settings/business-hours/clusters/:cluster_id` |
| Namespace override | GET, PUT, DELETE | `/settings/business-hours/clusters/:cluster_id/namespaces/:namespace` |
| Effective (resolved) | GET | `/settings/business-hours/effective?cluster_id=&namespace=` |

**Inheritance:** namespace → cluster → org → disabled (no schedule row).

**PUT** returns `202 Accepted` and triggers asynchronous digest reship via Koku masu. **DELETE** returns `204 No Content` and removes the override at that scope.

### Request body

```json
{
  "timezone": "America/New_York",
  "schedule": {
    "days": ["monday", "tuesday", "wednesday", "thursday", "friday"],
    "start_time": "08:00",
    "end_time": "17:00"
  },
  "off_hours_weight": 0.0,
  "enabled": true
}
```

| Field | Notes |
|-------|-------|
| `timezone` | IANA timezone for schedule boundaries |
| `schedule.days` | Lowercase English day names (`monday` … `sunday`) |
| `schedule.start_time` / `end_time` | 24-hour `HH:MM` in the configured timezone; `end_time` must be after `start_time` (overnight windows not supported) |
| `off_hours_weight` | `0.0`–`1.0`; weight for off-hours samples in BH percentiles (`0.0` = in-window only) |
| `enabled` | `false` keeps the row but disables BH digest generation for that scope |

### Effective schedule response

`GET .../effective` adds `resolved_from`: `namespace`, `cluster`, `org`, or `none`.

Cluster GET responses may include `reship_status`: `complete`, `pending`, or `forward_only`.

## Recommendation response

When a schedule applies and reship is complete:

`recommendation_engines.{cost|performance}.business_hours`

Same `amount`/`format` shape as the parent engine (CPU and memory requests/limits). Omitted when no schedule applies — clients do not need extra filters.

## Scope

**v1: container and namespace only.** Nodes, GPUs, PVCs, and VMs do not receive business-hours recommendations.

## Configuration

| Variable | Default | Purpose |
|----------|---------|---------|
| `ROS_BUSINESS_HOURS_ENABLED` | `true` | Kill-switch — when `false`, settings routes return 404, capabilities report `business_hours: false`, and only `all_hours` digests are produced |
| `ROS_BUSINESS_HOURS_RESHIP_FORWARD_ONLY_FALLBACK` | `false` | After max reship retries, accept forward-only data |
| `ROS_SETTINGS_LOCKED_BUSINESS_HOURS` | `true` (under global lock) | Blocks PUT/DELETE; GET returns `settings_locked: true` |

Discover availability via `GET .../settings/capabilities` (`business_hours: true|false`).

## Related documentation

- [Plugin reference — Business hours](../plugin-reference/business-hours.md)
- [Configurability — Business Hours](../architecture/configurability.md#business-hours)
- [UI integration — Business hours settings](../ui-integration-guide.md#business-hours-settings)
