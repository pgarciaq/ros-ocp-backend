# Test Data Recipes

How to generate targeted test data for each native engine plugin using NISE fixtures.

## Prerequisites

- **NISE** installed (`pip install koku-nise` or from source at `~/dev/koku/nise/`)
- Fixtures are bundled with the nise package under `examples/ros_ocp_e2e/`
- For auto-seeding (E2E test baseline), see `examples/ros_ocp_seeding/`
- A registered OCP source/provider with a matching `cluster_id`

## Quick Reference

### E2E Feature Templates

| Plugin / Feature | Fixture | Key data generated |
|---|---|---|
| **Container** | | |
| Container (basic) | `ocp_report_ros_0.yml` | 2 nodes, 6 namespaces, varied CPU/memory patterns incl. idle and OOM |
| Container (advanced) | `ocp_report_advanced.yml` | 3 clusters, 9 namespaces, PVCs, multi-node projects |
| Container (advanced daily) | `ocp_report_advanced_daily.yml` | Same as advanced with no explicit start/end dates |
| Business hours | `ocp_report_business_hours.yml` | 3 workers, diurnal vs batch vs overnight usage patterns, PVCs |
| **GPU** | | |
| GPU MIG (Koku cost) | `ocp_report_gpu_mig.yml` | H100 node, MIG profiles (1g.18gb, 3g.40gb), 2 namespaces |
| GPU MIG (ROS recs) | `ocp_report_gpu_mig_ros.yml` | H100 with DCGM metrics, underutilized + idle pods for MIG profile recs |
| GPU time-slicing | `ocp_report_gpu_timeslicing.yml` | T4 node, 3 pods with DCGM profiling overrides (sm/tensor/dram/fb) |
| GPU combined | `ocp_report_gpu_combined.yml` | T4 time-slicing + H100 MIG + A100 model diversity, 3 GPU node types |
| GPU + VM time-slicing | `ocp_report_vm_gpu_timeslicing.yml` | VMs with vGPU time-slicing (notifications 56–57) |
| **Virtual Machine** | | |
| VM basic | `ocp_report_vm.yml` | 8 VMs: active/idle/abandoned, Linux/Windows, guest agent on/off |
| VM enhancements (46–49) | `ocp_report_vm_enhancements.yml` | Windows kernel compare, downsize unstable, notification 46–49 scenarios |
| VM enhancements (64–69) | `ocp_report_vm_enhancements_64_69.yml` | Power-off schedule, storage cold, extended notification codes |
| VM + GPU | `ocp_report_vm_gpu.yml` | VMs with DCGM GPU columns (31 fields), GPU vs non-GPU baseline |
| VM I/O profiling | `ocp_report_vm_io_profiling.yml` | Sequential vs random I/O patterns (notifications 58–59) |
| VM network | `ocp_report_vm_network.yml` | Network-saturated workload (notification 55) |
| VM notifications (37–42) | `ocp_report_vm_notifications.yml` | Disk grow, guest agent, notification matrix codes 37–42 |
| VM placement | `ocp_report_vm_placement.yml` | Uneven VM distribution across workers, NUMA oversized (notifications 60–63) |
| VM MVP promotions | `ocp_report_vm_mvp_promotions.yml` | Adaptive margin, multi-GPU, variable CPU pattern VMs |
| **Storage** | | |
| PVC rightsizing | `ocp_report_pvc_rightsizing.yml` | 5 PVC scenarios: oversized, near-full, orphaned, healthy, VM-owned |
| Snapshot classification | `ocp_report_snapshot_classification.yml` | 4 snapshots: stale, orphaned, never-restored, active |
| **Quotas** | | |
| Namespace quota | `ocp_report_quota.yml` | High pod requests, explicit resource_quota used values |
| Cluster quota | `ocp_report_cluster_quota.yml` | 2 CRQs with high used/hard ratios, tighten/raise paths |
| **Nodes** | | |
| Node idle consolidation | `ocp_report_node_idle_consolidation.yml` | 6 nodes: zombie, idle, 4 lightly-loaded m5 workers for consolidation |
| **Forecasting** | | |
| Constant forecast | `ocp_report_forecast_const.yml` | Single pod with constant CPU/memory usage pattern |
| Outlier forecast | `ocp_report_forecast_outlier.yml` | Pod with varying CPU usage including outlier periods |
| **AI/ML** | | |
| AI workloads | `ocp_ai_workloads_template.yml` | Multi-GPU models (A30, V100, H100, T4, A10, L40S), Jinja2 date placeholders |
| **Cost Distribution** | | |
| Distribution scenarios | `ocp_report_distro.yml` | 3 nodes, GPU-attached + non-AI namespaces for cost distribution testing |
| **Edge Cases** | | |
| Missing items | `ocp_report_missing_items.yml` | Large and near-zero resource pods for missing data handling |
| Random CPU (EAP) | `ocp_random_cpu_for_eap_report.yml` | Random CPU patterns for EAP report testing |

### Template / Parameterized Fixtures

These fixtures use Jinja2 placeholders (`{{ variable }}`) and are rendered by the
E2E test framework before use:

| Fixture | Purpose |
|---|---|
| `ocp_report_0_template.yml` | Basic multi-cluster template with `{{ echo_orig_end }}` date placeholder |
| `ocp_report_daily_flow_template.yml` | Daily ingestion flow with `{{ start_date_1 }}` / `{{ end_date_1 }}` placeholders |
| `ocp_ai_workloads_template.yml` | AI workloads with per-GPU-model date placeholders |

### Today-Dated Fixtures

These fixtures use `start_date: today` and are designed for smoke tests
that verify same-day data processing:

| Fixture | Purpose |
|---|---|
| `today_ocp_report_0.yml` | Basic smoke test (single node, 1 namespace) |
| `today_ocp_report_1.yml` | Extended smoke test |
| `today_ocp_report_2.yml` | Additional smoke test variant |
| `today_ocp_report_fractional_vm_template.yml` | Fractional VM test data |
| `today_ocp_report_multiple_nodes.yml` | Multi-node smoke test |
| `today_ocp_report_multiple_nodes_projects.yml` | Multi-node, multi-project smoke test |
| `today_ocp_report_node.yml` | Node-focused smoke test |
| `today_ocp_report_tiers_0.yml` | Tier-based smoke test |
| `today_ocp_report_tiers_1.yml` | Tier-based smoke test variant |

## Usage Pattern

### Locating fixtures

```bash
# If NISE is installed via pip
python3 -c "from importlib.resources import files; print(files('nise') / 'examples' / 'ros_ocp_e2e')"

# Or use a nise source checkout directly
FIXTURE_DIR=~/dev/koku/nise/examples/ros_ocp_e2e
```

### Generating data for a specific plugin

```bash
FIXTURE_DIR=~/dev/koku/nise/examples/ros_ocp_e2e
CLUSTER_ID=550e8400-e29b-41d4-a716-446655440001

nise report ocp \
  --static-report-file $FIXTURE_DIR/ocp_report_gpu_mig_ros.yml \
  --ocp-cluster-id $CLUSTER_ID \
  --ros-ocp-info \
  --write-monthly \
  -w /tmp/nise-output
```

### What each flag does

| Flag | Required | Purpose |
|---|---|---|
| `--ros-ocp-info` | **Yes** | Generates container-level columns needed by the ROS processor |
| `--write-monthly` | **Yes** | Organizes output by month (required for manifest structure) |
| `--static-report-file` | **Yes** | Uses the fixture YAML to define workload shapes |
| `--ocp-cluster-id` | **Yes** | Must match a registered source/provider |
| `-w` / `--write-dir` | Recommended | Output directory (default: current directory) |

### Ingesting into local dev environment

After generating data, follow the ingestion steps in the
[Quick Start Tutorial](../quickstart.md#6-ingest-data) — either Path A
(Kafka publish) or Path B (ingress upload).

For the full ingestion flow including manifest creation and CSV serving via nginx,
see the Quick Start step 6.

## Recipes by Plugin

### Container Recommendations

Generate basic container sizing data (CPU/memory requests, limits, usage):

```bash
nise report ocp \
  --static-report-file $FIXTURE_DIR/ocp_report_ros_0.yml \
  --ocp-cluster-id $CLUSTER_ID \
  --ros-ocp-info \
  --write-monthly \
  -w /tmp/nise-container

# Verify in the API
curl -s -H "x-rh-identity: $IDENTITY" \
  'http://localhost:8000/api/cost-management/v1/recommendations/openshift?limit=5' \
  | python3 -m json.tool
```

**What to look for:** `recommendations.recommendation_terms.short_term.recommendation_engines.cost`
should contain CPU and memory request/limit values.

For business hours recommendations (diurnal patterns), use `ocp_report_business_hours.yml` instead.
This generates 3 namespaces with distinct workload patterns (daytime, batch, nighttime) that
exercise business-hours vs all-hours digest comparison.

### GPU Recommendations

#### MIG Profile Recommendations

```bash
nise report ocp \
  --static-report-file $FIXTURE_DIR/ocp_report_gpu_mig_ros.yml \
  --ocp-cluster-id $CLUSTER_ID \
  --ros-ocp-info \
  --write-monthly \
  -w /tmp/nise-gpu-mig

# Verify MIG recommendations
curl -s -H "x-rh-identity: $IDENTITY" \
  'http://localhost:8000/api/cost-management/v1/recommendations/openshift/gpu/mig?limit=5' \
  | python3 -m json.tool
```

**What to look for:** Pods with low DCGM metrics (sm_active, tensor_pipe_active) on full H100 GPUs.
The engine recommends a smaller MIG profile (e.g., `1g.18gb` or `3g.40gb`) based on actual utilization.

**Fixture variants:**

- `ocp_report_gpu_mig.yml` — MIG profiles for Koku GPU cost reports (no DCGM profiling)
- `ocp_report_gpu_mig_ros.yml` — MIG with DCGM metrics for ROS MIG profile recommendations
- `ocp_report_gpu_combined.yml` — All GPU scenarios: T4 time-slicing + H100 MIG + A100

#### Time-Slicing Recommendations

```bash
nise report ocp \
  --static-report-file $FIXTURE_DIR/ocp_report_gpu_timeslicing.yml \
  --ocp-cluster-id $CLUSTER_ID \
  --ros-ocp-info \
  --write-monthly \
  -w /tmp/nise-gpu-ts
```

**What to look for:** T4 GPUs with low SM/tensor/DRAM active percentages. The engine
recommends time-slicing configuration to share GPU resources across pods.

### Virtual Machine Recommendations

```bash
nise report ocp \
  --static-report-file $FIXTURE_DIR/ocp_report_vm.yml \
  --ocp-cluster-id $CLUSTER_ID \
  --ros-ocp-info \
  --write-monthly \
  -w /tmp/nise-vm

# Verify VM recommendations
curl -s -H "x-rh-identity: $IDENTITY" \
  'http://localhost:8000/api/cost-management/v1/recommendations/openshift/virtual-machines?limit=5' \
  | python3 -m json.tool
```

**What to look for:** VM sizing recommendations per guest OS (Linux vs Windows), idle/abandoned
detection, guest agent presence affecting recommendation quality.

**Fixture variants by scenario:**

| Scenario | Fixture | Notifications tested |
|---|---|---|
| Basic VM sizing | `ocp_report_vm.yml` | Idle, abandoned, downsize unstable |
| Enhancement codes 46–49 | `ocp_report_vm_enhancements.yml` | Windows kernel, catalog, settings |
| Enhancement codes 64–69 | `ocp_report_vm_enhancements_64_69.yml` | Power-off schedule, storage cold |
| Disk I/O patterns | `ocp_report_vm_io_profiling.yml` | Sequential vs random I/O (58–59) |
| Network saturation | `ocp_report_vm_network.yml` | Network-optimized recs (55) |
| Placement / NUMA | `ocp_report_vm_placement.yml` | Uneven distribution (60–63) |
| Notification matrix | `ocp_report_vm_notifications.yml` | Disk grow, guest agent (37–42) |
| VM + GPU | `ocp_report_vm_gpu.yml` | GPU-attached VMs with DCGM |
| VM + GPU time-slicing | `ocp_report_vm_gpu_timeslicing.yml` | vGPU time-slicing (56–57) |
| MVP promotions | `ocp_report_vm_mvp_promotions.yml` | Adaptive margin, multi-GPU history |

### PVC Rightsizing

```bash
nise report ocp \
  --static-report-file $FIXTURE_DIR/ocp_report_pvc_rightsizing.yml \
  --ocp-cluster-id $CLUSTER_ID \
  --ros-ocp-info \
  --write-monthly \
  -w /tmp/nise-pvc

# Verify PVC recommendations
curl -s -H "x-rh-identity: $IDENTITY" \
  'http://localhost:8000/api/cost-management/v1/recommendations/openshift/pvc?limit=5' \
  | python3 -m json.tool
```

**What to look for:** Five deterministic PVC scenarios in the `pvc-rightsizing` namespace:

| PVC name | Scenario | Expected recommendation |
|---|---|---|
| `pvc-oversized` | 100Gi capacity, ~5Gi usage (5%) | Downsize |
| `pvc-near-full` | 10Gi capacity, growing to ~9.5Gi | Expand or warn |
| `pvc-orphaned` | 20Gi capacity, zero usage | Flag as orphaned |
| `pvc-healthy` | 50Gi capacity, ~30Gi usage (60%) | No action needed |
| `pvc-vm-disk` | VM-owned PVC (`virt-launcher` pod) | Correlate with VM |

### Snapshot Classification

```bash
nise report ocp \
  --static-report-file $FIXTURE_DIR/ocp_report_snapshot_classification.yml \
  --ocp-cluster-id $CLUSTER_ID \
  --ros-ocp-info \
  --write-monthly \
  -w /tmp/nise-snapshot
```

**What to look for:** Four VolumeSnapshot records in `e2e-snap-ns`:

| Snapshot | Age | Source PVC exists | Restores | Classification |
|---|---|---|---|---|
| `e2e-snap-stale` | 120 days | Yes | 0 | Stale |
| `e2e-snap-orphaned` | 30 days | No | 0 | Orphaned |
| `e2e-snap-never-restored` | 45 days | Yes | 0 | Never restored |
| `e2e-snap-active` | 5 days | Yes | 2 | Active |

### Quota Recommendations

#### Namespace ResourceQuota

```bash
nise report ocp \
  --static-report-file $FIXTURE_DIR/ocp_report_quota.yml \
  --ocp-cluster-id $CLUSTER_ID \
  --ros-ocp-info \
  --write-monthly \
  -w /tmp/nise-quota
```

**What to look for:** High pod CPU/memory requests with explicit `resource_quota` used values
in namespace `quota-e2e-ns`. The quota plugin recommends tightening or raising limits.

#### ClusterResourceQuota

```bash
nise report ocp \
  --static-report-file $FIXTURE_DIR/ocp_report_cluster_quota.yml \
  --ocp-cluster-id $CLUSTER_ID \
  --ros-ocp-info \
  --write-monthly \
  -w /tmp/nise-crq

# Verify cluster quota recommendations
curl -s -H "x-rh-identity: $IDENTITY" \
  'http://localhost:8000/api/cost-management/v1/recommendations/openshift/cluster-quota?limit=5' \
  | python3 -m json.tool
```

**What to look for:** Two CRQs (`e2e-crq-team`, `e2e-crq-platform`) with 85–90% utilization
ratios. The cluster-quota plugin recommends adjusting hard limits based on actual usage.

### Node Consolidation

```bash
nise report ocp \
  --static-report-file $FIXTURE_DIR/ocp_report_node_idle_consolidation.yml \
  --ocp-cluster-id $CLUSTER_ID \
  --ros-ocp-info \
  --write-monthly \
  -w /tmp/nise-node

# Verify node recommendations
curl -s -H "x-rh-identity: $IDENTITY" \
  'http://localhost:8000/api/cost-management/v1/recommendations/openshift/nodes?limit=10' \
  | python3 -m json.tool
```

**What to look for:** 6 nodes with distinct utilization profiles:

| Node | CPU usage | Classification |
|---|---|---|
| `node-zombie` | ~0.001 cores | Zombie (near-zero) |
| `node-idle` | ~0.09 cores | Idle |
| `m5-node-1` through `m5-node-4` | ~0.3–0.4 cores (of 16) | Underutilized, consolidation candidates |

## Auto-Seeding Templates

The `examples/ros_ocp_seeding/` directory contains templates used by the
cost-onprem-chart E2E framework's automatic data seeding fixture (`data_seeding.py`).
These ensure minimum baseline data exists before tests run. They are not typically
used for manual testing but can serve as minimal examples.

| Template | Target table | Minimum rows |
|---|---|---|
| `seed_container.yml` | `daily_container_digests` | 100 |
| `seed_pvc.yml` | `daily_pvc_digests` | 20 |
| `seed_gpu.yml` | `gpu_container_digests` | 20 |
| `seed_cluster_quota.yml` | `cluster_quota_recommendation_sets` | 2 |
| `seed_domain.yml` | `daily_container_digests` (domain/workload classification) | 30 |

The seeding fixture is idempotent — it checks current row counts and only generates
data for categories below their threshold. See
[Testing — Automatic Data Seeding](../testing.md#automatic-data-seeding) for details
on thresholds and skip conditions.

## Tips

!!! warning "Always use `--ros-ocp-info`"
    Without this flag, NISE won't generate the container-level columns
    (`ocp_ros_usage.csv`, `ocp_ros_namespace_usage.csv`) needed by the ROS processor.
    Cost-only data (`ocp_pod_usage.csv`) is insufficient for recommendations.

!!! warning "Use `--write-monthly`, not `--daily-reports`"
    `--daily-reports` requires `INSIGHTS_ACCOUNT_ID` and `INSIGHTS_ORG_ID` environment
    variables and silently produces no output without them. `--write-monthly` works
    without extra env vars.

- The `--ocp-cluster-id` must match a source registered in the Koku listener or
  the cluster UUID used in your Kafka message metadata
- For multi-month data, NISE generates separate directories per month — package each
  month into its own tarball
- Templates with `start_date: last_month` / `end_date: today` automatically cover
  ~30 days of data, satisfying the ROS minimum data requirement (7 days)
- Templates with Jinja2 placeholders (`{{ variable }}`) are rendered by the E2E test
  framework at runtime — they cannot be used directly with the `nise` CLI without
  first substituting the placeholders
