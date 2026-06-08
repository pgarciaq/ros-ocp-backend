# IQE requirement registration for ROS recommendation tests

**Last updated:** 2026-06-02

IQE tests in `iqe-cost-management-plugin` tag each testcase with a `requirements:` marker in the docstring. The plugin declares those requirement IDs (with summary and priority) in `iqe_cost_management/conf/requirements.yaml`. CI jobs that pass `--requirements=<id>` only collect tests whose markers intersect the processed requirement set (see `iqe-core` `iqe/fixtures/requirements.py`).

This document lists ROS-related requirements, what is registered in the plugin, and what still needs an **app-interface** merge request for requirement-filtered CI runs on stage/prod.

## ROS requirement inventory

| Requirement ID | Test file(s) | Registered in `requirements.yaml` | Typical recommendation scope |
|----------------|--------------|-------------------------------------|------------------------------|
| `cost_ros_ocp` | `test_ros.py`, `test_ros_container_detail.py`, `test_ros_namespace_recommendations.py`, `test_ros_threshold_settings.py`, `test_ros_*` (containers, namespaces, settings, tags, idle, history, quality, savings, fleet, business hours, term) | Yes | Base ROS-OCP ingest + container/namespace recommendations, settings, cross-cutting APIs |
| `cost_ros_ocp_vm` | `test_ros_vm_recommendations.py` | Yes | OpenShift Virtualization VM recommendations |
| `cost_ros_ocp_nodes` | `test_ros_node_recommendations.py`, `test_ros_node_utilization.py` | Yes (added 2026-06-02) | Node right-sizing + utilization |
| `cost_ros_ocp_gpu` | `test_ros_gpu_recommendations.py` | Yes (added 2026-06-02) | GPU recommendations |
| `cost_ros_ocp_pvc` | `test_pvc_recommendations.py` | Yes (added 2026-06-02) | PVC / storage recommendations |
| `cost_ros_ocp_snapshot` | `iqe_ros_ocp/tests/rest/test_snapshot_recommendations.py` | Yes (added 2026-06-02) | Snapshot inventory / staleness |
| `cost_ros_ocp_cluster_quota` | `test_ros_cluster_quota_recommendations.py` | Yes (added 2026-06-02) | ClusterResourceQuota recommendations |
| `cost_ros_ocp_quota` | `test_ros_quota_recommendations.py` | Yes (added 2026-06-02) | Namespace ResourceQuota recommendations |

### Containers vs namespaces vs other types

- **Containers and generic namespace recommendations** use the broad `cost_ros_ocp` requirement (shared with many ROS API tests).
- **Specialized recommendation plugins** (nodes, PVCs, GPUs, VMs, snapshots, cluster-quota, namespace quota) each have a **dedicated** requirement ID so CI can run targeted subsets once app-interface enables them.

### Container recommendations — IQE ownership split

Container list/detail coverage is intentionally split across two IQE plugins to avoid
duplicate maintenance and matrix drift:

| Plugin | Owns |
|--------|------|
| **iqe-cost-management-plugin** | Detail contract, dual-engine (cost + performance), tag filtering, history, quality, idle detection, business hours |
| **iqe-ros-ocp-plugin** | List filters (cluster, project, workload, container, workload_type), sorting, keyset pagination, settings (thresholds, business hours), CSV export, notification codes catalog |

When adding a new container API behavior, place the IQE test in the owning plugin above
(and tag with `cost_ros_ocp` in IQE-CM where applicable). Do not mirror the same
assertion in both plugins unless the behavior spans both surfaces (e.g. a list field
that must also appear on detail).

## Plugin-side registration (done in this repo)

Add or update entries in:

`iqe-cost-management-plugin/iqe_cost_management/conf/requirements.yaml`

Example entry shape:

```yaml
cost_ros_ocp_quota:
  summary: ROS-OCP namespace ResourceQuota recommendations API
  priority: high
```

After merging plugin changes, release a new `iqe-cost-management-plugin` image/version consumed by CI.

## app-interface registration (still required for filtered CI)

Requirement-filtered IQE jobs (e.g. `--requirements=cost_ros_ocp_quota`) also need the requirement IDs registered in **app-interface** so the CI template knows which requirements exist and can map them to job parameters / feature toggles.

**Typical MR steps:**

1. Open a merge request in [app-interface](https://gitlab.cee.redhat.com/service/app-interface) adding the new requirement IDs to the cost-management IQE job configuration (same pattern as existing `cost_ros_ocp` / `cost_ros_ocp_vm` entries).
2. Get AppSRE review and merge to stage; validate a requirement-filtered job, for example:
   ```bash
   iqe tests plugin cost_management \
     --requirements=cost_ros_ocp_quota \
     --requirements-priority=high
   ```
3. Promote to prod after stage validation.

Until app-interface lists a requirement ID, **tests still run** in unfiltered profiles (`smoke`, `extended`, etc.) because IQE matches markers directly when `--requirements` is passed manually. Missing app-interface entries mainly block **automated requirement-scoped** pipelines (nightly quota-only, PR gates on a single feature, etc.).

## Does the same problem affect other recommendation types?

**Yes.** Before 2026-06-02, only `cost_ros_ocp` and `cost_ros_ocp_vm` were in `requirements.yaml`, while tests already used six additional markers:

- `cost_ros_ocp_nodes`
- `cost_ros_ocp_gpu`
- `cost_ros_ocp_pvc`
- `cost_ros_ocp_snapshot`
- `cost_ros_ocp_cluster_quota`
- `cost_ros_ocp_quota`

Those tests were not broken in full-profile runs (markers are optional filters), but:

1. `--requirements=<id>` with priority filtering could not select them reliably from plugin metadata.
2. app-interface could not schedule requirement-scoped jobs for those features.

All eight ROS requirement IDs are now declared in the plugin. **app-interface MRs are still needed** for each new ID if you want isolated CI jobs (same as quota).

## Related docs

- On-prem IQE setup: `cost-onprem-chart/docs/development/iqe-testing-setup.md`
- Cluster quota IQE case catalog: [iqe-cluster-quota-coverage.md](../testing/iqe-requirements-registration.md)
- Skipped IQE groups (on-prem): `cost-onprem-chart/docs/development/skipped-iqe-tests.md`
