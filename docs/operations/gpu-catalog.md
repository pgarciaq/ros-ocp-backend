# GPU Catalog Maintenance

The ROS-OCP backend maintains a catalog of NVIDIA GPU hardware specs used for
MIG partitioning recommendations, utilization scoring, and time-slicing savings
calculations. This document explains how to keep it current.

## File Locations

| File | Purpose |
|------|---------|
| [`internal/engine/gpu_catalog.yaml`](../../internal/engine/gpu_catalog.yaml) | GPU specs (VRAM, SM count, MIG profiles) — edit this to add models |
| [`internal/engine/gpu_metadata.go`](../../internal/engine/gpu_metadata.go) | DCGM string matching + YAML loader + Prometheus counter |
| [`internal/engine/gpu_metadata_test.go`](../../internal/engine/gpu_metadata_test.go) | Test coverage for model matching |

## How to Know When an Update Is Needed

A Prometheus counter is emitted every time a cluster reports a GPU model that
isn't in the catalog:

```
rosocp_gpu_model_unrecognized_total{model_name="NVIDIA B100"}
```

**Set up an alert** on this counter. Any non-zero value means real clusters have
GPUs we're not providing recommendations for. The `model_name` label shows the
exact DCGM-reported string.

A one-time warning log is also emitted per unrecognized model:
```
gpu_metadata: unrecognized GPU model "NVIDIA B100" — add to gpu_catalog.yaml and matchGPUModelKey
```

## Adding a New GPU Model

### Step 1: Find the specs

Visit NVIDIA's datasheets to collect:
- **Total VRAM** (in MiB) — e.g., 81920 for 80GB
- **SM count** — streaming multiprocessors
- **MIG support** — yes/no
- **MIG profiles** — slice count + memory per profile (if MIG-capable)

### Step 2: Add to `gpu_catalog.yaml`

```yaml
  NEW_MODEL:
    name: NEW_MODEL
    total_fb_mib: 81920
    sm_count: 132
    mig_supported: true
    profiling_supported: true
    profiles:
      - name: "1g.10gb"
        slices: 1
        fb_size_mib: 10240
      - name: "7g.80gb"
        slices: 7
        fb_size_mib: 81920
```

### Step 3: Add string matching in `gpu_metadata.go`

In `matchGPUModelKey()`, add a case that maps the DCGM-reported string (lowercased)
to your catalog key. Order matters — put specific matches before general ones:

```go
case strings.Contains(lower, "b100"):
    return "B100"
```

### Step 4: Add test coverage

In `gpu_metadata_test.go`, add to the `TestMatchGPUModel` table:

```go
{"B100", "NVIDIA B100", "B100"},
```

If MIG-capable, add to the `migModels` slice in `TestGPUModelMIGProfiles`.
If not, add to `nonMIG`.

### Step 5: Verify

```bash
go test ./internal/engine/... -run "TestMatchGPUModel|TestGPUModelMIG|TestGPUModelCount" -v
```

## NVIDIA Reference URLs

### Data Center GPU Product Pages

| GPU Family | URL |
|-----------|-----|
| Full catalog | https://www.nvidia.com/en-us/data-center/products/ |
| Overview | https://resources.nvidia.com/en-us-data-center-overview |

### Individual Datasheets (SM count, VRAM, TDP)

| Model | URL |
|-------|-----|
| A100 | https://www.nvidia.com/en-us/data-center/a100/ |
| H100 | https://www.nvidia.com/en-us/data-center/h100/ |
| H200 | https://www.nvidia.com/en-us/data-center/h200/ |
| B200 | https://www.nvidia.com/en-us/data-center/b200/ |
| L40S | https://www.nvidia.com/en-us/data-center/l40s/ |
| L4 | https://www.nvidia.com/en-us/data-center/l4/ |
| T4 | https://www.nvidia.com/en-us/data-center/tesla-t4/ |

### MIG Partition Geometries

The definitive source for MIG profile names, slice counts, and memory allocations:

https://docs.nvidia.com/datacenter/tesla/mig-user-guide/index.html

See "Supported GPU Products" → each model lists available profiles.

### Cloud-Specific GPU Variants

Some GPUs are cloud-exclusive SKUs with different specs than their data center counterparts:

| Cloud | GPU Docs |
|-------|----------|
| AWS | https://aws.amazon.com/ec2/instance-types/g5/ (A10G), https://aws.amazon.com/ec2/instance-types/p5/ (H100) |
| GCP | https://cloud.google.com/compute/docs/gpus |
| Azure | https://learn.microsoft.com/en-us/azure/virtual-machines/sizes/gpu-accelerated/overview |

### Identifying DCGM Model Strings

The operator reports GPU model names via DCGM. To see what string a specific GPU
reports (needed for `matchGPUModelKey`):

```bash
# On a GPU node:
nvidia-smi -L
# Output: GPU 0: NVIDIA A100-SXM4-80GB (UUID: ...)

# Or from Prometheus (DCGM exporter):
# Look for DCGM_FI_DEV_NAME label value
```

Common patterns:
- `"NVIDIA A100-SXM4-80GB"` — A100 SXM 80GB
- `"NVIDIA A100-PCIE-40GB"` — A100 PCIe 40GB
- `"NVIDIA H100 80GB HBM3"` — H100 80GB
- `"NVIDIA H100 NVL"` — H100 NVL 94GB
- `"Tesla V100-SXM2-32GB"` — V100 32GB (older "Tesla" branding)
- `"NVIDIA A10G"` — A10G (AWS-specific)
- `"NVIDIA L4"` — L4
- `"NVIDIA T4"` — T4

## Architecture Notes

- The YAML is embedded at compile time via `go:embed` — no file I/O at runtime
- `ComputeFrac` (fraction of GPU compute capacity) is calculated automatically
  from `slices / 7` during YAML parsing
- The Prometheus counter uses a 64-character label cap to prevent cardinality
  explosion from malformed DCGM strings
- Warning logs are deduplicated per model string (one-shot) to avoid log spam
