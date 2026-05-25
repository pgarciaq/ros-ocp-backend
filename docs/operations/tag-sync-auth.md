# Tag Sync Authentication

Push authentication applies only when `ROS_TAGS_SOURCE=api`. With the default on-prem
configuration (`ROS_TAGS_SOURCE=db`), ROS reads Koku tag tables directly and the push
endpoints return 404 — no ServiceAccount auth is required for tag filtering.

Internal tag sync endpoints (`POST /api/cost-management/v1/internal/tags/sync`,
`GET /api/cost-management/v1/internal/tags/status`) are not exposed through the
public ROS API identity/RBAC middleware. When push sync is enabled, access is restricted
to in-cluster callers.

---

## Current: Kubernetes ServiceAccount Token Validation

Koku pushes resolved tags from the worker/listener pod using a bearer token:

```
Authorization: Bearer <service-account-token>
```

On the ROS side, [`internal/tags/auth.go`](../../internal/tags/auth.go) validates the
token via the Kubernetes **TokenReview API**:

1. ROS reads its own pod ServiceAccount token (reviewer identity).
2. ROS POSTs a `TokenReview` to `https://kubernetes.default.svc/apis/authentication.k8s.io/v1/tokenreviews`.
3. The API confirms the caller token is authenticated and identifies the ServiceAccount.
4. Optionally, `ROS_TAGS_ALLOWED_SERVICE_ACCOUNTS` restricts which ServiceAccount names are accepted.

**Why this approach:**

- Zero-config inside the cluster — no shared secrets to distribute.
- Uses native Kubernetes identity — the caller must be a real pod SA.
- Works with standard RBAC: grant the Koku worker SA permission to call the ROS internal route.

**Dev fallback:** When `ROS_TAGS_DEV_TOKEN` is set, matching bearer tokens are accepted
with a warning log. Use only for local/docker-compose development where TokenReview is
unavailable. Set the **same** token on Koku (`ROS_TAGS_DEV_TOKEN`) and ROS.

| Variable | Default | Purpose |
|----------|---------|---------|
| `ROS_TAGS_ENABLED` | `false` | Master switch for tag sync API and list filters |
| `ROS_TAGS_ALLOWED_SERVICE_ACCOUNTS` | (empty) | Comma-separated SA names; empty accepts any authenticated SA |
| `ROS_TAGS_DEV_TOKEN` | (empty) | Dev-only static bearer token |

Implementation: [`internal/tags/auth.go`](../../internal/tags/auth.go),
[`internal/api/handlers_tags_sync.go`](../../internal/api/handlers_tags_sync.go).

---

## Tag Lifecycle and Auth Interaction

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

## Failure Modes and Recovery

### TokenReview unavailable

**Symptoms:** ROS logs `service account reviewer token unavailable` or TokenReview HTTP errors.

**Causes:** ROS API pod not running with a mounted SA token; wrong `KUBERNETES_TOKEN_REVIEW_URL`; API server unreachable.

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

**Causes:** Network partition after summarization; Celery task backlog; `ROS_TAGS_ENABLED=false` on Koku.

**Recovery:** Wait for 6-hour periodic sync or manually trigger summarization/tag settings change. Monitor Koku `ROS tag sync failed` logs.

### Partial org corruption (theoretical)

Full-replace runs in a transaction — a mid-sync crash rolls back. Worst case: last **successful** sync remains visible (eventual consistency, not data loss).

---

## Monitoring and Alerting

| Signal | Source | Suggested alert |
|--------|--------|-----------------|
| Sync failures | Koku log `ROS tag sync failed` | > N failures in 1h per org |
| Stale `synced_at` | `GET /internal/tags/status` | `synced_at` older than 7h |
| Auth failures | ROS API 401/403 on `/internal/tags/*` | Spike in unauthorized pushes |
| Zero `updated` rows | Sync response `updated: 0` with non-empty payload | Investigate missing `org_container_keys` rows |
| Removed keys | ROS log `tag sync: removed tag key` | Informational (expected on disable) |

**Health check workflow:**

```bash
# From a pod with the Koku worker SA token:
curl -s -H "Authorization: Bearer $TOKEN" \
  "$ROS_URL/api/cost-management/v1/internal/tags/status?org_id=1234567"
```

Compare `synced_at` to last successful OCP manifest completion time.

---

## Future: mTLS

**Planned upgrade for on-prem deployments:** mutual TLS between Koku and ros-ocp-backend pods.

### Motivation

ServiceAccount tokens work well in-cluster but have operational trade-offs:

- **Token rotation** — projected SA tokens expire; callers must refresh (Kubernetes handles this, but misconfiguration can cause sync failures).
- **Unidirectional trust** — TokenReview validates the caller, but Koku cannot cryptographically verify it is talking to the real ROS API (beyond TLS server cert).
- **Off-cluster callers** — TokenReview requires in-cluster network access to the Kubernetes API.

mTLS addresses these by establishing **bidirectional authentication** at the transport layer.

### Intended Design

```
┌─────────────┐   client cert (Koku SA)    ┌─────────────┐
│ Koku worker │ ─────────────────────────▶ │ ROS API     │
│             │ ◀───────────────────────── │             │
└─────────────┘   server cert (ROS API)    └─────────────┘
         both sides verify peer certificate
```

**Implementation options (on-prem):**

1. **cert-manager** — Issue per-service certificates from a cluster CA; mount cert/key into Koku and ROS deployments; configure Go `http.Client` and Echo TLS listener for mutual verification.
2. **Service mesh sidecar** (Istio/Linkerd) — mTLS handled transparently by sidecars; application continues HTTP but traffic is encrypted and authenticated between mesh members.

**Migration path:**

1. Ship mTLS support alongside existing TokenReview auth (feature flag, e.g. `ROS_TAGS_MTLS_ENABLED`).
2. Helm chart mounts cert-manager `Certificate` resources for Koku worker and ROS API.
3. Configure Koku `requests` client with client cert; ROS Echo with `VerifyClientCertIfGiven` or mesh policy.
4. Deprecate TokenReview for tag sync once mTLS is validated in cost-onprem CI.
5. Remove `ROS_TAGS_DEV_TOKEN` requirement for production paths.

**What mTLS does not replace:**

- Feature gating (`ROS_TAGS_ENABLED`) — still required to enable tag sync endpoints.
- Payload validation and org scoping — ROS still validates `org_id` and applies full-replace semantics.
- Periodic safety-net — still recommended until webhook-based instant sync exists.

---

## Related

- Feature overview: [`../features/tag-filtering.md`](../features/tag-filtering.md)
- Tag sync data flow: [`tag-sync.md`](tag-sync.md)
- Configuration reference: [`configuration.md`](configuration.md#tag-sync)
- Koku integration: [`koku/docs/architecture/ros-ocp-integration.md`](../../../../koku/docs/architecture/ros-ocp-integration.md)
