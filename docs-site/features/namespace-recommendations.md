# Namespace Recommendations

!!! info "Quick Facts"
    **API:** `GET /api/cost-management/v1/recommendations/openshift/namespaces` (list),
    `GET .../namespaces/{id}` (detail),
    `GET .../namespaces/{id}/history` (per-term history)  
    **Engines:** cost, performance (both stored; `filter[engine]` limits list/detail)  
    **Savings:** No dollar field — aggregate from container recommendations

## Overview

The **namespace** plugin rolls up container usage digests per OpenShift namespace and
recommends CPU/memory **request** and **limit** targets for each term × engine. It
complements the [ResourceQuota](quota-recommendations.md) plugin, which right-sizes
existing `ResourceQuota` **hard** limits.

## API quick reference

| Concern | Detail |
|---------|--------|
| List filters | `filter[cluster]`, `filter[project]` (alias `filter[namespace]`), `filter[idle_state]`, `filter[stale]`, `filter[engine]`, `filter[tag:*]` |
| List sorting | `order_by`: `cluster`, `project`, `last_reported`, and 12 `*_variation_*` columns |
| History only | `filter[term]` (`short_term` / `medium_term` / `long_term`), `filter[engine]` |
| CSV | `Accept: text/csv` or `?format=csv` on the list endpoint |
| Terms | `GET/PUT/DELETE .../settings/terms?recommendation_type=namespace` |
| Business hours | Dual `all_hours` / `business_hours` sizing on detail when enabled — [Business hours](business-hours.md) |

Full parameter tables and handlers: [namespace plugin reference](../plugin-reference/namespace.md).

## Notification codes

| Code | Name | Severity | Message |
|------|------|----------|---------|
| 1 | `LOW_CONFIDENCE` | WARNING | Less than 4 days of data available for this workload |
| 2 | `STALE_DATA` | WARNING | No new metrics data received for more than 48 hours |
| 7 | `NEW_WORKLOAD` | INFO | Less than 24 hours of data — recommendation may be unstable |
| 9 | `MEMORY_TRENDING_UP` | WARNING | Memory usage trend suggests capacity risk within 30 days |

Catalog: `GET .../notification-codes?filter[plugin]=namespace`. See [Notification codes — Namespaces](../architecture/notification-codes.md#namespaces).

## Related

- [Dual engine (cost vs performance)](dual-engine.md)
- [Idle / zombie detection](idle-detection.md) — namespace `idle_state` aggregation
- Internal design: [`docs/features/namespace-recommendations.md`](../../docs/features/namespace-recommendations.md)
