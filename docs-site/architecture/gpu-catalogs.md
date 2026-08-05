# GPU Catalogs — Data Sources and Validation

> **Last verified:** 2026-08-05

ROS embeds two YAML catalogs at compile time (`go:embed`) in `internal/engine/`:

| File | Scope | Used by |
|------|-------|---------|
| [`gpu_catalog.yaml`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/engine/gpu_catalog.yaml) | GPU model specs, MIG profiles, SM count, total VRAM | **Containers** (Pods, Jobs, OpenShift AI) and **VMs** (MIG / `recommended_gpu_profile`) |
| [`vgpu_profiles.yaml`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/engine/vgpu_profiles.yaml) | NVIDIA GRID **C-series** vGPU profiles | **VMs only** (`recommended_vgpu_profile`, notification **56**) |

Operational workflow (DCGM matching, Prometheus alerts, test commands): [GPU Catalog Maintenance](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/operations/gpu-catalog.md).

Classification thresholds (idle, memory-bound, MIG sizing math): [GPU Classification](gpu-classification.md).

VM-specific sharing behavior: [GPU sharing by workload type](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/design/vm-recommendations.md#gpu-sharing-mechanisms-by-workload-type).

---

## Data Sources

### `gpu_catalog.yaml` (MIG profiles)

Used for MIG recommendations on **both** containers and KubeVirt VMs. Entries include total frame buffer, SM count, and per-profile slice geometry.

| Source | URL | What it provides |
|--------|-----|------------------|
| NVIDIA Multi-Instance GPU User Guide | https://docs.nvidia.com/datacenter/tesla/mig-user-guide/ | Canonical MIG profile names, FB sizes, compute slices per GPU model |
| NVIDIA Data Center GPU product pages | https://www.nvidia.com/en-us/data-center/ | Total VRAM per GPU model |
| `nvidia-smi mig -lgip` on real hardware | (cluster access) | Ground truth for profile names on a specific driver version |

### `vgpu_profiles.yaml` (vGPU / GRID profiles)

Used only when recommending **vGPU C-series** profiles for VM time-slicing (not loaded by the container `gpu` plugin).

| Source | URL | What it provides |
|--------|-----|------------------|
| NVIDIA Virtual GPU Software User Guide, Appendix A | https://docs.nvidia.com/vgpu/latest/grid-vgpu-user-guide/ | Profile names, FB per profile, max instances per GPU |
| NVIDIA vGPU release notes | https://docs.nvidia.com/vgpu/latest/ | New profiles added per driver version |

---

## Profile Family Choice

- **C-series** (compute / CUDA) is used, not **Q-series** (graphics / VDI).
- **Rationale:** ROS optimizes compute workloads (ML, AI, batch processing). Graphics-heavy guests would need Q-series profiles plus workload-type detection (labels, existing Q profile on guest, or tenant settings) — see [Future Q-series support](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/design/vm-recommendations.md#future-q-series-support).

---

## Validation Process

When adding or updating GPU catalog entries:

1. **Identify the GPU model and SKU** (e.g., A100 80GB PCIe, H100 SXM5 80GB, B200 180GB).
2. **Look up the official MIG or vGPU profile table** in the NVIDIA documentation links above.
3. **Cross-reference profile names** — naming is strict:
   - **MIG:** `{compute_slices}g.{fb_gb}gb` (e.g., `3g.40gb`)
   - **vGPU C-series:** `{model}-{fb_gb}C` (e.g., `A100D-40C`, `T4-4C`; YAML uses lowercase `grid_*` API hints)
4. **Verify FB sizes** — the sum of concurrent profile FB allocations must not exceed total GPU VRAM for the SKU.
5. **Check max instances** — from the NVIDIA table (varies by profile size); set `max_instances` in `vgpu_profiles.yaml`.
6. **Test with `nvidia-smi mig -lgip`** (if hardware is available) to confirm profile names match the installed driver version.
7. **Update code and tests:**
   - `gpu_catalog.yaml`: add `matchGPUModelKey()` case in [`gpu_metadata.go`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/engine/gpu_metadata.go) and tests in [`gpu_metadata_test.go`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/engine/gpu_metadata_test.go).
   - `vgpu_profiles.yaml`: tests in [`vgpu_profiles_test.go`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/engine/vgpu_profiles_test.go).
8. **Run unit tests:**

   ```bash
   go test ./internal/engine/ -run 'TestGPUCatalog|TestMatchGPUModel|TestGPUModelMIG|TestVGPUProfile'
   ```

---

## Common Pitfalls

| Pitfall | Detail |
|---------|--------|
| **Marketing vs driver naming** | NVIDIA marketing may cite 192GB for B200, but driver MIG profiles use **180GB** geometry. |
| **H100 variants** | H100 80GB (SXM5) ≠ H100 94GB (NVL) ≠ H100 96GB (GH200) — each has different profile FB sizes; use separate catalog keys. |
| **Profile suffixes** | `+me` (media engine) profiles exist but are **omitted** from our catalog (compute-only focus). |
| **vGPU Q vs C vs A** | Q = graphics/VDI, C = compute, A = apps — we use **C only**. |
| **T4 has no MIG** | Time-slicing only; smallest vGPU compute profile is **T4-4C** (4GB), not 1GB. |

---

## When to Update

- New NVIDIA GPU generation announced (e.g., Blackwell B200, next-gen).
- NVIDIA driver update adds new MIG or vGPU profiles.
- Customer reports **unknown GPU model** via notifications (code **6** for containers, **50** for VMs) or `rosocp_gpu_model_unrecognized_total` increases. Check logs for `gpu_metadata: unrecognized GPU model` to identify the exact model string.
- **Periodic review:** recommend quarterly, aligned with NVIDIA driver / vGPU software releases.

---

## Validation History

| Date | Catalog | Validated against | Changes |
|------|---------|-------------------|---------|
| 2026-06-01 | `vgpu_profiles.yaml` | NVIDIA vGPU User Guide Appendix A.1 | Fixed A100/A30/T4: switched from Q to C-series, corrected FB sizes and max instances |
| 2026-06-01 | `gpu_catalog.yaml` | NVIDIA MIG User Guide | Fixed H100_94GB (47GB profiles), B200 (180GB geometry, renamed) |

---

## Related Files

| File | Role |
|------|------|
| [`gpu_metadata.go`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/engine/gpu_metadata.go) | Loads `gpu_catalog.yaml`, DCGM model matching |
| [`vgpu_profiles.go`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/engine/vgpu_profiles.go) | Loads `vgpu_profiles.yaml` |
| [`vm_gpu_timeslicing.go`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/engine/vm/vm_gpu_timeslicing.go) | VM vGPU profile selection |
| [`gpu_recommender.go`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/engine/gpu_recommender.go) | Container MIG recommendations |
