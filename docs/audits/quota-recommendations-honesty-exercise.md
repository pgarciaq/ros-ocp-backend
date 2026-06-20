# Honesty Exercise — Quota (Namespace ResourceQuota) Recommendations

**Date:** 2026-06-20
**Auditor:** AI (Opus 4.6)
**Feature:** Namespace ResourceQuota right-sizing recommendations
**Plugin:** `quota` (priority 35)

---

## Executive Summary

The namespace ResourceQuota recommendations feature is **fully implemented** across the
entire stack — from Prometheus metric collection in the koku-metrics-operator through
data generation (Nise), ingestion (Koku listener), processing (ROS-OCP engine), API
endpoints (list, detail, settings, CSV export, notification codes), documentation
(internal feature docs, public plugin reference, API cheatsheet, Bruno collection,
OpenAPI spec), and testing (E2E and IQE suites).

**Discrepancies found and fixed:** 2 (both documentation-only)

The feature is mature and well-aligned across all sources. The only misalignment was
that two documentation files listed an abbreviated set of CSV export columns instead
of the actual 37-column header produced by the code.

---

## Feature Overview

Namespace ResourceQuota recommendations compare configured Kubernetes ResourceQuota
hard limits against observed usage and aggregated container recommendation totals, then
classify each namespace as:

| Type | Meaning |
|------|---------|
| `tighten` | Over-provisioned — quota can be reduced (savings opportunity) |
| `raise` | Under-provisioned — usage approaching or exceeding limits |
| `optimal` | Current limits are well-sized |
| `none` | Insufficient data to generate a recommendation |

Risk levels (`high`, `medium`, `low`, `none`) are derived from utilization thresholds
(default 90% high, 70% medium — see ADR-0029).

---

## Sources Audited

| # | Source | Location | Status |
|---|--------|----------|--------|
| 1 | Requirements/design | `ros-ocp-backend/docs/features/quota-recommendations.md` | ✅ Comprehensive |
| 2 | ADR | `ros-ocp-backend/docs/adr/0029-quota-headroom-10-percent-70-90-risk-bands.md` | ✅ Present |
| 3 | Go implementation — plugin | `ros-ocp-backend/internal/plugins/quota/plugin.go` | ✅ Implemented |
| 4 | Go implementation — list handler | `ros-ocp-backend/internal/api/handlers_quota_recs.go` | ✅ Implemented |
| 5 | Go implementation — detail handler | `ros-ocp-backend/internal/api/handlers_quota_detail.go` | ✅ Implemented |
| 6 | Go implementation — order_by | `ros-ocp-backend/internal/api/handlers_quota_order.go` | ✅ Implemented |
| 7 | Go implementation — CSV export | `ros-ocp-backend/internal/api/handlers_list_csv.go` | ✅ Implemented |
| 8 | Internal architecture docs | `ros-ocp-backend/docs/features/quota-recommendations.md` | ✅ (fixed) |
| 9 | Public website docs | `ros-ocp-backend/docs-site/plugin-reference/quota.md` | ✅ (fixed) |
| 10 | API cheatsheet | `costmgmt-api-cheatsheet/costmgmt-api-cheatsheet.adoc` | ✅ Accurate |
| 11 | Bruno collection | `costmgmt-api-cheatsheet/bruno/Optimizations/` (23 files) | ✅ Comprehensive |
| 12 | OpenAPI spec | `ros-ocp-backend/openapi.json` | ✅ Complete |
| 13 | Notification catalog | `ros-ocp-backend/internal/notifications/catalog.go` | ✅ Correct |
| 14 | Notification names | `ros-ocp-backend/internal/notifications/names.go` | ✅ Correct |
| 15 | Nise data generation | `nise/nise/generators/ocp/ocp_generator.py` | ✅ Generates quota data |
| 16 | Operator metrics | `koku-metrics-operator/internal/collector/quota_namespace_queries.go` | ✅ Collects metrics |
| 17 | E2E tests | `cost-onprem-chart/tests/suites/ros/test_quota_recommendations.py` | ✅ Comprehensive |
| 18 | IQE tests | `iqe-ros-ocp-plugin/iqe_ros_ocp/tests/rest/test_quota_recommendations.py` | ✅ Comprehensive |
| 19 | Koku backend | Listener processes `ocp_ros_namespace_usage` CSVs | ✅ Functional |
| 20 | Koku-UI | No quota UI components found | — Deferred (by design) |

---

## Alignment Matrix

| Aspect | Requirements | Go Code | Internal Docs | Public Docs | Cheatsheet | Bruno | OpenAPI | E2E Tests | IQE Tests | Nise | Operator |
|--------|:-----------:|:-------:|:-------------:|:-----------:|:----------:|:-----:|:-------:|:---------:|:---------:|:----:|:--------:|
| **Endpoints** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | — | — |
| **Filters** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | — | — |
| **Pagination** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | — | — |
| **Response shape** | ✅ | ✅ | ✅ | ✅ | ✅ | — | ✅ | ✅ | ✅ | — | — |
| **CSV export** | ✅ | ✅ | ✅ (fixed) | ✅ (fixed) | ✅ | ✅ | ✅ | ✅ | ✅ | — | — |
| **Notification codes** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | — | — |
| **Rec types** | ✅ | ✅ | ✅ | ✅ | ✅ | — | ✅ | ✅ | ✅ | — | — |
| **`meta.currency`** | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | — | — | — | — |
| **`confidence_level`** | — | — | — | — | — | — | — | — | — | — | — |
| **Data generation** | — | — | ✅ | — | — | — | — | — | — | ✅ | — |
| **Savings** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | — | — |
| **Metrics collection** | ✅ | — | ✅ | — | — | — | — | — | — | — | ✅ |
| **Settings endpoint** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | — | ✅ | — | — |
| **group_by** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | — | ✅ | — | — |
| **order_by** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | — | — |

**Legend:** ✅ Aligned | ⚠️ Partially aligned | ❌ Wrong/missing | — Not applicable

---

## Discrepancies Found and Fixed

### 1. CSV Export Column List — Internal Feature Docs (FIXED)

**File:** `ros-ocp-backend/docs/features/quota-recommendations.md`

**Problem:** Documentation claimed CSV export includes only 9 columns (`cluster_uuid`,
`namespace`, `quota_name`, `recommendation_type`, `risk_level`, `estimated_savings_value`,
`estimated_savings_units`, `last_observed_at`, `count`).

**Reality:** The Go code (`handlers_list_csv.go`, `quotaRecCSVHeader`) produces 37 columns
including full `quota_hard_*`, `quota_used_*`, `quota_recommended_*` resource blocks,
`utilization_*_percent` columns, `capacity_freed_*` columns, and `notification_codes`.

**Authoritative source:** Go code (it generates the actual CSV).

**Fix:** Updated the feature doc to describe the full 37-column CSV header.

### 2. CSV Export Column List — Public Plugin Reference (FIXED)

**File:** `ros-ocp-backend/docs-site/plugin-reference/quota.md`

**Problem:** Same abbreviated column list as the internal docs.

**Fix:** Updated to describe the full 37-column CSV header.

---

## Detailed Audit Results

### API Endpoints

| Endpoint | Method | Purpose | Documented | Implemented | Tested |
|----------|--------|---------|:----------:|:-----------:|:------:|
| `/recommendations/openshift/quota/` | GET | List recommendations | ✅ | ✅ | ✅ |
| `/recommendations/openshift/quota/detail` | GET | Single recommendation detail | ✅ | ✅ | ✅ |
| `/recommendations/openshift/settings/quota` | GET/PUT/DELETE | Per-org thresholds | ✅ | ✅ | ✅ |
| `/recommendations/openshift/notification-codes/?filter[plugin]=quota` | GET | Notification catalog | ✅ | ✅ | ✅ |

### Filters (List Endpoint)

| Filter | Code | Cheatsheet | OpenAPI | E2E | IQE |
|--------|:----:|:----------:|:-------:|:---:|:---:|
| `filter[cluster]` | ✅ | ✅ | ✅ | ✅ | ✅ |
| `filter[project]` | ✅ | ✅ | ✅ | ✅ | ✅ |
| `filter[quota_name]` | ✅ | ✅ | ✅ | ✅ | ✅ |
| `filter[resource_quota_name]` (alias) | ✅ | ✅ | ✅ | — | — |
| `filter[recommendation_type]` | ✅ | ✅ | ✅ | — | ✅ |
| `filter[risk_level]` | ✅ | ✅ | ✅ | — | ✅ |
| `filter[tag:<key>]` | ✅ | ✅ | ✅ | ✅ | ✅ |
| `tag=key:value` (legacy) | ✅ | — | ✅ | — | — |

### Order By

| Field | Code | Cheatsheet | OpenAPI |
|-------|:----:|:----------:|:-------:|
| `namespace` (default) | ✅ | ✅ | ✅ |
| `quota_name` | ✅ | ✅ | ✅ |
| `utilization` | ✅ | ✅ | ✅ |
| `estimated_monthly_savings` | ✅ | ✅ | ✅ |
| `risk_level` | ✅ | ✅ | ✅ |

Default sort: `namespace asc` (no explicit `order_by`). When `order_by` IS set and
`order_how` is omitted, defaults to `desc`.

### Pagination

| Style | Implemented | Documented |
|-------|:-----------:|:----------:|
| Offset (`limit` + `offset`) | ✅ | ✅ |
| Keyset (`after` cursor) | ✅ | ✅ |

### Notification Codes

| Code | Name | Applies To | In catalog.go | In cheatsheet | In IQE |
|------|------|-----------|:-------------:|:-------------:|:------:|
| 70 | `QUOTA_NEAR_CAPACITY` | quota, cluster-quota | ✅ | ✅ | ✅ |
| 71 | `QUOTA_OVERSIZED` | quota, cluster-quota | ✅ | ✅ | ✅ |
| 72 | `QUOTA_BLOCKING` | quota, cluster-quota | ✅ | ✅ | ✅ |
| 73 | `CLUSTER_QUOTA_AT_CAPACITY` | cluster-quota only | ✅ | ✅ | — |

### Response Shape (JSON)

```json
{
  "meta": {
    "count": 42,
    "limit": 20,
    "offset": 0,
    "has_next": true,
    "next_cursor": "...",
    "currency": "USD"
  },
  "links": { "first": "...", "next": "...", "previous": null, "last": "..." },
  "data": [
    {
      "cluster_uuid": "...",
      "namespace": "production",
      "quota_name": "compute-resources",
      "recommendation_type": "tighten",
      "risk_level": "low",
      "quota_hard": {
        "cpu_request_millicores": 8000,
        "cpu_limit_millicores": 16000,
        "memory_request_bytes": 17179869184,
        "memory_limit_bytes": 34359738368,
        "storage_request_bytes": 107374182400,
        "pods": 50
      },
      "quota_used": { ... },
      "quota_recommended": { ... },
      "utilization": {
        "cpu_request_percent": 45.5,
        "cpu_limit_percent": 22.3,
        "memory_request_percent": 60.2,
        "memory_limit_percent": 31.1,
        "storage_request_percent": 15.0,
        "pods_percent": 30.0
      },
      "capacity_freed": {
        "cpu_millicores": 4000,
        "memory_bytes": 8589934592,
        "storage_request_bytes": 0,
        "pods_freed": 20
      },
      "estimated_savings": {
        "value": 125.50,
        "units": "USD"
      },
      "notification_codes": [71],
      "last_observed_at": "2026-06-20T10:00:00Z",
      "count": 1
    }
  ]
}
```

### CSV Export

The CSV export produces **37 columns** (verified from `quotaRecCSVHeader` in
`handlers_list_csv.go`):

```
cluster_uuid, namespace, quota_name, recommendation_type, risk_level,
quota_hard_cpu_request_millicores, quota_hard_cpu_limit_millicores,
quota_hard_memory_request_bytes, quota_hard_memory_limit_bytes,
quota_hard_storage_request_bytes, quota_hard_pods,
quota_used_cpu_request_millicores, quota_used_cpu_limit_millicores,
quota_used_memory_request_bytes, quota_used_memory_limit_bytes,
quota_used_storage_request_bytes, quota_used_pods,
quota_recommended_cpu_request_millicores, quota_recommended_cpu_limit_millicores,
quota_recommended_memory_request_bytes, quota_recommended_memory_limit_bytes,
quota_recommended_storage_request_bytes, quota_recommended_pods,
utilization_cpu_request_percent, utilization_cpu_limit_percent,
utilization_memory_request_percent, utilization_memory_limit_percent,
utilization_storage_request_percent, utilization_pods_percent,
capacity_freed_cpu_millicores, capacity_freed_memory_bytes,
capacity_freed_storage_request_bytes, capacity_freed_pods,
estimated_savings_value, estimated_savings_units, last_observed_at,
notification_codes, count
```

### Savings Computation

- Savings are computed only for `tighten` recommendations (over-provisioned quotas)
- Monetized for CPU, memory, and storage (using cluster cost data)
- Pods and object counts are NOT monetized (no FinOps unit cost for pods)
- `estimated_savings` uses `MoneyAmount` format (`{value, units}`)
- `meta.currency` reflects the cluster's configured currency

### Metrics Collection (Operator)

The operator collects 14 `kube_resourcequota` metric series per namespace:

| Metric Key | Resource | Type |
|------------|----------|------|
| `ros:cpu_request_namespace_sum` | `requests.cpu` | hard |
| `ros:cpu_request_namespace_used` | `requests.cpu` | used |
| `ros:cpu_limit_namespace_sum` | `limits.cpu` | hard |
| `ros:cpu_limit_namespace_used` | `limits.cpu` | used |
| `ros:memory_request_namespace_sum` | `requests.memory` | hard |
| `ros:memory_request_namespace_used` | `requests.memory` | used |
| `ros:memory_limit_namespace_sum` | `limits.memory` | hard |
| `ros:memory_limit_namespace_used` | `limits.memory` | used |
| `ros:storage_request_namespace_hard` | `requests.storage` | hard |
| `ros:storage_request_namespace_used` | `requests.storage` | used |
| `ros:pods_namespace_hard` | `pods` | hard |
| `ros:pods_namespace_used` | `pods` | used |
| `ros:object_count_namespace_hard` | `count/*` | hard |
| `ros:object_count_namespace_used` | `count/*` | used |

Metrics are filtered to namespaces with
`label_insights_cost_management_optimizations='true'` or
`label_cost_management_optimizations='true'`, excluding system namespaces.

### Data Generation (Nise)

Nise generates namespace quota data via `OCP_ROS_NAMESPACE_USAGE` report type:
- `namespace_quota_used_values()` produces CPU/memory hard/used data
- `namespace_quota_extended_values()` produces storage/pods/object-count data
- Static YAML configuration drives quota sizing per namespace
- Data flows as `ocp_ros_namespace_usage.csv` in the upload tarball

### `confidence_level`

Not applicable to quota recommendations. Quota recommendations are
deterministic threshold-based calculations (not ML-based), so there is no
confidence level — this is correct by design.

---

## Checklist

- [x] Operator collects `kube_resourcequota` metrics (14 metric series)
- [x] Quota data flows through Koku listener to ROS processor
- [x] OpenAPI documents quota endpoints fully (list, detail, settings)
- [x] Bruno has quota requests (23 files covering all aspects)
- [x] Cheatsheet covers quota recommendations
- [x] Notification codes for quotas are in `catalog.go` with correct plugin (70, 71, 72)
- [x] Nise generates namespace quota data (`OCP_ROS_NAMESPACE_USAGE`)
- [x] E2E tests cover quota data flow
- [x] Savings computation works for quotas (CPU, memory, storage — not pods)
- [x] CSV export includes quota-specific fields (37 columns)
- [x] Namespace-level aggregation logic is correct (`group_by[cluster]`, `group_by[project]`)

---

## Feature Maturity Assessment

**Rating: Fully Implemented**

The namespace ResourceQuota recommendations feature is complete and mature across
the entire stack:

1. **Data pipeline:** Operator collects metrics → Nise generates test data →
   Koku listener processes CSVs → ROS engine computes recommendations
2. **API:** List, detail, settings, CSV export, notification codes — all implemented
   and documented
3. **Filters/ordering:** Comprehensive filter set with both offset and keyset pagination
4. **Documentation:** Internal feature doc, public plugin reference, API cheatsheet,
   Bruno collection, OpenAPI spec — all aligned (after the CSV column fix)
5. **Testing:** E2E tests in cost-onprem-chart + IQE tests in iqe-ros-ocp-plugin
6. **Notifications:** Three codes (70-72) specific to namespace quotas

**Not implemented (by design):**
- **Koku-UI:** No quota recommendation UI components. The feature doc indicates
  UI is "deferred" — the backend API is ready but no frontend exists yet.
- **`confidence_level`:** Not applicable — quotas use deterministic thresholds.

**No design questions for the user.** The feature is well-aligned and the only
fixes needed were documentation corrections.
