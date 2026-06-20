# Honesty Exercise — PVC (Persistent Volume Claim) Recommendations

**Date:** 2026-06-20
**Scope:** Cross-source alignment audit for the PVC right-sizing recommendations feature
**Repos audited:** ros-ocp-backend, costmgmt-api-cheatsheet, cost-onprem-chart, iqe-ros-ocp-plugin, nise, koku, koku-ui, koku-metrics-operator

---

## Phase 1–2: Discovery and Audit Summary

### Feature Overview

PVC right-sizing analyzes PersistentVolumeClaim capacity vs. actual usage and classifies PVCs as:
- **oversized** — max usage < 20% of capacity (sustained `min_trend_days`, default 2)
- **near_full** — usage > 85% or growth-trend projects exhaustion within `days_to_full_alert` (default 30)
- **orphaned** — zero usage for `min_trend_days`+ (default 2)
- **healthy** — 20–85% utilization, no action

### Endpoints

| Endpoint | Method |
|----------|--------|
| `/recommendations/openshift/pvcs` | GET (list) |
| `/recommendations/openshift/pvcs/detail` | GET (single PVC, all terms + historical usage) |
| `/recommendations/openshift/settings/pvc` | GET/PUT/DELETE (thresholds) |
| `/recommendations/openshift/settings/terms?recommendation_type=pvc` | GET/PUT (term windows) |
| `/recommendations/openshift/notification-codes?filter[plugin]=pvc` | GET (PVC notification catalog) |
| `/recommendations/openshift/savings-summary` | GET (fleet rollup includes `by_plugin.pvc`) |

### Filters (list endpoint)

| Parameter | Type | Description |
|-----------|------|-------------|
| `filter[cluster]` | UUID | Cluster UUID (aliases: `cluster_uuid`, `cluster`) |
| `filter[project]` | string | Namespace (aliases: `namespace`, `project`) |
| `filter[recommendation_type]` | enum | `oversized`, `near_full`, `orphaned`, `healthy` |
| `filter[term]` | enum | `short`, `medium`, `long` (default `medium`); aliases `short_term`, `medium_term`, `long_term` |
| `filter[storageclass]` | string | StorageClass name |
| `filter[tag:<key>]` | string | Tag filter (when `ROS_TAGS_ENABLED=true`) |
| `order_by` | string | `usage_ratio` (default), `estimated_monthly_savings`, `pvc_name`/`persistentvolumeclaim`, `capacity_bytes` |
| `order_how` | string | `asc` or `desc` (default `desc`) |
| `limit` | int | 1–100 (default 20) |
| `offset` | int | Pagination offset |
| `after` | string | Keyset cursor from `meta.next_cursor` |
| `format` | string | `csv` for CSV export |

### Pagination

Both **offset** and **keyset** (`after` cursor) are supported. When `after` is set, `offset` is ignored.

### Response shape (list)

```json
{
  "meta": { "count": N, "limit": 20, "offset": 0, "has_next": false, "next_cursor": "...", "currency": "USD" },
  "links": { ... },
  "data": [
    {
      "cluster_uuid": "...",
      "namespace": "...",
      "persistentvolumeclaim": "...",
      "mounted_by": "...",
      "vm_name": "...",
      "persistentvolume": "...",
      "storageclass": "...",
      "capacity_bytes": 107374182400,
      "usage_bytes_max": 0,
      "usage_ratio": 0.0,
      "recommendation_type": "orphaned",
      "recommended_bytes": null,
      "days_to_full": null,
      "growth_bytes_per_day": 0,
      "estimated_monthly_savings": { "value": "12.50", "units": "USD" },
      "notifications": { "20": { "type": "WARNING", "message": "...", "code": 20 } },
      "confidence_level": 1.0,
      "data_days": 14,
      "term": "medium",
      "idle_since": "2026-05-01",
      "idle_duration_days": 14,
      "resize_note": "..."
    }
  ]
}
```

### CSV export

23 columns exported (matches Go code `pvcRecCSVHeader` in `handlers_list_csv.go`):
`cluster_uuid`, `namespace`, `persistentvolumeclaim`, `mounted_by`, `vm_name`,
`persistentvolume`, `storageclass`, `recommendation_type`, `usage_ratio`,
`capacity_bytes`, `usage_bytes_max`, `recommended_bytes`, `days_to_full`,
`growth_bytes_per_day`, `estimated_monthly_savings_value`, `estimated_monthly_savings_units`,
`confidence_level`, `idle_since`, `idle_duration_days`, `data_days`, `term`,
`resize_note`, `notification_codes`.

### Notification codes

| Code | Name | Severity | Trigger |
|------|------|----------|---------|
| 1 | LOW_CONFIDENCE | WARNING | `confidence_level` below threshold |
| 20 | PVC_ORPHANED | WARNING | Zero usage across all intervals |
| 25 | NO_COST_DATA | INFO | No cost data — savings not computed |
| 29 | PVC_OVERSIZED | INFO | Capacity exceeds sustained usage |
| 30 | PVC_NEAR_FULL | WARNING | Usage approaching capacity or growth trend |
| 77 | SPARSE_DATA | INFO | `data_days` at or below sparse threshold (default 2) |

Registered in `internal/notifications/catalog.go` under plugin `"pvc"`: `{1, 20, 25, 29, 30, 77}`.

### Terms

| Term | Window | Min data days | Max window |
|------|--------|--------------|------------|
| short | 7 days | 3 days | 365 days |
| medium | 30 days | 14 days | 365 days |
| long | 90 days | 30 days | 365 days |

### Savings

| Classification | Formula |
|----------------|---------|
| Oversized | `(current_gib − recommended_gib) × storage_rate_per_gib_month` |
| Orphaned | `current_gib × storage_rate_per_gib_month` (full reclaim) |

When cost data unavailable, notification code 25 is appended and savings are zero/omitted.

### Data source

koku-metrics-operator collects PVC metrics via Prometheus and writes them into
`cm-openshift-storage-usage-YYYYMM.csv`. No operator changes required for PVC data
collection. Prometheus queries: `kube_persistentvolume_capacity_bytes`,
`kubelet_volume_stats_used_bytes`, `kube_persistentvolumeclaim_resource_requests_storage_bytes`,
`persistentvolume_pod_info`, volume labels.

---

## Phase 3: Alignment Matrix

| Aspect | Requirements | Go Code | Internal Docs | Public Docs | Cheatsheet | Bruno | Unit Tests | E2E Tests | IQE Tests | OpenAPI |
|--------|:-----------:|:-------:|:-------------:|:-----------:|:----------:|:-----:|:----------:|:---------:|:---------:|:-------:|
| **List endpoint** `GET .../pvcs` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| **Detail endpoint** `GET .../pvcs/detail` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| **filter[cluster]** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | — | ✅ | ✅ | ✅ |
| **filter[project]** | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | ✅ | ✅ |
| **filter[recommendation_type]** | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | ✅ | ✅ |
| **filter[term]** (short/medium/long) | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | ✅ | ✅ |
| **filter[storageclass]** | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | ✅ | ✅ |
| **filter[tag:\<key\>]** | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | ✅ | ✅ |
| **order_by options** | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | — | — | ✅ |
| **Pagination: offset** | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | ✅ | ✅ |
| **Pagination: keyset (after)** | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | ✅ | ✅ |
| **Response: cluster_uuid** | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | ✅ | ✅ |
| **Response: namespace** | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | ✅ | ✅ |
| **Response: persistentvolumeclaim** | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | ✅ | ✅ |
| **Response: mounted_by** | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | ✅ | ✅ |
| **Response: vm_name** | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | ✅ | ✅ |
| **Response: storageclass** | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | ✅ | ✅ |
| **Response: capacity_bytes** | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | ✅ | ✅ |
| **Response: usage_bytes_max** | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | ✅ | ✅ |
| **Response: usage_ratio** | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | ✅ | ✅ |
| **Response: recommendation_type** | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | ✅ | ✅ |
| **Response: recommended_bytes** | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | — | — | ✅ |
| **Response: days_to_full** | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | ✅ | ✅ |
| **Response: growth_bytes_per_day** | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | ✅ | ✅ |
| **Response: estimated_monthly_savings** | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | ✅ | ✅ |
| **Response: notifications** (map) | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | ✅ | ✅ |
| **Response: confidence_level** | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | — | — | ✅ |
| **Response: data_days** | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | — | — | ✅ |
| **Response: term** | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | ✅ | ✅ |
| **Response: idle_since** | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | ✅ | ✅ |
| **Response: idle_duration_days** | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | ✅ | ✅ |
| **Response: resize_note** | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | — | — | ✅ |
| **Response: explanation** (detail only, `?include=explanation`) | — | ✅ | — | — | — | — | — | — | — | ✅ (added) |
| **Notification codes {1,20,25,29,30,77}** | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | ✅ | ✅ |
| **CSV export** | ✅ | ✅ | ✅ (fixed) | ✅ | ✅ | ✅ | — | ✅ | ✅ | ✅ |
| **CSV: 23 columns** | — | ✅ | ✅ (fixed) | ✅ | ✅ | — | — | — | — | — |
| **meta.currency** | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | — | — | ✅ |
| **Savings computation** | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | ✅ | ✅ |
| **Data generation (nise)** | — | — | — | — | — | — | — | ✅ | ✅ | — |
| **Operator PVC Prometheus queries** | ✅ | — | ✅ | ✅ | — | — | — | — | — | — |
| **Settings endpoints** | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | ✅ | ✅ |
| **Historical usage (detail)** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | — | ✅ | ✅ | ✅ |

**Legend:** ✅ aligned, ⚠️ partially aligned, ❌ wrong/missing, — not applicable

---

## Phase 4: Discrepancies Found and Fixed

### Discrepancy 1: CSV export column list in docs was stale (FIXED)

**What was wrong:** Two documentation files claimed CSV export had only 11 columns
and stated "Growth, idle, and notification fields are JSON-only (not exported to CSV
today)." The actual Go code exports **23 columns** including `mounted_by`, `vm_name`,
`persistentvolume`, `recommended_bytes`, `days_to_full`, `growth_bytes_per_day`,
`confidence_level`, `idle_since`, `idle_duration_days`, `data_days`, `resize_note`,
and `notification_codes`.

**Authoritative source:** Go code (`internal/api/handlers_list_csv.go` line 14–21).

**Files fixed:**
- `docs/features-f27-pvc-rightsizing.md` — Updated CSV column list from 11 to 23 columns, removed "JSON-only" claim
- `docs-site/features/pvc-rightsizing.md` — Same fix

### Discrepancy 2: OpenAPI missing PVCExplanation schema (FIXED)

**What was wrong:** The Go code supports `?include=explanation` on the PVC detail
endpoint, returning a `PVCExplanationAPI` struct with classification driving factors.
The OpenAPI spec did not document the `explanation` field on `PVCRecommendation` or
include a `PVCExplanation` schema.

**Authoritative source:** Go code (`internal/model/explanation_api.go` lines 57–68,
`internal/api/handlers_pvc_detail.go` line 106).

**Files fixed:**
- `openapi.json` — Added `PVCExplanation` schema with all 9 fields, added `explanation` property to `PVCRecommendation`

---

## Phase 5: Verification

See commit for test and build results.

---

## Phase 6: Report

### What works end-to-end

The PVC recommendations feature is **fully implemented and well-documented**. The end-to-end pipeline works:

1. **Data collection** — koku-metrics-operator collects PVC capacity/usage/request metrics from Prometheus via existing queries (`kube_persistentvolume_capacity_bytes`, `kubelet_volume_stats_used_bytes`, etc.) and writes `cm-openshift-storage-usage-YYYYMM.csv`. No operator changes were needed.

2. **Ingestion** — ros-ocp-backend parses storage CSV rows, computes daily PVC digests (`daily_pvc_digests` table), and runs the recommendation engine.

3. **Classification** — The Go engine (`internal/engine/pvc_recommend.go`) correctly classifies PVCs as oversized/near_full/orphaned/healthy using configurable thresholds. Growth trend projection uses OLS/WLS linear regression on daily average usage.

4. **Savings** — Dollar savings computed for oversized (capacity delta) and orphaned (full reclaim) using Masu storage rates. Code 25 when cost data unavailable.

5. **API** — List, detail, settings, notification-codes, savings-summary endpoints all work with proper filters, pagination (offset + keyset), and CSV export.

6. **Notification codes** — All 6 codes ({1, 20, 25, 29, 30, 77}) correctly registered in `catalog.go` for the `pvc` plugin.

7. **E2E tests** — Comprehensive coverage in `cost-onprem-chart/tests/suites/ros/test_pvc_recommendations.py` with deterministic nise fixture data (`ocp_report_pvc_rightsizing.yml`) covering all 4 classification types + VM-owned PVCs.

8. **IQE tests** — Full coverage in `iqe-ros-ocp-plugin/iqe_ros_ocp/tests/rest/test_pvc_recommendations.py` matching E2E patterns.

9. **Documentation** — Internal docs (`docs/features-f27-pvc-rightsizing.md`), public docs (`docs-site/features/pvc-rightsizing.md`, `docs-site/plugin-reference/pvc.md`), cheatsheet, and Bruno collection all cover PVC recommendations.

### What was broken or misaligned (now fixed)

1. **CSV column documentation** — Two doc files claimed 11 CSV columns and "JSON-only" for growth/idle/notification fields. Reality: 23 columns with full field coverage. **Fixed.**

2. **OpenAPI missing PVCExplanation** — The `?include=explanation` feature on PVC detail was undocumented in the OpenAPI spec. **Fixed** by adding `PVCExplanation` schema and `explanation` field on `PVCRecommendation`.

### What's genuinely missing

1. **Bruno coverage** — The Bruno collection has basic PVC requests (list, detail, CSV export) but does not exercise all filters (e.g., `filter[recommendation_type]`, `filter[storageclass]`, `filter[tag:*]`). This is a minor convenience gap, not a correctness issue.

2. **Unit test coverage for CSV export** — There are no dedicated unit tests for `generatePVCRecCSV()` in Go. The function is exercised by E2E/IQE tests but not by Go unit tests. Low risk since the CSV generation is straightforward string formatting.

3. **E2E/IQE tests don't assert `confidence_level` or `resize_note`** — These fields are returned by the API and present in the Go code but not explicitly asserted in E2E or IQE tests. They are, however, implicitly present in the response objects.

### Feature maturity comparison

| Aspect | Container Recs | PVC Recs | Node Recs |
|--------|:-----------:|:--------:|:---------:|
| Classification types | 3 (idle/active/zombie) | 4 (oversized/near_full/orphaned/healthy) | 5 (underutilized/overcommitted/stranded/idle/well-utilized) |
| Savings estimation | ✅ | ✅ | ✅ |
| CSV export | ✅ | ✅ (23 columns) | ✅ |
| Growth trend projection | — | ✅ (OLS/WLS) | ✅ (EMA-smoothed LR) |
| Multi-term support | ✅ (short/medium/long) | ✅ (short/medium/long) | ✅ (short/medium/long) |
| Explanation API | ✅ | ✅ | ✅ |
| Keyset pagination | ✅ | ✅ | ✅ |
| Tag filters | ✅ | ✅ | — |
| Settings API | ✅ | ✅ | ✅ |
| E2E test coverage | ✅ | ✅ | ✅ |
| Nise fixture data | ✅ | ✅ | ✅ |
| Business hours | ✅ | — (N/A for storage) | — |

PVC recommendations are at **feature parity** with container and node recommendations for the aspects that apply to storage. Business hours weighting is correctly excluded (storage patterns don't have business-hour seasonality).

### Design questions (none)

No design questions or ambiguities remain. The feature is well-specified in `docs/archive/requirements.md` (REQ-6.3), the implementation matches requirements, and the documentation is now accurate.

---

## Checklist

- [x] PVC capacity/request/usage metrics from operator match what ros-ocp-backend expects
- [x] Storage class information flows through the pipeline
- [x] OpenAPI documents PVC endpoints fully (including `PVCExplanation`, added)
- [x] Bruno has PVC requests (list, detail, CSV export)
- [x] Cheatsheet covers PVC recommendations
- [x] Notification codes for PVC are in catalog.go with correct plugin `{1, 20, 25, 29, 30, 77}`
- [x] Nise generates storage/PVC data (dedicated `ocp_report_pvc_rightsizing.yml` fixture)
- [x] E2E tests cover PVC data flow (oversized, near_full, orphaned, healthy, VM-owned)
- [x] Savings computation works for PVCs (oversized: delta savings, orphaned: full reclaim)
- [x] CSV export includes PVC-specific fields (23 columns, all fields exported)
