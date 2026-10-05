# hcp

> **Last verified:** 2026-10-05

Package: [`internal/plugins/hcp`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/plugins/hcp)

**HyperShift namespace observer** — watches container CSV ingestion on
management clusters for HyperShift control-plane namespaces (counts +
workload-inventory tripwire). Writes nothing itself; the dedicated HCP
surface lives in core handlers.

## Plugin metadata

| Property | Value |
|----------|-------|
| Name | `hcp` |
| Requires | `container` (reads container rows) |
| CSV types | None (observes `container` rows via hook) |
| Terms | short 1d / medium 7d / long 15d (configurable, same defaults as sibling fast-moving plugins) |
| Honors | `ROS_DISABLED_PLUGINS=hcp`, Kruize mutual exclusivity |

## Traits

| Trait | Supported |
|-------|-----------|
| CSVIngestor | No — no new CSV type |
| IngestHook | Yes — observes `container` rows after ingestion (`AfterIngest`) |
| RetentionProvider | No — reuses container digests |
| APIProvider | Yes — registers `GET /recommendations/openshift/hcp` and `GET .../hcp/:recommendation-id` (absent under Kruize or when disabled) |
| TermProvider | Yes — short/medium/long windows via `DefaultTerms`, `MaxWindowDays` 90 |

## Observability

- `rosocp_hcp_namespace_rows_total` — HCP-namespaced rows observed
- `rosocp_hcp_pin_miss_total` — workload-inventory tripwire

## Architecture

- Association persistence: [`manifest_hcp_snapshots` + `hosted_cluster_id` marking (#632)](https://github.com/pgarciaq/ros-ocp-backend/issues/632); frozen history IDs ([#634](https://github.com/pgarciaq/ros-ocp-backend/issues/634)); snapshot-union routing ([#631](https://github.com/pgarciaq/ros-ocp-backend/issues/631)).
- Serving: dedicated surface ([#638](https://github.com/pgarciaq/ros-ocp-backend/issues/638)), grouped savings ([#639](https://github.com/pgarciaq/ros-ocp-backend/issues/639)), projection slice ([#651](https://github.com/pgarciaq/ros-ocp-backend/issues/651)).
- Consumer-facing contract: [Hosted Control Plane Recommendations](../features/hosted-control-plane.md).
