# ros-ocp-backend Documentation

## UI Integration

| Document | Description |
|----------|-------------|
| [ui-integration-guide.md](ui-integration-guide.md) | **Frontend integration guide** — REST API reference for koku-ui: recommendations, settings, notifications, pagination, UX patterns |

## Architecture & Design

| Document | Description |
|----------|-------------|
| [design/seasonality-plugin.md](design/seasonality-plugin.md) | **Planned** — Seasonality detection, Augurs forecasting, proactive recommendations |
| [design/vm-recommendations.md](design/vm-recommendations.md) | **Implemented (phase11)** — OpenShift Virtualization VM right-sizing, instance types, notifications, API |
| [design/vm-test-plan.md](design/vm-test-plan.md) | VM recommendations test inventory (unit, E2E, IQE) |
| [design/java-recommendations.md](design/java-recommendations.md) | **Planned** — Java/JVM heap, GC, thread pools, non-heap OOM, MaxRAMPercentage (Phase 9) |
| [design/network-recommendations.md](design/network-recommendations.md) | **Planned** — NetObserv integration, egress/DNS/drops, SaaS cost attribution, cross-zone v2 |
| [requirements.md](architecture/requirements.md) | Master requirements document — 60 features, 87 REQs, database schema, phasing strategy, deployment model |
| [plugin-architecture.md](architecture/plugin-architecture.md) | Recommendation plugin architecture — registry, traits, optional legacy Kruize path |
| [recommendation-engines.md](architecture/recommendation-engines.md) | Plugin thresholds, percentiles, terms, and engine behavior |
| [configurability.md](architecture/configurability.md) | **Environment variable reference** — all `ROS_*` vars, Settings API routes, precedence model, tuning guidance |
| [database-conventions.md](architecture/database-conventions.md) | **Schema design** — when to use JSONB vs normalized tables, decision matrix, anti-patterns |
| [recommendation-math.md](architecture/recommendation-math.md) | Recommendation algorithms — decay weighting, adaptive margin, trend detection |
| [gpu-classification.md](architecture/gpu-classification.md) | GPU utilization classification thresholds and MIG profile selection |
| [gpu-catalogs.md](architecture/gpu-catalogs.md) | `gpu_catalog.yaml` / `vgpu_profiles.yaml` data sources and validation |
| [cost-integration.md](architecture/cost-integration.md) | Cost/savings integration with Koku — `effective_rates`, kill-switch, currency field, fleet savings summary, node/PVC/container/GPU formulas, plugin matrix |
| [kafka-schema.md](architecture/kafka-schema.md) | Kafka message schemas between Koku and ROS |
| [api-versioning.md](architecture/api-versioning.md) | API versioning strategy and compatibility policy |
| [native-migration.md](architecture/native-migration.md) | Legacy Kruize to native engine migration guide |
| [performance-analysis.md](architecture/performance-analysis.md) | Legacy pipeline performance analysis (historical — see staleness notice) |
| [test-plan.md](architecture/test-plan.md) | TDD test plan covering all phases |
| [koku-tdigest-idea.md](architecture/koku-tdigest-idea.md) | Historical: t-digest exploration (superseded by Go-side exact percentiles) |

## Operations & Maintenance

| Document | Scope |
|----------|-------|
| [Runbooks](operations/runbooks.md) | Alert response procedures, failure mode diagnosis |
| [Retention Policy](operations/retention.md) | Data lifecycle, partition sweeps, configuration |
| [Stale Detection](operations/stale-detection.md) | ROS staleness (code 2) and Koku SaaS stale-source checks |
| [RBAC Model](operations/rbac.md) | Permission types, filtering logic, endpoint coverage |
| [GPU Catalog](operations/gpu-catalog.md) | How to add new GPU models, specs, monitoring for gaps |
| [Upgrade Runbook](upgrade-runbook.md) | Database migration procedures and advisory lock patterns |
| [migrations/README.md](../migrations/README.md) | Migration best practices (CONCURRENTLY indexes, pre-steps) |

## Feature Documentation

| Document | Scope |
|----------|-------|
| [features/quota-recommendations.md](features/quota-recommendations.md) | ResourceQuota right-sizing (`quota` plugin, priority 35) |
| [features-f26-f33-f54-f55.md](features-f26-f33-f54-f55.md) | Staleness, idle/abandoned detection, adoption, fleet summary, fleet savings summary |
| [features-f27-pvc-rightsizing.md](features-f27-pvc-rightsizing.md) | PVC right-sizing: oversized, near-full, orphaned, growth trend |
| [features-f-snapshot-staleness.md](features-f-snapshot-staleness.md) | Snapshot staleness detection |
| [known-issues.md](known-issues.md) | Feature status report — executive summary, implementation status, caveats |
| [kruize-vs-native-comparison.md](kruize-vs-native-comparison.md) | Comparison between Kruize and native engine |
| [native-engine-performance.md](native-engine-performance.md) | Native engine performance benchmarks |
| [native-engine-notification-gap.md](native-engine-notification-gap.md) | Notification gap analysis (native vs legacy) |
| [gpu-time-slicing-plan.md](gpu-time-slicing-plan.md) | GPU time-slicing design |
| [phase5-implementation-notes.md](phase5-implementation-notes.md) | Phase 5 implementation notes |
| [phase6-namespace-boxplots-implementation.md](phase6-namespace-boxplots-implementation.md) | Phase 6 namespace box plots implementation |

## Archive

| Location | Description |
|----------|-------------|
| [docs/archive/](archive/) | Completed phase plans and historical development notes |
| [docs/audits/](audits/) | Issue audits ([490-issues.md](audits/490-issues.md)) |

## API Reference

The authoritative API specification is [`openapi.json`](../openapi.json) at the repository root. See [api-versioning.md](architecture/api-versioning.md) for compatibility policy.

## Changelog

See [`CHANGELOG.md`](../CHANGELOG.md) at the repository root for API and behavioral changes.

## External Resources

- [ROS OCP Wiki](https://github.com/RedHatInsights/ros-ocp-backend/wiki/Resource-Optimization-For-Openshift:-Architecture-Overview)
