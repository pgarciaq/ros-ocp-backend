# Cost Data Integration Contract

This document describes the integration between ROS-OCP-Backend and Koku for cost/savings estimation.

## Overview

ROS-OCP-Backend fetches cost model rates from Koku to compute estimated monthly savings for each recommendation. The integration uses Koku's internal `effective_rates` endpoint.

## Endpoint

```
GET {KOKU_MASU_URL}/api/cost-management/v1/effective_rates/
```

### Query Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `org_id` | string | Organization ID (without `org` prefix) |
| `cluster_id` | string | Cluster UUID |
| `start_date` | string | Start date (YYYY-MM-DD, UTC) |
| `end_date` | string | End date (YYYY-MM-DD, UTC) |

### Response Schema

```json
{
  "cluster_id": "abc123-...",
  "provider_uuid": "def456-...",
  "distribution_type": "cpu",
  "markup_pct": 10.0,
  "configured_rates": {
    "cpu_core_usage_per_hour": {
      "infrastructure": 0.0,
      "supplementary": 0.007
    },
    "cpu_core_request_per_hour": {
      "infrastructure": 0.0,
      "supplementary": 0.2
    },
    "memory_gb_usage_per_hour": {
      "infrastructure": 0.0,
      "supplementary": 0.009
    },
    "memory_gb_request_per_hour": {
      "infrastructure": 0.0,
      "supplementary": 0.05
    },
    "node_cost_per_month": {
      "infrastructure": 1000.0,
      "supplementary": 0.0
    },
    "gpu_cost_per_hour": {
      "infrastructure": 2.50,
      "supplementary": 0.0
    }
  },
  "namespace_aggregates": {
    "my-namespace": {
      "cost_model_cpu_cost": 150.25,
      "cost_model_memory_cost": 80.50,
      "infrastructure_cost": 500.00,
      "distributed_cost": 200.00,
      "cpu_usage_hours": 720.0,
      "cpu_request_hours": 1440.0,
      "mem_usage_hours": 360.0,
      "mem_request_hours": 720.0
    }
  }
}
```

## How ROS Uses Cost Data

### Container Savings

1. Fetch effective rates for the cluster's most recent billing period
2. Compute per-core-hour and per-GiB-hour unit costs from `configured_rates`
3. Calculate savings = `(current_request - recommended_request) × unit_cost × hours_per_month`
4. Apply markup percentage
5. Include distributed costs (platform/worker) proportionally

### GPU Savings

1. Use `gpu_cost_per_hour` from configured rates (or infrastructure node cost ÷ GPUs per node)
2. For idle GPUs: savings = full GPU hourly rate × hours/month
3. For MIG candidates: savings = (1 - profile_fraction) × GPU hourly rate × hours/month
4. For time-slicing: savings = (candidates - 1) / candidates × node GPU cost

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `KOKU_MASU_URL` | `""` | Koku masu API base URL (e.g., `http://cost-onprem-masu:5042`) |
| `GLOBAL_HTTP_CLIENT_TIMEOUT_SECS` | 30 | HTTP timeout for cost data requests |

When `KOKU_MASU_URL` is empty, a `NilCostDataProvider` is used — all savings values are $0.00, and `NotifNoCostData` notification is appended.

## Error Handling

| Scenario | Behavior |
|----------|----------|
| Koku unreachable | Log warning, use NilCostDataProvider for this cycle |
| Non-200 response | Log error with status + body, skip savings for this cluster |
| JSON decode failure | Log error, skip savings for this cluster |
| Empty configured_rates | Savings computed as $0.00 (no cost model assigned) |

## Authentication

The `effective_rates` endpoint is an **internal masu API** endpoint — it does NOT require `x-rh-identity` authentication. It is only accessible within the cluster network (service-to-service communication). In on-prem deployments, network policies restrict access.

## Freshness

Cost data is fetched once per ingestion cycle per cluster. The date range covers the most recent 30 days to capture the current billing period's rates. Rate changes in Koku (e.g., cost model updates) are reflected on the next ingestion cycle.
