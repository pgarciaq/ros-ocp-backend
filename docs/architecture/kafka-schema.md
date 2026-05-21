# Kafka Message Schema

This document describes the Kafka message formats used by ROS-OCP-Backend.

## Topics

| Topic | Direction | Purpose |
|-------|-----------|---------|
| `platform.upload.announce` | Consume | Notification of new OCP report uploads |
| `platform.sources.event-stream` | Consume | Source lifecycle events (create/update/delete) |

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
4. Parses CSV rows into metric samples (`internal/ingestion/csvparser.go`)
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

The actual data payload (downloaded from the S3 URL) is a CSV file with headers matching the koku-metrics-operator output:

### Container/Pod CSV (`ros_usage`)

```csv
report_period_start,report_period_end,interval_start,interval_end,namespace,pod,container,node,cpu_request_mc,cpu_limit_mc,cpu_usage_mc_p50,...,mem_request_kib,mem_limit_kib,...,desired_replicas,available_replicas
```

Key columns: CPU percentiles (p50, p90, p95, p98, p99, max), memory percentiles, OOM kill count, GPU metrics (when present), replica counts.

### Node Capacity CSV (`node_labels`)

```csv
interval_start,interval_end,node,node_capacity_cpu_cores,node_capacity_memory_bytes,...
```

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
