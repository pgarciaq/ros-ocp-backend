# IQE coverage: ClusterResourceQuota recommendations

The `iqe-ros-ocp-plugin` repo does not yet include REST tests for the native
`cluster-quota` plugin. Add cases on branch `pgarciaq-rosocp-superpowers-phase12`
(or mirror in `iqe-cost-management-plugin` under `test_ros_quota_recommendations.py`).

## Endpoints to cover

| Test area | Method and path |
|-----------|-----------------|
| List | `GET /api/cost-management/v1/recommendations/openshift/cluster-quota/` |
| Detail | `GET /api/cost-management/v1/recommendations/openshift/cluster-quota/detail` |
| Settings | `GET/PUT/DELETE .../settings/cluster-quota` |

## List scenarios

- Default list returns 200 and expected row shape (`cluster_quota_name`, `recommendation_type`, `risk_level`).
- `filter[cluster]`, `filter[cluster_quota_name]`, `filter[recommendation_type]`, `filter[risk_level]`.
- `filter[namespace]` / `filter[project]` when CRQ rows include `namespaces` membership.
- Pagination (`limit`, `offset`).
- `order_by=cluster_quota_name`, `order_by=utilization`, `order_by=risk_level`, `order_by=estimated_monthly_savings` with `order_how=asc|desc`.
- 404 when `cluster-quota` plugin disabled (`ROS_DISABLED_PLUGINS`).

## Detail scenarios

- 200 with `history[]` array for a known `(cluster_uuid, cluster_quota_name)`.
- 404 for unknown CRQ name.
- 400 when required query params missing.

## Prerequisites

- Deployment with `cluster-quota` in `ROS_ENABLED_PLUGINS`.
- Ingested `ros-openshift-cluster-quota-*.csv` and populated `cluster_quota_recommendation_sets`.

E2E coverage for list `order_by` and detail lives in
`cost-onprem-chart/tests/suites/ros/test_cluster_quota_recommendations.py`.
