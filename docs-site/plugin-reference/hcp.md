# hcp

> **Last verified:** 2026-10-27

Package: [`internal/plugins/hcp`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/plugins/hcp)

**HyperShift hosted-control-plane plugin** — two halves. The observer half
watches container CSV ingestion on management clusters for HyperShift
control-plane namespaces (counts + workload-inventory tripwire). The
fleet-capacity half (W4, [#392](https://github.com/pgarciaq/ros-ocp-backend/issues/392))
ingests the operator's `ros-openshift-hcp-capacity-*` snapshot CSV and persists
it as capacity history rows via `internal/ingestion`.

## Plugin metadata

| Property | Value |
|----------|-------|
| Name | `hcp` |
| Requires | `container` (observer reads container rows) |
| CSV types | `hcp-capacity` — `ros-openshift-hcp-capacity-*`, the 25-column operator contract (W4) |
| Terms | short 1d / medium 7d / long 15d (configurable, same defaults as sibling fast-moving plugins) |
| Honors | `ROS_DISABLED_PLUGINS=hcp`, Kruize mutual exclusivity |

## Traits

| Trait | Supported |
|-------|-----------|
| CSVIngestor | Yes — claims the `hcp-capacity` CSV type; rows are parsed and persisted as history by `internal/ingestion` (shared parser with the robne CLI, `librobne/csv`) |
| IngestHook | Yes — observes `container` rows after ingestion (`AfterIngest`) |
| RetentionProvider | No — the core history sweep owns `hcp_capacity_history` (the trait carries only the month cutoff; a plugin sweep would silently deliver 6 months instead of 90 days — tracked in [#701](https://github.com/pgarciaq/ros-ocp-backend/issues/701)) |
| APIProvider | Yes — registers the dedicated HCP surface: `GET /recommendations/openshift/hcp`, `GET .../hcp/:recommendation-id`, plus the fleet-capacity endpoints `GET .../hcp/fleet-capacity` (latest per authorized management cluster) and `GET .../hcp/fleet-capacity/history` (per-cluster trend). Routes are absent under Kruize or when disabled |
| TermProvider | Yes — short/medium/long windows via `DefaultTerms`, `MaxWindowDays` 90 |

## Observability

- `rosocp_hcp_namespace_rows_total` — HCP-namespaced rows observed
- `rosocp_hcp_pin_miss_total` — workload-inventory tripwire
- `rosocp_hcp_association_total` — namespace→hosted-cluster association marking (`associated`/`cleared`)
- `rosocp_hcp_correlator_runs_total` / `rosocp_hcp_correlation_advisories_total` — hourly cross-plane correlator

## Architecture

- Association persistence: [`manifest_hcp_snapshots` + `hosted_cluster_id` marking (#632)](https://github.com/pgarciaq/ros-ocp-backend/issues/632); frozen history IDs ([#634](https://github.com/pgarciaq/ros-ocp-backend/issues/634)); snapshot-union routing ([#631](https://github.com/pgarciaq/ros-ocp-backend/issues/631)).
- Fleet capacity (W4): ADR-0334 (amended) — published-sizing model from raw management-cluster inputs, MCE capacity gauges as a separately labeled reference; serve-time derivation shared with the robne CLI in `librobne/hcp`.
- Correlator policy (incl. W3 idle floor `z_idle_cpu_floor_mc`): three-tier `hcp-correlation` settings domain — see [configurability](../architecture/configurability.md).
- Consumer-facing contract: [Hosted Control Plane Recommendations](../features/hosted-control-plane.md).
