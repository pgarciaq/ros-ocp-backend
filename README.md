# Resource Optimization for OpenShift

Backend for the Resource Optimization for OpenShift (ROS-OCP) service.
Provides CPU, memory, and GPU rightsizing recommendations for containerized
workloads running on OpenShift clusters.

## Recommendation Engines

The service supports two modes:

- **Native Go engine** (default) — built-in multi-term recommendation engine
  with exponential decay weighting, OOM-aware feedback, GPU classification,
  MIG profile selection, time-slicing analysis, and cost savings estimation.
  Processes container-level CSV digests into daily aggregates, computes
  short/medium/long term recommendations, and persists results in PostgreSQL.
  Enabled whenever the **`kruize`** plugin is **not** active (see **Plugins** below).

- **Kruize (legacy)** — delegates recommendation computation to the
  [Kruize Autotune](https://github.com/kruize/autotune) service via HTTP.
  Enable with **`ROS_ENABLED_PLUGINS=kruize`**. The deprecated **`ROS_USE_NATIVE_ENGINE=false`** flag forces **`ROS_ENABLED_PLUGINS=kruize`** at startup (overriding any previous allowlist) via [`ApplyLegacyUseNativeEngineEnv`](internal/plugin/registry.go); prefer setting **`ROS_ENABLED_PLUGINS=kruize`** explicitly.

## Plugins

ros-ocp-backend uses a plugin architecture for recommendation domains. Plugins are enabled/disabled via environment variables (see [`internal/plugin/registry.go`](internal/plugin/registry.go)).

**Available plugins:**

| Plugin | Type | Default | Description |
|--------|------|---------|-------------|
| `container` | CSVIngestor + RetentionProvider | Enabled | Container CPU/memory recommendations |
| `gpu` | IngestHook + APIProvider + RetentionProvider + APIEnricher | Enabled | GPU time-slicing and MIG recommendations |
| `node` | IngestHook + APIProvider + RetentionProvider | Enabled | Node capacity/utilization recommendations |
| `namespace` | CSVIngestor + APIProvider + RetentionProvider | Enabled | Namespace-level recommendations |
| `pvc` | CSVIngestor + APIProvider + RetentionProvider | Enabled | PVC/storage recommendations |
| `snapshot` | CSVIngestor + APIProvider | Enabled | Snapshot/staleness processing |
| `kruize` | Legacy engine | **Disabled** | Legacy Kruize-based recommendations (mutually exclusive) |

**Configuration:**

```bash
# Default: all native plugins enabled, kruize disabled
# (no env vars needed)

# Enable specific plugins only:
ROS_ENABLED_PLUGINS=container,gpu,node

# Disable specific plugins:
ROS_DISABLED_PLUGINS=gpu

# Switch to legacy Kruize engine (disables all native plugins):
ROS_ENABLED_PLUGINS=kruize

# Deprecated (forces ROS_ENABLED_PLUGINS=kruize — ignores prior allowlist):
ROS_USE_NATIVE_ENGINE=false
```

**Rules:**

- Enabling `kruize` automatically disables all other plugins (mutual exclusivity).
- Native plugins can be individually enabled/disabled.
- `ROS_ENABLED_PLUGINS` is an allowlist (only listed plugins active).
- `ROS_DISABLED_PLUGINS` is a blocklist (all plugins active except listed).
- If both are set, `ROS_ENABLED_PLUGINS` takes precedence.

## Key Features (Native Engine)

- **Multi-term recommendations** — short (1d), medium (7d), long (15d) windows
  with configurable per-org overrides via settings API
- **Cost and performance engines** — separate recommendation profiles optimized
  for cost reduction vs performance headroom
- **GPU workload classification** — idle, underutilized, memory-bound,
  compute-bound, well-utilized, no-profiling (based on DCGM metrics)
- **MIG profile recommendations** — for A100/A30/H100/H200/B100/B200 GPUs
- **GPU time-slicing analysis** — node-level time-slicing recommendations for
  non-MIG GPUs with per-container cross-references
- **Cost savings estimation** — integrates with Koku `effective_rates` endpoint
  for CPU/memory/GPU dollar savings (API fields use structured `estimated_monthly_savings`)
- **Tag filtering** — filter container lists by OpenShift labels (`filter[tag:key]`); optional savings grouping by tag
- **Namespace-level recommendations** — aggregated project-level recommendations
  with boxplot visualizations
- **Recommendation history and quality tracking** — historical snapshots,
  stability metrics, OOM event correlation
- **Configurable thresholds** — GPU classification thresholds are loaded from environment variables (`ROS_GPU_IDLE_THRESHOLD`, etc.) into the application configuration at startup (alongside defaults and optional config files).

## API Endpoints

All endpoints are under `/api/cost-management/v1/`:

| Endpoint | Description |
|----------|-------------|
| `GET /recommendations/openshift` | List container recommendations |
| `GET /recommendations/openshift/:recommendation-id` | Container recommendation detail |
| `GET /recommendations/openshift/fleet-summary` | Organization-wide aggregates: total/active/idle/abandoned container counts, summed monthly savings, distinct cluster count (RBAC cluster filter when enabled) |
| `GET /recommendations/openshift/savings-summary` | Fleet savings by plugin/cluster; optional `group_by[tag:key]` when tag filtering is enabled |
| `GET /recommendations/openshift/gpu` | GPU summary (counts, links to timeslicing/MIG listings) |
| `GET /recommendations/openshift/gpu/timeslicing` | Node-level GPU time-slicing recommendations |
| `GET /recommendations/openshift/gpu/mig` | Containers with MIG profile recommendations (non-`full_gpu`) |
| `GET /recommendations/openshift/nodes` | Node CPU/memory utilization recommendations |
| `GET /recommendations/openshift/nodes/utilization` | Deprecated alias of `/nodes` (same response + warning) |
| `GET /recommendations/openshift/namespaces` | Namespace recommendations list |
| `GET /recommendations/openshift/namespaces/:recommendation-id` | Namespace recommendation detail |
| `GET /recommendations/openshift/pvcs` | PVC/storage recommendations |
| `GET /recommendations/openshift/snapshots` | Volume snapshot staleness recommendations |
| `GET /recommendations/openshift/settings/snapshot` | Snapshot recommendation settings |
| `PUT /recommendations/openshift/settings/snapshot` | Update snapshot recommendation settings |
| `GET /openshift/namespace/recommendations` | Legacy alias for namespace list (same handler as `/namespaces`) |
| `GET /recommendations/openshift/namespace/:recommendation-id` | Legacy alias for namespace detail (same handler as `/namespaces/:recommendation-id`) |
| `GET /recommendations/openshift/settings/terms` | Get term configuration |
| `PUT /recommendations/openshift/settings/terms` | Set custom term windows |
| `DELETE /recommendations/openshift/settings/terms` | Reset to defaults |
| `GET /recommendations/openshift/history` | Recommendation history |
| `GET /recommendations/openshift/quality` | Quality/stability metrics |

## Documentation

### In-repo docs (`docs/`)

- [Recommendation plugin architecture](docs/architecture/plugin-architecture.md)
- [GPU time-slicing plan](docs/plans/gpu-timeslicing-implementation-plan.md)
- [GPU recommendations design](docs/plans/gpu-recommendations.md)
- [Native engine performance benchmarks](docs/native-engine-performance.md)
- [Namespace boxplots implementation](docs/archive/phase6-namespace-boxplots-implementation.md)
- [Native vs Kruize comparison tool](docs/kruize-vs-native-comparison.md)

### External docs

- [Architecture overview (wiki)](https://github.com/RedHatInsights/ros-ocp-backend/wiki/Resource-Optimization-For-Openshift:-Architecture-Overview)
- [Data aggregation examples (wiki)](https://github.com/RedHatInsights/ros-ocp-backend/wiki/Data-Aggregation-%7C-Report-Exemplum)
- [Kruize API schema](https://github.com/kruize/autotune/blob/mvp_demo/design/MonitoringModeAPI.md)
- [Deploying in ephemeral environment (wiki)](https://github.com/RedHatInsights/ros-ocp-backend/wiki/Deploying-in-ephemeral-environment)
- [Dev environment setup (wiki)](https://github.com/RedHatInsights/ros-ocp-backend/wiki/Dev-environment-setup-(local))

## Blogs

- [Red Hat Insights Brings Resource Optimization to Red Hat OpenShift](https://www.redhat.com/en/blog/red-hat-insights-brings-resource-optimization-red-hat-openshift)
