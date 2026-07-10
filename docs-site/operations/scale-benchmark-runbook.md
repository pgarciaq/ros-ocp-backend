# Scale Benchmark Runbook

> **Last updated:** 2026-07-10
> **Target cluster:** Any OpenShift cluster running cost-onprem (tested on dell-r640-082 SNO)

This runbook describes how to run a scale benchmark of the ROS-OCP native engine
at 4K–10K+ containers on an OpenShift cluster. It covers data generation, ingestion,
monitoring, and metric collection.

!!! info "Data flow"
    Generated data flows through the **full Cost Management pipeline**, not directly
    to `ros-ocp-backend`:

    ```
    nise (CSV) → tar.gz → ingress service → Koku listener → MinIO → Koku worker
                                                                          ↓
    Prometheus ← ros-ocp-backend (processor) ← S3 notification ← MinIO/Kafka
    ```

    The ROS processor receives data only after the Koku listener has parsed CSVs,
    written Parquet files, and stored ROS CSVs in MinIO.

---

## Prerequisites

| Requirement | Details |
|-------------|---------|
| OpenShift cluster | With cost-onprem Helm chart deployed |
| `oc` CLI | Authenticated to the cluster |
| Namespace | `cost-onprem` (default) |
| Storage | LVMS or other StorageClass with ≥30 GiB free |
| MinIO | Deployed and accessible within the cluster |
| Network access | sshuttle or VPN to reach the cluster API |

## Step 0: Pre-flight checks

```bash
# Verify cluster access
oc whoami
oc get pods -n cost-onprem | head -20

# Check available storage
oc get pvc -n cost-onprem

# Check MinIO has space
oc exec -n cost-onprem deploy/minio -- df -h /data
```

---

## Step 1: Clean up previous benchmark data

Before each benchmark run, clean both MinIO and PostgreSQL to ensure fresh results.

### Clean MinIO

```bash
# Check current bucket sizes
oc exec -n cost-onprem deploy/minio -- sh -c '
  for b in insights-upload-perma koku-bucket ros-data; do
    size=$(du -sh /data/$b 2>/dev/null | cut -f1)
    echo "$b: ${size:-empty}"
  done
'

# Clear benchmark data from buckets (keeps buckets intact)
oc exec -n cost-onprem deploy/minio -- sh -c '
  rm -rf /data/insights-upload-perma/*
  rm -rf /data/koku-bucket/*
  rm -rf /data/ros-data/*
'
```

### Clean PostgreSQL (ROS database)

```bash
# Find the database pod
DB_POD=$(oc get pods -n cost-onprem -l app.kubernetes.io/component=database -o name | head -1)

# Find the ROS database name (usually costonprem_ros)
oc exec -n cost-onprem $DB_POD -- psql -U postgres -l | grep ros

# Truncate all ROS tables
oc exec -n cost-onprem $DB_POD -- psql -U postgres -d costonprem_ros -c "
DO \$\$
DECLARE r RECORD;
BEGIN
  FOR r IN SELECT tablename FROM pg_tables WHERE schemaname = 'public'
    AND tablename NOT LIKE 'pg_%' AND tablename NOT LIKE 'sql_%'
  LOOP
    EXECUTE 'TRUNCATE TABLE ' || quote_ident(r.tablename) || ' CASCADE';
  END LOOP;
END\$\$;
"
```

---

## Step 2: Build and deploy ros-ocp-backend

Build and deploy the version you want to benchmark:

```bash
cd ~/dev/koku/ros-ocp-backend

# Use a unique tag (never reuse tags — imagePullPolicy: IfNotPresent)
TAG="bench-$(date -u +%Y%m%d%H%M)"

# Build (match cluster architecture — check with: oc get nodes -o wide)
podman build -t ros-ocp-backend:$TAG -f Dockerfile .

# Push to cluster registry (requires sshuttle + image-pusher SA)
REGISTRY="default-route-openshift-image-registry.apps.<cluster>.karmalabs.corp:443/cost-onprem"
TOKEN=$(oc create token image-pusher -n cost-onprem --duration=1h)
podman login $REGISTRY -u image-pusher -p "$TOKEN" --tls-verify=false
podman tag ros-ocp-backend:$TAG $REGISTRY/ros-ocp-backend:$TAG
podman push --tls-verify=false $REGISTRY/ros-ocp-backend:$TAG

# If podman push fails with "unable to retrieve auth token", use skopeo:
skopeo copy --dest-tls-verify=false --dest-creds "image-pusher:${TOKEN}" \
  containers-storage:localhost/ros-ocp-backend:${TAG} \
  docker://${REGISTRY}/ros-ocp-backend:${TAG}

# Deploy to processor and API
INTERNAL="image-registry.openshift-image-registry.svc:5000/cost-onprem"
oc set image deployment/cost-onprem-ros-processor ros-processor=$INTERNAL/ros-ocp-backend:$TAG -n cost-onprem
oc set image deployment/cost-onprem-ros-api ros-api=$INTERNAL/ros-ocp-backend:$TAG -n cost-onprem
oc rollout status deployment/cost-onprem-ros-processor -n cost-onprem --timeout=120s
oc rollout status deployment/cost-onprem-ros-api -n cost-onprem --timeout=120s
```

---

## Step 3: Create a nise data generator pod

!!! warning "Generate data on-cluster, NOT on your laptop"
    Transferring multi-gigabyte tarballs over sshuttle will fail. Generate data
    directly on the cluster using a dedicated pod.

### Create a PVC for data storage

```yaml
# /tmp/nise-generator-pvc.yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: nise-generator-data
  namespace: cost-onprem
spec:
  accessModes:
    - ReadWriteOnce
  storageClassName: lvms-vg1  # adjust for your cluster
  resources:
    requests:
      storage: 30Gi  # 10K containers × 30 days ≈ 6 GiB compressed
```

```bash
oc apply -f /tmp/nise-generator-pvc.yaml
```

### Create the generator pod

```yaml
# /tmp/nise-generator-pod.yaml
apiVersion: v1
kind: Pod
metadata:
  name: nise-generator
  namespace: cost-onprem
  labels:
    app: nise-generator  # needed for NetworkPolicy (see Step 5)
spec:
  restartPolicy: Never
  containers:
  - name: nise
    image: registry.access.redhat.com/ubi9/python-311:latest
    command: ["sleep", "infinity"]
    resources:
      requests:
        cpu: "2"
        memory: "4Gi"
      limits:
        cpu: "4"
        memory: "8Gi"
    volumeMounts:
    - name: data
      mountPath: /data
  volumes:
  - name: data
    persistentVolumeClaim:
      claimName: nise-generator-data
```

```bash
oc apply -f /tmp/nise-generator-pod.yaml
oc wait pod/nise-generator -n cost-onprem --for=condition=Ready --timeout=120s
```

### Install nise

```bash
# Install from the fork with optimizations (or use upstream: koku-nise)
oc exec -n cost-onprem nise-generator -- pip install \
  'koku-nise @ git+https://github.com/pgarciaq/nise@pgarciaq-rosocp-superpowers-phase15'
```

---

## Step 4: Generate benchmark data

### Generate the nise configuration YAML

The YAML must follow nise's `OCPGenerator` format exactly. Here's a Python script
that generates a correct 10K-container configuration:

```bash
oc exec -n cost-onprem nise-generator -- python3 -c "
import random
random.seed(42)

CONTAINERS = 10000
NAMESPACES = 150
NODES = 15
PODS_PER_NS = CONTAINERS // NAMESPACES  # 66-67

nodes = [f'bench-node-{i:03d}' for i in range(NODES)]

lines = ['---', 'generators:']
total = 0
for ns_idx in range(NAMESPACES):
    ns = f'bench-ns-{ns_idx:04d}'
    # Distribute evenly: first 100 ns get 67, rest get 66
    pods = PODS_PER_NS + (1 if ns_idx < CONTAINERS % NAMESPACES else 0)
    node = nodes[ns_idx % NODES]

    lines.append(f'  - OCPGenerator:')
    lines.append(f'      start_date: 2026-06-01')
    lines.append(f'      end_date: 2026-06-30')
    lines.append(f'      nodes:')
    lines.append(f'        - node:')
    lines.append(f'          node_name: {node}')
    lines.append(f'          cpu_cores: 32')
    lines.append(f'          memory_gig: 128')
    lines.append(f'          namespaces:')
    lines.append(f'            {ns}:')
    lines.append(f'              pods:')

    for p in range(pods):
        cpu_req = random.randint(50, 500)
        cpu_lim = random.randint(500, 2000)
        mem_req = round(random.uniform(0.1, 2.0), 1)
        mem_lim = round(random.uniform(2.0, 8.0), 1)
        lines.append(f'                - pod:')
        lines.append(f'                  pod_name: {ns}-pod-{p:03d}')
        lines.append(f'                  cpu_request: {cpu_req}')
        lines.append(f'                  cpu_limit: {cpu_lim}')
        lines.append(f'                  mem_request_gig: {mem_req}')
        lines.append(f'                  mem_limit_gig: {mem_lim}')
        lines.append(f'                  labels: label_app:{ns}|label_version:v1')
        total += 1

with open('/data/bench_config.yml', 'w') as f:
    f.write('\n'.join(lines) + '\n')

print(f'Config written: {total} containers, {NAMESPACES} namespaces, {NODES} nodes')
"
```

### Run nise

```bash
# Generate data (runs in the background — takes 15-30 min for 10K × 30 days)
oc exec -n cost-onprem nise-generator -- bash -c '
  cd /data &&
  nise report ocp \
    --static-report-file /data/bench_config.yml \
    --ocp-cluster-id bench-cluster-10k \
    --ros-ocp-info \
    -w > /data/nise.log 2>&1 &
  echo "nise started in background (PID: $!)"
'

# Monitor progress
oc exec -n cost-onprem nise-generator -- tail -5 /data/nise.log

# Check when done (look for the manifest.json)
oc exec -n cost-onprem nise-generator -- ls -la /data/manifest.json
```

!!! warning "nise YAML format pitfalls"
    - Each namespace must be inside an `OCPGenerator:` block with `nodes:` → `namespaces:` hierarchy
    - The `node:` field must be at the same indentation as `node_name:`
    - Pod names should be unique across the entire cluster
    - `--ros-ocp-info` is required to generate ROS container-level CSVs
    - `-w` (write-monthly) organizes output properly; do NOT use `--daily-reports`

---

## Step 5: Package and upload data

### Chunk the data into manageable tarballs

The Koku listener has memory limits and processes one tarball at a time. Split
the data into chunks of ~30-35 CSV files per tarball:

```bash
oc exec -n cost-onprem nise-generator -- python3 -c "
import json, os, subprocess, uuid

os.chdir('/data')

with open('manifest.json') as f:
    manifest = json.load(f)

all_files = manifest['files'] + manifest['resource_optimization_files']
ros_set = set(manifest['resource_optimization_files'])

CHUNK_SIZE = 33
chunks = [all_files[i:i+CHUNK_SIZE] for i in range(0, len(all_files), CHUNK_SIZE)]
print(f'Creating {len(chunks)} tarballs from {len(all_files)} files...')

for f in os.listdir('.'):
    if f.startswith('bench-') and f.endswith('.tar.gz'):
        os.remove(f)

for i, chunk_files in enumerate(chunks):
    chunk_manifest = {
        'uuid': str(uuid.uuid4()),
        'cluster_id': manifest['cluster_id'],
        'version': manifest['version'],
        'date': manifest['date'],
        'start': manifest['start'],
        'end': manifest['end'],
        'files': [f for f in chunk_files if f not in ros_set],
        'resource_optimization_files': [f for f in chunk_files if f in ros_set],
    }

    with open('chunk_manifest.json', 'w') as f:
        json.dump(chunk_manifest, f, indent=2)

    tarball = f'bench-{i:02d}.tar.gz'
    cmd = ['tar', 'czf', tarball,
           '--transform', 's/chunk_manifest.json/manifest.json/',
           'chunk_manifest.json'] + chunk_files
    result = subprocess.run(cmd, capture_output=True, text=True)
    if result.returncode != 0:
        print(f'ERROR chunk {i}: {result.stderr}')
        break

    size = os.path.getsize(tarball) / (1024*1024)
    print(f'  Chunk {i:02d}: {len(chunk_files)} files, {size:.0f} MB')

os.remove('chunk_manifest.json')
print('Done')
"
```

!!! danger "Critical: tarball format requirements"
    Three things **must** be correct or the Koku listener will reject the data:

    1. **Manifest filename must be `manifest.json`** — not `manifest-00.json`, not
       `chunk_manifest.json`. Use `tar --transform` to rename it inside the archive.
    2. **The `uuid` field must be a valid UUID** — the listener's Pydantic model
       validates it. Use `uuid.uuid4()`, not an arbitrary string.
    3. **No `./` prefix on filenames** — use `tar --transform='s|^\./||'` if needed.

### Upload tarballs to MinIO

```bash
oc exec -n cost-onprem nise-generator -- python3 -c "
import boto3, os, time

s3 = boto3.client('s3',
    endpoint_url='http://minio.cost-onprem.svc:9000',
    aws_access_key_id='minioadmin',
    aws_secret_access_key='minioadmin123',
    region_name='us-east-1')

bucket = 'insights-upload-perma'

tarballs = sorted(f for f in os.listdir('/data') if f.startswith('bench-') and f.endswith('.tar.gz'))
print(f'Uploading {len(tarballs)} tarballs to MinIO...')

for tb in tarballs:
    path = f'/data/{tb}'
    size = os.path.getsize(path) / (1024*1024)
    print(f'  {tb} ({size:.0f} MB)...', flush=True)
    start = time.time()
    s3.upload_file(path, bucket, tb)
    print(f'    Done in {time.time()-start:.1f}s')

print('All uploads complete')
"
```

### Patch NetworkPolicy for ingress access

The ingress service has a NetworkPolicy that restricts access. The nise pod
needs to be allowed through:

```bash
# Add a rule allowing the nise-generator pod
oc patch networkpolicy cost-onprem-ingress -n cost-onprem --type=json -p='[
  {"op": "add", "path": "/spec/ingress/0/from/-",
   "value": {"podSelector": {"matchLabels": {"app": "nise-generator"}}}}
]'

# Verify the label is on the pod
oc get pod nise-generator -n cost-onprem --show-labels | grep app=nise-generator
```

### Submit tarballs via the ingress service

```bash
oc exec -n cost-onprem nise-generator -- python3 -c "
import requests, os, json, time, base64, glob

INGRESS = 'http://cost-onprem-ingress.cost-onprem.svc:8081/api/ingress/v1/upload'

identity = base64.b64encode(json.dumps({
    'identity': {
        'account_number': '10001', 'org_id': '1234567', 'type': 'User',
        'user': {'username': 'admin', 'email': 'admin@example.com', 'is_org_admin': True}
    },
    'entitlements': {'cost_management': {'is_entitled': True}}
}).encode()).decode()

tarballs = sorted(glob.glob('/data/bench-*.tar.gz'))
print(f'Submitting {len(tarballs)} chunks to ingress...')

for tb in tarballs:
    name = os.path.basename(tb)
    size = os.path.getsize(tb) / (1024*1024)
    print(f'  {name} ({size:.0f} MB)...', flush=True)
    start = time.time()
    with open(tb, 'rb') as f:
        resp = requests.post(INGRESS,
            files={'file': (name, f, 'application/vnd.redhat.hccm.tar+tgz')},
            data={'type': 'cost-mgmt'},
            headers={'x-rh-identity': identity},
            timeout=600)
    print(f'    Status: {resp.status_code} ({time.time()-start:.1f}s)')
    if resp.status_code not in (200, 202):
        print(f'    ERROR: {resp.text[:300]}')
        break
    time.sleep(5)

print('All chunks submitted')
"
```

!!! danger "Critical: ingress requirements"
    1. **`x-rh-identity` header is required** — the ingress service rejects requests
       without it, even in on-prem mode.
    2. **Content-Type must be `application/vnd.redhat.hccm.tar+tgz`** — other types
       are rejected.
    3. **`type` form field must be `cost-mgmt`** — this routes the upload to the
       Cost Management pipeline.

---

## Step 6: Monitor ingestion

### Watch the Koku listener

The listener processes tarballs first (CSV parsing, Parquet conversion). This is
typically the slowest phase (~35 min for 10K containers):

```bash
# Watch for processing progress
oc logs -n cost-onprem -l app.kubernetes.io/component=listener --tail=50

# Look for completion markers
oc logs -n cost-onprem -l app.kubernetes.io/component=listener --tail=200 | \
  grep -E 'manifest|complete|error|failed'
```

### Watch the ROS processor

Once the listener finishes, data flows to the ROS processor:

```bash
# Watch processor logs for digest creation
oc logs -n cost-onprem -l app.kubernetes.io/component=ros-processor --tail=50

# Look for benchmark-specific indicators
oc logs -n cost-onprem -l app.kubernetes.io/component=ros-processor --tail=500 | \
  grep -E 'digest groups|recommendation|benchmark|complete|error'
```

---

## Step 7: Collect metrics

### Prometheus metrics (ROS processor)

Port-forward to the processor's metrics endpoint:

```bash
# Find the processor pod
PROC_POD=$(oc get pods -n cost-onprem -l app.kubernetes.io/component=ros-processor -o name | head -1)

# Port-forward
oc port-forward -n cost-onprem $PROC_POD 9000:9000 &

# Fetch pipeline phase timings
curl -s http://localhost:9000/metrics | grep rosocp_pipeline_phase_duration

# Fetch recommendation timings
curl -s http://localhost:9000/metrics | grep rosocp_recommendation_duration

# Fetch DB operation timings
curl -s http://localhost:9000/metrics | grep rosocp_db_query_duration

# Memory usage
curl -s http://localhost:9000/metrics | grep go_memstats_alloc_bytes
```

### Database metrics

```bash
DB_POD=$(oc get pods -n cost-onprem -l app.kubernetes.io/component=database -o name | head -1)

# Total digests created
oc exec -n cost-onprem $DB_POD -- psql -U postgres -d costonprem_ros -t -c \
  "SELECT COUNT(*) FROM daily_container_digests;"

# Recommendations by type
oc exec -n cost-onprem $DB_POD -- psql -U postgres -d costonprem_ros -c \
  "SELECT recommendation_type, COUNT(*) FROM container_recommendation_sets GROUP BY 1 ORDER BY 2 DESC;"

# Database size
oc exec -n cost-onprem $DB_POD -- psql -U postgres -d costonprem_ros -t -c \
  "SELECT pg_size_pretty(pg_database_size('costonprem_ros'));"
```

---

## Step 8: Clean up

```bash
# Delete the generator pod and PVC
oc delete pod nise-generator -n cost-onprem --grace-period=0
oc delete pvc nise-generator-data -n cost-onprem

# Remove the NetworkPolicy patch (optional — only if you added it)
# Re-apply the original NetworkPolicy from Helm
```

---

## Scaling guidelines

| Containers | PVC size | nise time | Expected CSV files | Tarball chunks | Listener time | ROS time |
|------------|----------|-----------|-------------------|----------------|---------------|----------|
| 4,000 | 15 GiB | ~10 min | ~120 | 4 | ~10 min | ~7.5 min |
| 10,000 | 30 GiB | ~25 min | ~295 | 11 | ~35 min | ~13 min |
| 20,000 | 60 GiB | ~50 min | ~590 | 20 | ~70 min | ~25 min (est.) |
| 50,000 | 150 GiB | ~2h | ~1,500 | 50 | ~3h (est.) | ~1h (est.) |

!!! note "Bottleneck shifts with scale"
    At 10K+ containers, the **Koku listener** (not the ROS processor) becomes the
    bottleneck. The listener spends most time on CSV → Parquet conversion and
    PostgreSQL writes. The ROS processor scales nearly linearly.

---

## Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| nise `AttributeError: 'str' object has no attribute 'get'` | Wrong YAML format for nise config | Use `OCPGenerator:` wrapper with `nodes:` → `namespaces:` hierarchy |
| Listener `No manifest found in payload` | Manifest not named `manifest.json` in tarball | Use `tar --transform 's/chunk_manifest.json/manifest.json/'` |
| Listener `ValidationError ... uuid ... should be a valid UUID` | Manifest `uuid` field is not a valid UUID | Use `str(uuid.uuid4())` when generating manifest |
| Ingress `ConnectTimeout` from nise pod | NetworkPolicy blocks nise pod | Patch NetworkPolicy to allow `app: nise-generator` label |
| Ingress `400: missing x-rh-identity header` | No auth header in upload request | Add `x-rh-identity` header with base64-encoded identity JSON |
| `imagePullPolicy: IfNotPresent` — old image | Reused an existing image tag | Always use a unique tag: `bench-$(date -u +%Y%m%d%H%M)` |
| `podman push` fails with `unable to retrieve auth token` | Registry blob reuse auth issue | Use `skopeo copy` instead of `podman push` |
| nise exits with 0 but no output | Missing `--ros-ocp-info` or wrong flags | Add `--ros-ocp-info -w` flags |
| ROS processor `statement_timeout` | Large recommendation batch exceeds default timeout | Set `ROS_DB_INGEST_STATEMENT_TIMEOUT=120` (seconds) |
| 3+ hours for 10K containers | Intermediate flushes with small batch size | Set `ROS_INGEST_FLUSH_BATCH_SIZE` to `math.MaxInt32` (see #264) |
