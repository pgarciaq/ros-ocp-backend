# RBAC Permission Model

This document describes how ROS-OCP-Backend enforces role-based access control.

## Overview

ROS-OCP-Backend delegates authentication to the platform (3scale/turnpike in SaaS, Keycloak/Envoy in on-prem) via the `x-rh-identity` header. Authorization is enforced by querying the **RBAC service** (Red Hat's `rbac-service`) for the user's permissions.

## Middleware Chain

```
Request → IdentityMiddleware (decode x-rh-identity)
       → RbacMiddleware (query RBAC API, store permissions)
       → Handler (filter data by permissions)
```

## Permission Types

ROS uses the `cost-management` application's RBAC permissions:

| Resource Type | Meaning | Effect |
|--------------|---------|--------|
| `openshift.cluster` | Cluster-level access | Filters recommendations by cluster UUID |
| `openshift.project` | Project/namespace access | Filters recommendations by namespace |
| `openshift.node` | Node-level access | Filters node utilization data |
| `*` (wildcard) | Full access | No filtering applied |

## Permission Resolution

1. **RBAC query**: `GET /api/rbac/v1/access/?application=cost-management&limit=100`
2. **Pagination**: Follows `links.next` up to 50 pages (iterative, not recursive)
3. **Aggregation**: Permissions are grouped by resource type into a map:
   ```
   {
     "openshift.cluster": ["cluster-uuid-1", "cluster-uuid-2"],
     "openshift.project": ["namespace-a", "*"],
     "*": []
   }
   ```

## Access Control Rules

| Condition | Result |
|-----------|--------|
| No permissions returned from RBAC | **403 Forbidden** |
| `"*"` resource type present | Unrestricted access |
| `openshift.cluster` absent | Unrestricted cluster access (Koku convention) |
| `openshift.cluster` contains `"*"` | Unrestricted cluster access |
| `openshift.cluster` contains specific UUIDs | Filter to those clusters only |
| `openshift.project` absent | Unrestricted namespace access |
| `openshift.project` contains `"*"` | Unrestricted namespace access |
| `openshift.project` contains specific names | Filter to those namespaces only |

**Important**: Absence of a resource type means "no restriction on that dimension" (not "no access"). This matches the Koku RBAC convention.

## Handler-Level Filtering

Each handler applies RBAC filtering via helper functions:

- `filterClustersByRBAC(permissions, clusters)` — intersects queried clusters with allowed clusters
- `getClustersForOrg(pool, orgID)` — retrieves all clusters for the org
- SQL `WHERE cluster_uuid = ANY($N)` — restricts database queries to allowed clusters

## Endpoints and Their RBAC Dimensions

| Endpoint | Filters By |
|----------|-----------|
| `GET /recommendations/openshift` | cluster, project |
| `GET /recommendations/openshift/{id}` | cluster, project |
| `GET /recommendations/openshift/gpu` | cluster |
| `GET /recommendations/openshift/gpu/timeslicing` | cluster |
| `GET /recommendations/openshift/gpu/mig` | cluster |
| `GET /recommendations/openshift/nodes` | cluster |
| `GET /recommendations/openshift/history` | cluster, project |
| `GET /recommendations/openshift/quality` | cluster, project |
| `GET .../fleet-summary` | cluster |
| `GET .../namespace/recommendations` | cluster |
| `GET .../pvcs` | cluster |
| `GET .../snapshots` | cluster |

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `RBAC_ENABLE` | `true` | Enable/disable RBAC enforcement |
| `RBAC_HOST` | (from Clowder) | RBAC service hostname |
| `RBAC_PORT` | (from Clowder) | RBAC service port |
| `RBAC_PROTOCOL` | `http` | Protocol for RBAC calls |

When `RBAC_ENABLE=false`, no RBAC middleware is applied (all users get full access). This is used in development and on-prem deployments where Keycloak provides authorization instead.

## Security Notes

- The RBAC query forwards the user's `x-rh-identity` header to the RBAC service
- Pagination URLs are validated to start with `/api/rbac/` to prevent SSRF via crafted `links.next`
- Maximum 50 pages prevents unbounded memory growth from a misbehaving RBAC service
