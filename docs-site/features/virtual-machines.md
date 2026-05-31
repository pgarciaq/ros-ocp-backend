# Virtual Machine Recommendations

!!! info "Status: Preview (Beta)"
    VM recommendations are **available in ros-ocp-backend** when the `vm` plugin is enabled
    (`ROS_ENABLE_VM_RECS`, default on). The Cost Management UI does not yet have a dedicated
    VM optimizations page — use the REST API or IQE/E2E validation. Container, node, PVC, quota,
    and GPU recommendations are unchanged.

!!! info "Quick Facts"
    **Scope:** OpenShift Virtualization (KubeVirt) virtual machines  
    **Plugin:** `vm` (Produce phase, **priority 40**)  
    **Collection:** **15-minute** metrics; operator dual CSV (15-min ROS, hourly Koku)  
    **Analysis windows:** **7 / 15 / 30** days by default (configurable via terms API)  
    **Gate:** `ROS_ENABLE_VM_RECS` (**on by default**; no-ops if no VM data)  
    **Confidence:** `high` (guest agent) or `moderate` (hypervisor-only) per VM  
    **Units:** Whole vCPUs and whole GiB

---

## What it does

**Virtual Machine Recommendations** right-size OpenShift Virtualization workloads using the same ROS pipeline as containers, tuned for KubeVirt:

| Capability | Description |
|------------|-------------|
| **vCPU / memory** | Percentile-based sizing with downsize hysteresis; whole vCPU and GiB output |
| **Instance types** | Maps sizing to built-in types (`u1.large`, `cx1.xlarge`, `m1.2xlarge`, …) via smallest-fit |
| **Idle detection** | OS-aware CPU/memory thresholds (Linux vs Windows) |
| **Disk trending** | Guest-agent: days-until-full; hypervisor-only: allocation growth rate + expand GiB |
| **I/O profile** | p95 IOPS/throughput with high-I/O hints |

Recommendations are exposed at:

- `GET /api/cost-management/v1/recommendations/openshift/vm`
- `GET /api/cost-management/v1/recommendations/openshift/vm/detail`

Settings:

- `GET/PUT .../recommendations/openshift/settings/vm`
- `GET/PUT .../recommendations/openshift/settings/vm/terms`

Internal design: [VM recommendations design doc](../../docs/design/vm-recommendations.md).

---

## Guest agent: with or without

ROS works **without** a QEMU guest agent (hypervisor metrics only). Installing the guest agent improves accuracy:

| Mode | What you get | `confidence` |
|------|--------------|----------------|
| **With guest agent** | Filesystem growth, days-until-full, critical fill alerts | `high` |
| **Without guest agent** | CPU, memory, allocation growth, I/O; wider margins | `moderate` |

When guest-agent filesystem metrics are present, hypervisor-only disk trending is **not** used for that VM.

---

## Notifications

Each recommendation includes a `notifications` array of objects:

```json
{"code": 18, "type": "warning", "message": "..."}
```

| Code | Severity | Meaning (plain language) |
|------|----------|---------------------------|
| **18** | Warning | VM is **idle** — CPU and memory consistently near zero |
| **19** | Warning | VM is **oversized** — recommended size is much smaller than allocated |
| **37** | Info | **Disk allocation is growing** (no guest agent) — consider expanding proactively |
| **38** | Info | **No guest agent** — recommendations use hypervisor metrics only |
| **39** | Warning | **High disk I/O** — consider a faster storage class or compute-optimized type |
| **40** | Warning | **Disk filling up** (guest agent) — estimated days until full &lt; 90 |
| **41** | Info | **Instance type suggested** — e.g. move to `cx1.large` |
| **42** | Critical | **Filesystem nearly full** (&gt; 90% used) — expand soon |

---

## Instance types

ROS recommends specific **OpenShift Virtualization** instance type names when `instance_type_matching` is enabled (default on):

| Series | Examples | Profile |
|--------|----------|---------|
| **u1** | `u1.small`, `u1.large`, `u1.2xlarge` | General purpose / utility |
| **cx1** | `cx1.large`, `cx1.4xlarge` | Compute-optimized (high CPU:memory) |
| **m1** | `m1.large`, `m1.2xlarge` | Memory-optimized |

The engine picks the **smallest** type that fits recommended vCPU and memory. **GPU types (`gn1`) are not recommended** until GPU usage data is available.

---

## Configuration

Same three-tier model as other recommendation types: defaults → Settings API → environment locks.

### Settings API (tenant admins)

```bash
# Read effective thresholds
curl -s -H "x-rh-identity: $IDENTITY" \
  http://localhost:8000/api/cost-management/v1/recommendations/openshift/settings/vm

# Adjust idle threshold (example)
curl -s -X PUT -H "x-rh-identity: $IDENTITY" -H "Content-Type: application/json" \
  -d '{"thresholds":{"idle_cpu_mc":75}}' \
  http://localhost:8000/api/cost-management/v1/recommendations/openshift/settings/vm
```

Response blocks: `thresholds`, `memory_floors`, `disk` (includes `min_growth_mib_per_day`), `io`, `instance_type_matching`, `locked_fields`.

Terms (analysis windows):

```bash
curl -s -H "x-rh-identity: $IDENTITY" \
  http://localhost:8000/api/cost-management/v1/recommendations/openshift/settings/vm/terms
```

### Environment variables (platform operators)

| Variable | Default | Purpose |
|----------|---------|---------|
| `ROS_ENABLE_VM_RECS` | `true` | Master gate |
| `ROS_VM_CPU_PERCENTILE_COST` | `0.95` | CPU sizing percentile |
| `ROS_VM_CPU_PERCENTILE_PERF` | `0.99` | Performance engine CPU percentile |
| `ROS_VM_CPU_MARGIN_MIN` / `MAX` | `0.15` / `0.50` | Adaptive CPU margin |
| `ROS_VM_MEM_MARGIN_MIN` | `0.20` | Memory margin above p95 |
| `ROS_VM_DOWNSIZE_HYSTERESIS_RATIO` | `0.60` | Downsize ratio gate |
| `ROS_VM_MIN_VCPU_CHANGE` / `MIN_GIB_CHANGE` | `2` / `2` | Minimum change to recommend |
| `ROS_VM_IDLE_CPU_MC` / `IDLE_MEMORY_MIB` | `50` / `512` | Linux idle thresholds |
| `ROS_VM_IDLE_CPU_MC_WINDOWS` / `IDLE_MEMORY_MIB_WINDOWS` | `200` / `3072` | Windows idle thresholds |
| `ROS_VM_LINUX_MEMORY_FLOOR_GIB` / `WINDOWS_...` | `1` / `2` | Minimum recommended GiB |
| `ROS_VM_DISK_PROJECTION_DAYS` | `30` | Disk projection horizon |
| `ROS_VM_DISK_HEADROOM_PCT` | `0.25` | Headroom on projected size |
| `ROS_VM_DISK_ROUND_STEP_GIB` | `10` | Expand recommendation step |
| `ROS_VM_DISK_MIN_GROWTH_MIB_PER_DAY` | `100` | Hypervisor growth noise floor |
| `ROS_VM_HIGH_IOPS_THRESHOLD` | `3000` | High I/O notification threshold |
| `ROS_VM_ENABLE_INSTANCE_TYPE_MATCHING` | `true` | Enable u/cx/m matching |

See [Configurable thresholds](configurable-thresholds.md) for precedence rules.

---

## Example API response

```bash
curl -s -H "x-rh-identity: $IDENTITY" \
  'http://localhost:8000/api/cost-management/v1/recommendations/openshift/vm?limit=5&filter[namespace]=finance'
```

```json
{
  "data": [{
    "vm_name": "erp-backend",
    "namespace": "finance",
    "cluster_uuid": "cluster-uuid",
    "guest_os": "linux",
    "current": { "vcpu": 8, "memory_gib": 32, "disk_gib": 500, "instance_type": null },
    "recommended": {
      "vcpu": 4,
      "memory_gib": 16,
      "disk_gib": null,
      "instance_type": "u1.xlarge",
      "series": "general-purpose"
    },
    "metadata": {
      "guest_agent_detected": true,
      "confidence": "high",
      "term": "medium_term",
      "engine": "cost",
      "is_idle": false,
      "is_oversized": true
    },
    "disk_projection": {
      "days_until_full": 45,
      "growth_gib_per_day": 2.1,
      "recommended_expand_gib": 100
    },
    "notifications": [
      { "code": 19, "type": "warning", "message": "VM is oversized: ..." },
      { "code": 41, "type": "info", "message": "Recommended instance type: u1.xlarge (general-purpose series)" }
    ],
    "last_recommended_at": "2026-05-30T12:00:00Z"
  }]
}
```

Without a guest agent, `disk_projection.days_until_full` is typically **null** even when `growth_gib_per_day` is set.

---

## Limitations (not supported yet)

| Limitation | Impact |
|------------|--------|
| **No savings ($)** | API does not return cost or savings estimates |
| **GPU VMs** | No GPU/MIG recommendations |
| **UI** | No koku-ui VM recommendations page |
| **Current instance type** | Often `null` until operator sends instance type in CSV |
| **Per-mountpoint disk** | Single filesystem signal, not per mount |
| **Custom cluster instance types** | Built-in u/cx/m catalog only |
| **History** | Only the latest recommendation per VM/term/engine |

---

## Prerequisites

| Requirement | Why |
|-------------|-----|
| OpenShift Virtualization | KubeVirt metrics and VMIs |
| Metrics operator uploading `ros-openshift-vm-usage-*.csv` | Data into ROS |
| `vm` in ros-ocp-backend `enabledPlugins` | API and processing |
| QEMU guest agent (optional) | Higher confidence and filesystem alerts |

---

## Testing

| Layer | How to run |
|-------|------------|
| **Unit** | `go test ./internal/engine/... ./internal/ingestion/... ./internal/api/... -run 'VM\|Vm\|vm'` |
| **E2E** | `NAMESPACE=cost-onprem ./scripts/run-pytest.sh --ros -k vm` (cost-onprem-chart) |
| **IQE** | `test_ros_vm_recommendations.py` with nise `ocp_report_ros_vm.yml` |

See [internal test plan](../../docs/design/vm-test-plan.md).

---

## Related documentation

| Document | Audience |
|----------|----------|
| [Internal design: VM recommendations](../../docs/design/vm-recommendations.md) | Engineering — API, algorithms, notifications |
| [Container right-sizing](container-recommendations.md) | Available today |
| [Configurable thresholds](configurable-thresholds.md) | Settings precedence |
| [Dual engine](dual-engine.md) | Cost vs performance |
