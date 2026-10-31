# IQE requirement registration for ROS recommendation tests

> **Last verified:** 2026-11-01

**Last updated:** 2026-11-01

IQE tests tag each testcase with a `requirements:` marker in its docstring
metadata. The plugin declares those requirement IDs (with summary and
priority) in `iqe_ros_ocp/conf/requirements.yaml`; `iqe-core`'s
`load_requirements()` reads that file, so `--requirements=<id>` and
`--requirements-priority=<p>` only select tests whose markers resolve
against it. **An ID used in a test but absent from the declarations is
silently unselectable** — the job runs and selects nothing, which reads
like a pass. A CI guard against that defect class is tracked in
[#737](https://github.com/pgarciaq/ros-ocp-backend/issues/737).

## The marker convention (#736, owner decision 2026-11-01)

`cost_ros_ocp` is the **default** — container recommendations, list
mechanics (filters/sorting/search/pagination), engine selection,
settings, and cross-cutting API behavior. A specific ID is minted **only
when a group is a distinct capability QE would target by name** (a
recommendation surface, or a reportable feature like the OOM timeline).
The legacy umbrella IDs (`ROS-OCP`, `ROS-OCP-API`) and the phase-era
`ROS-OCP-Phase4` marker were retired from the REST tests in the same
change; the UI tests keep `ROS-OCP-UI`/`ROS-OCP-E2E` (declared, and a
different surface).

## ROS requirement inventory

Derived from the plugin tree (the derivation: for every
`cost_ros_ocp*` token in `iqe_ros_ocp/tests/**`, the files carrying it,
cross-checked against the declarations — every used ID is declared):

| Requirement ID | Priority | Files | Scope |
|---|---|---|---|
| `cost_ros_ocp` | medium | `rest/test_apispec`, `rest/test_boxplots`, `rest/test_business_hours`, `rest/test_container_recommendations`, `rest/test_endpoints`, `rest/test_filters`, `rest/test_fleet_summary`, `rest/test_idle_detection`, `rest/test_native_engine`, `rest/test_negative`, `rest/test_new_container_endpoint`, `rest/test_notifications`, `rest/test_offset_limit`, `rest/test_oom`, `rest/test_quality`, `rest/test_recommendation_history`, `rest/test_ros_tag_groupby`, `rest/test_savings_summary`, `rest/test_savings_summary_engine`, `rest/test_searching`, `rest/test_settings_dual_percentiles`, `rest/test_sorting`, `rest/test_term_settings`, `rest/test_threshold_settings` | ROS-OCP general API tests |
| `cost_ros_ocp_capabilities` | medium | `rest/test_capabilities` | ROS-OCP capabilities endpoint |
| `cost_ros_ocp_cluster_quota` | high | `rest/test_cluster_quota_recommendations` | ROS-OCP ClusterResourceQuota recommendations API |
| `cost_ros_ocp_fleet_heatmap` | medium | `rest/test_fleet_heatmap` | ROS-OCP fleet heatmap |
| `cost_ros_ocp_gpu` | medium | `rest/test_gpu_recommendations` | ROS-OCP GPU recommendations API |
| `cost_ros_ocp_gpu_summary` | medium | `rest/test_gpu_summary` | ROS-OCP GPU summary |
| `cost_ros_ocp_hcp` | medium | `rest/test_hcp_advisories`, `rest/test_hcp_correlation_settings`, `rest/test_hcp_fleet_capacity`, `rest/test_hcp_recommendations` | ROS-OCP hosted control-plane (HCP) API |
| `cost_ros_ocp_machinesets` | medium | `rest/test_machinesets` | ROS-OCP MachineSet recommendations API |
| `cost_ros_ocp_namespace` | medium | `rest/test_namespace_boxplots`, `rest/test_namespace_engine_filter`, `rest/test_namespace_filter`, `rest/test_namespace_notifications`, `rest/test_namespace_ordering`, `rest/test_namespace_recommendation_detail`, `rest/test_namespace_recommendation_history`, `rest/test_namespace_recommendation_list`, `rest/test_namespace_recommendation_rbac`, `rest/test_namespace_recommendations`, `rest/test_namespace_units` | ROS-OCP namespace recommendations API |
| `cost_ros_ocp_node_hourly` | medium | `rest/test_node_hourly_utilization` | ROS-OCP node hourly recommendations |
| `cost_ros_ocp_nodes` | medium | `rest/test_node_engine_filter`, `rest/test_node_list_smoke`, `rest/test_node_recommendations`, `rest/test_node_utilization` | ROS-OCP node recommendations API |
| `cost_ros_ocp_oom_timeline` | medium | `rest/test_oom_timeline` | ROS-OCP OOM timeline |
| `cost_ros_ocp_pvc` | medium | `rest/test_pvc_recommendations`, `rest/test_pvc_storage_groupby` | ROS-OCP PVC recommendations API |
| `cost_ros_ocp_quota` | high | `rest/test_quota_recommendations` | ROS-OCP namespace ResourceQuota recommendations API |
| `cost_ros_ocp_savings_recalculate` | medium | `rest/test_savings_recalculate` | ROS-OCP savings recalculation |
| `cost_ros_ocp_snapshot` | medium | `rest/test_snapshot_recommendations` | ROS-OCP snapshot recommendations API |
| `cost_ros_ocp_snapshot_age_distribution` | medium | `rest/test_snapshot_age_distribution` | ROS-OCP snapshot age distribution |
| `cost_ros_ocp_snapshot_cost_by_type` | medium | `rest/test_snapshot_cost_by_type` | ROS-OCP snapshot cost by type |
| `cost_ros_ocp_tags` | medium | `rest/test_tags_status_sync` | ROS-OCP tag filtering |
| `cost_ros_ocp_vm` | medium | `rest/test_vm_engine_filter`, `rest/test_vm_recommendations` | ROS-OCP VM recommendations API |
| `cost_ros_ocp_vm_hourly` | medium | `rest/test_vm_hourly_activity` | ROS-OCP VM hourly recommendations |
| `cost_ros_ocp_vm_instance_types` | medium | `rest/test_vm_instance_types` | ROS-OCP VM instance-type recommendations |
| `cost_ros_ocp_workload_types` | medium | `rest/test_workload_types` | ROS-OCP workload types |

## Plugin-side registration

Add or update entries in `iqe_ros_ocp/conf/requirements.yaml`:

```yaml
cost_ros_ocp_hcp:
  summary: ROS-OCP hosted control-plane (HCP) API
  priority: medium
  description: HCP capacity recommendations (list/detail/scope), fleet-capacity, advisories, and correlator settings
```

When adding a new test file: give every test function a docstring with a
`metadata:` block carrying a `requirements:` marker (the default is
`cost_ros_ocp`; mint a specific ID only for a targetable capability),
declare the ID, and keep the inventory table above in step — the CI
guard (#737) fails the merge if a used marker is undeclared.

## app-interface registration (still required for filtered CI)

Requirement-filtered CI runs on stage/prod resolve IDs through
app-interface's requirement definitions. The declarations in the plugin
make the IDs selectable locally and in fork CI; the app-interface merge
requests for the new IDs (`cost_ros_ocp_hcp`, `cost_ros_ocp_namespace`,
and the feature IDs) remain open follow-ups — same as the 2026-06-02
wave this page originally tracked.
