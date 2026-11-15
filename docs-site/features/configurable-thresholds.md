# Configurable Thresholds

> **Last verified:** 2026-11-15

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
| `/settings/hcp-correlation` | HCP correlator policy (whole-domain PUT; takes effect on next hourly run) |
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

### Invalid admin values fail at startup (#792)

An out-of-range or unparseable admin env value is **not** silently ignored: the
service refuses to start, with an error naming the variable, its value, and the
valid range (e.g. `ROS_TERMS_CONTAINER_LONG_WINDOW_DAYS=120` → "must be an
integer between 1 and 90"). The same applies to `ROS_TERMS_*` variables that
target a plugin, term, or field that does not exist — including term-less
plugins (hcp, quota, cluster-quota, snapshot) and typo'd names — so a
deployment either honours its admin configuration or does not start. A term
whose env value is rejected is never reported as `locked`: the lock flag and
the effective value come from the same resolution step, so `locked: true`
always describes a value that was applied.

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
| **Cache only (correlator poll)** | `hcp-correlation` | Settings cache invalidated; hourly correlator picks new values up within one cycle, no threshold recalc |
| **Reship** | `business-hours*` | Digest reship triggered; schedules applied on subsequent processing |

Disable background threshold recalc with `ROS_THRESHOLD_RECALCULATION_ENABLED=false`
(settings still save; recalc waits for the next operator upload).

Full route-by-route table:
[Settings PUT side effects](../architecture/configurability.md#settings-put-side-effects).

## RBAC

When RBAC is enabled, PUT and DELETE require wildcard
**`cost-management:settings:write`** permission (`settings.write` is derived from
the RBAC access response, not supplied as a writable identity-header attribute).
GET is available to authenticated users with recommendation read access.

A permission-denied request returns HTTP **403** with:

```json
{"status":"error","message":"User does not have permission to modify settings"}
```

It does **not** change stored overrides, prune business-hours digests, trigger
recalculation/reship, update reship-pending state or invalidate caches. This
applies to PUT and DELETE, including deprecated aliases and org, cluster and
namespace business-hours settings. Environment locks remain a separate reason
for denial, with their existing response fields.

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
