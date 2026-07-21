# Java/JVM Recommendations (Planned)

**Status:** Planned / Future Work  
**Last updated:** 2026-07-21  
**Public overview:** [Java & JVM Optimization (docs-site)](../../docs-site/planned-features/java-jvm.md)

**Related requirements:** [requirements.md §13 (Phase 9)](../archive/requirements.md) — REQ-9.1 through REQ-9.5  
**Feature inventory:** F48–F52 (not implemented)

---

## Overview

JVM workloads (Spring Boot, Quarkus, WebSphere/Open Liberty, plain Java) on OpenShift have unique optimization characteristics that generic container rightsizing cannot address. Container plugins reason about cgroup CPU and memory limits; they do not understand heap ergonomics, garbage collector selection, metaspace growth, or thread-pool sizing tied to `Runtime.availableProcessors()`.

Detection is **dual-axis**: JVM **runtime** (Hotspot vs OpenJ9/Semeru) × app **framework** (Liberty, Quarkus, Spring, plain). Heap/OOM math is shared; GC flag shape follows runtime; thread/JDBC config output follows framework.

**Today:**

- **koku-metrics-operator:** Collects only cgroup data (CPU, memory, OOM). No `jvm_*` metrics flow through the system.
- **Kruize:** Prototype handlers (MaxRAMPercentage, GC) exist but use static heuristics and never consume actual heap or GC pause metrics.
- **ros-ocp-backend:** Planned in requirements §13 (Phase 9, REQ-9.1–9.5) and feature inventory F48–F52. **Not implemented.**

**Goal:** A `java` plugin (Phase 2 Enrich, priority ~25) that annotates and enhances container recommendations with JVM-specific tuning — heap sizing, GC policy, thread pools, and container memory limits that account for non-heap footprint.

---

## Current state

| Component | JVM metrics | Recommendation logic |
|-----------|-------------|----------------------|
| koku-metrics-operator | **None** — cgroup CPU/memory/OOM only | N/A |
| Kruize | N/A (no ingestion) | Static MaxRAMPercentage / GC heuristics; never data-driven |
| ros-ocp-backend | No `jvm_*` digest fields | REQ-9.1–9.5 specified; plugin not registered |

The java plugin is **Phase 2 Enrich**: it runs after the container plugin (Phase 1 Produce) and reads recommended container limits as inputs for heap and non-heap sizing.

---

## Recommendation types

### 1. Heap sizing (MaxRAMPercentage) — REQ-9.2

**Inputs:**

- `heap_p95` from JVM digests (Prometheus `jvm_memory_used_bytes{area="heap"}`)
- Recommended container memory limit from the container plugin (same term window)

**Algorithm:**

```
heap_util = heap_p95 / container_limit
target_pct = clamp(ceil(heap_util * 100) + headroom_pct, min_pct, max_pct)
```

Defaults: `headroom_pct = 10`, `min_pct = 50` (`ROS_JVM_MIN_RAM_PERCENT`), `max_pct = 90` (`ROS_JVM_MAX_RAM_PERCENT`).

**Output (Hotspot):**

```
JDK_JAVA_OPTIONS="-XX:MaxRAMPercentage=<N>"
```

**Heuristic fallback** (no JVM metrics available):

| Container limit | Default MaxRAM% | Confidence |
|-----------------|-----------------|------------|
| ≤ 512 MiB | 50% | low |
| > 512 MiB | 80% | low |

Phase 0 heuristic-only mode **must not ship** without metrics — static guesses can worsen OOM or waste memory.

---

### 2. Container memory (OOMKill problem)

**Problem:** Containers are OOMKilled even when the heap is not full. JVM memory includes metaspace, thread stacks (~1 MiB per thread), direct buffers, code cache, and GC overhead — none of which count toward heap usage.

**Algorithm:**

```
container_rec = ceil(heap_target + non_heap_p95) × safety_factor
```

`safety_factor` default **1.20** (`ROS_JVM_NON_HEAP_FACTOR`); cost profile may use 1.15, performance 1.25–1.50.

**OOM classification:**

- If `oom_count > 0` **and** `heap_used_max < 0.70 × container_limit` → flag **`non_heap_oom`**
- Cross-check: if `memory_usage_max >> heap_p95` → inflate container recommendation (non-heap dominated)

**Customer message pattern:** *"OOMKill detected with low heap usage. Root cause: metaspace growth. Recommend: increase container limit, not heap."*

---

### 3. GC strategy — REQ-9.3

**Data-driven gate:** If `gc_pause_p95 > 200 ms` (`ROS_JVM_GC_PAUSE_HIGH_MS`) → bias toward low-latency collectors.

**Rule table (after pause check):**

| Condition | Recommendation |
|-----------|----------------|
| ≤ 1 core (CPU limit) | SerialGC |
| ≤ 2 cores, heap < 4 GiB | ParallelGC |
| Heap ≥ 4 GiB, JDK ≥ 17 | ZGC (generational if JDK 21+) |
| Heap ≥ 4 GiB, JDK 12–16 | Shenandoah |
| Else | G1GC |

**Semeru / Eclipse OpenJ9:** `-Xgcpolicy:gencon` vs `balanced` based on pause profile and throughput vs latency engine.

**Minimum data:** GC strategy changes require **7+ days** of steady-state data; skip the short (1d) term entirely — major collections are too infrequent for reliable pause percentiles in a single day.

---

### 4. CPU sizing (JVM ergonomics)

On cgroupv2/CFS, `Runtime.availableProcessors()` reflects the container CPU limit. JVM thread pools, fork-join pools, and GC worker counts scale to this value.

**Starvation pattern:** Many active threads (`jvm_threads_live` high) but low CPU limit → recommend raising container CPU request/limit or reducing pool sizes (see thread pool section).

This recommendation **enriches** container CPU guidance rather than replacing it.

---

### 5. Thread pool (Liberty / Quarkus / Spring) — REQ-9.4

Fixes a known Kruize bug where thread counts ignored actual CPU limits.

**WebSphere / Open Liberty:** Size Default Executor from `threadpool.activeThreads` / `threadpool.size` vs CPU limit. Output: `server.xml` `<executor … maxThreads="N"/>` (or equivalent).

**Quarkus:**

```
core_threads = max(8, 2 × ceil(cpu_cores))
queue_capacity = 2 × core_threads
```

Output: `application.properties` keys (`quarkus.thread-pool.core-threads`, etc.).

**Spring Boot (Tomcat / Undertow):** Similar scaling for `server.tomcat.threads.max` / Undertow worker counts.

**Minimum data:** 4+ hours under load (HTTP traffic patterns from `http_server_*`, Liberty servlet/REST, or framework metrics).

### 5b. JDBC connection pools (Liberty)

When `connectionpool.*` vendor metrics exist: recommend datasource `maxPoolSize` when queued requests indicate saturation, or when the pool is chronically oversized vs concurrent use.

---

### 6. Native image vs JVM mode

**GraalVM native image:**

- No heap tuning (`jvm_info` absent, native image name in workload metadata)
- Container sizing based on **RSS** only (same as any native process)
- Advisory comparison only — no MaxRAMPercentage

**Detection:** Absence of `jvm_info` + image name / label hints (`quarkus.native`, `graalvm`).

---

### 7. Startup / warmup handling

JIT compilation and class loading distort early samples. **Exclude** intervals where `process_uptime < warmup_min` (default **45 minutes**, `ROS_JVM_WARMUP_MINUTES`).

**Per pod:**

- If uptime < 45 min at sample time → skip sample entirely
- Else use data from minute 45 onward only

A 7-day window with one restart and warmup exclusion yields ~6.8 days of clean steady-state data.

**Liberty InstantOn:** Checkpoint/restore short-circuits classic JIT warmup. Use a shorter or adaptive warmup gate when InstantOn (or equivalent) is detected — do not always apply the full 45-minute default.

---

### 8. Out of scope — platform migration recommendations

ROS **must not** emit recommendations to migrate traditional WebSphere Application Server (or other platforms) to Liberty. Migration requires compatibility/licensing assessment outside cgroup and JVM metrics (IBM Transformation Advisor owns that). After workloads already run on Liberty, normal cost-engine container memory recommendations catch oversized post-migration limits; Liberty-specific items cover executor/JDBC/runtime tuning only.

---

## Terms design

**Same term windows as containers (1d / 7d / 15d)** — not separate Java-specific terms.

| Rationale | Explanation |
|-----------|-------------|
| Phase 2 Enrich | Java annotates container recs for the **same** windows |
| UX consistency | One coherent recommendation per term in the UI |
| Real Java behavior | Warmup exclusion and minimum-data gates — not different calendar windows |

### Minimum-data gates per recommendation type

| Recommendation | Min data required | Rationale |
|----------------|-------------------|-----------|
| MaxRAMPercentage | 4+ hours steady-state | Multiple GC cycles needed for stable heap p95 |
| Container memory (non-heap) | 2+ hours | Metaspace and thread stacks stabilize quickly |
| GC strategy change | 7+ days (skip 1d term) | Major collections infrequent; pause p95 unreliable short-term |
| Thread pool sizing | 4+ hours under load | Need HTTP / request traffic patterns |

### Warmup filter (key behavior)

```
for each pod sample:
  if process_uptime < ROS_JVM_WARMUP_MINUTES (default 45):
    skip
  else:
    include from minute warmup onward
```

Configurable via `ROS_JVM_WARMUP_MINUTES`.

---

## Cost vs performance profiles

Same dual-engine model as containers; JVM dimensions differ:

| Dimension | Cost engine | Performance engine |
|-----------|-------------|-------------------|
| Heap percentile | p95 | p99 / max |
| Container margin | 15% | 25–50% |
| MaxRAMPercentage | Maximize (tight fit, higher %) | Lower % (more headroom below limit) |
| GC bias | Throughput (ParallelGC acceptable) | Low-latency (ZGC / Shenandoah) |

---

## Metrics required (operator — ~8+ new queries)

Applications must expose standard Prometheus JVM metrics (Micrometer, SmallRye, JMX exporter) and/or Liberty MicroProfile Metrics. Planned operator queries (prefix `ros:jvm_*` or equivalent), with **name normalization** from Liberty MP Metrics aliases into the same digest fields:

| Metric (Micrometer) | Liberty MP Metrics alias | Purpose |
|--------|---------|---------|
| `jvm_info` | (image + vendor metrics) | Detection, vendor, JDK version, runtime name |
| `jvm_memory_used_bytes{area="heap"}` | `memory.usedHeap` | Peak / percentile heap usage |
| `jvm_memory_used_bytes{area="nonheap"}` | (non-heap / RSS cross-check) | Non-heap sizing for container rec |
| `jvm_memory_committed_bytes` | `memory.committedHeap` | Committed vs used gap |
| `jvm_memory_max_bytes{area="heap"}` | `memory.maxHeap` | Effective -Xmx / MaxRAM cap |
| `jvm_threads_live` | `threadpool.activeThreads` | Thread stack / executor pressure |
| `jvm_gc_pause_seconds` (histogram → max/p95) | `gc.time` / `gc.time.per.cycle` | GC latency |
| `jvm_gc_pause_seconds_count` | `gc.total` | GC frequency |
| (framework-specific) | `threadpool.size`, `connectionpool.*` | Liberty executor + JDBC |

Operator query budget increase: estimated **10–15%** over current ROS query set (higher if Liberty vendor series are always scraped).

---

## Prerequisites

| Requirement | Notes |
|-------------|-------|
| Prometheus endpoint on app | `/actuator/prometheus`, `/q/metrics`, Liberty `/metrics` (`mpMetrics`), or JMX exporter sidecar |
| User Workload Monitoring (UWM) | Must be enabled on cluster for scraping app metrics |
| Container plugin | Phase 1 must run first — java reads recommended container limits |

**Without metrics:** Heuristic fallback only, `confidence: low`. **Do not enable by default** until operator metrics ship. Heuristic-only mode **must not ship** for initial release.

**Adoption blocker:** UWM enabled on ~20% of clusters (estimated) — largest deployment risk.

---

## Framework detection matrix

| Axis | Value | Primary detection | Config output |
|-----------|-------------------|-------------------|---------------|
| Runtime | Hotspot OpenJDK | `jvm_info` Hotspot labels | `JDK_JAVA_OPTIONS` + `-XX:+Use*` GC |
| Runtime | Semeru / OpenJ9 | `jvm_info{vendor="IBM"}` / OpenJ9 image | `-Xgcpolicy:...` + MaxRAM% |
| Framework | WebSphere / Open Liberty | Image (`websphere-liberty`, `open-liberty`) + `threadpool_*` / MP Metrics | `server.xml` executor (+ JDBC pool) |
| Framework | Quarkus JVM | `jvm_info` + `quarkus_*` metrics | `application.properties` |
| Framework | Quarkus native | No `jvm_info`, native image labels | Memory limit only (RSS) |
| Framework | Spring Boot | `jvm_info` + `http_server_*` / Actuator | `JAVA_TOOL_OPTIONS` / `JDK_JAVA_OPTIONS` |
| Framework | Plain Java | `jvm_info` only | `JAVA_TOOL_OPTIONS` |

Liberty commonly pairs with OpenJ9 but the axes stay independent (e.g. Spring Boot on Semeru is not Liberty).

---

## Default thresholds

| Parameter | Default | Env var |
|-----------|---------|---------|
| MaxRAM min % | 50 | `ROS_JVM_MIN_RAM_PERCENT` |
| MaxRAM max % | 90 | `ROS_JVM_MAX_RAM_PERCENT` |
| Heap headroom % | 10 | `ROS_JVM_HEAP_HEADROOM_PERCENT` |
| GC pause high (ms) | 200 | `ROS_JVM_GC_PAUSE_HIGH_MS` |
| GC pause OK (ms) | 50 | `ROS_JVM_GC_PAUSE_OK_MS` |
| Non-heap safety factor | 1.20 | `ROS_JVM_NON_HEAP_FACTOR` |
| Warmup minutes | 45 | `ROS_JVM_WARMUP_MINUTES` |
| Enable plugin | false | `ROS_ENABLE_JVM_RECS` |

---

## Configuration / API

| Item | Value |
|------|-------|
| Plugin name | `java` |
| Phase | 2 — Enrich |
| Priority | ~25 (after container produce) |
| Master gate | `ROS_ENABLE_JVM_RECS=false` (default) |
| Settings | `recommendation_type=java` on thresholds / terms (same 3-tier precedence as other plugins) |
| API shape | Extends container recommendation **detail** with `runtime_recommendations` block — **not** a separate list endpoint |

Example detail enrichment (illustrative):

```json
{
  "container_recommendation": { "...": "..." },
  "runtime_recommendations": {
    "runtime": "jvm",
    "framework": "quarkus",
    "confidence": "high",
    "items": [
      {
        "type": "max_ram_percentage",
        "current": "80",
        "recommended": "55",
        "config": "JDK_JAVA_OPTIONS=-XX:MaxRAMPercentage=55"
      },
      {
        "type": "gc_collector",
        "recommended": "ZGC",
        "reason": "gc_pause_p95=350ms exceeds 200ms threshold"
      }
    ]
  }
}
```

---

## Implementation phases

| Phase | Scope | Effort | Value |
|-------|-------|--------|-------|
| 1 | Operator JVM + Liberty MP Metrics queries, dual-axis detection, data-driven MaxRAM% (Hotspot + OpenJ9) | 3–4 weeks | High |
| 2 | GC pause-driven policy (Hotspot + Semeru) + non-heap OOM advisories | 2 weeks | High |
| 3 | Liberty executor + JDBC pools; Quarkus thread/queue; Semeru consistency | 2 weeks | High |
| 4 | Spring pools, HTTP latency profile | 3 weeks | Medium |
| 5 | GraalVM native profile, InstantOn-aware warmup | 2 weeks | Niche |

---

## Risks

| Risk | Mitigation |
|------|------------|
| UWM not enabled (~80% of clusters) | Document prerequisites; gate behind `ROS_ENABLE_JVM_RECS`; no default-on |
| JMX exporter not universal for plain Java | Support `/actuator/prometheus`, `/q/metrics`, Liberty `/metrics`; document sidecar pattern |
| Liberty without `mpMetrics` | Detect image; emit prerequisite tip; no high-confidence Liberty items |
| Heuristic-only Phase 0 harmful | Do not ship without operator metrics |
| Operator query budget +10–15% | Batch queries; share scrape config with UWM |
| Wrong MaxRAM% causes OOM or waste | Require 4h+ steady-state; warmup exclusion (adaptive for InstantOn) |
| Accidental migrate-to-Liberty product scope | Explicitly out of scope — no migration recommendation type |

---

## Dependencies

1. **Container plugin (Phase 1)** — Must run first; java reads recommended container CPU/memory limits.
2. **koku-metrics-operator** — Add `ros:jvm_*` (or equivalent) Prometheus queries and CSV/digest fields.
3. **Customer cluster** — UWM enabled and application metrics exposed.
4. **Koku cost path (optional)** — No change required for JVM recs; savings may attach to container rec savings later.

---

## Related documentation

- [Plugin execution phases](../architecture/plugin-phases.md) — Phase 2 Enrich slot for `java`
- [Container recommendations](../../docs-site/features/container-recommendations.md) — Base rightsizing
- [Configurable thresholds](../architecture/configurability.md) — `ROS_*` precedence model
- [kruize-vs-native-comparison.md](../kruize-vs-native-comparison.md) — Legacy JVM handler gaps
