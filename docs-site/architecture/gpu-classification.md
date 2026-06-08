# GPU Classification Thresholds

This document describes how ROS-OCP-Backend classifies GPU workload utilization for the **container** `gpu` plugin (Pods, Jobs, and OpenShift AI workloads on the same code path).

For **how sharing mechanisms differ by workload type** (containers vs KubeVirt VMs, catalogs, API fields), see [GPU sharing mechanisms by workload type](../../docs/design/vm-recommendations.md#gpu-sharing-mechanisms-by-workload-type). VM guests additionally use `vgpu_profiles.yaml` (VM-only) for `recommended_vgpu_profile`.

For **NVIDIA documentation sources and catalog validation** when editing `gpu_catalog.yaml` or `vgpu_profiles.yaml`, see [GPU Catalogs — Data Sources and Validation](gpu-catalogs.md).

For the full cross-plugin parameter reference (including time-slicing and term defaults),
see [Recommendation Engine Reference](recommendation-engines.md).

User-facing guide (workload examples, what to do for each class):
[GPU Workload Classification](../features/gpu-classification.md).

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

GPU utilization is multidimensional (compute vs memory vs tensor). A single
10% SM threshold collapses distinct workload shapes into one bucket and produces
wrong remediations — for example, labeling memory-bound LLM inference as generic
"underutilized" instead of sizing MIG to framebuffer usage.

The tree evaluates metrics in priority order so **idle** (&lt;2% SM) is
separated from **underutilized** (&lt;25% SM), and **memory-bound** (high DRAM,
low tensor) is detected before generic underutilization checks. See
[workload examples](../features/gpu-classification.md#why-not-a-single-threshold).

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
| `idle` | GPU allocated but essentially unused (&lt;2% SM) | Remove GPU from workload spec | **26** |
| `underutilized` | Low SM (&lt;25%) and low tensor (&lt;15%) | MIG partition or time-slicing | **10** |
| `memory_bound` | High DRAM (&gt;60%), low tensor (&lt;15%) | MIG sized to framebuffer; not time-slicing | **27** |
| `compute_bound_underutil` | Moderate compute, low DRAM (&lt;30%) | Node time-slicing | **36** (node) |
| `well_utilized` | Healthy utilization | No action | — |
| `no_profiling` | Volta/Pascal — FB only | FB-based MIG sizing; lower confidence | **28** |

All recommendations are **advisory** — apply changes manually or via GitOps.

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
[known-issues.md § GPU MIG — Known limitations (Gap 5)](../known-issues.md#gpu-mig-known-limitations-gap-5).
Full tracking table:
[known-issues.md § GPU: Deferred / Future Work](../known-issues.md#gpu-deferred-future-work).

| # | Item | Consumer | Why deferred |
|---|------|----------|--------------|
| 1 | Node `node_gpu_count` (allocatable GPUs per node) | Tier 2 MachineSet GPU-aware consolidation; node GPU savings | No consumer until Tier 2 + GPU-aware node engine |
| 2 | Multi-GPU / cross-GPU consolidation (per-device DCGM; no node bin-packing) | ML pods requesting 4–8 GPUs; containers each using a slice on a different GPU | <5% of workloads; engine sizes MIG per container only; VMs have notification **54** |
| 3 | MIG list API SQL-backed pagination (`GET .../gpu/mig` loads all rows then paginates in Go) | Fleets with 10k+ MIG-capable containers | Tens–low hundreds of MIG containers today; in-memory path is <50ms |

## Source Files

- Classification logic: [`internal/engine/gpu_recommender.go`](../../internal/engine/gpu_recommender.go)
- GPU model catalog: [`internal/engine/gpu_metadata.go`](../../internal/engine/gpu_metadata.go)
- Threshold struct and classification: [`internal/engine/gpu_recommender.go` → `GPUThresholds.Classify()`](../../internal/engine/gpu_recommender.go)
- Process-wide initialization: [`internal/engine/gpu_recommender.go` → `InitGPUEngine()`](../../internal/engine/gpu_recommender.go)
- Config defaults: [`internal/config/config.go`](../../internal/config/config.go)
