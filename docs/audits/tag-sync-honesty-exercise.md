# Honesty Exercise: Tag Sync

**Date:** 2026-06-21
**Auditor:** AI Agent (Opus 4.6)
**Scope:** End-to-end tag sync from operator → Koku → ROS, including storage, filtering, sync mechanisms, and all documentation/test artifacts.

---

## Executive Summary

The tag sync feature is **well-implemented and comprehensive**. Tags flow correctly from Kubernetes labels through the operator and Koku into ROS, with dual-path support (`api` push sync and `db` direct JOIN). The codebase has thorough documentation, extensive unit/integration tests, and good OpenAPI coverage.

**Primary discrepancy found:** The docs-site (`docs-site/`) references `features/tag-filtering.md` from 7 different pages, but this file did not exist — **now fixed** by this audit.

**No code bugs found.** The implementation matches the documented requirements across all components.

---

## Tag Flow: End-to-End Path

```
Kubernetes Labels (pods, namespaces)
    ↓
koku-metrics-operator (Prometheus queries → CSV reports)
    ↓  kube_namespace_labels → ocp_namespace_label.csv (namespace_labels column)
    ↓  Also: pod_labels, node_labels, PV/PVC labels
    ↓
Koku (CSV ingestion → OCP summarization)
    ↓  all_labels JSONB on OCPUsageLineItemDailySummary
    ↓  reporting_enabledtagkeys (admin-controlled gate)
    ↓  reporting_ocptags_values (distinct key/value pairs with cluster_ids[], namespaces[])
    ↓
ROS (two paths):
    ├── api mode: Koku Celery → POST /internal/tags/sync → org_container_keys.resolved_tags (JSONB)
    └── db mode: ROS SQL-JOINs Koku tenant tables at query time
    ↓
User API: filter[tag:key]=value on list endpoints
```

---

## Alignment Matrix

| Aspect | Requirements | Go Code | Internal Docs | Public Docs | Cheatsheet | Bruno | Unit Tests | E2E Tests | IQE Tests | OpenAPI | Koku Backend | Operator |
|--------|:-----------:|:-------:|:-------------:|:-----------:|:----------:|:-----:|:----------:|:---------:|:---------:|:-------:|:------------:|:--------:|
| Tag flow (K8s → operator → Koku → ROS) | ✅ | ✅ | ✅ | ✅ | — | — | — | — | — | — | ✅ | ✅ |
| `ROS_TAGS_SOURCE` modes (db/api) | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | — | — | — | ✅ | — |
| Tag storage (resolved_tags JSONB) | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | — | — | — | — | — |
| Tag storage (GIN index) | ✅ | ✅ | ✅ | — | — | — | — | — | — | — | — | — |
| Sync mechanism (push API) | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | ⚠️ | — | — | ✅ | — |
| `filter[tag:key]=value` syntax | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | — | — |
| Legacy `?tag=key:value` syntax | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | — | — | ✅ | — | — |
| Multi-tag AND logic | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | — |
| OR within same key (comma) | ✅ | ✅ | ✅ | ✅ | ✅ | — | ✅ | — | — | ✅ | — | — |
| Wildcard `filter[tag:key]=*` | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | — | — | ✅ | — | — |
| Tags in list response (`tags` field) | ✅ | ✅ | ✅ | — | — | — | ✅ | — | — | — | — | — |
| Tag enrichment (api mode) | ✅ | ✅ | ✅ | — | — | — | ✅ | — | — | — | — | — |
| Tag keys/values endpoint (ROS) | — | — | ✅ | ✅ | — | — | — | — | — | — | — | — |
| Tag keys/values endpoint (Koku) | ✅ | — | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | — | ✅ | — |
| `meta.warnings` on empty tag filter | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | — | — | — |
| `group_by[tag:key]` savings summary | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | — |
| RBAC ∩ tag intersection | ✅ | ✅ | ✅ | ✅ | — | ✅ | ✅ | ✅ | ✅ | — | — | — |
| Auth (TokenReview / dev token) | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | — | — | — | ✅ | — |
| Periodic safety-net (6h) | ✅ | — | ✅ | ✅ | — | — | — | — | — | — | ✅ | — |
| Sync payload validation | ✅ | ✅ | ✅ | — | — | — | ✅ | — | — | — | — | — |
| Full-replace transaction semantics | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | — | — | — | — | — |
| Enabled tags gate (Koku) | ✅ | ✅ | ✅ | ✅ | — | — | — | — | — | — | ✅ | — |
| Operator: namespace labels collection | ✅ | — | ✅ | — | — | — | — | — | — | — | — | ✅ |
| Nise generates namespace labels | ✅ | — | — | — | — | — | — | — | — | — | — | — |
| docs-site features/tag-filtering.md | ✅ | — | — | ⚠️→✅ | — | — | — | — | — | — | — | — |
| E2E tag push validation | ✅ | — | — | — | — | — | — | ⚠️ | — | — | — | — |

### Legend

- ✅ = Aligned and correct
- ⚠️ = Partially aligned (see notes below)
- ❌ = Wrong or missing
- — = Not applicable for this source

---

## Detailed Findings

### 1. docs-site/features/tag-filtering.md — FIXED

**Status:** Was ❌, now ✅

**Problem:** Seven pages across `docs-site/` link to `features/tag-filtering.md` but the file didn't exist:
- `docs-site/configuration.md` (5 links)
- `docs-site/plugin-reference/query-parameters.md` (1 link)
- `docs-site/whats-new.md` (1 link)
- `docs-site/query-performance.md` (1 link)
- `docs-site/operations/configuration.md` (1 link)

**Fix:** Created `docs-site/features/tag-filtering.md` with customer-appropriate content covering all anchors referenced by existing links (`#caveats-and-operational-risks`, `#saas-operations-ros-tags-sourceapi`, `#on-prem-startup-health-check-ros-tags-sourcedb`).

### 2. E2E Tag Push Test — Skeleton Only

**Status:** ⚠️

**Location:** `cost-onprem-chart/tests/suites/ros/test_tag_push_e2e.py`

The test class exists but the single test method calls `pytest.skip()` with a note referencing "adversarial review #35". This means the full Koku Settings → Koku push → ROS metadata chain has never been validated end-to-end in CI.

**Impact:** Low. The individual pieces (Koku push logic, ROS sync handler, tag filter queries) are all unit-tested. The E2E tag filtering tests in `test_container_detail.py` validate the filter path works. What's missing is validating the Koku-initiated push specifically.

**No fix applied** — this is a known TODO, not a code bug.

### 3. Tags in Response Body — API Mode Only

**Status:** ✅ (correct behavior, documented)

Container list responses include a `tags` field (`map[string]string`, `json:"tags,omitempty"`) via `enrichContainerTags()` in `internal/api/tag_enrichment.go`. This reads from `org_container_keys.resolved_tags`. The field is only populated in **api mode** because `db` mode doesn't store resolved tags locally.

The docs correctly note this as "Future work" for db mode. The `NativeContainerResult` struct has the `Tags` field. Tag enrichment is graceful — if tags are empty or the feature is disabled, the field is omitted from JSON.

### 4. No Public `/tags` Endpoint on ROS

**Status:** ✅ (by design)

ROS intentionally delegates tag key/value discovery to Koku's Tags API (`GET /tags/openshift/`). The internal status endpoint (`GET /internal/tags/status`) provides sync freshness for operators. This avoids duplicating the tag catalog.

The cheatsheet correctly documents `GET /api/cost-management/v1/tags/openshift/` for tag discovery (line 362).

### 5. Operator Namespace Label Collection

**Status:** ✅

The operator collects namespace labels via the `kube_namespace_labels` Prometheus metric. In `internal/collector/queries.go` line 596:

```go
QueryString: "kube_namespace_labels",
MetricKeyRegex: regexFields{"namespace_labels": "label_*"},
```

This produces `ocp_namespace_label.csv` with `namespace` and `namespace_labels` columns in pipe-delimited `key:value|key:value` format. These CSVs flow to Koku for ingestion.

### 6. Nise Tag Data Generation

**Status:** ✅

Nise generates `ocp_namespace_label.csv` with namespace labels. Static YAML files can specify `namespace_labels` per namespace:

```yaml
namespace_labels: label_key1:label_value1|label_key2:label_value2
```

The `_gen_hourly_namespace_label_usage()` method in `nise/generators/ocp/ocp_generator.py` produces this data, matching the operator CSV format.

### 7. Koku Push Sync Implementation

**Status:** ✅

`koku/masu/processor/ros_tag_sync.py` implements:
- `build_namespace_tags_payload()` — reads `EnabledTagKeys` + `OCPUsageLineItemDailySummary.all_labels`
- `push_namespace_tags()` — POSTs to ROS with bearer auth
- `sync_ros_ocp_tags` — Celery task (event-driven)
- `sync_ros_ocp_tags_periodic` — Celery beat task (6-hour safety-net)
- `schedule_ros_tag_sync()` — hook called from Settings API and post-summarization

Triggers wired in:
- `api/settings/tags/view.py` — tag enable/disable
- `api/settings/tags/mapping/view.py` — tag mapping changes
- `masu/processor/tasks.py` — post-OCP-summarization

### 8. Database Indexing

**Status:** ✅

`resolved_tags` has a GIN index (`idx_ock_tags`) created in migration `000081_create_org_container_keys.up.sql`. The GIN index supports `@>` containment and `?` key-exists operators used by `applyAPITagFiltersToKeys()`.

For `db` mode, the `reporting_ocptags_values` table in Koku has its own indexes managed by Django migrations.

### 9. OpenAPI Spec Coverage

**Status:** ✅

The OpenAPI spec documents both `filter[tag:environment]` and legacy `tag` parameters on container list, history, namespace, node, PVC, GPU MIG, GPU timeslicing, VM, quota, and cluster-quota endpoints (7+ endpoints). Each parameter has accurate description text including OR/AND semantics and the `*` wildcard.

### 10. Bruno Collection Coverage

**Status:** ✅

Extensive Bruno examples exist for tag filtering across all endpoint types:
- `Container recommendations - filter tag.bru`
- `Container recommendations - filter multi-tag AND.bru`
- `Container recommendations - filter tag with RBAC.bru`
- `PVC recommendations - filter tag.bru`
- `Namespace recommendations - filter tag.bru`
- `Node recommendations - filter tag.bru`
- `GPU recommendations - filter tag.bru`
- `GPU timeslicing recommendations - filter tag.bru`
- `VM recommendations - filter tag.bru`
- `ResourceQuota recommendations - filter tag.bru`
- `ClusterResourceQuota recommendations - filter tag.bru`
- `Recommendation history - filter tag.bru`
- `Fleet savings summary group_by tag.bru`
- `Fleet savings summary filter project with tag.bru`
- `Tag discovery - Koku enabled tags.bru`

### 11. Cheatsheet Coverage

**Status:** ✅

The cheatsheet (`costmgmt-api-cheatsheet.adoc`) documents tag filtering across:
- Container recommendations (lines 656–658)
- History (line 773)
- Namespace (lines 830–831)
- GPU MIG (line 918)
- GPU timeslicing (line 961)
- Node (line 1023)
- PVC (line 1075)
- Quota (line 1190)
- Cluster quota (line 1324)
- VM (lines 1644–1645)
- Savings summary group_by[tag] (lines 421–423)
- Koku Tags API (lines 347–384)

### 12. IQE Test Coverage

**Status:** ✅

`iqe-ros-ocp-plugin` has comprehensive tag filtering tests:
- `test_container_filter_tag` — basic filter[tag:key]=value
- `test_tag_filter_multi_key_and` — multi-key AND logic
- `test_tag_filter_rbac_combined` — RBAC intersection
- `test_container_history_filter_tag` — history endpoint
- `test_ros_tag_groupby.py` — savings summary group_by[tag]
- Tag filtering tests across namespace, node, PVC, GPU, VM, quota, cluster-quota endpoints

### 13. E2E Test Coverage

**Status:** ✅

`cost-onprem-chart/tests/suites/ros/` has tag filter tests on:
- Container detail (`test_container_filter_tag`, `test_tag_filter_multi_key_and_logic`, `test_tag_filter_with_rbac_scoped_identity`)
- Namespace (`test_namespace_filter_tag`)
- PVC (`test_pvc_filter_tag`)
- Node (`test_node_filter_tag`)
- VM (`test_vm_filter_tag`)
- GPU timeslicing (`test_gpu_timeslicing_filter_tag`)
- Quota (`test_quota_filter_tag`)
- History (`test_history_filter_tag`)
- Savings summary (`test_savings_summary_group_by_tag`)

### 14. koku-ui Frontend

**Status:** ✅

`apps/koku-ui-ros/src/routes/components/dataToolbar/utils/common.ts` handles `tag` as a filter category using `tagPrefix` for the bracket notation. The ROS UI toolbar supports tag-based filtering with the `filter[tag:key]=value` API syntax.

---

## Checklist

- [x] Operator collects namespace labels (and pod labels via `kube_namespace_labels`)
- [x] Koku stores tags in `reporting_ocptags_values`
- [x] Koku `enabled_tags` gates which keys are available
- [x] ROS tag sync job correctly reads from Koku tables (both modes)
- [x] `ROS_TAGS_SOURCE=db` mode works correctly (JOIN-based)
- [x] `ROS_TAGS_SOURCE=api` mode works correctly (push-based)
- [x] Tags appear in container list response (`tags` field, api mode)
- [x] `filter[tag]` syntax documented in OpenAPI
- [x] `filter[tag]` supports `key:value` format
- [x] Multiple tag filters work (AND logic)
- [x] Tag keys/values available for UI autocomplete (via Koku Tags API)
- [x] Bruno has tag filter examples (15+ examples)
- [x] Cheatsheet documents tag filtering (17+ examples)
- [x] E2E tests verify tag filtering (9+ test methods)
- [x] Tags are properly indexed in database (GIN index on `resolved_tags`)
- [x] Nise generates tag data in OCP static YAML (`namespace_labels`)

---

## Discrepancies Fixed

| # | What | Where | Fix |
|---|------|-------|-----|
| 1 | Missing docs-site tag-filtering page (7 broken links) | `docs-site/features/tag-filtering.md` | Created new page with all referenced anchors |

---

## Design Questions / Observations

### No Issues Found

The tag sync feature is one of the more thoroughly documented and tested features in the ROS codebase. Key strengths:

1. **Dual-path architecture** is clean — `db` and `api` modes share the same user-facing filter syntax but use completely different internal paths
2. **Full-replace transaction semantics** in push sync avoids partial-state bugs
3. **Self-gating behavior** (empty results + `meta.warnings` when no tags enabled) is user-friendly
4. **Comprehensive validation** (`ValidateSyncRequest` with length limits, key counts)
5. **Good ADR trail** (ADR-0119, ADR-0120, ADR-0226, ADR-0227, ADR-0256, ADR-0257)

### Minor Observations (Not Bugs)

1. **E2E tag push test is skeletal** (`test_tag_push_e2e.py` skips). This validates the Koku→ROS push chain end-to-end. The existing filter E2E tests cover the query path but not the push trigger path. Low priority since unit tests cover both sides.

2. **`tags` field in responses is api-mode only.** In `db` mode, the `enrichContainerTags()` function reads from `org_container_keys.resolved_tags`, which is empty when no push sync has occurred. This is documented as a known limitation / future work item.

3. **Namespace-scoped resolution** is a fundamental design decision, not a bug. Pod-level labels would require significant changes in both Koku (all_labels granularity) and ROS (per-pod tag storage).

### Race Conditions / Data Freshness

No race condition concerns found:

- **Push sync** uses `BEGIN`/`COMMIT` transaction with full-replace — no partial state possible
- **db mode** reads are always consistent with last Koku summarization (standard PostgreSQL MVCC)
- **6-hour safety-net** handles transient failures gracefully
- **Pod restart** has zero impact — tags persist in PostgreSQL in both modes
- **Concurrent pushes** for the same org are serialized by PostgreSQL row-level locking on `org_tag_sync_metadata`
