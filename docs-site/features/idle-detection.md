# Idle and zombie workload detection

!!! info "Quick Facts"
    **States:** `active`, `idle`, `zombie` on containers, GPUs, namespaces, and nodes  
    **List filter:** `filter[idle_state]=zombie,idle`  
    **Settings:** `GET/PUT/DELETE .../settings/idle-detection`  
    **Waste:** `estimated_monthly_waste` on non-active container rows; fleet rollup via `group_by[idle_state]`

Classify underutilized OpenShift workloads so operators can find reclaimable spend and terminate guidance without chasing false positives from bursty or platform workloads.

Full design (classification math, DB schema, rollout): [internal idle-detection design](../../docs/features/idle-detection.md).

## What the states mean

| State | Meaning |
|-------|---------|
| **active** | Utilization relative to requests (or burst activity) is above idle thresholds. Rightsizing savings apply. |
| **idle** | Sustained low CPU and memory utilization vs configured percentages over the observation window. |
| **zombie** | Near-zero CPU: P95 and peak below zombie millicore thresholds (defaults 1 mc / 10 mc). Strongest terminate signal. |

Zombie is a subset of non-active: a namespace is only **zombie** when every container and GPU in it is zombie; a mix of idle and zombie containers rolls up to namespace **idle**.

## Viewing idle workloads

Container list (comma-separated values are ORed):

```bash
curl -s -H "x-rh-identity: $IDENTITY" \
  'https://<ros-api>/api/cost-management/v1/recommendations/openshift?filter[idle_state]=zombie,idle'
```

Namespace list (canonical plural path; `/namespace` alias also works):

```bash
curl -s -H "x-rh-identity: $IDENTITY" \
  'https://<ros-api>/api/cost-management/v1/recommendations/openshift/namespaces?filter[idle_state]=idle,zombie'
```

Sort idle workloads:

```bash
curl -s -H "x-rh-identity: $IDENTITY" \
  'https://<ros-api>/api/cost-management/v1/recommendations/openshift?order_by=idle_duration_days&order_how=desc'
```

Node list uses the same `filter[idle_state]` on `GET .../recommendations/openshift/nodes`.

GPU workloads on container rows: `filter[has_gpu]=true` and `filter[gpu_idle_state]=idle,zombie`. MIG recommendations support `filter[gpu_idle_state]` on the MIG endpoint.

Non-active container rows include `idle_since`, `idle_duration_days`, `peak_cpu_millicores`, `peak_memory_bytes`, `estimated_monthly_waste`, and `idle_recommendation` (`action`, `confidence`, `reason`). Active rows omit waste and terminate fields; rightsizing `estimated_monthly_savings` is cleared when `idle_state` is not `active`.

## Waste estimation

For **idle** and **zombie** containers (and idle GPUs), waste reflects the **full monthly cost** of retained resources—not the rightsizing delta. Fleet rollup:

```bash
curl -s -H "x-rh-identity: $IDENTITY" \
  'https://<ros-api>/api/cost-management/v1/recommendations/openshift/savings-summary?group_by[idle_state]=*'
```

CSV export (`?format=csv`) adds `idle_state`, `idle_since`, `idle_duration_days`, `estimated_monthly_waste`, and currency columns.

## Configuring thresholds

Per-organization settings (tenant DB overrides with environment locks):

| Method | Path |
|--------|------|
| GET | `/api/cost-management/v1/recommendations/openshift/settings/idle-detection` |
| PUT | same (partial `idle_detection` body) |
| DELETE | same (reset tenant overrides) |

Example PUT:

```bash
curl -s -X PUT -H "x-rh-identity: $IDENTITY" -H "Content-Type: application/json" \
  -d '{
    "idle_detection": {
      "enabled": true,
      "thresholds": {
        "cpu_utilization_percent": 2,
        "memory_utilization_percent": 5,
        "burst_ratio": 10,
        "minimum_observation_days": 14,
        "gpu_sm_active_basis_points": 500,
        "gpu_dram_active_basis_points": 500,
        "zombie_cpu_millicores": 1,
        "zombie_peak_millicores": 10
      },
      "exclusions": {
        "namespaces": ["kube-system", "openshift-*"],
        "workload_types": ["DaemonSet"]
      }
    }
  }' \
  "https://<ros-api>/api/cost-management/v1/recommendations/openshift/settings/idle-detection"
```

PUT and DELETE enqueue async recalculation for **container**, **gpu**, **namespace**, **node**, and **pvc** plugins.

Key thresholds: utilization percents, `burst_ratio` (protects CronJobs), `minimum_observation_days`, GPU basis points, and zombie millicore guards. Fields listed in `locked_fields` cannot be changed via API when set by `ROS_IDLE_*` deployment env vars.

## Node, GPU, and PVC support

- **Nodes** — idle/zombie from node digests and pod counts; filter with `filter[idle_state]` on the node utilization API. Notification code **15** (`NODE_IDLE`).
- **GPUs** — DCGM SM/DRAM basis points on container and MIG rows; `gpu_idle_state` on list/detail `gpu` blocks. Filter with `filter[gpu_idle_state]=idle,zombie`. Notification code **26** (`GPU_IDLE`).
- **PVC orphans** — orphaned PVC recommendations (`recommendation_type=orphaned`) include `idle_since` and `idle_duration_days` tracking when zero usage was first detected.

## Notification codes

| Code | Name | When |
|------|------|------|
| 5 | IDLE_WORKLOAD | Container idle or zombie (`idle_state`) |
| 8 | ABANDONED_WORKLOAD | Legacy all-zero usage when inline classification did not run |
| 15 | NODE_IDLE | Node idle/zombie |
| 26 | GPU_IDLE | GPU idle |

When full idle classification applies, `idle_state` drives code **5**; code **8** is not set for zombies (classification is authoritative). Legacy paths remain for workloads with insufficient observation data.

## Related

- [Configurable thresholds](configurable-thresholds.md)
- [Savings estimations](savings-estimations.md)
- [Container right-sizing](container-recommendations.md)
- [Plugin reference: idle detection](../plugin-reference/idle-detection.md)
