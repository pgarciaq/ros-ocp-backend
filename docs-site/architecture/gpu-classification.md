# GPU Classification Thresholds

> **Last verified:** 2026-08-05

This document describes how ROS-OCP-Backend classifies GPU workload utilization for the **container** `gpu` plugin (Pods, Jobs, and OpenShift AI workloads on the same code path).

For **how sharing mechanisms differ by workload type** (containers vs KubeVirt VMs, catalogs, API fields), see [GPU sharing mechanisms by workload type](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/design/vm-recommendations.md#gpu-sharing-mechanisms-by-workload-type). VM guests additionally use `vgpu_profiles.yaml` (VM-only) for `recommended_vgpu_profile`.

For **NVIDIA documentation sources and catalog validation** when editing `gpu_catalog.yaml` or `vgpu_profiles.yaml`, see [GPU Catalogs — Data Sources and Validation](gpu-catalogs.md).

For the full cross-plugin parameter reference (including time-slicing and term defaults),
see [Recommendation Engine Reference](recommendation-engines.md).

## Classification Algorithm

GPU classification uses three DCGM profiling metrics averaged across all daily digests within the term window:

- **SM Active** (`DCGM_FI_PROF_SM_ACTIVE`) — fraction of streaming multiprocessors active
- **Tensor Pipe Active** (`DCGM_FI_PROF_PIPE_TENSOR_ACTIVE`) — fraction of tensor cores active
- **DRAM Active** (`DCGM_FI_PROF_DRAM_ACTIVE`) — fraction of memory bandwidth utilized

The classification decision tree (evaluated in order):

```
1. Has profiling data?  (any SM/Tensor/DRAM > 0)
   NO  → "no_profiling" (Tier 2: frame-buffer only)
   YES → continue

2. avgSM < IDLE_THRESHOLD (default 2%)?
   YES → "idle"

3. avgDRAM > MEMBOUND_DRAM_THRESHOLD (60%) AND avgTensor < MEMBOUND_TENSOR_THRESHOLD (15%)?
   YES → "memory_bound"

4. avgTensor < UNDERUTIL_TENSOR_THRESHOLD (15%) AND avgSM < UNDERUTIL_SM_THRESHOLD (25%)?
   YES → "underutilized"

5. avgTensor < 25% AND avgDRAM < 30%?
   YES → "compute_bound_underutil"

6. Default → "well_utilized"
```

Compact reference (defaults):

```
SM < 2%                          → idle           → deallocate
DRAM > 60% AND Tensor < 15%      → memory_bound   → MIG sized to FB
Tensor < 15% AND SM < 25%        → underutilized  → MIG or time-slicing
Tensor < 25% AND DRAM < 30%      → compute_bound_underutil → time-slicing
else                             → well_utilized  → no action
```

## Why Multi-Metric Classification Beats a Single Threshold

A common naive approach classifies any GPU with SM Active below 10% as
"underutilized" and recommends the same remediation (MIG or removal). That
collapses distinct workload shapes into one bucket and produces wrong actions.

**GPU utilization is multidimensional.** DCGM exposes three independent signals:

| Metric | What it measures | Why it matters |
|--------|------------------|----------------|
| SM Active | Streaming-multiprocessor occupancy | General compute intensity |
| Tensor Pipe Active | Tensor-core utilization | ML training/inference signature |
| DRAM Active | Memory-bandwidth pressure | Memory-bound vs compute-bound |

Different combinations imply **different actionable recommendations**:

- **Idle** (SM &lt; 2%) → remove the GPU allocation entirely
- **Memory-bound** (high DRAM, low tensor) → MIG profile sized to framebuffer, not time-slicing
- **Underutilized** (low SM *and* low tensor) → MIG partition or node time-slicing
- **Compute-bound underutil** (moderate SM/tensor, low DRAM) → time-slicing without MIG memory isolation
- **Well utilized** → no change

**Separates idle from underutilized.** A dev notebook at 1% SM and a batch
inference job at 18% SM both fall below a 10% single threshold, but only the
notebook should be deallocated. The tree's 2% idle gate (step 2) fires before
the 25% underutilized check (step 4), so idle workloads never receive a
partitioning recommendation they do not need.

**Catches workloads a single threshold misses.** Memory-bound LLM inference can
show 8–15% SM while DRAM exceeds 60%. A 10% SM rule labels it "underutilized"
(MIG or time-slice); the tree labels it `memory_bound` and sizes MIG to
framebuffer usage instead.

Implementation: [`GPUThresholds.Classify()`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/librobne/gpu/recommend.go).

## Misclassification Examples

Typical DCGM averages over a 7–30 day term window. Values below are illustrative.

| Workload | SM | DRAM | Tensor | Single 10% threshold | Multi-metric class | Correct action |
|----------|-----|------|--------|----------------------|-------------------|----------------|
| LLM inference (memory-bound) | 8% | 75% | 5% | "Underutilized" — misses memory binding | `memory_bound` | MIG profile sized to FB (e.g. `3g.20gb`) |
| Dev notebook (idle) | 1% | 2% | 0% | "Idle" (accidentally correct) | `idle` | Deallocate GPU; reclaim full device cost |
| Distributed training (compute-bound underutil) | 35% | 15% | 20% | "Well utilized" — SM above 10% | `compute_bound_underutil` | Node time-slicing; keep full GPU per job |
| Batch inference (underutilized) | 18% | 25% | 8% | "Underutilized" (same bucket as idle-ish workloads) | `underutilized` | MIG partition or time-slicing on MIG-capable node |

Public-facing summary: [GPU Classification (feature doc)](../features/gpu-classification.md).

## Threshold Configuration

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `ROS_GPU_IDLE_THRESHOLD` | 0.02 | SM Active below this = idle (2%) |
| `ROS_GPU_UNDERUTILIZED_SM_THRESHOLD` | 0.25 | SM Active below this = underutilized (25%) |
| `ROS_GPU_UNDERUTILIZED_TENSOR_THRESHOLD` | 0.15 | Tensor Active below this = underutilized (15%) |
| `ROS_GPU_MEMBOUND_DRAM_THRESHOLD` | 0.60 | DRAM Active above this = memory bound (60%) |
| `ROS_GPU_MEMBOUND_TENSOR_THRESHOLD` | 0.15 | Tensor Active below this (with high DRAM) = memory bound |
| `ROS_GPU_FB_HEADROOM_FACTOR` | 1.20 | Frame buffer headroom for MIG profile selection (20%) |

## Classifications and Their Meaning

| Classification | Meaning | User action | Notification codes |
|---------------|---------|-------------|-------------------|
| `idle` | GPU allocated but essentially unused (&lt;2% SM) | Remove GPU request/limit from the workload spec; reclaim the device for other tenants | **26** (GPU idle) |
| `underutilized` | Low compute *and* low tensor (SM &lt;25%, tensor &lt;15%) | Enable MIG on the node and assign a smaller profile, or enable GPU time-slicing for software multiplexing | **10** (GPU underutilized) |
| `memory_bound` | High DRAM (&gt;60%) with low tensor (&lt;15%) — weights/activations dominate | MIG profile sized to P98 framebuffer × headroom; avoid time-slicing (no memory isolation) | **27** (GPU memory-bound) |
| `compute_bound_underutil` | Moderate SM/tensor, low DRAM (&lt;30%) — compute present but device mostly idle | Prefer node-level time-slicing over MIG; workload benefits from shared device, not memory partition | **36** (time-slicing candidate, node scope) |
| `well_utilized` | None of the underutilization predicates match | No change; GPU sizing matches workload shape | — |
| `no_profiling` | Volta/Pascal — frame-buffer only, no SM/tensor/DRAM | FB-based MIG sizing only; treat confidence as lower | **28** (no profiling data) |

All recommendations are **advisory**. The operator collects metrics; ROS computes
classifications; the user applies changes manually (or via their own GitOps
pipeline). See [HPA/VPA and deployment modes](hpa-vpa-deployment-modes.md) for
the broader actuation model.

## MIG Profile Selection

For workloads classified as `underutilized` or `memory_bound` on MIG-capable GPUs (A100, A30, H100, H200, B100, B200):

1. Compute P98 of daily max frame buffer usage across all digests
2. Apply headroom factor: `required_FB = P98_FB × ROS_GPU_FB_HEADROOM_FACTOR`
3. Select smallest MIG profile where `profile_FB_MiB >= required_FB`
4. If no profile fits → recommend `"full_gpu"` (no MIG benefit)

## GPU Confidence Score

The confidence score (0.0–1.0) is based on:

1. **Data volume base** (how many days of data):
   - 1 day → 0.3
   - 2–3 days → 0.6
   - 4–6 days → 0.8
   - 7+ days → 1.0

2. **Stability penalty** (30% reduction when max SM > 5× average SM):
   - High variability suggests bursty workloads where classification may be unreliable

## Two-Tier GPU Support

| Tier | GPU Generation | Available Metrics | Classification |
|------|---------------|-------------------|----------------|
| Tier 1 | Turing+ (T4, A100, H100, etc.) | SM Active, Tensor, DRAM, FB | Full 6-class classification |
| Tier 2 | Volta, Pascal | Frame buffer only | `no_profiling` (FB-based MIG only) |

## Deferred Items

Enhancements documented but not implemented. **Gap 5** (MIG list in-memory pagination and
lack of multi-GPU bin-packing) is expanded in
[known-issues.md § GPU MIG — Known limitations (Gap 5)](../historical/feature-status-archive.md#gpu-mig-known-limitations-gap-5).
Full tracking table:
[known-issues.md § GPU: Deferred / Future Work](../historical/feature-status-archive.md#gpu-deferred-future-work).

| # | Item | Consumer | Why deferred |
|---|------|----------|--------------|
| 1 | Node `node_gpu_count` (allocatable GPUs per node) | Tier 2 MachineSet GPU-aware consolidation; node GPU savings | No consumer until Tier 2 + GPU-aware node engine |
| 2 | Multi-GPU / cross-GPU consolidation (per-device DCGM; no node bin-packing) | ML pods requesting 4–8 GPUs; containers each using a slice on a different GPU | <5% of workloads; engine sizes MIG per container only; VMs have notification **54** |
| 3 | MIG list API SQL-backed pagination (`GET .../gpu/mig` loads all rows then paginates in Go) | Fleets with 10k+ MIG-capable containers | Tens–low hundreds of MIG containers today; in-memory path is <50ms |

## Source Files

- Classification logic: [`librobne/gpu/recommend.go`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/librobne/gpu/recommend.go)
- GPU model catalog: [`librobne/gpu/catalog.go`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/librobne/gpu/catalog.go)
- Threshold struct and classification: [`librobne/gpu/recommend.go` → `GPUThresholds.Classify()`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/librobne/gpu/recommend.go)
- Process-wide initialization: [`internal/engine/gpu_wire.go` → `InitGPUEngine()`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/engine/gpu_wire.go)
- Config defaults: [`internal/config/config.go`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/config/config.go)
