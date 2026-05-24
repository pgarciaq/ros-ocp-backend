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

### API Layer

- All CRUD operations for settings (thresholds, terms, business hours, snapshots)
- RBAC enforcement (read-only users blocked from PUT/DELETE)
- OpenAPI spec/route parity (no spec drift)
- Pagination, filtering, engine selection, CSV export
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

# With race detector
go test -race ./internal/...

# Benchmarks only
go test -bench=. -run='^$' ./internal/engine/

# E2E (requires deployed cluster)
NAMESPACE=cost-onprem ./scripts/run-pytest.sh --ros

# IQE (requires VPN + cluster)
./scripts/run-iqe-tests-local.sh --profile smoke
```

## Quality Gates

Before merge, all PRs must pass:

- `go build ./...` (compilation)
- `go test ./internal/...` (unit + integration)
- `go test -race ./internal/...` (race detection)
- Goroutine leak check (goleak in TestMain)
- OpenAPI contract validation (spec matches routes)
