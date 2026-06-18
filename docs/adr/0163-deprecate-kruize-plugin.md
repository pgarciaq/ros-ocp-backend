# ADR-0163: Deprecate and Remove the Kruize Plugin

## Status

Accepted

## Context

ROS-OCP-Backend originally integrated with Kruize (Autotune), an external Java service, as its recommendation engine. ADR-0001 established the native Go engine as the production path; ADR-0104 made Kruize mutually exclusive with native plugins. The native engine now covers all recommendation domains that Kruize ever supported—containers, namespaces, nodes, GPUs, PVCs, ResourceQuotas, ClusterResourceQuotas, VolumeSnapshots, and OpenShift Virtualization VMs—and adds capabilities Kruize never had (notification codes, settings API, dollar savings, recommendation history, tag filters, and more).

The Kruize plugin itself was never fully implemented as a first-class plugin. It registers only for enable/disable semantics and mutual exclusivity (`internal/plugins/kruize/plugin.go`); it does not implement `CSVIngestor`, `APIProvider`, or `TermProvider`. Processing logic remains scattered across `internal/services/report_processor.go` (legacy CSV dataframe path), `internal/utils/kruize/` (HTTP client), and `internal/types/kruizePayload/` (request/response types). The integration with Autotune/Kruize was never completed—some wiring to connect with the external service is missing or incomplete.

Four adversarial review findings in the Kruize code path were accepted as won't-fix specifically because Kruize is slated for removal:

| Finding | Issue | Location |
|---------|-------|----------|
| #33 | Legacy Kruize CSV path lacks `report_file_status` tracking | `internal/services/report_processor.go` |
| #39 | Kruize API debug logs include full HTTP payloads | `internal/utils/kruize/kruize_api.go` |
| #44 | Kruize legacy fetch errors misclassified as transient | `internal/services/report_processor.go` |
| #55 | Kruize heavy endpoints share global HTTP client timeout | `internal/utils/kruize/kruize_api.go` |

The plugin code remains as dead weight: it increases maintenance surface, complicates ingestion routing (`plugin.EnabledFor(plugin.KruizePluginName)` branches throughout the codebase), and requires deploying a separate Kruize/Autotune container that no production deployment should rely on.

## Decision

The Kruize plugin is formally deprecated. It will be removed in a future release.

- No further enhancements, bug fixes, or security hardening will be applied to Kruize plugin code paths.
- The native recommendation engine is the sole supported recommendation backend.
- Accepted adversarial review findings #33, #39, #44, and #55 will not be remediated in the Kruize path.
- Operators still running `ROS_ENABLED_PLUGINS=kruize` must migrate to the native engine before removal. (`ROS_USE_NATIVE_ENGINE` has been removed; see ADR-0157.)

## Consequences

### Positive

- Reduces codebase complexity and attack surface by eliminating legacy branching and external HTTP calls.
- Resolves four accepted adversarial review risks (#33, #39, #44, #55) by removing the affected code.
- Removes dependency on external Autotune/Kruize service and its Java runtime.
- Simplifies deployment: no Kruize sidecar/service, no `recommendation-poller`, no `KRUIZE_*` configuration.
- Consolidates all recommendation logic in the native Go engine and plugin architecture.

### Negative

- Operators who configured Kruize-only mode must migrate to the native engine (see [native-migration.md](../architecture/native-migration.md)).
- The Kruize vs Native comparison tool (ADR-0140) loses its Kruize baseline once the service is removed; comparison against historical exports remains possible.
- No features are uniquely available through Kruize that the native engine lacks. Native covers a strict superset: VM recommendations, quota/CRQ, snapshot analysis, dollar savings, settings API, 54+ notification codes, and business-hours dual streams were never available on the Kruize path.

### Removal Plan

| Phase | Scope | Status |
|-------|-------|--------|
| **Phase 1** | Mark all Kruize-specific config as deprecated in docs; publish this ADR | Current |
| **Phase 2** | Remove Kruize plugin package, legacy CSV path in `report_processor.go`, `internal/utils/kruize/`, `internal/types/kruizePayload/`, Kruize API constants, and `plugin.EnabledFor(KruizePluginName)` branches | Planned |
| **Phase 3** | Remove Kruize-related Helm chart values, `recommendation-poller` deployment, Kruize container images, `kruize-clowdapp.yaml`, and CI workflows (`test-ros-kruize-rm.yml`) | Planned |

## Phase Progress Update

**Native engine is sole recommendation source for queries.** Commit `f8dd05b1` removed `getNativeRecommendationByIDFallback()` from `internal/model/recommendation_set_native.go`. Detail lookup now uses indexed `container_id` only; composite-key fallback scans are no longer exercised.

Kruize plugin code remains for backward-compatible ingestion routing and mutual-exclusivity checks, but is **not** used for recommendation queries. Operators on native engine mode never hit Kruize HTTP endpoints for list/detail APIs.

## References

- [docs/audits/adversarial-review.md](../audits/adversarial-review.md) — Findings #33, #39, #44, #55
- [ADR-0001](0001-native-engine-over-kruize.md) — Native engine over Kruize
- [ADR-0104](0104-kruize-mutually-exclusive-native.md) — Kruize mutually exclusive with native plugins
- [ADR-0140](0140-kruize-vs-native-comparison-tool.md) — Kruize vs Native comparison tool
- [docs/architecture/native-migration.md](../architecture/native-migration.md) — Migration guide
- [internal/plugins/kruize/plugin.go](../../internal/plugins/kruize/plugin.go) — Plugin registration (traits only)
- [internal/utils/kruize/kruize_api.go](../../internal/utils/kruize/kruize_api.go) — HTTP client
- [internal/services/report_processor.go](../../internal/services/report_processor.go) — Legacy CSV ingestion path
