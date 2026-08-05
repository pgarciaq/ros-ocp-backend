# Configurable Thresholds

> **Last verified:** 2026-08-06

!!! info "Quick Facts"
    **API:** Per-plugin `GET/PUT/DELETE` under `/api/cost-management/v1/recommendations/openshift/settings/`  
    **Configurable:** Yes (meta-feature)  
    **Engines:** Applies to all engine types  
    **RBAC:** `cost-management:settings:write` required for PUT/DELETE

## Overview

Configurable thresholds let each tenant tune recommendation behavior — percentiles,
classification cutoffs, margins, idle detection, and plugin-specific knobs — without
redeploying ROS. Administrators can lock parameters platform-wide via environment
variables or freeze all tenant overrides with `ROS_SETTINGS_LOCKED`.

## Canonical settings routes

Use these dedicated paths (not query parameters):

| Route | Purpose |
|-------|---------|
| `/settings/container` | Container CPU/memory percentiles, margins, idle thresholds |
| `/settings/namespace` | Namespace-aggregated sizing (same field set as container) |
| `/settings/node` | Node utilization, consolidation, classification |
| `/settings/gpu` | GPU classification, MIG, time-slicing parameters |
| `/settings/pvc` | PVC oversized/near-full ratios, growth projection |
| `/settings/quota` | ResourceQuota headroom and risk bands |
| `/settings/cluster-quota` | ClusterResourceQuota headroom and risk bands |
| `/settings/snapshot` | Snapshot staleness and cost thresholds |
| `/settings/vm` | VM rightsizing thresholds, disk, I/O, instance-type matching |
| `/settings/idle-detection` | Idle/zombie classification (`{"idle_detection":{...}}`) |

**Deprecated alias:** `GET/PUT/DELETE /settings/thresholds?recommendation_type=<type>`
still works for `container`, `namespace`, `node`, `gpu`, and `pvc`. Responses include
`Deprecation: true` and a `Link` header pointing at the canonical `/settings/<type>` path.

Term windows, VM terms, and business-hours schedules use separate routes — see
[Configurability Reference](../architecture/configurability.md#settings-api-routes).

## Three-tier precedence

| Tier | Source | Behavior |
|------|--------|----------|
| **1 — Admin env var** | `ROS_CONTAINER_*`, `ROS_NODE_*`, etc. | **Locks** the field; tenant PUT returns `403` with `locked_fields` |
| **2 — Tenant Settings API** | Per-org DB overrides | Applied when no env lock exists |
| **3 — Compiled default** | Engine constants / `Default*()` functions | Fallback |

Resolution order: Tier 1 → Tier 2 → Tier 3. Env locks and `ROS_SETTINGS_LOCKED` are
documented in [Configurability Reference](../architecture/configurability.md).

## Typical workflow

**GET** returns merged effective values plus `locked_fields` (and `settings_locked`
when the platform lock is on).

**PUT** accepts partial JSON, validates ranges, saves tier-2 overrides, then applies
side effects per route (see [async behavior](#async-behavior-after-put) below).

**DELETE** removes tier-2 overrides; effective values revert to compiled defaults unless
env-locked. Returns `204` on some routes and `200` with the reset body on others — see
the reference doc.

When `ROS_SETTINGS_LOCKED=true` (or a per-plugin `ROS_SETTINGS_LOCKED_<TYPE>` opt-out
is inverted), PUT/DELETE return `403` with `settings are locked by platform administrator`.

## Async behavior after PUT

| Category | Routes | After successful PUT |
|----------|--------|----------------------|
| **Async recalc** | `container`, `namespace`, `node`, `gpu`, `pvc`, `quota`, `cluster-quota`, `snapshot` | Background re-recommendation for all clusters in the org (existing digest data; typically seconds) |
| **Idle detection** | `idle-detection` | Async recalc for **container** recommendations (idle runs inline on container/GPU ingest) |
| **Cache only** | `vm`, `vm/terms`, `terms` | Settings cache invalidated; new values apply on **next ingest** |
| **Reship** | `business-hours*` | Digest reship triggered; schedules applied on subsequent processing |

Disable background threshold recalc with `ROS_THRESHOLD_RECALCULATION_ENABLED=false`
(settings still save; recalc waits for the next operator upload).

Full route-by-route table:
[Settings PUT side effects](../architecture/configurability.md#settings-put-side-effects).

## RBAC

When RBAC is enabled, PUT and DELETE require **`cost-management:settings:write`**
(mapped to `settings.write` in the identity header). GET is available to authenticated
users with recommendation read access.

## Full reference

Parameter catalogs, env-var matrices, global lock, term windows, snapshot knobs, and
VM/business-hours settings:

**[Configurability Reference](../architecture/configurability.md)**

For how thresholds affect engine output, see
**[Recommendation Engines](../architecture/recommendation-engines.md)**.

## Related

- [Dual Engine](dual-engine.md) — Percentile differences between cost and performance
- [Container Right-Sizing](container-recommendations.md) — Primary consumer of container thresholds
- [UI Integration Guide — Settings](../ui-integration-guide.md)
