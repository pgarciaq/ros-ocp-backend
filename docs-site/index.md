# ROS-OCP Backend

**Resource Optimization for OpenShift** — a Go backend that ingests cluster metrics from the [koku-metrics-operator](https://github.com/project-koku/koku-metrics-operator), runs native percentile-based recommendation engines, and serves optimization guidance through a REST API consumed by the [Cost Management UI](https://github.com/project-koku/koku-ui).

## What it does

ROS-OCP Backend turns historical OpenShift usage into actionable right-sizing: containers, namespaces, nodes, GPUs, storage, quotas, snapshots, and **OpenShift Virtualization (KubeVirt) VMs**. Each domain is a compile-time plugin with configurable short/medium/long **term windows**, optional **cost** and **performance** engine perspectives, **dollar savings** estimates (where Koku cost data is available), and a unified **notification code** system so the UI can explain *why* a recommendation looks the way it does.

## Recommendation domains

| Plugin | Domain | What it recommends |
|--------|--------|-------------------|
| **container** | CPU & memory | Per-container requests/limits (P95/P99 percentiles, adaptive margins, OOM-aware memory bumps); inline idle/zombie classification |
| **namespace** | Namespace quotas | Namespace-level CPU/memory targets aggregated from container recs; namespace idle when all workloads are idle/zombie |
| **node** | Node sizing | Under/over-provisioned nodes, consolidation, target node CPU/memory (cost + performance engines) |
| **gpu** | NVIDIA GPUs | Utilization classification (idle, underutilized, memory/compute-bound), MIG profile selection, node time-slicing replicas; catalog-driven via `gpu_catalog.yaml` |
| **pvc** | Storage | PVC capacity right-sizing from growth trends (oversized, near-full, orphaned, healthy) |
| **quota** | ResourceQuota | Tighten/raise/optimal namespace ResourceQuota hard limits vs usage and container rec aggregates |
| **cluster-quota** | ClusterResourceQuota | Team/tenant ClusterResourceQuota pools vs aggregated namespace quota recommendations |
| **snapshot** | VolumeSnapshots | Orphaned, stale, redundant, and never-restored snapshots; recoverable cost estimates |
| **vm** | OpenShift Virtualization | Whole vCPU/GiB sizing, disk projection, I/O profiling, instance type matching, idle/abandoned VMs, crash-loop detection, GPU passthrough/vGPU/MIG on guests ([Preview Beta](features/virtual-machines.md); `ROS_ENABLE_VM_RECS=true`) |

**Engines:** `container`, `namespace`, `node`, and `vm` expose parallel **cost** (P95-oriented) and **performance** (P99 / stability-oriented) recommendations. Other plugins use a single sizing engine tuned via Settings API.

## Cross-cutting analysis

These capabilities span multiple plugins rather than separate registry entries:

| Capability | Scope | Highlights |
|------------|-------|--------------|
| **Idle / zombie detection** | Containers, GPUs, namespaces, VMs | Configurable thresholds via `GET/PUT .../settings/idle-detection`; full monthly waste estimates; `filter[idle_state]` on list APIs |
| **Business hours** | Container, namespace | Dual all-hours vs business-hours streams when clusters run on a schedule |
| **Historical tracking** | Container (primary) | Time-series of past recommendations; quality metrics (stability, adoption, OOM after change) |
| **Tag filtering** | Container | Filter by OpenShift labels synced from Koku (`filter[tag:key]=value`) |
| **Notification codes** | All native plugins | 77 notification codes (confidence, OOM, idle, GPU, PVC, snapshot, VM guest-agent, disk, crash loop, SPARSE_DATA, …) — [lookup table](architecture/notification-codes.md) |
| **Configurable thresholds & terms** | Per plugin | Three-tier precedence: admin `ROS_*` env (locks) → tenant Settings API → compiled defaults; custom 1–90 day windows per term |
| **Savings estimates** | Container, node, PVC, GPU, snapshot, quota (tighten) | Monthly dollar impact via Koku `effective_rates`; fleet rollup at `GET .../savings-summary` |

## Architecture highlights

- **Plugin-based**: Self-contained plugins implement trait interfaces (ingest, produce, API, terms, retention)
- **Native Go engine (default)**: Replaces legacy Kruize; relational `recommendation_sets` and domain digest tables — [migration guide](architecture/native-migration.md)
- **Dual engine**: Cost-minimizing vs headroom-maximizing perspectives for containers, namespaces, nodes, and VMs (VMs are native-only—Kruize never supported VMs; VM “dual engine” is cost vs performance within the native engine)
- **Multi-tenant**: Isolated per organization via `org_id` scoping
- **Prometheus-compatible**: Per-phase histograms and operational metrics
- **Cost-aware savings**: Integrates with Koku Masu `effective_rates` — [Cost Integration](architecture/cost-integration.md)

## Quick links

- [API Pagination](pagination.md) — Keyset vs offset strategy across all list endpoints
- [UI Integration Guide](ui-integration-guide.md) — REST API reference for koku-ui frontend developers
- [Savings estimations](features/savings-estimations.md) — Dollar savings, fleet rollup, recalculation
- [What's new](whats-new.md) — Native engine release highlights
- [Cost Integration](architecture/cost-integration.md) — Savings formulas, kill-switch, currency, fleet savings summary
- [Recommendation Engines](architecture/recommendation-engines.md) — Thresholds, percentiles, and term reference for all plugins
- [Configurability Reference](architecture/configurability.md) — Environment variables, Settings API, and tuning guidance
- [Notification codes](architecture/notification-codes.md) — All API notification codes in one place
- [Notification codes API](api-reference/notification-codes.md) — `GET .../notification-codes` reference catalog
- [Contributing Guide](contributing.md) — Setup, testing, PR process
- [Plugin Architecture](architecture/plugin-architecture.md) — How plugins work
- [Plugin Execution Phases](architecture/plugin-phases.md) — Phase and priority ordering
- [Plugin Reference](plugin-reference/index.md) — Auto-generated from source code
- [OpenAPI Specification](openapi.md) — OpenAPI/Swagger docs
- [Known Issues](known-issues.md) — Current limitations and workarounds
