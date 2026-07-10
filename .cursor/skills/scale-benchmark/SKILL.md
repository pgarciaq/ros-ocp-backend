---
name: scale-benchmark
description: >-
  Run scale benchmarks of the ROS-OCP native engine on OpenShift clusters.
  Covers nise data generation on-cluster, tarball packaging, ingestion via
  the Cost Management pipeline, and metric collection. Use when the user asks
  to run, re-run, or prepare a scale benchmark, generate nise data for
  benchmarking, upload data to a cluster, or troubleshoot benchmark failures.
---

# Scale Benchmark Skill

Run scale benchmarks (4K–50K+ workloads) of the ROS-OCP native engine on
OpenShift clusters, including containers, GPU workloads, and VMs. This skill
captures hard-won knowledge from multiple benchmark iterations — follow it
precisely to avoid repeating past failures.

## Golden rules

1. **Generate data ON the cluster, never on the laptop.** Transferring
   multi-GiB tarballs over sshuttle will fail. Create a `nise-generator`
   pod with a PVC and run nise inside it.

2. **Never use ConfigMaps for large files.** Kubernetes limits ConfigMaps
   to 1 MiB (3 MiB with base64). A 10K-container nise config is ~2 MB.
   Use `oc cp` or generate it directly inside the pod with a Python script.

3. **Always use unique image tags.** `imagePullPolicy: IfNotPresent` means
   reusing a tag silently keeps the old image. Use `bench-$(date -u +%Y%m%d%H%M)`.

4. **Clean MinIO AND PostgreSQL before each run.** Stale data causes
   misleading results and can trigger duplicate-key errors.

5. **Check cluster connectivity first.** Run `oc whoami` before any operation.
   If it fails, ask the user to reconnect sshuttle.

## Detailed runbook

Read [scale-benchmark-runbook.md](../../../docs-site/operations/scale-benchmark-runbook.md)
for the full step-by-step procedure with copy-pasteable commands.

## Data flow (critical to understand)

```
nise (CSV) → tar.gz → ingress service → Koku listener → MinIO → Koku worker
                                                                      ↓
Prometheus ← ros-ocp-backend ← S3 notification ← MinIO/Kafka
```

Data does NOT go directly to `ros-ocp-backend`. It flows through the full
Cost Management pipeline. The Koku listener is typically the bottleneck at
10K+ containers (~35 min), not the ROS processor (~13 min).

## Nise configuration format

The YAML MUST use nise's `OCPGenerator` format. This is the only format that
works:

```yaml
---
generators:
  - OCPGenerator:
      start_date: 2026-06-01
      end_date: 2026-06-30
      nodes:
        - node:
          node_name: bench-node-000
          cpu_cores: 32
          memory_gig: 128
          namespaces:
            bench-ns-0000:
              pods:
                - pod:
                  pod_name: bench-ns-0000-pod-000
                  cpu_request: 100
                  cpu_limit: 500
                  mem_request_gig: 0.5
                  mem_limit_gig: 4.0
                  labels: label_app:bench-ns-0000|label_version:v1
```

**Common mistakes:**
- Missing `OCPGenerator:` wrapper → `AttributeError: 'str' has no attribute 'get'`
- Putting namespaces as a list instead of a dict → silent empty output
- Missing `--ros-ocp-info` flag → no ROS container CSVs generated
- Using `--daily-reports` instead of `-w` → needs `INSIGHTS_ACCOUNT_ID` env var

## GPU container configuration

To generate GPU recommendations, add a `gpus:` block to pods inside
`OCPGenerator`. Without this, nise generates NO GPU data and the benchmark
will have zero GPU recommendations.

```yaml
# Inside an OCPGenerator pod definition:
- pod:
  pod_name: gpu-training-pod-000
  cpu_request: 4
  cpu_limit: 8
  mem_request_gig: 16
  mem_limit_gig: 32
  labels: label_app:ml-training|label_workload:training
  gpus:
    - gpu:
      gpu_model: "Tesla T4"                    # DCGM name (required)
      gpu_memory_capacity_mib: 15360           # frame buffer in MiB
      sm_active_avg: 0.12                      # optional: GPU SM utilization
      tensor_pipe_active_avg: 0.05             # optional: tensor core utilization
      dram_active_avg: 0.08                    # optional: memory bandwidth
      fb_usage_avg: 1200.0                     # optional: FB usage in MiB
    - gpu:
      gpu_model: "NVIDIA A100-SXM4-80GB"       # MIG-capable GPU
      gpu_memory_capacity_mib: 81559
      mig_profile: "3g.40gb"                   # enables MIG slice recommendations
```

**Supported GPU models** (from `nise/generators/ocp/gpu_models.py`):
- `Tesla T4` (16 GiB, time-slicing only)
- `NVIDIA A10` / `NVIDIA A10G` (24 GiB)
- `NVIDIA A30-24GB` (24 GiB, MIG: 1g.6gb, 2g.12gb, 4g.24gb)
- `NVIDIA A100-PCIE-40GB` / `NVIDIA A100-SXM4-80GB` (MIG capable)
- `NVIDIA L4` / `NVIDIA L40` / `NVIDIA L40S`
- `NVIDIA H100-SXM5-80GB` / `NVIDIA H100 NVL` (MIG capable)
- `NVIDIA H200` / `NVIDIA B200` (MIG capable)
- `Tesla V100-SXM2-16GB` / `Tesla V100-SXM2-32GB` (no profiling)

**GPU data generates these additional CSV files:**
- `ocp_gpu_usage` — GPU device metrics (uuid, model, memory, uptime, MIG info)
- `ocp_ros_vm_gpu_device` — per-device ROS metrics (only for VMs with GPUs)

**Key rule:** GPU nodes should have `node_labels: nvidia_com_gpu_present:True`
for realistic data, though it's not strictly required by nise.

## VM (OpenShift Virtualization) configuration

VM data uses a **completely different generator**: `OCPVirtualMachineGenerator`.
It is NOT part of `OCPGenerator`. You must add a separate generator block.

```yaml
generators:
  # Regular containers (OCPGenerator)
  - OCPGenerator:
      start_date: 2026-06-01
      end_date: 2026-06-30
      nodes:
        # ... container pods here ...

  # VMs (OCPVirtualMachineGenerator) — separate block
  - OCPVirtualMachineGenerator:
      start_date: 2026-06-01
      end_date: 2026-06-30
      vms:
        - vm_name: web-server-linux-01
          namespace: production
          node_name: worker-1
          guest_os: linux           # linux, windows, or ""
          guest_agent: true         # populate guest-agent CSV columns
          vcpu: 4
          memory_gib: 8
          disk_gib: 100
        - vm_name: idle-vm-01
          namespace: dev
          node_name: worker-2
          guest_os: linux
          guest_agent: true
          vcpu: 2
          memory_gib: 4
          disk_gib: 40
          idle: true                # low usage → idle detection
        - vm_name: gpu-vm-01
          namespace: ml-training
          guest_os: linux
          guest_agent: true
          vcpu: 16
          memory_gib: 64
          disk_gib: 500
          gpu_count: 1
          gpu_model: "NVIDIA A100-SXM4-80GB"
          gpu_utilization: medium   # idle, low, medium, high, saturated, fb_saturated
```

**VM-specific flags** (all optional):
- `idle: true` — near-zero usage (abandoned VM detection)
- `abandoned: true` — zero usage all days
- `crash_loop: true` — high restart count
- `windows_update_spike: true` — Windows-specific CPU spikes
- `oversized_for_instance_type: true` — low usage on large VM
- `high_io: true` — disk IOPS above threshold
- `gpu_count`, `gpu_model`, `gpu_utilization` — VM-attached GPUs
- `gpu_mig_profile` — MIG slice for VM GPU
- `fixed_usage: {cpu_pct: 0.7, mem_pct: 0.85}` — deterministic load

**VM data generates these CSV files:**
- `ocp_ros_vm_usage` — VM-level metrics (CPU, memory, disk, network, GPU)
- `ocp_ros_vm_gpu_device` — per-GPU-device metrics for VMs with GPUs

**Critical:** Without `OCPVirtualMachineGenerator` blocks, nise generates NO
VM data and the benchmark will have zero VM recommendations.

## Recommended workload mix for comprehensive benchmarks

For a benchmark that exercises ALL recommendation engines:

| Component | Count | Generator | Exercises |
|-----------|-------|-----------|-----------|
| Regular containers | 19,000 | `OCPGenerator` | container, node, namespace, quota, PVC recs |
| GPU containers | 500 | `OCPGenerator` with `gpus:` | GPU time-slicing, MIG, idle detection |
| VMs | 500 | `OCPVirtualMachineGenerator` | VM rightsizing, idle, crash-loop, GPU passthrough |
| **Total workloads** | **20,000** | | **All 8 recommendation engines** |

Mix GPU models: ~40% T4 (time-slicing), ~30% A100 (MIG), ~20% H100 (MIG),
~10% L40S/L4. Mix VM types: ~60% Linux, ~30% Windows, ~10% idle/abandoned.

## Tarball format (three strict requirements)

1. **Manifest must be named `manifest.json`** inside the tarball. Not
   `manifest-00.json`, not `chunk_manifest.json`. Use `tar --transform`.

2. **The manifest `uuid` field must be a valid UUID.** The Koku listener's
   Pydantic model validates it. Use `str(uuid.uuid4())`.

3. **No `./` prefix on filenames.** If creating tarballs with `tar czf . ...`,
   add `--transform='s|^\./||'` to strip the prefix.

Failure to follow any of these causes the listener to silently reject the data.

## Ingress upload requirements

1. **`x-rh-identity` header is mandatory** — base64-encoded JSON with
   `account_number`, `org_id`, and `entitlements.cost_management.is_entitled: true`.

2. **NetworkPolicy blocks the nise pod by default.** Patch it:
   ```bash
   oc patch networkpolicy cost-onprem-ingress -n cost-onprem --type=json -p='[
     {"op": "add", "path": "/spec/ingress/0/from/-",
      "value": {"podSelector": {"matchLabels": {"app": "nise-generator"}}}}
   ]'
   ```
   And ensure the pod has `app: nise-generator` label.

3. **Content-Type must be `application/vnd.redhat.hccm.tar+tgz`** and the
   `type` form field must be `cost-mgmt`.

## Chunking strategy

The Koku listener processes one tarball at a time and has memory limits.
Split large datasets into chunks of ~30-35 CSV files per tarball.

For 10K containers × 30 days: ~295 CSV files → 11 chunks of ~33 files each.

Use a Python script inside the pod to create properly formatted chunks with
individual `manifest.json` files per chunk (each with a unique UUID).

## MinIO credentials (on-prem default)

```
endpoint: http://minio.cost-onprem.svc:9000
access_key: minioadmin
secret_key: minioadmin123
buckets: insights-upload-perma, koku-bucket, ros-data
```

## Monitoring

| What | How |
|------|-----|
| Listener progress | `oc logs -l app.kubernetes.io/component=listener --tail=50` |
| ROS processor | `oc logs -l app.kubernetes.io/component=ros-processor --tail=50` |
| Prometheus metrics | Port-forward processor pod port 9000, then `curl localhost:9000/metrics` |
| Digest count | `psql -d costonprem_ros -c "SELECT COUNT(*) FROM daily_container_digests"` |
| Recommendation count | `psql -d costonprem_ros -c "SELECT recommendation_type, COUNT(*) FROM container_recommendation_sets GROUP BY 1"` |
| DB size | `psql -d costonprem_ros -c "SELECT pg_size_pretty(pg_database_size('costonprem_ros'))"` |

## Common errors and fixes

| Error | Root cause | Fix |
|-------|-----------|-----|
| `'str' has no attribute 'get'` | Wrong nise YAML format | Use `OCPGenerator:` wrapper |
| `No manifest found in payload` | Manifest not named `manifest.json` | Use `tar --transform` |
| `uuid ... should be a valid UUID` | Non-UUID string in manifest | Use `uuid.uuid4()` |
| `ConnectTimeout` from nise pod | NetworkPolicy blocks access | Patch NetworkPolicy + label pod |
| `400: missing x-rh-identity` | No auth header | Add base64-encoded identity header |
| `unable to retrieve auth token` | podman push blob reuse bug | Use `skopeo copy` instead |
| `statement_timeout` | Large batch exceeds DB timeout | Set `ROS_DB_INGEST_STATEMENT_TIMEOUT=120` |
| 3h for 10K containers | Intermediate flush threshold cliff | Set `ROS_INGEST_FLUSH_BATCH_SIZE` to `math.MaxInt32` |

## Scaling estimates

| Workloads | PVC | nise time | CSV files | Chunks | Listener | ROS processor |
|-----------|-----|-----------|-----------|--------|----------|---------------|
| 4K containers | 15 GiB | ~10 min | ~120 | 4 | ~10 min | ~7.5 min |
| 10K containers | 30 GiB | ~25 min | ~295 | 11 | ~35 min | ~13 min |
| 20K mixed (19K+500 GPU+500 VM) | 60 GiB | ~50 min | ~600+ | 20 | ~70 min | ~25 min |
| 50K mixed | 150 GiB | ~2h | ~1,500 | 50 | ~3h | ~1h |
