# Tag Sync Authentication

Authentication for tag data transfer depends on deployment mode (`ROS_TAGS_SOURCE`).

---

## On-Prem (`db` mode): Direct DB reads, authenticated internal endpoints

When ROS and Koku share a PostgreSQL instance (default for cost-onprem), ROS reads tag
data **directly from Koku tenant tables**:

- `org{org_id}.reporting_enabledtagkeys`
- `org{org_id}.reporting_ocptags_values`

There is **no HTTP push** from Koku for tag filtering in db mode. ROS uses its database
credentials to query Koku schemas on the same PostgreSQL server.

Internal push endpoints (`POST /internal/tags/sync`, `GET /internal/tags/status`) remain
registered but are not used for the on-prem data path. When called, they require bearer
TokenReview auth when `ROS_INTERNAL_TAGS_AUTH_REQUIRED=true` (default) — set
`ROS_INTERNAL_TAGS_AUTH_REQUIRED=false` for local dev without service account tokens.

**What you still configure:**

| Variable | Purpose |
|----------|---------|
| `ROS_TAGS_ENABLED=true` | Enables tag list filters |
| `ROS_TAGS_SOURCE=db` | Default; selects direct DB reads |

No Koku-side tag sync env vars are required.

---

## SaaS (`api` mode): ServiceAccount authentication

When Koku and ROS use separate databases, Koku pushes tags via HTTP. Internal endpoints
are **not** exposed through public ROS identity/RBAC middleware — access is restricted
to in-cluster callers with valid Kubernetes ServiceAccount identity.

| Method | Path |
|--------|------|
| `POST` | `/api/cost-management/v1/internal/tags/sync` |
| `GET` | `/api/cost-management/v1/internal/tags/status` |

---

### Current: Kubernetes ServiceAccount TokenReview

Koku pushes resolved tags from the worker/listener pod:

```
Authorization: Bearer <service-account-token>
```

On the ROS side, [`internal/tags/auth.go`](../../internal/tags/auth.go) validates via
the Kubernetes **TokenReview API**:

```mermaid
sequenceDiagram
    participant KW as Koku worker
    participant ROS as ROS API
    participant K8s as Kubernetes API
    KW->>ROS: POST internal tags sync with Bearer token
    ROS->>K8s: TokenReview for caller token
    K8s-->>ROS: authenticated SA username
    ROS->>ROS: optional SA allowlist check
    ROS-->>KW: 200 OK with updated row count
```

1. ROS reads its own pod ServiceAccount token (reviewer identity).
2. ROS POSTs `TokenReview` to `https://kubernetes.default.svc/apis/authentication.k8s.io/v1/tokenreviews`.
3. The API confirms the caller token is authenticated and returns the ServiceAccount
   username (`system:serviceaccount:<ns>:<name>`).
4. Optionally, `ROS_TAGS_ALLOWED_SERVICE_ACCOUNTS` restricts which SA names are accepted
   (empty = any authenticated SA).

**Why TokenReview:**

- Zero shared secrets to distribute inside the cluster.
- Native Kubernetes identity — caller must be a real pod ServiceAccount.
- Standard RBAC: grant the Koku worker SA permission to reach ROS internal routes.

**Dev fallback:** When `ROS_TAGS_DEV_TOKEN` is set **and** `DEVELOPMENT=true`, matching bearer tokens are accepted
with a warning log. Use only for local/docker-compose where TokenReview is unavailable.
Startup **fails** if the dev token is set outside development mode.
Set the **same** token on Koku (`ROS_TAGS_DEV_TOKEN`) and ROS.

**Production (api mode):** `ROS_TAGS_ALLOWED_SERVICE_ACCOUNTS` must be non-empty. Empty allowlist is rejected at startup and at runtime outside development.

### Internal `org_id` scope (by design)

Internal endpoints (`POST /internal/tags/sync`, `GET /internal/tags/status`, `POST /internal/recalculate-savings`) authenticate the **caller** ServiceAccount but do **not** bind the request `org_id` to the caller's tenant. This is intentional: Koku/Masu invoke ROS on behalf of arbitrary tenants using cluster-internal credentials. Defense-in-depth relies on:

- **NetworkPolicy** restricting who can reach internal routes (on-prem chart default)
- **`ROS_TAGS_ALLOWED_SERVICE_ACCOUNTS`** allowlist (production)
- **Structured audit logging** on each internal call (`caller_sa`, `target_org_id`, `action`)
- **`rosocp_internal_endpoint_calls_total`** metric (labels `endpoint`, `org_id`, `sa_name`) for anomaly detection
- **Optional `ROS_INTERNAL_ALLOWED_ORGS`** comma-separated allowlist when operators want to restrict target orgs (empty = all allowed, default)

| Variable | Default | Purpose |
|----------|---------|---------|
| `ROS_TAGS_ENABLED` | `true` | Master switch for tag sync API and list filters (cost-onprem chart default) |
| `ROS_TAGS_SOURCE` | `api` | Must be `api` for push endpoints to accept traffic (`db` is advanced on-prem only) |
| `ROS_TAGS_ALLOWED_SERVICE_ACCOUNTS` | (empty) | Comma-separated SA names; empty accepts any authenticated SA |
| `ROS_TAGS_DEV_TOKEN` | (empty) | Dev-only static bearer token |
| `ROS_INTERNAL_ALLOWED_ORGS` | (empty) | Optional comma-separated org IDs internal endpoints may target; empty allows all |

Implementation: [`internal/tags/auth.go`](../../internal/tags/auth.go),
[`internal/api/handlers_tags_sync.go`](../../internal/api/handlers_tags_sync.go).

---

## Tag lifecycle and auth interaction (api mode)

Authentication gates **who** may push; lifecycle events determine **when** pushes occur.

| Scenario | Sync triggered? | Auth required? | ROS state if sync fails |
|----------|-----------------|----------------|-------------------------|
| Tag key enabled in Settings | Yes (immediate) | Bearer token | Previous tags retained |
| Tag key disabled | Yes (immediate) | Bearer token | Stale until success |
| OCP summarization complete | Yes (per tenant) | Bearer token | Stale until success |
| Periodic safety-net (6h) | Yes (all tenants) | Bearer token | Stale until success |
| Invalid/missing token | N/A | **401/403** | Unchanged |
| ROS pod restart | N/A | N/A | Tags persisted in DB |

Failed auth does **not** corrupt tag data — ROS rejects the request before the
full-replace transaction begins.

---

## Manual sync (api mode)

Operators can force a tag push without waiting for the next summarization cycle or
6-hour safety-net.

### Masu API

When the endpoint is exposed in your deployment:

```bash
curl -s "http://localhost:5042/api/cost-management/v1/sync_ros_tags/?schema=org1234567"
```

Use the full tenant schema name (`org1234567` for org_id `1234567`).

### Django shell (Koku)

```python
from masu.processor.ros_tag_sync import sync_ros_ocp_tags

# Single tenant
sync_ros_ocp_tags.delay("org1234567")

# All tenants (same fan-out as periodic safety-net)
from masu.processor.ros_tag_sync import sync_ros_ocp_tags_periodic
sync_ros_ocp_tags_periodic.delay()
```

### Celery CLI (Koku worker pod)

```bash
celery -A koku call masu.processor.ros_tag_sync.sync_ros_ocp_tags --args='["org1234567"]'
```

Requires `ROS_TAGS_ENABLED=true` and `ROS_TAGS_SOURCE=api` on the worker; otherwise the
task exits immediately as a no-op.

---

## Failure modes and recovery (api mode)

### TokenReview unavailable

**Symptoms:** ROS logs `service account reviewer token unavailable` or TokenReview HTTP errors.
Koku push requests return **401 Unauthorized** even with a valid projected worker token.

**Common cause (on-prem):** The ROS ServiceAccount lacks permission to call the Kubernetes
TokenReview API. ROS must hold the cluster `system:auth-delegator` ClusterRole (or equivalent
`create` on `tokenreviews.authentication.k8s.io`). The cost-onprem chart creates a
`ClusterRoleBinding` for `ros-backend` when `ros.internalAuth.enabled=true` (default).

**Verify binding:**

```bash
kubectl get clusterrolebinding -l app.kubernetes.io/instance=cost-onprem | grep ros-auth-delegator
```

**Causes:** ROS API pod not running with a mounted SA token; missing `system:auth-delegator`
binding for the ROS ServiceAccount; wrong `KUBERNETES_TOKEN_REVIEW_URL`; API server unreachable.

**Recovery:**

1. Verify ROS deployment has `automountServiceAccountToken: true`.
2. Confirm in-cluster DNS to `kubernetes.default.svc`.
3. For local dev, set `ROS_TAGS_DEV_TOKEN` on both Koku and ROS.

### Caller token rejected

**Symptoms:** `token not authenticated` or `service account "…" is not allowed`.

**Causes:** Expired projected token; SA not in `ROS_TAGS_ALLOWED_SERVICE_ACCOUNTS`; non-SA user token.

**Recovery:**

1. Confirm Koku worker uses the expected ServiceAccount.
2. Add SA name to `ROS_TAGS_ALLOWED_SERVICE_ACCOUNTS` or leave empty for any SA.
3. Check projected token volume mount on Koku worker pod.

### Koku cannot read SA token

**Symptoms:** Koku log `service account token unavailable for ROS tag sync`.

**Recovery:** Set `ROS_TAGS_DEV_TOKEN` for dev, or fix SA token mount on Koku worker.

### Sync succeeds but filters stale

**Symptoms:** `GET /internal/tags/status` shows old `synced_at`.

**Causes:** Network partition after summarization; Celery task backlog; `ROS_TAGS_ENABLED=false`
on Koku; `ROS_TAGS_SOURCE` not set to `api`.

**Recovery:** Wait for 6-hour periodic sync or manually trigger summarization/tag settings change.
Monitor Koku `ROS tag sync failed` logs.

### Partial org corruption (theoretical)

Full-replace runs in a transaction — a mid-sync crash rolls back. Worst case: last **successful**
sync remains visible (eventual consistency, not data loss).

---

## Monitoring (api mode)

### Koku worker logs

The Celery task `masu.processor.ros_tag_sync.sync_ros_ocp_tags` runs inside `koku-worker`.
Search worker logs for sync lifecycle messages:

```bash
# Kubernetes
kubectl logs -l app=koku-worker --tail=500 | grep -E "ROS tag sync"

# Docker Compose
docker compose logs koku-worker --tail=200 | grep -E "ROS tag sync"
```

| Log message | Meaning |
|-------------|---------|
| `ROS tag sync completed` | Push succeeded; context includes `namespace_count`, `updated`, `synced_at` |
| `ROS tag sync failed` | Push failed; context includes `schema`, `org_id`, `error` |
| `ROS periodic tag sync scheduled` | 6-hour safety-net queued tasks for all tenants |
| `service account token unavailable for ROS tag sync` | Koku cannot read bearer token — fix SA mount or set `ROS_TAGS_DEV_TOKEN` |

### ROS freshness endpoint

```bash
curl -s -H "Authorization: Bearer $TOKEN" \
  "$ROS_URL/api/cost-management/v1/internal/tags/status?org_id=1234567"
```

Use bare `org_id` (not `org1234567`). Response fields:

| Field | Purpose |
|-------|---------|
| `synced_at` | ISO-8601 UTC timestamp of last successful push |
| `tag_keys` | Enabled-key catalog from last sync |

**Alert threshold:** `synced_at` older than **~6 hours** during normal operations indicates
a stuck pipeline (event triggers and periodic safety-net both failing).

Compare `synced_at` to the last OCP manifest completion time for the org.

### Suggested alerts

| Signal | Source | Suggested alert |
|--------|--------|-----------------|
| Sync failures | Koku log `ROS tag sync failed` | > N failures in 1h per org |
| Stale `synced_at` | `GET /internal/tags/status` | `synced_at` older than 6–7h |
| Auth failures | ROS API 401/403 on `/internal/tags/*` | Spike in unauthorized pushes |
| Zero `updated` rows | Sync response `updated: 0` with non-empty payload | Investigate missing `org_container_keys` rows |
| Removed keys | ROS log `tag sync: removed tag key` | Informational (expected on disable) |

**On-prem (`db`):** Monitor Koku summarization completion instead — no `synced_at` push metadata.

---

## Future: mTLS (SaaS)

**Planned upgrade** for SaaS and high-security on-prem deployments: mutual TLS between
Koku worker and ros-ocp-backend pods.

### Motivation

ServiceAccount tokens work well in-cluster but have operational trade-offs:

- **Token rotation** — projected SA tokens expire; misconfiguration can cause sync failures.
- **Unidirectional trust** — TokenReview validates the caller, but Koku cannot
  cryptographically verify it is talking to the real ROS API beyond TLS server cert.
- **Off-cluster callers** — TokenReview requires in-cluster access to the Kubernetes API.

mTLS establishes **bidirectional authentication** at the transport layer.

### Intended design

```
┌─────────────┐   client cert (Koku SA)    ┌─────────────┐
│ Koku worker │ ─────────────────────────▶ │ ROS API     │
│             │ ◀───────────────────────── │             │
└─────────────┘   server cert (ROS API)    └─────────────┘
         both sides verify peer certificate
```

**Implementation options:**

1. **cert-manager** — per-service certificates from a cluster CA; mount cert/key into
   Koku and ROS; configure Go `http.Client` and Echo TLS for mutual verification.
2. **Service mesh sidecar** (Istio/Linkerd) — mTLS handled by sidecars; application
   continues HTTP but traffic is encrypted and authenticated between mesh members.

**Migration path:**

1. Ship mTLS alongside TokenReview (feature flag, e.g. `ROS_TAGS_MTLS_ENABLED`).
2. Helm chart mounts cert-manager `Certificate` resources for Koku worker and ROS API.
3. Configure Koku HTTP client with client cert; ROS with client cert verification.
4. Deprecate TokenReview for tag sync once mTLS is validated in CI.
5. Remove `ROS_TAGS_DEV_TOKEN` requirement for production paths.

**What mTLS does not replace:**

- Feature gating (`ROS_TAGS_ENABLED`) — still required.
- Payload validation and org scoping — ROS still validates `org_id` and full-replace semantics.
- Periodic safety-net — still recommended until webhook-based instant sync exists.

**On-prem note:** Shared-database deployments (`db` mode) do not need push auth today.
mTLS applies when operators choose `api` source or for other ROS↔Koku HTTP integrations.

---

## Related

- Feature overview: [`../features/tag-filtering.md`](../features/tag-filtering.md)
- Tag sync data flow (api): [`tag-sync.md`](tag-sync.md)
- Configuration: [`configuration.md`](configuration.md#tag-sync)
- Public docs: [`../../docs-site/features/tag-filtering.md`](../../docs-site/features/tag-filtering.md)
- Koku integration: [`koku/docs/architecture/ros-ocp-integration.md`](../../../koku/docs/architecture/ros-ocp-integration.md)
