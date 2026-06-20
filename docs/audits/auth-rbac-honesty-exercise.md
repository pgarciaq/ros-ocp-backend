# Authentication & RBAC — Honesty Exercise

**Date:** 2026-06-21
**Scope:** JWT validation, org_id tenant scoping, multi-tenant isolation, RBAC, Keycloak interaction, service-to-service auth

---

## Executive Summary

The ROS-OCP-Backend authentication and RBAC system is **well-designed and largely consistent** across sources. The architecture cleanly separates concerns: upstream proxies (3scale/Envoy) handle JWT validation, the Identity middleware extracts claims, the Entitlement middleware gates access, and RBAC middleware queries fine-grained permissions. Tenant isolation via org_id is enforced consistently in every handler through `requireXRHID()` and database-level filtering.

**Key findings:**
- Auth flow is correct and well-tested across middleware, handlers, and integration tests
- Tenant isolation (org_id scoping) is enforced in all handlers — no cross-tenant leaks found
- RBAC permission aggregation correctly handles wildcards, resource definitions, and settings permissions
- Internal endpoint auth (TokenReview) is properly gated with defense-in-depth controls
- Two fixable gaps: OpenAPI spec lacks `securitySchemes`, Identity middleware missing edge-case tests

---

## Phase 1: Discovery — Sources Located

| Source | Location | Content |
|--------|----------|---------|
| Identity middleware | `internal/api/middleware/identity.go` | Decodes `x-rh-identity`, extracts Identity + entitlement flag |
| Entitlement middleware | `internal/api/middleware/entitlement.go` | Rejects requests without `cost_management.is_entitled=true` |
| RBAC middleware | `internal/api/middleware/rbac.go` | Queries RBAC API, aggregates permissions by resource type |
| RBAC cache | `internal/api/middleware/rbac_cache.go` | LRU cache with TTL, Prometheus metrics, SHA-256 key hashing |
| Identity context helper | `internal/api/identity_context.go` | `requireXRHID()` — extracts org_id with 401 on failure |
| Settings RBAC | `internal/api/settings_rbac.go` | `settings.write` permission check for PUT/DELETE endpoints |
| Internal auth | `internal/tags/auth.go` | Kubernetes TokenReview validation for service-to-service calls |
| Internal auth config | `internal/tags/auth_config.go` | Startup validation of tag auth configuration |
| Internal endpoint auth | `internal/api/internal_endpoints.go` | `authenticateInternalCaller()` + org target validation + audit |
| Security config | `internal/config/security.go` | Production security enforcement (`IsDevelopment()`, CSV SSRF, internal auth) |
| Server setup | `internal/api/server.go` | Route registration with middleware chain |
| Config | `internal/config/config.go` | RBAC, CORS, development mode, internal auth settings |
| RBAC types | `internal/types/rbacResponse.go` | RBAC API response structures |
| RBAC docs | `docs/operations/rbac.md` | RBAC permission model documentation |
| Tag sync auth docs | `docs/operations/tag-sync-auth.md` | Internal endpoint authentication documentation |
| OpenAPI spec | `openapi.json` | API specification (missing securitySchemes) |
| Cheatsheet | `costmgmt-api-cheatsheet.adoc` | API usage examples with auth |
| E2E tests | `cost-onprem-chart/tests/` | `get_fresh_token()`, Keycloak client credentials flow |
| IQE tests | `iqe-ros-ocp-plugin/` | `IdentityAuth`, 401/403 negative tests |
| Helm chart | `cost-onprem-chart/cost-onprem/values.yaml` | jwtAuth, Keycloak, Envoy, RBAC, TLS configuration |
| Middleware tests | `internal/api/middleware/*_test.go` | Unit tests for all three middleware layers |

---

## Phase 2: Audit

### 2.1 Authentication Flow

**Cloud (SaaS) — console.redhat.com:**
```
Browser → 3scale/Turnpike → validates JWT → injects x-rh-identity header → ROS API
```

**On-prem — cost-onprem chart:**
```
Browser → Keycloak (RHBK) → JWT → Envoy proxy → validates JWT via native jwt_authn filter
       → Lua script extracts claims → injects x-rh-identity header → ROS API
```

**Internal (service-to-service):**
```
Koku worker → Bearer token (SA projected token) → ROS internal endpoint
           → TokenReview against Kubernetes API → SA allowlist check → proceed
```

**Finding:** ROS does NOT validate JWTs directly. It trusts the `x-rh-identity` header injected by the upstream proxy. This is **by design** — the proxy layer handles JWT verification, and ROS receives pre-validated identity. This is documented in `docs/operations/rbac.md` line 7.

### 2.2 Middleware Chain (v1 routes)

```
Request
  → RemoveTrailingSlash (pre-middleware)
  → Gzip
  → RequestID
  → RequestLogger
  → CORS
  → Identity middleware (decode x-rh-identity → 401 if missing/invalid)
  → CostManagementEntitlement middleware (check is_entitled → 403 if false; skipped when DEVELOPMENT=true)
  → Rbac middleware (query RBAC API → 403 if no permissions; skipped when RBAC_ENABLE=false)
  → Handler (extract org_id, filter by RBAC permissions, query DB)
```

### 2.3 Tenant Isolation

Every handler extracts `orgID` from the identity and uses it to scope database queries:

```go
xrhid, err := requireXRHID(c)  // returns 401 if missing
orgID := xrhid.Identity.OrgID  // used in all DB queries
```

Database tables use `rh_accounts.org_id` → `clusters.tenant_id` → recommendation tables. All queries join through this chain. Models carry `OrgID string` columns for direct filtering.

**Verified in handlers:** `GetNativeRecommendationSetList`, `GetGPUSummary`, `GetPVCRecommendations`, `GetSnapshotSummary`, `GetQuotaRecommendations`, `GetFleetSavingsSummary`, `GetFleetSummary`, `GetVMRecommendations`, `GetNodeRecommendations`, `GetNamespaceRecommendationSetList`, and all detail/history endpoints.

**Finding:** No cross-tenant data leaks found. Every handler path goes through `requireXRHID()` → org_id extraction → DB scoping.

### 2.4 RBAC Model

| Permission | Resource Type | Effect |
|-----------|---------------|--------|
| `cost-management:openshift.cluster:read` | `openshift.cluster` | Filters by cluster UUID |
| `cost-management:openshift.project:read` | `openshift.project` | Filters by namespace |
| `cost-management:openshift.node:read` | `openshift.node` | Filters node utilization data |
| `cost-management:*:read` | `*` | Full access (no filtering) |
| `cost-management:settings:write` | `settings.write` | Allows PUT/DELETE on settings endpoints |
| `cost-management:settings:read` | `settings.read` | Allows GET on settings endpoints |

**Convention:** Absence of a resource type means "no restriction on that dimension" (not "no access"). This matches Koku's RBAC convention and is documented in `docs/operations/rbac.md`.

### 2.5 Token Claims Required

| Claim | Path in x-rh-identity | Required | Used for |
|-------|----------------------|----------|----------|
| `org_id` | `identity.org_id` | Yes (401 if empty) | Tenant scoping |
| `type` | `identity.type` | Parsed but not enforced by ROS | Identity type |
| `is_entitled` | `entitlements.cost_management.is_entitled` | Yes (403 if false/missing, skipped in dev) | Entitlement gate |

### 2.6 Error Responses

| Condition | HTTP Status | Response |
|-----------|------------|----------|
| Missing/invalid `x-rh-identity` header | **401** | `{"message":"Unable to decode X-Rh-Identity"}` or `{"message":"Unable to unmarshal X-Rh-Identity into struct"}` |
| Empty org_id in identity | **401** | `{"status":"error","message":"missing or invalid identity"}` |
| Missing cost-management entitlement | **403** | `{"message":"Cost Management entitlement required ..."}` |
| No RBAC permissions | **403** | `{"message":"User is not authorized"}` |
| Valid permissions but empty result set | **200** | `{"data":[],"meta":{"count":0}}` |
| Missing/invalid internal bearer token | **401** | `{"message":"invalid or missing service account token"}` |
| Internal endpoint targeting disallowed org | **403** | `{"message":"org_id ... not in internal allowlist"}` |

### 2.7 Unauthenticated Endpoints

| Endpoint | Auth Required | Reason |
|----------|--------------|--------|
| `GET /status` | No | Basic app status |
| `GET /healthz` | No | Liveness probe |
| `GET /readyz` | No | Readiness probe |
| `GET /api/cost-management/v1/recommendations/openshift/openapi.json` | No | API specification |
| `GET /api/cost-management/v1/recommendations/openshift/notification-codes` | No | Reference data (no org context) |
| `GET /metrics` (separate port) | No | Prometheus metrics (separate Echo instance) |

All `v1` routes require Identity + Entitlement + RBAC (when enabled). Internal routes use bearer token auth.

### 2.8 Service-to-Service Authentication

Internal endpoints (`/api/cost-management/v1/internal/*`) use Kubernetes ServiceAccount TokenReview:

1. Caller sends `Authorization: Bearer <SA-token>`
2. ROS reads its own SA token (reviewer identity)
3. ROS POSTs `TokenReview` to Kubernetes API
4. Kubernetes validates caller token, returns SA username
5. ROS checks SA name against `ROS_TAGS_ALLOWED_SERVICE_ACCOUNTS` allowlist
6. Audit log + Prometheus metric recorded

**Dev fallback:** `ROS_TAGS_DEV_TOKEN` accepted when `DEVELOPMENT=true`. Startup **fails** if dev token is set outside development mode.

### 2.9 TLS Configuration

- **On-prem:** Envoy handles TLS termination for external traffic. Internal cluster traffic uses Kubernetes service mesh or direct HTTP.
- **Certificate trust:** `initContainer.prepareCABundle` combines system and OpenShift service CA certificates for internal HTTPS calls.
- **ROS → Kubernetes API:** Uses pod-mounted CA bundle for TokenReview calls.
- **No mTLS yet:** Documented as planned future enhancement in `docs/operations/tag-sync-auth.md`.

### 2.10 Security Headers

| Header | Configuration | Default |
|--------|--------------|---------|
| CORS Allow-Origins | `ROS_CORS_ALLOWED_ORIGINS` | Development: `*`; Production: denied |
| CORS Allow-Methods | Hardcoded | `GET`, `PUT`, `DELETE` |
| Cache-Control | Per-handler | `no-store` on recommendation responses |
| X-Request-ID | Echo middleware | Auto-generated |
| ReadHeaderTimeout | `READ_HEADER_TIMEOUT` | Configurable (seconds) |

---

## Phase 3: Alignment Matrix

| Aspect | Go Code | Internal Docs | OpenAPI Spec | Cheatsheet | E2E Tests | IQE Tests | Helm Chart |
|--------|---------|---------------|-------------|------------|-----------|-----------|------------|
| Identity middleware (x-rh-identity decode) | ✅ | ✅ | ⚠️ (described in info, no securitySchemes) | ✅ | ✅ | ✅ | ✅ |
| Entitlement check (is_entitled) | ✅ | ✅ | ✅ (described in info.description) | — | ✅ | — | ✅ |
| RBAC permission aggregation | ✅ | ✅ | — | ⚠️ (mentions RBAC briefly) | — | ✅ | ✅ |
| org_id tenant scoping | ✅ | ✅ | — | — | ✅ | ✅ | ✅ |
| 401 for missing/invalid identity | ✅ | ✅ | — | — | ✅ | ✅ | — |
| 403 for missing entitlement | ✅ | ✅ | ✅ | — | — | — | — |
| 403 for no RBAC permissions | ✅ | ✅ | — | — | — | — | ✅ |
| Health endpoints skip auth | ✅ | — | — | — | ✅ | — | ✅ |
| OpenAPI endpoint skips auth | ✅ | — | — | — | — | — | — |
| Internal bearer token auth | ✅ | ✅ | — | ✅ | — | — | ✅ |
| RBAC cache with TTL + LRU | ✅ | — | — | — | — | — | — |
| CORS restrictive in production | ✅ | — | — | — | — | — | — |
| RBAC pagination SSRF protection | ✅ | ✅ | — | — | — | — | — |
| Settings write RBAC check | ✅ | ✅ | — | — | — | — | — |
| Token refresh (E2E) | — | — | — | — | ✅ | ✅ | ✅ |
| Dev token forbidden in production | ✅ | ✅ | — | — | — | — | — |
| SA allowlist enforcement | ✅ | ✅ | — | — | — | — | ✅ |
| Internal org allowlist | ✅ | ✅ | — | — | — | — | — |
| Audit logging on internal calls | ✅ | ✅ | — | — | — | — | — |
| OpenAPI securitySchemes defined | ❌ | — | ❌ | — | — | — | — |
| Notification-codes skips auth | ✅ | — | — | ✅ | — | — | — |

**Legend:** ✅ aligned, ⚠️ partially, ❌ wrong/missing, — N/A

---

## Phase 4: Discrepancies Found & Fixes

### D1: OpenAPI spec missing `securitySchemes` (FIXED)

**What's wrong:** `openapi.json` describes auth requirements in `info.description` text but has no formal `components.securitySchemes` or top-level `security` definitions. API consumers and code generators cannot discover auth requirements programmatically.

**Fix:** Added `components.securitySchemes` with `XRhIdentity` (apiKey header) and top-level `security` array.

### D2: Identity middleware missing edge-case tests (FIXED)

**What's wrong:** No unit tests for:
- Missing `X-Rh-Identity` header entirely (empty string → base64 decode of empty = empty bytes → JSON unmarshal error → 401)
- Invalid base64 string → 401
- Valid base64 but invalid JSON → 401

All three paths return 401 correctly by code inspection, but were untested.

**Fix:** Added three test cases in `identity_test.go`.

### D3: No hardcoded secrets found ✅

Verified: no hardcoded tokens, passwords, or credentials in any non-vendor Go source file. Dev token (`ROS_TAGS_DEV_TOKEN`) is environment-variable only and startup-rejected in production.

### D4: No cross-tenant data leaks found ✅

Every handler uses `requireXRHID()` → `orgID` → database scoping. Internal endpoints validate org targets against optional allowlist.

---

## Phase 5: Auth-Specific Checklist

- [x] JWT validation rejects expired tokens — handled by upstream proxy (3scale/Envoy), not ROS directly (by design)
- [x] JWT validation rejects tokens with wrong audience/issuer — handled by upstream proxy
- [x] org_id is extracted and used in ALL database queries (no cross-tenant leaks)
- [x] Health/metrics endpoints are properly excluded from auth
- [x] 401 returned for missing/invalid token (not 500)
- [x] 403 returned for valid token but insufficient permissions (entitlement and RBAC)
- [x] Token refresh handled gracefully — E2E tests use `get_fresh_token()` with client credentials
- [x] E2E tests use `get_fresh_token()` not password grant (client credentials flow confirmed)
- [x] OpenAPI securitySchemes are defined — **FIXED** (was missing)
- [x] No hardcoded secrets in code or configs
- [x] TLS certificates properly validated — Envoy handles TLS; initContainer prepares CA bundle
- [x] Service-to-service auth is documented — `docs/operations/tag-sync-auth.md`
- [x] Multi-cluster auth (multiple org_ids) is handled correctly — each request carries one org_id; internal endpoints validate org target

---

## Security Assessment

### Strengths

1. **Defense in depth**: Three middleware layers (Identity → Entitlement → RBAC) each independently gatekeeping
2. **Consistent tenant isolation**: `requireXRHID()` pattern enforced uniformly
3. **RBAC pagination SSRF protection**: `links.next` prefix validation prevents open redirect
4. **Internal endpoint hardening**: TokenReview + SA allowlist + org allowlist + audit logging + Prometheus metrics
5. **Production security validation**: Startup fails if dev tokens or missing configs are detected in non-development mode
6. **RBAC caching**: LRU with TTL prevents thundering herd and respects permission changes
7. **CORS locked down in production**: No wildcard origins outside development mode

### Acceptable Risks (by design)

1. **ROS trusts x-rh-identity header**: JWT validation is delegated to the upstream proxy. If the proxy is bypassed, identity can be forged. This is the standard Red Hat Insights architecture — all backend services in the platform trust this header. Mitigation: NetworkPolicy restricts direct access to ROS pods.

2. **RBAC_ENABLE=false in on-prem**: When RBAC is disabled (typical for on-prem where Keycloak provides authorization), all authenticated users get full access within their org. This is acceptable because on-prem deployments have a single org and trust cluster-level access.

3. **Internal endpoints accept any authenticated SA when allowlist is empty**: In development mode, this is acceptable. In production, `ROS_TAGS_ALLOWED_SERVICE_ACCOUNTS` must be non-empty (enforced at startup).

### No Concerning Gaps

No security vulnerabilities, authentication bypasses, or tenant isolation failures were found. The system is well-designed for its operational context.

---

## Design Questions for the User

None — the authentication and RBAC system is internally consistent and well-documented. The two fixes (OpenAPI securitySchemes and middleware edge-case tests) are quality improvements, not security fixes.
