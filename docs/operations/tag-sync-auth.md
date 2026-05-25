# Tag Sync Authentication

Internal tag sync endpoints (`POST /api/cost-management/v1/internal/tags/sync`,
`GET /api/cost-management/v1/internal/tags/status`) are not exposed through the
public ROS API identity/RBAC middleware. Access is restricted to in-cluster callers.

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
unavailable.

| Variable | Default | Purpose |
|----------|---------|---------|
| `ROS_TAGS_ENABLED` | `false` | Master switch for tag sync API and list filters |
| `ROS_TAGS_ALLOWED_SERVICE_ACCOUNTS` | (empty) | Comma-separated SA names; empty accepts any authenticated SA |
| `ROS_TAGS_DEV_TOKEN` | (empty) | Dev-only static bearer token |

Implementation: [`internal/tags/auth.go`](../../internal/tags/auth.go),
[`internal/api/handlers_tags_sync.go`](../../internal/api/handlers_tags_sync.go).

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
3. Deprecate TokenReview for tag sync once mTLS is validated in cost-onprem CI.
4. Remove `ROS_TAGS_DEV_TOKEN` requirement for production paths.

**What mTLS does not replace:**

- Feature gating (`ROS_TAGS_ENABLED`) — still required to enable tag sync endpoints.
- Payload validation and org scoping — ROS still validates `org_id` and applies full-replace semantics.

---

## Related

- Tag sync data flow: [`tag-sync.md`](tag-sync.md)
- Configuration reference: [`configuration.md`](configuration.md#tag-sync)
- Koku integration: [`koku/docs/architecture/ros-ocp-integration.md`](../../../../koku/docs/architecture/ros-ocp-integration.md)
