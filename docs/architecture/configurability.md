# Configurability Reference

Complete environment variable reference for ROS-OCP Backend recommendation engines,
classification thresholds, retention, and platform settings.

For decay weighting — formula, visual examples, edge-weight table, and tuning guidance —
see [Decay Weights](decay-weights.md). For adaptive margin and trend detection, see
[Recommendation Math](recommendation-math.md). For how thresholds affect each plugin's
output, see [Recommendation Engines](recommendation-engines.md).

---

## Configuration Precedence

ROS-OCP uses a **three-tier precedence model** for every configurable parameter:

| Tier | Source | Scope | Behavior |
|------|--------|-------|----------|
| **1 — Admin env var** | `ROS_*` environment variable | Platform-wide | **Locks** the field for all tenants. Threshold/quota/idle/snapshot/VM settings PUTs return `403 Forbidden` with `locked_fields`; term PUTs return `422 Unprocessable Entity` with `locked_terms`. |
| **2 — Tenant Settings API** | Per-`org_id` database record | Single tenant | Applied when no admin env var is set. Stored in PostgreSQL (`org_recommendation_terms`, snapshot settings, business-hours schedules, etc.). |
| **3 — Compiled default** | Hardcoded in plugin or engine | Fallback | Used when neither tier 1 nor tier 2 provides a value. Defined in `DefaultTerms()`, `DefaultCPUConfig()`, plugin constants, etc. |

**Resolution order:** Tier 1 → Tier 2 → Tier 3. Admin env vars always win on both
**read** and **write**.

On **read**, the engine starts from compiled defaults (already patched from env at
process init where applicable), overlays per-tenant values from PostgreSQL, then
**re-applies any set admin env vars** so a stale tenant override cannot mask a
platform lock. Container, namespace, node, GPU, and PVC threshold plugins use
`resolveSizingThresholds()`; VM, quota, cluster-quota, and idle-detection settings
use the same pattern in their respective resolve functions.

On **write**, locked threshold fields are rejected with `403 Forbidden` when the
corresponding `ROS_*` env var is set.

Term-specific env vars (`ROS_TERMS_<PLUGIN>_<TERM>_<FIELD>`) lock individual term
fields. Threshold env vars (`ROS_CONTAINER_*`, `ROS_GPU_*`, etc.) lock the
corresponding sizing or classification parameter platform-wide.

### Current Implementation

Settings resolution is implemented **per plugin** today. Each settings type has its
own resolve function and matching `applyXxxEnvLocks` helper:

| Settings type | Resolve function | Env-lock helper |
|---------------|------------------|-----------------|
| Container / namespace sizing | `resolveSizingThresholds()` | `applyContainerEnvLocks`, `applyNamespaceEnvLocks` |
| Node / GPU / PVC thresholds | `ResolveNodeThresholdSettings`, etc. | `applyNodeEnvLocks`, `applyGPUEnvLocks`, `applyPVCEnvLocks` |
| VM rightsizing | `ResolveVMSettings` | `applyVMEnvLocks` |
| ResourceQuota | `ResolveQuotaSettings` | `applyQuotaEnvLocks` |
| ClusterResourceQuota | `ResolveClusterQuotaSettings` | `applyClusterQuotaEnvLocks` |
| Idle detection | `ResolveIdleDetectionSettings` | `applyIdleEnvLocks` |

Each path follows the same three steps manually: start from compiled defaults,
overlay tenant values from PostgreSQL, then re-apply any set admin env vars on read.
This pattern was kept for the env-lock-on-read fix (Option A): a small, targeted
change per plugin rather than refactoring every resolver at once. See
[Future Enhancement: Generic Settings Resolver](#future-enhancement-generic-settings-resolver)
for the planned centralized alternative (Option B).

Implementation references:
[`threshold_settings.go`](../../internal/engine/threshold_settings.go),
[`vm_settings.go`](../../internal/engine/vm_settings.go),
[`quota_settings.go`](../../internal/engine/quota_settings.go),
[`cluster_quota_settings.go`](../../internal/engine/cluster_quota_settings.go),
[`idle_settings.go`](../../internal/engine/idle_settings.go).

### In-process settings cache (60 seconds)

All settings resolution paths share a single in-process cache keyed by
`(org_id, recommendation_type)` with a **60-second TTL**. Repeated resolves within
the same minute reuse the merged result without querying PostgreSQL. Cache entries
are invalidated immediately on successful Settings API PUT or DELETE for that org
and type (via `InvalidateThresholdCache`).

Cached recommendation types: `container`, `namespace`, `node`, `gpu`, `pvc`, `vm`,
`quota`, `cluster-quota`, and `idle_detection`. The Prometheus gauge
`ros_threshold_cache_entries` reports the number of entries currently held.

---

## Settings API Routes

Base path: `/api/cost-management/v1/recommendations/openshift/settings/`

| Route | Methods | Status | Purpose |
|-------|---------|--------|---------|
| `/settings/terms?recommendation_type=<plugin>` | GET, PUT, DELETE | **Existing** | Per-tenant term windows (short / medium / long). Valid plugins: `container`, `namespace`, `node`, `gpu`, `pvc`. |
| `/settings/snapshot` | GET, PUT, DELETE | **Existing** | Snapshot staleness thresholds (orphan age, never-restored days, stale days, redundant count, cost per GiB/month). |
| `/settings/vm` | GET, PUT, DELETE | **Existing** | VM rightsizing thresholds, memory floors, disk, I/O, instance-type matching (`vm` plugin). |
| `/settings/vm/terms` | GET, PUT, DELETE | **Existing** | VM recommendation term windows (`vm` plugin). |
| `/settings/business-hours` | GET, PUT, DELETE | **Existing** | Org-default business-hours schedule. |
| `/settings/business-hours/clusters/:cluster_id` | GET, PUT, DELETE | **Existing** | Cluster-level schedule override. |
| `/settings/business-hours/clusters/:cluster_id/namespaces/:namespace` | GET, PUT, DELETE | **Existing** | Namespace-level schedule override. |
| `/settings/container`, `/settings/namespace`, `/settings/node`, `/settings/gpu`, `/settings/pvc` | GET, PUT, DELETE | **Existing** | Per-tenant sizing and classification thresholds for each plugin (canonical paths). |
| `/settings/thresholds?recommendation_type=<plugin>` | GET, PUT, DELETE | **Deprecated** | Alias for the five paths above; responses include `Deprecation: true` and a `Link` successor header. |
| `/settings/quota` | GET, PUT, DELETE | **Existing** | ResourceQuota headroom and utilization risk thresholds (`quota` plugin). |
| `/settings/cluster-quota` | GET, PUT, DELETE | **Existing** | ClusterResourceQuota headroom and risk thresholds (`cluster-quota` plugin). |
| `/settings/idle-detection` | GET, PUT, DELETE | **Existing** | Idle/zombie classification thresholds. |
| `/settings/capabilities` | GET | **Existing** | Read-only feature discovery: enabled plugins, term support, business-hours gate (see below). |

### Reference table columns

| Column | Meaning |
|--------|---------|
| **Setting** | Short label plus operator-focused prose (`<br><em>…</em>`): what it controls, default rationale, tuning trade-offs, and interactions |
| **Default** | Compiled default (tier 3) from [`config.go`](../../internal/config/config.go) or plugin defaults |
| **Env var** | Tier 1; when set in the environment, locks the field platform-wide (`403` on threshold PUT with `locked_fields`; `422` on term PUT with `locked_terms`; `locked_fields` on GET) |
| **API endpoint** | Settings API path (`GET`/`PUT`/`DELETE`); `—` if not tenant-configurable |
| **JSON field** | PUT/GET field (dot notation for nested objects) |
| **Lockable** | `Yes` = tenant PUT when env unset and not globally locked; `No` = admin only; `Global` = `ROS_SETTINGS_LOCKED` blocks PUT (unless feature opt-out) |

**Term env vars:** `ROS_TERMS_<PLUGIN>_<TERM>_<FIELD>` — e.g. `ROS_TERMS_CONTAINER_MEDIUM_WINDOW_DAYS`, `ROS_TERMS_VM_SHORT_TERM_WINDOW_DAYS`.

When a parameter is locked by an admin env var, the Settings API marks it `"locked": true`
in GET responses and rejects PUT attempts for that field.

**Threshold recalculation:** After a successful PUT, ROS re-runs the native recommendation
engine asynchronously for every cluster in the tenant, using existing digest data in
PostgreSQL (no masu reship or Kafka). The PUT returns `200 OK` immediately; updated
recommendations typically appear within seconds. Disable with
`ROS_THRESHOLD_RECALCULATION_ENABLED=false` if needed. See
[`threshold_recalculate.go`](../../internal/engine/threshold_recalculate.go).

Implementation references: [`handlers_terms.go`](../../internal/api/handlers_terms.go),
[`handlers_snapshot_settings.go`](../../internal/api/handlers_snapshot_settings.go),
[`handlers_vm_settings.go`](../../internal/api/handlers_vm_settings.go),
[`handlers_business_hours_settings.go`](../../internal/api/handlers_business_hours_settings.go),
[`handlers_threshold_settings.go`](../../internal/api/handlers_threshold_settings.go),
[`handlers_capabilities.go`](../../internal/api/handlers_capabilities.go),
[`term_config.go`](../../internal/engine/term_config.go),
[`threshold_settings.go`](../../internal/engine/threshold_settings.go).

## Capabilities endpoint

`GET /api/cost-management/v1/recommendations/openshift/settings/capabilities` is **read-only**
(no PUT or DELETE). It answers which recommendation domains exist for the deployment and
whether each is currently enabled, so clients do not hard-code plugin lists.

### Response shape

```json
{
  "recommendation_types": [
    {
      "name": "container",
      "enabled": true,
      "supports_terms": true
    },
    {
      "name": "snapshot",
      "enabled": true,
      "supports_terms": false
    }
  ],
  "business_hours": true
}
```

| Field | Meaning |
|-------|---------|
| `recommendation_types[]` | One entry per registered production plugin (except disabled legacy `kruize`). |
| `recommendation_types[].name` | Plugin id used in URLs and `recommendation_type` query params (e.g. `container`, `quota`, `pvc`). |
| `recommendation_types[].enabled` | Whether `ROS_ENABLED_PLUGINS` / `ROS_DISABLED_PLUGINS` currently expose routes for that plugin. |
| `recommendation_types[].supports_terms` | Whether `GET/PUT/DELETE /settings/terms?recommendation_type=<name>` is valid for that plugin. |
| `business_hours` | Whether business-hours settings routes and dual-stream digests are active (`ROS_BUSINESS_HOURS_ENABLED`). |

### Client usage

- **koku-ui / Optimizations:** Call once per session (or on app init) to build navigation and
  settings tabs—hide GPU, snapshot, quota, or business-hours panels when `enabled` is `false`.
- **Term settings UI:** Only show term editors when `supports_terms` is `true` for that plugin;
  use the same `name` values as `recommendation_type` on `/settings/terms`.
- **Business hours:** Gate schedule editors on `business_hours: true`; when `false`, BH routes
  return `404` and the field is omitted or `false` in capabilities.

Implementation: [`GetCapabilities`](../../internal/api/handlers_capabilities.go) iterates
[`plugin.All()`](../../internal/plugin/registry.go) and checks the [`TermProvider`](../../internal/plugin/plugin.go)
trait for `supports_terms`.

### Settings PUT side effects

What happens after a successful tenant **PUT** (and after **DELETE** where noted).
The HTTP response returns immediately; long-running work runs in the background unless
`ROS_THRESHOLD_RECALCULATION_ENABLED=false` (threshold recalc only).

| Route | On successful PUT |
|-------|-------------------|
| `/settings/container` | Async recalc (container) |
| `/settings/namespace` | Async recalc (namespace) |
| `/settings/node` | Async recalc (node) |
| `/settings/gpu` | Async recalc (gpu) |
| `/settings/pvc` | Async recalc (pvc) |
| `/settings/quota` | Async recalc (quota) |
| `/settings/cluster-quota` | Async recalc (cluster-quota) |
| `/settings/snapshot` | Async recalc (snapshot) |
| `/settings/idle-detection` | Async recalc (container, gpu, namespace, node, pvc) |
| `/settings/vm` | Cache invalidation; next ingest applies |
| `/settings/vm/terms` | Cache invalidation; next ingest applies |
| `/settings/terms` | Cache invalidation; next ingest applies |
| `/settings/business-hours*` | Digest reship + schedule apply |

**DELETE** on threshold-style routes also invalidates the in-process settings cache;
idle-detection DELETE additionally triggers async recalc for container, gpu, namespace,
node, and pvc. Snapshot DELETE
returns `204` without recalc (effective values revert on the next classification run).

---

## Global Settings Lock

When `ROS_SETTINGS_LOCKED=true`, ROS behaves as if **every** tenant Settings API override were cleared:
compiled defaults are enforced on read and resolve paths, and PUT/DELETE on settings routes return
`403 Forbidden` with `{"error":"settings are locked by platform administrator","locked":true}`.

This reuses the existing `locked_fields` mechanism on GET responses (`locked_fields: ["*"]`) plus
`settings_locked: true` on API envelopes. Individual admin env vars (`ROS_CONTAINER_*`, `ROS_VM_*`, etc.)
still override compiled defaults on read/resolve even under the global lock.

### Environment variables

| Setting | Default | Env var | API endpoint | JSON field | Lockable |
|---------|---------|---------|--------------|------------|----------|
| Global settings lock (master) <br><em>When `true`, every tenant Settings API override is ignored on read and PUT/DELETE return `403`. Compiled defaults (plus any per-field `ROS_*` admin locks) are enforced platform-wide. Use during upgrades, compliance freezes, or when tenants must not change recommendation math.</em> | `false` | `ROS_SETTINGS_LOCKED` | All `/settings/*` routes | — | No |
| Lock container thresholds under global lock <br><em>Default `true`: with global lock on, container sizing/classification via `/settings/container` is frozen. Set `false` to opt out—tenants may PUT container thresholds while other plugins stay locked.</em> | `true` | `ROS_SETTINGS_LOCKED_CONTAINER` | `/settings/container` | — | No |
| Lock GPU thresholds under global lock <br><em>Freezes GPU threshold PUTs when global lock is on. Pair with `ROS_GPU_*` env vars for hard platform caps.</em> | `true` | `ROS_SETTINGS_LOCKED_GPU` | `/settings/gpu` | — | No |
| Lock node thresholds under global lock <br><em>Freezes node utilization and consolidation targets under global lock.</em> | `true` | `ROS_SETTINGS_LOCKED_NODE` | `/settings/node` | — | No |
| Lock namespace thresholds under global lock <br><em>Freezes namespace-aggregated sizing thresholds under global lock.</em> | `true` | `ROS_SETTINGS_LOCKED_NAMESPACE` | `/settings/namespace` | — | No |
| Lock PVC thresholds under global lock <br><em>Freezes storage oversized/near-full parameters under global lock.</em> | `true` | `ROS_SETTINGS_LOCKED_PVC` | `/settings/pvc` | — | No |
| Lock VM settings and VM terms under global lock <br><em>Freezes both `/settings/vm` and `/settings/vm/terms`.</em> | `true` | `ROS_SETTINGS_LOCKED_VM` | `/settings/vm`, `/settings/vm/terms` | — | No |
| Lock ResourceQuota settings under global lock <br><em>Freezes `/settings/quota` headroom and risk bands.</em> | `true` | `ROS_SETTINGS_LOCKED_QUOTA` | `/settings/quota` | — | No |
| Lock ClusterResourceQuota settings under global lock <br><em>Freezes `/settings/cluster-quota`.</em> | `true` | `ROS_SETTINGS_LOCKED_CLUSTER_QUOTA` | `/settings/cluster-quota` | — | No |
| Lock idle-detection settings under global lock <br><em>Freezes idle/zombie thresholds and exclusions.</em> | `true` | `ROS_SETTINGS_LOCKED_IDLE` | `/settings/idle-detection` | — | No |
| Lock snapshot staleness settings under global lock <br><em>Freezes `/settings/snapshot` tenant overrides.</em> | `true` | `ROS_SETTINGS_LOCKED_SNAPSHOT` | `/settings/snapshot` | — | No |
| Lock business-hours schedules under global lock <br><em>PUT/DELETE return `403`; schedules not applied on ingest; GET returns `enabled: false`.</em> | `true` | `ROS_SETTINGS_LOCKED_BUSINESS_HOURS` | `/settings/business-hours*` | — | No |
| Lock generic term windows under global lock <br><em>Freezes `/settings/terms` for container, namespace, node, gpu, pvc—not `/settings/vm/terms`.</em> | `true` | `ROS_SETTINGS_LOCKED_TERMS` | `/settings/terms?recommendation_type=*` | — | No |

Per-feature opt-outs apply **only** when `ROS_SETTINGS_LOCKED=true`. Example: with global lock on and
`ROS_SETTINGS_LOCKED_VM=false`, tenants may PUT/DELETE VM settings while container thresholds remain frozen.

### Startup logging

On service start, when the global lock is enabled, ROS logs a warning listing any per-feature opt-outs.
See [`settings_locked_startup.go`](../../internal/engine/settings_locked_startup.go) and
[`IsSettingsLocked`](../../internal/engine/settings_locked.go).

Example log lines:

```
ROS_SETTINGS_LOCKED=true: all tenant settings overrides will be ignored; compiled defaults enforced
ROS_SETTINGS_LOCKED: per-feature opt-out (tenant API allowed): vm, terms
```

### DELETE — reset tenant overrides

When settings are **not** locked, `DELETE` on a settings route removes tier-2 PostgreSQL overrides for that org
and returns **`204 No Content`**. Effective values revert to compiled defaults (tier 3), still subject to
admin env-var locks on read. The in-process settings cache is invalidated for that org and type.

| Route | DELETE clears |
|-------|----------------|
| `/settings/snapshot` | Per-org snapshot staleness overrides |
| `/settings/vm` | VM threshold / disk / I/O / instance-type overrides |
| `/settings/vm/terms` | VM term-window overrides (separate from generic terms table) |
| `/settings/terms?recommendation_type=<plugin>` | Generic term rows for that plugin |
| `/settings/container`, `/settings/namespace`, `/settings/node`, `/settings/gpu`, `/settings/pvc` | Threshold JSON for that plugin |
| `/settings/thresholds?recommendation_type=<plugin>` | Same as dedicated path (deprecated alias) |
| `/settings/quota`, `/settings/cluster-quota`, `/settings/idle-detection` | Respective override rows |
| Business-hours routes | Schedule override at that scope |

Under global lock (or per-feature lock), DELETE returns **`403 Forbidden`** with
`{"error":"settings are locked by platform administrator","locked":true}`.

### Generic terms lock alignment

`/settings/terms?recommendation_type=<plugin>` evaluates **two** locks (either blocks PUT/DELETE and sets
`settings_locked: true` on GET):

1. **Type-specific** — `IsSettingsLocked(<plugin>)` (e.g. `vm` via `ROS_SETTINGS_LOCKED_VM`)
2. **Generic terms** — `IsSettingsLocked("terms")` via `ROS_SETTINGS_LOCKED_TERMS`

Example: with `ROS_SETTINGS_LOCKED=true`, `ROS_SETTINGS_LOCKED_TERMS=false`, and
`ROS_SETTINGS_LOCKED_VM=true`, container terms remain editable on the generic endpoint while
`recommendation_type=vm` is frozen. VM term windows on **`/settings/vm/terms`** consult only the **`vm`**
lock (not the generic `terms` lock).

Implementation: [`termsSettingsLocked()`](../../internal/api/handlers_terms.go).

### Business hours under global lock

When `ROS_SETTINGS_LOCKED_BUSINESS_HOURS` is true (default under global lock), all business-hours
PUT/DELETE routes return `403`, schedules are not applied during recommendation ingestion, and GET
returns `{"enabled": false, "settings_locked": true}`.

---

## General / Infrastructure

Process, database, Kafka, HTTP, plugins, and operational toggles. **No Settings API** — configure on API, processor, and poller Deployments.

| Setting | Default | Env var | API endpoint | JSON field | Lockable |
|---------|---------|---------|--------------|------------|----------|
| Service name (logs) <br><em>Identifies this process in log lines and metrics (`service_name`). Change only when multiple ROS deployments share one observability stack.</em> | `rosocp` | `SERVICE_NAME` | — | — | No |
| Log level <br><em>Minimum severity (`DEBUG`–`ERROR`). Use `DEBUG` briefly for Kafka/digest troubleshooting; `INFO` in production.</em> | `INFO` | `LOG_LEVEL` | — | — | No |
| Log format <br><em>`text` for local dev; `json` in Clowder/OpenShift for log aggregation field extraction.</em> | `text` (local) / `json` (Clowder) | `LogFormater` | — | — | No |
| API listen port <br><em>Echo HTTP bind port for REST and callbacks. Must match Service `targetPort`.</em> | `8000` | `API_PORT` | — | — | No |
| Prometheus metrics port <br><em>Separate `/metrics` listener (histograms, cache gauges). Local 5005; Clowder injects platform port.</em> | `5005` (local) / Clowder | `PROMETHEUS_PORT` | — | — | No |
| HTTP read-header timeout (s) <br><em>Max seconds to read request headers; protects against slow clients. Lower may break slow proxies.</em> | 15 | `READ_HEADER_TIMEOUT` | — | — | No |
| Outbound HTTP client timeout (s) <br><em>Default timeout for Masu, Sources, Kruize, TokenReview. Raise if effective-rates calls timeout on large tenants.</em> | 30 | `GLOBAL_HTTP_CLIENT_TIMEOUT_SECS` | — | — | No |
| Max values per repeated query param <br><em>Cap per filter key repetitions in list APIs; limits SQL `IN` size and abuse.</em> | 5 | `MAXIMUM_COUNT_PER_QUERY_PARAM` | — | — | No |
| CSV export row batch <br><em>DB fetch size per round-trip for streaming CSV exports. Higher = more memory.</em> | 1000 | `RECORD_LIMIT_CSV` | — | — | No |
| CSV stream flush interval (rows) <br><em>Flush CSV to client every N rows for UI progress; lower = more syscalls.</em> | 100 | `CSV_STREAM_INTERVAL` | — | — | No |
| PostgreSQL host <br><em>Database hostname for digests, recommendations, settings. Must resolve from API and processor pods.</em> | `localhost` | `DB_HOST` | — | — | No |
| PostgreSQL port <br><em>PostgreSQL TCP port (dev 15432; chart 5432).</em> | `15432` | `DB_PORT` | — | — | No |
| PostgreSQL database <br><em>Database name; org_id is a column, not separate DB.</em> | `postgres` | `DB_NAME` | — | — | No |
| PostgreSQL user <br><em>DB user credential (dev default `postgres`; use Secrets in prod).</em> | `postgres` | `DB_USER` | — | — | No |
| PostgreSQL password <br><em>DB password; rotate with rolling restart of API + processor.</em> | `postgres` | `DB_PASSWORD` | — | — | No |
| PostgreSQL SSL mode <br><em>`disable` local only; use `verify-full` + CA on OpenShift.</em> | `disable` | `DB_SSL` | — | — | No |
| PostgreSQL CA cert path <br><em>CA bundle path for TLS verify; empty uses system trust.</em> | (empty) | `DB_CA_CERT` | — | — | No |
| pgxpool max connections <br><em>Pool max; size for API concurrency + `ROS_KAFKA_WORKERS` + pollers. Too low → `pool_timeout`; too high → exhaust PG `max_connections`.</em> | 5 | `ROS_DB_MAX_CONNS` | — | — | No |
| pgxpool min connections <br><em>Warm idle connections; lowers cold-start latency, uses DB slots.</em> | 2 | `ROS_DB_MIN_CONNS` | — | — | No |
| Connection max lifetime (minutes) <br><em>Recycle connections for failover/PgBouncer; 30m typical.</em> | 30 | `ROS_DB_MAX_CONN_LIFETIME` | — | — | No |
| Connection max idle (minutes) <br><em>Close idle pool connections after N minutes; frees DB slots.</em> | 5 | `ROS_DB_MAX_CONN_IDLE_TIME` | — | — | No |
| Statement cache mode <br><em>pgx `describe` (default) or `prepare` (session pooling only).</em> | `describe` | `ROS_DB_STATEMENT_CACHE_MODE` | — | — | No |
| Pool acquire timeout (s); 0 = none <br><em>Max wait for pool conn; `0` waits forever. 5s → 503 under overload.</em> | 5 | `ROS_DB_ACQUIRE_TIMEOUT_SECS` | — | — | No |
| API statement timeout (ms) <br><em>Session default for API/GORM queries; per-endpoint overrides via `SetLocalStatementTimeout()`. SaaS ingress is ~30s.</em> | 30000 | `ROS_API_STATEMENT_TIMEOUT_MS` | — | — | No |
| Heavy API statement timeout (ms) <br><em>Extended `SET LOCAL` for savings-summary and fleet-wide container list. SaaS should use ~28000.</em> | 45000 | `ROS_HEAVY_API_STATEMENT_TIMEOUT_MS` | — | — | No |
| Kafka bootstrap servers <br><em>Broker list for upload + sources consumers; must match Strimzi DNS.</em> | `localhost:29092` | `KAFKA_BOOTSTRAP_SERVERS` | — | — | No |
| Kafka consumer group <br><em>Processor consumer group for partition balance; **new id reprocesses offsets**.</em> | `ros-ocp` | `KAFKA_CONSUMER_GROUP_ID` | — | — | No |
| Kafka auto-commit <br><em>`false` commits after successful processing (recommended). `true` risks loss on crash.</em> | false | `KAFKA_AUTO_COMMIT` | — | — | No |
| Upload topic <br><em>Incoming OCP usage topic (`hccm.ros.events` on-prem).</em> | `hccm.ros.events` | `UPLOAD_TOPIC` | — | — | No |
| Recommendation topic (Kruize) <br><em>Legacy Kruize topic; unused on native-only deployments.</em> | `rosocp.kruize.recommendations` | `RECOMMENDATION_TOPIC` | — | — | No |
| Sources event topic <br><em>Platform Sources lifecycle stream for provider sync.</em> | `platform.sources.event-stream` | `SOURCES_EVENT_TOPIC` | — | — | No |
| Parallel Kafka processing <br><em>`true` enables concurrent message handlers; `false` serializes for debugging.</em> | true | `ROS_KAFKA_PARALLEL` | — | — | No |
| Kafka worker goroutines <br><em>Concurrent ingest workers per pod; keep × replicas within DB pool budget.</em> | 3 | `ROS_KAFKA_WORKERS` | — | — | No |
| RBAC enabled <br><em>Enforce cost-management RBAC on APIs. Never `false` in production.</em> | false (local) / true (Clowder) | `RBAC_ENABLE` | — | — | No |
| RBAC cache TTL (s); 0 = off <br><em>RBAC decision cache TTL; `0` disables cache.</em> | 60 | `ROS_RBAC_CACHE_TTL` | — | — | No |
| Kruize URL <br><em>Legacy autotune/Kruize base URL when legacy path enabled.</em> | `http://localhost:8080` | `KRUIZE_URL` | — | — | No |
| Kruize wait time (s) <br><em>HTTP timeout for Kruize calls.</em> | 30 | `KRUIZE_WAIT_TIME` | — | — | No |
| Kruize bulk chunk size <br><em>Recommendations per Kruize bulk POST.</em> | 100 | `KRUIZE_MAX_BULK_CHUNK_SIZE` | — | — | No |
| Kruize performance profile version <br><em>Profile schema version (`v2.0`) must match autotune.</em> | `v2.0` | `KRUIZE_PERFORMANCE_PROFILE_VERSION` | — | — | No |
| Recommendation poller interval (h) <br><em>Background poller period for legacy paths.</em> | 24 | `RECOMMENDATION_POLL_INTERVAL_HOURS` | — | — | No |
| Legacy data retention period <br><em>Retention for legacy artifacts; not digest/history retention.</em> | 15 | `DATA_RETENTION_PERIOD` | — | — | No |
| Removed native-engine flag <br><em>Removed; native engine is unconditionally active.</em> | ~~true~~ | ~~`ROS_USE_NATIVE_ENGINE`~~ | — | — | No |
| Plugin allowlist (CSV); empty = all native <br><em>CSV of plugins to run; empty = all registered.</em> | (empty) | `ROS_ENABLED_PLUGINS` | — | — | No |
| Plugin denylist (CSV) <br><em>CSV of plugins to skip; overrides allowlist.</em> | (empty) | `ROS_DISABLED_PLUGINS` | — | — | No |
| Tag filtering enabled <br><em>Enable tag filters on list APIs; requires tag sync + Koku tags.</em> | true | `ROS_TAGS_ENABLED` | — | — | No |
| Tag source (`db` or `api`) <br><em>Chart default `api` (Koku push sync into `resolved_tags`); binary default `db` (direct Koku PostgreSQL reads on shared instance — advanced).</em> | `api` (chart) / `db` (binary) | `ROS_TAGS_SOURCE` | — | — | No |
| Tag sync allowed service accounts <br><em>CSV of SAs allowed to POST tag sync; required in api mode (non-dev).</em> | (empty) | `ROS_TAGS_ALLOWED_SERVICE_ACCOUNTS` | — | — | No |
| Tag sync dev bearer token <br><em>Dev-only static token; blocked outside DEVELOPMENT=true.</em> | (empty) | `ROS_TAGS_DEV_TOKEN` | — | — | No |
| Tag sync max body (MiB) <br><em>Max tag sync POST body; prevents OOM from huge payloads.</em> | 10 | `ROS_TAGS_SYNC_MAX_BODY_MIB` | — | — | No |
| CSV download max body (bytes) <br><em>Max external CSV download size (500 MiB default).</em> | 524288000 | `ROS_CSV_MAX_BODY_BYTES` | — | — | No |
| CSV download timeout (s) <br><em>Timeout for external CSV fetch.</em> | 120 | `ROS_CSV_DOWNLOAD_TIMEOUT_SECS` | — | — | No |
| CSV allowed hosts (CSV) <br><em>SSRF allowlist hostnames for CSV URLs.</em> | (empty) | `ROS_CSV_ALLOWED_HOSTS` | — | — | No |
| CSV deny private networks <br><em>Block RFC1918/link-local/loopback CSV targets.</em> | true | `ROS_CSV_DENY_PRIVATE_NETWORKS` | — | — | No |
| API max offset <br><em>Max pagination offset before HTTP 400.</em> | 10000 | `ROS_API_MAX_OFFSET` | — | — | No |
| Development mode <br><em>Relaxes security checks for local dev only.</em> | false | `DEVELOPMENT` | — | — | No |
| Log poison Kafka payload <br><em>Log truncated payload on permanent failure (debug).</em> | false | `ROS_LOG_POISON_PAYLOAD` | — | — | No |
| Housekeeper shutdown grace (s) <br><em>Grace period on SIGTERM during cleanup.</em> | 30 | `ROS_HOUSEKEEPER_SHUTDOWN_GRACE_SECS` | — | — | No |
| K8s SA token path (tag sync) <br><em>Projected SA token path for TokenReview auth.</em> | `/var/run/secrets/.../token` | `KUBERNETES_SA_TOKEN_PATH` | — | — | No |
| K8s TokenReview URL <br><em>Override TokenReview URL; default in-cluster.</em> | cluster default | `KUBERNETES_TOKEN_REVIEW_URL` | — | — | No |
| Sources API base URL <br><em>Sources service base URL for cluster/source metadata.</em> | platform / `http://127.0.0.1:8002` | `SOURCES_API_BASE_URL` | — | — | No |
| Sources API prefix <br><em>Sources API version prefix (`/api/sources/v3.1`).</em> | `/api/sources/v3.1` | `SOURCES_API_PREFIX` | — | — | No |

Deployment-focused subsets are also summarized in [Configuration](../operations/configuration.md) (database, Kafka, performance tuning, tags).

---

## Global / Platform

Platform-wide recommendation lifecycle and OOM behavior. **No dedicated Settings API** for these rows.

| Setting | Default | Env var | API endpoint | JSON field | Lockable |
|---------|---------|---------|--------------|------------|----------|
| Staleness threshold (hours) <br><em>Hours without a cluster usage report before recommendations are marked **stale**. Stale rows stay visible but flagged. Lower (48) surfaces ingest gaps sooner; higher (120+) tolerates long upload holidays. Works with `ROS_STALE_CLEANUP_DAYS` for deletion timing.</em> | 48 | `ROS_STALENESS_THRESHOLD_HOURS`, `ROS_STALE_DATA_THRESHOLD_HOURS` (alias) | — | — | No |
| Max digest lookback (days) <br><em>Hard cap on digest history queries. Lower speeds DB work; raise for seasonal analysis if partitions retained. Must stay within `ROS_RETENTION_MONTHS` data still on disk.</em> | 90 | `ROS_MAX_LOOKBACK_DAYS` | — | — | No |
| OOM memory bump base factor <br><em>Log-scaling constant after OOMKill events. Higher → larger memory bumps; pairs with `ROS_OOM_MAX_BUMP` cap.</em> | 0.15 | `ROS_OOM_BASE_BUMP` | — | — | No |
| OOM memory bump max multiplier <br><em>Max memory recommendation multiplier after repeated OOMs (1.60 = +60% cap). Prevents runaway suggestions from crash loops.</em> | 1.60 | `ROS_OOM_MAX_BUMP` | — | — | No |
| Recommendation history retention (days) <br><em>Archived prior recommendation versions kept for audit. Independent of digest partition retention.</em> | 90 | `ROS_HISTORY_RETENTION_DAYS` | — | — | No |
| Stale recommendation cleanup (days) <br><em>Permanently delete stale recommendations after this many days. 30d default grace before UI cleanup. `ROS_STALE_ARCHIVE_DAYS` is a deprecated alias.</em> | 30 | `ROS_STALE_CLEANUP_DAYS` | — | — | No |
| Digest partition retention (months) <br><em>Drop monthly digest partitions older than N months. **Irreversible**—align with lookback and compliance needs.</em> | 6 | `ROS_RETENTION_MONTHS` | — | — | No |
| Raw sample partition retention (days) <br><em>Drop `container_usage_samples` / `namespace_usage_samples` partitions older than N days. Detail percentile-band plots use digests; samples are optional drill-down.</em> | 45 | `ROS_SAMPLE_RETENTION_DAYS` | — | — | No |
| Plugin allowlist (CSV) <br><em>Only listed plugins run; empty = all. See General / Infrastructure for deploy-focused notes.</em> | (empty) | `ROS_ENABLED_PLUGINS` | — | — | No |
| Plugin denylist (CSV) <br><em>Skipped plugins; overrides allowlist. Example: `gpu,snapshot` without DCGM or inventory.</em> | (empty) | `ROS_DISABLED_PLUGINS` | — | — | No |

---

## Term windows (all plugins)

**Terms** control observation windows for each recommendation horizon:

- **`window_days`** — how many calendar days of digest history to query.
- **`min_data_days`** — minimum days with real reports inside that window (avoids single-spike recommendations).
- **`decay_halflife_hours`** — exponential recency weighting; `0` = uniform weight across the window.
  When a tenant overrides `window_days` via the Settings API but omits `decay_halflife_hours` (NULL in DB), the engine auto-derives **`window_days × 12`** hours (half the window in hours). Explicit values and admin env-var overrides take precedence. Plugin defaults apply only when no DB row exists for that term. See [Decay Weights](decay-weights.md) for formula, charts, and edge-weight reference.

Short terms react quickly; long terms capture drift. PVC and VM defaults use **longer** windows than container because storage and guests change slowly.

| Plugin | API endpoint | Term names | Default windows (days) / min-data / decay (h) |
|--------|--------------|------------|--------------------------------------------------|
| `container` | `/settings/container` | `short`, `medium`, `long` | 1/1/0 · 7/3/168 · 15/7/360 |
| `namespace` | `...&recommendation_type=namespace` | same | same as container |
| `node` | `...&recommendation_type=node` | same | same as container |
| `gpu` | `...&recommendation_type=gpu` | same | same as container |
| `pvc` | `...&recommendation_type=pvc` | same | 7/3/0 · 30/14/0 · 90/30/0 |
| `vm` | `/settings/vm/terms` | `short_term`, `medium_term`, `long_term` | 7/3/0 · 15/7/0 · 30/15/0 |

Per-term overrides: `PUT` body `{"terms":[{"name":"medium","window_days":10,"min_data_days":5,"decay_halflife_hours":168}]}`. Env locks per field; also `ROS_SETTINGS_LOCKED_TERMS` (generic route only) or type-specific lock under global lock.

---

## Container

Sizing, classification, and notification thresholds for per-container CPU/memory
recommendations via **`GET/PUT/DELETE /settings/container`** (or the deprecated `/settings/thresholds?recommendation_type=container` alias). Cost and performance engines share these parameters with different percentile values.

| Setting | Default | Env var | API endpoint | JSON field | Lockable |
|---------|---------|---------|--------------|------------|----------|
| CPU percentile for cost engine (P60). <br><em>Expanded: The cost engine sizes CPU requests to cover this fraction of observed usage moments. P60 means the recommendation covers 60% of observed CPU usage peaks—potentially leaving 40% of spikes uncovered but saving cost through smaller requests. Lower percentiles = more aggressive rightsizing; higher = safer but more expensive.</em> | 0.60 | `ROS_CONTAINER_CPU_COST_PERCENTILE` | `/settings/container` | cpu_cost_percentile | Yes |
| CPU percentile for performance engine (P98). <br><em>Expanded: The performance engine uses a much higher percentile for safety. P98 means the recommended CPU request covers 98% of observed usage—only the rarest spikes exceed it. This slightly over-provisions but minimizes CPU throttling risk for latency-sensitive workloads.</em> | 0.98 | `ROS_CONTAINER_CPU_PERF_PERCENTILE` | `/settings/container` | cpu_perf_percentile | Yes |
| Memory percentile for cost engine (P95). <br><em>Expanded: Same concept as CPU cost percentile but for memory. P95 means the cost engine recommends enough memory to cover 95% of observed usage peaks. Memory uses a higher default than CPU because OOM kills are more disruptive than CPU throttling.</em> | 0.95 | `ROS_CONTAINER_MEM_COST_PERCENTILE` | `/settings/container` | mem_cost_percentile | Yes |
| Memory percentile for performance engine (max). <br><em>Expanded: A value of 1.0 (100%) means the performance engine recommends at least the maximum memory ever observed—never less than peak usage. This is the maximally safe setting: no OOM risk from undersizing, but potentially significant over-provisioning for workloads with occasional spikes.</em> | 1.0 | `ROS_CONTAINER_MEM_PERF_PERCENTILE` | `/settings/container` | mem_perf_percentile | Yes |
| Adaptive margin floor. <br><em>Expanded: The recommendation engine adds a safety buffer above the observed usage to prevent throttling or OOM kills. This margin adapts based on how variable the workload is (high variance = larger margin). This setting is the minimum margin that will be applied even for perfectly stable workloads. A value of 1.15 means at least 15% headroom above observed usage.</em> | 1.15 | `ROS_CONTAINER_MIN_MARGIN` | `/settings/container` | min_margin | Yes |
| Adaptive margin ceiling. <br><em>Expanded: The maximum safety buffer applied to highly variable workloads. A value of 1.50 means at most 50% headroom. Workloads with erratic usage patterns get closer to this cap.</em> | 1.50 | `ROS_CONTAINER_MAX_MARGIN` | `/settings/container` | max_margin | Yes |
| Limit = request × multiplier. <br><em>Expanded: In Kubernetes, the "request" is what the scheduler reserves for the container; the "limit" is the hard cap on usage. This multiplier sets limit = request × value. 1.05 means the limit is 5% above the recommended request—a small buffer that allows brief bursts without letting the container consume unbounded resources. Limits above 1.0 prevent runaway usage while keeping request and limit closely aligned.</em> | 1.05 | `ROS_CONTAINER_LIMIT_MULTIPLIER` | `/settings/container` | limit_multiplier | Yes |
| Minimum CPU request (millicores). <br><em>Expanded: No recommendation will ever suggest less than this value. This prevents impractically small CPU requests that could cause scheduling issues or extreme throttling. 25 millicores (0.025 cores) is the practical minimum for most containers on OpenShift.</em> | 25 | `ROS_CONTAINER_CPU_FLOOR_MC` | `/settings/container` | cpu_floor_mc | Yes |
| Max CPU for idle classification. <br><em>Expanded: Maximum CPU usage (millicores) for a container to be classified as "idle." If a container never exceeds this usage over the entire observation window, it is considered idle—a candidate for removal or decommissioning. 10 millicores means essentially zero CPU activity. Idle containers trigger a special notification suggesting they may no longer be needed.</em> | 10 | `ROS_CONTAINER_IDLE_CPU_THRESHOLD_MC` | `/settings/container` | idle_cpu_threshold_mc | Yes |
| Max memory for idle classification (10 MiB). <br><em>Expanded: Maximum memory usage (KiB) for idle classification. If peak memory never exceeds this value, the container is idle. 10240 KiB = 10 MiB—a container using less than 10 MiB of memory over the observation window is likely a dormant sidecar, forgotten job, or abandoned deployment.</em> | 10240 | `ROS_CONTAINER_IDLE_MEM_THRESHOLD_KIB` | `/settings/container` | idle_mem_threshold_kib | Yes |
| Memory trend slope (KiB/day) for notification. <br><em>Expanded: If the container's memory consumption is growing faster than this rate (measured by linear regression over recent days), a notification is emitted warning about potential memory leaks or growing datasets. 100 KiB/day ≈ 3 MiB/month.</em> | 100.0 | `ROS_CONTAINER_MEM_TREND_SLOPE_THRESHOLD` | `/settings/container` | mem_trend_slope_threshold | Yes |
| Confidence below which low-confidence notification fires. <br><em>Expanded: Confidence is calculated as `min(days_of_data / window_days, 1.0)` — i.e., how much of the requested observation window actually has data. A threshold of 0.5 means: if less than 50% of the window has data (e.g., 3 days of data in a 7-day window), the recommendation is flagged as low-confidence.</em> | 0.5 | `ROS_CONTAINER_LOW_CONFIDENCE_THRESHOLD` | `/settings/container` | low_confidence_threshold | Yes |
| Absolute data days at or below which sparse-data notification fires. <br><em>Expanded: Orthogonal to low-confidence: flags recommendations with few days of observation regardless of window coverage. Default 2 means 1–2 days of data emit code 77 (`SPARSE_DATA`); 3+ days do not.</em> | 2 | `ROS_CONTAINER_SPARSE_DATA_THRESHOLD` | `/settings/container` | sparse_data_threshold | Yes |
| Short-term window. <br><em>Expanded: How many days of usage history the short-term recommendation looks at. 1 day captures very recent behavior—useful for detecting sudden changes or newly deployed workloads. Shorter windows react faster but are noisier.</em> | 1 | `ROS_TERMS_CONTAINER_SHORT_WINDOW_DAYS` | `/settings/terms?recommendation_type=container` | terms[].window_days | Yes |
| Short min data days. <br><em>Expanded: Minimum days of actual data required before a short-term recommendation is produced. If the container has less than this many days of reports, the short-term engine skips it. 1 means even a single day of data is enough.</em> | 1 | `ROS_TERMS_CONTAINER_SHORT_MIN_DATA_DAYS` | `/settings/terms?recommendation_type=container` | terms[].min_data_days | Yes |
| Short decay (0=none). <br><em>Expanded: Exponential decay half-life for weighting recent data more heavily in the short-term window. 0 means no decay—all hours in the window are weighted equally. A non-zero value (e.g., 24) would make yesterday's data count twice as much as data from two days ago.</em> | 0 | `ROS_TERMS_CONTAINER_SHORT_DECAY_HALFLIFE_HOURS` | `/settings/terms?recommendation_type=container` | terms[].decay_halflife_hours | Yes |
| Medium window. <br><em>Expanded: Observation window for medium-term recommendations. 7 days captures a full week of usage patterns, including weekday/weekend variation. This is the default "steady state" recommendation most users see.</em> | 7 | `ROS_TERMS_CONTAINER_MEDIUM_WINDOW_DAYS` | `/settings/terms?recommendation_type=container` | terms[].window_days | Yes |
| Medium min data. <br><em>Expanded: Minimum days of data required for a medium-term recommendation. 3 means at least 3 days of reports must exist within the 7-day window. Prevents recommendations based on a single busy day or deployment spike.</em> | 3 | `ROS_TERMS_CONTAINER_MEDIUM_MIN_DATA_DAYS` | `/settings/terms?recommendation_type=container` | terms[].min_data_days | Yes |
| Medium decay (7d). <br><em>Expanded: Decay half-life for the medium-term window. 168 hours = 7 days, meaning data from the start of the window has half the weight of data from today. Recent usage patterns influence the recommendation more than older data within the same window.</em> | 168 | `ROS_TERMS_CONTAINER_MEDIUM_DECAY_HALFLIFE_HOURS` | `/settings/terms?recommendation_type=container` | terms[].decay_halflife_hours | Yes |
| Long window. <br><em>Expanded: Observation window for long-term recommendations. 15 days captures broader trends, release cycles, and monthly patterns. Used for capacity planning and detecting gradual drift in resource needs.</em> | 15 | `ROS_TERMS_CONTAINER_LONG_WINDOW_DAYS` | `/settings/terms?recommendation_type=container` | terms[].window_days | Yes |
| Long min data. <br><em>Expanded: Minimum days of data required for a long-term recommendation. 7 means at least a full week of data must exist within the 15-day window. Ensures long-term recommendations aren't based on incomplete history.</em> | 7 | `ROS_TERMS_CONTAINER_LONG_MIN_DATA_DAYS` | `/settings/terms?recommendation_type=container` | terms[].min_data_days | Yes |
| Long decay (15d). <br><em>Expanded: Decay half-life for the long-term window. 360 hours = 15 days, matching the window length. Data from two weeks ago has half the influence of today's data, emphasizing recent trends while still considering the full window.</em> | 360 | `ROS_TERMS_CONTAINER_LONG_DECAY_HALFLIFE_HOURS` | `/settings/terms?recommendation_type=container` | terms[].decay_halflife_hours | Yes |

\* Threshold fields via `PUT /settings/container` (or thresholds alias). Term windows via `PUT /settings/terms?recommendation_type=container`. Admin `ROS_TERMS_*` env vars lock individual term fields.

See [Container recommendations](../features/container-recommendations.md).

---

## Namespace

Same sizing parameters as container. Namespace recommendations aggregate
container-level digests; thresholds apply to the aggregated series.

| Setting | Default | Env var | API endpoint | JSON field | Lockable |
|---------|---------|---------|--------------|------------|----------|
| CPU cost percentile. <br><em>Expanded: Same as container CPU cost percentile but applied to the namespace aggregate—the combined CPU usage of all containers in a Kubernetes namespace. P60 covers 60% of observed namespace-level CPU peaks, enabling cost-focused rightsizing for the entire namespace's resource quota.</em> | 0.60 | `ROS_NAMESPACE_CPU_COST_PERCENTILE` | `/settings/namespace` | cpu_cost_percentile | Yes |
| CPU perf percentile. <br><em>Expanded: Performance-engine CPU percentile for namespace aggregates. P98 covers 98% of namespace-level CPU usage, providing safe headroom for the combined workload of all containers in the namespace.</em> | 0.98 | `ROS_NAMESPACE_CPU_PERF_PERCENTILE` | `/settings/namespace` | cpu_perf_percentile | Yes |
| Memory cost percentile. <br><em>Expanded: Cost-engine memory percentile for namespace aggregates. P95 covers 95% of observed namespace-level memory peaks. Namespace memory is the sum of all container memory in the namespace.</em> | 0.95 | `ROS_NAMESPACE_MEM_COST_PERCENTILE` | `/settings/namespace` | mem_cost_percentile | Yes |
| Memory perf percentile. <br><em>Expanded: Performance-engine memory percentile for namespace aggregates. 1.0 = max means the recommendation never goes below the highest memory usage ever observed across all containers in the namespace.</em> | 1.0 | `ROS_NAMESPACE_MEM_PERF_PERCENTILE` | `/settings/namespace` | mem_perf_percentile | Yes |
| Adaptive margin floor. <br><em>Expanded: Adaptive margin floor for namespace-level recommendations. Minimum headroom percentage applied even for stable namespace-wide usage. 1.15 = at least 15% above observed.</em> | 1.15 | `ROS_NAMESPACE_MIN_MARGIN` | `/settings/namespace` | min_margin | Yes |
| Adaptive margin ceiling. <br><em>Expanded: Adaptive margin ceiling for namespace-level recommendations. Maximum headroom for highly variable namespace usage. 1.50 = at most 50% above observed.</em> | 1.50 | `ROS_NAMESPACE_MAX_MARGIN` | `/settings/namespace` | max_margin | Yes |
| Limit multiplier. <br><em>Expanded: Sets namespace-level resource limit = request × multiplier. 1.05 means limits are 5% above recommended requests for the namespace aggregate. Applies to namespace resource quota recommendations.</em> | 1.05 | `ROS_NAMESPACE_LIMIT_MULTIPLIER` | `/settings/namespace` | limit_multiplier | Yes |
| CPU floor (m). <br><em>Expanded: Minimum CPU request in namespace recommendations (millicores). Prevents recommending impractically tiny CPU requests for the entire namespace aggregate.</em> | 25 | `ROS_NAMESPACE_CPU_FLOOR_MC` | `/settings/namespace` | cpu_floor_mc | Yes |
| Idle CPU (m). <br><em>Expanded: Maximum CPU usage (millicores) for a namespace to be classified as idle. If no container in the namespace ever exceeds this usage over the observation window, the entire namespace is considered idle. Idle namespaces get a special notification suggesting they may be candidates for decommissioning.</em> | 10 | `ROS_NAMESPACE_IDLE_CPU_THRESHOLD_MC` | `/settings/namespace` | idle_cpu_threshold_mc | Yes |
| Idle memory (KiB). <br><em>Expanded: Maximum memory usage (KiB) for idle namespace classification. 10240 KiB = 10 MiB. If peak memory across all containers never exceeds this, the namespace is idle.</em> | 10240 | `ROS_NAMESPACE_IDLE_MEM_THRESHOLD_KIB` | `/settings/namespace` | idle_mem_threshold_kib | Yes |
| Trend slope (KiB/day). <br><em>Expanded: Memory growth rate (KiB/day) above which a 'trending up' notification fires for the namespace. 500 KiB/day ≈ 15 MiB/month; higher than container default because namespace aggregates grow faster. Helps detect runaway growth across the namespace before it becomes critical.</em> | 500.0 | `ROS_NAMESPACE_MEM_TREND_SLOPE_THRESHOLD` | `/settings/namespace` | mem_trend_slope_threshold | Yes |
| Low confidence threshold. <br><em>Expanded: Confidence threshold for namespace recommendations. Same calculation as container: days_of_data / window_days. Below this → low-confidence notification.</em> | 0.5 | `ROS_NAMESPACE_LOW_CONFIDENCE_THRESHOLD` | `/settings/namespace` | low_confidence_threshold | Yes |
| Sparse data threshold. <br><em>Expanded: Same as container: absolute `data_days` at or below this value emit code 77 (`SPARSE_DATA`).</em> | 2 | `ROS_NAMESPACE_SPARSE_DATA_THRESHOLD` | `/settings/namespace` | sparse_data_threshold | Yes |
| Short-term window. <br><em>Expanded: Days of namespace-aggregated usage history for short-term recommendations. 1 day captures immediate namespace-level changes (e.g., a new deployment scaling up the entire namespace).</em> | 1 | `ROS_TERMS_NAMESPACE_SHORT_WINDOW_DAYS` | `/settings/terms?recommendation_type=namespace` | terms[].window_days | Yes |
| Short min data days. <br><em>Expanded: Minimum days of namespace data required for a short-term recommendation. 1 means a single day of aggregated namespace reports is sufficient.</em> | 1 | `ROS_TERMS_NAMESPACE_SHORT_MIN_DATA_DAYS` | `/settings/terms?recommendation_type=namespace` | terms[].min_data_days | Yes |
| Short decay (0=none). <br><em>Expanded: Decay half-life for short-term namespace window. 0 = equal weighting across all hours in the window.</em> | 0 | `ROS_TERMS_NAMESPACE_SHORT_DECAY_HALFLIFE_HOURS` | `/settings/terms?recommendation_type=namespace` | terms[].decay_halflife_hours | Yes |
| Medium window. <br><em>Expanded: 7-day observation window for namespace medium-term recommendations. Captures weekly usage patterns across all containers in the namespace.</em> | 7 | `ROS_TERMS_NAMESPACE_MEDIUM_WINDOW_DAYS` | `/settings/terms?recommendation_type=namespace` | terms[].window_days | Yes |
| Medium min data. <br><em>Expanded: At least 3 days of namespace data required within the 7-day medium window before producing a recommendation.</em> | 3 | `ROS_TERMS_NAMESPACE_MEDIUM_MIN_DATA_DAYS` | `/settings/terms?recommendation_type=namespace` | terms[].min_data_days | Yes |
| Medium decay (7d). <br><em>Expanded: 168-hour (7-day) decay half-life. Recent namespace usage weighted more heavily than data from the start of the window.</em> | 168 | `ROS_TERMS_NAMESPACE_MEDIUM_DECAY_HALFLIFE_HOURS` | `/settings/terms?recommendation_type=namespace` | terms[].decay_halflife_hours | Yes |
| Long window. <br><em>Expanded: 15-day window for long-term namespace recommendations. Useful for namespace quota planning and detecting gradual resource growth across a project.</em> | 15 | `ROS_TERMS_NAMESPACE_LONG_WINDOW_DAYS` | `/settings/terms?recommendation_type=namespace` | terms[].window_days | Yes |
| Long min data. <br><em>Expanded: Minimum 7 days of namespace data required within the 15-day long window.</em> | 7 | `ROS_TERMS_NAMESPACE_LONG_MIN_DATA_DAYS` | `/settings/terms?recommendation_type=namespace` | terms[].min_data_days | Yes |
| Long decay (15d). <br><em>Expanded: 360-hour (15-day) decay half-life for the long-term namespace window.</em> | 360 | `ROS_TERMS_NAMESPACE_LONG_DECAY_HALFLIFE_HOURS` | `/settings/terms?recommendation_type=namespace` | terms[].decay_halflife_hours | Yes |

\* Threshold fields via `PUT /settings/namespace` (or thresholds alias). Term windows via `PUT /settings/terms?recommendation_type=namespace`.

See [Namespace recommendations](../features/namespace-recommendations.md).

---

## Node

Node-level CPU/memory utilization, classification, and dual-engine sizing
(cost target 80%, performance target 55%).

| Setting | Default | Env var | API endpoint | JSON field | Lockable |
|---------|---------|---------|--------------|------------|----------|
| P95 util below → underutilized. <br><em>Expanded: Node underutilization threshold (fraction of allocatable capacity). A node is classified as 'underutilized' when BOTH its CPU P95 AND memory P95 usage are below this fraction of allocatable capacity. 0.30 means: if a node never uses more than 30% of its CPU and 30% of its memory, it's underutilized and a candidate for consolidation.</em> | 0.30 | `ROS_NODE_UNDERUTIL_THRESHOLD` | `/settings/node` | underutil_threshold | Yes |
| Requests/allocatable above → overcommitted. <br><em>Expanded: Node overcommit threshold (ratio of total pod requests to allocatable). A node is 'overcommitted' when the sum of all pod CPU requests exceeds this multiple of the node's allocatable CPU. 1.50 means: if pods request 150% of what the node can actually provide, the node is dangerously overcommitted and likely to experience evictions.</em> | 1.50 | `ROS_NODE_OVERCOMMIT_THRESHOLD` | `/settings/node` | overcommit_threshold | Yes |
| Fallback when allocatable unknown. <br><em>Expanded: Fallback multiplier when node allocatable capacity is unknown. Some nodes don't report allocatable resources. In that case, allocatable is estimated as `max_observed_requests × this_factor`. 0.93 accounts for system-reserved resources (~7% for kubelet, OS, etc.).</em> | 0.93 | `ROS_NODE_ALLOCATABLE_FACTOR` | `/settings/node` | allocatable_factor | Yes |
| CPU/mem imbalance → stranded. <br><em>Expanded: CPU/memory imbalance ratio above which a node is classified as having 'stranded resources'. Stranded means one resource (e.g., CPU) is heavily used while the other (memory) has large amounts wasted. Calculated as ` | 0.60 | `ROS_NODE_STRANDED_IMBALANCE_THRESHOLD` | `/settings/node` | stranded_imbalance_threshold | Yes |
| EMA smoothing factor. <br><em>Expanded: Exponential Moving Average (EMA) smoothing factor for node trend and imbalance calculations. Higher values (closer to 1.0) react faster to recent changes but are noisier. Lower values (closer to 0.0) are smoother but lag behind. 0.30 gives moderate smoothing, weighting recent data ~30% vs ~70% history.</em> | 0.30 | `ROS_NODE_EMA_ALPHA` | `/settings/node` | ema_alpha | Yes |
| Cost engine target (80%). <br><em>Expanded: The cost engine sizes node recommendations assuming nodes should run at this utilization level. 0.80 (80%) means the engine recommends enough nodes to keep average utilization around 80%—leaving 20% spare capacity for bursts. Lower values = more spare capacity and higher cost; higher values = tighter packing with less headroom.</em> | 0.80 | `ROS_NODE_COST_TARGET_UTILIZATION` | `/settings/node` | cost_target_utilization | Yes |
| Performance engine target (55%). <br><em>Expanded: The performance engine uses a much more conservative target. 0.55 (55%) means nodes should run at most ~55% utilization, leaving 45% headroom for latency-sensitive workloads and failure recovery. This produces fewer consolidation recommendations and larger node counts than the cost engine.</em> | 0.55 | `ROS_NODE_PERF_TARGET_UTILIZATION` | `/settings/node` | perf_target_utilization | Yes |
| Perf consolidates only when current ≥ N× recommended. <br><em>Expanded: Performance engine consolidation guard. The performance engine will only recommend consolidating nodes (reducing node count) when the current capacity is at least this multiple of the recommended capacity on BOTH CPU and memory. 2.0 means: the cluster must have at least twice the recommended resources before consolidation is suggested. This prevents the performance engine from being too aggressive with capacity reduction.</em> | 2.0 | `ROS_NODE_PERF_CONSOLIDATION_HEADROOM_MULTIPLIER` | `/settings/node` | perf_consolidation_headroom_multiplier | Yes |
| Min days for CPU trend slope. <br><em>Expanded: Minimum days of node usage data required before computing a CPU trend (growth or decline). Trend detection uses linear regression and needs enough data points to be meaningful. 3 days prevents noisy trend alerts from a single day's spike or dip in node utilization.</em> | 3 | `ROS_NODE_TREND_MIN_DAYS` | `/settings/node` | trend_min_days | Yes |
| Pod scheduling consolidation gate. <br><em>Expanded: Minimum pod scheduling headroom (fraction 0.0–1.0) before the cost/performance engines suppress `node_count_reduction` consolidation on that node. Headroom is `(pod_capacity − max_pod_count) / pod_capacity` when capacity is known. Below this fraction (default **0.15**), consolidation is blocked even if the node is underutilized. Must be ≥ `pod_headroom_notification_threshold`.</em> | 0.15 | `ROS_NODE_POD_HEADROOM_CONSOLIDATION_GATE` | `/settings/node` | pod_headroom_consolidation_gate | Yes |
| Pod scheduling notification threshold. <br><em>Expanded: Headroom below this fraction emits notification code **74** (`NotifNodePodSchedulingLimit`). Default **0.10** (10%).</em> | 0.10 | `ROS_NODE_POD_HEADROOM_NOTIFICATION_THRESHOLD` | `/settings/node` | pod_headroom_notification_threshold | Yes |
| Zombie CPU P95 (millicores). <br><em>Expanded: Node is classified `zombie` when CPU P95 is below this value and running pod count is at most `zombie_max_pods`. Stricter than idle (near-zero CPU with few pods).</em> | 200 | `ROS_NODE_ZOMBIE_CPU_MC` | `/settings/node` | zombie_cpu_p95_mc | Yes |
| Zombie max pods. <br><em>Expanded: Maximum running pods for zombie classification.</em> | 5 | `ROS_NODE_ZOMBIE_MAX_PODS` | `/settings/node` | zombie_max_pods | Yes |
| Idle CPU util % of allocatable. <br><em>Expanded: Idle candidate when CPU P95 utilization (fraction of allocatable) is below this percent (0–100).</em> | 10 | `ROS_NODE_IDLE_CPU_UTIL_PCT` | `/settings/node` | idle_cpu_util_pct | Yes |
| Idle memory util % of allocatable. <br><em>Expanded: Idle candidate when memory P95 utilization is below this percent (0–100).</em> | 10 | `ROS_NODE_IDLE_MEM_UTIL_PCT` | `/settings/node` | idle_mem_util_pct | Yes |
| Idle max pods. <br><em>Expanded: Maximum running pods for idle classification (with low CPU and memory util).</em> | 10 | `ROS_NODE_IDLE_MAX_PODS` | `/settings/node` | idle_max_pods | Yes |
| Short-term window. <br><em>Expanded: Days of node-level usage history for short-term node recommendations. 1 day captures immediate node utilization changes.</em> | 1 | `ROS_TERMS_NODE_SHORT_WINDOW_DAYS` | `/settings/terms?recommendation_type=node` | terms[].window_days | Yes |
| Short min data days. <br><em>Expanded: Minimum days of node telemetry required for a short-term node recommendation. 1 day is sufficient.</em> | 1 | `ROS_TERMS_NODE_SHORT_MIN_DATA_DAYS` | `/settings/terms?recommendation_type=node` | terms[].min_data_days | Yes |
| Short decay (0=none). <br><em>Expanded: Decay half-life for short-term node window. 0 = no decay, all hours weighted equally.</em> | 0 | `ROS_TERMS_NODE_SHORT_DECAY_HALFLIFE_HOURS` | `/settings/terms?recommendation_type=node` | terms[].decay_halflife_hours | Yes |
| Medium window. <br><em>Expanded: 7-day window for medium-term node recommendations. Captures weekly node utilization patterns including weekday/weekend variation in cluster load.</em> | 7 | `ROS_TERMS_NODE_MEDIUM_WINDOW_DAYS` | `/settings/terms?recommendation_type=node` | terms[].window_days | Yes |
| Medium min data. <br><em>Expanded: At least 3 days of node data required within the 7-day medium window.</em> | 3 | `ROS_TERMS_NODE_MEDIUM_MIN_DATA_DAYS` | `/settings/terms?recommendation_type=node` | terms[].min_data_days | Yes |
| Medium decay (7d). <br><em>Expanded: 168-hour decay half-life for medium-term node window.</em> | 168 | `ROS_TERMS_NODE_MEDIUM_DECAY_HALFLIFE_HOURS` | `/settings/terms?recommendation_type=node` | terms[].decay_halflife_hours | Yes |
| Long window. <br><em>Expanded: 15-day window for long-term node capacity planning recommendations.</em> | 15 | `ROS_TERMS_NODE_LONG_WINDOW_DAYS` | `/settings/terms?recommendation_type=node` | terms[].window_days | Yes |
| Long min data. <br><em>Expanded: Minimum 7 days of node data required within the 15-day long window.</em> | 7 | `ROS_TERMS_NODE_LONG_MIN_DATA_DAYS` | `/settings/terms?recommendation_type=node` | terms[].min_data_days | Yes |
| Long decay (15d). <br><em>Expanded: 360-hour decay half-life for long-term node window.</em> | 360 | `ROS_TERMS_NODE_LONG_DECAY_HALFLIFE_HOURS` | `/settings/terms?recommendation_type=node` | terms[].decay_halflife_hours | Yes |

**Node threshold Settings API:** `GET` / `PUT` / `DELETE`
`/settings/node` (canonical) or deprecated `/settings/thresholds?recommendation_type=node`.
JSON fields: `underutil_threshold`, `overcommit_threshold`, `allocatable_factor`,
`stranded_imbalance_threshold`, `ema_alpha`, `cost_target_utilization`,
`perf_target_utilization`, `perf_consolidation_headroom_multiplier`, `trend_min_days`,
`zombie_cpu_p95_mc`, `zombie_max_pods`, `idle_cpu_util_pct`, `idle_mem_util_pct`,
`idle_max_pods`, `pod_headroom_consolidation_gate`, `pod_headroom_notification_threshold`
(plus `locked_fields`). Term windows remain on
`PUT /settings/node`.

\* Threshold fields via `PUT /settings/node` (or thresholds alias). Term windows via `PUT /settings/node`.

See [Node recommendations](../features/node-recommendations.md).

---

## GPU

!!! warning "Expert configuration only"
    GPU thresholds interact with NVIDIA DCGM profiling semantics and MIG hardware sizing.
    Change only with GPU workload expertise. Incorrect values produce misleading
    recommendations.

Classification, MIG sizing, confidence scoring, and time-slicing parameters.

| Setting | Default | Env var | API endpoint | JSON field | Lockable |
|---------|---------|---------|--------------|------------|----------|
| Avg SM below → idle. <br><em>Expanded: SM (Streaming Multiprocessor) utilization measures what fraction of the GPU's compute cores are active. Average SM below 0.02 (2%) means the GPU is essentially idle—almost none of its processing units are doing work. Such GPUs are candidates for deallocation or reassignment to other workloads.</em> | 0.02 | `ROS_GPU_IDLE_THRESHOLD` | `/settings/gpu` | idle_threshold | Yes |
| SM below → underutilized. <br><em>Expanded: A GPU is "underutilized" when its average SM utilization is below 25%. In practice, this means the GPU is provisioned but most of its compute capacity goes unused—common when a full GPU is assigned to a workload that only needs a fraction of its power. Triggers rightsizing or MIG partitioning recommendations.</em> | 0.25 | `ROS_GPU_UNDERUTILIZED_SM_THRESHOLD` | `/settings/gpu` | underutilized_sm_threshold | Yes |
| Tensor below → underutilized. <br><em>Expanded: Tensor cores are specialized GPU units optimized for matrix math used in ML training and inference. Average tensor core utilization below 15% (combined with low SM) indicates the workload isn't using the GPU's AI-optimized hardware—suggesting the GPU may be oversized for the task or the workload isn't GPU-accelerated.</em> | 0.15 | `ROS_GPU_UNDERUTILIZED_TENSOR_THRESHOLD` | `/settings/gpu` | underutilized_tensor_threshold | Yes |
| DRAM above → memory-bound. <br><em>Expanded: GPU DRAM (framebuffer) utilization above 60% indicates a memory-bound workload—the GPU's compute cores are waiting for data rather than doing computation. Such workloads may benefit from more GPU memory (larger MIG profile) rather than more compute.</em> | 0.60 | `ROS_GPU_MEMBOUND_DRAM_THRESHOLD` | `/settings/gpu` | membound_dram_threshold | Yes |
| Tensor below + high DRAM → memory-bound. <br><em>Expanded: Combined condition for memory-bound classification: DRAM utilization is high (above membound threshold) AND tensor core utilization is low (below this value). This pattern confirms the workload is limited by GPU memory bandwidth, not compute—typical for large model inference or data-heavy preprocessing.</em> | 0.15 | `ROS_GPU_MEMBOUND_TENSOR_THRESHOLD` | `/settings/gpu` | membound_tensor_threshold | Yes |
| MIG sizing: P98 × factor. <br><em>Expanded: Framebuffer (FB) is the GPU's dedicated video memory. When sizing MIG (Multi-Instance GPU) partitions, the recommended FB allocation is P98 framebuffer usage × this factor. 1.20 adds 20% headroom above the 98th percentile of observed memory usage, preventing OOM on the GPU while avoiding over-provisioning MIG slices.</em> | 1.20 | `ROS_GPU_FB_HEADROOM_FACTOR` | `/settings/gpu` | fb_headroom_factor | Yes |
| DRAM below → compute_bound_underutil. <br><em>Expanded: When DRAM utilization is below 30% but SM utilization is also low, the GPU is "compute-bound underutilized"—it has plenty of memory but isn't doing much computation. This pattern suggests the GPU is oversized for the workload and could be replaced with a smaller GPU or shared via time-slicing/MIG.</em> | 0.30 | `ROS_GPU_COMPUTE_BOUND_DRAM_THRESHOLD` | `/settings/gpu` | compute_bound_dram_threshold | Yes |
| Percentile for MIG FB selection. <br><em>Expanded: Which percentile of observed GPU framebuffer usage to use when selecting a MIG profile size. P98 means the MIG partition will be sized to cover 98% of observed memory usage peaks. Higher percentiles = larger (safer) MIG slices; lower = more aggressive partitioning with more slices per GPU.</em> | 0.98 | `ROS_GPU_MIG_FB_PERCENTILE` | `/settings/gpu` | mig_fb_percentile | Yes |
| Days below → confidence 0.3. <br><em>Expanded: First confidence tier boundary (days of GPU data). With fewer than this many days of profiling data, the recommendation confidence starts at 0.3 (very low). This protects against making bold GPU recommendations from insufficient data.</em> | 3 | `ROS_GPU_CONFIDENCE_DAYS_TIER1` | `/settings/gpu` | confidence_days_tier1 | Yes |
| Days below → confidence 0.6. <br><em>Expanded: Second tier boundary. Below this → confidence base 0.6. Between tier1 and tier2, the GPU classification is considered moderately confident.</em> | 7 | `ROS_GPU_CONFIDENCE_DAYS_TIER2` | `/settings/gpu` | confidence_days_tier2 | Yes |
| Days below → confidence 0.8. <br><em>Expanded: Third tier boundary. Below this → confidence base 0.8. Above → confidence 1.0 (full confidence). The tiered approach ensures that longer observation periods produce more trustworthy GPU recommendations.</em> | 14 | `ROS_GPU_CONFIDENCE_DAYS_TIER3` | `/settings/gpu` | confidence_days_tier3 | Yes |
| max SM / avg SM → bursty. <br><em>Expanded: Burst detection ratio for GPU workloads. Calculated as `max_SM_utilization / avg_SM_utilization`. When this ratio exceeds the threshold, the workload is considered 'bursty' (short intense GPU use interspersed with idle periods). Bursty workloads get a confidence penalty because peak vs average divergence makes sizing uncertain. 5.0 means: if peak GPU use is 5× the average, it's bursty.</em> | 5.0 | `ROS_GPU_SPIKE_RATIO_THRESHOLD` | `/settings/gpu` | spike_ratio_threshold | Yes |
| Penalty on spike. <br><em>Expanded: Confidence multiplier applied when a GPU workload is classified as bursty (spiky usage). 0.70 means confidence is reduced to 70% of its normal value. Bursty workloads are harder to size correctly because average utilization understates peak needs—this penalty reflects that uncertainty in the recommendation.</em> | 0.70 | `ROS_GPU_SPIKE_CONFIDENCE_PENALTY` | `/settings/gpu` | spike_confidence_penalty | Yes |
| Confidence when no profiling. <br><em>Expanded: Confidence multiplier when NVIDIA DCGM profiling metrics are absent. Some GPUs or drivers don't report detailed SM/tensor/DRAM metrics. In that case, classification relies only on basic utilization data and confidence is scaled by this factor. 0.50 means halved confidence without profiling.</em> | 0.50 | `ROS_GPU_NO_PROFILING_CONFIDENCE_FACTOR` | `/settings/gpu` | no_profiling_confidence_factor | Yes |
| Min fraction of eligible containers. <br><em>Expanded: Minimum fraction of GPU-using containers on a node that must be time-slicing candidates for a time-slicing recommendation to be emitted. Prevents recommending time-slicing when only a small minority of GPU workloads would benefit. 0.50 means at least half the GPU containers must be underutilizing their full GPU.</em> | 0.50 | `ROS_GPU_TIMESLICING_MAJORITY_THRESHOLD` | `/settings/gpu` | timeslicing_majority_threshold | Yes |
| Min replicas. <br><em>Expanded: Minimum number of time-slicing replicas in a recommendation. Time-slicing allows multiple containers to share one physical GPU by rapidly switching between them. 2 replicas means at least 2 workloads can share the GPU. Lower bound prevents recommending time-slicing when only one container would benefit.</em> | 2 | `ROS_GPU_TIMESLICING_MIN_REPLICAS` | `/settings/gpu` | timeslicing_min_replicas | Yes |
| Max replicas. <br><em>Expanded: Maximum number of time-slicing replicas in a recommendation. 8 means up to 8 containers can share one GPU. Higher replica counts increase cost savings but reduce per-container GPU performance due to context switching overhead. Caps prevent unrealistic sharing recommendations.</em> | 8 | `ROS_GPU_TIMESLICING_MAX_REPLICAS` | `/settings/gpu` | timeslicing_max_replicas | Yes |
| Base confidence penalty. <br><em>Expanded: Starting confidence level for time-slicing recommendations before adjusting for how many containers would benefit. 0.70 means time-slicing recommendations start at 70% confidence because sharing a GPU introduces performance unpredictability—workloads may interfere with each other during context switches.</em> | 0.70 | `ROS_GPU_TIMESLICING_BASE_PENALTY` | `/settings/gpu` | timeslicing_base_penalty | Yes |
| Impacted-container weight. <br><em>Expanded: Weight of the 'impacted container ratio' in time-slicing confidence calculation. Time-slicing confidence = `base_penalty + impacted_weight × (candidates/total_gpu_containers)`. Higher weight means confidence increases more when a larger proportion of containers would benefit from sharing.</em> | 0.30 | `ROS_GPU_TIMESLICING_IMPACTED_WEIGHT` | `/settings/gpu` | timeslicing_impacted_weight | Yes |
| Max age of node GPU telemetry. <br><em>Expanded: Maximum age (days) of node-level GPU telemetry data for time-slicing analysis. Nodes whose last GPU report is older than this are excluded from time-slicing recommendations. Prevents stale data from producing outdated sharing suggestions.</em> | 7 | `ROS_GPU_NODE_FRESHNESS_DAYS` | `/settings/gpu` | node_freshness_days | Yes |
| Short-term window. <br><em>Expanded: Days of GPU profiling data for short-term recommendations. 1 day captures recent GPU usage—useful for detecting training job starts or inference scaling events.</em> | 1 | `ROS_TERMS_GPU_SHORT_WINDOW_DAYS` | `/settings/terms?recommendation_type=gpu` | terms[].window_days | Yes |
| Short min data days. <br><em>Expanded: Minimum days of GPU data required for a short-term GPU recommendation.</em> | 1 | `ROS_TERMS_GPU_SHORT_MIN_DATA_DAYS` | `/settings/terms?recommendation_type=gpu` | terms[].min_data_days | Yes |
| Short decay (0=none). <br><em>Expanded: Decay half-life for short-term GPU window. 0 = equal weighting.</em> | 0 | `ROS_TERMS_GPU_SHORT_DECAY_HALFLIFE_HOURS` | `/settings/terms?recommendation_type=gpu` | terms[].decay_halflife_hours | Yes |
| Medium window. <br><em>Expanded: 7-day window for medium-term GPU recommendations. Captures full training epochs and weekly inference patterns.</em> | 7 | `ROS_TERMS_GPU_MEDIUM_WINDOW_DAYS` | `/settings/terms?recommendation_type=gpu` | terms[].window_days | Yes |
| Medium min data. <br><em>Expanded: At least 3 days of GPU profiling data required within the 7-day medium window.</em> | 3 | `ROS_TERMS_GPU_MEDIUM_MIN_DATA_DAYS` | `/settings/terms?recommendation_type=gpu` | terms[].min_data_days | Yes |
| Medium decay (7d). <br><em>Expanded: 168-hour decay half-life for medium-term GPU window.</em> | 168 | `ROS_TERMS_GPU_MEDIUM_DECAY_HALFLIFE_HOURS` | `/settings/terms?recommendation_type=gpu` | terms[].decay_halflife_hours | Yes |
| Long window. <br><em>Expanded: 15-day window for long-term GPU capacity planning. Captures multiple training cycles and sustained inference load patterns.</em> | 15 | `ROS_TERMS_GPU_LONG_WINDOW_DAYS` | `/settings/terms?recommendation_type=gpu` | terms[].window_days | Yes |
| Long min data. <br><em>Expanded: Minimum 7 days of GPU data required within the 15-day long window.</em> | 7 | `ROS_TERMS_GPU_LONG_MIN_DATA_DAYS` | `/settings/terms?recommendation_type=gpu` | terms[].min_data_days | Yes |
| Long decay (15d). <br><em>Expanded: 360-hour decay half-life for long-term GPU window.</em> | 360 | `ROS_TERMS_GPU_LONG_DECAY_HALFLIFE_HOURS` | `/settings/terms?recommendation_type=gpu` | terms[].decay_halflife_hours | Yes |

\* Threshold fields via `PUT /settings/gpu` (or thresholds alias). Term windows via `PUT /settings/terms?recommendation_type=gpu`.

See also [GPU Classification](gpu-classification.md) for the decision tree,
[GPU MIG profiling](../features/gpu-mig.md), and
[GPU Time-Slicing](../features/gpu-time-slicing.md) for replica selection logic.

---

## PVC

Storage right-sizing thresholds. PVC uses longer default term windows (7d / 30d / 90d)
because storage growth is slow.

| Setting | Default | Env var | API endpoint | JSON field | Lockable |
|---------|---------|---------|--------------|------------|----------|
| Usage/capacity below → oversized. <br><em>Expanded: PVC oversized classification threshold (fraction). A PVC is 'oversized' when actual peak usage divided by provisioned capacity is below this value. 0.20 means: if you provisioned 100 GiB but never use more than 20 GiB, the PVC is flagged as oversized and a downsizing recommendation is produced.</em> | 0.20 | `ROS_PVC_OVERSIZED_THRESHOLD` | `/settings/pvc` | oversized_threshold | Yes |
| Usage/capacity above → near-full. <br><em>Expanded: PVC near-full classification threshold (fraction). A PVC is 'near-full' when usage/capacity exceeds this value. 0.85 means: using more than 85% of provisioned storage triggers an expansion warning.</em> | 0.85 | `ROS_PVC_NEAR_FULL_THRESHOLD` | `/settings/pvc` | near_full_threshold | Yes |
| Min days for growth slope. <br><em>Expanded: Minimum days of usage data required before computing a storage growth trend (linear regression slope). Prevents noisy slope estimates from too-short time series. 7 means: at least a week of PVC usage data before projecting future growth.</em> | 7 | `ROS_PVC_MIN_TREND_DAYS` | `/settings/pvc` | min_trend_days | Yes |
| Recommended = max usage × N. <br><em>Expanded: Multiplier for recommended PVC size. When a PVC is oversized, the recommendation is `max_observed_usage × multiplier`. 2 means: recommend provisioning 2× the peak usage, giving 50% headroom for growth.</em> | 2 | `ROS_PVC_RECOMMENDED_SIZE_MULTIPLIER` | `/settings/pvc` | recommended_size_multiplier | Yes |
| Floor (1 GiB). <br><em>Expanded: Minimum recommended PVC size (GiB). No downsizing recommendation will ever suggest less than this. Prevents recommending impractically small volumes. 1 GiB is the minimum.</em> | 1 | `ROS_PVC_MIN_RECOMMENDED_GIB` | `/settings/pvc` | min_recommended_gib | Yes |
| Days-to-full below → alert. <br><em>Expanded: Days-to-full alert window. If the current growth trend projects the PVC filling up within fewer than this many days, a near-full alert is triggered even if current usage hasn't crossed the near-full threshold yet. 30 means: a warning fires if the PVC will fill up within a month at current growth rate.</em> | 30 | `ROS_PVC_DAYS_TO_FULL_ALERT` | `/settings/pvc` | days_to_full_alert | Yes |
| Short-term window. <br><em>Expanded: 7-day window for short-term PVC recommendations. Storage changes slowly, so even the "short" PVC window is a full week—longer than container/GPU short windows.</em> | 7 | `ROS_TERMS_PVC_SHORT_WINDOW_DAYS` | `/settings/terms?recommendation_type=pvc` | terms[].window_days | Yes |
| Short min data days. <br><em>Expanded: At least 3 days of PVC usage data required within the 7-day short window.</em> | 3 | `ROS_TERMS_PVC_SHORT_MIN_DATA_DAYS` | `/settings/terms?recommendation_type=pvc` | terms[].min_data_days | Yes |
| Short decay (0=none). <br><em>Expanded: Decay half-life for short-term PVC window. 0 = no decay for storage data.</em> | 0 | `ROS_TERMS_PVC_SHORT_DECAY_HALFLIFE_HOURS` | `/settings/terms?recommendation_type=pvc` | terms[].decay_halflife_hours | Yes |
| Medium window. <br><em>Expanded: 30-day window for medium-term PVC recommendations. One month of storage usage captures typical growth patterns for databases, logs, and application data.</em> | 30 | `ROS_TERMS_PVC_MEDIUM_WINDOW_DAYS` | `/settings/terms?recommendation_type=pvc` | terms[].window_days | Yes |
| Medium min data. <br><em>Expanded: At least 14 days (2 weeks) of PVC data required within the 30-day medium window. Storage trends need more data than CPU/memory to be reliable.</em> | 14 | `ROS_TERMS_PVC_MEDIUM_MIN_DATA_DAYS` | `/settings/terms?recommendation_type=pvc` | terms[].min_data_days | Yes |
| Medium decay (0=none). <br><em>Expanded: No decay for PVC medium window. Storage usage is cumulative and doesn't benefit from recency weighting as much as compute metrics.</em> | 0 | `ROS_TERMS_PVC_MEDIUM_DECAY_HALFLIFE_HOURS` | `/settings/terms?recommendation_type=pvc` | terms[].decay_halflife_hours | Yes |
| Long window. <br><em>Expanded: 90-day (3-month) window for long-term PVC capacity planning. Captures quarterly storage growth trends for capacity forecasting.</em> | 90 | `ROS_TERMS_PVC_LONG_WINDOW_DAYS` | `/settings/terms?recommendation_type=pvc` | terms[].window_days | Yes |
| Long min data. <br><em>Expanded: Minimum 30 days of PVC data required within the 90-day long window. A full month of data ensures the long-term trend isn't skewed by a single large write event.</em> | 30 | `ROS_TERMS_PVC_LONG_MIN_DATA_DAYS` | `/settings/terms?recommendation_type=pvc` | terms[].min_data_days | Yes |
| Long decay (0=none). <br><em>Expanded: No decay for PVC long window. All days in the 90-day window contribute equally to storage trend analysis.</em> | 0 | `ROS_TERMS_PVC_LONG_DECAY_HALFLIFE_HOURS` | `/settings/terms?recommendation_type=pvc` | terms[].decay_halflife_hours | Yes |

\* Threshold fields via `PUT /settings/pvc` (or thresholds alias). Term windows via `PUT /settings/terms?recommendation_type=pvc`.

See [PVC right-sizing](../features-f27-pvc-rightsizing.md).

---

## ResourceQuota

Namespace **ResourceQuota** recommendations (`quota` plugin). **`GET/PUT/DELETE /settings/quota`**.

| Setting | Default | Env var | API endpoint | JSON field | Lockable |
|---------|---------|---------|--------------|------------|----------|
| Headroom on recommended hard limits (%) <br><em>Buffer above summed container recommendations when proposing ResourceQuota `hard` values. 10% → hard ≈ aggregate × 1.10. Lower = tighter quotas; raise for bursty namespaces. One-cycle lag vs container recs.</em> | 10 | `ROS_QUOTA_HEADROOM_PERCENT` | `/settings/quota` | `headroom_percent` | Yes |
| High risk / raise threshold (%) <br><em>Utilization ≥ this % of hard triggers **`raise`** + high risk (90% default). Lower = earlier warnings before quota admission failures.</em> | 90 | `ROS_QUOTA_HIGH_RISK_THRESHOLD_PERCENT` | `/settings/quota` | `high_risk_threshold_percent` | Yes |
| Medium risk threshold (%) <br><em>Medium risk band between this and high threshold; does not alone trigger `raise`.</em> | 70 | `ROS_QUOTA_MEDIUM_RISK_THRESHOLD_PERCENT` | `/settings/quota` | `medium_risk_threshold_percent` | Yes |

\* Configurable via `PUT /settings/quota` unless the matching `ROS_QUOTA_*` env var is set (field locked).

See [quota-recommendations.md](../features/quota-recommendations.md) for ingestion timing,
one-cycle lag, and API fields.

---

## ClusterResourceQuota

OpenShift **ClusterResourceQuota** recommendations (`cluster-quota` plugin). **`GET/PUT/DELETE /settings/cluster-quota`**.

| Setting | Default | Env var | API endpoint | JSON field | Lockable |
|---------|---------|---------|--------------|------------|----------|
| Headroom on recommended CRQ hard limits (%) <br><em>Same headroom semantics as namespace ResourceQuota but for ClusterResourceQuota across selected projects.</em> | 10 | `ROS_CLUSTER_QUOTA_HEADROOM_PERCENT` | `/settings/cluster-quota` | `headroom_percent` | Yes |
| High risk / raise threshold (%) <br><em>Cluster-wide CRQ utilization % triggering **`raise`** + high risk.</em> | 90 | `ROS_CLUSTER_QUOTA_HIGH_RISK_THRESHOLD_PERCENT` | `/settings/cluster-quota` | `high_risk_threshold_percent` | Yes |
| Medium risk threshold (%) <br><em>Medium utilization band for CRQ; keep below high threshold.</em> | 70 | `ROS_CLUSTER_QUOTA_MEDIUM_RISK_THRESHOLD_PERCENT` | `/settings/cluster-quota` | `medium_risk_threshold_percent` | Yes |

\* Configurable via `PUT /settings/cluster-quota` unless the matching `ROS_CLUSTER_QUOTA_*` env var is set (field locked).

See [cluster-resource-quota.md](../features/cluster-resource-quota.md) for ingestion timing,
one-cycle lag (same as namespace quota), and API fields.

---

## OpenShift Virtualization (VM)

OpenShift Virtualization rightsizing (`vm` plugin). Requires `vm` not in `ROS_DISABLED_PLUGINS` (enabled by default). Tenant thresholds via **`/settings/vm`**; term windows via **`/settings/vm/terms`** (separate from generic `/settings/terms`).

Implementation: [`vm_settings.go`](../../internal/engine/vm_settings.go), [`vm_config.go`](../../internal/engine/vm_config.go), [`handlers_vm_settings.go`](../../internal/api/handlers_vm_settings.go).

### VM thresholds, disk, I/O, stability (`/settings/vm`)

| Setting | Default | Env var | API endpoint | JSON field | Lockable |
|---------|---------|---------|--------------|------------|----------|
| CPU cost percentile <br><em>P95 cost vCPU sizing for batch/dev VMs. Lower = smaller vCPUs, more starvation risk.</em> | 0.95 | `ROS_VM_CPU_PERCENTILE_COST` | `/settings/vm` | `thresholds.cpu_percentile_cost` | Yes |
| CPU performance percentile <br><em>P99 perf vCPU sizing for latency-sensitive guests.</em> | 0.99 | `ROS_VM_CPU_PERCENTILE_PERF` | `/settings/vm` | `thresholds.cpu_percentile_perf` | Yes |
| CPU margin minimum <br><em>Minimum fractional CPU headroom (+15% default); with adaptive margin when enabled.</em> | 0.15 | `ROS_VM_CPU_MARGIN_MIN` | `/settings/vm` | `thresholds.cpu_margin_min` | Yes |
| CPU margin maximum <br><em>Maximum adaptive CPU headroom cap (+50%).</em> | 0.50 | `ROS_VM_CPU_MARGIN_MAX` | `/settings/vm` | `thresholds.cpu_margin_max` | Yes |
| Memory margin minimum <br><em>Minimum memory headroom (+20%); memory bumps costlier than CPU throttling.</em> | 0.20 | `ROS_VM_MEM_MARGIN_MIN` | `/settings/vm` | `thresholds.mem_margin_min` | Yes |
| Downsize hysteresis ratio <br><em>Current must exceed recommended × ratio before downsize (0.60). Raise to reduce flip-flop.</em> | 0.60 | `ROS_VM_DOWNSIZE_HYSTERESIS_RATIO` | `/settings/vm` | `thresholds.downsize_hysteresis_ratio` | Yes |
| Minimum vCPU change <br><em>Suppress recommendations smaller than this vCPU delta.</em> | 2 | `ROS_VM_MIN_VCPU_CHANGE` | `/settings/vm` | `thresholds.min_vcpu_change` | Yes |
| Minimum GiB change <br><em>Suppress memory changes smaller than this GiB delta.</em> | 2 | `ROS_VM_MIN_GIB_CHANGE` | `/settings/vm` | `thresholds.min_gib_change` | Yes |
| Idle CPU (Linux), millicores <br><em>Peak CPU below 50m → idle Linux guest.</em> | 50 | `ROS_VM_IDLE_CPU_MC` | `/settings/vm` | `thresholds.idle_cpu_mc` | Yes |
| Idle memory (Linux), MiB <br><em>Peak memory below 512 MiB → idle Linux guest.</em> | 512 | `ROS_VM_IDLE_MEMORY_MIB` | `/settings/vm` | `thresholds.idle_memory_mib` | Yes |
| Idle CPU (Windows), millicores <br><em>Peak CPU below 200m → idle Windows guest.</em> | 200 | `ROS_VM_IDLE_CPU_MC_WINDOWS` | `/settings/vm` | `thresholds.idle_cpu_mc_windows` | Yes |
| Idle memory (Windows), MiB <br><em>Peak memory below 3072 MiB → idle Windows guest.</em> | 3072 | `ROS_VM_IDLE_MEMORY_MIB_WINDOWS` | `/settings/vm` | `thresholds.idle_memory_mib_windows` | Yes |
| Abandoned VM min days <br><em>Days continuously idle → abandoned classification (3d).</em> | 3 | `ROS_VM_ABANDONED_MIN_DAYS` | `/settings/vm` | `thresholds.abandoned_min_days` | Yes |
| Linux memory floor (GiB) <br><em>Never recommend below 1 GiB RAM (Linux).</em> | 1 | `ROS_VM_LINUX_MEMORY_FLOOR_GIB` | `/settings/vm` | `memory_floors.linux_gib` | Yes |
| Windows memory floor (GiB) <br><em>Never recommend below 2 GiB RAM (Windows).</em> | 2 | `ROS_VM_WINDOWS_MEMORY_FLOOR_GIB` | `/settings/vm` | `memory_floors.windows_gib` | Yes |
| Windows kernel reserve (GiB) <br><em>Extra 1.5 GiB reserved for Windows kernel/drivers in sizing.</em> | 1.5 | `ROS_VM_WINDOWS_KERNEL_RESERVE_GIB` | `/settings/vm` | `memory_floors.windows_kernel_reserve_gib` | Yes |
| Downsize stability days <br><em>Consecutive days below threshold before downsize (anti-flap).</em> | 3 | `ROS_VM_DOWNSIZE_STABILITY_DAYS` | `/settings/vm` | `stability.downsize_stability_days` | Yes |
| Crash-loop restart threshold <br><em>Restart count above this blocks aggressive downsize.</em> | 3 | `ROS_VM_CRASH_LOOP_RESTART_THRESHOLD` | `/settings/vm` | `stability.crash_loop_restart_threshold` | Yes |
| Disk projection window (days) <br><em>Days of disk usage for growth projection (30d).</em> | 30 | `ROS_VM_DISK_PROJECTION_DAYS` | `/settings/vm` | `disk.projection_window_days` | Yes |
| Disk headroom fraction <br><em>Extra capacity above projected peak (+25%).</em> | 0.25 | `ROS_VM_DISK_HEADROOM_PCT` | `/settings/vm` | `disk.headroom_pct` | Yes |
| Disk round step (GiB) <br><em>Round disk recommendations to 10 GiB steps.</em> | 10 | `ROS_VM_DISK_ROUND_STEP_GIB` | `/settings/vm` | `disk.round_step_gib` | Yes |
| Disk min growth (MiB/day) <br><em>Minimum slope to treat disk as growing (100 MiB/day).</em> | 100 | `ROS_VM_DISK_MIN_GROWTH_MIB_PER_DAY` | `/settings/vm` | `disk.min_growth_mib_per_day` | Yes |
| High IOPS threshold <br><em>Average IOPS above 3000 flags IO-sensitive VM.</em> | 3000 | `ROS_VM_HIGH_IOPS_THRESHOLD` | `/settings/vm` | `io.high_iops_threshold` | Yes |
| Sequential I/O threshold (bytes) <br><em>Peak BPS/IOPS average at or above 64 KiB → sequential pattern.</em> | 65536 | `ROS_VM_IO_SEQUENTIAL_THRESHOLD_BYTES` | `/settings/vm` | `io.sequential_threshold_bytes` | Yes |
| Random I/O threshold (bytes) <br><em>Peak BPS/IOPS average below 16 KiB → random pattern.</em> | 16384 | `ROS_VM_IO_RANDOM_THRESHOLD_BYTES` | `/settings/vm` | `io.random_threshold_bytes` | Yes |
| Min IOPS for I/O classification <br><em>Combined peak IOPS below 100 → low-io pattern.</em> | 100 | `ROS_VM_IO_MIN_IOPS_CLASSIFICATION` | `/settings/vm` | `io.min_iops_for_classification` | Yes |
| Instance type matching <br><em>Map vCPU/RAM to nearest instance type when true.</em> | true | `ROS_VM_ENABLE_INSTANCE_TYPE_MATCHING` | `/settings/vm` | `instance_type_matching` | Yes |
GET also returns top-level `enabled` (derived from plugin registry — `vm` in `ROS_ENABLED_PLUGINS`/`ROS_DISABLED_PLUGINS`; not stored per tenant). PUT accepts partial JSON objects for `thresholds`, `memory_floors`, `stability`, `disk`, `io`, and `instance_type_matching`.

### VM term windows (`/settings/vm/terms`)

Plugin defaults (`internal/plugins/vm/plugin.go`): `short_term` 7d/3 min-data, `medium_term` 15d/7, `long_term` 30d/15 (no decay on VM terms by default).

| Setting | Default (short / medium / long) | Env var pattern | API endpoint | JSON field | Lockable |
|---------|--------------------------------|-----------------|--------------|------------|----------|
| Window days <br><em>VM horizons 7/15/30d—longer than containers.</em> | 7 / 15 / 30 | `ROS_TERMS_VM_SHORT_TERM_WINDOW_DAYS`, `ROS_TERMS_VM_MEDIUM_TERM_WINDOW_DAYS`, `ROS_TERMS_VM_LONG_TERM_WINDOW_DAYS` | `/settings/vm/terms` | `terms[].window_days` | Yes |
| Min data days <br><em>Min telemetry days 3/7/15 per VM horizon.</em> | 3 / 7 / 15 | `ROS_TERMS_VM_*_MIN_DATA_DAYS` | `/settings/vm/terms` | `terms[].min_data_days` | Yes |
| Decay half-life (hours) <br><em>Default 0/0/0; set decay to weight recent VM usage.</em> | 0 / 0 / 0 | `ROS_TERMS_VM_*_DECAY_HALFLIFE_HOURS` | `/settings/vm/terms` | `terms[].decay_halflife_hours` | Yes |
Term names in PUT body: `short_term`, `medium_term`, `long_term`. Locked when any env var is set for that term or when `ROS_SETTINGS_LOCKED_VM=true` under global lock (generic `ROS_SETTINGS_LOCKED_TERMS` does **not** apply to this route).

### VM — admin-only (no Settings API field)

| Setting | Default | Env var | API endpoint | JSON field | Lockable |
|---------|---------|---------|--------------|------------|----------|
| ~~Enable VM recommendations~~ | — | ~~`ROS_ENABLE_VM_RECS`~~ | — | — | — | Removed; VM plugin controlled by `ROS_ENABLED_PLUGINS`/`ROS_DISABLED_PLUGINS` |
| CPU adaptive margin (cost engine) <br><em>Variance-based CPU margin between min/max when true.</em> | true | `ROS_VM_CPU_ADAPTIVE_MARGIN_ENABLED` | `PUT /settings/vm` | `cpu_adaptive_margin_enabled` | Yes |
| VM recommendation history retention (days) <br><em>VM-specific history retention (90d). Read-only in GET /settings/vm.</em> | 90 | `ROS_VM_REC_HISTORY_RETENTION_DAYS` | `GET /settings/vm` | `history_retention_days` | No |
| VM GPU time-slice min replicas <br><em>Minimum `recommended_time_slice_count` when time-slicing is advised.</em> | 2 | `ROS_VM_GPU_TIMESLICE_MIN_REPLICAS` | `PUT /settings/vm` | `gpu.gpu_timeslice_min_replicas` | Yes |
| VM GPU time-slice max replicas <br><em>Upper cap on slice count; reduced when DRAM exceeds penalty threshold.</em> | 16 | `ROS_VM_GPU_TIMESLICE_MAX_REPLICAS` | `PUT /settings/vm` | `gpu.gpu_timeslice_max_replicas` | Yes |
| VM GPU time-slice FB safety (basis points) <br><em>Do not recommend time-slicing when FB fraction ≥ this (8000 = 80%).</em> | 8000 | `ROS_VM_GPU_TIMESLICE_FB_SAFETY_BP` | `PUT /settings/vm` | `gpu.gpu_timeslice_fb_safety_threshold_bp` | Yes |
| VM GPU time-slice DRAM penalty (basis points) <br><em>When DRAM ≥ this, max replicas is halved (5000 = 50%).</em> | 5000 | `ROS_VM_GPU_TIMESLICE_DRAM_PENALTY_BP` | `PUT /settings/vm` | `gpu.gpu_timeslice_dram_penalty_threshold_bp` | Yes |
| VM GPU idle threshold (basis points) <br><em>vGPU SM below 5% → idle.</em> | 500 | `ROS_VM_GPU_IDLE_THRESHOLD` | `PUT /settings/vm` | `gpu.idle_threshold_bp` | Yes |
| VM GPU underutilized threshold (basis points) <br><em>vGPU SM below 30% → underutilized.</em> | 3000 | `ROS_VM_GPU_UNDERUTIL_THRESHOLD` | `PUT /settings/vm` | `gpu.underutil_threshold_bp` | Yes |
| VM GPU frame-buffer saturation (MiB) <br><em>FB usage above this → memory saturated; 0 auto-detects 90% of catalog GPU memory.</em> | 0 | `ROS_VM_GPU_FB_SATURATION_MIB` | `PUT /settings/vm` | `gpu.fb_saturation_mib` | Yes |
| VM GPU compute saturation threshold (basis points) <br><em>vGPU SM at or above 85% → compute saturated.</em> | 8500 | `ROS_VM_GPU_COMPUTE_SATURATION_THRESHOLD` | `PUT /settings/vm` | `gpu.compute_saturation_threshold_bp` | Yes |
| Network throughput P95 threshold (bytes/sec) <br><em>Sustained aggregate rx+tx above this → network-optimized series (62_500_000 ≈ 500 Mbps).</em> | 62500000 | `ROS_VM_NETWORK_THROUGHPUT_THRESHOLD_BPS` | `PUT /settings/vm` | `network.throughput_threshold_bps` | Yes |
| Network PPS P95 threshold <br><em>Used with drop ratio for alternate network-bound path.</em> | 100000 | `ROS_VM_NETWORK_PPS_THRESHOLD` | `PUT /settings/vm` | `network.pps_threshold` | Yes |
| Network drop ratio (basis points) <br><em>Max daily drop ratio must exceed this (10 = 0.1%) with high PPS.</em> | 10 | `ROS_VM_NETWORK_DROP_RATIO_BP` | `PUT /settings/vm` | `network.drop_ratio_bp` | Yes |
| Network sustained days <br><em>Days in term window meeting throughput or PPS+drop criteria.</em> | 7 | `ROS_VM_NETWORK_SUSTAINED_DAYS` | `PUT /settings/vm` | `network.sustained_days` | Yes |
| Enable n1 network series matching <br><em>When false, n1 types are skipped; falls back to u1.</em> | true | `ROS_VM_ENABLE_NETWORK_SERIES` | `PUT /settings/vm` | `network.enable_network_series` | Yes |
| Enable placement checks <br><em>Same-node redundancy and node skew notifications (60–61).</em> | true | `ROS_VM_ENABLE_PLACEMENT_CHECKS` | `PUT /settings/vm` | `placement.enable_placement_checks` | Yes |
| Placement skew ratio <br><em>Max:min VM count per node for profile group before skew notification (61).</em> | 3 | `ROS_VM_PLACEMENT_SKEW_RATIO` | `PUT /settings/vm` | `placement.placement_skew_ratio` | Yes |
| Enable shared PVC correlation <br><em>Correlated profile peers in namespace (62) until per-VM PVC column exists.</em> | true | `ROS_VM_ENABLE_SHARED_PVC_CORRELATION` | `PUT /settings/vm` | `placement.enable_shared_pvc_correlation` | Yes |
| NUMA node memory (GiB) <br><em>Fallback per-NUMA cap for notification **63** when `daily_node_digests` has no allocatable memory for the VM's node.</em> | 64 | `ROS_VM_NUMA_NODE_MEMORY_GIB` | `PUT /settings/vm` | `placement.numa_node_memory_gib` | Yes |
| NUMA assumed sockets <br><em>Divides node allocatable memory from `daily_node_digests` for per-NUMA estimate (default 2).</em> | 2 | `ROS_VM_NUMA_ASSUMED_SOCKETS` | `PUT /settings/vm` | `placement.numa_assumed_sockets` | Yes |
| Enable power-off scheduling <br><em>Detect periodically idle VMs (notification 64).</em> | true | `ROS_VM_ENABLE_POWER_SCHEDULE` | `PUT /settings/vm` | `power_schedule.enabled` | Yes |
| Power-off min idle days <br><em>Minimum digest history for power-off candidate detection.</em> | 14 | `ROS_VM_POWER_OFF_MIN_IDLE_DAYS` | `PUT /settings/vm` | `power_schedule.min_idle_days` | Yes |
| Power-off idle ratio threshold <br><em>Fraction of observed days that must be idle (requires some active days).</em> | 0.7 | `ROS_VM_POWER_OFF_IDLE_RATIO_THRESHOLD` | `PUT /settings/vm` | `power_schedule.idle_ratio_threshold` | Yes |
| Enable network QoS hints <br><em>SR-IOV/DPDK notifications (65–66) for network-bound VMs.</em> | true | `ROS_VM_NETWORK_QOS_ENABLED` | `PUT /settings/vm` | `network_qos.enabled` | Yes |
| SR-IOV drop ratio threshold <br><em>Packet drop ratio (0–1) to suggest SR-IOV.</em> | 0.01 | `ROS_VM_NETWORK_QOS_SRIOV_DROP_THRESHOLD` | `PUT /settings/vm` | `network_qos.sriov_drop_threshold` | Yes |
| SR-IOV throughput threshold (bps) <br><em>Sustained throughput to suggest SR-IOV without drops.</em> | 5000000000 | `ROS_VM_NETWORK_QOS_SRIOV_THROUGHPUT_BPS` | `PUT /settings/vm` | `network_qos.sriov_throughput_bps` | Yes |
| DPDK PPS threshold <br><em>Packets/sec for small-packet DPDK hint.</em> | 500000 | `ROS_VM_NETWORK_QOS_DPDK_PPS_THRESHOLD` | `PUT /settings/vm` | `network_qos.dpdk_pps_threshold` | Yes |
| Enable storage tiering hints <br><em>Cold/IOPS/throughput notifications (67–69) from multi-day I/O patterns.</em> | true | `ROS_VM_STORAGE_TIERING_ENABLED` | `PUT /settings/vm` | `storage_tiering.enabled` | Yes |
| Storage tiering min days <br><em>Minimum digest history before tier hints run.</em> | 7 | `ROS_VM_STORAGE_TIERING_MIN_DAYS` | `PUT /settings/vm` | `storage_tiering.min_days` | Yes |
| Storage tiering cold min days <br><em>Low-io days to suggest lower-cost tier.</em> | 14 | `ROS_VM_STORAGE_TIERING_COLD_MIN_DAYS` | `PUT /settings/vm` | `storage_tiering.cold_min_days` | Yes |
| Storage tiering IOPS min days <br><em>Random high-IOPS days for IOPS-optimized hint.</em> | 7 | `ROS_VM_STORAGE_TIERING_IOPS_MIN_DAYS` | `PUT /settings/vm` | `storage_tiering.iops_min_days` | Yes |
| Storage tiering throughput min days <br><em>Sequential high-throughput days for throughput hint.</em> | 7 | `ROS_VM_STORAGE_TIERING_THROUGHPUT_MIN_DAYS` | `PUT /settings/vm` | `storage_tiering.throughput_min_days` | Yes |
| Storage tiering high IOPS threshold <br><em>Peak daily read+write IOPS for random high-IOPS day.</em> | 5000 | `ROS_VM_STORAGE_TIERING_HIGH_IOPS_THRESHOLD` | `PUT /settings/vm` | `storage_tiering.high_iops_threshold` | Yes |
| Storage tiering high throughput (bps) <br><em>Peak daily read+write BPS for sequential high-throughput day.</em> | 104857600 | `ROS_VM_STORAGE_TIERING_HIGH_THROUGHPUT_BPS` | `PUT /settings/vm` | `storage_tiering.high_throughput_bps` | Yes |

**VM GPU catalogs (not Settings API fields):** MIG sizing for VMs and containers both use embedded [`gpu_catalog.yaml`](../../internal/engine/gpu_catalog.yaml). **vGPU profile names** (`recommended_vgpu_profile`, notification **56**) come from [`vgpu_profiles.yaml`](../../internal/engine/vgpu_profiles.yaml), which is **VM-only** — the container `gpu` plugin never loads it. Container time-slicing exposes integer replica counts only (node `nvidia.com/gpu.replicas`); VM time-slicing adds optional `grid_*` C-series profile hints. See [GPU sharing by workload type](../design/vm-recommendations.md#gpu-sharing-mechanisms-by-workload-type).

See [VM recommendations design](../design/vm-recommendations.md).

---

## Snapshot

VolumeSnapshot staleness classification. Snapshot thresholds are tenant-configurable
via `GET/PUT /settings/snapshot` (tier 2) or admin env vars (tier 1).

| Setting | Default | Env var | API endpoint | JSON field | Lockable |
|---------|---------|---------|--------------|------------|----------|
| Source PVC gone + age > N → orphaned. <br><em>Expanded: A snapshot becomes "orphaned" when the PersistentVolumeClaim (PVC) it was created from no longer exists, and the snapshot is older than this many days. Orphaned snapshots consume storage but protect data from a deleted volume—they may be safe to remove if the deletion was intentional. 7 days gives time to restore before flagging.</em> | 7 | `ROS_SNAPSHOT_ORPHAN_AGE_DAYS` | `/settings/snapshot` | orphan_age_days | Yes |
| Age > N, 0 restores → never restored. <br><em>Expanded: Days since creation without any restore event before flagging as 'never restored'. Snapshot restores are tracked by the koku-metrics-operator which monitors VolumeSnapshot and VolumeSnapshotContent resources on the cluster. If a snapshot exists for longer than this and has never been used to create a new PVC (restore count = 0), it may be unnecessary.</em> | 30 | `ROS_SNAPSHOT_NEVER_RESTORED_DAYS` | `/settings/snapshot` | never_restored_days | Yes |
| Age > N → stale. <br><em>Expanded: A snapshot is classified as "stale" when it is older than this many days, regardless of whether its source PVC still exists. Stale snapshots may contain outdated data that is no longer useful for recovery. 90 days (3 months) is the default threshold before recommending cleanup.</em> | 90 | `ROS_SNAPSHOT_STALE_DAYS` | `/settings/snapshot` | stale_days | Yes |
| > N per PVC → redundant. <br><em>Expanded: Maximum number of snapshots per PVC before older ones are flagged as redundant. If a PVC has more than 3 snapshots, the excess are likely unnecessary—keeping the 2-3 most recent is usually sufficient for recovery. Redundant snapshots waste storage and increase backup costs.</em> | 3 | `ROS_SNAPSHOT_REDUNDANT_THRESHOLD` | `/settings/snapshot` | redundant_threshold | Yes |
| Fallback $/GiB/month. <br><em>Expanded: Fallback storage cost rate (USD per GiB per month) used when no cost model rate is available from Koku. This is used to estimate the monthly cost of keeping a snapshot. The resolution chain is: Koku effective-rates `storage_gb_usage_per_month` (dynamic) → tenant DB override → this env var → $0.05 default. Set this to match your actual block storage provider's snapshot pricing.</em> | 0.05 | `ROS_SNAPSHOT_COST_PER_GIB_MONTH` | `/settings/snapshot` | cost_per_gib_month_usd | Yes |
| Inventory freshness window (hours) <br><em>Classification and reconciliation only consider `snapshot_inventory` rows ingested within this many hours. Shorter windows react faster to operator gaps; longer windows tolerate delayed uploads.</em> | 6 | `ROS_SNAPSHOT_INVENTORY_FRESH_HOURS` | `/settings/snapshot` | inventory_fresh_hours | Yes |

### Snapshot — admin-only (no Settings API field)

| Setting | Default | Env var | API endpoint | JSON field | Lockable |
|---------|---------|---------|--------------|------------|----------|
| Snapshot inventory retention (hours) <br><em>Retain raw inventory rows in DB (48h)—ops tuning, not classification thresholds.</em> | 48 | `ROS_SNAPSHOT_INVENTORY_RETENTION_HOURS` | — | — | No |
| Snapshot stale grace without fresh inventory (hours) <br><em>Defer stale classification this long when inventory ingest is down (48h).</em> | 48 | `ROS_SNAPSHOT_STALE_GRACE_HOURS` | — | — | No |

\* Tenant fields via **`PUT /settings/snapshot`** unless the matching env var is set.

See [Snapshot staleness](../features-f-snapshot-staleness.md).

---

## Idle / zombie detection

Inline idle and zombie classification during container/GPU recommendation runs.

**API:** `GET/PUT/DELETE /settings/idle-detection` — body `{"idle_detection":{...}}`.

| Setting | Default | Env var | API endpoint | JSON field | Lockable |
|---------|---------|---------|--------------|------------|----------|
| Idle detection enabled <br><em>Inline idle/zombie during container/GPU runs.</em> | true | `ROS_IDLE_DETECTION_ENABLED` | `/settings/idle-detection` | `idle_detection.enabled` | Yes |
| CPU utilization % (idle) <br><em>Peak CPU % of request below 2% → idle.</em> | 2 | `ROS_IDLE_CPU_UTILIZATION_PCT` | `/settings/idle-detection` | `idle_detection.thresholds.cpu_utilization_percent` | Yes |
| Memory utilization % (idle) <br><em>Peak memory % of request below 5% → idle.</em> | 5 | `ROS_IDLE_MEMORY_UTILIZATION_PCT` | `/settings/idle-detection` | `idle_detection.thresholds.memory_utilization_percent` | Yes |
| Burst ratio (stay active) <br><em>peak/p95 above 10× keeps workload active despite low average.</em> | 10 | `ROS_IDLE_BURST_RATIO` | `/settings/idle-detection` | `idle_detection.thresholds.burst_ratio` | Yes |
| Min observation days <br><em>Minimum 14d data before idle classification.</em> | 14 | `ROS_IDLE_MIN_OBSERVATION_DAYS` | `/settings/idle-detection` | `idle_detection.thresholds.minimum_observation_days` | Yes |
| GPU SM active (basis points) <br><em>GPU idle if SM below 500 bp (5%).</em> | 500 | `ROS_IDLE_GPU_SM_ACTIVE_BP` | `/settings/idle-detection` | `idle_detection.thresholds.gpu_sm_active_basis_points` | Yes |
| GPU DRAM active (basis points) <br><em>GPU idle if DRAM below 500 bp (5%).</em> | 500 | `ROS_IDLE_GPU_DRAM_ACTIVE_BP` | `/settings/idle-detection` | `idle_detection.thresholds.gpu_dram_active_basis_points` | Yes |
| Exclude namespaces (CSV globs) <br><em>Globs never classified idle (`kube-system`, `openshift-*`).</em> | `kube-system,openshift-*` | `ROS_IDLE_EXCLUDE_NAMESPACES` | `/settings/idle-detection` | `idle_detection.exclusions.namespaces` | Yes |
| Exclude workload types (CSV) <br><em>Kinds excluded (`DaemonSet` default).</em> | `DaemonSet` | `ROS_IDLE_EXCLUDE_WORKLOAD_TYPES` | `/settings/idle-detection` | `idle_detection.exclusions.workload_types` | Yes |
### Idle — admin-only (env overrides, not in PUT body)

| Setting | Default | Env var | API endpoint | JSON field | Lockable |
|---------|---------|---------|--------------|------------|----------|
| Zombie CPU P95 (millicores) <br><em>Zombie if P95 CPU below 1m (stricter than idle).</em> | 1 | `ROS_IDLE_ZOMBIE_CPU_MILLICORES` | — | — | No |
| Zombie CPU peak (millicores) <br><em>Zombie if peak CPU below 10m.</em> | 10 | `ROS_IDLE_ZOMBIE_PEAK_MILLICORES` | — | — | No |
See [Idle / zombie detection](../features/idle-detection.md).

---

## Business Hours

**API routes:** `/settings/business-hours`, `/settings/business-hours/clusters/:cluster_id`, `/settings/business-hours/clusters/:cluster_id/namespaces/:namespace` — `GET`/`PUT`/`DELETE`.

| Setting | Default | Env var | API endpoint | JSON field | Lockable |
|---------|---------|---------|--------------|------------|----------|
| Business hours feature <br><em>Platform gate for schedule-based usage weighting during ingest. `false` = 24/7 averages only.</em> | true | `ROS_BUSINESS_HOURS_ENABLED` | — (gates routes) | `enabled` on GET when disabled | No |
| Reship forward-only fallback <br><em>When BH history is missing, reship backfill only forward from gap start if `true` (cheaper, may leave early gap empty).</em> | false | `ROS_BUSINESS_HOURS_RESHIP_FORWARD_ONLY_FALLBACK` | — | — | No |
| Timezone <br><em>IANA timezone for schedule boundaries (org/cluster/namespace override). Wrong TZ shifts which hours count as business vs off-hours.</em> | tenant | — | `/settings/business-hours*` | `timezone` | Global |
| Schedule days <br><em>Weekdays in business window; off-hours use `off_hours_weight`.</em> | tenant | — | same | `schedule.days` | Global |
| Schedule start / end <br><em>Local start/end times in tenant timezone (e.g. 09:00–17:00).</em> | tenant | — | same | `schedule.start_time`, `schedule.end_time` | Global |
| Off-hours weight <br><em>Multiplier for samples outside schedule (0.2 = 20% weight). 1.0 ≈ 24/7 behavior.</em> | tenant | — | same | `off_hours_weight` | Global |
| Schedule enabled (scope) <br><em>Per org/cluster/namespace toggle in PUT body.</em> | tenant | — | same | `enabled` | Global |

`Global` under `ROS_SETTINGS_LOCKED` + `ROS_SETTINGS_LOCKED_BUSINESS_HOURS` (default true): PUT/DELETE return `403`; GET returns `enabled: false`, `settings_locked: true`.

Admin guide: [Business Hours](../business-hours-admin-guide.md).

---

## Reship

Internal reship poller for business-hours historical data backfill. Admin-only.

| Setting | Default | Env var | API endpoint | JSON field | Lockable |
|---------|---------|---------|--------------|------------|----------|
| Retry interval. <br><em>Expanded: How often (in seconds) the reship poller checks for pending reship requests and retries failed ones. The reship poller manages backfill of historical usage data needed for business-hours analysis. 60 seconds means failed reship attempts are retried every minute until success or max retries.</em> | 60 | `ROS_RESHIP_POLLER_INTERVAL_SECS` | — | — | No |
| Max failures. <br><em>Expanded: Maximum number of retry attempts for a failed reship request before it is abandoned. After this many failures, the reship request is marked as permanently failed and business-hours recommendations for that time range will use whatever data is available (possibly with reduced confidence). Prevents infinite retry loops against unreachable clusters.</em> | 10 | `ROS_RESHIP_MAX_RETRIES` | — | — | No |

Prometheus: duplicate reship triggers coalesced while in-flight — `rosocp_reship_coalesced_total{org_id}`.

---

## Threshold Recalculation

Async re-recommendation after tenant threshold changes (Settings API PUT). Unlike business
hours, thresholds only change how existing digests are interpreted—no masu/Kafka/S3
backfill is required.

| Setting | Default | Env var | API endpoint | JSON field | Lockable |
|---------|---------|---------|--------------|------------|----------|
| Kill-switch for async recalculation after threshold PUT. <br><em>Expanded: When true (default), each successful threshold settings PUT triggers background re-recommendation for all clusters in the org (bounded to 3 concurrent clusters). When false, settings are saved and the threshold cache is invalidated, but existing recommendations are not recomputed until the next ingestion cycle.</em> | true | `ROS_THRESHOLD_RECALCULATION_ENABLED` | — | — | No |

Prometheus: `ros_threshold_recalculation_total{org_id,recommendation_type,status}`. Coalesced duplicate triggers: `rosocp_threshold_recalc_coalesced_total{org_id,recommendation_type}`.

---

## Savings / Cost

Dollar estimate integration with Koku Masu `effective_rates`. See
[Cost Integration](cost-integration.md) for formulas and plugin matrix.

| Setting | Default | Env var | API endpoint | JSON field | Lockable |
|---------|---------|---------|--------------|------------|----------|
| Kill-switch. <br><em>Expanded: Gates dollar-value savings estimates on recommendations. When enabled, container, node, PVC, snapshot, and VM plugins persist monthly savings (or recoverable cost for snapshots) from Koku `effective_rates`. When disabled, resource recommendations still run but dollar fields are null/zero (containers/nodes/PVCs may include notification code **25**; VM list/detail `savings` is always `null`).</em> | true | `ROS_SAVINGS_ESTIMATES_ENABLED` | — | — | No |
| Savings recalculation after cost model change. <br><em>Expanded: When true (default), ROS accepts `POST /api/cost-management/v1/internal/recalculate-savings` (service-account auth, same as tag sync). Koku calls this after [`update_summary_cost_model_costs`](https://github.com/project-koku/koku/blob/main/koku/masu/processor/ocp/ocp_cost_model_cost_updater.py) to refresh persisted `estimated_savings_cents` without re-ingestion. Requires `ROS_SAVINGS_ESTIMATES_ENABLED` and `KOKU_MASU_URL`. When false, savings update only on the next ingestion cycle.</em> | true | `ROS_SAVINGS_RECALCULATION_ENABLED` | — | — | No |
| Koku masu base URL. <br><em>Expanded: Base URL of the Koku Masu service used to fetch effective cost model rates (CPU, memory, storage, node, VM, GPU pricing). Masu provides the `effective_rates` API that ROS uses for `savings` on VM recommendations and fleet `by_plugin.vm`. Must point to a reachable Masu instance (e.g., `http://masu-server:5042`). Empty skips dynamic rate lookup.</em> | (empty) | `KOKU_MASU_URL` | — | — | No |

### Koku → ROS savings recalculation (not ROS env vars)

Configure on the **Koku** worker / masu deployment so rate changes trigger ROS recalculation:

| Variable | Default | Description |
|----------|---------|-------------|
| `ROS_API_HOST` | (unset) | ROS API hostname. No callback when unset unless `ROS_OCP_BACKEND_URL` is set. |
| `ROS_API_PORT` | `8000` | ROS API port when using `ROS_API_HOST`. |
| `ROS_OCP_BACKEND_URL` | `http://cost-onprem-ros-api:8000` | Fallback base URL (on-prem chart). |
| `ROS_SERVICE_TOKEN` | (unset) | Optional bearer token for `POST /internal/recalculate-savings`; otherwise Koku uses the projected service account token. |

See [Cost Integration — Savings recalculation](cost-integration.md#savings-recalculation-after-cost-model-changes).

Prometheus: `ros_savings_recalculation_total{org_id,recommendation_type,status}`; coalesced duplicate triggers — `rosocp_savings_recalc_coalesced_total{org_id}`. Effective-rates cache capacity (`ROS_COST_CACHE_MAX_ENTRIES`): `rosocp_cost_cache_size`, `rosocp_cost_cache_evictions_total`.

---

## Cache and coalescing observability

Tune in-memory caches and observe duplicate async-job suppression via Prometheus (full catalog in [Monitoring](../operations/monitoring.md)):

| Configuration | Observable metrics |
|---------------|-------------------|
| `ROS_RBAC_CACHE_MAX_ENTRIES` | `rosocp_rbac_cache_size`, `rosocp_rbac_cache_evictions_total` |
| `ROS_COST_CACHE_MAX_ENTRIES` | `rosocp_cost_cache_size`, `rosocp_cost_cache_evictions_total` |
| `ROS_FLEET_SUMMARY_CACHE_CAPACITY` | `rosocp_fleet_summary_cache_size`, `rosocp_fleet_summary_cache_hits_total`, `rosocp_fleet_summary_cache_misses_total`, `rosocp_fleet_summary_cache_evictions_total`, `rosocp_fleet_summary_cache_invalidations_total`, `rosocp_fleet_summary_cache_lazy_expiry_total`, `rosocp_savings_summary_cache_size`, `rosocp_savings_summary_cache_hits_total`, `rosocp_savings_summary_cache_misses_total`, `rosocp_savings_summary_cache_evictions_total`, `rosocp_savings_summary_cache_invalidations_total`, `rosocp_savings_summary_cache_lazy_expiry_total` |
| Threshold recalc coalescing | `rosocp_threshold_recalc_coalesced_total` |
| Savings recalc coalescing | `rosocp_savings_recalc_coalesced_total` |
| Reship coalescing | `rosocp_reship_coalesced_total` |

---

## List API — Node recommendations

`GET /api/cost-management/v1/recommendations/openshift/nodes`

### Response fields (per node object)

| Field | Type | Description |
|-------|------|-------------|
| `pod_capacity` | int, `omitempty` | Maximum schedulable pods on the node (from operator CSV / `daily_node_digests`). Omitted when unknown. |
| `pod_scheduling_headroom` | float, `omitempty` | Ratio of available pod slots: `(pod_capacity − pod_count) / pod_capacity` when capacity is known (0.0–1.0). Omitted when capacity unknown. |

### Filters

| Query param | Values | Description |
|-------------|--------|-------------|
| `filter[stranded_resource]` | `cpu`, `memory`, `none` | Filter by stranded resource classification (`none` = no stranded resource). |
| `filter[instance_type]` | string | Filter by node instance type label (exact match). |
| `filter[machineset_name]` | string | Filter by OpenShift MachineSet name (exact match). |

Also supports standard list params (`limit`, `offset`, `after`, `filter[cluster]`, `filter[engine]`,
`filter[idle_state]`, `format=csv`, etc.). Implementation:
[`handlers_node_utilization.go`](../../internal/api/handlers_node_utilization.go).

### Node notification codes (reference)

| Code | Constant | Description |
|------|----------|-------------|
| 11 | `NotifNodeUnderutilized` | CPU and memory P95 below `underutil_threshold` |
| 12 | `NotifNodeOvercommitted` | Pod CPU requests exceed `overcommit_threshold` × allocatable |
| 13 | `NotifStrandedResources` | CPU/memory imbalance above `stranded_imbalance_threshold` |
| 15 | `NotifNodeIdle` | Node idle or zombie (`idle_state`); DB name `NODE_IDLE` (migration 000121) |
| 25 | `NotifNoCostData` | Savings could not be computed (Masu rates missing) |
| 74 | `NotifNodePodSchedulingLimit` | Pod scheduling headroom below `pod_headroom_notification_threshold` (default 10%) |
| 75 | — | Reserved for Tier 3 MachineAutoscaler `minReplicas` recommendations |

Full catalog: [Notification codes](notification-codes.md).

---

## List API — Fleet savings summary

`GET /api/cost-management/v1/recommendations/openshift/savings-summary`

| Query param | Values | Default | Description |
|-------------|--------|---------|-------------|
| `term` | `short`, `medium`, `long` | `medium` | Which recommendation term window to aggregate for fleet savings. |

Also supports `engine` (`cost` / `performance`), `group_by[tag:key]`, `group_by[idle_state]`, and
cluster filters. Default rollup responses (no `group_by`) are cached in memory with the same TTL and
invalidation as fleet summary (`ROS_FLEET_SUMMARY_CACHE_TTL`). Implementation: [`handlers_savings_summary.go`](../../internal/api/handlers_savings_summary.go).

---

## Recommended Values by Use Case

These are starting points for tuning. Validate against your workload profiles before
applying in production.

### Aggressive cost optimization

Maximize rightsizing and deallocation recommendations. Accept higher risk of
occasional CPU throttling or OOM under burst load.

| Parameter | Suggested value | Rationale |
|-----------|-----------------|-----------|
| `ROS_CONTAINER_CPU_COST_PERCENTILE` | 0.50 | Lower percentile → smaller CPU requests |
| `ROS_CONTAINER_MEM_COST_PERCENTILE` | 0.90 | Slightly below default P95 |
| `ROS_CONTAINER_IDLE_CPU_THRESHOLD_MC` | 15 | Classify more containers as idle |
| `ROS_CONTAINER_IDLE_MEM_THRESHOLD_KIB` | 20480 | 20 MiB idle memory ceiling |
| `ROS_NODE_COST_TARGET_UTILIZATION` | 0.85 | Target higher node utilization |
| `ROS_NODE_UNDERUTIL_THRESHOLD` | 0.25 | Flag underutilized nodes sooner |

### Conservative / stability-first

Prioritize headroom and performance engine recommendations. Fewer aggressive
downsizes; higher percentiles and tighter margins.

| Parameter | Suggested value | Rationale |
|-----------|-----------------|-----------|
| `ROS_CONTAINER_CPU_PERF_PERCENTILE` | 0.99 | Near-peak CPU coverage |
| `ROS_CONTAINER_MEM_PERF_PERCENTILE` | 1.0 | Max observed memory (default) |
| `ROS_CONTAINER_MIN_MARGIN` | 1.25 | Wider safety margin floor |
| `ROS_CONTAINER_MAX_MARGIN` | 1.60 | Allow larger adaptive margins |
| `ROS_NODE_PERF_TARGET_UTILIZATION` | 0.50 | More headroom on performance engine |
| `ROS_NODE_PERF_CONSOLIDATION_HEADROOM_MULTIPLIER` | 2.5 | Require more waste before perf consolidation |

### GPU training workloads

Training jobs have long warmup phases and bursty SM utilization. Default idle
thresholds may misclassify warming-up GPUs as idle.

| Parameter | Suggested value | Rationale |
|-----------|-----------------|-----------|
| `ROS_GPU_IDLE_THRESHOLD` | 0.05 | Tolerate low SM during warmup |
| `ROS_GPU_SPIKE_RATIO_THRESHOLD` | 8.0 | Reduce false bursty classification |
| `ROS_GPU_CONFIDENCE_DAYS_TIER3` | 21 | Require more data before high confidence |
| `ROS_TERMS_GPU_MEDIUM_WINDOW_DAYS` | 14 | Longer window captures full training cycles |

### Batch / HPC storage

Pre-provisioned PVC capacity is normal; default oversized threshold (20%) flags
too many volumes as oversized.

| Parameter | Suggested value | Rationale |
|-----------|-----------------|-----------|
| `ROS_PVC_OVERSIZED_THRESHOLD` | 0.40 | Allow 40% utilization before oversized |
| `ROS_PVC_RECOMMENDED_SIZE_MULTIPLIER` | 3 | Larger headroom for burst writes |
| `ROS_TERMS_PVC_LONG_WINDOW_DAYS` | 120 | Slow growth needs longer observation |

---

## Future Enhancement: Generic Settings Resolver

> **Status:** Not implemented — documented as Option B for a future cleanup PR.
> Do not mix this refactor with feature work.

### Problem

Each plugin implements its own resolve path: load compiled defaults → overlay
tenant values from the database → re-apply admin env locks on read. The algorithm
is correct but scattered. Every new settings type must remember to add its own
env-lock-on-read step after the DB overlay. Forgetting that step caused an
inconsistency bug where VM and quota settings could return stale tenant overrides
even when the corresponding `ROS_*` env var was set (platform lock not enforced on
read).

### Proposed Solution (Option B)

A generic `ResolveSettings[T]` function using Go generics that encapsulates the
full three-tier resolution for any settings struct:

```go
func ResolveSettings[T any](
    ctx context.Context,
    pool *pgxpool.Pool,
    orgID, recType string,
    defaults T,
    lockMap map[string]string,
) (T, []string, error) {
    result := defaults
    if err := overlayFromDB(ctx, pool, orgID, recType, &result); err != nil {
        return result, nil, err
    }
    locked := applyEnvLocks(&result, lockMap)
    return result, locked, nil
}
```

`applyEnvLocks` would use reflection or a field-tag-based approach to:

1. Iterate the lock map (`env var name` → `struct field name`)
2. For each env var that is present (`os.LookupEnv`), override the corresponding
   struct field with the parsed config value
3. Return the list of locked field names (for Settings API `"locked": true` responses)

Plugins would register only `defaults` + `lockMap`; the resolver owns the algorithm.

### Benefits

- **Impossible to forget env re-application** — it is built into the resolver, not
  duplicated per plugin
- **Single point of maintenance** for the three-tier resolution algorithm
- **Self-documenting** — each plugin declares defaults and which env vars lock which
  fields
- **Locked fields as a side effect** — no separate `lockedFieldsFromEnvMap` call;
  the resolver returns the locked field list from the same pass

### Considerations

- Requires Go generics (1.18+; already available in this codebase)
- Reflection for field mapping adds runtime complexity; alternative is
  code-generated `applyEnvLocks` per struct (more boilerplate, zero reflection)
- DB overlay via `overlayFromDB` assumes settings stored in
  `recommendation_thresholds` JSONB — plugins with dedicated tables (snapshot
  settings, term windows, business-hours schedules) need adapters or separate
  overlay hooks
- Should ship as a **standalone cleanup PR**, not mixed with feature work

### Migration Path

1. Implement `ResolveSettings[T]` with reflection-based `applyEnvLocks`
2. Migrate one plugin first (e.g., container sizing thresholds) and verify behavior
   matches the existing `resolveSizingThresholds` + `applyContainerEnvLocks` path
3. Run full unit and integration tests; confirm locked-field lists in GET responses
4. Migrate remaining plugins one at a time (namespace, node, GPU, PVC, VM, quota,
   cluster-quota, idle-detection)
5. Remove per-plugin `applyXxxEnvLocks` functions and consolidate env lock maps

Until that migration completes, new settings types should follow the existing
per-plugin pattern documented in [Current Implementation](#current-implementation)
and mirror the env re-apply step used in
[`resolveSizingThresholds`](../../internal/engine/threshold_settings.go) and
[`ResolveQuotaSettings`](../../internal/engine/quota_settings.go).

---

## Related Documentation

| Document | Scope |
|----------|-------|
| [Recommendation Engines](recommendation-engines.md) | Plugin-by-plugin threshold behavior and formulas |
| [Recommendation Math](recommendation-math.md) | Adaptive margin, decay weighting, trend detection |
| [Plugin Architecture](plugin-architecture.md) | Term resolution, plugin traits, enable/disable |
| [GPU Classification](gpu-classification.md) | GPU decision tree and MIG profile selection |
| [Cost Integration](cost-integration.md) | Savings formulas, fleet summary, savings recalculation |
| [Notification codes](notification-codes.md) | Full notification code catalog |
| [Upgrade Runbook](../upgrade-runbook.md) | Migration procedures and deploy notes |

## Source File Index

| Area | Primary files |
|------|---------------|
| Config loading | [`config.go`](../../internal/config/config.go) |
| VM settings | [`vm_settings.go`](../../internal/engine/vm_settings.go), [`vm_config.go`](../../internal/engine/vm_config.go) |
| Idle detection | [`idle_settings.go`](../../internal/engine/idle_settings.go) |
| Quota / cluster-quota | [`quota_settings.go`](../../internal/engine/quota_settings.go), [`cluster_quota_settings.go`](../../internal/engine/cluster_quota_settings.go) |
| Term resolution | [`term_config.go`](../../internal/engine/term_config.go) |
| Container sizing | [`types.go`](../../internal/engine/types.go), [`recommend_all.go`](../../internal/engine/recommend_all.go) |
| Node sizing | [`recommend_nodes.go`](../../internal/engine/recommend_nodes.go) |
| GPU classification | [`gpu_recommender.go`](../../internal/engine/gpu_recommender.go), [`gpu_timeslicing.go`](../../internal/engine/gpu_timeslicing.go) |
| PVC sizing | [`pvc_recommend.go`](../../internal/engine/pvc_recommend.go) |
| Snapshot classification | [`snapshot_classify.go`](../../internal/engine/snapshot_classify.go), [`snapshot_settings.go`](../../internal/engine/snapshot_settings.go) |
