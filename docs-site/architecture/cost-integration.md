# Koku Integration Contract

This document describes integration between ROS-OCP-Backend and Koku: **data ingestion**,
**tag filtering**, **cost/savings estimation** (`effective_rates`), **savings recalculation**,
and **business-hours reship** (`reship_ros`).

For recommendation thresholds and term configuration (not cost-specific), see
[Recommendation Engine Reference](recommendation-engines.md).

## Overview

ROS-OCP-Backend fetches cost model rates from Koku to compute estimated monthly savings for each recommendation. The integration uses Koku's internal `effective_rates` endpoint.

**Cost rate source:** Rates are sourced exclusively from Koku cost models via the
`effective_rates` API. ROS does not provide a standalone cost-rate configuration
endpoint. To change rates, update the cost model in Koku (which triggers rate refresh
on the next ingestion cycle or via [savings recalculation](#savings-recalculation-after-cost-model-changes)
when configured).

## Integration map

| Integration | Direction | Contract | ROS implementation |
|-------------|-----------|----------|-------------------|
| **Data ingestion** | Koku → ROS (Kafka + S3) | [Data ingestion](#data-ingestion-contract) · [`kafka-schema.md`](kafka-schema.md) | `internal/kafka/consumer.go`, `internal/services/report_processor.go` |
| **Effective rates** | ROS → Koku (HTTP GET) | [Effective rates](#effective-rates-endpoint) | [`internal/costdata/provider.go`](../../internal/costdata/provider.go) |
| **Savings recalculation** | Koku → ROS (HTTP POST) | [Savings recalculation](#savings-recalculation-after-cost-model-changes) | [`internal/engine/savings_recalculate.go`](../../internal/engine/savings_recalculate.go) |
| **Tag filtering** | DB (on-prem) or HTTP push (SaaS) | [Tag filtering](#tag-filtering-enabled-tag-keys) | [`internal/tags/`](../../internal/tags/) |
| **Business-hours reship** | ROS → Koku (HTTP POST) | [Reship](#business-hours-reship-reship_ros) | [`internal/reship/`](../../internal/reship/) |

---

## Data ingestion contract

ROS does **not** read Koku PostgreSQL line-item tables for recommendations. Usage data flows through the **same operator upload pipeline** as Cost Management, with ROS-specific CSVs shipped separately.

### Pipeline

1. **koku-metrics-operator** uploads a tarball to ingress (`/api/ingress/v1/upload`) containing `manifest.json` with `resource_optimization_files` (ROS container, namespace, GPU, storage, VM, quota CSVs).
2. **Koku listener** processes cost CSVs; `ROSReportShipper` copies ROS files to the ROS S3 bucket and publishes one Kafka announce per file.
3. **ROS processor** consumes `platform.upload.announce`, filters `category == "ros"`, downloads the presigned CSV URL, parses rows, and writes digests/recommendations to the **ROS PostgreSQL** database.

ROS and Koku use **separate databases**. The only shared persistence in on-prem **advanced** mode is PostgreSQL **instance** access for tag filtering (`ROS_TAGS_SOURCE=db`). The cost-onprem chart defaults to `api` (push sync into `org_container_keys.resolved_tags`).

### Kafka message shape

Documented in [`kafka-schema.md`](kafka-schema.md). Reship uses the same Kafka payload as initial upload (see [Business-hours reship](#business-hours-reship-reship_ros)).

### Manifest contract

| Field | ROS uses |
|-------|----------|
| `resource_optimization_files[]` | Filenames Koku ships to ROS S3 (container, namespace, GPU, storage, VM, quota CSVs) |
| `files[]` | Koku cost pipeline only (pod/node labels, etc.) — not consumed by ROS |

CSV column headers must match koku-metrics-operator output ([`internal/ingestion/csv_contract_test.go`](../../internal/ingestion/csv_contract_test.go)).

---

## Tag filtering (enabled tag keys)

ROS recommendation list APIs support `filter[tag:key]=value`. Tag **keys** are governed by Koku's **enabled tag keys** (`reporting_enabledtagkeys`, managed via Cost Management **Settings → Tags**). Tags do **not** flow through `effective_rates` or the ingestion Kafka message; they are a parallel concern.

| Deployment | Chart default | Binary default | How tags reach ROS |
|------------|---------------|----------------|-------------------|
| **On-prem** (cost-onprem chart) | `api` | `db` | **api:** Koku Celery `sync_ros_ocp_tags` POSTs to ROS `POST /api/cost-management/v1/internal/tags/sync` (chart default). **db (advanced):** ROS JOINs Koku tenant tables `reporting_enabledtagkeys` + `reporting_ocptags_values` at query time on a shared PostgreSQL instance — no HTTP sync |
| **SaaS** | `api` | `api` | Same push path as on-prem `api` mode |

---

## Effective rates endpoint

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
  "currency": "USD",
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
    "gpu_cost_per_month": {
      "infrastructure": 1800.0,
      "supplementary": 0.0
    },
    "storage_gb_request_per_month": {
      "infrastructure": 0.0,
      "supplementary": 0.01
    },
    "storage_gb_usage_per_month": {
      "infrastructure": 0.0,
      "supplementary": 0.01
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

1. Fetch effective rates once per ingestion cycle per cluster
2. Look up per-namespace aggregates in `namespace_aggregates` (includes `infrastructure_cost` from OCP-on-cloud correlation when available)
3. Compute savings in [`ApplySavingsEstimates()`](../../internal/engine/savings.go) and persist `estimated_savings_cents` on ingest

### Node Savings

Computed at **ingestion** (same cycle as container recommendations). Uses `cpu_core_usage_per_hour`, `memory_gb_usage_per_hour`, and `node_cost_per_month` from `configured_rates`.

### PVC Savings

Uses `storage_gb_request_per_month` (fallback: `storage_gb_usage_per_month`) from `configured_rates`.

### GPU Savings

Uses `gpu_cost_per_month` from `configured_rates` for MIG and idle GPU savings.

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `KOKU_MASU_URL` | `""` | Koku masu API base URL (e.g., `http://cost-onprem-masu:5042`) |
| `ROS_SAVINGS_ESTIMATES_ENABLED` | `true` | Kill-switch — set `false` to skip all Masu cost fetches |
| `GLOBAL_HTTP_CLIENT_TIMEOUT_SECS` | 30 | HTTP timeout for cost data requests |

## Error Handling

| Scenario | Behavior |
|----------|----------|
| `ROS_SAVINGS_ESTIMATES_ENABLED=false` | No Masu HTTP calls; ingestion savings `$0`; `NotifNoCostData` on container, node, PVC |
| `KOKU_MASU_URL` empty | Same as kill-switch — `NilCostDataProvider` |
| Koku/Masu unreachable | Log warning, use `NilCostDataProvider` for this cycle |
| Non-200 response | Log error with status + body, skip savings for this cluster |
| JSON decode failure | Log error, skip savings for this cluster |
| Empty `configured_rates` | Savings computed as `$0.00` (no cost model assigned) |
| Namespace missing from aggregates | Container savings `$0`, `NotifNoCostData` for that workload |

## Authentication

The `effective_rates` endpoint is an **internal masu API** endpoint — it does NOT require `x-rh-identity` authentication. It is only accessible within the cluster network (service-to-service communication).

## Savings recalculation after cost model changes

When Koku finishes applying cost model rates, the masu cost model updater notifies ROS to refresh **persisted dollar savings only** — recommendation sizing and classification are unchanged.

### Endpoint

```
POST {ROS_API}/api/cost-management/v1/internal/recalculate-savings
```

### Request body

| Field | Required | Description |
|-------|----------|-------------|
| `org_id` | Yes | Organization ID **without** the `org` schema prefix (e.g. `1234567`) |
| `cluster_uuid` | No | Scope recalculation to one cluster (Koku provider UUID) |
| `recommendation_types` | No | Subset of `container`, `node`, `pvc`, `quota`, `cluster-quota` (default: all five) |

### Response

`202 Accepted` with `status: "accepted"`. Work runs asynchronously in a background goroutine.

## Business-hours reship (`reship_ros`)

When an administrator changes a business-hours schedule, ROS must rebuild historical `business_hours` digests from stored ROS CSVs. ROS triggers Koku Masu to **re-publish** existing S3 objects to Kafka (no re-upload from the cluster).

### Endpoint

```
POST {KOKU_MASU_URL}/api/cost-management/v1/reship_ros/
```

### Query parameters

| Parameter | Required | Description |
|-----------|----------|-------------|
| `schema` | Yes | Tenant schema (e.g. `org1234567`) |
| `provider_uuid` | Yes | Koku provider UUID |
| `start_date` | Yes | Inclusive start date `YYYY-MM-DD` |
| `end_date` | Yes | Inclusive end date `YYYY-MM-DD` |

### Authentication

Internal Masu API — **no** `x-rh-identity` required (service-to-service, same as `effective_rates`).

## Multi-currency savings conversion

Savings amounts are stored in the cost model currency (typically USD). When the user has
configured a different display currency in Koku, all `MoneyAmount` fields in API responses
are converted at response time.

### Endpoints

#### `GET {KOKU_MASU_URL}/api/cost-management/v1/user_currency/?org_id=<org_id>`

Returns the user's preferred display currency.

| Parameter | Required | Description |
|-----------|----------|-------------|
| `org_id` | Yes | Organization ID (without `org` prefix) |

Response: `{"currency": "EUR"}` (or `"USD"` when unset).

#### `GET {KOKU_MASU_URL}/api/cost-management/v1/exchange_rate/?schema=<schema>&from=<from>&to=<to>`

Returns a single exchange rate pair.

| Parameter | Required | Description |
|-----------|----------|-------------|
| `schema` | Yes | Tenant schema (e.g. `org1234567`) |
| `from` | Yes | Source currency code |
| `to` | Yes | Target currency code |

Response: `{"from_currency": "USD", "to_currency": "EUR", "rate": "0.92"}` (rate is `null` when unavailable).

### Conversion flow

1. Resolve stored currency from `effective_rates` response.
2. Resolve user currency via `user_currency/` (cached 1 hour per org_id).
3. Fetch exchange rate via `exchange_rate/` (cached 1 hour per org_id+pair).
4. Convert: multiply stored cents by rate, round half-up at cent boundary.
5. If rate is 1.0 and stored != user, fall back to stored currency (graceful degradation).

### Authentication

Internal Masu API — **no** `x-rh-identity` required.
