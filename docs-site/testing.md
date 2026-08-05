# Testing & Quality Assurance

> **Last verified:** 2026-08-05

ROS-OCP Backend maintains comprehensive test coverage across multiple repositories and testing layers, ensuring reliability from individual functions through full-stack deployment validation.

## Test Inventory

| Layer | Repository | Tests | Purpose |
|-------|-----------|------:|---------|
| Unit & Integration | ros-ocp-backend | ~2,620 | Go `Test*` functions (~2,618) covering engine logic, handlers, DB accessors; plus ~360 `t.Run` sub-tests and ~22 benchmarks |
| End-to-End | cost-onprem-chart | ~870 | Full-stack tests against deployed OpenShift cluster (CI runs ~88 non-extended) |
| IQE (cost-management) | iqe-cost-management-plugin | ~1,900 | Smoke, extended, stable, and full profiles for CI/CD pipelines |
| IQE (ros-ocp) | iqe-ros-ocp-plugin | ~680 | ROS-specific GPU/MIG, namespace, VM, and native-engine tests |
| Backend API | koku (masu / settings) | ~50 | Reship, effective-rates, tag sync, shipper, and ROS integration tests |
| **Total** | | **~6,100** | |

Nise itself has ~450 unit tests; they are not summed above because they validate the data generator, not ROS runtime behavior.

Counts are **approximate** and go stale after large landings. Prefer the recount commands
below over treating the table as an exact ledger.

### How to recount

Run from each repo root (siblings under the same workspace). Round to the nearest ten when
updating the table.

```bash
# ros-ocp-backend — Go Test* functions (excludes examples under /tmp)
rg -c '^func Test' --glob '*_test.go' -g '!vendor/**' | awk -F: '{s+=$2} END {print s}'

# Optional: include named subtests (noisy; not used in the table above)
# rg -c 't\.Run\(' --glob '*_test.go' -g '!vendor/**' | awk -F: '{s+=$2} END {print s}'

# cost-onprem-chart — pytest test functions
rg -c '^\s*def test_' --glob '*.py' tests/ | awk -F: '{s+=$2} END {print s}'

# iqe-cost-management-plugin
rg -c '^\s*def test_' --glob '*.py' iqe_cost_management/tests/ | awk -F: '{s+=$2} END {print s}'

# iqe-ros-ocp-plugin
rg -c '^\s*def test_' --glob '*.py' iqe_ros_ocp/tests/ | awk -F: '{s+=$2} END {print s}'

# koku — ROS-related masu tests (adjust path if layout changes)
rg -c '^\s*def test_' --glob '*ros*' koku/masu/test/ | awk -F: '{s+=$2} END {print s}'
```

After recounting, bump the **Last verified** date on this page and refresh the inventory
table. Do not invent vanity totals.

### Native Engine Contribution (historical)

As of mid-2026, the native engine effort had added on the order of **~3,100** test functions versus the pre-native upstream baselines below. **Current inventory is in the table above** — do not treat these rows as live counts.

| Repo | Pre-native upstream | Mid-2026 snapshot | Approx. added |
|------|--------------------:|------------------:|--------------:|
| ros-ocp-backend | 49 | ~2,170 | **~+2,100** |
| cost-onprem-chart | 317 | ~740 | **~+420** |
| iqe-ros-ocp-plugin | 80 | ~550 | **~+470** |
| nise | 381 | ~450 | **~+70** |
| koku (ROS-related) | 24 | ~65 | **~+40** |

Pre-native ros-ocp-backend was essentially a Kruize glue layer (~49 tests). The native engine filled in recommendation types, the plugin system, API handlers, DB accessors, ingestion, savings, history, notifications, and explanations.

## What's Tested

### Recommendation Engine

- Container, Namespace, Node, GPU (MIG + time-slicing), PVC, ResourceQuota, ClusterResourceQuota, Snapshot, and VM recommendation accuracy
- Dual-engine (cost vs performance) divergence correctness
- Business hours weighted digest computation
- Custom threshold application and 3-tier resolution (env > tenant DB > defaults)
- Async recalculation when thresholds change
- Recommendation explanations (`?include=explanation` on detail endpoints)
- Recommendation history tracking (container, namespace, quota, CRQ)
- Data decay (stale metrics age out with configurable half-life)

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
- GPU time-slicing persistence (compute-at-ingest, history, backfill)
- Manifest ID synthesis and debounce logic

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

**Unit tests (`*_test.go`):** Test individual functions in isolation. Mock external dependencies (DB, HTTP). Run in milliseconds. ~2,620 test functions plus ~360 sub-tests.

**Integration tests (`*_integration_test.go`):** Test components with a real PostgreSQL database via Testcontainers. Validate SQL queries, schema migrations, and data flow. Run in seconds.

**E2E tests (cost-onprem-chart):** Deploy the full stack on OpenShift, ingest real data via NISE, and validate API responses end-to-end. Run in minutes. ~870 test functions (CI runs ~88 non-extended).

**IQE tests:** Red Hat's internal quality engineering framework. Run in CI pipelines against stage and production-like environments. Profiles: smoke (~43 tests, ~17 min), extended (~2,100, ~33 min), stable (~2,350, ~40 min), full (~3,324, ~60 min).

**Benchmarks (`Benchmark*`):** Measure performance characteristics (~22 benchmarks). Run with `go test -bench`. Not included in production binary.

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

### NISE fixture libraries

Scenario and seeding YAMLs live in the **nise** repo (also shipped in the `koku-nise`
wheel). This page documents filename rules; use the catalogs below for which YAML to run.

| Directory | Purpose | Count (approx.) |
|-----------|---------|----------------:|
| [`examples/ros_ocp_e2e/`](https://github.com/project-koku/nise/tree/main/examples/ros_ocp_e2e) | Feature scenarios (GPU MIG/time-slicing, VM, quota, PVC, business hours, node idle, …) | ~40 YAMLs |
| [`examples/ros_ocp_seeding/`](https://github.com/project-koku/nise/tree/main/examples/ros_ocp_seeding) | Minimal baselines for cost-onprem-chart auto-seeding | 5 YAMLs |
| [`examples/ocp_dual_engine/`](https://github.com/project-koku/nise/tree/main/examples/ocp_dual_engine) | Cost vs performance divergence workloads | — |
| [`examples/ocp_vm/`](https://github.com/project-koku/nise/tree/main/examples/ocp_vm), `ocp_vm_recommendations/` | VM-focused static data | — |

Per-plugin copy-paste recipes: [Test Data Recipes](testing/test-data-recipes.md).
Nise READMEs: [`ros_ocp_e2e/README.md`](https://github.com/project-koku/nise/blob/main/examples/ros_ocp_e2e/README.md),
[`ros_ocp_seeding/README.md`](https://github.com/project-koku/nise/blob/main/examples/ros_ocp_seeding/README.md).

### Automatic data seeding

cost-onprem-chart E2E runs a session-scoped fixture (`tests/fixtures/data_seeding.py`)
that queries ROS row counts and, when below threshold, generates data from
`examples/ros_ocp_seeding/` (`seed_container`, `seed_pvc`, `seed_gpu`,
`seed_cluster_quota`, `seed_domain`). Skip with `E2E_SKIP_SEED=true`. Details and
thresholds: [Test Data Recipes — Auto-Seeding](testing/test-data-recipes.md#auto-seeding-templates).

### CSV Filename Conventions

Each plugin expects specific filename patterns. `DetermineCSVType()` classifies
files using ordered prefix matching with a `Contains` fallback:

| Plugin | Operator filename | Nise `--insights-upload` | Nise `--write-monthly` |
|--------|-------------------|--------------------------|------------------------|
| container | `ros-openshift-container-YYYYMM.csv` | `{uuid}_openshift_report.N.csv` | `Month-Year-UUID-ocp_ros_usage.csv` |
| gpu | *(piggybacks on container CSV)* | *(same as container)* | *(same as container)* |
| node | *(piggybacks on container CSV)* | *(same as container)* | *(same as container)* |
| namespace | `ros-openshift-namespace-YYYYMM.csv` | `{uuid}-ros-openshift-namespace-YYYYMM.N.csv` | `Month-Year-UUID-ocp_ros_namespace_usage.csv` |
| vm | `ros-openshift-vm-usage-YYYYMM.csv` | *(operator / package naming)* | `Month-Year-UUID-ocp_ros_vm_usage.csv` |
| vm-pvc | `ros-openshift-vm-pvc-YYYYMM.csv` | *(operator / package naming)* | `Month-Year-UUID-ocp_ros_vm_pvc.csv` |
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

For per-plugin fixture details and copy-paste commands, see
[Test Data Recipes](testing/test-data-recipes.md).

See also [Local Development](development.md) and [Quick Start Tutorial](quickstart.md).

## Quality Gates

Before merge, all PRs must pass:

- `go build ./...` (compilation)
- `go test ./internal/...` (unit + integration)
- `go test -race ./internal/...` (race detection)
- Goroutine leak check (goleak in TestMain)
- OpenAPI contract validation (spec matches routes)
