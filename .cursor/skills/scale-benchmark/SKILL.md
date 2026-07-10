---
name: scale-benchmark
description: >-
  Run scale benchmarks of the ROS-OCP native engine on OpenShift clusters.
  Two modes: full pipeline (nise → ingress → listener → MinIO → ROS processor)
  for integration validation, or direct-to-MinIO (nise → MinIO → Kafka → ROS
  processor) for fast ROS-only benchmarks. Covers nise data generation on-cluster,
  tarball packaging, ingestion, and metric collection. Use when the user asks
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
   misleading results and can trigger duplicate-key errors. See
   [Cleanup commands](#cleanup-commands) for exact steps.

5. **Check cluster connectivity first.** Run `oc whoami` before any operation.
   If it fails, ask the user to reconnect sshuttle.

6. **Register the cluster source BEFORE uploading data.** The Koku listener
   silently rejects payloads from unregistered cluster UUIDs. See
   [Source registration](#source-registration).

7. **Use a consistent cluster UUID.** The UUID in the nise config, every
   chunk's `manifest.json`, and the Koku source registration MUST be
   identical. Generate one UUID at the start and reuse it everywhere.

## Pre-flight checklist

Run through this list IN ORDER before every benchmark. Skipping any step
risks a silent failure that wastes hours.

1. `oc whoami` — verify cluster connectivity
2. Decide on a cluster UUID: `CLUSTER_UUID=$(python3 -c "import uuid; print(uuid.uuid4())")`
3. **Verify and ensure sufficient disk space** (see [Space verification](#space-verification))
4. Clean MinIO buckets (see [Cleanup commands](#cleanup-commands))
5. Clean PostgreSQL databases (see [Cleanup commands](#cleanup-commands))
6. Build and deploy the `ros-ocp-backend` image (if code changed)
7. Register the cluster source (see [Source registration](#source-registration))
8. Verify source is registered: grep for the cluster UUID in `api_provider`
9. Patch NetworkPolicy to allow nise pod → ingress (see [Ingress upload requirements](#ingress-upload-requirements))
10. Generate nise data on-cluster
11. Upload data (direct-to-MinIO or via ingress with chunked tarballs)
12. If full pipeline: verify listener is ACCEPTING (not rejecting) the data
13. Monitor until complete
14. **Verify all entity types produced recommendations** (see [Post-benchmark entity verification](#post-benchmark-entity-verification))

## Space verification

**STOP and verify space BEFORE generating data.** Insufficient space causes
silent truncation or pod eviction mid-benchmark. Check all three storage
locations and compare against the required minimums.

### Required space by benchmark size

| Containers | Nise PVC | MinIO (`/data`) | PostgreSQL (ROS DB) |
|---|---|---|---|
| 4K | 15 GiB | 5 GiB free | 2 GiB free |
| 10K | 30 GiB | 10 GiB free | 5 GiB free |
| 20K mixed | 60 GiB | 20 GiB free | 10 GiB free |
| 50K mixed | 150 GiB | 50 GiB free | 25 GiB free |

### Check commands (run ALL three before proceeding)

```bash
# 1. Nise generator PVC — check current size and usage
NISE_POD=$(oc get pods -n cost-onprem -l app=nise-generator -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -z "$NISE_POD" ]; then
  echo "NISE POD: not created yet (will be created in step 3)"
  oc get pvc nise-generator-data -n cost-onprem -o jsonpath='PVC size: {.spec.resources.requests.storage}{"\n"}' 2>/dev/null || echo "PVC: not created yet"
else
  oc exec -n cost-onprem "$NISE_POD" -- df -h /data | tail -1
fi

# 2. MinIO — check free space on the data volume
oc exec -n cost-onprem deploy/minio -- df -h /data | tail -1

# 3. PostgreSQL — check database sizes and disk free space
DB_POD=$(oc get pods -n cost-onprem -l app.kubernetes.io/component=database -o jsonpath='{.items[0].metadata.name}')
oc exec -n cost-onprem "$DB_POD" -- bash -c '
  echo "=== Disk usage ==="
  df -h /var/lib/postgresql/data | tail -1
  echo "=== Database sizes ==="
  psql -U postgres -t -c "SELECT datname, pg_size_pretty(pg_database_size(datname)) FROM pg_database WHERE datname LIKE '\''costonprem%'\''"
'
```

### Remediation if space is insufficient

**Nise PVC too small** — resize it (StorageClass must support expansion):

```bash
# Example: resize to 60 GiB for a 20K benchmark
oc patch pvc nise-generator-data -n cost-onprem --type=merge \
  -p '{"spec":{"resources":{"requests":{"storage":"60Gi"}}}}'
```

**MinIO full** — clean old benchmark data (see [Cleanup commands](#cleanup-commands)).
If still insufficient after cleanup, resize the MinIO PVC.

**PostgreSQL full** — truncate old benchmark data (see [Cleanup commands](#cleanup-commands)).
If the underlying PV is full, resize the database PVC.

**Do NOT proceed to data generation until all three locations meet the
minimums.** A mid-benchmark disk-full failure wastes the entire run.

## Detailed runbook

Read [scale-benchmark-runbook.md](../../../docs-site/operations/scale-benchmark-runbook.md)
for the full step-by-step procedure with copy-pasteable commands.

## Two benchmark modes

### Mode 1: Full pipeline (default, for integration validation)

```
nise (CSV) → tar.gz → ingress service → Koku listener → MinIO → Koku worker
                                                                      ↓
Prometheus ← ros-ocp-backend ← S3 notification ← MinIO/Kafka
```

Data flows through the full Cost Management pipeline. The Koku listener is
the dominant bottleneck at 10K+ containers:

| Benchmark | Listener time | ROS processor time |
|---|---|---|
| 4K | ~20-30 min | ~7.5 min |
| 10K | ~2-3 hours | ~13 min |
| 20K | ~6.5 hours | ~25 min |

Use this mode for first-time validation and when testing listener changes.

### Mode 2: Direct-to-MinIO (fast, for ROS processor benchmarks)

```
nise (CSV) → MinIO (ros-data bucket) → Kafka message → ROS processor
             ~~~minutes~~~                              ~~~same as above~~~
```

Bypasses the Koku listener entirely. The listener does NOT transform ROS
CSVs — it just moves them to MinIO and sends a Kafka notification. We can
do both of those directly, saving hours of listener processing time.

**When to use**: ROS processor performance benchmarks, rapid iteration on
ROS optimizations, large-scale testing (50K+ containers).

**Steps for direct-to-MinIO mode:**

1. Generate nise data on-cluster (same as full pipeline)
2. Install deps in the nise-generator pod: `pip install boto3 kafka-python`
3. Copy the upload script: `oc cp scripts/direct_to_minio.py cost-onprem/<pod>:/data/direct_to_minio.py`
4. Get MinIO credentials:
   ```bash
   export MINIO_ACCESS_KEY=$(oc get secret cost-onprem-storage-credentials \
     -n cost-onprem -o jsonpath='{.data.access-key}' | base64 -d)
   export MINIO_SECRET_KEY=$(oc get secret cost-onprem-storage-credentials \
     -n cost-onprem -o jsonpath='{.data.secret-key}' | base64 -d)
   ```
5. Run the script from the pod:
   ```bash
   oc exec -n cost-onprem <nise-pod> -- bash -c "
     export MINIO_ACCESS_KEY='$MINIO_ACCESS_KEY'
     export MINIO_SECRET_KEY='$MINIO_SECRET_KEY'
     python3 /data/direct_to_minio.py \
       --data-dir /data/nise_output \
       --cluster-uuid \$CLUSTER_UUID \
       --cluster-alias bench-direct \
       --provider-uuid \$PROVIDER_UUID
   "
   ```

The script (`scripts/direct_to_minio.py`) handles all three operations:
- Uploads ROS CSVs to MinIO `ros-data` bucket
- Generates presigned URLs (48h expiry)
- Publishes Kafka messages to `hccm.ros.events`

**Infrastructure details** (on-prem cost-onprem chart defaults):
- MinIO: `http://minio.cost-onprem.svc.cluster.local:9000`
- Kafka: `cost-onprem-kafka-kafka-bootstrap.kafka.svc.cluster.local:9092`
  (PLAINTEXT, no TLS/SASL)
- Bucket: `ros-data`
- Topic: `hccm.ros.events`
- Credentials: `cost-onprem-storage-credentials` secret (`access-key` / `secret-key`)

**No Koku source registration needed** for direct-to-MinIO mode — the ROS
processor auto-creates `rh_accounts` and `clusters` entries. However, you
still need a registered source if you also want cost data.

**Dry-run mode**: Add `--dry-run` to see what would be uploaded without
actually uploading or publishing messages.

See GitHub issue [#268](https://github.com/pgarciaq/ros-ocp-backend/issues/268)
and `scripts/direct_to_minio.py` for full details.

## Source registration

**This step is MANDATORY before uploading any data.** The Koku listener
checks every incoming payload against registered sources. If the cluster
UUID is not registered, the listener logs `Received unexpected OCP report
from <uuid>` and silently discards the entire payload. There is no retry —
the data is gone.

```bash
# Choose a UUID once and reuse everywhere
CLUSTER_UUID=$(python3 -c "import uuid; print(uuid.uuid4())")
echo "Cluster UUID: $CLUSTER_UUID"

# Build the identity header
IDENTITY=$(echo -n '{"identity":{"account_number":"10001","org_id":"1234567","type":"User","user":{"username":"admin","email":"admin@example.com","is_org_admin":true}},"entitlements":{"cost_management":{"is_entitled":true}}}' | base64 -w0)

# Register the source (run from inside the cluster via oc exec)
KOKU_POD=$(oc get pods -n cost-onprem -l app.kubernetes.io/component=cost-api -o jsonpath='{.items[0].metadata.name}')
oc exec -n cost-onprem "$KOKU_POD" -c koku-api -- curl -s -X POST \
  -H "x-rh-identity: $IDENTITY" \
  -H "Content-Type: application/json" \
  -d "{\"name\":\"bench-${CLUSTER_UUID:0:8}\",\"source_type\":\"OCP\",\"authentication\":{\"credentials\":{\"cluster_id\":\"$CLUSTER_UUID\"}},\"billing_source\":{\"data_source\":{}}}" \
  "http://localhost:8000/api/cost-management/v1/sources/"
```

**Verify registration:**

```bash
DB_POD=$(oc get pods -n cost-onprem -l app.kubernetes.io/component=database -o jsonpath='{.items[0].metadata.name}')
oc exec -n cost-onprem "$DB_POD" -- psql -U koku -d costonprem_koku -t -c \
  "SELECT uuid::text, name, credentials FROM api_provider p JOIN api_providerauthentication a ON p.authentication_id = a.id WHERE type = 'OCP' ORDER BY name"
```

**If you uploaded data before registering**: The data was discarded. You must
re-upload ALL tarballs after registration.

**Common field name mistake**: The API uses `source_type`, not `type`. Using
`type` returns `400: Either source_type or source_type_id is required`.

## Cleanup commands

### MinIO cleanup

Three buckets must be cleaned. Use `mc` from a MinIO client container or
from any pod with `boto3`:

```bash
# From within the cluster (e.g. nise-generator pod with boto3)
python3 -c "
import boto3
s3 = boto3.client('s3', endpoint_url='http://minio.cost-onprem.svc:9000',
    aws_access_key_id='minioadmin', aws_secret_access_key='minioadmin123')
for bucket in ['insights-upload-perma', 'koku-bucket', 'ros-data']:
    try:
        objs = s3.list_objects_v2(Bucket=bucket).get('Contents', [])
        for obj in objs:
            s3.delete_object(Bucket=bucket, Key=obj['Key'])
        print(f'{bucket}: deleted {len(objs)} objects')
    except Exception as e:
        print(f'{bucket}: {e}')
"
```

### PostgreSQL cleanup

There are TWO databases on the on-prem cluster:

| Database | User | Contains |
|----------|------|----------|
| `costonprem_koku` | `koku` | Koku sources, providers, manifests, cost data |
| `costonprem_ros` | `ros_user` | ROS digests, recommendations, quality metrics |

**Clean ROS database** (most common — removes all digests and recommendations):

```bash
DB_POD=$(oc get pods -n cost-onprem -l app.kubernetes.io/component=database -o jsonpath='{.items[0].metadata.name}')
oc exec -n cost-onprem "$DB_POD" -- psql -U ros_user -d costonprem_ros -c "
TRUNCATE
  daily_container_digests, daily_namespace_digests, daily_node_digests,
  gpu_container_digests, daily_pvc_digests, daily_cluster_quota_digests,
  daily_namespace_quota_digests,
  recommendation_sets, recommendation_history, recommendation_quality,
  historical_recommendation_sets, historical_namespace_recommendation_sets,
  namespace_recommendation_sets,
  node_recommendations, node_gpu_timeslicing_recommendations, node_gpu_timeslicing_recommendation_history,
  cluster_quota_recommendation_sets, cluster_quota_recommendation_history,
  quota_recommendation_sets, quota_recommendation_history,
  pvc_recommendation_sets, pvc_recommendation_quality,
  gpu_mig_recommendation_sets, gpu_mig_recommendation_quality,
  vm_recommendations, vm_recommendation_history, vm_recommendation_quality,
  snapshot_recommendation_sets, snapshot_recommendation_quality,
  org_recommendation_stats, org_recommendation_terms,
  cluster_threshold_recalc_state
  CASCADE;
"
```

**Clean Koku manifest data** (optional — only if re-ingesting same date range):

```bash
oc exec -n cost-onprem "$DB_POD" -- psql -U koku -d costonprem_koku -c "
TRUNCATE reporting_common_costusagereportmanifest CASCADE;
"
```

**WARNING**: Do NOT truncate `api_provider` or `api_sources` — these contain
the source registrations you just created.

## PVC resizing

If the nise-generator PVC is too small for the target workload:

```bash
# Check current size
oc get pvc nise-generator-data -n cost-onprem -o jsonpath='{.spec.resources.requests.storage}'

# Resize (StorageClass must support expansion — lvms-vg1 does)
oc patch pvc nise-generator-data -n cost-onprem --type=merge \
  -p '{"spec":{"resources":{"requests":{"storage":"60Gi"}}}}'

# Verify expansion (may take a few seconds)
oc get pvc nise-generator-data -n cost-onprem
```

**Sizing guide:**

| Workloads | Minimum PVC | Recommended PVC |
|-----------|-------------|-----------------|
| 4K containers | 15 GiB | 20 GiB |
| 10K containers | 30 GiB | 40 GiB |
| 20K mixed | 50 GiB | 60 GiB |
| 50K mixed | 130 GiB | 150 GiB |

## Comprehensive nise configuration

### Automated config generator (recommended)

For benchmarks that exercise ALL recommendation engines, use the comprehensive
config generator script (`scripts/gen_benchmark_config.py`):

```bash
# Copy to the nise pod
oc cp scripts/gen_benchmark_config.py cost-onprem/$NISE_POD:/data/gen_benchmark_config.py

# Generate a 20K mixed workload config
oc exec -n cost-onprem $NISE_POD -- python3 /data/gen_benchmark_config.py \
  --containers 20000 \
  --start-date 2026-07-01 --end-date 2026-07-31 \
  --output /data/bench_config.yml
```

The generator automatically includes proportional counts of:

| Entity type | Default % of total | Generator |
|---|---|---|
| Regular containers | ~94.5% | `OCPGenerator` pods |
| Idle/zombie containers | ~3% | `OCPGenerator` pods (near-zero usage) |
| GPU time-slicing | ~2% | `OCPGenerator` pods with `gpus:` |
| GPU MIG | ~0.5% | `OCPGenerator` pods with `mig_instances` |
| VMs (Linux, Windows, idle, abandoned) | ~2.5% | `OCPVirtualMachineGenerator` |
| VM GPUs (passthrough, MIG) | ~30% of VMs | `OCPVirtualMachineGenerator` with GPU |
| PVCs (oversized, near-full, orphaned, healthy) | ~10% of namespaces | `volumes:` blocks |
| Snapshots (stale, orphaned) | 2 per PVC namespace | `snapshots:` blocks |
| Namespace quotas | ~15% of namespaces | `resource_quota:` blocks |
| Cluster quotas | 2 (fixed) | `cluster_resource_quotas:` blocks |

Business hours are implicit — ros-ocp-backend classifies intervals by timestamp
when `ROS_BUSINESS_HOURS_ENABLED=true`.

### Manual YAML format

The YAML MUST use nise's `OCPGenerator` format (and optionally
`OCPVirtualMachineGenerator` for VMs). This is the basic structure:

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
- Missing `OCPVirtualMachineGenerator` block → zero VM data
- No `gpus:` blocks on pods → zero GPU container recommendations
- No `volumes:` blocks → zero PVC recommendations
- No `snapshots:` blocks → zero snapshot recommendations
- No `resource_quota:` under namespaces → zero namespace quota recommendations
- No `cluster_resource_quotas:` → zero cluster quota recommendations

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

## Ingress service details (on-prem default)

```
service: cost-onprem-ingress.cost-onprem.svc.cluster.local
port: 8081  (NOT 3000 — check with: oc get svc -n cost-onprem | grep ingress)
upload URL: http://cost-onprem-ingress.cost-onprem.svc.cluster.local:8081/api/ingress/v1/upload
```

**IMPORTANT**: The ingress port varies by deployment. Always verify with
`oc get svc` before uploading. Using the wrong port causes curl to return
HTTP 000 (connection refused) and no data is ingested.

## Monitoring

| What | How |
|------|-----|
| Listener progress | `oc logs -l app.kubernetes.io/component=listener --tail=50` |
| ROS processor | `oc logs -l app.kubernetes.io/component=ros-processor --tail=50` |
| Prometheus metrics | Port-forward processor pod port 9000, then `curl localhost:9000/metrics` |
| Digest count | `oc exec -n cost-onprem $DB_POD -- psql -U ros_user -d costonprem_ros -t -c "SELECT COUNT(*) FROM daily_container_digests"` |
| Recommendation count | `oc exec -n cost-onprem $DB_POD -- psql -U ros_user -d costonprem_ros -t -c "SELECT COUNT(*) FROM recommendation_sets"` |
| DB size | `oc exec -n cost-onprem $DB_POD -- psql -U ros_user -d costonprem_ros -t -c "SELECT pg_size_pretty(pg_database_size('costonprem_ros'))"` |
| GPU/VM digests | `oc exec -n cost-onprem $DB_POD -- psql -U ros_user -d costonprem_ros -t -c "SELECT 'gpu: ' \|\| COUNT(*) FROM gpu_container_digests UNION ALL SELECT 'vm: ' \|\| COUNT(*) FROM vm_recommendations"` |

### Detecting rejected uploads

**Always check this after the first upload.** If the source wasn't registered,
ALL uploads are silently rejected:

```bash
# Count rejected vs accepted
oc logs -n cost-onprem -l app.kubernetes.io/component=listener --tail=5000 | grep -c "unexpected OCP report"
oc logs -n cost-onprem -l app.kubernetes.io/component=listener --tail=5000 | grep -c "Extracting Payload"
```

If the "unexpected" count equals the "Extracting" count, ALL chunks were
rejected. Register the source and re-upload everything.

## Post-benchmark entity verification

After the ROS processor finishes, verify that ALL entity types produced non-zero
recommendations. Zero recommendations for any entity type means data was not
generated or not ingested properly.

```bash
DB_POD=$(oc get pods -n cost-onprem -l app.kubernetes.io/component=database -o jsonpath='{.items[0].metadata.name}')

oc exec -n cost-onprem "$DB_POD" -- psql -U ros_user -d costonprem_ros -t -c "
SELECT 'container_digests' AS entity, COUNT(*) FROM daily_container_digests
UNION ALL SELECT 'container_recs', COUNT(*) FROM recommendation_sets
UNION ALL SELECT 'namespace_digests', COUNT(*) FROM daily_namespace_digests
UNION ALL SELECT 'namespace_recs', COUNT(*) FROM namespace_recommendation_sets
UNION ALL SELECT 'node_recs', COUNT(*) FROM node_recommendations
UNION ALL SELECT 'pvc_digests', COUNT(*) FROM daily_pvc_digests
UNION ALL SELECT 'pvc_recs', COUNT(*) FROM pvc_recommendation_sets
UNION ALL SELECT 'gpu_digests', COUNT(*) FROM gpu_container_digests
UNION ALL SELECT 'gpu_mig_recs', COUNT(*) FROM gpu_mig_recommendation_sets
UNION ALL SELECT 'gpu_ts_recs', COUNT(*) FROM node_gpu_timeslicing_recommendations
UNION ALL SELECT 'cluster_quota_digests', COUNT(*) FROM daily_cluster_quota_digests
UNION ALL SELECT 'cluster_quota_recs', COUNT(*) FROM cluster_quota_recommendation_sets
UNION ALL SELECT 'ns_quota_recs', COUNT(*) FROM quota_recommendation_sets
UNION ALL SELECT 'vm_recs', COUNT(*) FROM vm_recommendations
UNION ALL SELECT 'snapshot_recs', COUNT(*) FROM snapshot_recommendation_sets
UNION ALL SELECT 'snapshot_inventory', COUNT(*) FROM snapshot_inventory
ORDER BY 1;
"
```

**Expected non-zero counts for a comprehensive benchmark:**

| Entity | Digest table | Recommendation table | Zero means... |
|---|---|---|---|
| Containers | `daily_container_digests` | `recommendation_sets` | Missing `--ros-ocp-info` flag |
| Namespaces | `daily_namespace_digests` | `namespace_recommendation_sets` | Missing `--ros-ocp-info` |
| Nodes | — | `node_recommendations` | No container data (nodes inferred) |
| PVCs | `daily_pvc_digests` | `pvc_recommendation_sets` | No `volumes:` in YAML |
| GPU (TS) | `gpu_container_digests` | `node_gpu_timeslicing_recommendations` | No `gpus:` blocks (without `mig_instances`) |
| GPU (MIG) | `gpu_container_digests` | `gpu_mig_recommendation_sets` | No `mig_instances` in `gpus:` blocks |
| Cluster quota | `daily_cluster_quota_digests` | `cluster_quota_recommendation_sets` | No `cluster_resource_quotas:` in YAML |
| Namespace quota | — | `quota_recommendation_sets` | No `resource_quota:` in namespace |
| VMs | — | `vm_recommendations` | Missing `OCPVirtualMachineGenerator` block |
| Snapshots | `snapshot_inventory` | `snapshot_recommendation_sets` | No `snapshots:` in YAML |

If any count is zero when it should not be, check:
1. Was `--ros-ocp-info` passed to nise? (required for all ROS CSVs)
2. Does the YAML include the correct generator block for that entity type?
3. Did the ROS processor logs show errors for that CSV type?
4. For direct-to-MinIO mode: were all ROS CSV files found and uploaded?

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
| `Received unexpected OCP report` | Cluster UUID not registered as source | Register source BEFORE uploading (see [Source registration](#source-registration)) |
| `source_type or source_type_id is required` | Used `type` instead of `source_type` in API | Use `source_type: "OCP"` in POST body |
| `relation "api_provider" does not exist` | Wrong database name | On-prem uses `costonprem_koku` (user: `koku`), not `postgres` |
| All chunks rejected, DB stays empty | Uploaded before registering source | Register source, then re-upload ALL tarballs |

## Scaling estimates

| Workloads | PVC | nise time | CSV files | Chunks | Listener | ROS processor |
|-----------|-----|-----------|-----------|--------|----------|---------------|
| 4K containers | 15 GiB | ~10 min | ~120 | 4 | ~10 min | ~7.5 min |
| 10K containers | 30 GiB | ~25 min | ~295 | 11 | ~35 min | ~13 min |
| 20K mixed (19K+500 GPU+500 VM) | 60 GiB | ~50 min | ~600+ | 20 | ~70 min | ~25 min |
| 50K mixed | 150 GiB | ~2h | ~1,500 | 50 | ~3h | ~1h |
