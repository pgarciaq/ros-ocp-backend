# Known Limitations

> **Last verified:** 2026-11-14

Current product and operational caveats for the native recommendation engine.
This is **not** a full feature-status matrix.

| Looking for… | Go here |
|--------------|---------|
| **Shipping / phase archaeology / REQ matrices** | [Feature status archive (Historical)](historical/feature-status-archive.md) (site: [historical/feature-status-archive](https://pgarciaq.github.io/ros-ocp-backend/historical/feature-status-archive/)) |
| **Planned product work** | [Features (planned)](https://pgarciaq.github.io/ros-ocp-backend/planned-features/) · issues such as [#340](https://github.com/pgarciaq/ros-ocp-backend/issues/340) (Java), MachineSet / Local Mode epics |
| **Open engineering gaps** | Table below + linked GitHub issues |

---

## Current caveats

| Issue | Impact | Severity |
|-------|--------|----------|
| Namespace recs can be disabled per-org | Cloud: Unleash `rosocp.namespace_disabled`. On-prem: always on. | By design |
| Node recommendation cold start (~3 days) | `GET .../nodes` empty until enough digest history | Low — by design |
| No recommendations while every pod looks green | CSV `Succeeded` + pod `Running` only prove OLM installed the bundle; the operator can still have never reconciled. An empty `CostManagementMetricsConfig` `.status` means nothing was uploaded — see [Operator looks green but nothing uploads](testing/validating-native-engine.md#operator-looks-green-but-nothing-uploads) | Medium — check `.status` before debugging the engine |
| Operator log repeats `is forbidden: … cannot list resource` | Only the logger field decides: `collector.*` is best-effort (one CSV column stays empty, e.g. `machineset_name` → [#739](https://github.com/pgarciaq/ros-ocp-backend/issues/739)); any other logger is a reconcile stall that blocks uploads | Low (best-effort) → High (stall) |
| Legacy Kruize code still present | Optional path / dual-write work; native is default when enabled | Low when native-only |
| `workload_metrics` JSONB not dropped | Bypassed by native ingest; leftover schema | Low |
| Replica count fallback for old operators | `"source": "derived"` when CSV lacks `desired_replicas` | Low |
| Replica count missing if all pods die before scrape | Falls back to derived pod count | Very low |
| Savings stale until re-ingestion / recalc | Mitigated when `ROS_SAVINGS_RECALCULATION_ENABLED` + Koku recalc path deployed | Low when integrated |
| Partial Optimizations UI coverage | Some domains API-only (GPU ROS UI: [#447](https://github.com/pgarciaq/ros-ocp-backend/issues/447); snapshots / some history views) | Medium |
| Unparsable Kafka messages log full payload | May include `org_id`, URLs, presigned query strings — [#82](https://github.com/pgarciaq/ros-ocp-backend/issues/82) | Medium — policy-dependent |
| GPU summary `timeslicing.count` vs list | Summary counts telemetry triples; list is actionable rows — intentional | Document for UI |
| Custom image mixes `vendor/` and `./librobne` from different commits | Parent tests / hermetic builds can compile yesterday’s engine; product `Dockerfile` excludes `vendor/` via `.dockerignore` | Low — [#510](https://github.com/pgarciaq/ros-ocp-backend/issues/510); [Integrating librobne](architecture/librobne.md#vendor-vs-replace-image-builds) |
| List results differ from pre-November 2026 deployments | Corrected behaviors: previously-ignored `filter[category]` / `filter[engine]` now apply (counts change), variation sorts return data, ascending pagination no longer strands mid-list, numeric sorts order numerically | By design — corrections toward the documented contract ([#841](https://github.com/pgarciaq/ros-ocp-backend/issues/841)–[#844](https://github.com/pgarciaq/ros-ocp-backend/issues/844), [#850](https://github.com/pgarciaq/ros-ocp-backend/issues/850), [#852](https://github.com/pgarciaq/ros-ocp-backend/issues/852)) |
| Sleep advisory reclaim estimate grew (~60% on weekday schedules) | The node-hours/week estimate now counts non-scheduled days (weekends are part of the sleep window) — output differs from pre-November advisories | By design — correction toward the documented contract ([#848](https://github.com/pgarciaq/ros-ocp-backend/issues/848)) |
| Sleep advisories appear for schedules that previously never qualified | Timezone-shifted weekly schedules now evaluate correctly; the robne CLI recovers despite stale platform evidence rows | By design ([#845](https://github.com/pgarciaq/ros-ocp-backend/issues/845), [#847](https://github.com/pgarciaq/ros-ocp-backend/issues/847)) |
| Sleep advisory can over-trust sparse rotation patterns | A workload fleet rotating one active hour per container can still receive high-confidence sleep advice (the off-peak statistic's known residual); the hour-grain coverage fix is adopted but not yet implemented | Medium — [#835](https://github.com/pgarciaq/ros-ocp-backend/issues/835) |
| Page ordering can reorder across DST boundaries | A timestamp text-cast in the page sort can mis-order results in a DST-transition week | Low — [#851](https://github.com/pgarciaq/ros-ocp-backend/issues/851) |
| Populated downgrades refuse over duplicate rows | Rolling back migrations on a database whose data the older contract cannot hold now fails with remediation guidance instead of silently deleting | By design — the rollback safety policy ([#846](https://github.com/pgarciaq/ros-ocp-backend/issues/846); see the upgrade runbook) |
| Tag grain differs from Cost Management | Multi-value tags collapse to one value; the DB mode reads a per-run tag snapshot; tag warnings scope by cluster while Koku scopes by project/node too | Medium — [#814](https://github.com/pgarciaq/ros-ocp-backend/issues/814), [#815](https://github.com/pgarciaq/ros-ocp-backend/issues/815), [#834](https://github.com/pgarciaq/ros-ocp-backend/issues/834) |

---

## Open work (tracked issues)

| Topic | Issue |
|-------|-------|
| ~~Remaining `rh_accounts` joins on **cluster-scoped** paths~~ | [#445](https://github.com/pgarciaq/ros-ocp-backend/issues/445) — **closed** (implemented) |
| Durable per-org fleet savings **rollup** (per-row cents + LRU done; cold-cache `SUM` remains) | [#446](https://github.com/pgarciaq/ros-ocp-backend/issues/446) |
| ~~GPU digest `org_id` (unique, reads, drop 000186 cluster-only index)~~ | [#512](https://github.com/pgarciaq/ros-ocp-backend/issues/512) — **closed** (implemented) |
| ~~koku-ui ROS GPU MIG / time-slicing Optimizations UI~~ | [#447](https://github.com/pgarciaq/ros-ocp-backend/issues/447) — **closed** (implemented) |
| Redact presigned URLs in Kafka error logs | [#82](https://github.com/pgarciaq/ros-ocp-backend/issues/82) |
| MIG + time-slicing combined strategy | [#28](https://github.com/pgarciaq/ros-ocp-backend/issues/28) |
| Materialize GPU time-slicing recommendations | [#29](https://github.com/pgarciaq/ros-ocp-backend/issues/29) |
| Retire / dual-write Kruize path | [#352](https://github.com/pgarciaq/ros-ocp-backend/issues/352) (and children) |
| Java / JVM recommendations | [#340](https://github.com/pgarciaq/ros-ocp-backend/issues/340) |
| Stream-path digest flush can replace a complete container-day with a fragment | [#822](https://github.com/pgarciaq/ros-ocp-backend/issues/822) |
| Kafka commit protocol: failed-but-uncommitted reports can be acked by later commits | [#803](https://github.com/pgarciaq/ros-ocp-backend/issues/803) |
| Hour-grain activity coverage for the sleep advisory (the #835 fix) | [#835](https://github.com/pgarciaq/ros-ocp-backend/issues/835) |

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
