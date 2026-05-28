# Testing & Quality Assurance

ROS-OCP Backend maintains comprehensive test coverage across multiple repositories and testing layers, ensuring reliability from individual functions through full-stack deployment validation.

## Test Inventory

| Layer | Repository | Tests | Purpose |
|-------|-----------|------:|---------|
| Unit & Integration | ros-ocp-backend | ~990 | Go test functions covering all engine logic, handlers, DB accessors |
| End-to-End | cost-onprem-chart | ~105 | Full-stack tests against deployed OpenShift cluster |
| IQE (Integration Quality) | iqe-cost-management-plugin | ~117 | Smoke and regression tests for CI/CD pipelines |
| Backend API | koku (masu) | ~26 | Reship and effective-rates endpoint tests |
| **Total** | | **~1,250** | |

## What's Tested

### Recommendation Engine

- Container, Namespace, Node, GPU, PVC, and Snapshot recommendation accuracy
- Dual-engine (cost vs performance) divergence correctness
- Business hours weighted digest computation
- Custom threshold application and 3-tier resolution (env > tenant DB > defaults)
- Async recalculation when thresholds change

### Idle / zombie detection

- Contract tests in `internal/api/contract_test.go` (`TestContractIdleDetection_*`): `idle_state` on list responses, conditional waste/recommendation fields, `filter[idle_state]`, savings-summary `group_by[idle_state]`, CSV idle columns
- IQE: `iqe_cost_management/tests/rest_api/v1/test_ros_idle_detection.py` (filters, settings GET/PUT/validation, CSV headers)
- OpenAPI: `openapi.json` and `docs/openapi/idle-detection.yaml`

### API Layer

- All CRUD operations for settings (thresholds, terms, business hours, snapshots, idle-detection)
- RBAC enforcement (read-only users blocked from PUT/DELETE)
- OpenAPI spec/route parity (no spec drift)
- Pagination, filtering, engine selection, CSV export (including `currency` column)
- Tag filtering (`filter[tag:key]`, `meta.warnings`, savings-summary `group_by[tag:key]`)
- Input validation with detailed error responses

### Data Pipeline

- Ingestion → digest → recommendation → savings end-to-end flow
- Dual-stream business hours ingestion
- Reship triggering and completion
- Savings column population (node, PVC, container)

### Financial Accuracy

- Cross-service currency propagation (EUR/GBP/USD)
- Negative savings (scale-up scenarios correctly show cost increase)
- Savings summary aggregation across fleet
- Cost model integration via Koku effective_rates

### Reliability & Performance

- Concurrent cache access (race-safe under parallel writes)
- Memory stability benchmarks (no cache leaks under load)
- Goroutine leak detection (uber-go/goleak)
- Threshold resolution scales linearly (benchmarked to 100 orgs)
- Performance benchmarks for savings calculation (1000 containers < 1s)

### Production Observability

- Prometheus gauge `ros_threshold_cache_entries` for cache monitoring
- Prometheus counter `ros_threshold_recalculation_total` for recalc tracking
- Standard Go runtime metrics (heap, goroutines)

## Testing Layers Explained

**Unit tests (`*_test.go`):** Test individual functions in isolation. Mock external dependencies (DB, HTTP). Run in milliseconds. ~990 tests.

**Integration tests (`*_integration_test.go`):** Test components with a real PostgreSQL database via Testcontainers. Validate SQL queries, schema migrations, and data flow. Run in seconds.

**E2E tests (cost-onprem-chart):** Deploy the full stack on OpenShift, ingest real data via NISE, and validate API responses end-to-end. Run in minutes. ~105 tests.

**IQE tests:** Red Hat's internal quality engineering framework. Run in CI pipelines against stage and production-like environments. ~117 tests across smoke, extended, and stable profiles.

**Benchmarks (`Benchmark*`):** Measure performance characteristics. Run with `go test -bench`. Not included in production binary.

## Running Tests

```bash
# Unit + integration (requires Docker for testcontainers)
go test ./internal/... -v

# Full suite via Makefile (serial packages, 30m timeout — avoids testcontainers starvation)
make test

# With race detector (same as CI)
go test -race -count=1 -timeout=30m -p=1 ./...

# Benchmarks only
go test -bench=. -run='^$' ./internal/engine/

# E2E (requires deployed cluster)
NAMESPACE=cost-onprem ./scripts/run-pytest.sh --ros

# IQE (requires VPN + cluster)
./scripts/run-iqe-tests-local.sh --profile smoke
```

## Test Data Generation (NISE)

[NISE](https://github.com/project-koku/nise) generates realistic OCP metric CSVs
for all ROS plugins. Understanding how filenames flow through the system is critical
for test data to be processed correctly.

### CSV Filename Conventions

Each plugin expects specific filename patterns. `DetermineCSVType()` classifies
files using ordered prefix matching with a `Contains` fallback:

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

**Classification logic:** Prefix match is tried first (handles operator and
`--insights-upload` filenames). If no prefix matches, a `Contains` fallback handles
`--write-monthly` filenames where the pattern is embedded after a date/UUID prefix.

### Recommended: `--insights-upload`

The `--insights-upload` flag generates, renames, tarballs, and uploads in one step.
It produces correctly-named files that match prefix rules:

```bash
nise report ocp \
  --static-report-file config.yml \
  --ocp-cluster-id $CLUSTER_UUID \
  --ros-ocp-info \
  --insights-upload http://ingress-service:port/api/ingress/v1/upload
```

Requires either `INSIGHTS_ACCOUNT_ID` + `INSIGHTS_ORG_ID` env vars, or basic auth,
or a bearer token. Nise must be able to reach the ingress service network.

### Alternative: `--write-monthly` + manual upload

When nise runs on a different machine than the cluster (common in local dev):

```bash
# 1. Generate files locally
nise report ocp \
  --static-report-file config.yml \
  --ocp-cluster-id $CLUSTER_UUID \
  --ros-ocp-info \
  --write-monthly

# 2. Package (strip ./ prefix to avoid ingress issues)
cd output_dir/ && tar czf /tmp/upload.tar.gz --transform='s|^\./||' .

# 3. SCP to cluster-accessible machine
scp /tmp/upload.tar.gz user@hypervisor:/tmp/

# 4. Upload from there
curl -X POST -F "file=@/tmp/upload.tar.gz;type=application/vnd.redhat.hccm.tar+tgz" \
  -H "Authorization: Bearer $TOKEN" \
  http://ingress-route/api/ingress/v1/upload
```

This works because `DetermineCSVType()` has a `Contains` fallback that handles
the `Month-Year-UUID-` prefix in nise's `--write-monthly` filenames.

### Manifest structure

The upload tarball must include a `manifest.json`:

```json
{
  "cluster_id": "UUID",
  "uuid": "assembly-uuid",
  "date": "2026-05-28T00:00:00",
  "start": "2026-05-01T00:00:00",
  "end": "2026-05-28T00:00:00",
  "version": "1.0.0",
  "files": ["pod_usage.csv", "storage_usage.csv"],
  "resource_optimization_files": ["ros_usage.csv", "ros_namespace.csv", "ros_cluster_quota.csv"]
}
```

- `files` → shipped to Koku for cost processing
- `resource_optimization_files` → shipped to ROS for recommendation processing
- `start` and `end` are **required** for Koku summary table population

**Warning:** Omitting `start`/`end` causes a silent failure — data ingests
successfully but Koku's cost summary tables remain empty. ROS-OCP still
processes recommendations correctly (it never sees the manifest), but cost
reports will show no data. If code-level validation is needed, implement it
in Koku's manifest parser (`koku/masu/external/kafka_msg_handler.py`), not in
ros-ocp-backend.

See also [Local Development](development.md) and [Quick Start Tutorial](quickstart.md).

## Quality Gates

Before merge, all PRs must pass:

- `go build ./...` (compilation)
- `go test ./internal/...` (unit + integration)
- `go test -race ./internal/...` (race detection)
- Goroutine leak check (goleak in TestMain)
- OpenAPI contract validation (spec matches routes)
