# ros-ocp-backend Documentation

## Architecture & Requirements

| Document | Description |
|----------|-------------|
| [requirements.md](architecture/requirements.md) | Master requirements document — 60 features, 87 REQs, database schema, phasing strategy, deployment model |
| [test-plan.md](architecture/test-plan.md) | TDD test plan covering all phases (Go stdlib + testify + testcontainers) |
| [performance-analysis.md](architecture/performance-analysis.md) | Performance analysis of the legacy Kruize pipeline and rationale for the native Go engine |
| [plugin-architecture.md](architecture/plugin-architecture.md) | Recommendation plugin architecture — registry, traits, optional legacy Kruize path |
| [koku-tdigest-idea.md](architecture/koku-tdigest-idea.md) | Historical: t-digest exploration for Koku (superseded by Go-side exact percentiles) |

## Archive & audits

| Location | Description |
|----------|-------------|
| [docs/archive/](archive/) | Completed phase plans kept for historical reference |
| [docs/audits/](audits/) | Issue audits and similar meta-documents ([490-issues.md](audits/490-issues.md)) |

## Implementation Plans (per phase)

| Document | Scope |
|----------|-------|
| [phase-0-critical-fixes.md](archive/phase-0-critical-fixes.md) | Phase 0: Critical bug fixes *(completed — archived)* |
| [phase-1-2-3-go-engine.md](archive/phase-1-2-3-go-engine.md) | Phases 1-3: Core Go engine, metrics pipeline, decay weighting *(completed — archived)* |
| [phase-4-oom-feedback.md](plans/phase-4-oom-feedback.md) | Phase 4: Memory algorithm with OOM feedback |
| [phase-4-pr-checklist.md](archive/phase-4-pr-checklist.md) | Phase 4: PR checklist *(completed — archived)* |
| [phase-5-history-and-boxplots.md](plans/phase-5-history-and-boxplots.md) | Phase 5: Recommendation history and box plots |
| [gpu-recommendations.md](plans/gpu-recommendations.md) | Phase 5: GPU recommendations |
| [gpu-recommendations-test-plan.md](plans/gpu-recommendations-test-plan.md) | Phase 5: GPU test plan |
| [gpu-timeslicing-tdd-plan.md](plans/gpu-timeslicing-tdd-plan.md) | Phase 5: GPU time-slicing TDD plan |
| [replica-count-and-cost-impact.md](plans/replica-count-and-cost-impact.md) | Phase 7: Replica count and dollar savings |

## Feature Documentation

| Document | Scope |
|----------|-------|
| [features-f26-f33-f54-f55.md](features-f26-f33-f54-f55.md) | Staleness, idle/abandoned detection, adoption, fleet summary |
| [features-f27-pvc-rightsizing.md](features-f27-pvc-rightsizing.md) | PVC right-sizing: oversized, near-full, orphaned, growth trend |
| [features-f-snapshot-staleness.md](features-f-snapshot-staleness.md) | Snapshot staleness detection (implemented — backend, operator, listener; optional UI) |
| [known-issues.md](known-issues.md) | **Feature status report** — executive summary for customer discussions, implementation status, UI gaps, known caveats |
| [kruize-vs-native-comparison.md](kruize-vs-native-comparison.md) | Comparison between Kruize and native engine |
| [native-engine-performance.md](native-engine-performance.md) | Native engine performance benchmarks |
| [native-engine-notification-gap.md](native-engine-notification-gap.md) | Notification gap analysis (native vs legacy) |
| [gpu-time-slicing-plan.md](gpu-time-slicing-plan.md) | GPU time-slicing design |
| [namespace-boxplots-performance-analysis.md](namespace-boxplots-performance-analysis.md) | Namespace box plots performance |
| [phase5-implementation-notes.md](phase5-implementation-notes.md) | Phase 5 implementation notes |
| [phase6-namespace-boxplots-implementation.md](phase6-namespace-boxplots-implementation.md) | Phase 6 namespace box plots implementation |

## Operations & Maintenance

| Document | Scope |
|----------|-------|
| [GPU Catalog Maintenance](operations/gpu-catalog.md) | How to add new GPU models, where to find specs, monitoring for gaps |
| [Upgrade Runbook](upgrade-runbook.md) | Database migration procedures and advisory lock patterns |
| [migrations/README.md](../migrations/README.md) | Migration best practices (CONCURRENTLY indexes, pre-steps) |

## API Reference

The authoritative API specification is [`openapi.json`](../openapi.json) at the repository root.

## External Resources

- [ROS OCP Wiki](https://github.com/RedHatInsights/ros-ocp-backend/wiki/Resource-Optimization-For-Openshift:-Architecture-Overview)
