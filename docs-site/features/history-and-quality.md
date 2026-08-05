# Recommendation History & Quality

> **Last verified:** 2026-08-06

!!! info "Quick Facts"
    **History API:** `GET /api/cost-management/v1/recommendations/openshift/history`  
    **Quality API — Containers:** `GET /api/cost-management/v1/recommendations/openshift/quality/containers` (alias: `/quality`)  
    **Quality API — PVCs:** `GET /api/cost-management/v1/recommendations/openshift/quality/pvcs`  
    **Quality API — VMs:** `GET /api/cost-management/v1/recommendations/openshift/quality/vms`  
    **Quality API — GPU MIG:** `GET /api/cost-management/v1/recommendations/openshift/quality/gpu`  
    **Quality API — Snapshots:** `GET /api/cost-management/v1/recommendations/openshift/quality/snapshots`  
    **Export:** `?format=csv` on all endpoints  
    **Configurable:** Retention via `ROS_HISTORY_RETENTION_DAYS` (default 90)

Features for tracking recommendation changes over time and measuring recommendation effectiveness across containers, PVCs, VMs, GPU MIG, and snapshots.

## Overview

Recommendation **history** records how sizing values change over time.
Recommendation **quality** measures whether recommendations are stable, adopted,
and free of adverse outcomes. Together they help operators trust
ROS guidance and detect flip-flopping or ignored recommendations.

## History

Recommendation history is a **fleet-wide** API. There is no per-container ID sub-resource.

Each time recommendations are generated, prior values are archived to
`recommendation_history` with a `recorded_at` timestamp. History captures CPU
and memory request/limit values per container × term × engine.

### Use cases

- **Trend analysis** — See whether recommendations converge or oscillate
- **Audit** — Prove what ROS suggested before a capacity incident
- **Adoption tracking** — Compare history to current cluster config (see quality)

### API

```http
GET /api/cost-management/v1/recommendations/openshift/history
GET /api/cost-management/v1/recommendations/openshift/history?format=csv
```

Filter to a single container (or cluster, project, workload) with query parameters:

```
GET /api/cost-management/v1/recommendations/openshift/history?filter[container]=<name>&filter[cluster]=<cluster_alias>
```

| Filter | Parameter |
|--------|-----------|
| Cluster | `cluster` (UUID or alias) |
| Project | `project` |
| Workload | `workload` |
| Container | `container` |
| Term | `term` (`short`, `medium`, `long`) |
| Engine | `engine` (`cost`, `performance`) |
| Date range | `start_date`, `end_date` (YYYY-MM-DD; default: current month) |

Sort by `recorded_at`, `cluster`, `project`, `container`, `term`, or `engine`.
Pagination via `offset` and `limit` (offset-only by design — see [API Pagination](../pagination.md)).

Each row is one container + `term` + `engine` snapshot at `recorded_at`. Data is retained for `ROS_HISTORY_RETENTION_DAYS` (default 90).

**Not available for:** node, PVC, VM, namespace, GPU, or quota plugins — container recommendations only.

### Example (abbreviated)

```json
{
  "meta": { "count": 150 },
  "data": [{
    "recorded_at": "2026-05-20T08:00:00Z",
    "cluster_alias": "prod-east",
    "project": "payments",
    "container": "api",
    "term": "medium",
    "engine": "cost",
    "cpu_request_mc": 500,
    "memory_request_kib": 524288,
    "cpu_limit_mc": 525,
    "memory_limit_kib": 550502
  }]
}
```

### Scope limits (by design)

These are intentional boundaries, not missing implementations:

| Area | Behavior |
|------|----------|
| **Fleet history API** | Container recommendations only — no node, PVC, GPU, or VM fleet `GET .../history` endpoints |
| **PVC history** | Usage time-series on PVC detail — not recommendation snapshot history |
| **Quota / cluster quota** | `history[]` is embedded in **detail** responses (`/quota/detail`, `/cluster-quota/detail`), not a separate fleet history API |

### Future work

| Item | Status |
|------|--------|
| **GPU time-slicing / MIG recommendation history** | Not implemented — GPU recommendations have list/detail APIs but no `recommendation_history` writer |
| **PVC recommendation snapshot history** | By design, not implemented — PVC detail exposes usage time-series only; see [PVC plugin](../plugin-reference/pvc.md#history) |

## Quality

Quality metrics measure stability, adoption, and outcome signals after recommendations are issued.

Quality is available for **containers**, **PVCs**, **VMs**, **GPU MIG**, and **snapshots**, each with entity-specific outcome signals. Each entity type has its own database table and API endpoint.

#### Common fields (all entity types)

| Field | Meaning | Scale |
|-------|---------|-------|
| `stability_pct` | How much the new recommendation changed vs the prior cycle | **0.0–1.0** (1.0 = unchanged) |
| `adoption_detected` | Current config matches the prior recommendation within 5% | boolean |
| `recommendation_age_hours` | Hours since the prior recommendation | integer |
| `measured_at` | UTC date bucket for the row | timestamp |

#### Container-specific fields

| Field | Meaning | Scale |
|-------|---------|-------|
| `oom_events_after_rec` | OOM events in the **current ingestion batch** (not cumulative; repeated non-zero values across batches indicate ongoing pressure) | integer |

#### PVC-specific fields

| Field | Meaning | Scale |
|-------|---------|-------|
| `pvc_name` | Name of the PersistentVolumeClaim | string |
| `days_above_threshold` | Days in the digest window where usage ratio exceeded 95% of capacity | integer |

#### VM-specific fields

| Field | Meaning | Scale |
|-------|---------|-------|
| `vm_name` | Name of the virtual machine | string |
| `saturation_days` | Days where CPU or memory utilization exceeded 95% of allocated | integer |

Default `filter[engine]` is **cost** when omitted.

### What "previous generation" means

**Previous generation** is the recommendation from the **last ingestion run**
before the current one — not a calendar "yesterday" label, but the prior
recommendation row that existed immediately before overwriting. In typical daily
deployments that is effectively **today's run vs. yesterday's run**.

For each entity type, the engine reads prior values before writing new rows:

- **Containers:** `ReadClusterOldRecommendations()` in [`internal/engine/quality.go`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/engine/quality.go) reads `recommendation_sets`
- **PVCs:** `ReadClusterOldPVCRecommendations()` in [`internal/engine/pvc_quality.go`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/engine/pvc_quality.go) reads `pvc_recommendation_sets`
- **VMs:** `ReadClusterOldVMRecommendations()` in [`internal/engine/vm_quality.go`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/engine/vm_quality.go) reads `vm_recommendations`
- **GPU MIG:** `ReadClusterOldGPURecommendations()` in [`internal/engine/gpu_quality.go`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/engine/gpu_quality.go) reads `gpu_mig_recommendation_sets`
- **Snapshots:** Adoption uses snapshot inventory disappearance (no prior recommendation comparison needed)

Stability and adoption metrics use this pre-overwrite comparison.

### Stability

Stability scores how much the recommendation changed compared to the previous generation.

**Containers:** weighted average of CPU and memory variation:

```
stability = max(0, 1.0 - |cpu_variation|/100 * 0.5 - |mem_variation|/100 * 0.5)
```

**PVCs:** single-dimension variation on recommended bytes:

```
stability = max(0, 1.0 - |bytes_variation|/100)
```

**VMs:** weighted average of vCPU and memory variation (same formula as containers).

**GPU MIG:** binary — **1.0** if the recommended MIG profile is unchanged, **0.0** if it changed.

**Snapshots:** N/A — snapshots have categorical recommendations (delete, reduce frequency) where continuous stability scoring does not apply. `stability_pct` is always null.

A score of **1.0** means no change since the last run; lower scores indicate
larger shifts. Use stability to detect **flip-flopping** recommendations that
may erode operator trust.

### Adoption

Adoption is detected when the workload's **current** configuration
matches the **previous generation** recommendation within **5% tolerance**.

- **Containers:** Both CPU and memory requests must be within 5% of the prior recommendation.
- **PVCs:** Current capacity must be within 5% of the prior recommended bytes.
- **VMs:** Both current vCPU and memory must be within 5% of the prior recommendation.
- **GPU MIG:** Current GPU profile matches the prior recommended MIG profile (exact match, not tolerance-based).
- **Snapshots:** Detected via disappearance — if a snapshot recommended for deletion no longer appears in the inventory.

### Entity-specific outcome signals

**Containers — OOM events:**
Counts OOM kills occurring in the **current ingestion batch** after a
recommendation was issued. Rising OOM counts on performance-engine recommendations may
signal insufficient headroom or workload changes not yet reflected in digests.

**PVCs — Days above threshold:**
Counts days in the digest window where usage ratio exceeded 95% of capacity.
High values indicate the PVC is consistently near-full and the recommendation
may need urgent attention.

**VMs — Saturation days:**
Counts days where CPU or memory utilization exceeded 95% of allocated resources.
Persistent saturation suggests the VM is undersized relative to actual demand.

**GPU MIG — Contention days:**
Counts days where SM (streaming multiprocessor) active utilization exceeded the
MIG profile's compute capacity threshold. High contention indicates the workload
needs a larger MIG slice or dedicated GPU access.

**Snapshots — No bad outcome signal:**
Snapshots have categorical recommendations (delete orphan, reduce frequency) with
no continuous resource metric to monitor. There is no outcome signal beyond adoption.

Prometheus gauges (`ros_recommendation_stability`, `ros_recommendation_adoption_rate`, `ros_recommendation_oom_rate`) are updated after each ingestion quality write; gauge stability/adoption values use a **0–100** scale (API quality fields use **0.0–1.0**). See [Monitoring](../monitoring.md).

### Quality API

#### Container quality

```http
GET /api/cost-management/v1/recommendations/openshift/quality/containers
GET /api/cost-management/v1/recommendations/openshift/quality
GET /api/cost-management/v1/recommendations/openshift/quality/containers?format=csv
```

The `/quality` path is a backward-compatible alias for `/quality/containers`.

| Filter | Parameter |
|--------|-----------|
| Cluster | `cluster` |
| Project | `project` |
| Workload | `workload` |
| Container | `container` |
| Engine | `filter[engine]` or `engine` (`cost`, `performance`; defaults to `cost`) |
| Date range | `start_date`, `end_date` |

Sort by `measured_at`, `stability`, `adoption`, `oom_events`, or
`recommendation_age`.

#### PVC quality

```http
GET /api/cost-management/v1/recommendations/openshift/quality/pvcs
GET /api/cost-management/v1/recommendations/openshift/quality/pvcs?format=csv
```

| Filter | Parameter |
|--------|-----------|
| Cluster | `cluster` |
| Project | `project` |
| PVC name | `pvc_name` |
| Engine | `filter[engine]` or `engine` (`cost`, `performance`; defaults to `cost`) |
| Date range | `start_date`, `end_date` |

Sort by `measured_at`, `stability`, `adoption`, `days_above_threshold`, or
`recommendation_age`.

#### VM quality

```http
GET /api/cost-management/v1/recommendations/openshift/quality/vms
GET /api/cost-management/v1/recommendations/openshift/quality/vms?format=csv
```

| Filter | Parameter |
|--------|-----------|
| Cluster | `cluster` |
| Project | `project` |
| VM name | `vm_name` |
| Engine | `filter[engine]` or `engine` (`cost`, `performance`; defaults to `cost`) |
| Date range | `start_date`, `end_date` |

Sort by `measured_at`, `stability`, `adoption`, `saturation_days`, or
`recommendation_age`.

#### GPU MIG quality

```http
GET /api/cost-management/v1/recommendations/openshift/quality/gpu
GET /api/cost-management/v1/recommendations/openshift/quality/gpu?format=csv
```

| Filter | Parameter |
|--------|-----------|
| Cluster | `cluster` |
| Project | `project` |
| Workload | `workload` |
| Container | `container` |
| Engine | `filter[engine]` or `engine` (`cost`, `performance`; defaults to `cost`) |
| Date range | `start_date`, `end_date` |

Sort by `measured_at`, `stability`, `adoption`, `contention_days`, or
`recommendation_age`.

GPU MIG quality uses **binary stability** (1.0 if the recommended MIG profile is
unchanged from the prior cycle, 0.0 otherwise). Adoption is detected when the
workload's current GPU profile matches the prior recommended profile. The
entity-specific outcome signal is **contention days** — days where SM active
utilization exceeded the profile's compute capacity threshold.

**GPU MIG-specific fields:**

| Field | Meaning | Scale |
|-------|---------|-------|
| `stability_pct` | Binary: 1.0 if MIG profile unchanged, 0.0 if changed | 0.0 or 1.0 |
| `adoption_detected` | Current GPU profile matches prior recommendation | boolean |
| `contention_days` | Days where SM active exceeded profile compute capacity | integer |

#### Snapshot quality

```http
GET /api/cost-management/v1/recommendations/openshift/quality/snapshots
GET /api/cost-management/v1/recommendations/openshift/quality/snapshots?format=csv
```

| Filter | Parameter |
|--------|-----------|
| Cluster | `cluster` |
| Project | `project` |
| Snapshot name | `snapshot_name` |
| Date range | `start_date`, `end_date` |

Sort by `measured_at`, `adoption`, or `recommendation_age`.

Snapshot quality measures **adoption only** — there is no stability score (snapshots
have categorical recommendations like "delete orphan" or "reduce frequency", not
continuous sizing values) and no bad-outcome signal.

Adoption is detected via **disappearance**: if a snapshot that was previously
recommended for deletion is no longer present in the inventory, it is considered
adopted.

**Snapshot-specific fields:**

| Field | Meaning | Scale |
|-------|---------|-------|
| `stability_pct` | N/A — always null for snapshots | null |
| `adoption_detected` | Snapshot disappeared after deletion recommendation | boolean |
| `recommendation_age_hours` | Hours since the recommendation was issued | integer |

### Examples (abbreviated)

**Container quality:**

```json
{
  "data": [{
    "measured_at": "2026-05-20T08:00:00Z",
    "container": "api",
    "project": "payments",
    "engine": "cost",
    "stability_pct": 0.95,
    "adoption_detected": true,
    "oom_events_after_rec": 0,
    "recommendation_age_hours": 168
  }]
}
```

**PVC quality:**

```json
{
  "data": [{
    "measured_at": "2026-05-20T08:00:00Z",
    "pvc_name": "data-postgres-0",
    "namespace": "database",
    "engine": "cost",
    "stability_pct": 1.0,
    "adoption_detected": false,
    "days_above_threshold": 3,
    "recommendation_age_hours": 720
  }]
}
```

**VM quality:**

```json
{
  "data": [{
    "measured_at": "2026-05-20T08:00:00Z",
    "vm_name": "worker-vm-01",
    "namespace": "virt-workloads",
    "engine": "cost",
    "stability_pct": 0.88,
    "adoption_detected": true,
    "saturation_days": 5,
    "recommendation_age_hours": 336
  }]
}
```

**GPU MIG quality:**

```json
{
  "data": [{
    "measured_at": "2026-05-20T08:00:00Z",
    "container": "inference-worker",
    "project": "ml-serving",
    "engine": "cost",
    "stability_pct": 1.0,
    "adoption_detected": false,
    "contention_days": 2,
    "recommendation_age_hours": 504
  }]
}
```

**Snapshot quality:**

```json
{
  "data": [{
    "measured_at": "2026-05-20T08:00:00Z",
    "snapshot_name": "nightly-backup-2026-04-01",
    "namespace": "database",
    "stability_pct": null,
    "adoption_detected": true,
    "recommendation_age_hours": 168
  }]
}
```

### Future work

| Item | Status |
|------|--------|
| **`data_coverage_pct`** — share of expected digest days in the analysis window | Not implemented |
| **Stale recommendation archive on cleanup** — copy rows to `recommendation_history` before deleting stale `recommendation_sets` (today `ROS_STALE_CLEANUP_DAYS` deletes stale rows without archiving) | Not implemented |
| **Node quality** — requires operator changes to report node eviction events | Not implemented |
| **GPU timeslicing quality** — requires operator to report current time-slicing configuration for adoption detection | Not implemented |

Internal design detail: [quality-metrics design](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/design/quality-metrics.md).

## Retention and cleanup

| Variable | Default | Purpose |
|----------|---------|---------|
| `ROS_HISTORY_RETENTION_DAYS` | 90 | Drop `recommendation_history`, `recommendation_quality`, `pvc_recommendation_quality`, `vm_recommendation_quality`, `gpu_mig_recommendation_quality`, and `snapshot_recommendation_quality` partitions older than this |
| `ROS_STALE_CLEANUP_DAYS` | 30 | Delete `recommendation_sets` rows marked `stale = true` older than this (not archived first) |
| `ROS_STALENESS_THRESHOLD_HOURS` | 48 | Hours without cluster report before marking recommendations stale |

The ros-processor retention ticker runs every 24 hours. See [Configuration — Retention](../configuration.md#retention-and-data-lifecycle).

### Confidence on recommendations and history

`confidence_level` is a **float from 0.0 to 1.0** (1.0 = highest confidence) on live container recommendations and on container history rows. It is **not** exposed on `/quality` list rows.

When `confidence_level` is below `low_confidence_threshold` (configurable via container settings; default **0.5**) and digest data exists (`data_days > 0`), notification code **1** (`NotifLowConfidence`) is emitted. See [Notification codes — Containers](../architecture/notification-codes.md#containers-and-idle-detection).

### Pipeline resilience

History and quality writes are **non-fatal**: if the analytics pipeline fails (database error, timeout), recommendations are still persisted successfully.

- **Containers:** Failed `WriteRecommendationHistory` or `WriteRecommendationQuality` calls log an error, set `pipelineDegraded`, and processing continues — recommendations are not blocked by analytics failures.
- **PVCs:** Failed `WritePVCQuality` calls log a warning and processing continues — PVC recommendations are not blocked by quality write failures.
- **VMs:** Failed `WriteVMQuality` calls log a warning and processing continues — VM recommendations are not blocked by quality write failures.
- **GPU MIG:** Failed `WriteGPUQuality` calls log a warning and processing continues — GPU recommendations are not blocked by quality write failures.
- **Snapshots:** Failed `WriteSnapshotQuality` calls log a warning and processing continues — snapshot recommendations are not blocked by quality write failures.
- **Namespaces:** Transient database errors return a Kafka retry so history is written on redelivery; permanent errors skip history but keep recommendations available.

This design ensures recommendations are never blocked by analytics failures.

## Related

- [Container recommendations](../features/container-recommendations.md) — Source recommendations
- [PVC right-sizing](../features/pvc-rightsizing.md) — PVC recommendations
- [Virtual Machine recommendations](../features/virtual-machines.md) — VM recommendations
- [Configurability](../architecture/configurability.md) — Tuning that affects stability
- [UI Integration Guide — History](../ui-integration-guide.md#13-recommendation-history) and [Quality](../ui-integration-guide.md#14-recommendation-quality)
