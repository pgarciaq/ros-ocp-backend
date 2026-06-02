# IQE coverage: ClusterResourceQuota recommendations

**Status:** Implemented in `iqe-cost-management-plugin` (canonical) and mirrored in `iqe-ros-ocp-plugin`.

Primary test module:

- `iqe-cost-management-plugin/iqe_cost_management/tests/rest_api/v1/test_ros_cluster_quota_recommendations.py`

Requirement ID: `cost_ros_ocp_cluster_quota` (declared in both plugins' `requirements.yaml`).

## Endpoints

| Area | Method and path |
|------|-----------------|
| List | `GET /api/cost-management/v1/recommendations/openshift/cluster-quota/` |
| Detail | `GET /api/cost-management/v1/recommendations/openshift/cluster-quota/detail` |
| Settings | `GET/PUT/DELETE /api/cost-management/v1/recommendations/openshift/settings/cluster-quota` |

## Implemented test cases

| Test | Coverage |
|------|----------|
| `test_cluster_quota_list_returns_200` | List envelope (`meta`, `data`, `links`) |
| `test_cluster_quota_list_has_expected_fields` | Row shape, quota blocks, type/risk enums |
| `test_cluster_quota_filter_by_cluster` | `filter[cluster]` |
| `test_cluster_quota_filter_by_cluster_quota_name` | `filter[cluster_quota_name]` |
| `test_cluster_quota_filter_crq_alias_matches_cluster_quota_name` | `filter[crq]` alias |
| `test_cluster_quota_filter_by_namespace` | `filter[namespace]` membership |
| `test_cluster_quota_filter_project_alias_matches_namespace` | `filter[project]` alias |
| `test_cluster_quota_filter_recommendation_type_and_risk_level` | Type and risk filters |
| `test_cluster_quota_order_by_utilization_desc` | `order_by=utilization` |
| `test_cluster_quota_pagination` | `limit` / `offset` |
| `test_cluster_quota_detail_returns_history` | Detail + `history[]` |
| `test_cluster_quota_detail_returns_history_and_notifications` | Notification codes (70–73) |
| `test_cluster_quota_group_by_cluster` | `group_by[cluster]` aggregation |
| `test_cluster_quota_settings_get_returns_200` | Settings GET |
| `test_cluster_quota_settings_has_expected_fields` | Settings defaults + `locked_fields` |
| `test_cluster_quota_settings_put_and_delete` | Settings PUT/DELETE round-trip |
| `test_cluster_quota_list_empty_for_unknown_cluster` | Empty filter result |

Field assertions include `storage_request_bytes`, `pods`, `capacity_freed`, and notification codes when present in live data.

## Prerequisites

- `cluster-quota` in `ROS_ENABLED_PLUGINS` (not disabled).
- Ingested `ros-openshift-cluster-quota-*.csv` with operator column set (storage, pods, namespaces) and populated `cluster_quota_recommendation_sets` / `cluster_quota_recommendation_history`.

## Related automated coverage

- E2E list filters, `order_by`, detail `history[]`: `cost-onprem-chart/tests/suites/ros/test_cluster_quota_recommendations.py`
- Extended upload flow: `cost-onprem-chart/tests/suites/e2e/test_cluster_quota_recommendations_flow.py`
- Bruno examples: `costmgmt-api-cheatsheet/bruno/Optimizations/Cluster quota recommendations*.bru`
- Unit/integration (Go): `ros-ocp-backend/internal/api/handlers_cluster_quota_recs_test.go`

## app-interface registration

See [iqe-requirements-registration.md](./iqe-requirements-registration.md) — register `cost_ros_ocp_cluster_quota` alongside `cost_ros_ocp_quota` for requirement-filtered CI jobs.
