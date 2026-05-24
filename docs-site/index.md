# ROS-OCP Backend

**Resource Optimization for OpenShift** — a Go backend service that ingests OpenShift cluster metrics and serves resource optimization recommendations via HTTP.

## What it does

ROS-OCP Backend receives metric reports from the [koku-metrics-operator](https://github.com/project-koku/koku-metrics-operator) (installed on OpenShift clusters), processes them through domain-specific recommendation engines, and exposes results through a REST API consumed by the [Cost Management UI](https://github.com/project-koku/koku-ui).

## Recommendation Domains

| Plugin | Domain | What it recommends |
|--------|--------|-------------------|
| **container** | CPU & memory | Per-container request/limit sizing using percentile analysis |
| **gpu** | NVIDIA GPUs | Utilization classification, MIG partitioning, time-slicing replicas |
| **node** | Node sizing | Node over/under-provisioning, consolidation opportunities |
| **pvc** | Storage | PVC capacity right-sizing using growth trend projection |
| **namespace** | Quotas | Namespace-level resource quota recommendations |
| **snapshot** | VolumeSnapshots | Stale snapshot detection |

## Architecture Highlights

- **Plugin-based**: Each domain is a self-contained plugin implementing trait interfaces
- **Configurable terms**: Recommendations use short/medium/long time windows, customizable per-tenant
- **Dual engine**: Native Go engine (default) or legacy Kruize (Java) backend
- **Multi-tenant**: Isolated per organization via `org_id` scoping
- **Prometheus-compatible**: Exposes operational metrics for monitoring
- **Cost-aware savings:** Integrates with Koku Masu `effective_rates` for dollar estimates across container, node, PVC, GPU, and snapshot plugins; fleet savings summary at `GET .../savings-summary` ([Cost Integration](architecture/cost-integration.md))

## Quick Links

- [Cost Integration](architecture/cost-integration.md) — savings formulas, kill-switch, currency, fleet savings summary, plugin matrix
- [Contributing Guide](contributing.md) — setup, testing, PR process
- [Plugin Architecture](architecture/plugin-architecture.md) — how plugins work
- [Plugin Reference](api-reference/index.md) — auto-generated from source code
- [API Specification](openapi.md) — OpenAPI/Swagger docs
- [Known Issues](known-issues.md) — current limitations and workarounds
