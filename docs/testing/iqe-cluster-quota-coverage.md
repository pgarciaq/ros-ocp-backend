# IQE coverage: ClusterResourceQuota recommendations

**Status:** Ready to implement when `iqe-ros-ocp-plugin` is on branch `pgarciaq-rosocp-superpowers-phase12` (current workspace branch is `pgarciaq-phase6-native-namespaces`).

Add cases under `iqe_ros_ocp_plugin/tests/` following patterns in existing ROS recommendation tests (identity header, skip when plugin disabled).

## Endpoints

| Area | Method and path |
|------|-----------------|
| List | `GET /api/cost-management/v1/recommendations/openshift/cluster-quota/` |
| Detail | `GET /api/cost-management/v1/recommendations/openshift/cluster-quota/detail` |
| Settings | `GET/PUT/DELETE /api/cost-management/v1/recommendations/openshift/settings/cluster-quota` |

## Test cases (ready-to-implement)

### `test_cluster_quota_list_default_shape`

- **Request:** `GET .../cluster-quota/?limit=20`
- **Assert:** status 200; `meta.count` is int; `data` is list; each row has `cluster_uuid`, `cluster_quota_name`, `recommendation_type` in `{tighten, raise, optimal, none}`, `risk_level` in `{high, medium, low, none}`; optional `namespaces` is a list of strings when present.

### `test_cluster_quota_list_filter_cluster`

- **Setup:** capture `cluster_uuid` from first list row (skip if empty).
- **Request:** `filter[cluster]={cluster_uuid}`
- **Assert:** every row has matching `cluster_uuid`.

### `test_cluster_quota_list_filter_cluster_quota_name`

- **Setup:** capture `cluster_quota_name` from first row.
- **Request:** `filter[cluster_quota_name]={name}` (also exercise alias `filter[crq]` in a subtest).
- **Assert:** every row has matching `cluster_quota_name`.

### `test_cluster_quota_list_filter_recommendation_type`

- **Request:** `filter[recommendation_type]=tighten`
- **Assert:** every row has `recommendation_type == "tighten"`.

### `test_cluster_quota_list_filter_risk_level`

- **Request:** `filter[risk_level]=high`
- **Assert:** every row has `risk_level == "high"`.

### `test_cluster_quota_list_filter_namespace`

- **Setup:** row with non-empty `namespaces` array; pick one namespace value.
- **Request:** `filter[namespace]={ns}` (subtest with `filter[project]` alias).
- **Assert:** returned rows include the namespace in `namespaces`; no row lists a disjoint membership set.

### `test_cluster_quota_list_pagination`

- **Request:** `limit=2&offset=0` then `limit=2&offset=2` when `meta.count > 2`.
- **Assert:** `(cluster_uuid, cluster_quota_name)` tuples differ across pages.

### `test_cluster_quota_list_order_by_cluster_quota_name`

- **Request:** `order_by=cluster_quota_name&order_how=asc`
- **Assert:** `cluster_quota_name` values are sorted ascending.

### `test_cluster_quota_list_order_by_utilization`

- **Request:** `order_by=utilization&order_how=desc`
- **Assert:** max of `utilization.*_percent` per row is non-increasing.

### `test_cluster_quota_list_plugin_disabled`

- **When:** deployment has `cluster-quota` in `ROS_DISABLED_PLUGINS`.
- **Assert:** status 404 with message referencing disabled plugin.

### `test_cluster_quota_detail_success_with_history`

- **Setup:** known `(cluster_uuid, cluster_quota_name)` from list.
- **Request:** `GET .../cluster-quota/detail?cluster_uuid=...&cluster_quota_name=...`
- **Assert:** status 200; matches list identity fields; `history` is a non-empty array; each entry has `recorded_at`, `resource`, `recommendation_type`, `risk_level`; numeric fields are int or null.

### `test_cluster_quota_detail_not_found`

- **Request:** unknown `cluster_quota_name`.
- **Assert:** status 404.

### `test_cluster_quota_detail_missing_params`

- **Request:** omit `cluster_quota_name`.
- **Assert:** status 400.

### `test_cluster_quota_settings_get_defaults`

- **Request:** `GET .../settings/cluster-quota`
- **Assert:** status 200; `headroom_percent`, `high_risk_threshold_percent`, `medium_risk_threshold_percent` present; `locked_fields` is list.

### `test_cluster_quota_settings_put_and_delete`

- **Request:** PUT valid thresholds, GET confirms, DELETE resets (skip when `settings_locked` true).
- **Assert:** persisted values match payload; DELETE restores defaults.

## Prerequisites

- `cluster-quota` in `ROS_ENABLED_PLUGINS` (not disabled).
- Ingested `ros-openshift-cluster-quota-*.csv` with operator column set (storage, pods, namespaces) and populated `cluster_quota_recommendation_sets` / `cluster_quota_recommendation_history`.

## Related automated coverage

- E2E list filters, `order_by`, detail `history[]`: `cost-onprem-chart/tests/suites/ros/test_cluster_quota_recommendations.py`
- Extended upload flow: `cost-onprem-chart/tests/suites/e2e/test_cluster_quota_recommendations_flow.py`
- Bruno examples: `costmgmt-api-cheatsheet/bruno/Optimizations/Cluster quota recommendations*.bru`
