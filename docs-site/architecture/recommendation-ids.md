# Deterministic Recommendation IDs

**Last updated:** 2026-06-10

Native ROS recommendations expose stable UUIDs in list and detail API responses. IDs are **UUID v5** values derived from cluster and workload identity — not random per request.

---

## Why deterministic IDs?

| Benefit | Explanation |
|---------|-------------|
| **Idempotent upserts** | Ingest can `ON CONFLICT` on `(org_id, cluster_uuid, namespace, workload, container_name, term, engine)` while the API `id` stays constant across runs. |
| **Stable deep links** | UI bookmarks and automation scripts can reference `/recommendations/openshift/{id}` without chasing a new UUID after each ingest. |
| **Indexed lookups** | `container_id` / `namespace_id` columns support O(1) detail queries instead of scanning composite keys. |

Implementation: [`NativeContainerID`](../../internal/model/recommendation_set_native.go), [`NativeNamespaceID`](../../internal/model/recommendation_set_native.go) using namespace `f47ac10b-58cc-4372-a567-0e02b2c3d479`.

---

## Security invariant: org_id is mandatory on detail lookups

Deterministic IDs are **not tenant-scoped**. The same cluster UUID, namespace, workload, and container name in two different organizations would produce the **same** recommendation UUID if both orgs had identical cluster topology (unlikely in production but possible in test fixtures).

Therefore every detail or single-record query **must** constrain results to the caller's `org_id` from `X-Rh-Identity`. Fetching by UUID alone would be an authorization bypass.

### Detail endpoints audited (2026-06-10)

| Endpoint | Model / handler | org_id filter |
|----------|-----------------|---------------|
| `GET /recommendations/openshift/{id}` (native) | `GetNativeRecommendationByID` → `nativeContainerDetailQuery` | `rs.org_id = ?` |
| `GET /recommendations/openshift/{id}` (legacy fallback) | `GetRecommendationSetByID` → `getRecommendationQuery` | `recommendation_sets.org_id = ?` |
| `GET /recommendations/openshift/namespaces/{id}` (native) | `GetNativeNamespaceRecommendationByID` → `nativeNamespaceDetailQuery` | `ns.org_id = ?` |
| `GET /recommendations/openshift/namespaces/{id}` (legacy) | `GetNamespaceRecommendationSetByID` → `getNamespaceRecommendationQuery` | `namespace_recommendation_sets.org_id = ?` |
| `GET /recommendations/openshift/pvcs/detail` | `GetPVCRecommendationDetail` | SQL `WHERE org_id = $1` (composite key, not UUID) |
| `GET /recommendations/openshift/namespaces/{id}/history` | Resolves ID via native/legacy namespace detail above | Same org_id path |

List endpoints apply the same org boundary via denormalized `org_id` filters plus optional RBAC cluster filters.

### Regression guard

[`internal/model/recommendation_detail_org_scope_test.go`](../../internal/model/recommendation_detail_org_scope_test.go) uses GORM dry-run SQL inspection to assert that legacy and native detail query builders include `org_id` predicates.

---

## When adding new detail endpoints

1. Never query recommendation tables by UUID (or composite surrogate ID) without an `org_id` predicate tied to the authenticated tenant.
2. Prefer filtering `recommendation_sets.org_id` / `namespace_recommendation_sets.org_id` directly; join `rh_accounts` only when the table has no `org_id` column (e.g. `clusters`).
3. Add a dry-run SQL test alongside existing detail query tests if you introduce a new query builder.
