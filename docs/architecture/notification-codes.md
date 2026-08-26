# Notification codes reference (developer)

Canonical catalog of all `notification_code_definitions` codes used by the native ROS-OCP engine.
The catalog defines **81** notification codes (including **SPARSE_DATA**, code 77, **NODE_BH_NOT_PEAK_SAFE**, code 79, **GPU_BH_OFFICE_WINDOW**, code 80, **GPU_TS_BH_CLUSTER_WINDOW**, code 81, and **VM_BH_OFFICE_WINDOW**, code 82). Code **78** is not in Definitions.
For operator-facing explanations and remediation steps, see
[`docs-site/architecture/notification-codes.md`](../../docs-site/architecture/notification-codes.md)
(published on the developer site under **Architecture → Notification Codes**).

## System overview

| Layer | Location | Role |
|-------|----------|------|
| **Database** | `notification_code_definitions` (`code`, `name`, `severity`, `description`) | Source of truth for names and default messages |
| **Go constants** | [`librobne/types/notifications.go`](../../librobne/types/notifications.go) (canonical), product aliases in [`internal/engine/notifications.go`](../../internal/engine/notifications.go), [`librobne/gpu/timeslicing.go`](../../librobne/gpu/timeslicing.go) (36), [`librobne/vm/vm_notifications.go`](../../librobne/vm/vm_notifications.go) (37–57) | Emitters reference numeric codes |
| **API mapping** | [`internal/notifications/mapping.go`](../../internal/notifications/mapping.go) `Definitions` | Converts `SMALLINT[]` → full `notifications` map for **detail** and per-engine responses only |
| **VM JSONB** | [`librobne/vm/vm_notifications.go`](../../librobne/vm/vm_notifications.go) `vmBuildNotifications` | VM list/detail use lowercase `type` (`info`/`warning`/`critical`) in `vm_recommendations.notifications` |

**Storage by plugin:**

| Plugin | Table / column | List API | Detail / per-engine API |
|--------|----------------|----------|-------------------------|
| Container | `recommendation_sets.notification_codes` | `notification_codes` (int array) | Full `notifications` map |
| Namespace | `namespace_recommendation_sets.notification_codes` | `notification_codes` (int array) | Full `notifications` map |
| Node | `node_recommendations.notification_codes` | `notification_codes` (int array) | Full `notifications` map per engine |
| GPU (container) | Embedded in GPU enrichment / recommendation flow | `notification_codes` on list when present | Full map on detail GPU blocks |
| GPU time-slicing | Node rec + candidate containers | `notification_codes` on list | Code **36** in engine `notifications` |
| PVC | `pvc_recommendation_sets.notification_codes` | `notification_codes` (int array) | Full `notifications` map |
| Snapshot | `snapshot_recommendation_sets.notification_codes` | `notification_codes` (int array) | Full `notifications` map |
| VM | `vm_recommendations.notifications` | `notifications` JSON array. Code **82** is never on list. | Same array on detail. Nested **detail** `business_hours.notifications` is the Kruize **map** and may include **82** when BH sizing is present — not merged into the parent array |
| Quota / ClusterResourceQuota | 70–73 | `notification_codes` (int array) | Full `notifications` map on detail |

**ADR-0293/0294 list contract:** List rows expose `notification_codes` only (slim DTO).
Full `notifications` maps and `plots` are **detail-only**. Per-engine notification
emission follows **A-2 deduplication** (ADR-0293): codes appear on engine blocks in
detail responses, not duplicated at term or list level.

**robne CLI ([#492](https://github.com/pgarciaq/ros-ocp-backend/issues/492)):** `robne recommend` JSON BH siblings (`business_hours_node_recommendations`, `business_hours_gpu_recommendations`, `business_hours_gpu_timeslicing_recommendations`, `business_hours_vm_recommendations`) may include codes **79–82** on `notification_codes` when that row has sizing. All-hours CLI node/GPU/VM DTOs omit the key. The array is **only** those BH codes (not the engine catalog). Envelope stays **11**. Container/namespace CLI BH siblings do not use 79–82.

**Reference API:** `GET /api/cost-management/v1/recommendations/openshift/notification-codes` returns the full catalog from [`internal/notifications/Definitions`](../../internal/notifications/mapping.go) (sorted by code). Optional `filter[plugin]` (`container`, `namespace`, `node`, `gpu`, `pvc`, `snapshot`, `vm`, `quota`, `cluster-quota`) limits results. No identity header required. DB table `notification_code_definitions` remains the migration source of truth; Go maps must stay in sync (`TestDefinitionsMatchDB`).

### Maintaining codes

When adding a code:

1. Add `INSERT` in a new migration under `migrations/`.
2. Add `Notif*` constant in `librobne/types/notifications.go` (and product aliases if needed) or a domain file (`librobne/vm/vm_notifications.go`, `librobne/gpu/timeslicing.go`).
3. Emit from the appropriate engine function (see table below).
4. Extend `internal/notifications/mapping.go` `Definitions` if the code applies to non-VM APIs (codes **43–63** are VM-only JSONB today and are **not** in `Definitions`; extend through **63** when container APIs need them).
5. Run `go test ./internal/notifications/...` (`TestDefinitionsMatchDB` catches DB/Go drift).

---

## Master table (codes 1–81)

Severity in DB/API mapping is `INFO` | `WARNING` | `CRITICAL` (uppercase in `Definitions`).
VM JSONB uses lowercase equivalents.

| Code | DB name | Severity | Plugin | Emitted? | Primary emitter |
|------|---------|----------|--------|----------|-----------------|
| 1 | `LOW_CONFIDENCE` | WARNING | Container, Namespace | Yes | [`EvaluateNotificationsWithThresholds`](../../internal/engine/notifications.go), [`EvaluateNamespaceNotificationsWithThresholds`](../../librobne/namespace/notifications.go) |
| 2 | `STALE_DATA` | WARNING | Container, Namespace | Yes | [`EvaluateNotificationsWithThresholds`](../../internal/engine/notifications.go), [`EvaluateNamespaceNotificationsWithThresholds`](../../librobne/namespace/notifications.go) when `rec.Stale` |
| 3 | `OOM_DETECTED` | CRITICAL | Container | Yes | [`EvaluateNotificationsWithThresholds`](../../internal/engine/notifications.go) when `OOMCountSum > 0` |
| 4 | `PDB_CAVEAT` | WARNING | Node (MachineSet) | **No** | Reserved — not set by native engine |
| 5 | `IDLE_WORKLOAD` | INFO | Container | Yes | [`EvaluateNotificationsWithThresholds`](../../internal/engine/notifications.go) — idle/zombie/abandoned path |
| 6 | `RECOMMENDATION_APPLIED` | INFO | Container | Yes | [`MarkAdopted`](../../internal/engine/adoption.go) |
| 7 | `NEW_WORKLOAD` | INFO | Container, Namespace | Yes | `DataDays < 1` in evaluate functions |
| 8 | `ABANDONED_WORKLOAD` | — | Container | No | Removed — zombie classification via `idle_state` supersedes it. Containers with zero usage are now classified as `idle_state='zombie'` and receive code 5 instead. |
| 9 | `MEMORY_TRENDING_UP` | WARNING | Container, Namespace | Yes | `MemTrendSlope` above threshold |
| 10 | `GPU_UNDERUTILIZED` | INFO | GPU | Yes | [`gpu_recommender.go`](../../librobne/gpu/recommend.go) classification `underutilized` / `compute_bound_underutil` |
| 11 | `NODE_UNDERUTILIZED` | INFO | Node | Yes | [`classifyNode`](../../librobne/node/recommend.go) CPU+mem P95 below underutil threshold |
| 12 | `NODE_OVERCOMMITTED` | WARNING | Node | Yes | [`classifyNode`](../../librobne/node/recommend.go) request/allocatable ratio |
| 13 | `STRANDED_RESOURCES` | INFO | Node | Yes | [`classifyNode`](../../librobne/node/recommend.go) CPU/mem imbalance EMA |
| 14 | `AUTOSCALER_SATURATED` | WARNING | Node | **No** | Reserved — MachineAutoscaler Tier 3 |
| 15 | `NODE_IDLE` | INFO | Node | Yes | [`applyNodeIdleClassification`](../../librobne/node/recommend.go) when `idle_state` is `idle` or `zombie` |
| 16 | `AUTOSCALER_FLAPPING` | WARNING | Node | **No** | Reserved |
| 17 | `AUTOSCALER_RECOMMENDED` | INFO | Node | **No** | Reserved |
| 18 | `VM_IDLE` | WARNING | VM | Yes | [`vmBuildNotifications`](../../librobne/vm/vm_notifications.go) |
| 19 | `VM_OVERSIZED` | WARNING | VM | Yes | [`vmBuildNotifications`](../../librobne/vm/vm_notifications.go) |
| 20 | `PVC_ORPHANED` | WARNING | PVC | Yes | [`pvc_recommend.go`](../../librobne/pvc/recommend.go) zero usage |
| 21 | `HPA_SATURATED` | WARNING | Container | **No** | Reserved — needs HPA metrics |
| 22 | `HPA_ACTIVE` | INFO | Container | **No** | Reserved |
| 23 | `INSTANCE_TYPE_NOT_IN_CATALOG` | INFO | Node | **No** | Reserved — cloud catalog |
| 24 | `INSTANCE_TYPE_DEPRECATED` | INFO | Node | **No** | Reserved |
| 25 | `NO_COST_DATA` | INFO | Container, Node, PVC | Yes | [`savings.go`](../../internal/engine/savings.go), [`node_savings.go`](../../internal/engine/node/savings.go), [`pvc_savings.go`](../../internal/engine/pvc/pvc_savings.go) |

VM recommendations do not emit code **25**; when `ROS_SAVINGS_ESTIMATES_ENABLED=false` or masu rates are missing, API `savings` is JSON `null` and fleet `by_plugin.vm` is `0` until the next ingestion cycle with rates.
| 26 | `GPU_IDLE` | INFO | GPU | Yes | [`gpu_recommender.go`](../../librobne/gpu/recommend.go) `GPUClassIdle` |
| 27 | `GPU_MEMORY_BOUND` | INFO | GPU | Yes | [`gpu_recommender.go`](../../librobne/gpu/recommend.go) `GPUClassMemoryBound` |
| 28 | `GPU_NO_PROFILING_DATA` | INFO | GPU | Yes | [`gpu_recommender.go`](../../librobne/gpu/recommend.go) no DCGM profiling |
| 29 | `PVC_OVERSIZED` | INFO | PVC | Yes | [`pvc_recommend.go`](../../librobne/pvc/recommend.go) usage ratio below oversized threshold |
| 30 | `PVC_NEAR_FULL` | WARNING | PVC | Yes | High usage ratio or `DaysToFull` alert |
| 31 | `SNAPSHOT_ORPHANED` | WARNING | Snapshot | Yes | [`classifySnapshot`](../../librobne/snapshot/classify.go) |
| 32 | `SNAPSHOT_NEVER_RESTORED` | INFO | Snapshot | Yes | [`classifySnapshot`](../../librobne/snapshot/classify.go) |
| 33 | `SNAPSHOT_REDUNDANT` | INFO | Snapshot | Yes | [`classifySnapshot`](../../librobne/snapshot/classify.go) |
| 34 | `SNAPSHOT_STALE` | INFO | Snapshot | Yes | [`classifySnapshot`](../../librobne/snapshot/classify.go) |
| 35 | `SNAPSHOT_MANAGED` | INFO | Snapshot | Yes | [`classifySnapshot`](../../librobne/snapshot/classify.go) backup tool label |
| 36 | `GPU_TIMESLICING_CANDIDATE` | INFO | GPU, Node | Yes | [`ComputeNodeTimeslicingRec`](../../librobne/gpu/timeslicing.go) |
| 37 | `VM_DISK_GROWING_NO_CAPACITY` | WARNING | VM | Yes | [`vmBuildNotifications`](../../librobne/vm/vm_notifications.go) hypervisor disk growth, no guest agent |
| 38 | `VM_NO_GUEST_AGENT` | INFO | VM | Yes | [`vmBuildNotifications`](../../librobne/vm/vm_notifications.go) |
| 39 | `VM_HIGH_IO` | WARNING | VM | Yes | [`vmBuildNotifications`](../../librobne/vm/vm_notifications.go) I/O hint high |
| 40 | `VM_DISK_FILLING` | WARNING | VM | Yes | Guest `days_until_full < 90` |
| 41 | `VM_INSTANCE_TYPE` | INFO | VM | Yes | Instance type match produced |
| 42 | `VM_DISK_CRITICAL` | CRITICAL | VM | Yes | Guest filesystem &gt; 90% used |
| 43 | `VM_ABANDONED` | CRITICAL | VM | Yes | [`vmBuildNotifications`](../../librobne/vm/vm_notifications.go) — suppresses 18 |
| 44 | `VM_GUEST_AGENT_INTERRUPTED` | INFO | VM | Yes | Agent unstable on latest day (suppresses 38) |
| 45 | `VM_INSUFFICIENT_DATA` | INFO | VM | Yes | `confidence == low` |
| 46 | `VM_UNKNOWN_OS` | INFO | VM | Yes | Empty `guest_os` |
| 47 | `VM_WINDOWS_UPDATE_SPIKE` | INFO | VM | Yes | Windows P99≫P95 spread |
| 48 | `VM_CRASH_LOOP` | WARNING | VM | Yes | Restart count in window |
| 49 | `VM_DOWNSIZE_HELD` | INFO | VM | Yes | Performance engine stability hold |
| 50 | `VM_GPU_IDLE` | WARNING | VM | Yes | [`vmGPUClassificationNotificationCodes`](../../librobne/vm/vm_gpu.go) |
| 51 | `VM_GPU_UNDERUTILIZED` | WARNING | VM | Yes | [`vm_gpu.go`](../../librobne/vm/vm_gpu.go) |
| 52 | `VM_GPU_MEMORY_SATURATED` | WARNING | VM | Yes | [`vm_gpu.go`](../../librobne/vm/vm_gpu.go) |
| 53 | `VM_GPU_COMPUTE_SATURATED` | WARNING | VM | Yes | [`vm_gpu.go`](../../librobne/vm/vm_gpu.go) |
| 54 | `VM_GPU_MIXED_IDLE` | WARNING | VM | Yes | Some GPUs idle, others active |
| 55 | `VM_NETWORK_SATURATED` | WARNING | VM | Yes | Network-saturated workload; recommend n1 network-optimized instance type |
| 56 | `VM_VGPU_PROFILE_RECOMMENDED` | INFO | VM | Yes | vGPU profile recommended — see `recommended_vgpu_profile` |
| 57 | `VM_GPU_TIMESLICE_UNSAFE_FB` | WARNING | VM | Yes | GPU time-slicing not safe — frame-buffer usage too high for shared vGPU |
| 58 | `VM_IO_SEQUENTIAL` | INFO | VM | Yes | [`ClassifyIOPattern`](../../librobne/vm/vm_io_classification.go) — average I/O size ≥ sequential threshold |
| 59 | `VM_IO_RANDOM` | INFO | VM | Yes | [`ClassifyIOPattern`](../../librobne/vm/vm_io_classification.go) — average I/O size &lt; random threshold |
| 60 | `VM_REDUNDANT_COLOCATION` | WARNING | VM | Yes | [`DetectSameNodeRedundancy`](../../librobne/vm/vm_placement.go) — same-node peers with matching profile |
| 61 | `VM_UNEVEN_NODE_DISTRIBUTION` | INFO | VM | Yes | [`DetectSameNodeRedundancy`](../../librobne/vm/vm_placement.go) — skew ratio across nodes for profile group |
| 62 | `VM_SHARED_STORAGE` | INFO | VM | Yes | [`DetectSharedPVCs`](../../librobne/vm/vm_pvc_correlation.go) — correlated profile peers in namespace |
| 63 | `VM_NUMA_OVERSIZED` | WARNING | VM | Yes | [`CheckNUMAFit`](../../librobne/vm/vm_numa_check.go) — memory exceeds `numa_node_memory_gib` |
| 74 | `NODE_POD_SCHEDULING_LIMIT` | WARNING | Node | Yes | [`classifyNode`](../../librobne/node/recommend.go) — low pod scheduling headroom on node |
| 76 | `NODE_FLEET_CONSOLIDATION` | INFO | Node | Yes | [`applyInstanceTypeConsolidation`](../../librobne/node/recommend.go) — fleet consolidation opportunity (MachineSet/instance-type group has excess nodes) |
| 77 | `SPARSE_DATA` | INFO | Container, Namespace, Node, PVC | Yes | [`EvaluateNotificationsWithThresholds`](../../internal/engine/notifications.go), [`EvaluateNamespaceNotificationsWithThresholds`](../../librobne/namespace/notifications.go), [`evaluateNodeNotifications`](../../librobne/node/recommend.go), [`EvaluatePVCNotifications`](../../librobne/pvc/recommend.go) — `data_days <= sparse_data_threshold` (default 2) |
| 79 | `NODE_BH_NOT_PEAK_SAFE` | WARNING | Node | Yes | [`attachNodeBHEngine`](../../internal/engine/recommend_node_business_hours.go) — nested node-detail `business_hours` sizing only (not list/parent merge). CLI: [`bhOnlyNotificationCodes`](../../cmd/robne/output.go) on `business_hours_node_recommendations` rows ([#492](https://github.com/pgarciaq/ros-ocp-backend/issues/492)) |
| 80 | `GPU_BH_OFFICE_WINDOW` | WARNING | GPU | Yes | [`attachGPUBusinessHoursToDetail`](../../internal/engine/recommend_gpu_business_hours.go) — nested container-detail `gpu.{term}.business_hours` sizing only (not list/MIG/timeslicing/parent merge). CLI: BH GPU sibling rows ([#492](https://github.com/pgarciaq/ros-ocp-backend/issues/492)) |
| 81 | `GPU_TS_BH_CLUSTER_WINDOW` | WARNING | GPU | Yes | [`attachTimeslicingBusinessHours`](../../internal/engine/recommend_gpu_timeslicing_business_hours.go) — nested timeslicing-detail `business_hours` replica sizing only (not list/history/summary/parent merge). CLI: BH timeslicing sibling rows ([#492](https://github.com/pgarciaq/ros-ocp-backend/issues/492)) |
| 82 | `VM_BH_OFFICE_WINDOW` | WARNING | VM | Yes | [`EnrichVMDetailWithBusinessHours`](../../internal/engine/vm/vm_business_hours.go) — nested VM-detail `business_hours` vCPU/GiB sizing only (not list/history/CSV/parent array merge). CLI: BH VM sibling rows ([#492](https://github.com/pgarciaq/ros-ocp-backend/issues/492)) |

---

## Per-code detail

### Container plugin (`container`)

| Code | Constant | Trigger conditions | Message (DB / mapping) |
|------|----------|-------------------|------------------------|
| 1 | `NotifLowConfidence` | `confidence_level < low_confidence_threshold` (default 0.5) and `data_days > 0` | Less than 4 days of data available for this workload |
| 77 | `NotifSparseData` | `data_days > 0` and `data_days <= sparse_data_threshold` (default 2) | Recommendation based on limited data; accuracy improves with more observation time |
| 2 | `NotifStaleData` | `stale == true` — cluster not reported within `ROS_STALENESS_THRESHOLD_HOURS` (default 48h). See [`docs/operations/stale-detection.md`](../operations/stale-detection.md) | No new metrics data received for more than 48 hours |
| 3 | `NotifOOMDetected` | `oom_count_sum > 0` in term window | OOM kill events detected within the analysis window |
| 5 | `NotifIdleWorkload` | `IsIdle` or `idle_state` idle/zombie, or legacy abandoned path (see `recommend_all.go`) | Workload uses less than 1% of requested resources |
| 6 | `NotifRecApplied` | Current requests within 5% of prior recommendation ([`FindAdoptedContainers`](../../internal/engine/adoption.go) / [`MarkAdopted`](../../internal/engine/adoption.go)) | Resource change detected matching a previous recommendation |
| 7 | `NotifNewWorkload` | `data_days < 1` | Less than 24 hours of data — recommendation may be unstable |
| 8 | `NotifAbandonedWorkload` | Removed — zombie classification via `idle_state='zombie'` supersedes it ([Issue #86](https://github.com/pgarciaq/ros-ocp-backend/issues/86)) | Zero-usage containers are now classified as zombie and emit code 5 |
| 9 | `NotifMemoryTrendingUp` | `mem_trend_slope > mem_trend_slope_threshold` (container default 100 KiB/day) | Memory usage trend suggests capacity risk within 30 days |
| 21 | `NotifHPASaturated` | — | Not emitted |
| 22 | `NotifHPAActive` | — | Not emitted |
| 25 | `NotifNoCostData` | No Koku cost rates for cluster ([`applyContainerSavings`](../../internal/engine/savings.go)) | No cost data available — savings estimate not computed |

**Idle classification** ([`ClassifyIdleState`](../../internal/engine/idle_classification.go)) drives `idle_state` and complements codes 5/8. Defaults: idle when CPU and memory utilization vs requests fall below configured `%`; zombie when P95 and peak CPU near zero.

### Namespace plugin (`namespace`)

| Code | Constant | Trigger |
|------|----------|---------|
| 1 | `NotifLowConfidence` | Same as container, namespace aggregates |
| 77 | `NotifSparseData` | Same as container — absolute `data_days` at or below `sparse_data_threshold` |
| 2 | `NotifStaleData` | `stale == true` — same cluster staleness as containers ([`docs/operations/stale-detection.md`](../operations/stale-detection.md)) |
| 7 | `NotifNewWorkload` | `data_days < 1` |
| 9 | `NotifMemoryTrendingUp` | Namespace slope threshold **500 KiB/day** ([`MemTrendSlopeThreshold`](../../librobne/namespace/types.go)) |

Emitter: [`EvaluateNamespaceNotificationsWithThresholds`](../../librobne/namespace/notifications.go) during namespace recommendation write.

### Node plugin (`node`)

| Code | Constant | Trigger |
|------|----------|---------|
| 11 | `NotifNodeUnderutilized` | Avg CPU P95 and mem P95 both &lt; `UnderutilThreshold` |
| 12 | `NotifNodeOvercommitted` | `max(requests) / allocatable CPU > OvercommitThreshold` |
| 13 | `NotifStrandedResources` | EMA of CPU vs mem imbalance &gt; `StrandedImbalanceThreshold` (default 0.6) for ≥2 days |
| 1 | `NotifLowConfidence` | [`evaluateNodeNotifications`](../../librobne/node/recommend.go) — `confidence_level < 0.5` and `data_days > 0` |
| 77 | `NotifSparseData` | [`evaluateNodeNotifications`](../../librobne/node/recommend.go) — `data_days > 0` and `data_days <= 2` (default) |
| 25 | `NotifNoCostData` | [`applyNodeSavings`](../../internal/engine/node/savings.go) |
| 15 | `NotifNodeIdle` | `idle_state` is `idle` or `zombie` ([`ClassifyNodeIdleState`](../../librobne/node/recommend.go)) |
| 74 | `NotifNodePodSchedulingLimit` | `pod_scheduling_headroom` below `pod_headroom_notification_threshold` (default 10%) |
| 76 | `NotifNodeFleetConsolidation` | Fleet consolidation assigned `node_count_reduction` for this node |
| 79 | `NotifNodeBHNotPeakSafe` | Nested `business_hours` sizing on node **detail** engines — overnight spikes outside the cluster schedule are excluded. Not on list or parent engine maps. Reason-only insufficient-data blocks omit 79 |
| 4, 14–17, 23–24 | — | Reserved (14–17 MachineAutoscaler Tier 3; **75** reserved for future minReplicas) |

Emitter: [`classifyNode`](../../librobne/node/recommend.go) and [`applyNodeIdleClassification`](../../librobne/node/recommend.go) → persisted in [`PersistRecommendations`](../../internal/engine/node/recommend.go).

### GPU plugin (`gpu`)

| Code | Constant | Trigger |
|------|----------|---------|
| 10 | `NotifGPUUnderutilized` | Classification `underutilized` or `compute_bound_underutil` |
| 26 | `NotifGPUIdle` | Classification `idle` |
| 27 | `NotifGPUMemBound` | Classification `memory_bound` |
| 28 | `NotifGPUNoProfilingData` | No DCGM profiling metrics in digests |
| 36 | `NotifGPUTimeSharingCandidate` | Node passes time-slicing heuristics ([`ComputeNodeTimeslicingRec`](../../librobne/gpu/timeslicing.go)); appended to candidate containers |
| 80 | `NotifGPUBHOfficeWindow` | Nested `business_hours` sizing on container **detail** `gpu.{term}` — overnight training and off-hours bursts are excluded. Not on list, MIG list, timeslicing, or parent GPU maps. Reason-only insufficient-data blocks omit 80 |
| 81 | `NotifGPUTSBHClusterWindow` | Nested `business_hours` replica sizing on **GET .../gpu/timeslicing/{node}** — overnight training and off-hours bursts are excluded. Not on list, history, summary, or parent `notification_codes`. Reason-only insufficient-data blocks omit 81. Heterogeneous windows omit the nested object |
| 81 | `NotifGPUTSBHClusterWindow` | Nested `business_hours` replica sizing on **GET .../gpu/timeslicing/{node}** — overnight training and off-hours bursts are excluded. Not on list, history, summary, or parent `notification_codes`. Reason-only insufficient-data blocks omit 81. Heterogeneous windows omit the nested object |

Thresholds: [`GPUThresholds`](../../librobne/gpu/recommend.go) / Settings API `gpu` section. See [`gpu-classification.md`](gpu-classification.md).

### PVC plugin (`pvc`)

| Code | Constant | Trigger |
|------|----------|---------|
| 1 | `NotifLowConfidence` | `confidence_level < low_confidence_threshold` and `data_days > 0` |
| 77 | `NotifSparseData` | `data_days > 0` and `data_days <= sparse_data_threshold` (container defaults) |
| 20 | `NotifPVCOrphaned` | All intervals zero usage, ≥ `MinDataDays` |
| 29 | `NotifPVCOversized` | `usage_ratio < OversizedThreshold`, recommended size &lt; capacity |
| 30 | `NotifPVCNearFull` | `usage_ratio > NearFullThreshold` or `days_to_full < DaysToFullAlert` |
| 25 | `NotifNoCostData` | [`applyPVCSavings`](../../internal/engine/pvc/pvc_savings.go) |

Emitter: [`ComputePVCRecommendation`](../../librobne/pvc/recommend.go). Feature doc: [`docs/features-f27-pvc-rightsizing.md`](../features-f27-pvc-rightsizing.md).

### Snapshot plugin (`snapshot`)

Priority order in [`classifySnapshot`](../../librobne/snapshot/classify.go): orphaned → managed → redundant → stale → never_restored → active (no code).

| Code | Constant | Trigger |
|------|----------|---------|
| 31 | `NotifSnapshotOrphaned` | Source PVC deleted, age &gt; `OrphanAgeDays` |
| 35 | `NotifSnapshotManaged` | Backup-tool annotation detected |
| 33 | `NotifSnapshotRedundant` | Older snapshot when newer ones exist for same PVC |
| 34 | `NotifSnapshotStale` | Age &gt; `StaleDays`, never restored |
| 32 | `NotifSnapshotNeverUsed` | Age &gt; `NeverRestoredDays`, `RestoredPVCCount == 0` |

### VM plugin (`vm`)

VM messages are built in [`vmBuildNotifications`](../../librobne/vm/vm_notifications.go) and [`vmNotificationForGPUCode`](../../librobne/vm/vm_gpu.go); may differ slightly from DB `description` (runtime `fmt.Sprintf`).

| Code | Constant | Trigger (summary) |
|------|----------|-------------------|
| 18 | `NotifVMIdle` | CPU+memory P95 below OS idle thresholds; not abandoned |
| 19 | `NotifVMOversized` | Recommended vCPU/memory below current by resize threshold |
| 37 | `NotifVMDiskGrowingNoCapacity` | Hypervisor disk growth, no guest agent capacity |
| 38 | `NotifVMNoGuestAgent` | Never stable guest agent (and not 44) |
| 39 | `NotifVMHighIO` | Total P95 IOPS above high threshold |
| 40 | `NotifVMDiskFillingGuest` | Guest `days_until_full < 90` |
| 41 | `NotifVMInstanceTypeRec` | `recommended_instance_type` set |
| 42 | `NotifVMDiskCritical` | Filesystem &gt; 90% used (guest agent) |
| 43 | `NotifVMAbandoned` | Zero CPU+memory max for N days — **replaces 18** |
| 44 | `NotifVMGuestAgentInterrupted` | Agent present in window but latest day &lt; 80% stable |
| 45 | `NotifVMInsufficientData` | `confidence == low` |
| 46 | `NotifVMUnknownOS` | `guest_os` empty |
| 47 | `NotifVMWindowsUpdateSpike` | Windows periodic spike heuristic |
| 48 | `NotifVMCrashLoop` | `restart_count_sum` ≥ threshold |
| 49 | `NotifVMDownsizeHeld` | Performance engine: downsize suppressed for stability |
| 50–54 | `NotifVMGPU*` | [`analyzeVMGPU`](../../librobne/vm/vm_gpu.go) / mixed-idle |
| 55 | `NotifVMNetworkSaturated` | [`RecommendVM`](../../librobne/vm/vm_recommender.go) — network-bound n1 recommendation |
| 56 | `NotifVMVGPUProfileRecommended` | [`analyzeVMGPU`](../../librobne/vm/vm_gpu.go) — vGPU profile in `recommended_vgpu_profile` |
| 57 | `NotifVMGPUTimeSliceUnsafeFB` | [`RecommendVMTimeSlicingForDevice`](../../librobne/vm/vm_gpu_timeslicing.go) — FB above safety threshold |
| 58 | `NotifVMIOSequential` | [`ClassifyIOPattern`](../../librobne/vm/vm_io_classification.go) — average I/O size ≥ sequential threshold |
| 59 | `NotifVMIORandom` | [`ClassifyIOPattern`](../../librobne/vm/vm_io_classification.go) — average I/O size &lt; random threshold |
| 60 | `NotifVMRedundantColocation` | [`DetectSameNodeRedundancy`](../../librobne/vm/vm_placement.go) — co-located redundant peers (`is_redundant_placement`) |
| 61 | `NotifVMUnevenNodeDistribution` | [`DetectSameNodeRedundancy`](../../librobne/vm/vm_placement.go) — uneven node spread for profile group |
| 62 | `NotifVMSharedStorage` | [`DetectSharedPVCs`](../../librobne/vm/vm_pvc_correlation.go) — correlated workload group (`has_shared_storage`) |
| 63 | `NotifVMNUMAOversized` | [`CheckNUMAFit`](../../librobne/vm/vm_numa_check.go) — memory &gt; single NUMA node cap (`numa_oversized`) |
| 64 | `NotifVMPowerOffSchedule` | [`DetectPowerOffCandidate`](../../internal/engine/vm/vm_power_schedule.go) — periodically idle VM (`is_power_off_candidate`) |
| 65 | `NotifVMNetworkQoSSRIOV` | [`EvaluateNetworkQoS`](../../internal/engine/vm/vm_network_qos.go) — network-bound VM with high throughput or packet drops |
| 66 | `NotifVMNetworkQoSDPDK` | [`EvaluateNetworkQoS`](../../internal/engine/vm/vm_network_qos.go) — network-bound VM with high PPS and small average packet size |
| 67 | `NotifVMStorageTierCold` | [`EvaluateStorageTiering`](../../internal/engine/vm/vm_storage_tiering.go) — sustained minimal daily I/O (cold-storage candidate) |
| 68 | `NotifVMStorageTierIOPS` | [`EvaluateStorageTiering`](../../internal/engine/vm/vm_storage_tiering.go) — sustained random high IOPS |
| 69 | `NotifVMStorageTierThroughput` | [`EvaluateStorageTiering`](../../internal/engine/vm/vm_storage_tiering.go) — sustained sequential high throughput |
| 82 | `NotifVMBHOfficeWindow` | [`EnrichVMDetailWithBusinessHours`](../../internal/engine/vm/vm_business_hours.go) — nested VM-detail `business_hours` when sizing is present (thin nest; not merged into parent array) |

Design detail: [`docs/design/vm-recommendations.md`](../design/vm-recommendations.md#notifications) and [placement (60–63)](../design/vm-recommendations.md#placement-correlated-workloads-and-numa-codes-6063).

---

## Reserved codes (defined, not emitted)

| Codes | Intended domain | Notes |
|-------|-----------------|-------|
| 4 | MachineSet / PDB | [`docs/known-issues.md`](../known-issues.md) |
| 14, 16–17 | MachineAutoscaler | Tier 3 MachineAutoscaler (not node idle) |
| 21–22 | HPA | Replica display exists; scaling advice does not |
| 23–24 | Cloud instance catalog | Node MachineSet tier 2 |

---

### SPARSE_DATA vs LOW_CONFIDENCE

These two notifications answer different questions and are orthogonal:

| | LOW_CONFIDENCE (code 1) | SPARSE_DATA (code 77) |
|---|---|---|
| Question | "How reliable is this recommendation *relative to the term's window*?" | "Is there enough *absolute* data to trust any recommendation?" |
| Trigger | `confidence_level < 0.5` (data covers less than half the term window) | `data_days <= sparse_data_threshold` (default 2) |
| Example: fires | Medium term (7-day window), 2 days of data → confidence = 0.29 | Any term, 1 day of data |
| Example: does NOT fire | Short term (1-day window), 1 day of data → confidence = 1.0 | Any term, 3+ days of data |
| Severity | WARNING | INFO |

A recommendation can be:

- **Neither**: 10 days of data in a 15-day window (plenty of data, good coverage)
- **SPARSE_DATA only**: 1 day in a 1-day window (full coverage but objectively sparse)
- **LOW_CONFIDENCE only**: 3 days in a 15-day window (not sparse, but poor coverage)
- **Both**: 1 day in a 7-day window (sparse AND low coverage)

---

## Related documentation

- Native engine notification mapping: [`docs/archive/native-engine-notification-gap.md`](../archive/native-engine-notification-gap.md)
- UI integration (partial table): [`docs/ui-integration-guide.md`](../ui-integration-guide.md)
- Consolidated feature notes: [`docs/known-issues.md`](../known-issues.md) (notification sections)
- Migrations: `migrations/000027_*.sql` through `migrations/000098_*.sql`
