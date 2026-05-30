# Java & JVM Optimization

!!! warning "Status: Planned / Future Work"
    This feature is **not yet implemented**. The description below is the intended
    product direction for a future ROS-OCP release. Container, namespace, node, PVC,
    quota, and GPU recommendations remain available today.

!!! info "Quick Facts (planned)"
    **Scope:** JVM workloads on OpenShift (Spring Boot, Quarkus, plain Java)  
    **Plugin:** `java` (Enrich phase — builds on container recommendations)  
    **Analysis windows:** Same as containers (1 / 7 / 15 days) with JVM warmup exclusion  
    **Gate:** `ROS_ENABLE_JVM_RECS` (off by default until release)  
    **Warmup exclusion:** First 45 minutes after pod start (configurable)

---

## What it does

**Java & JVM Optimization** provides JVM-specific tuning recommendations for Java
applications running on OpenShift:

- **Heap sizing** — `MaxRAMPercentage` and effective heap limits aligned to real usage
- **Garbage collector selection** — Data-driven choice among G1, ZGC, Shenandoah, Parallel, Serial
- **Thread pool configuration** — Quarkus and Spring worker counts matched to CPU limits
- **Container memory optimization** — Limits that account for metaspace, thread stacks, and native memory — not just the heap

Recommendations appear **alongside** container right-sizing guidance in a
`runtime_recommendations` section on container detail — not a separate product silo.

**Why it matters:** Java is one of the most common languages on OpenShift, and
**container-level rightsizing alone routinely mis-tunes JVM apps** — causing OOMKills,
GC pauses, and wasted memory simultaneously.

---

## The problem — why Java is special

### JVM memory is not "one number"

The JVM divides process memory into regions that behave differently under load:

| Region | What it holds | Controlled by |
|--------|---------------|---------------|
| **Heap** | Application objects | `-Xmx`, `-XX:MaxRAMPercentage` |
| **Metaspace** | Class metadata | `-XX:MaxMetaspaceSize` (optional cap) |
| **Thread stacks** | ~1 MiB × thread count | Thread pools, framework defaults |
| **Code cache** | JIT compiled code | JVM ergonomics |
| **Direct buffers** | NIO, Netty, gRPC | Application code |
| **GC structures** | Collector overhead | Collector choice and heap size |

Generic container recommendations optimize **cgroup memory** as a single bucket.
That works for Go or Node when RSS ≈ working set. For Java, **heap is only part of the story**.

### Container OOMKill ≠ Java heap exhaustion

Kubernetes OOMKills the container when **total RSS** exceeds the cgroup limit — not when
the heap is full.

A common failure mode:

1. Container limit: **2 GiB**
2. `MaxRAMPercentage=80` → heap can grow to ~**1.6 GiB**
3. Metaspace grows to **200 MiB** after deployments
4. **50 threads** × ~1 MiB stack ≈ **50 MiB**
5. Code cache + GC + direct buffers add hundreds of MiB
6. **Working set exceeds 2 GiB** → **OOMKill** while heap shows **40–60%** used in metrics

**Raising MaxRAMPercentage makes this worse** — it steals cgroup budget from non-heap regions.

**The fix:** increase the **container limit** *or* **lower** MaxRAMPercentage to reserve cgroup space for non-heap.

### JVM ergonomics follow CPU limits

The JVM sets default parallelism (GC threads, ForkJoin pools, etc.) from
`Runtime.availableProcessors()`, which on Kubernetes equals the **CPU limit**.

| CPU limit | JVM assumption | Risk |
|-----------|----------------|------|
| 4 cores | 4 GC threads, modest pools | May under-utilize if request is low |
| 0.5 cores | 1 GC thread | Long GC pauses on multi-GB heap |
| 8 cores, Quarkus defaults | `core-threads` may follow cores | Under-threaded for I/O-heavy APIs |

Thread pool recommendations align framework defaults with **allocated CPU**, not node size.

### Warmup distorts short windows

For the first **30–45 minutes** after start:

- JIT compiles hot methods (CPU spike, code cache growth)
- Heap grows as caches warm
- GC patterns are not representative of steady state

ROS excludes this **warmup period** so recommendations reflect production behavior, not startup.

---

## The container OOMKill problem (detailed)

### Diagram in words

```
┌─────────────────────────────────────────────────────────────┐
│  Kubernetes cgroup memory limit: 2 GiB                       │
├─────────────────────────────────────────────────────────────┤
│  ┌─────────────────────────────────────────────────────┐  │
│  │ Heap (MaxRAMPercentage=80% → up to ~1.6 GiB)          │  │
│  │  Objects, caches, session state                       │  │
│  └─────────────────────────────────────────────────────┘  │
│  Metaspace (~200 MiB after deploy)                          │
│  Thread stacks (50 threads × ~1 MiB)                        │
│  Code cache (~100 MiB)                                      │
│  Direct buffers / native (variable)                         │
│  GC overhead                                                │
├─────────────────────────────────────────────────────────────┤
│  If sum > 2 GiB → OOMKill (even if heap chart shows 40%)   │
└─────────────────────────────────────────────────────────────┘
```

### Scenario walkthrough

**Symptoms:**

- Pod restarts with **OOMKilled**
- Prometheus shows heap peaked at **60%** of limit
- SRE raises `MaxRAMPercentage` from 50% → 75%
- OOMKills **increase**

**ROS diagnosis (planned):**

> *"3 OOMKills in 7 days. Peak heap **60%** of container limit. Peak non-heap **350 MiB**.
> Likely cause: **metaspace** or **direct buffer** growth. **Do not** raise MaxRAMPercentage.
> Recommend: container limit **2.5 GiB** OR MaxRAMPercentage **45%** with metaspace cap review."*

**Why it matters:** Misdiagnosed OOM loops waste weeks of tuning; the right lever is
often **container limit** or **thread/direct buffer** control, not heap percent.

---

## Recommendation types

### Heap sizing

Analyzes peak heap usage over your chosen window (after warmup exclusion) and recommends
`MaxRAMPercentage` (Hotspot) or equivalent heap fraction so the JVM uses cgroup memory efficiently.

**Example:**

> *"p95 heap usage **400 MiB** over 7 days. Container limit **2 GiB**. Recommend
> **MaxRAMPercentage=45%** (~900 MiB heap cap) — leaves **~1.1 GiB** for non-heap and headroom."*

**Why it matters:** FinOps saves memory; SRE avoids heap-driven OOM while preserving slack for metaspace.

**Output format (Hotspot):**

```
JDK_JAVA_OPTIONS="-XX:MaxRAMPercentage=45"
```

### Container memory

Combines heap target, non-heap peak, and safety margin when cgroup usage exceeds what heap tuning alone can fix.

**Example:**

> *"Total JVM footprint: heap p95 (**400 MiB**) + non-heap p95 (**350 MiB**) + 15% safety ≈ **860 MiB**.
> Recommend: reduce container limit from **2 GiB** → **1 GiB** (cost profile)."*

**Why it matters:** Paying for 2 GiB limits on 860 MiB workloads multiplies cost across hundreds of replicas.

### GC strategy

Uses GC pause percentiles and JDK/heap size rules to recommend a collector suited to your SLA.

**Example:**

> *"p95 GC pause **350 ms** on G1GC. JDK **21**, heap **6 GiB** qualifies for **ZGC Generational**.
> Expected improvement: p95 pause **< 10 ms** for latency-sensitive APIs."*

| Profile | Collector bias | When |
|---------|----------------|------|
| **Cost** | G1 / Parallel when pauses acceptable | Batch, internal tools |
| **Performance** | ZGC / Shenandoah when pauses high | User-facing APIs |

**OpenJ9 / Semeru:** Recommendations use `-Xgcpolicy:` instead of Hotspot `-XX:+Use*` flags.

### Thread pools (Quarkus / Spring)

Aligns worker threads with CPU limits for frameworks that default to core count.

**Example (Quarkus):**

> *"CPU limit **4 cores**, Quarkus `core-threads=4`. Recommend **core-threads=8** for I/O-heavy REST
> (throughput profile) — or confirm CPU limit should be **2** if intentional."*

**Example (Spring Boot):**

> *"Tomcat `maxThreads=200` on **2 CPU** limit — thread contention likely. Recommend **maxThreads=50**
> aligned with CPU, or raise CPU limit if load requires 200 threads."*

### OOM prevention / diagnosis

Classifies OOMKills where heap usage was low — pointing to non-heap causes.

**Example:**

> *"OOMKill with heap max **40%** of limit. Classification: **non_heap_oom**. Check
> `-XX:MaxMetaspaceSize`, direct buffer leaks, and thread pool growth."*

---

## Supported frameworks

### OpenJDK Hotspot

**Full suite:** heap (`MaxRAMPercentage`), GC flags, thread hints (when metrics exist), container memory.

Most common on OpenShift application images.

### Eclipse OpenJ9 / IBM Semeru

**Adapted recommendations:** GC via `-Xgcpolicy:gencon` / `balanced` etc.; heap sizing with OpenJ9 ergonomics.

Same OOM anatomy — metaspace and stacks still matter.

### Quarkus (JVM mode)

**Heap + GC + thread pool** with optional `application.properties` snippets:

```properties
quarkus.thread-pool.core-threads=8
quarkus.thread-pool.max-threads=32
```

### Quarkus Native (GraalVM)

**Different profile:** no heap tuning; **RSS-based container sizing** only.

Native image removes JVM heap/GC recommendations — container rightsizing remains primary.

### Spring Boot

**HTTP thread pools** (Tomcat/Undertow), connection pool advisories when metrics exposed, plus standard JVM tuning.

Actuator Prometheus endpoint is the typical metrics source.

### Plain Java

Standard JVM tuning from `jvm_*` metrics; thread pool hints when executor metrics are present.

---

## Cost vs performance tradeoff

ROS applies the same **dual-engine** model as containers:

| Aspect | Cost engine | Performance engine |
|--------|-------------|-------------------|
| Heap percentile | p95 | p99 / max |
| Container margin | ~15% | 25–50% |
| MaxRAMPercentage | Higher when safe | Lower for spike headroom |
| GC | Throughput-friendly if pauses OK | ZGC / Shenandoah when pauses high |
| Goal | Minimize memory $ | Minimize tail latency |

**How to choose:**

- **Batch processing, ETL, internal cron** → cost profile; accept higher GC pause if throughput is fine.
- **User-facing API, checkout, real-time** → performance profile; pay for headroom and low-latency GC.

You select the engine per recommendation the same way as [container right-sizing](container-recommendations.md) — see [Dual engine](dual-engine.md).

---

## Warmup handling

| Setting | Default | Purpose |
|---------|---------|---------|
| `ROS_JVM_WARMUP_MINUTES` | 45 | Exclude samples after pod start |

**What we exclude:** First N minutes after each pod start in the observation window.

**What you still get:** Steady-state heap, GC pause, and thread metrics for rightsizing.

**Customer message:**

> *"Recommendations based on steady-state behavior (warmup excluded). If you deploy
> frequently, ensure medium/long terms include enough post-warmup hours."*

---

## How it works

```mermaid
flowchart LR
  A[Metrics operator] -->|cgroup + jvm_*| B[ROS digests]
  C[Container plugin] -->|Phase 1 limits| D[Java plugin]
  B --> D
  D -->|runtime_recommendations| E[API / UI]
```

1. **Detection** — JVM workloads identified when `jvm_*` metrics exist in workload scrape data.
2. **Warmup exclusion** — Post-start samples dropped from percentile calculations.
3. **Analysis** — Heap, non-heap, GC pause, thread signals over container term windows.
4. **Enrichment** — JVM tuning attached to container recommendation detail; CPU/memory recs remain the base layer.

Without JVM metrics, ROS will **not** emit high-confidence JVM guidance (heuristic-only mode is **not** planned for initial release).

---

## What you'll see in the API

JVM recommendations enrich **container detail** responses (planned shape):

```json
{
  "container": "order-api",
  "project": "commerce",
  "workload": "order-api",
  "recommendations": {
    "medium_term": {
      "cost": {
        "config": {
          "requests": {
            "cpu": {"amount": 0.5, "format": "cores"},
            "memory": {"amount": 1, "format": "GiB"}
          }
        },
        "runtime_recommendations": {
          "runtime": "jvm",
          "framework_detected": "quarkus",
          "jdk_version": "21",
          "confidence": 0.92,
          "items": [
            {
              "category": "heap",
              "tunable": "MaxRAMPercentage",
              "current_value": "80",
              "recommended_value": "45",
              "formatted_flag": "-XX:MaxRAMPercentage=45",
              "rationale": "p95 heap 400 MiB; reserve cgroup space for non-heap"
            },
            {
              "category": "gc",
              "tunable": "collector",
              "current_value": "G1GC",
              "recommended_value": "ZGC",
              "formatted_flag": "-XX:+UseZGC -XX:+ZGenerational",
              "rationale": "p95 pause 350ms exceeds 200ms threshold on JDK 21"
            },
            {
              "category": "threads",
              "tunable": "quarkus.thread-pool.core-threads",
              "current_value": "4",
              "recommended_value": "8",
              "formatted_flag": "quarkus.thread-pool.core-threads=8",
              "rationale": "CPU limit 4; I/O-bound profile"
            },
            {
              "category": "container_memory",
              "tunable": "memory_limit",
              "current_value": "2Gi",
              "recommended_value": "1Gi",
              "rationale": "heap + non-heap p95 + 15% margin"
            }
          ],
          "oom_diagnosis": null
        }
      }
    }
  }
}
```

**OOM example** (`oom_diagnosis` populated):

```json
"oom_diagnosis": {
  "classification": "non_heap_oom",
  "oom_count_7d": 3,
  "heap_max_pct_of_limit": 0.60,
  "message": "OOMKill with low heap usage — increase container limit or reduce MaxRAMPercentage; check metaspace"
}
```

| Field | Meaning |
|-------|---------|
| `framework_detected` | quarkus, spring, hotspot, openj9, native |
| `confidence` | Based on data days and metric completeness |
| `formatted_flag` | Copy-paste for Deployment env or JVM options |
| `category` | heap, gc, threads, container_memory, oom |

---

## Prerequisites

| Requirement | Why |
|-------------|-----|
| **JVM metrics endpoint** | Application exposes Prometheus metrics (`/actuator/prometheus`, `/q/metrics`, or JMX exporter sidecar) |
| **User Workload Monitoring (UWM)** | Cluster allows scraping workload metrics into the operator pipeline |
| **Container recommendations** | Java plugin runs in **Enrich** phase after container **Produce** |
| **JDK / framework metrics** | `jvm_memory_used_bytes`, `jvm_gc_pause_seconds`, `jvm_threads_*`, etc. |

**Without JVM metrics:** recommendations fall back to **container-level analysis only** with **no** `runtime_recommendations` block — lower confidence for Java tuning.

### Metric checklist (typical Micrometer / Prometheus)

| Metric family | Used for |
|---------------|----------|
| `jvm_memory_used_bytes{area="heap"}` | Heap sizing |
| `jvm_memory_used_bytes{area="nonheap"}` or metaspace series | Container memory, OOM class |
| `jvm_gc_pause_seconds` | GC strategy |
| `jvm_threads_live` | Thread/stack pressure |
| `jvm_info` | JDK version for GC rules |

---

## Configuration

Same **three-tier** model as other ROS features:

| Variable | Default | Purpose |
|----------|---------|---------|
| `ROS_ENABLE_JVM_RECS` | `false` | Master feature gate |
| `ROS_JVM_WARMUP_MINUTES` | `45` | Exclude post-start samples |
| `ROS_JVM_MIN_RAM_PERCENT` | `50` | Floor for MaxRAMPercentage |
| `ROS_JVM_MAX_RAM_PERCENT` | `90` | Ceiling for MaxRAMPercentage |
| `ROS_JVM_GC_PAUSE_HIGH_MS` | `200` | Pause threshold for low-latency GC bias |
| `ROS_JVM_NON_HEAP_FACTOR` | `1.20` | Safety margin on container memory |

**Settings API:** `GET/PUT/DELETE .../settings/ros/thresholds/?recommendation_type=java`

See [Configurable thresholds](configurable-thresholds.md).

---

## Related documentation

| Document | Scope |
|----------|-------|
| [Container Right-Sizing](container-recommendations.md) | Base CPU/memory recommendations JVM builds on |
| [Dual Engine (Cost vs Performance)](dual-engine.md) | Cost vs performance profiles |
| [Configurable Thresholds](configurable-thresholds.md) | Settings API and precedence |
| [Plugin Execution Phases](../architecture/plugin-phases.md) | Enrich-phase placement |
| Internal design | [`docs/design/java-recommendations.md`](../../../docs/design/java-recommendations.md) |
