# Network Recommendations (Planned)

**Status:** Planned / Future Work  
**Last updated:** 2026-05-30  
**Public overview:** [Network Optimization (docs-site)](../../docs-site/planned-features/network.md)

**Related requirements:** Reserved in plugin phase inventory (Phase 1 Produce candidate)  
**Ecosystem analysis:** [performance-analysis.md](../architecture/performance-analysis.md) — VM network metrics noted as future placement input

---

## Overview

Network cost and performance are invisible in today's ROS-OCP pipeline. Container, node, PVC, and quota plugins reason about CPU, memory, and storage — not **who talks to whom**, **how much egress** leaves the cluster, or **whether DNS and packet loss** add latency.

**Goal:** A `network` plugin (Phase 1 Produce) that ingests flow-derived daily digests from the OpenShift **Network Observability Operator** (NetObserv), classifies traffic (internal, cross-zone, internet egress), and emits **actionable recommendations** — not another dashboard.

**Today:** No network flow CSV in the operator → ROS path. Koku cost pipelines do not attribute egress dollars to workloads at the ROS recommendation layer.

---

## Current state

| Component | Network flow data | Recommendation logic |
|-----------|-------------------|----------------------|
| koku-metrics-operator | **No** `ros:net_*` or network flow CSV | N/A |
| Koku backend | Cloud egress may appear in **AWS/Azure/GCP** CUR (off-cluster); OCP line items lack per-workload egress optimization | Cost **reporting** only |
| ros-ocp-backend | No network plugin, no digests | Not implemented |
| NetObserv (cluster) | Optional customer install; eBPF flows → Loki/S3/Prometheus | Not integrated with ROS |

### Gap summary

1. **Data:** Flow records (src/dst, bytes, protocol, zone labels) are not in the hourly/daily upload tarball ROS already processes.
2. **Processing:** `aggregator.go` and digest pipelines have no network report type.
3. **API:** No `/recommendations/openshift/network/` (name TBD) endpoints.
4. **SaaS FinOps:** Egress $ attribution requires correlating NetObserv bytes with cloud rate cards (Phase C / SaaS-heavy).

---

## OpenShift Network Observability Operator integration

### What NetObserv provides

[Network Observability Operator](https://docs.openshift.com/container-platform/latest/networking/network_observability/installing-operators.html) deploys:

- **FlowCollector** — eBPF-based flow collection on nodes
- **Flow metrics** — Aggregated bytes/packets by workload, namespace, protocol
- **Optional:** DNS latency, RTT, drops (feature-gated in FlowCollector spec)
- **Egress classification** — When enabled, marks internet-bound traffic

### Integration model (target)

Same pattern as container and planned VM pipelines:

```
NetObserv (cluster) → Prometheus / flow export
        ↓
koku-metrics-operator (new ros:net_* queries OR flow metric scrape)
        ↓
ros-openshift-network-usage-<YYYYMM>.csv (hourly → daily digest)
        ↓
ros-ocp-backend: ParseNetworkRows → daily_network_digests → recommendNetwork()
        ↓
GET /recommendations/openshift/network/...
```

**Operator work (NET-A):** Define PromQL or flow-metric mappings for:

| Digest field | Source (conceptual) |
|--------------|---------------------|
| `egress_bytes_total` | Sum egress-classified bytes per workload/namespace |
| `cross_zone_bytes` | Bytes where src zone ≠ dst zone (see risks — v2) |
| `internal_bytes` | Cluster-internal Service/ Pod traffic |
| `dns_latency_p99` | DNS RTT histogram |
| `packet_drop_rate` | Drops / packets |
| `top_external_destinations` | Cardinality-capped label set |

**Cardinality control:** Aggregate to **namespace + workload** (or Service) by default; top-N external hosts only.

---

## Recommendation tiers (A / B / C)

Prioritization for MVP vs follow-on releases.

### Tier A — MVP (highest value, lowest misclassification risk)

| ID | Type | Inputs | Output |
|----|------|--------|--------|
| A1 | **High egress volume** | `egress_bytes` p95 over 7–30d vs tenant threshold | "Namespace X sends Y TB/week external — cache, compress, CDN" |
| A2 | **DNS latency outlier** | `dns_latency_p99` vs cluster baseline | "Workload Z p99 DNS 220ms vs cluster 5ms — ndots, CoreDNS" |
| A3 | **Elevated packet drops** | Drop rate vs baseline between src/dst classes | "Drops between frontend and cache — MTU, CNI, congestion" |

**Rationale:** These do not require perfect **cross-zone** or **ClusterIP vs external IP** classification.

### Tier B — Post-MVP (needs stable labels + SaaS cost)

| ID | Type | Inputs | Output |
|----|------|--------|--------|
| B1 | **Egress cost attribution (SaaS)** | Egress bytes × cloud egress rate from Koku | "$4,200/mo namespace data-pipeline — top dest S3 40%" |
| B2 | **Chatty cross-namespace** | Internal bytes matrix | "api → legacy-db 12 TB/mo internal — review API design" |
| B3 | **Protocol mix** | TCP vs UDP dominance | Informational for VoIP/gaming outliers |

### Tier C — v2+ (topology / placement)

| ID | Type | Inputs | Output |
|----|------|--------|--------|
| C1 | **Co-location / topology** | Cross-zone bytes **when labels trustworthy** | "analytics-worker ↔ kafka-broker 800 GB/wk cross-AZ — topology-aware scheduling" |
| C2 | **Service mesh egress** | Istio/CM egress metrics overlay | Mesh-specific hints |

**C1 explicitly deferred** — see KubeCost ClusterIP pitfall below.

---

## Competitor comparison

| Capability | ROS (planned) | KubeCost | Cast.ai |
|------------|---------------|----------|---------|
| Egress $ attribution | B1 (SaaS, with Koku rates) | Strong on cloud | Strong |
| Per-namespace egress bytes | A1 | Yes | Yes |
| DNS / latency | A2 (NetObserv) | Limited | Limited |
| Cross-zone / AZ cost | C1 v2 (careful) | Yes (with caveats) | Yes |
| Actionable rec text | Core product goal | Dashboards + alerts | Automation focus |
| On-prem value prop | A2, A3 performance | Weak without cloud $ | Weak without cloud $ |
| OpenShift-native | NetObserv integration | Generic K8s | Generic K8s |

**Differentiation:** ROS targets **OpenShift + NetObserv** first — eBPF flows, DNS, drops — with recommendations in the **same API/UI** as container and node rightsizing, not a separate network product SKU.

---

## Metrics needed

### From NetObserv / FlowCollector

| Metric / record | Required for | FlowCollector config |
|-----------------|--------------|----------------------|
| Workload-level byte counters | A1, B2 | Standard flow export |
| Egress classification | A1, B1 | `spec.processor.egress` / enable internet classification |
| DNS latency | A2 | Enable DNS tracking in collector |
| RTT (optional) | A3 enrichment | RTT feature gate |
| Drop counts | A3 | Drop metrics enabled |
| Zone / AZ labels on nodes | C1 v2 | Node labels synced to flows |

### From koku-metrics-operator (new)

Planned `ros:net_*` recording rules (hourly, aligned with ROS cadence):

- `ros:net_egress_bytes_total` — by namespace, workload, protocol
- `ros:net_internal_bytes_total` — cluster-internal
- `ros:net_dns_latency_seconds` — histogram
- `ros:net_packet_drops_total` — by src/dst class

**CSV:** `ros-openshift-network-usage-<YYYYMM>.csv` manifest entry alongside existing ROS files.

### From Koku (SaaS only)

- Egress rate cards (AWS $/GB, Azure, GCP)
- Optional: correlate namespace to cloud account labels for chargeback

On-prem: **no** egress `$` — performance and reliability messaging only.

---

## Architecture

### Plugin registration

| Property | Value |
|----------|-------|
| Name | `network` |
| Phase | **1 — Produce** |
| Gate | `ROS_ENABLE_NETWORK_RECS` (default `false`) |
| Tables | `daily_network_digests`, `network_recommendations` (names TBD) |

### Digest model (conceptual)

```sql
-- Conceptual; exact DDL in requirements when scheduled
daily_network_digests (
  org_id, cluster_id, namespace, workload,
  bucket_date,
  egress_bytes_max, egress_bytes_avg,
  internal_bytes_avg,
  cross_zone_bytes_avg,  -- nullable until v2 quality gate
  dns_latency_p99_ms,
  packet_drop_p95_rate,
  top_destinations_json  -- capped cardinality
)
```

### Recommendation engine

- **Terms:** 7 / 30 days (align with VM-style stability; network patterns weekly)
- **Thresholds:** Tenant Settings API `recommendation_type=network`
- **Notifications:** New codes TBD (egress high, DNS outlier, drops)

### API (target)

| Method | Path |
|--------|------|
| GET | `/api/cost-management/v1/recommendations/openshift/network/` |
| GET | `/api/cost-management/v1/recommendations/openshift/network/:id` |

Filters: `namespace`, `workload`, `cluster`, `recommendation_type` (egress, dns, health).

---

## On-prem vs SaaS value proposition

| Mode | Primary value | Savings metric |
|------|---------------|----------------|
| **SaaS (cloud workers)** | FinOps: egress + cross-zone $ | `estimated_monthly_egress_savings_usd` when B1 enabled |
| **On-prem** | Performance: DNS, drops, chatty paths | Latency reduction, incident prevention — no egress $ |

Both benefit from **A1–A3**; B1 requires Koku cloud rate integration.

---

## Thresholds and terms (defaults)

| Parameter | Default | Notes |
|-----------|---------|-------|
| Egress high threshold | 100 GiB/week/namespace | Tenant-tunable |
| DNS latency outlier | p99 > 100ms AND > 3× cluster median | A2 |
| Drop rate alert | p95 > 0.1% of packets | A3 |
| Min history days | 7 | Before first rec |
| Top destinations cap | 10 | Cardinality |
| Cross-zone enable | `false` | Until v2 |

Env locks via `ROS_NETWORK_*` prefix — see future [configurability.md](../architecture/configurability.md) section.

---

## Risks and mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| **NetObserv not installed** | No data | Clear prerequisite in docs; plugin no-op; UI explains install |
| **Flow cardinality explosion** | Operator/Prometheus pressure | Namespace/workload aggregation; top-N external hosts |
| **Cross-zone misclassification** | Bad placement recs | **Defer C1 to v2**; feature flag `ROS_NETWORK_CROSS_ZONE_ENABLED=false` |
| **KubeCost ClusterIP pitfall** | Internal traffic counted as egress | See dedicated section — do not ship C1 without validation |
| **SaaS rate card mismatch** | Wrong $ | Use Koku effective_rates; show bytes + $ separately |
| **Multi-cluster federation** | Incomplete graphs | Per-cluster recommendations only in v1 |

### KubeCost ClusterIP misclassification pitfall (why we defer cross-zone to v2)

Industry tools have historically misclassified traffic to **ClusterIP addresses** or **node-local destinations** as "internet egress" or "cross-region" because:

- Flow records show **virtual Service IPs** without immediate pod resolution
- NAT and kube-proxy paths obscure true dst workload
- Short-lived endpoints cause stale dst labels

**Impact:** Inflated egress $ and false "move workload" advice — destroys trust.

**ROS policy:**

1. **MVP (Tier A):** Use NetObserv **egress classification flags** where the operator explicitly marks internet-bound flows — do not infer egress from dst IP alone.
2. **v2 (Tier C1):** Cross-zone recommendations require:
   - Valid **topology.kubernetes.io/zone** (or equivalent) on **both** src and dst
   - Consistency checks (sanity cap: cross-zone bytes ≤ total bytes)
   - Optional shadow mode: report only, no UI promotion, until false-positive rate < threshold in field tests

This is why **co-location / topology-aware scheduling** recommendations are **Tier C / v2**, not MVP.

---

## Implementation phases and effort

| Phase | Work | Effort | Owner |
|-------|------|--------|-------|
| **NET-A** | Operator: `ros:net_*` queries + CSV + manifest | Medium | koku-metrics-operator |
| **NET-B** | Migrations, parser, `daily_network_digests` | Low–medium | ros-ocp-backend |
| **NET-C** | `recommendNetwork()` Tier A rules + notifications | Medium | ros-ocp-backend |
| **NET-D** | API list/detail + settings `recommendation_type=network` | Low | ros-ocp-backend + Koku API proxy |
| **NET-E** | SaaS B1 egress $ (Koku rates) | Medium | ros-ocp-backend + Koku |
| **NET-F** | Tier C1 cross-zone (v2) | High | All — after field validation |

**NET-A** can prototype against NetObserv's Prometheus metrics in lab clusters. **NET-C** depends on NET-A/B.

Rough estimate: **8–12 weeks** for Tier A MVP (NET-A through NET-D); +4–6 weeks for SaaS B1; C1 v2 separate.

---

## Testing (planned)

- Mock network CSV → digest upsert
- Threshold boundary tests (egress GiB/week)
- DNS outlier detection vs cluster baseline
- No NetObserv → plugin skips gracefully
- Cardinality cap on `top_destinations_json`
- Cross-zone rules **disabled** by default in CI

---

## Dependencies

| Dependency | Notes |
|------------|-------|
| NetObserv operator | Customer-installed; documented prerequisite |
| koku-metrics-operator NET-A | New CSV type |
| `ROS_ENABLE_NETWORK_RECS` | Master gate |
| Koku egress rates | SaaS B1 only |
| UI | Optimizations network section (out of scope for backend doc) |

---

## References

- [Network feature page (public)](../../docs-site/planned-features/network.md)
- OpenShift: [Network Observability](https://docs.openshift.com/container-platform/latest/networking/network_observability/installing-operators.html)
- KubeVirt network metrics (VM placement — separate from this plugin): [performance-analysis.md §30](../architecture/performance-analysis.md#30-openshift-virtualization-vm-recommendations)
- [Plugin phases](../architecture/plugin-phases.md)
