# Cost Integration Honesty Exercise

**Date:** 2026-06-21
**Scope:** Rate fetching, savings computation, API exposure, fleet aggregation, recalculation
**Branch:** `pgarciaq-rosocp-superpowers-phase14`

---

## Phase 1: Discovery Summary

### Sources Examined

| Source | Location | Status |
|--------|----------|--------|
| Architecture docs | `docs/architecture/cost-integration.md` | Comprehensive (779 lines) |
| ADR-0111 | `docs/adr/0111-rates-from-koku-masu.md` | Accepted — rates from Masu only |
| ADR-0112 | `docs/adr/0112-bounded-lru-ttl-cost-cache.md` | Accepted — bounded LRU 1000 entries, 5min TTL |
| ADR-0228 | `docs/adr/0228-effective-rates-cache-key-org-cluster-only.md` | Accepted — cache by org+cluster, no dates |
| ADR-0229 | `docs/adr/0229-container-savings-effective-rates-from-namespace-aggregates.md` | Accepted — derive effective rates from aggregates |
| ADR-0280 | `docs/adr/0280-fixed-point-savings-migration-float-to-integer-cents.md` | Integer cents migration |
| ADR-0291 | `docs/adr/0291-integer-micro-cents-savings-computation.md` | Micro-cents arithmetic |
| Feature page | `docs/features/savings-estimations.md` | Reference hub (points to arch doc and docs-site) |
| Public docs-site | `docs-site/features/savings-estimations.md` | Customer-facing (aligned) |
| Go implementation | `internal/costdata/provider.go` | HTTP provider + LRU cache |
| Savings engine | `internal/engine/savings.go`, `savings_int.go` | Micro-cents integer arithmetic |
| Money formatter | `internal/money/format.go`, `cents.go` | MoneyAmount struct, FormatCentsToAmount |
| OpenAPI spec | `openapi.json` | MoneyAmount schema (line 11279), savings fields on all plugins |
| API cheatsheet | `costmgmt-api-cheatsheet.adoc` | Savings section, effective_rates docs |
| Bruno collection | `bruno/Optimizations/Fleet savings summary.bru` + 13 others | Savings requests covered |
| Contract test (Go) | `internal/costdata/provider_contract_test.go` | 8 test cases, full response parsing |
| Koku endpoint | `koku/masu/api/effective_rates.py` | AllowAny, schema-scoped SQL |
| Koku test | `koku/masu/test/api/test_effective_rates.py` | Unit tests for 200, 400, 404 |
| E2E tests | `cost-onprem-chart/tests/suites/ros/test_savings_summary.py` | Structure, plugin sums, engine filters |
| IQE tests | `iqe-ros-ocp-plugin/iqe_ros_ocp/tests/rest/test_savings_summary.py` | Fleet savings |
| IQE fixtures | `iqe-ros-ocp-plugin/iqe_ros_ocp/fixtures/ros_savings.py` | MoneyAmount parsing helpers |
| Config | `internal/config/config.go` (lines 143, 622, 872) | KOKU_MASU_URL validated at startup |

### Key Architecture Facts

1. **Rate source:** Exclusively from Koku Masu `GET /api/cost-management/v1/effective_rates/`
2. **Query parameters:** `cluster_id`, `org_id`, `start_date`, `end_date`
3. **Cache:** In-memory LRU (max 1000 entries, 5-minute TTL) keyed by `org_id + cluster_id`
4. **Rate structure:** `configured_rates` (8 metrics: CPU usage/request/hour, memory usage/request/hour, storage usage/request/month, node/month, cluster/month, GPU/month) + `namespace_aggregates` (per-NS cost breakdown)
5. **Savings formula:** Micro-cents integer arithmetic → round to cents → persist BIGINT
6. **Kill-switch:** `ROS_SAVINGS_ESTIMATES_ENABLED` (default true)
7. **Unavailable rates:** NilCostDataProvider returns empty struct → savings $0, NotifNoCostData (code 25)
8. **Per-cluster rates:** Yes — cache key is `(org_id, cluster_id)`
9. **Currency:** From Koku response `currency` field (ISO 4217), default USD
10. **Recalculation:** POST `/internal/recalculate-savings` triggered by Koku after cost model updates

---

## Phase 2: Detailed Audit

### Rate Fetching

| Aspect | Implementation | Notes |
|--------|----------------|-------|
| HTTP client | `httpclient.NewClient(timeout)` with shared transport | ✅ Proper |
| Timeout | `GLOBAL_HTTP_CLIENT_TIMEOUT_SECS` (default 30s) | ✅ Configurable |
| Error on non-200 | Logs status + body, returns error | ✅ Proper |
| JSON decode error | Returns error, logged upstream | ✅ Proper |
| Masu unreachable | Error returned → NilCostDataProvider used for this cycle | ✅ Non-blocking |
| Empty KOKU_MASU_URL | Warning at startup, NilCostDataProvider | ✅ Graceful degradation |
| Context cancellation | Honors context (tested) | ✅ |

### Rate Structure (from `configured_rates`)

| Metric | Used By | Status |
|--------|---------|--------|
| `cpu_core_usage_per_hour` | Node savings | ✅ |
| `cpu_core_request_per_hour` | (Not directly — derived from namespace aggregates for containers) | ✅ |
| `memory_gb_usage_per_hour` | Node savings | ✅ |
| `memory_gb_request_per_hour` | (Derived from namespace aggregates) | ✅ |
| `storage_gb_request_per_month` | PVC savings (primary) | ✅ |
| `storage_gb_usage_per_month` | PVC savings (fallback), snapshot default | ✅ |
| `node_cost_per_month` | Node savings (consolidation) | ✅ |
| `gpu_cost_per_month` | GPU MIG/idle savings | ✅ |
| `cluster_cost_per_month` | (Returned by Koku, not consumed by savings — informational) | ✅ |

### Savings Formula

| Plugin | Formula | Units | Status |
|--------|---------|-------|--------|
| Container | delta_mc × effective_rate_micro_cents/mc-hour × 730 × replicas (both cost-model and infra pools) | micro-cents → cents | ✅ Integer arithmetic |
| Node | delta_cores × rate + delta_GiB × rate + node_count_reduction × node_cost | micro-cents → cents | ✅ |
| PVC | delta_GiB × storage_rate_per_month | micro-cents → cents | ✅ |
| VM | delta_vCPU × cpu_rate + delta_GiB × mem_rate + vm_monthly | micro-cents → cents | ✅ |
| Snapshot | restore_size_GiB × cost_per_GiB_month (from settings chain) | micro-cents → cents | ✅ |
| GPU MIG | (1 - rec_slices/total_slices) × gpu_rate | micro-cents → cents | ✅ |
| GPU idle | full gpu_cost_per_month | micro-cents → cents | ✅ |

### Unavailable Rates Behavior

| Scenario | Savings | Notification | Status |
|----------|---------|--------------|--------|
| Kill-switch off | $0 | Code 25 (container/node/PVC) | ✅ |
| KOKU_MASU_URL empty | $0 | Code 25 (container/node/PVC) | ✅ |
| Masu unreachable | $0 | Code 25 (container/node/PVC) | ✅ |
| Namespace missing from aggregates | $0 for that workload | Code 25 | ✅ |
| VM without rates | `null` (not zero) | No code 25 | ✅ |
| GPU without rates | Omitted/zero | No code 25 | ✅ |

### API Response Fields

| Endpoint | Savings field | Type | Currency location | Status |
|----------|--------------|------|-------------------|--------|
| Container list | `estimated_monthly_savings` | MoneyAmount | Top-level `currency` | ✅ |
| Container list | `cpu_savings`, `memory_savings` | MoneyAmount | Same | ✅ |
| Node list | `estimated_monthly_savings` per engine | MoneyAmount | `meta.currency` | ✅ |
| PVC list | `estimated_monthly_savings` | MoneyAmount | `meta.currency` | ✅ |
| VM list | `savings` | MoneyAmount | Top-level | ✅ |
| Snapshot list | `estimated_monthly_cost` | MoneyAmount | `meta.currency` | ✅ |
| Savings-summary | `estimated_monthly_savings`, `by_plugin.*` | MoneyAmount | Top-level `currency` | ✅ |
| Fleet-summary | `total_monthly_savings` | MoneyAmount | Top-level `currency` | ✅ |
| GPU MIG | `estimated_monthly_gpu_savings` | MoneyAmount | Container currency | ✅ |
| GPU time-slicing | `total_node_savings`, `savings_per_gpu` | MoneyAmount | Top-level | ✅ |

### Configuration

| Variable | Default | Validated | Documented | Status |
|----------|---------|-----------|------------|--------|
| `KOKU_MASU_URL` | `""` | ✅ Warn at startup | ✅ In arch doc, config, cheatsheet | ✅ |
| `ROS_SAVINGS_ESTIMATES_ENABLED` | `true` | ✅ | ✅ | ✅ |
| `ROS_SAVINGS_RECALCULATION_ENABLED` | `true` | ✅ | ✅ | ✅ |
| `GLOBAL_HTTP_CLIENT_TIMEOUT_SECS` | `30` | ✅ | ✅ | ✅ |
| `ROS_COST_CACHE_MAX_ENTRIES` | `1000` | ✅ | ✅ (ADR-0112) | ✅ |

### Caching

| Aspect | Implementation | Status |
|--------|----------------|--------|
| Algorithm | Bounded LRU with TTL (5 min) | ✅ |
| Key | `org_id + "\x00" + cluster_id` (no date range) | ✅ (ADR-0228) |
| Max entries | 1000 (configurable) | ✅ |
| Invalidation | `InvalidateCostDataCache(org, cluster)` called on cost model changes | ✅ |
| Metrics | `rosocp_cost_data_cache_{hits,misses,evictions,size}` | ✅ |
| Per-process | Yes (no shared Redis for cost cache) | ✅ (ADR-0112 rationale) |

### Storage

| Aspect | Implementation | Status |
|--------|----------------|--------|
| DB column type | BIGINT (cents) | ✅ (ADR-0280, migration 000026+) |
| Computation precision | int64 micro-cents (10^-8 of dollar) | ✅ (ADR-0291) |
| Rounding | Half away from zero at micro-cents→cents | ✅ |
| Negative values | Allowed (ADR-0040) | ✅ |

---

## Phase 3: Alignment Matrix

| Aspect | Requirements (ADRs) | Go Code | Internal Docs | Public Docs | Cheatsheet | Bruno | Unit Tests | E2E Tests | IQE Tests | OpenAPI | Koku Backend |
|--------|---------------------|---------|---------------|-------------|------------|-------|------------|-----------|-----------|---------|--------------|
| Rate source = Masu only | ✅ ADR-0111 | ✅ | ✅ | ✅ | ✅ | — | ✅ | — | — | — | ✅ |
| Query param = `cluster_id` | ✅ (Koku code) | ✅ | ✅ | — | ⚠️→✅ FIXED | — | ✅ | — | — | — | ✅ |
| LRU cache 1000 entries | ✅ ADR-0112 | ✅ | ✅ | — | — | — | ✅ | — | — | — | — |
| Cache key = org+cluster | ✅ ADR-0228 | ✅ | ✅ | — | — | — | ✅ | — | — | — | — |
| TTL = 5 min | ✅ ADR-0228 | ✅ | ✅ | — | — | — | ✅ | — | — | — | — |
| Namespace aggregate rates | ✅ ADR-0229 | ✅ | ✅ | ✅ | ✅ | — | ✅ | — | — | — | ✅ |
| Integer micro-cents math | ✅ ADR-0291 | ✅ | ✅ | — | — | — | ✅ | — | — | — | — |
| BIGINT cents storage | ✅ ADR-0280 | ✅ | ✅ | — | — | — | ✅ | — | — | — | — |
| 730 hours/month | ✅ ADR-0182 | ✅ (const) | ✅ | ✅ | — | — | ✅ | — | — | — | — |
| MoneyAmount schema | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | — |
| Nil savings when unavailable | ✅ ADR-0113 | ✅ | ✅ | ✅ | ✅ | — | ✅ | — | — | — | — |
| NotifNoCostData (code 25) | ✅ ADR-0114 | ✅ | ✅ | ✅ | ✅ | — | ✅ | — | — | — | — |
| meta.currency on lists | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | ✅ | — | ✅ | — |
| Kill-switch | ✅ ADR-0160 | ✅ | ✅ | ✅ | ✅ | — | ✅ | — | — | — | — |
| Savings recalculation | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | ✅ |
| Negative savings allowed | ✅ ADR-0040 | ✅ | ✅ | ✅ | ✅ | — | ✅ | — | — | ✅ | — |
| Per-cluster rates | ✅ | ✅ | ✅ | ✅ | ✅ | — | ✅ | — | — | — | ✅ |
| Fleet savings aggregation | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | — |
| GPU excluded from fleet | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | — | ✅ | — |
| Container breakdown (cpu+mem) | ✅ | ✅ | ✅ | — | ✅ | ✅ | ✅ | — | — | ✅ | — |
| Koku endpoint tested | ✅ | — | — | — | — | — | — | — | — | — | ✅ |
| CSV export savings columns | ✅ | ✅ | ✅ | — | — | — | ✅ | — | — | — | — |
| E2E non-zero savings | ✅ | — | — | — | — | — | — | ✅ (structure) | ✅ | — | — |

---

## Phase 4: Discrepancies Found and Fixed

### 1. Cheatsheet: Wrong query parameter name for `effective_rates`

**What was wrong:** The API cheatsheet (`costmgmt-api-cheatsheet.adoc` line 439) documented the effective_rates query parameter as `cluster_uuid` instead of `cluster_id`.

**Authoritative source:** Koku code (`masu/api/effective_rates.py` line 164: `params.get("cluster_id")`) and ROS Go client (`internal/costdata/provider.go` line 145: `params.Set("cluster_id", clusterID)`).

**Fix applied:** Changed `cluster_uuid=<cluster-uuid>` to `cluster_id=<cluster-id>` in the cheatsheet.

### No other discrepancies found

All other sources are aligned. The architecture docs, public docs-site, Bruno collection, OpenAPI spec, Go code, Koku Python code, contract tests, E2E tests, and IQE fixtures all agree on:

- The MoneyAmount shape (`{"value": "12.34", "units": "USD"}`)
- Savings stored as BIGINT cents
- Computation using micro-cents integer arithmetic
- Per-cluster caching with org+cluster key
- NotifNoCostData (code 25) when rates unavailable
- GPU excluded from fleet totals
- Negative savings allowed and documented
- Currency propagation from Koku

---

## Phase 5: Verification Results

```
$ go test ./internal/costdata/ -count=1 -short     → PASS (0.329s)
$ go test ./internal/engine/ -count=1 -short -run Savings → PASS (0.048s)
$ go test ./internal/money/ -count=1 -short        → PASS (0.003s)
$ go build ./...                                    → OK (no errors)
```

---

## Phase 6: Summary

### What Works End-to-End

The cost integration feature is **robust and well-aligned across all sources**:

1. **Rate fetching:** HTTP client with proper timeouts, context cancellation, error handling. Bounded LRU cache prevents OOM under multi-tenant load.
2. **Savings computation:** Fixed-point micro-cents arithmetic eliminates floating-point rounding drift. All formulas documented in architecture docs with matching implementation.
3. **API exposure:** MoneyAmount schema used consistently. Currency field present on all list endpoints. Negative savings supported and documented.
4. **Fleet aggregation:** Plugin breakdown matches DB queries. GPU correctly excluded.
5. **Recalculation:** Koku triggers ROS after cost model updates; async background with 202 response.
6. **Failure modes:** Kill-switch, empty URL, unreachable Masu, missing namespace — all handled gracefully with notification code 25.
7. **Tests:** Contract tests validate the Koku↔ROS JSON interface. E2E validates structure and sums. Koku has its own unit tests for the endpoint.

### What's Genuinely Missing or Planned

| Item | Status | Tracked |
|------|--------|---------|
| Billing-derived snapshot costs | Planned | COST-7523 |
| Savings trends/time-series API | Planned | Not yet ticketed |
| GPU savings in fleet totals | Future v2 | Documented in arch doc |
| VM savings recalculation (without re-ingestion) | Not supported | By design (requires CSV) |

### Design Robustness Assessment

| Failure Mode | Handling | Rating |
|--------------|----------|--------|
| Masu down during ingestion | NilProvider, code 25, recommendations still persist | Excellent |
| Masu returns 500 | Error logged, savings skipped for this cluster | Good |
| Cost model removed (empty rates) | All rates zero → savings $0 | Good |
| Network timeout | 30s default, context-aware | Good |
| Cache stampede | LRU + TTL bounded; invalidation on cost model changes | Good |
| Concurrent recalc + ingestion | Single-flight guards in recalc | Good |

### Checklist Results

- [x] KOKU_MASU_URL is documented and validated at startup (warning log)
- [x] Rate fetching has proper timeouts and error handling
- [x] Missing rates produce nil/zero savings (not garbage)
- [x] NotifNoCostData (code 25) fires when rates are missing (container, node, PVC)
- [x] MoneyAmount schema is used consistently (not raw floats)
- [x] meta.currency is present on all list endpoints with monetary amounts
- [x] Savings are stored as BIGINT cents (not float dollars)
- [x] CPU and memory savings breakdown adds up to total (micro-cents math ensures this)
- [x] Fleet savings correctly aggregates per-plugin savings
- [x] Rate refresh doesn't block the recommendation pipeline (async recalc)
- [x] Per-cluster rates are supported (cache key = org+cluster)
- [x] CSV export includes savings with proper formatting
- [x] E2E tests verify savings structure when cost model is configured
- [x] Koku effective_rates endpoint is tested (unit tests in koku repo)

### Discrepancy Fixed

One cheatsheet documentation bug fixed: `cluster_uuid` → `cluster_id` in the effective_rates URL example.
