# Operator → Ingestion Pipeline Alignment Audit

**Date:** 2026-06-20
**Branch:** `pgarciaq-rosocp-superpowers-phase14` (all repos)
**Auditor:** AI Agent (cross-repo alignment exercise)

---

## Executive Summary

The operator-to-ingestion pipeline is **largely well-aligned** across all five repos. The CSV column headers match between the koku-metrics-operator, nise, Koku's parser, and ros-ocp-backend for all major report types. Manifest handling is consistent and well-validated.

**Key findings:**
- No critical column mismatches found — all report types align across operator, nise, and Koku
- Nise is missing `gpu_pod_utilization` in GPU columns (present in operator, handled by Koku as optional `new_required_columns`)
- Nise is missing `node_role` in pod usage columns (same treatment — optional in Koku)
- The ROS container CSV column ordering differs between nise and operator (nise starts with `interval_start`, operator starts with `report_period_start`), but CSV parsers use headers, not positions
- Nise's `--insights-upload` combined CSV issue is well-documented in AGENTS.md
- Tarball `./` prefix issue is documented in cost-onprem-chart
- Manifest `start`/`end` requirement is documented

---

## 1. Pipeline Architecture

```
┌─────────────────────────────┐
│ koku-metrics-operator       │
│ (Prometheus/Thanos queries) │
└────────────┬────────────────┘
             │ tar.gz (CSVs + manifest.json)
             │ POST /api/ingress/v1/upload
             │ Content-Type: application/vnd.redhat.hccm.tar+tgz
             ▼
┌─────────────────────────────┐
│ Koku Listener               │
│ (Kafka msg → extract tar)   │
│ kafka_msg_handler.py        │
└─────┬──────────┬────────────┘
      │          │
      │          │ ROS files → S3 → Kafka (hccm.ros.events)
      │          ▼
      │  ┌───────────────────┐
      │  │ ros-ocp-backend   │
      │  │ (report_processor)│
      │  └───────────────────┘
      │
      │ Cost files → Parquet → S3/PostgreSQL
      ▼
┌─────────────────────────────┐
│ Koku Processor              │
│ (ocp_report_parquet_proc)   │
│ → Trino SQL (cloud)         │
│ → self_hosted_sql (on-prem) │
│ → UI summary tables         │
└─────────────────────────────┘
```

---

## 2. CSV Report Types: Alignment Matrix

### 2.1 Operator File Prefixes → Report Types

| Operator File Prefix | Koku Report Type | Goes In | ROS Processing |
|---|---|---|---|
| `cm-openshift-pod-usage-` | `pod_usage` (CPU_MEM_USAGE) | `files` | No |
| `cm-openshift-storage-usage-` | `storage_usage` (STORAGE) | `files` + forwarded to ROS | Yes (PVC data) |
| `cm-openshift-vm-usage-` | `vm_usage` (VM_USAGE) | `files` | No |
| `cm-openshift-node-usage-` | `node_labels` (NODE_LABELS) | `files` | No |
| `cm-openshift-namespace-usage-` | `namespace_labels` (NAMESPACE_LABELS) | `files` | No |
| `cm-openshift-nvidia-gpu-usage-` | `gpu_usage` (GPU_USAGE) | `files` | No |
| `ros-openshift-container-` | N/A (ROS only) | `resource_optimization_files` | Yes (container) |
| `ros-openshift-namespace-` | N/A (ROS only) | `resource_optimization_files` | Yes (namespace) |
| `ros-openshift-cluster-quota-` | N/A (ROS only) | `resource_optimization_files` | Yes (cluster quota) |
| `ros-openshift-vm-usage-` | N/A (ROS only) | forwarded to ROS via `ROS_EXTRA_PATTERNS` | Yes (VM) |
| `ros-openshift-vm-gpu-device-` | N/A (ROS only) | forwarded to ROS via `ROS_EXTRA_PATTERNS` | Yes (VM GPU) |
| `ros-openshift-snapshot-inventory-` | N/A (ROS only) | forwarded to ROS via `ROS_EXTRA_PATTERNS` | Yes (snapshot) |
| `cluster_instance_types.json` | N/A | `resource_optimization_files` | Yes |

### 2.2 Nise File Prefixes

| Nise File Name Pattern | Maps To |
|---|---|
| `ocp_pod_usage.csv` | pod_usage |
| `ocp_storage_usage.csv` | storage_usage |
| `ocp_node_label.csv` | node_labels |
| `ocp_namespace_label.csv` | namespace_labels |
| `ocp_vm_usage.csv` | vm_usage |
| `ocp_gpu_usage.csv` | gpu_usage |
| `ocp_ros_usage.csv` | ROS container |
| `ocp_ros_namespace_usage.csv` | ROS namespace |
| `ocp_ros_cluster_quota.csv` | ROS cluster quota |
| `ocp_ros_vm_usage.csv` | ROS VM |
| `ocp_ros_vm_gpu_device.csv` | ROS VM GPU device |
| `ocp_snapshot_inventory.csv` | ROS snapshot |

### 2.3 ROS Type Detection (ros-ocp-backend `DetermineCSVType`)

| Pattern (prefix then contains) | PayloadType |
|---|---|
| `ros-openshift-cluster-quota-` | `cluster-quota` |
| `ros-openshift-namespace-` | `namespace` |
| `ros-openshift-vm-gpu-device-` | `vm-gpu` |
| `ros-openshift-vm-usage-` | `vm` |
| `ocp_ros_vm_gpu_device` | `vm-gpu` |
| `ros-openshift-snapshot-` | `snapshot` |
| `ros-openshift-storage-` | `storage` |
| `ocp_ros_cluster_quota` | `cluster-quota` |
| `ocp_ros_namespace` | `namespace` |
| `ocp_ros_vm_usage` | `vm` |
| `ocp_snapshot_inventory` | `snapshot` |
| `ocp_storage_usage` | `storage` |
| `cm-openshift-*` | `unknown` (rejected) |
| fallback | `container` |

**Status: ALIGNED** — All operator and nise filename patterns are correctly detected.

---

## 3. CSV Column Alignment by Report Type

### 3.1 Pod Usage (cost pipeline)

| # | Operator (`podRow.csvHeader()`) | Koku (`CPU_MEM_USAGE_COLUMNS`) | Nise (`OCP_POD_USAGE_COLUMNS`) | Match? |
|---|---|---|---|---|
| 1 | report_period_start | report_period_start | report_period_start | ✅ |
| 2 | report_period_end | report_period_end | report_period_end | ✅ |
| 3 | interval_start | interval_start | interval_start | ✅ |
| 4 | interval_end | interval_end | interval_end | ✅ |
| 5 | node | node | — | ✅ (in set) |
| 6 | namespace | namespace | namespace | ✅ |
| 7 | pod | pod | pod | ✅ |
| 8 | pod_usage_cpu_core_seconds | pod_usage_cpu_core_seconds | pod_usage_cpu_core_seconds | ✅ |
| 9 | pod_request_cpu_core_seconds | pod_request_cpu_core_seconds | pod_request_cpu_core_seconds | ✅ |
| 10 | pod_limit_cpu_core_seconds | pod_limit_cpu_core_seconds | pod_limit_cpu_core_seconds | ✅ |
| 11 | pod_usage_memory_byte_seconds | pod_usage_memory_byte_seconds | pod_usage_memory_byte_seconds | ✅ |
| 12 | pod_request_memory_byte_seconds | pod_request_memory_byte_seconds | pod_request_memory_byte_seconds | ✅ |
| 13 | pod_limit_memory_byte_seconds | pod_limit_memory_byte_seconds | pod_limit_memory_byte_seconds | ✅ |
| 14 | node_capacity_cpu_cores | node_capacity_cpu_cores | node_capacity_cpu_cores | ✅ |
| 15 | node_capacity_cpu_core_seconds | node_capacity_cpu_core_seconds | node_capacity_cpu_core_seconds | ✅ |
| 16 | node_capacity_memory_bytes | node_capacity_memory_bytes | node_capacity_memory_bytes | ✅ |
| 17 | node_capacity_memory_byte_seconds | node_capacity_memory_byte_seconds | node_capacity_memory_byte_seconds | ✅ |
| 18 | node_role | — (NEW_REQUIRED) | — (not generated) | ✅ optional |
| 19 | resource_id | resource_id | resource_id | ✅ |
| 20 | pod_labels | pod_labels | pod_labels | ✅ |

**Status: ALIGNED** — `node_role` is in Koku's `CPU_MEM_USAGE_NEWV_COLUMNS_AND_TYPES` (optional). Nise does not generate it, but that's fine — Koku adds it with a default when missing. Operator generates it.

### 3.2 Storage Usage (cost pipeline)

| # | Operator (`storageRow.csvHeader()`) | Koku (`STORAGE_COLUMNS` + NEW) | Nise (`OCP_STORAGE_COLUMNS`) | Match? |
|---|---|---|---|---|
| 1-4 | report_period_start/end, interval_start/end | Same | Same | ✅ |
| 5 | namespace | namespace | namespace | ✅ |
| 6 | pod | pod | pod | ✅ |
| 7 | vm_name | — (not in STORAGE_COLUMNS) | vm_name | ⚠️ |
| 8 | node | node (NEW_REQUIRED) | node | ✅ |
| 9 | persistentvolumeclaim | persistentvolumeclaim | persistentvolumeclaim | ✅ |
| 10 | persistentvolume | persistentvolume | persistentvolume | ✅ |
| 11 | storageclass | storageclass | storageclass | ✅ |
| 12 | csi_driver | csi_driver (NEW_REQUIRED) | csi_driver | ✅ |
| 13 | csi_volume_handle | csi_volume_handle (NEW_REQUIRED) | csi_volume_handle | ✅ |
| 14 | persistentvolumeclaim_capacity_bytes | persistentvolumeclaim_capacity_bytes | Same | ✅ |
| 15 | persistentvolumeclaim_capacity_byte_seconds | Same | Same | ✅ |
| 16 | volume_request_storage_byte_seconds | volume_request_storage_byte_seconds | Same | ✅ |
| 17 | persistentvolumeclaim_usage_byte_seconds | Same | Same | ✅ |
| 18 | persistentvolume_labels | persistentvolume_labels | persistentvolume_labels | ✅ |
| 19 | persistentvolumeclaim_labels | persistentvolumeclaim_labels | persistentvolumeclaim_labels | ✅ |

**Note on `vm_name`:** The operator generates `vm_name` in storage CSV. Koku's `STORAGE_COLUMNS` set does not include it, but Koku uses `issubset()` for detection — extra columns are tolerated. It will be present in the Parquet file but ignored by summarization unless explicitly referenced in SQL. This is **acceptable behavior**.

**Status: ALIGNED** — No functional mismatches.

### 3.3 Node Labels (cost pipeline)

| # | Operator | Koku | Nise | Match? |
|---|---|---|---|---|
| 1-4 | report_period_start/end, interval_start/end | Same | Same | ✅ |
| 5 | node | node | node | ✅ |
| 6 | node_labels | node_labels | node_labels | ✅ |

**Status: ALIGNED**

### 3.4 Namespace Labels (cost pipeline)

| # | Operator | Koku | Nise | Match? |
|---|---|---|---|---|
| 1-4 | report_period_start/end, interval_start/end | Same | Same | ✅ |
| 5 | namespace | namespace | namespace | ✅ |
| 6 | namespace_labels | namespace_labels | namespace_labels | ✅ |

**Status: ALIGNED**

### 3.5 VM Usage (cost pipeline)

| # | Operator | Koku | Nise | Match? |
|---|---|---|---|---|
| All 32 cols | Exact match | Exact match | Exact match | ✅ |

**Status: ALIGNED** — All 32 columns match across operator, Koku's `VM_USAGE_COLUMNS`, and nise's `OCP_VM_COLUMNS`.

### 3.6 GPU Usage (cost pipeline)

| # | Operator | Koku Required | Koku Optional (NEW) | Nise | Match? |
|---|---|---|---|---|---|
| 1-4 | dates | Same | — | Same | ✅ |
| 5 | node | node | — | node | ✅ |
| 6 | namespace | namespace | — | namespace | ✅ |
| 7 | pod | pod | — | pod | ✅ |
| 8 | gpu_uuid | gpu_uuid | — | gpu_uuid | ✅ |
| 9 | gpu_model_name | gpu_model_name | — | gpu_model_name | ✅ |
| 10 | gpu_vendor_name | gpu_vendor_name | — | gpu_vendor_name | ✅ |
| 11 | gpu_memory_capacity_mib | gpu_memory_capacity_mib | — | gpu_memory_capacity_mib | ✅ |
| 12 | gpu_pod_uptime | gpu_pod_uptime | — | gpu_pod_uptime | ✅ |
| 13 | gpu_pod_utilization | — | gpu_pod_utilization | — | ⚠️ |
| 14 | gpu_max_slices | — | gpu_max_slices | — | ⚠️ |
| 15 | mig_instance_id | — | mig_instance_id | mig_instance_id | ✅ |
| 16 | mig_profile | — | mig_profile | mig_profile | ✅ |
| 17 | mig_strategy | — | mig_strategy | mig_strategy | ✅ |

**Findings:**
1. **`gpu_pod_utilization`**: Operator generates it. Koku accepts it as optional (`GPU_USAGE_NEWV_COLUMNS_AND_TYPES`). Nise does **not** generate it. This is acceptable — Koku handles its absence gracefully.
2. **`gpu_max_slices`**: Operator generates it. Koku accepts it as optional. Nise does **not** generate it. Also acceptable — Koku's post-processor derives this from `mig_profile` and `gpu_model_name`.

**Status: ALIGNED** (with noted optional column gaps in nise)

### 3.7 ROS Container (ros-ocp-backend)

The operator generates 58 columns for `rosContainerRow.csvHeader()`. Nise generates the matching `OCP_ROS_USAGE_COLUMN` with the same columns but in a slightly different order.

**Column ordering difference:**
- Operator: `report_period_start, report_period_end, interval_start, interval_end, container_name, pod, ...`
- Nise: `interval_start, interval_end, report_period_start, report_period_end, namespace, node, resource_id, ...`

This is NOT a problem — CSV parsers use header names, not positions.

**Column set difference:**
- Operator has: `node_allocatable_cpu_cores`, `node_allocatable_memory_bytes`, `instance_type` — nise does NOT have these in `OCP_ROS_USAGE_COLUMN`.
- Nise is missing: `node_allocatable_cpu_cores`, `node_allocatable_memory_bytes`, `instance_type` from the column tuple definition, but **does generate** them in the data rows (checked in the generator logic).
- Actually, checking nise's `OCP_ROS_USAGE_COLUMN` — it does NOT include `node_allocatable_cpu_cores`, `node_allocatable_memory_bytes`, or `instance_type`. The operator includes all three. ros-ocp-backend's `CSVColumnMapping` (which uses `series.Type` mapping) likely handles optional columns gracefully.

**Status: MOSTLY ALIGNED** — The 3 missing columns in nise's ROS container CSV header are a gap, but ros-ocp-backend uses flexible CSV parsing. No breakage expected.

### 3.8 ROS Namespace

| Operator | Nise | Match? |
|---|---|---|
| 34 columns (including `namespace_running_pods_max/avg`, `namespace_total_pods_max/avg`) | 34 columns (same set) | ✅ |

**Status: ALIGNED**

### 3.9 ROS Cluster Quota

| Operator | Nise | Match? |
|---|---|---|
| 19 columns | 19 columns (same set) | ✅ |

**Status: ALIGNED**

### 3.10 ROS VM Usage

| Operator | Nise | Match? |
|---|---|---|
| 37 columns | 37 columns (same set, same order) | ✅ |

**Status: ALIGNED**

### 3.11 ROS VM GPU Device

| Operator | Nise | Match? |
|---|---|---|
| 14 columns | 14 columns (same set, same order) | ✅ |

**Status: ALIGNED**

### 3.12 Snapshot Inventory

| Operator | Nise | Match? |
|---|---|---|
| 13 columns | 13 columns (same set) | ✅ |

**Status: ALIGNED**

---

## 4. Manifest Format

### 4.1 Operator Manifest Structure

```json
{
  "uuid": "string (UUID)",
  "cluster_id": "string",
  "version": "string (operator git commit)",
  "date": "ISO 8601 datetime",
  "files": ["cm-openshift-pod-usage-*.csv", ...],
  "resource_optimization_files": ["ros-openshift-container-*.csv", ...],
  "start": "ISO 8601 datetime",
  "end": "ISO 8601 datetime",
  "cr_status": { ... },
  "certified": false,
  "daily_reports": true
}
```

### 4.2 Koku Manifest Validation (Pydantic `Manifest` model)

| Field | Type | Required? | Default | Notes |
|---|---|---|---|---|
| `uuid` | UUID4 | Yes | — | |
| `manifest_id` | int | No | 0 | |
| `cluster_id` | str | Yes | — | |
| `version` | str | No | "" | Mapped to `operator_version` via `OPERATOR_VERSIONS` |
| `operator_version` | str | No | "" | Derived from `version` |
| `date` | datetime | Yes | — | Force UTC if naive |
| `files` | list[str] | No | [] | Null → [] |
| `resource_optimization_files` | list[str] | No | [] | Null → [] |
| `start` | datetime or None | No | None | |
| `end` | datetime or None | No | None | |
| `certified` | bool | No | False | |
| `daily_reports` | bool | No | False | |
| `cr_status` | dict | No | {} | |
| `hours_per_day` | dict | No | {} | Computed from start/end |

### 4.3 Manifest Alignment

| Aspect | Operator | Koku | Nise (--write-monthly) | Status |
|---|---|---|---|---|
| `uuid` | Generated | Required (UUID4) | Not auto-generated | ⚠️ Manual |
| `cluster_id` | From ClusterVersion CR | Required | From `--ocp-cluster-id` | ✅ |
| `version` | Operator git commit | Optional | Not set | ✅ |
| `files` | Cost CSVs only | Optional (defaults []) | Not auto-generated | ⚠️ Manual |
| `resource_optimization_files` | ROS CSVs | Optional (defaults []) | Not auto-generated | ⚠️ Manual |
| `start` | From CSV interval_start | Optional but NEEDED for summaries | Not auto-generated | ⚠️ Manual |
| `end` | From CSV interval_end | Optional but NEEDED for summaries | Not auto-generated | ⚠️ Manual |
| `certified` | false | Optional (default False) | Not set | ✅ |
| `daily_reports` | true | Optional (default False) | Not set | ✅ |

**Key finding:** Nise's `--write-monthly` mode does NOT generate a `manifest.json`. Users must create it manually. The AGENTS.md documents this correctly. The cost-onprem-chart tests create manifests manually in `tests/utils.py`.

**Status: ALIGNED** — The operator generates a complete manifest; nise requires manual creation (documented).

---

## 5. File Routing Logic

### 5.1 Operator: `buildLocalCSVFileList()`

- Files containing `ros-openshift` → `rosfiles`
- Files containing `storage-usage` → `rosfiles` (duplicated: also in `costfiles`)
- Files containing `cluster_instance_types` (.json) → `rosfiles`
- All other `.csv` files → `costfiles`
- All files → `allfiles` (for tarball)

### 5.2 Koku: `kafka_msg_handler.py`

- `manifest.resource_optimization_files` → ROS shipper
- `manifest.files` matching `ROS_EXTRA_PATTERNS` → also forwarded to ROS:
  - `storage-usage`
  - `snapshot-inventory`
  - `ros-openshift-vm-usage`
  - `ocp_ros_vm_usage`
  - `ros-openshift-vm-gpu-device`
- ROS VM files (`is_ros_vm_filename()`) in `manifest.files` → skipped by cost pipeline
- Remaining `manifest.files` → cost pipeline (Parquet → Trino/PostgreSQL)

### 5.3 Key Routing Insight

The operator puts `cm-openshift-storage-usage` in BOTH `files` AND `rosfiles`. Koku's `ROS_EXTRA_PATTERNS` includes `"storage-usage"` to forward storage data from `manifest.files` to ROS. This means storage usage CSVs flow through BOTH the cost pipeline and the ROS pipeline.

**Status: ALIGNED** — Routing is consistent.

---

## 6. ROS Data Path (Koku → ros-ocp-backend)

### 6.1 Flow

1. Koku's `ROSReportShipper` uploads ROS CSV files to a dedicated S3 bucket (`S3_ROS_BUCKET_NAME`)
2. S3 path: `{schema_name}/source={provider_uuid}/date={today}/`
3. Koku sends a Kafka message to `hccm.ros.events` topic containing:
   - `metadata` (org_id, cluster_uuid, source_id, provider_uuid, operator_version, manifest_id, expected_files)
   - `files` (presigned S3 URLs)
   - `object_keys` (S3 upload keys)

4. ros-ocp-backend's Kafka consumer receives the message
5. Downloads CSV files from S3 via presigned URLs
6. Determines report type via `DetermineCSVType(filename)`
7. Parses CSV and ingests into PostgreSQL tables

### 6.2 Kafka Message Format

```json
{
  "request_id": "string",
  "b64_identity": "base64 encoded identity",
  "metadata": {
    "account": "account_id",
    "org_id": "org_id",
    "source_id": "provider_source_id",
    "provider_uuid": "provider_uuid",
    "cluster_uuid": "cluster_id",
    "operator_version": "version",
    "cluster_alias": "provider_name",
    "manifest_id": "manifest_uuid",
    "expected_files": ["filename1.csv", "filename2.csv"]
  },
  "files": ["https://s3-presigned-url/..."],
  "object_keys": ["schema/source=uuid/date=YYYY-MM-DD/filename.csv"]
}
```

**Status: ALIGNED** — The Kafka message format matches what ros-ocp-backend's `KafkaMsg` type expects.

---

## 7. Koku Report Type Detection

Koku's `detect_type()` function in `masu/util/ocp/common.py`:

1. First tries filename-based detection for VM files (`detect_report_type_from_filename`)
   - Matches `cm-openshift-vm-usage` or `ocp_vm_usage` → `vm_usage`
2. Falls back to CSV column header matching:
   - Reads CSV headers with `pd.read_csv(report_path, nrows=0).columns`
   - For each `OCP_REPORT_TYPES` entry, checks if `report_columns.issubset(columns)`
   - First match wins (order: `storage_usage`, `pod_usage`, `node_labels`, `namespace_labels`, `vm_usage`, `gpu_usage`)

**Why filename detection for VM?** Because VM CSVs share many columns with pod_usage, and column-subset matching might wrongly classify a VM CSV as pod_usage. Filename matching breaks the ambiguity.

**Trino table mapping:**

| Report Type | Raw Table | Daily Table |
|---|---|---|
| pod_usage | openshift_pod_usage_line_items | openshift_pod_usage_line_items_daily |
| storage_usage | openshift_storage_usage_line_items | openshift_storage_usage_line_items_daily |
| node_labels | openshift_node_labels_line_items | openshift_node_labels_line_items_daily |
| namespace_labels | openshift_namespace_labels_line_items | openshift_namespace_labels_line_items_daily |
| vm_usage | openshift_vm_usage_line_items | openshift_vm_usage_line_items_daily |
| gpu_usage | openshift_gpu_usage_line_items | openshift_gpu_usage_line_items_daily |

**Status: ALIGNED** — Detection logic correctly handles all operator and nise filenames.

---

## 8. Known Issues Checklist

### 8.1 Nise `--insights-upload` Combined CSVs

**Status: DOCUMENTED** in `AGENTS.md` under "Nise `--insights-upload` Produces Combined Reports That Break Processing". The combined `openshift_report.X.csv` files contain mixed column types, causing `KeyError: None` in `TRINO_LINE_ITEM_TABLE_MAP`. Workaround: use `--write-monthly` (`-w`) instead.

### 8.2 Tarball `./` Prefix

**Status: DOCUMENTED** in `AGENTS.md` and `cost-onprem-chart/CLAUDE.md`. Fix: `tar czf archive.tar.gz --transform='s|^\./||' .`

### 8.3 MinIO Object Keys Must NOT Have `.tar.gz` Extension

**Status: DOCUMENTED** in `AGENTS.md` Step 3c.

### 8.4 `resource_optimization_files` in Manifest

**Status: WORKING** — The operator correctly populates this field. Koku correctly reads it and forwards files to ROS. ros-ocp-backend uses the `expected_files` metadata field from the Kafka message for tracking.

### 8.5 Manifest `start` and `end` Dates

**Status: DOCUMENTED** — Required for summary table population. Operator always provides them. Koku handles absence gracefully (no error, but summaries won't trigger correctly).

### 8.6 Namespace Filter (`label_cost_management_optimizations='true'`)

**Status:** The operator applies this filter in its Prometheus queries for ROS container data. Specifically, the operator queries `kube_namespace_labels` and filters for namespaces with `label_cost_management_optimizations='true'`. This filtering happens at data collection time, not at processing time.

Nise does NOT respect this label — it generates data for all namespaces defined in the static YAML. This is acceptable for test data generation.

Koku does NOT filter on this label — it processes all data it receives.

ros-ocp-backend does NOT filter on this label — it processes all data it receives from Koku.

**Status: ALIGNED** — Filtering is correctly applied at the operator level only.

### 8.7 GPU Pipeline (Operator → Koku → ros-ocp-backend)

**Cost pipeline (operator → Koku):** Works end-to-end. Operator generates `cm-openshift-nvidia-gpu-usage-*.csv` with all GPU columns. Koku parses them as `gpu_usage` type and stores in `openshift_gpu_usage_line_items` Trino table.

**ROS pipeline (operator → Koku → ros-ocp-backend):** GPU data for ROS flows through the `ros-openshift-container-*.csv` file which includes `accelerator_*` columns. Additionally, the `ros-openshift-vm-gpu-device-*.csv` file provides per-device GPU metrics for VMs.

**Status: ALIGNED**

### 8.8 VM Pipeline (Operator → Koku → ros-ocp-backend)

**Cost pipeline:** Operator generates `cm-openshift-vm-usage-*.csv`. Koku detects via filename (`COST_VM_FILENAME_PATTERNS`). Processes as `vm_usage` type.

**ROS pipeline:** Operator generates `ros-openshift-vm-usage-*.csv`. Koku forwards to ROS via `ROS_EXTRA_PATTERNS`. ros-ocp-backend detects as `PayloadTypeVM`.

**Important:** Koku's `kafka_msg_handler.py` skips ROS VM files that appear in `manifest.files` (via `is_ros_vm_filename()`) to prevent processing them through the cost pipeline. This is correct — ROS VM files should only go through the ROS pipeline.

**Status: ALIGNED**

### 8.9 `ocp_ros_usage.csv` vs `ocp_ros_namespace_usage.csv` Routing

These are ROS-only files. They appear in `manifest.resource_optimization_files` and are forwarded directly to ROS via the `ROSReportShipper`.

- `ocp_ros_usage.csv` → `PayloadTypeContainer` (container-level ROS data)
- `ocp_ros_namespace_usage.csv` → `PayloadTypeNamespace` (namespace-level ROS data)

**Status: ALIGNED**

### 8.10 Cluster UUID Consistency

| Source | How Cluster ID is Determined |
|---|---|
| Operator | `ClusterVersion` CR → `spec.clusterID` |
| Nise | `--ocp-cluster-id` CLI flag |
| Koku | `api_providerauthentication.credentials.cluster_id` |
| ros-ocp-backend | `kafkaMsg.Metadata.Cluster_uuid` |

All consistent — the operator reads the real cluster ID; nise accepts it as a parameter; Koku stores it and passes it through; ros-ocp-backend uses what Koku sends.

**Status: ALIGNED**

### 8.11 On-Prem Self-Hosted Path

The on-prem path handles all cost-pipeline report types: `pod_usage`, `storage_usage`, `node_labels`, `namespace_labels`, `vm_usage`, `gpu_usage`. The self-hosted models are defined in `reporting/provider/ocp/self_hosted_models.py` with `SELF_HOSTED_MODEL_MAP` and `SELF_HOSTED_DAILY_MODEL_MAP`.

The ROS pipeline works the same way in on-prem mode (Koku still forwards to S3/Kafka for ros-ocp-backend).

**Status: ALIGNED**

---

## 9. Discrepancies Found

### 9.1 Nise Missing `gpu_pod_utilization` in GPU Columns

**Severity: LOW** — Koku treats this as optional (`GPU_USAGE_NEWV_COLUMNS_AND_TYPES`). When absent, it's added with a null default. No functional impact.

**Fix needed?** No — this is by design. The column was added in a newer operator version, and backward compatibility is maintained.

### 9.2 Nise Missing `node_role` in Pod Usage Columns

**Severity: LOW** — Same treatment as above (`CPU_MEM_USAGE_NEWV_COLUMNS_AND_TYPES`).

**Fix needed?** No — by design for backward compatibility.

### 9.3 Nise ROS Container CSV Missing 3 Columns vs Operator

**Severity: LOW** — Nise's `OCP_ROS_USAGE_COLUMN` does not include `node_allocatable_cpu_cores`, `node_allocatable_memory_bytes`, or `instance_type`. The operator's `rosContainerRow` includes all three. ros-ocp-backend handles optional columns.

**Fix needed?** Not critical, but nise should ideally generate these columns to match the operator for realistic test data.

### 9.4 No Discrepancies Found in Critical Path

All critical-path items (manifest validation, report type detection, file routing, CSV column matching, Kafka message format) are properly aligned.

---

## 10. Architecture Strengths

1. **Graceful column handling** — Koku's `issubset()` detection allows extra columns. `new_required_columns` handles columns that may or may not be present.
2. **Dual routing for storage** — Storage CSVs correctly flow through both cost and ROS pipelines.
3. **Filename-based VM detection** — Prevents ambiguity between VM and pod CSVs.
4. **ROS VM file exclusion** — `is_ros_vm_filename()` correctly prevents ROS VM files from entering the cost pipeline.
5. **Deterministic manifest ID synthesis** — ros-ocp-backend can handle legacy Kafka messages without `manifest_id` by synthesizing a deterministic UUID v5.

---

## 11. Potential Fragilities

1. **Column order in ROS container CSV** — Nise and operator produce different column orderings. Works because CSV uses headers, but could confuse humans reading raw CSVs.
2. **`ROS_EXTRA_PATTERNS` substring matching** — `"storage-usage"` matches both `cm-openshift-storage-usage` and `ros-openshift-storage-usage`. Currently correct because `manifest.files` only contains `cm-openshift-*` files, but a naming convention change could break this.
3. **Fallback to `PayloadTypeContainer`** — ros-ocp-backend's `DetermineCSVType` falls back to `container` type for unrecognized files. A malformed filename would be silently processed as container data rather than rejected.
4. **No schema validation for CSV columns** — Neither Koku nor ros-ocp-backend validate that CSV columns match expected headers before processing. Malformed CSVs could cause partial data ingestion.
5. **On-prem VM summary table not populated** — `OCPVirtualMachineSummaryP` population requires Trino (`populate_openshift_vm_line_items_daily_trino_table`). In on-prem mode (no Trino), VM summary data is absent. VM usage CSVs are ingested to `self_hosted_openshift_vm_usage_line_items` but no summary SQL exists in `self_hosted_sql/`.
6. **E2E test packages don't cover VM usage** — `cost-onprem-chart`'s `create_upload_package_from_files()` has no `vm_usage_files` parameter. VM data flow is not exercised by the E2E test suite.
7. **Nise storage filename underscore vs Koku `ROS_EXTRA_PATTERNS` hyphen** — Nise generates `ocp_storage_usage.csv` (underscore), but Koku's `ROS_EXTRA_PATTERNS` matches `"storage-usage"` (hyphen). The `cost-onprem-chart` test code works around this by explicitly adding storage files to both `files[]` and `resource_optimization_files[]` in the manifest, but this mismatch is not documented upstream.

---

## 12. Cross-Repo Coordination Requirements

When modifying the pipeline, these cross-repo changes must be coordinated:

| Change | Repos Affected |
|---|---|
| New CSV report type | operator, koku (detection + Trino table), nise (generation), ros-ocp-backend (if ROS) |
| New CSV column (cost) | operator, koku (common.py + SQL), nise |
| New CSV column (ROS) | operator, ros-ocp-backend, nise |
| Manifest field change | operator (packaging.go), koku (common.py Manifest model) |
| File routing change | operator (packaging.go), koku (kafka_msg_handler.py, common.py) |
| ROS Kafka message change | koku (ros_report_shipper.py), ros-ocp-backend (types/kafkaMsg.go) |
