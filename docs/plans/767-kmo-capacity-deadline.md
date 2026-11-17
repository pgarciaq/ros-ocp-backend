# #767 KMO Capacity Query Deadline — Implementation Plan

> **For agentic workers:** superpowers:test-driven-development governs every step. Owner go-ahead 2026-11-05. Implementation in `~/dev/koku/koku-metrics-operator` (fork `pgarciaq`, branch `pgarciaq-rosocp-superpowers-phase17`); central tracker ros-ocp-backend #767.

**Goal:** per-HCP QPS and MCE-gauge queries honor the configured `ContextTimeout` budget — a stalled Prometheus response degrades the capacity facts instead of wedging the synchronous hook (and with it packaging, status updates, and requeue; default reconcile concurrency is 1, and Ping health stays green while wedged).

**Mechanism (verified at HEAD, zero drift):** `gatherHCPCapacityProm` runs every query under `context.Background()`; `SetPrometheusConnection` sets no HTTP timeout. The codebase's own established pattern (`node_alloc_cap_queries.go:84`, `prometheus.go:220`) is `context.WithTimeout(context.Background(), c.ContextTimeout)` — the hook simply missed it.

**Fix (consistency, not invention):**
1. `gatherHCPCapacityProm` gains a `ctx context.Context` parameter (it is a `var` — the existing test seam) and drops its internal `context.Background()`.
2. `collectHCPCapacityHourly` derives the budget before the gather: `ctx, cancel := context.WithTimeout(context.Background(), c.ContextTimeout); defer cancel()` — identical semantics to every other query group ("retain ordinary query timeout behavior"). No `GenerateReports` signature threading (it takes no ctx; adding one is the invasive refactor the criteria warn against). NOT the topology short timeout — the configured query budget.

**Degradation (already structural, activated by the bound):** per-query `Failed` flags → QPS unknown, MCE references absent; K8s facts still emit; hook errors are logged-and-skipped ("never fails the run").

**Tests (append to `hcp_capacity_collector_test.go`; the criterion's "actual client construction and hook together"):**
- RED: withholding httptest server + REAL `SetPrometheusConnection`; only `gatherHCPCapacityK8s` stubbed (the Prom path runs real); short `c.ContextTimeout`; drive `collectHCPCapacityHourly` in a goroutine; assert return within budget+ε, no error. Baseline: wedged → the outer timeout fails naming the mechanism; `defer close(release)` unblocks cleanup.
- Control (ordinary behavior retained): fast server returning valid vector JSON → facts complete (QPS known, gauges pass).
- Fan-out control: five ready namespaces against the stalling server → gather returns within ~one budget, not 5× (shared ctx: first query eats the pool, the rest fail fast with Failed flags).
- Existing gather tests updated mechanically (ctx arg).

**Constraints:** KMO-only; no client-construction timeout change (the ctx is the mechanism — "retain ordinary query timeout behavior"); no CRD/config changes; commits wait for owner go-ahead.
