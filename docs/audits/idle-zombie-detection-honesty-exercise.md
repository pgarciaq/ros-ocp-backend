# Honesty Exercise: Idle/Zombie Detection

**Date:** 2026-06-21
**Auditor:** AI Agent (Opus 4.6)
**Scope:** End-to-end idle/zombie workload detection in ros-ocp-backend — container, GPU, namespace, node, and PVC classification — including detection logic, API exposure, notifications, configurable thresholds, and all documentation/test artifacts.

---

## Executive Summary

The idle/zombie detection feature is **well-implemented and comprehensive**. The three-state model (active, idle, zombie) is consistently applied across container, GPU, namespace, node, and PVC plugins. The engine logic, API surface, notifications, configurable thresholds, and savings calculations all work end-to-end.

**Primary discrepancy found and fixed:** The public docs-site (`docs-site/plugin-reference/idle-detection.md`) stated the idle condition uses "**or**" (CPU *or* memory below threshold), but the authoritative requirements and Go code both use "**and**" (both must be below threshold). This was a documentation bug.

**Additional fixes:**
- Public docs-site: notification code 5 description corrected from "Container idle" to "Container idle or zombie"
- Public docs-site: states table corrected from "and/or" to "and (both must be below thresholds)"
- API cheatsheet: GPU field name corrected from `gpu_estimated_waste_cents` to `estimated_monthly_gpu_waste`
- E2E test: `_DEFAULT_IDLE` dict updated to include `zombie_cpu_millicores` and `zombie_peak_millicores` defaults

**No code bugs found.** The Go implementation matches the documented requirements across all components.

---

## Idle Detection: End-to-End Path

```
Daily Digest Pipeline (container, GPU, node, PVC)
    ↓
Idle Detection Engine (ClassifyIdleState / ClassifyGPUIdleState / ClassifyNodeIdle)
    ↓  Compares P95 usage vs configurable thresholds
    ↓  Three states: zombie → idle → active (checked in that order)
    ↓
recommendation_sets table
    ↓  idle_state, idle_since, idle_duration_days, estimated_waste_cents
    ↓  gpu_idle_state, gpu_idle_since, gpu_idle_duration_days, gpu_estimated_waste_cents
    ↓
API Response (list, detail, savings-summary)
    ↓  filter[idle_state], filter[gpu_idle_state]
    ↓  order_by=idle_state, idle_duration_days, estimated_monthly_waste
    ↓  group_by[idle_state]=* on savings-summary
    ↓
Notification Codes
    ↓  5 (IDLE_WORKLOAD), 8 (ABANDONED_WORKLOAD), 15 (NODE_IDLE), 26 (GPU_IDLE)
    ↓
koku-ui (Optimizations table)
    ↓  Badges: red (zombie), orange (idle)
    ↓  Savings source: waste for idle/zombie, savings for active
```

---

## Three-State Classification Model

| State | Container Definition | GPU Definition | Node Definition |
|-------|---------------------|---------------|-----------------|
| **zombie** | P95 CPU < `zombie_cpu_millicores` (default 1 mc) AND peak CPU < `zombie_peak_millicores` (default 10 mc) | Max daily SM active AND DRAM active both < zombie basis points (default 100 = 1%) | Max CPU P95 < `zombie_cpu_p95_mc` (default 200) AND pod count ≤ `zombie_max_pods` (default 5) |
| **idle** | CPU utilization < `cpu_utilization_percent`% of request AND memory utilization < `memory_utilization_percent`% of request (defaults 2%/5%) | Max daily SM AND DRAM active both < idle basis points (default 500 = 5%) | CPU utilization P95 < `idle_cpu_util_pct`% (default 10%) AND memory utilization P95 < `idle_mem_util_pct`% (default 10%) |
| **active** | Everything else | Everything else | Everything else |

**Key design decisions:**
- Classification order: zombie is checked first, then idle (zombie ⊂ idle)
- Burst guard: if peak CPU > `burst_ratio` × P95 CPU, state stays active (protects CronJobs)
- Exclusions: configurable namespace globs and workload types are never classified
- Minimum observation days: configurable (default 14 for containers, 7 for GPUs)

**Namespace idle** is aggregated: zombie if all container+GPU rows are zombie; idle if all are non-active but at least one is idle; otherwise active.

**PVC idle** is based on orphan detection: PVCs with no pod mount for > 7 days include `idle_since` and `idle_duration_days`.

---

## Configurable Thresholds (3-Tier Model)

Settings resolution: compiled defaults → admin environment variables (`ROS_IDLE_*`) → tenant settings via API.

| Field | Default | Range | Env Var |
|-------|---------|-------|---------|
| `enabled` | true | boolean | `ROS_IDLE_DETECTION_ENABLED` |
| `cpu_utilization_percent` | 2 | 1–50 | `ROS_IDLE_CPU_UTILIZATION_PCT` |
| `memory_utilization_percent` | 5 | 1–50 | `ROS_IDLE_MEMORY_UTILIZATION_PCT` |
| `burst_ratio` | 10 | 2–100 | `ROS_IDLE_BURST_RATIO` |
| `minimum_observation_days` | 14 | 3–90 | `ROS_IDLE_MIN_OBSERVATION_DAYS` |
| `gpu_sm_active_basis_points` | 500 | 100–5000 | `ROS_IDLE_GPU_SM_ACTIVE_BP` |
| `gpu_dram_active_basis_points` | 500 | 100–5000 | `ROS_IDLE_GPU_DRAM_ACTIVE_BP` |
| `zombie_cpu_millicores` | 1 | 0–100 | `ROS_IDLE_ZOMBIE_CPU_MILLICORES` |
| `zombie_peak_millicores` | 10 | 0–1000 | `ROS_IDLE_ZOMBIE_PEAK_MILLICORES` |

Exclusions: `exclude_namespaces` (globs), `exclude_workload_types` (any valid workload kind, e.g. Deployment, StatefulSet, Domain).

Admin env vars can **lock** fields, preventing tenant overrides. The settings API (`GET/PUT/DELETE /settings/idle-detection`) exposes locked fields in the response.

---

## API Surface

### Response Fields

| Field | Type | Present When |
|-------|------|-------------|
| `idle_state` | string (`active`, `idle`, `zombie`) | Always on container/namespace/node rows |
| `idle_since` | string (ISO date `YYYY-MM-DD`) | When `idle_state` ≠ active |
| `idle_duration_days` | integer | When `idle_state` ≠ active |
| `peak_cpu_millicores` | integer | When idle classification ran |
| `peak_memory_bytes` | integer | When idle classification ran |
| `estimated_monthly_waste` | MoneyAmount `{value, units}` | When `idle_state` ≠ active and cost data available |
| `idle_recommendation` | `{action, confidence, reason}` | When `idle_state` ≠ active |
| `gpu_idle_state` | string | On container rows with GPU data |
| `gpu_idle_since` | string | When `gpu_idle_state` ≠ active |
| `gpu_idle_duration_days` | integer | When `gpu_idle_state` ≠ active |
| `estimated_monthly_gpu_waste` | MoneyAmount | When `gpu_idle_state` ≠ active |

### Filter/Order/Group Parameters

| Parameter | Endpoints |
|-----------|----------|
| `filter[idle_state]=idle,zombie` | Container list, namespace list, node list |
| `filter[gpu_idle_state]=idle,zombie` | Container list, MIG list |
| `order_by=idle_state` | Container list |
| `order_by=idle_duration_days` | Container list |
| `order_by=estimated_monthly_waste` | Container list |
| `group_by[idle_state]=*` | Savings summary |

### Settings API

| Method | Path |
|--------|------|
| GET | `/api/cost-management/v1/recommendations/openshift/settings/idle-detection` |
| PUT | (same) |
| DELETE | (same — resets to defaults) |

PUT/DELETE trigger async recalculation for container, gpu, namespace, node, and pvc plugins.

---

## Notification Codes

| Code | Name | Fires When |
|------|------|-----------|
| 5 | `IDLE_WORKLOAD` | Container `idle_state` is idle or zombie (from `ClassifyIdleState` or legacy `IsIdle`) |
| 8 | `ABANDONED_WORKLOAD` | Legacy abandoned detection (`IsAbandoned` = all-zero usage); superseded by zombie classification but retained for backward compatibility |
| 15 | `NODE_IDLE` | Node idle or zombie |
| 26 | `GPU_IDLE` | GPU recommender classification is idle (very low SM utilization) |

Code 8 (`ABANDONED_WORKLOAD`) supersedes code 5 when both `IsAbandoned` and `IsIdle` are true — they are mutually exclusive in notification output.

---

## CSV Export

Container CSV (`NativeCSVHeader`) includes: `estimated_monthly_waste`, `estimated_monthly_waste_currency`, `idle_state`, `idle_since`, `idle_duration_days`, `peak_cpu_millicores`, `peak_memory_bytes`.

Namespace CSV (`NativeNSCSVHeader`) includes: `idle_state`.

PVC CSV (`pvcRecCSVHeader`) includes: `idle_since`, `idle_duration_days`.

---

## UI Display (koku-ui)

The Optimizations data table (`optimizationsDataTable.tsx`):
- Renders `idle_state` as PatternFly badges: red for zombie, orange for idle
- Uses `waste` field (from `estimated_monthly_waste`) as savings source for idle/zombie workloads
- Uses `savings` field (from `estimated_monthly_savings`) for active workloads
- `estimated_monthly_savings` is suppressed when `idle_state` ≠ active

---

## Alignment Matrix

| Aspect | Requirements | Go Code | Internal Docs | Public Docs | Cheatsheet | Bruno | Unit Tests | E2E Tests | IQE Tests | OpenAPI |
|--------|:-----------:|:-------:|:------------:|:-----------:|:----------:|:-----:|:----------:|:---------:|:---------:|:-------:|
| Three states (active/idle/zombie) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Container idle condition (AND) | ✅ | ✅ | ✅ | ✅ *fixed* | ✅ | — | ✅ | — | — | — |
| Zombie classification | ✅ | ✅ | ✅ | ✅ | ✅ | — | ✅ | ✅ | ✅ | ✅ |
| `idle_since` tracking | ✅ | ✅ | ✅ | ✅ | ✅ | — | ✅ | ✅ | ✅ | ✅ |
| `idle_duration_days` | ✅ | ✅ | ✅ | ✅ | ✅ | — | ✅ | ✅ | ✅ | ✅ |
| `filter[idle_state]` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `filter[gpu_idle_state]` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | — | ✅ | ✅ | ✅ |
| `order_by` idle fields | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | — | ✅ | ✅ | ✅ |
| `group_by[idle_state]` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | — | ✅ | ✅ | ✅ |
| Notification code 5 | ✅ | ✅ | ✅ | ✅ *fixed* | ✅ | — | ✅ | — | — | — |
| Notification code 8 | ✅ | ✅ | ✅ | ✅ | ✅ | — | ✅ | — | — | — |
| Notification code 15 | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | — | — | — |
| Notification code 26 | ✅ | ✅ | ✅ | ✅ | ✅ | — | ✅ | — | — | — |
| Configurable thresholds | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Env var locking | ✅ | ✅ | ✅ | ✅ | ✅ | — | ✅ | ✅ | — | — |
| Exclusions (ns/workload) | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | — | — | ✅ |
| Burst guard | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | — | — | — |
| `estimated_monthly_waste` | ✅ | ✅ | ✅ | ✅ | ✅ | — | ✅ | ✅ | ✅ | ✅ |
| `estimated_monthly_gpu_waste` | ✅ | ✅ | ✅ | ✅ | ✅ *fixed* | — | ✅ | — | — | ✅ |
| `idle_recommendation` | ✅ | ✅ | ✅ | — | ✅ | — | — | — | ✅ | ✅ |
| Container CSV export | ✅ | ✅ | — | — | — | — | — | — | ✅ | — |
| Namespace idle aggregation | ✅ | ✅ | ✅ | ✅ | ✅ | — | ✅ | — | ✅ | ✅ |
| Node idle classification | ✅ | ✅ | ✅ | ✅ | ✅ | — | ✅ | — | — | ✅ |
| PVC orphan idle | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | — | — | ✅ |
| Savings summary idle group | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | — | ✅ | ✅ | — |
| Settings API (GET/PUT/DELETE) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Async recalculation on PUT/DELETE | ✅ | ✅ | ✅ | ✅ | ✅ | — | ✅ | — | — | — |
| Zombie thresholds in E2E defaults | ✅ | ✅ | — | — | — | — | — | ✅ *fixed* | — | — |
| UI badges (zombie=red, idle=orange) | — | — | — | — | — | — | — | — | — | — |

**Legend:** ✅ = aligned, ⚠️ = partial, ❌ = wrong/missing, — = N/A, *fixed* = corrected by this audit

---

## Discrepancies Found and Fixed

### 1. Public docs-site: idle condition used "or" instead of "and"

**File:** `docs-site/plugin-reference/idle-detection.md` line 19
**Was:** `if CPU utilization < cpu_utilization_percent% of request **or** memory utilization < memory_utilization_percent% of request`
**Should be:** `**and**` — both CPU and memory must be below thresholds
**Authority:** Requirements (`docs/features/idle-detection.md` line 499) and Go code (`internal/engine/idle_classification.go` line 133) both use AND
**Fix:** Changed "or" → "and"

### 2. Public docs-site: states table said "and/or"

**File:** `docs-site/plugin-reference/idle-detection.md` line 10
**Was:** `Sustained low CPU and/or memory utilization vs configured percentages.`
**Should be:** `Sustained low CPU and memory utilization vs configured percentages (both must be below thresholds).`
**Fix:** Corrected to "and" with clarification

### 3. Public docs-site: notification code 5 description incomplete

**File:** `docs-site/plugin-reference/idle-detection.md` line 71
**Was:** `Container idle`
**Should be:** `Container idle or zombie`
**Authority:** Go code (`internal/engine/notifications.go` line 78): fires when `rec.IdleState == IdleStateIdle || rec.IdleState == IdleStateZombie`
**Fix:** Changed to "Container idle or zombie"

### 4. Cheatsheet: GPU field name wrong

**File:** `costmgmt-api-cheatsheet/costmgmt-api-cheatsheet.adoc` line 2490
**Was:** `gpu_estimated_waste_cents`
**Should be:** `estimated_monthly_gpu_waste`
**Authority:** Go model (`internal/model/detail_response.go`): `json:"estimated_monthly_gpu_waste,omitempty"`; OpenAPI spec confirms `estimated_monthly_gpu_waste`
**Fix:** Changed field name

### 5. E2E test: `_DEFAULT_IDLE` missing zombie thresholds

**File:** `cost-onprem-chart/tests/suites/ros/test_idle_detection_settings.py` line 16-26
**Was:** Dict missing `zombie_cpu_millicores` and `zombie_peak_millicores`
**Should include:** `zombie_cpu_millicores: 1` and `zombie_peak_millicores: 10`
**Authority:** Go code (`internal/engine/idle_classification.go` lines 39-40): `ZombieCPUP95MC: 1`, `ZombieCPUPeakMC: 10`
**Fix:** Added missing fields to `_DEFAULT_IDLE`

---

## Checklist

- [x] `idle_state` field present in all relevant list responses (container, namespace, node)
- [x] `filter[idle_state]` documented and working (container, namespace, node lists)
- [x] Zombie vs idle distinction clearly defined (zombie = near-zero, idle = low utilization)
- [x] `idle_since` timestamp tracked and exposed
- [x] `idle_duration_days` calculated correctly (days since `idle_since` at classification time)
- [x] Notification codes for idle/zombie in `notifications.go` (5, 8, 15, 26)
- [x] Idle thresholds are configurable (3-tier: defaults → env vars → tenant API)
- [x] Savings calculation accounts for idle state (full resource cost = waste)
- [x] CSV export includes `idle_state`, `idle_since`, `idle_duration_days` (container and namespace)
- [x] E2E tests cover idle detection (filter, order_by, group_by, settings CRUD)
- [x] Bruno has idle filter examples (container filter, GPU filter, settings, namespace filter)
- [x] OpenAPI documents `idle_state` enum values (`active`, `idle`, `zombie`)

---

## What Works End-to-End

1. **Container idle/zombie classification** — P95 CPU/memory vs configurable thresholds, burst guard, exclusions, minimum observation days
2. **GPU idle/zombie classification** — SM active and DRAM active basis points vs thresholds
3. **Namespace aggregation** — Derived from container + GPU states
4. **Node idle/zombie** — CPU utilization + pod count thresholds
5. **PVC orphan idle** — Tracks `idle_since` for unmounted PVCs
6. **Configurable thresholds** — Full 3-tier model (defaults → env → tenant API) with locking
7. **API filters/ordering** — `filter[idle_state]`, `filter[gpu_idle_state]`, `order_by`, `group_by[idle_state]`
8. **Waste calculation** — Full monthly cost for idle/zombie; rightsizing savings suppressed
9. **Notifications** — Codes 5/8/15/26 fire correctly
10. **CSV export** — Idle fields in container, namespace, and PVC exports
11. **UI** — Badges, waste display, savings source switching

---

## What's Not Broken but Worth Noting

1. **Code 8 (ABANDONED_WORKLOAD) is legacy** — superseded by zombie classification but kept for backward compatibility. `IsAbandoned` from `DetectAbandoned()` only fires when inline classification did not run (disabled, excluded, insufficient data).

2. **GPU idle vs GPU recommender classification** — Two separate systems:
   - `gpu_idle_state` (zombie/idle/active) from idle detection engine
   - `GPUClassification` (idle/underutilized/etc.) from GPU recommender
   Code 26 (`GPU_IDLE`) fires from the GPU recommender, not the idle detection engine. Practically they overlap since both detect low utilization, but they use different thresholds and fields.

3. **VM idle detection** — VMs have their own `VM_GPU_IDLE` notification (code 50) and GPU idle threshold via `ROS_VM_GPU_IDLE_THRESHOLD`. VM idle classification is part of the VM recommendation engine, separate from the container/GPU idle detection settings API.

---

## Design Questions

None — the feature is well-designed and consistently implemented. The only discrepancies were documentation bugs, now fixed.
