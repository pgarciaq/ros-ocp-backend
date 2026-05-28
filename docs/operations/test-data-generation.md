# Test Data Generation with NISE

This document describes how to generate and upload test data for the ROS-OCP
Backend using the [NISE](https://github.com/project-koku/nise) data generator.

---

## Overview

NISE generates realistic OCP metric CSVs that exercise all ROS plugins.
The generated data flows through the same ingestion path as production data
from the koku-metrics-operator.

**Ingestion path:**
```
nise → tarball → ingress → Koku listener → S3 bucket → ros-ocp-backend
```

---

## CSV Filename Conventions

`ros-ocp-backend` classifies CSV files using `DetermineCSVType()` in
`internal/utils/utils.go`. The function uses ordered prefix matching with a
`Contains` fallback:

1. **Prefix match** (longest match first): handles operator-generated and
   `--insights-upload` filenames
2. **Contains fallback**: handles `--write-monthly` filenames where a
   `Month-Year-UUID-` prefix precedes the pattern

### Filename patterns by source

| Plugin | Operator filename | Nise `--insights-upload` | Nise `--write-monthly` |
|--------|-------------------|--------------------------|------------------------|
| container | `ros-openshift-container-YYYYMM.csv` | `{uuid}_openshift_report.N.csv` | `Month-Year-UUID-ocp_ros_usage.csv` |
| gpu | *(piggybacks on container CSV)* | *(same as container)* | *(same as container)* |
| node | *(piggybacks on container CSV)* | *(same as container)* | *(same as container)* |
| namespace | `ros-openshift-namespace-YYYYMM.csv` | `{uuid}-ros-openshift-namespace-YYYYMM.N.csv` | `Month-Year-UUID-ocp_ros_namespace_usage.csv` |
| quota | *(no CSV — reads namespace digests)* | — | — |
| cluster-quota | `ros-openshift-cluster-quota-YYYYMM.csv` | `ros-openshift-cluster-quota-{start}-{end}.N.csv` | `Month-Year-UUID-ocp_ros_cluster_quota.csv` |
| pvc | `ros-openshift-storage-YYYYMM.csv` | `cm-openshift-storage-usage-YYYYMM.N.csv` | `Month-Year-UUID-ocp_storage_usage.csv` |
| snapshot | `ros-openshift-snapshot-inventory-YYYYMM.csv` | `cm-openshift-snapshot-inventory-YYYYMM.N.csv` | `Month-Year-UUID-ocp_snapshot_inventory.csv` |

### Classification rules (ordered, `internal/utils/utils.go`)

```
ros-openshift-cluster-quota-  → PayloadTypeClusterQuota
ros-openshift-namespace-      → PayloadTypeNamespace
ros-openshift-snapshot-       → PayloadTypeSnapshot
ros-openshift-storage-        → PayloadTypeStorage
ocp_ros_cluster_quota         → PayloadTypeClusterQuota
ocp_ros_namespace             → PayloadTypeNamespace
ocp_snapshot_inventory        → PayloadTypeSnapshot
ocp_storage_usage             → PayloadTypeStorage
(default)                     → PayloadTypeContainer
```

Ordering matters: `ros-openshift-cluster-quota-` is checked before
`ros-openshift-` patterns to avoid false positives.

---

## PVC and Snapshot: shipped via Koku's `ROS_EXTRA_PATTERNS`

PVC (`storage_usage`) and Snapshot (`snapshot_inventory`) data are **not** directly
part of the ROS payload. Instead, Koku's cost pipeline extracts them from
the standard OCP report and re-ships them to the ROS S3 bucket.

In `koku/masu/external/kafka_msg_handler.py`:
```python
ROS_EXTRA_PATTERNS = ("storage-usage", "snapshot-inventory")
```

This means nise `--insights-upload` creates standard OCP storage/snapshot files;
Koku then detects and forwards them to ROS. For manual `--write-monthly` testing,
the files can be included directly in the tarball with proper patterns.

---

## Method 1: `--insights-upload` (Recommended)

This is the preferred method when nise can reach the ingress service:

```bash
INSIGHTS_ACCOUNT_ID=10001 \
INSIGHTS_ORG_ID=1234567 \
nise report ocp \
  --static-report-file config.yml \
  --ocp-cluster-id $CLUSTER_UUID \
  --ros-ocp-info \
  --insights-upload http://ingress-route:port/api/ingress/v1/upload
```

**Advantages:**
- Single command: generates, renames, tarballs, and uploads
- Files are renamed to match operator conventions (prefix rules match)
- Manifest is auto-generated with correct structure

**Requirements:**
- `INSIGHTS_ACCOUNT_ID` and `INSIGHTS_ORG_ID` environment variables
- Network access from nise host to the ingress endpoint

---

## Method 2: `--write-monthly` + manual upload

Use this when nise runs on a different machine (common in local dev):

### Step 1: Generate data

```bash
nise report ocp \
  --static-report-file config.yml \
  --ocp-cluster-id $CLUSTER_UUID \
  --ros-ocp-info \
  --write-monthly
```

Output directory structure:
```
output/
└── $CLUSTER_UUID/
    └── YYYYMMDD-YYYYMMDD/
        ├── Month-Year-UUID-ocp_ros_usage.csv
        ├── Month-Year-UUID-ocp_ros_namespace_usage.csv
        ├── Month-Year-UUID-ocp_ros_cluster_quota.csv
        ├── Month-Year-UUID-ocp_storage_usage.csv
        ├── Month-Year-UUID-ocp_snapshot_inventory.csv
        └── manifest.json
```

### Step 2: Create tarball

```bash
cd output/$CLUSTER_UUID/YYYYMMDD-YYYYMMDD/
tar czf /tmp/upload.tar.gz --transform='s|^\./||' .
```

**Critical:** Use `--transform='s|^\./||'` to strip the `./` prefix. Without it,
the ingress service cannot match manifest filenames against extracted file paths.

### Step 3: Upload

```bash
# From a host that can reach the ingress route
curl -X POST \
  -F "file=@/tmp/upload.tar.gz;type=application/vnd.redhat.hccm.tar+tgz" \
  -H "Authorization: Bearer $TOKEN" \
  http://ingress-route/api/ingress/v1/upload
```

The `DetermineCSVType()` `Contains` fallback handles the `Month-Year-UUID-`
prefix correctly, so all plugins receive their data.

---

## Method 3: MinIO direct upload (Koku dev environment)

For Koku's Docker Compose dev environment with MinIO:

```bash
# Upload to ocp-ingress bucket without .tar.gz extension
mc cp /tmp/upload.tar.gz local/ocp-ingress/$PAYLOAD_NAME

# Trigger ingestion
curl "http://localhost:5042/api/cost-management/v1/ingest_ocp_payload/?org_id=1234567&payload_name=$PAYLOAD_NAME"
```

See the [koku AGENTS.md](../../koku/AGENTS.md) for full MinIO workflow details.

---

## NISE Static Report Configuration

### Minimal config for all plugins

```yaml
---
generators:
  - OCPGenerator:
      start_date: 2026-05-01
      end_date: 2026-05-28
      nodes:
        - node_name: node-1
          cpu_cores: 8
          memory_gig: 32
          resource_id: i-node1
          namespaces:
            namespace_1:
              pods:
                - pod_name: app-pod-1
                  cpu_request: 100
                  mem_request_gig: 0.5
                  cpu_limit: 500
                  mem_limit_gig: 2
              volumes:
                - volume_name: pvc-data
                  storage_class: gp2
                  volume_request_gig: 50
                  capacity_gig: 100
              namespace_quotas:
                cpu_request_hard: "8000m"
                cpu_request_used: "4500m"
                memory_request_hard: "16Gi"
                memory_request_used: "9Gi"
              cluster_quotas:
                - quota_name: team-alpha-budget
                  cpu_request_hard: "16000m"
                  cpu_request_used: "9000m"
                  memory_request_hard: "32Gi"
                  memory_request_used: "18Gi"
                  cpu_limit_hard: "32000m"
                  cpu_limit_used: "12000m"
                  memory_limit_hard: "64Gi"
                  memory_limit_used: "24Gi"
```

### Key flags

| Flag | Purpose |
|------|---------|
| `--ros-ocp-info` | Generate container-level ROS data (required) |
| `--write-monthly` | Organize output by month with manifest |
| `--ocp-cluster-id` | Set cluster UUID in output |
| `--static-report-file` | Use YAML config instead of random generation |
| `--insights-upload URL` | Auto-upload to ingress endpoint |

---

## Manifest Structure

The upload tarball must include a `manifest.json`:

```json
{
  "cluster_id": "UUID",
  "uuid": "assembly-uuid",
  "date": "2026-05-28T00:00:00",
  "start": "2026-05-01T00:00:00",
  "end": "2026-05-28T00:00:00",
  "version": "1.0.0",
  "files": ["ocp_pod_usage.csv", "ocp_storage_usage.csv"],
  "resource_optimization_files": [
    "ocp_ros_usage.csv",
    "ocp_ros_namespace_usage.csv",
    "ocp_ros_cluster_quota.csv"
  ]
}
```

- `files` → shipped to Koku for cost processing
- `resource_optimization_files` → shipped to ROS for recommendation processing
- `start` and `end` are **required** for Koku's summary table population

### ⚠️ Missing `start`/`end` — silent failure

If `start` or `end` is omitted from `manifest.json`, Koku ingests the data without
error but does not populate its cost summary tables. ROS-OCP still receives and
processes the CSV files correctly (recommendations will appear), but Koku's cost
reports will show empty results for that time range.

**This is not validated by ros-ocp-backend.** Manifest parsing happens in the Koku
listener (`koku/masu/external/kafka_msg_handler.py`), which is the correct layer
for this validation. If code-level safety is desired, implement the check in Koku's
manifest parsing — not in ros-ocp-backend, which never sees `manifest.json` directly.

---

## Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| All files classified as `PayloadTypeContainer` | Filenames don't contain expected patterns | Verify nise config includes `--ros-ocp-info` and quota data |
| Upload succeeds but no recommendations generated | Missing `start`/`end` in manifest | Regenerate manifest with date range |
| `tar: Removing leading './' from member names` | Tarball created without `--transform` | Rebuild with `--transform='s\|^\./\|\|'` |
| Nise exits silently (code 0, no output) | Missing `--write-monthly` or env vars | Add `--write-monthly` or set `INSIGHTS_ACCOUNT_ID`/`INSIGHTS_ORG_ID` |
| Quota/CRQ data missing from output | Static YAML missing `namespace_quotas`/`cluster_quotas` | Add quota sections to the YAML config |
| PVC/snapshot data not reaching ROS | Direct upload without Koku in the path | Include PVC/snapshot files in `resource_optimization_files` manifest array |

---

## Handing Off to Another Developer

If another developer (e.g., Rohini) needs to generate test data:

1. **Install nise:** `pip install koku-nise` or clone from
   [project-koku/nise](https://github.com/project-koku/nise)
2. **Copy the static YAML** from `nise/examples/ros_ocp/ocp_static_data.yml`
   and adjust dates
3. **Choose a method:**
   - `--insights-upload` if they can reach the ingress route (simplest)
   - `--write-monthly` + manual tarball if working locally
4. **Verify:** Check ROS processor logs for `ingested N rows` messages
5. **Wait:** Recommendations appear after the async plugin pipeline completes
   (typically 30-60 seconds for namespace/quota, longer for container)

**No special configuration is needed.** The `DetermineCSVType()` `Contains`
fallback ensures nise's `--write-monthly` filenames work without renaming.
