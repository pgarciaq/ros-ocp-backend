# Known Limitations

> **Last verified:** 2026-08-31

Current product and operational caveats for the native recommendation engine.
This is **not** a full feature-status matrix.

| Looking for… | Go here |
|--------------|---------|
| **Shipping / phase archaeology / REQ matrices** | [Feature status archive (Historical)](../docs-site/historical/feature-status-archive.md) (site: [historical/feature-status-archive](https://pgarciaq.github.io/ros-ocp-backend/historical/feature-status-archive/)) |
| **Planned product work** | [Features (planned)](https://pgarciaq.github.io/ros-ocp-backend/planned-features/) · issues such as [#340](https://github.com/pgarciaq/ros-ocp-backend/issues/340) (Java), MachineSet / Local Mode epics |
| **Open engineering gaps** | Table below + linked GitHub issues |

---

## Current caveats

| Issue | Impact | Severity |
|-------|--------|----------|
| Namespace recs can be disabled per-org | Cloud: Unleash `rosocp.namespace_disabled`. On-prem: always on. | By design |
| Node recommendation cold start (~3 days) | `GET .../nodes` empty until enough digest history | Low — by design |
| Legacy Kruize code still present | Optional path / dual-write work; native is default when enabled | Low when native-only |
| `workload_metrics` JSONB not dropped | Bypassed by native ingest; leftover schema | Low |
| Replica count fallback for old operators | `"source": "derived"` when CSV lacks `desired_replicas` | Low |
| Replica count missing if all pods die before scrape | Falls back to derived pod count | Very low |
| Savings stale until re-ingestion / recalc | Mitigated when `ROS_SAVINGS_RECALCULATION_ENABLED` + Koku recalc path deployed | Low when integrated |
| Partial Optimizations UI coverage | Some domains API-only (GPU ROS UI: [#447](https://github.com/pgarciaq/ros-ocp-backend/issues/447); snapshots / some history views) | Medium |
| Unparsable Kafka messages log full payload | May include `org_id`, URLs, presigned query strings — [#82](https://github.com/pgarciaq/ros-ocp-backend/issues/82) | Medium — policy-dependent |
| GPU summary `timeslicing.count` vs list | Summary counts telemetry triples; list is actionable rows — intentional | Document for UI |
| Custom image mixes `vendor/` and `./librobne` from different commits | Parent tests / hermetic builds can compile yesterday’s engine; product `Dockerfile` excludes `vendor/` via `.dockerignore` | Low — [#510](https://github.com/pgarciaq/ros-ocp-backend/issues/510); [Integrating librobne](https://pgarciaq.github.io/ros-ocp-backend/architecture/librobne/) |

---

## Open work (tracked issues)

| Topic | Issue |
|-------|-------|
| Remaining `rh_accounts` joins on **cluster-scoped** paths (list/history/quality done) | [#445](https://github.com/pgarciaq/ros-ocp-backend/issues/445) |
| Durable per-org fleet savings **rollup** (per-row cents + LRU done; cold-cache `SUM` remains) | [#446](https://github.com/pgarciaq/ros-ocp-backend/issues/446) |
| ~~koku-ui ROS GPU MIG / time-slicing Optimizations UI~~ | [#447](https://github.com/pgarciaq/ros-ocp-backend/issues/447) — **closed** (implemented) |
| Redact presigned URLs in Kafka error logs | [#82](https://github.com/pgarciaq/ros-ocp-backend/issues/82) |
| MIG + time-slicing combined strategy | [#28](https://github.com/pgarciaq/ros-ocp-backend/issues/28) |
| Materialize GPU time-slicing recommendations | [#29](https://github.com/pgarciaq/ros-ocp-backend/issues/29) |
| Retire / dual-write Kruize path | [#352](https://github.com/pgarciaq/ros-ocp-backend/issues/352) (and children) |
| Java / JVM recommendations | [#340](https://github.com/pgarciaq/ros-ocp-backend/issues/340) |

Query-performance methodology: [Query Performance](https://pgarciaq.github.io/ros-ocp-backend/query-performance/).

---

## GPU MIG / time-slicing (pointers)

Backend GPU MIG and time-slicing APIs ship; combined MIG+time-slicing strategy and large-fleet materialization remain deferred ([#28](https://github.com/pgarciaq/ros-ocp-backend/issues/28), [#29](https://github.com/pgarciaq/ros-ocp-backend/issues/29)). UI: [#447](https://github.com/pgarciaq/ros-ocp-backend/issues/447).

Deep Gap-5 narrative and deferred tables: [feature status archive § GPU MIG](https://pgarciaq.github.io/ros-ocp-backend/historical/feature-status-archive/#gpu-mig-known-limitations-gap-5).

---

## See also

- [What's New](https://pgarciaq.github.io/ros-ocp-backend/whats-new/)
- [API Pagination](https://pgarciaq.github.io/ros-ocp-backend/pagination/)
- [UI Integration Guide](https://pgarciaq.github.io/ros-ocp-backend/ui-integration-guide/)
