# Kafka Message Schema

> **Last verified:** 2026-08-05

This document describes the Kafka message formats used by ROS-OCP-Backend.

## Topics

| Topic | Direction | Purpose |
|-------|-----------|---------|
| `platform.upload.announce` | Consume | Notification of new OCP report uploads |
| `platform.sources.event-stream` | Consume | Source lifecycle events (create/update/delete) |
| `hccm.ros.events` | Consume | ROS upload events (processor primary topic; configured via `UPLOAD_TOPIC`) |
| `hccm.ros.events.dlq` | Produce | Dead Letter Queue for messages that fail processing after max retries |

## Dead Letter Queue (`hccm.ros.events.dlq`)

Messages that exhaust the transient retry budget (`ROS_KAFKA_MAX_TRANSIENT_RETRIES`, default 5) are produced to this topic so the source partition can advance. Configure the topic name with `ROS_KAFKA_DLQ_TOPIC`.

| Property | Value |
|----------|-------|
| **Purpose** | Preserve failed messages for forensic analysis and manual replay |
| **Partitions** | 3 |
| **Replication factor** | Matches cluster config (same as other ROS topics) |
| **Retention** | 30 days (`retention.ms: 2592000000`) — longer than the source topic for forensics |
| **Segment** | 1 day (`segment.ms: 86400000`) |

### Forensic headers

DLQ messages include the original payload plus these headers:

| Header | Description |
|--------|-------------|
| `X-Original-Topic` | Source topic (typically `hccm.ros.events`) |
| `X-Original-Partition` | Source partition number |
| `X-Failure-Reason` | Error that caused routing to the DLQ |
| `X-Failed-At` | UTC timestamp when the message was dead-lettered |
| `X-Retry-Count` | Number of retries attempted (equals max at DLQ routing) |

Retry counting uses the `X-Retry-Count` header on requeued messages in the source topic. See [Monitoring — Kafka / DLQ metrics](../monitoring.md) for diagnosis and replay.

## Upload Announce Message (`platform.upload.announce`)

This message is produced by the Koku ROS report shipper (`ros_report_shipper.py`) after it uploads ROS-specific CSV files to S3.

### Schema

```json
{
  "account": "10001",
  "org_id": "1234567",
  "category": "ros",
  "request_id": "abc123-...",
  "b64_identity": "<base64 encoded identity header>",
  "url": "https://s3.example.com/bucket/path/to/ros_data.csv",
  "timestamp": "2026-05-15T10:30:00Z"
}
```

### Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `account` | string | No | Legacy account number (deprecated, use org_id) |
| `org_id` | string | Yes | Organization identifier (tenant) |
| `category` | string | Yes | Must be `"ros"` for ROS processing |
| `request_id` | string | Yes | Correlation ID for tracing |
| `b64_identity` | string | Yes | Base64-encoded identity header for downstream auth |
| `url` | string | Yes | Pre-signed S3 URL for the CSV payload |
| `timestamp` | string | No | ISO 8601 upload timestamp |

### Processing Flow

1. Consumer receives message on `platform.upload.announce`
2. Filters by `category == "ros"` (ignores other categories)
3. Downloads CSV from `url` using pre-signed URL
4. Parses CSV rows into metric samples (`librobne/csv.ForEachRow` / `ForEachNamespace` / `ForEachPVC` / `ForEachVM` / `ForEachVMPVC` / `ForEachVMGPU` / `ForEachSnapshot` via `internal/ingestion`)
5. Computes daily digests and recommendations
6. Persists results to PostgreSQL

## Sources Event Stream (`platform.sources.event-stream`)

Used by the housekeeper to detect source deletions and clean up orphaned data.

### Schema

```json
{
  "event_type": "Sources.destroy",
  "payload": {
    "id": 12345,
    "source_type_id": 1,
    "name": "My OCP Cluster",
    "uid": "cluster-uuid-here"
  }
}
```

## CSV Payload Format

The actual data payload (downloaded from the S3 URL) is a CSV file with headers
matching the koku-metrics-operator's `rosContainerRow.csvHeader()` output.

**Source of truth:** `koku-metrics-operator/internal/collector/types.go`

**Contract test:** `librobne/csv/csv_contract_test.go` (container header); `internal/ingestion/csv_contract_test.go` (cluster quota)

### ROS Container CSV Columns

The parser (`librobne/csv` → `buildColumnIndex` / `ForEachRow`) recognizes
the following columns. **Required** columns must be present or parsing fails.
**Optional** columns default to zero/empty when absent.

#### Required Columns

| Column | Type | Unit | Description |
|--------|------|------|-------------|
| `interval_start` | timestamp | UTC | Start of the metrics collection interval |
| `interval_end` | timestamp | UTC | End of the metrics collection interval |
| `namespace` | string | — | Kubernetes namespace |
| `workload` | string | — | Owner workload name (Deployment, StatefulSet, etc.) |
| `workload_type` | string | — | Owner kind (`Deployment`, `StatefulSet`, `DaemonSet`, etc.) |
| `container_name` | string | — | Container name within the pod |
| `pod` | string | — | Pod name |
| `cpu_request_container_avg` | float | cores | Average CPU request across the interval |
| `cpu_usage_container_avg` | float | cores | Average CPU usage across the interval |
| `memory_request_container_avg` | float | bytes | Average memory request |
| `memory_usage_container_avg` | float | bytes | Average memory usage |

#### Optional Columns (Core Metrics)

| Column | Type | Unit | Description |
|--------|------|------|-------------|
| `node` | string | — | Node the pod ran on |
| `node_capacity_cpu_cores` | float | cores | Node total CPU capacity |
| `node_capacity_memory_bytes` | float | bytes | Node total memory capacity |
| `cpu_limit_container_avg` | float | cores | Average CPU limit |
| `cpu_throttle_container_avg` | float | cores | Average CPU throttle time |
| `memory_limit_container_avg` | float | bytes | Average memory limit |
| `memory_rss_usage_container_avg` | float | bytes | Average RSS memory usage |
| `oom_count` | float | count | Number of OOM kills in the interval |
| `workload_pod_count` | float | count | Number of pods in the workload |
| `desired_replicas` | float | count | Desired replica count (from HPA/spec) |
| `available_replicas` | float | count | Available (ready) replicas |

#### Optional Columns (GPU / Accelerator)

| Column | Type | Unit | Description |
|--------|------|------|-------------|
| `accelerator_model_name` | string | — | GPU model (e.g., "NVIDIA A100-SXM4-40GB") |
| `accelerator_profile_name` | string | — | MIG profile name if applicable |
| `accelerator_frame_buffer_usage_min` | float | 0–1 | Min GPU frame buffer (VRAM) utilization |
| `accelerator_frame_buffer_usage_max` | float | 0–1 | Max GPU frame buffer utilization |
| `accelerator_frame_buffer_usage_avg` | float | 0–1 | Avg GPU frame buffer utilization |
| `tensor_pipe_active_min` | float | 0–1 | Min tensor core pipeline activity |
| `tensor_pipe_active_max` | float | 0–1 | Max tensor core pipeline activity |
| `tensor_pipe_active_avg` | float | 0–1 | Avg tensor core pipeline activity |
| `dram_active_min` | float | 0–1 | Min DRAM (HBM) bandwidth utilization |
| `dram_active_max` | float | 0–1 | Max DRAM bandwidth utilization |
| `dram_active_avg` | float | 0–1 | Avg DRAM bandwidth utilization |
| `sm_active_min` | float | 0–1 | Min streaming multiprocessor activity |
| `sm_active_max` | float | 0–1 | Max streaming multiprocessor activity |
| `sm_active_avg` | float | 0–1 | Avg streaming multiprocessor activity |

#### Columns Present in Operator Output But Ignored by ROS

These columns are produced by the operator but not consumed by ROS:

| Column | Reason Ignored |
|--------|----------------|
| `report_period_start` | Report metadata, not per-sample data |
| `report_period_end` | Report metadata |
| `owner_name` | Redundant with `workload` |
| `owner_kind` | Redundant with `workload_type` |
| `image_name` | Not used in recommendations |
| `resource_id` | Not used in recommendations |
| `cpu_request_container_sum` | ROS uses `_avg` variants only |
| `cpu_limit_container_sum` | ROS uses `_avg` variants only |
| `cpu_usage_container_min` | ROS uses `_avg` variants only |
| `cpu_usage_container_max` | ROS uses `_avg` variants only |
| `cpu_usage_container_sum` | ROS uses `_avg` variants only |
| `cpu_throttle_container_max` | ROS uses `_avg` variant only |
| `cpu_throttle_container_min` | ROS uses `_avg` variant only |
| `cpu_throttle_container_sum` | ROS uses `_avg` variant only |
| `memory_request_container_sum` | ROS uses `_avg` variants only |
| `memory_limit_container_sum` | ROS uses `_avg` variants only |
| `memory_usage_container_min` | ROS uses `_avg` variants only |
| `memory_usage_container_max` | ROS uses `_avg` variants only |
| `memory_usage_container_sum` | ROS uses `_avg` variants only |
| `memory_rss_usage_container_min` | ROS uses `_avg` variant only |
| `memory_rss_usage_container_max` | ROS uses `_avg` variant only |
| `memory_rss_usage_container_sum` | ROS uses `_avg` variant only |

### Timestamp Format

The parser accepts multiple timestamp formats via `parseFlexibleTimestamp()`
(defined in `internal/ingestion/pvc.go`):

- `2006-01-02 15:04:05 +0000 UTC` — Go default `.String()` format (operator output)
- `2006-01-02 15:04:05 -0700 MST` — Go format with named timezone
- `2006-01-02 15:04:05+00:00` — datetime with numeric offset, no `T` separator
- `2006-01-02T15:04:05Z07:00` — RFC 3339 (Nise output)

### Unit Conversions

| CSV Column Unit | Internal Unit | Conversion |
|-----------------|---------------|------------|
| cores (float) | millicores (int64) | `× 1000`, rounded |
| bytes (float) | kibibytes (int64) | `÷ 1024`, rounded |
| 0–1 ratio (float) | 0–1 ratio (float64) | passthrough |
| count (float) | count (int64) | rounded |

## Contract Between Koku and ROS

1. **Koku** receives tarball from the metrics operator via `/api/ingress/v1/upload`
2. **Koku** extracts ROS-relevant files (listed in manifest's `resource_optimization_files`)
3. **Koku** uploads individual CSV files to S3 (koku-bucket)
4. **Koku** sends an announce message to Kafka with the S3 pre-signed URL
5. **ROS** downloads and processes the CSV

### Critical Assumptions

- CSV column headers must match exactly (operator → Koku pass-through → ROS parser)
- `org_id` in the Kafka message must match the tenant's schema in ROS's database
- Pre-signed URLs must be valid for at least the consumer's processing time (~60s)
- `category: "ros"` is the routing key — other categories are ignored
