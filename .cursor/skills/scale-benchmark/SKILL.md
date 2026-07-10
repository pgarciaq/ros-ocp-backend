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

Run scale benchmarks (4K–50K containers) of the ROS-OCP native engine on
OpenShift clusters. This skill captures hard-won knowledge from multiple
benchmark iterations — follow it precisely to avoid repeating past failures.

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

| Containers | PVC | nise time | CSV files | Chunks | Listener | ROS processor |
|-----------|-----|-----------|-----------|--------|----------|---------------|
| 4K | 15 GiB | ~10 min | ~120 | 4 | ~10 min | ~7.5 min |
| 10K | 30 GiB | ~25 min | ~295 | 11 | ~35 min | ~13 min |
| 20K | 60 GiB | ~50 min | ~590 | 20 | ~70 min | ~25 min |
| 50K | 150 GiB | ~2h | ~1,500 | 50 | ~3h | ~1h |
