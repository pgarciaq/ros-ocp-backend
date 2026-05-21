# Contributing to ros-ocp-backend

## What is ros-ocp-backend?

ros-ocp-backend is the **Resource Optimization for OpenShift** backend service.
It analyzes container, GPU, node, namespace, PVC, and snapshot metrics from
OpenShift clusters and produces rightsizing recommendations — "you're over-requesting
CPU here", "this GPU is idle", "this node is under-utilized".

It's part of Red Hat's Cost Management ecosystem:

```
┌─────────────────────┐     ┌─────────────────────┐     ┌────────────────┐
│ OpenShift Cluster   │     │  Koku (cost-mgmt)   │     │  koku-ui       │
│                     │     │                     │     │  (React)       │
│ koku-metrics-       │────▶│  Ingestion pipeline │     │                │
│ operator            │     │  Cost models        │────▶│  Cost views    │
│ (collects metrics,  │     │  Reports API        │     │  Optimizations │
│  produces CSVs)     │     └──────────┬──────────┘     └────────────────┘
└─────────┬───────────┘                │                         ▲
          │                            │ effective_rates          │
          │ tar.gz (CSVs)              ▼                         │
          │                   ┌─────────────────────┐            │
          └──────────────────▶│  ros-ocp-backend    │────────────┘
            via Kafka/S3      │  (this service)     │
                              │                     │
                              │  Recommendations    │
                              │  API                │
                              └─────────────────────┘
```

**Koku tells you what you're spending. ros-ocp-backend tells you what you could save.**

---

## Architecture Overview

### Services (Processes)

ros-ocp-backend runs as 4 separate processes (same binary, different subcommands):

| Process | Command | Purpose |
|---------|---------|---------|
| **Processor** | `rosocp start processor` | Consumes Kafka messages, downloads CSVs, parses metrics, computes digests |
| **Recommendation Poller** | `rosocp start recommendation-poller` | Computes recommendations from digests on schedule |
| **API Server** | `rosocp start api` | Serves REST API for the frontend |
| **Housekeeper** | `rosocp start housekeeper` | Listens for source deletions, manages partitions |

### Data Flow

```
1. koku-metrics-operator collects Prometheus metrics → packages as CSVs → uploads tar.gz
2. Koku (ingress) stores the tar.gz in S3, publishes Kafka message
3. Processor consumes Kafka msg → downloads CSV from S3 → parses rows → upserts digests
4. Recommendation Poller reads digests → runs recommendation engine → persists recommendations
5. API Server reads recommendations from PostgreSQL → serves to frontend
```

### Key Packages

```
internal/
├── api/            # Echo HTTP handlers, middleware, serialization
├── config/         # Viper-based configuration (env vars, defaults)
├── costdata/       # HTTP client for Koku's effective_rates endpoint
├── db/             # pgxpool setup, connection management
├── engine/         # Recommendation math: percentiles, decay, GPU classification,
│                   #   node sizing, cost savings, retention sweeps
├── ingestion/      # CSV parsing, digest computation pipeline
├── kafka/          # Kafka consumer (confluent-kafka-go)
├── logging/        # Structured logging (logrus + WithFields)
├── metrics/        # Prometheus metric definitions
├── model/          # Database models and query builders
├── notifications/  # Notification code registry
├── plugin/         # Plugin registry (interfaces, init, enable/disable)
├── plugins/        # Plugin implementations:
│   ├── container/  #   Container CPU/memory recommendations
│   ├── gpu/        #   GPU MIG + time-slicing recommendations
│   ├── node/       #   Node utilization recommendations
│   ├── namespace/  #   Namespace-level aggregates
│   ├── pvc/        #   PVC storage recommendations
│   ├── snapshot/   #   VolumeSnapshot staleness detection
│   └── kruize/     #   Legacy Kruize delegation (deprecated)
├── rbac/           # Platform RBAC integration
├── services/       # Report processing orchestration
│   └── housekeeper/# Source deletion cleanup, partition management
├── testutil/       # Test database setup, fixtures
└── types/          # Shared type definitions
```

### Plugin Architecture

Plugins are **compile-time, in-process** Go interfaces toggled at runtime via env vars.
No dynamic loading — all plugins ship in the same binary.

Plugin interfaces:
- **`CSVIngestor`** — owns CSV parsing for a payload type
- **`IngestHook`** — runs after CSV parsing (e.g., GPU digest upserts)
- **`APIProvider`** — registers HTTP routes
- **`APIEnricher`** — enriches another plugin's API responses
- **`RetentionProvider`** — owns data retention sweeps for its tables

See [`docs/architecture/plugin-architecture.md`](docs/architecture/plugin-architecture.md)
for full design details.

---

## Local Development Setup

### Prerequisites

- **Go 1.25+** (see `go.mod` for exact version)
- **PostgreSQL 16** (direct install, Docker, or Podman)
- **Kafka** (via docker-compose or Podman)
- **Docker or Podman** (for infrastructure services)

### Quick Start

```bash
# 1. Start infrastructure (Kafka + PostgreSQL + topics)
docker compose -f scripts/docker-compose.yml up -d kafka db-ros kafka-create-topics

# 2. Run database migrations
go run rosocp.go db migrate up

# 3. Start the API server (in one terminal)
PROMETHEUS_PORT=5007 go run rosocp.go start api

# 4. Start the processor (in another terminal)
PROMETHEUS_PORT=5005 go run rosocp.go start processor

# 5. Start the recommendation poller (in another terminal)
PROMETHEUS_PORT=5006 go run rosocp.go start recommendation-poller
```

### Using Makefile Shortcuts

```bash
make run-api-server            # Start API (port 8000)
make run-processor             # Start processor
make run-recommendation-poller # Start recommendation poller
make build                     # Build binary → bin/rosocp
make test                      # Run all tests with -race
make lint                      # Run golangci-lint
make db-migrate                # Run migrations
```

### Docker Compose Services

The `scripts/docker-compose.yml` provides:

| Service | Port | Purpose |
|---------|------|---------|
| `kafka` | 29092 | Message broker |
| `zookeeper` | 32181 | Kafka coordination |
| `db-ros` | 15432 | PostgreSQL for ros-ocp-backend |
| `db-kruize` | — | PostgreSQL for Kruize (legacy) |
| `kruize-autotune` | 8080 | Kruize engine (legacy) |
| `ingress` | 3000 | Insights ingress (file upload → S3 → Kafka) |
| `sources-api-go` | 8002 | Sources API |
| `unleash-edge` | 3063 | Feature flags (offline mode) |
| `nginx` | 8888 | Serves sample CSV files for testing |

### Sending Test Data

```bash
# Upload a sample CSV message to Kafka (uses nginx to serve the file)
make upload-msg-to-rosocp

# Or use the sample tar.gz through ingress
curl -F "file=@scripts/samples/cost-mgmt.tar.gz;type=application/vnd.redhat.hccm.tar+tgz" \
  -H "x-rh-identity: $(echo -n '{"identity":{"org_id":"3340851","type":"System","auth_type":"cert-auth","system":{"cn":"1b36b20f-7fa0-4454-a6d2-008294e06378","cert_type":"system"},"internal":{"org_id":"3340851"}}}' | base64 -w0)" \
  http://localhost:3000/api/ingress/v1/upload
```

### Querying the API

```bash
IDENTITY=$(echo -n '{"identity":{"org_id":"3340851","type":"System","auth_type":"cert-auth","system":{"cn":"1b36b20f-7fa0-4454-a6d2-008294e06378","cert_type":"system"},"internal":{"org_id":"3340851"}}}' | base64 -w0)

# List container recommendations
curl -H "x-rh-identity: $IDENTITY" \
  http://localhost:8000/api/cost-management/v1/recommendations/openshift | python3 -m json.tool

# List GPU recommendations
curl -H "x-rh-identity: $IDENTITY" \
  http://localhost:8000/api/cost-management/v1/recommendations/openshift/gpu | python3 -m json.tool

# List node recommendations
curl -H "x-rh-identity: $IDENTITY" \
  http://localhost:8000/api/cost-management/v1/recommendations/openshift/nodes | python3 -m json.tool
```

---

## Configuration Reference

All configuration is via environment variables, loaded by `internal/config/config.go`
using Viper. When running under ClowdApp (production), many values are injected
automatically from the Clowder config.

### Core Settings

| Variable | Default | Description |
|----------|---------|-------------|
| `LOG_LEVEL` | `info` | Log level (debug, info, warn, error) |
| `API_PORT` | `8000` | API server listen port |
| `PROMETHEUS_PORT` | `9000` | Prometheus metrics port |
| `READ_HEADER_TIMEOUT` | `5` | HTTP read header timeout (seconds) |

### Database

| Variable | Default | Description |
|----------|---------|-------------|
| `DB_HOST` | `localhost` | PostgreSQL host |
| `DB_PORT` | `15432` | PostgreSQL port |
| `DB_NAME` | `postgres` | Database name |
| `DB_USER` | `postgres` | Database user |
| `DB_PASSWORD` | `postgres` | Database password |
| `DB_SSL` | `disable` | SSL mode |
| `ROS_DB_MAX_CONNS` | `10` | pgxpool max connections |
| `ROS_DB_ACQUIRE_TIMEOUT_SECS` | `5` | Connection acquire timeout |

### Kafka

| Variable | Default | Description |
|----------|---------|-------------|
| `KAFKA_BOOTSTRAP_SERVERS` | `localhost:29092` | Kafka broker addresses |
| `KAFKA_CONSUMER_GROUP_ID` | `ros-ocp` | Consumer group |
| `KAFKA_AUTO_COMMIT` | `true` | Auto-commit offsets |
| `UPLOAD_TOPIC` | `platform.upload.announce` | Upload notification topic |
| `RECOMMENDATION_TOPIC` | `rosocp.kruize.recommendations` | Recommendation trigger topic |
| `SOURCES_EVENT_TOPIC` | `platform.sources.event-stream` | Source lifecycle events |

### Recommendation Engine

| Variable | Default | Description |
|----------|---------|-------------|
| `ROS_RETENTION_MONTHS` | `6` | Data retention period |
| `ROS_MAX_LOOKBACK_DAYS` | `90` | Max lookback for recommendations |
| `ROS_HISTORY_RETENTION_DAYS` | `90` | History data retention |
| `ROS_STALENESS_THRESHOLD_HOURS` | `72` | Hours before marking stale |
| `ROS_STALE_ARCHIVE_DAYS` | `30` | Days before deleting stale recs |
| `ROS_OOM_BASE_BUMP` | `0.15` | OOM memory bump factor (15%) |
| `ROS_OOM_MAX_BUMP` | `1.60` | Max OOM bump cap (160%) |

### GPU Thresholds

| Variable | Default | Description |
|----------|---------|-------------|
| `ROS_GPU_IDLE_THRESHOLD` | `0.02` | SM activity below this = idle |
| `ROS_GPU_UNDERUTILIZED_SM_THRESHOLD` | `0.25` | SM threshold for underutilized |
| `ROS_GPU_UNDERUTILIZED_TENSOR_THRESHOLD` | `0.15` | Tensor threshold for underutilized |
| `ROS_GPU_MEMBOUND_DRAM_THRESHOLD` | `0.60` | DRAM threshold for memory-bound |
| `ROS_GPU_MEMBOUND_TENSOR_THRESHOLD` | `0.15` | Tensor threshold for memory-bound |
| `ROS_GPU_FB_HEADROOM_FACTOR` | `1.20` | FB headroom multiplier for MIG sizing |

### Node Recommendations

| Variable | Default | Description |
|----------|---------|-------------|
| `ROS_NODE_UNDERUTIL_THRESHOLD` | `0.30` | Below this = underutilized |
| `ROS_NODE_OVERCOMMIT_THRESHOLD` | `1.50` | Above this = overcommitted |
| `ROS_NODE_ALLOCATABLE_FACTOR` | `0.93` | Allocatable fraction of capacity |
| `ROS_NODE_STRANDED_IMBALANCE_THRESHOLD` | `0.6` | CPU/memory imbalance threshold |
| `ROS_NODE_EMA_ALPHA` | `0.3` | EMA smoothing factor |

### Snapshot Detection

| Variable | Default | Description |
|----------|---------|-------------|
| `ROS_SNAPSHOT_ORPHAN_AGE_DAYS` | `7` | PVC-less snapshot age threshold |
| `ROS_SNAPSHOT_NEVER_RESTORED_DAYS` | `30` | Never-restored age threshold |
| `ROS_SNAPSHOT_STALE_DAYS` | `90` | General staleness threshold |
| `ROS_SNAPSHOT_REDUNDANT_THRESHOLD` | `3` | Max snapshots per PVC |
| `ROS_SNAPSHOT_COST_PER_GIB_MONTH` | `0.05` | Cost estimate ($/GiB/month) |

### Plugins

| Variable | Default | Description |
|----------|---------|-------------|
| `ROS_ENABLED_PLUGINS` | (all native) | Allowlist of active plugins |
| `ROS_DISABLED_PLUGINS` | (none) | Blocklist of disabled plugins |
| `ROS_USE_NATIVE_ENGINE` | `true` | Deprecated: use `ROS_ENABLED_PLUGINS=kruize` |

### Cost Integration

| Variable | Default | Description |
|----------|---------|-------------|
| `KOKU_MASU_URL` | — | Koku masu API URL for effective_rates |

### RBAC

| Variable | Default | Description |
|----------|---------|-------------|
| `RBAC_ENABLE` | `false` (local) | Enable platform RBAC |
| `RBACHost` | `localhost` | RBAC service host |
| `RBACPort` | `9080` | RBAC service port |

---

## Data Ingestion

### What the Operator Sends

The koku-metrics-operator collects Prometheus metrics from each OpenShift cluster,
aggregates them into hourly CSV reports, packages them as a tar.gz, and uploads
to the platform ingress service.

### CSV Report Types

| File Pattern | Contents | Used By |
|--------------|----------|---------|
| `ros_ocp_usage.csv` | Container CPU/memory request/limit/usage per 15min interval | Container plugin |
| `ros_ocp_namespace.csv` | Namespace-level aggregates | Namespace plugin |
| `ros_ocp_storage.csv` | PVC capacity/request/usage | PVC plugin |
| `ros_ocp_snapshot.csv` | VolumeSnapshot inventory | Snapshot plugin |

### CSV Schema (Container — `ros_ocp_usage.csv`)

```
interval_start,interval_end,namespace,pod,workload,workload_type,container_name,
cpu_request_container_avg,cpu_limit_container_avg,cpu_usage_container_avg,cpu_throttle_container_avg,
memory_request_container_avg,memory_limit_container_avg,memory_usage_container_avg,memory_rss_usage_container_avg,oom_count
```

### Processing Pipeline

1. **Kafka consumer** receives `platform.upload.announce` message with `category: "ros"`
2. **Download** CSV from pre-signed S3 URL
3. **Parse** CSV rows into typed `MetricRow` structs (`internal/ingestion/csvparser.go`)
4. **Digest** rows into daily aggregates (percentiles, min/max/avg per container per day)
5. **Upsert** digests into PostgreSQL (`daily_container_digests`, `gpu_container_digests`, etc.)
6. **Recommend** (poller or inline): read digests, apply decay-weighted percentiles, produce recommendations
7. **Persist** recommendations to `recommendation_sets` / `node_recommendations` / etc.

---

## Database

### Migrations

Migrations are in `migrations/` using [golang-migrate](https://github.com/golang-migrate/migrate):

```bash
# Apply all pending migrations
go run rosocp.go db migrate up

# Roll back one migration
go run rosocp.go db migrate down 1

# Check current version
go run rosocp.go db migrate version
```

### Key Tables

| Table | Purpose |
|-------|---------|
| `rh_accounts` | Tenant registry (org_id → account metadata) |
| `clusters` | Cluster registry (UUID, alias, source_id) |
| `workloads` | Workload registry (deployment/statefulset/etc.) |
| `recommendation_sets` | Container CPU/memory recommendations |
| `daily_container_digests` | Daily container metric digests (partitioned) |
| `container_usage_samples` | Raw usage samples (partitioned) |
| `gpu_container_digests` | GPU metric digests per container (partitioned) |
| `node_recommendations` | Node CPU/memory utilization recs |
| `daily_node_digests` | Daily node metric digests (partitioned) |
| `daily_namespace_digests` | Namespace-level digests (partitioned) |
| `namespace_recommendation_sets` | Namespace recommendations |
| `daily_pvc_digests` | PVC storage digests (partitioned) |
| `snapshot_recommendation_sets` | VolumeSnapshot staleness recommendations |
| `recommendation_history` | Historical recommendation snapshots |
| `recommendation_quality` | Quality/stability tracking |

### Partitioning

Digest and sample tables use **monthly range partitioning** on the interval timestamp.
Partitions are created automatically and dropped by the retention sweep after
`ROS_RETENTION_MONTHS`.

---

## API Endpoints

All endpoints are under `/api/cost-management/v1/`:

| Endpoint | Description |
|----------|-------------|
| `GET /recommendations/openshift` | List container recommendations |
| `GET /recommendations/openshift/:id` | Container recommendation detail |
| `GET /recommendations/openshift/fleet-summary` | Organization-wide summary |
| `GET /recommendations/openshift/gpu` | GPU recommendation summary |
| `GET /recommendations/openshift/gpu/timeslicing` | Node GPU time-slicing recs |
| `GET /recommendations/openshift/gpu/mig` | Container MIG profile recs |
| `GET /recommendations/openshift/nodes` | Node utilization recs |
| `GET /recommendations/openshift/namespaces` | Namespace recs |
| `GET /recommendations/openshift/namespaces/:id` | Namespace rec detail |
| `GET /recommendations/openshift/pvcs` | PVC storage recs |
| `GET /recommendations/openshift/snapshots` | Snapshot staleness recs |
| `GET /recommendations/openshift/history` | Recommendation history |
| `GET /recommendations/openshift/quality` | Quality/stability metrics |
| `GET /recommendations/openshift/settings/terms` | Term configuration |
| `PUT /recommendations/openshift/settings/terms` | Set custom term windows |
| `DELETE /recommendations/openshift/settings/terms` | Reset to defaults |

### Authentication

All requests require an `x-rh-identity` header (base64-encoded JSON):

```json
{
  "identity": {
    "org_id": "1234567",
    "type": "User",
    "user": { "username": "developer", "is_org_admin": true }
  }
}
```

In local development, RBAC is disabled by default (`RBAC_ENABLE=false`).

---

## Developing a New Plugin

### 1. Create the plugin package

```
internal/plugins/myplugin/
├── plugin.go       # Plugin struct + interface implementations
└── plugin_test.go  # Tests
```

### 2. Implement the interfaces you need

```go
package myplugin

import "github.com/redhatinsights/ros-ocp-backend/internal/plugin"

type MyPlugin struct{}

func (p *MyPlugin) Name() string { return "myplugin" }

// Implement one or more of:
// - plugin.CSVIngestor     → owns CSV parsing for a payload type
// - plugin.IngestHook      → runs after another plugin's CSV parsing
// - plugin.APIProvider     → registers HTTP routes
// - plugin.APIEnricher     → enriches another plugin's API response
// - plugin.RetentionProvider → owns retention sweeps for its tables

func init() {
    plugin.Register(&MyPlugin{})
}
```

### 3. Import the plugin (blank import)

In `internal/plugins/plugins.go`:

```go
import _ "github.com/redhatinsights/ros-ocp-backend/internal/plugins/myplugin"
```

### 4. Enable/disable via env var

```bash
# Only enable your plugin (for focused testing)
ROS_ENABLED_PLUGINS=myplugin

# Enable alongside existing plugins
ROS_ENABLED_PLUGINS=container,gpu,node,myplugin
```

### 5. Add migrations if needed

```bash
# Create new migration files
migrate create -ext sql -dir migrations -seq create_myplugin_table
```

---

## Testing

### Running Tests

```bash
# Unit tests (fast, no external dependencies)
go test -short ./...

# Unit tests with race detector
go test -short -race ./...

# Full tests including integration (requires PostgreSQL on localhost:15432)
go test ./...

# Specific package
go test ./internal/engine/ -run TestClassify

# Fuzz tests (run until interrupted or failure found)
go test ./internal/ingestion/ -fuzz=FuzzParseCSVRows -fuzztime=30s
```

### Using Podman for Integration Tests

testcontainers-go works with Podman via rootless socket:

```bash
export DOCKER_HOST=unix:///run/user/$(id -u)/podman/podman.sock
export TESTCONTAINERS_RYUK_DISABLED=true
go test ./internal/engine/ -run Integration
```

Ryuk (the cleanup sidecar) is incompatible with rootless Podman. Disabling it
is safe because test containers are cleaned up via `t.Cleanup()`.

### Test Database

Integration tests use PostgreSQL on `localhost:15432`:

```bash
# Start a test database (if not using docker-compose)
podman run -d --name ros-test-db -p 15432:5432 -e POSTGRES_PASSWORD=postgres postgres:16
```

### Testing Conventions

#### Parallel Tests

All tests that are **pure computation** (no shared mutable state, no database,
no filesystem) MUST use `t.Parallel()`:

```go
func TestSomething(t *testing.T) {
    t.Parallel()
    // ...
}
```

#### Configurable Thresholds (GPUThresholds Pattern)

The GPU classification engine uses a `GPUThresholds` struct instead of
package-level global variables. This enables safe parallel testing:

```go
func TestCustomThreshold(t *testing.T) {
    t.Parallel()  // safe — no global mutation
    th := engine.GPUThresholds{
        IdleThreshold:       0.05,
        UnderutilizedSM:     0.25,
        UnderutilizedTensor: 0.15,
        MemBoundDRAM:        0.60,
        MemBoundTensor:      0.15,
        FBHeadroomFactor:    1.20,
    }
    cls, _ := th.Classify(digests)
    assert.Equal(t, engine.GPUClassIdle, cls)
}
```

**Never** mutate package-level globals in tests. If production code uses
globals (e.g., `InitGPUEngine`), tests that verify that function should NOT
be marked `t.Parallel()`.

#### Environment Variables in Tests

Use `t.Setenv()` instead of `os.Setenv()`:

```go
t.Setenv("ROS_GPU_IDLE_THRESHOLD", "0.05")  // auto-cleanup
```

#### Assertions

Use `require` for preconditions that would cause nil-pointer panics if failed,
`assert` for everything else:

```go
rec := RecommendGPU(digests)
require.NotNil(t, rec, "expected recommendation for idle workload")
assert.Equal(t, GPUClassIdle, rec.Classification)
assert.Greater(t, rec.Confidence, float32(0))
```

#### Error Path Testing

All external service integrations must have error-path tests covering:
- Timeouts (server takes longer than client timeout)
- Server errors (5xx)
- Authentication failures (401/403)
- Malformed responses (invalid JSON, unexpected schema)
- Context cancellation

See `internal/costdata/provider_contract_test.go` for the reference pattern.

#### Fuzz Tests

Add fuzz tests for any function that parses external input (CSV, JSON, query
parameters). Fuzz targets must not panic on arbitrary input:

```go
func FuzzParseCSVRows(f *testing.F) {
    f.Add("valid,csv,header\nrow,data,here\n")
    f.Fuzz(func(t *testing.T, data string) {
        ParseCSVRows(strings.NewReader(data)) //nolint:errcheck
    })
}
```

#### Integration Tests

Tests requiring PostgreSQL or external containers use `testing.Short()` guard:

```go
func TestPersistRecommendations(t *testing.T) {
    if testing.Short() {
        t.Skip("requires PostgreSQL")
    }
    pool := testutil.SetupTestDB(t)
    // ...
}
```

---

## Code Style

### Imports

Standard library → external packages → internal packages (blank-line separated):

```go
import (
    "context"
    "fmt"

    "github.com/labstack/echo/v4"
    "github.com/sirupsen/logrus"

    "github.com/redhatinsights/ros-ocp-backend/internal/config"
    "github.com/redhatinsights/ros-ocp-backend/internal/logging"
)
```

### Logging

Use the `internal/logging` package for structured logs:

```go
logging.ForOrg(orgID).WithField("cluster", clusterUUID).Info("processing complete")
logging.ForRequest(c).Warn("invalid parameter")
```

### Error Handling

Wrap errors with context. Never swallow errors:

```go
if err := db.Query(ctx, sql); err != nil {
    return fmt.Errorf("querying node recommendations for org %s: %w", orgID, err)
}
```

---

## Deployment

### Production (console.redhat.com)

Deployed as a ClowdApp on OpenShift with:
- 4 deployments (api, processor, recommendation-poller, housekeeper)
- Managed PostgreSQL (RDS)
- MSK Kafka
- Platform RBAC + identity middleware

### On-Premise (cost-onprem Helm chart)

Deployed alongside Koku in a single Helm chart (`cost-onprem-chart/`):
- Single PostgreSQL shared with Koku
- Internal Kafka (AMQ Streams)
- Keycloak for JWT authentication
- S3-compatible storage (NooBaa/Ceph RGW)

---

## Commit Messages

Use imperative mood, reference issue numbers:

```
Fix GPU threshold race in parallel tests (#428)

Introduce GPUThresholds struct with Classify/SelectMIGProfile methods.
Tests create local instances instead of mutating package globals.
```

## Pull Requests

- One logical change per PR
- Include test coverage for new/modified code
- Run `go vet ./...` and `go test -race -short ./...` before pushing
- Update relevant docs in `docs/` if changing behavior

## Further Reading

- [Plugin architecture](docs/architecture/plugin-architecture.md)
- [GPU classification algorithm](docs/architecture/gpu-classification.md)
- [Recommendation math](docs/architecture/recommendation-math.md)
- [Kafka message schema](docs/architecture/kafka-schema.md)
- [Cost integration](docs/architecture/cost-integration.md)
- [OpenAPI spec](openapi.json)
