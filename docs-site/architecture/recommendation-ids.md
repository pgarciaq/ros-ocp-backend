# Deterministic Recommendation IDs

> **Last verified:** 2026-08-05

**Last updated:** 2026-06-24

Native ROS recommendations expose stable UUIDs in list and detail API responses. IDs are **UUID v5** values derived from cluster and workload identity — not random per request.

---

## Why deterministic IDs?

| Benefit | Explanation |
|---------|-------------|
| **Idempotent upserts** | Ingest can `ON CONFLICT` on composite keys while the API `id` stays constant across runs. |
| **Stable deep links** | UI bookmarks and automation scripts can reference recommendations without chasing a new UUID after each ingest. |
| **Indexed lookups** | `container_id` / `namespace_id` columns support O(1) detail queries instead of scanning composite keys. |

Implementation: [`NativeContainerID`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/model/recommendation_set_native.go), [`NativeNamespaceID`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/model/recommendation_set_native.go), and plugin-specific helpers in [`recommendation_ids.go`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/model/recommendation_ids.go) using namespace `f47ac10b-58cc-4372-a567-0e02b2c3d479`.

---

## ID formulas

All IDs use UUID v5 (SHA-1) with the shared namespace above. Plugin types use a **type prefix** in the hashed name to avoid collisions with namespace IDs (same cluster + name string).

| Recommendation type | Go function | Hashed name format |
|---------------------|-------------|-------------------|
| Container | `NativeContainerID` | `{cluster}/{namespace}/{workload}/{workload_type}/{container}` |
| Namespace | `NativeNamespaceID` | `{cluster}/{namespace}` |
| Node | `NativeNodeID` | `node/{cluster}/{node}` |
| PVC | `NativePvcID` | `pvc/{cluster}/{namespace}/{pvc}` |
| Quota | `NativeQuotaID` | `quota/{cluster}/{namespace}/{quota_name}` |
| Cluster quota | `NativeClusterQuotaID` | `cluster-quota/{cluster}/{cluster_quota_name}` |
| Snapshot | `NativeSnapshotID` | `snapshot/{cluster}/{namespace}/{snapshot_name}` |
| VM | `NativeVMID` | `vm/{cluster}/{namespace}/{vm_name}` |

The koku-ui ROS app mirrors these formulas in `apps/koku-ui-ros/src/utils/recommendationIds.ts` for client-side fallback when an older backend omits `id`.

**Grouped list rows** (`group_by[cluster]` or `group_by[project]`) are aggregates over multiple recommendations and **omit** `id`.

---

## Security invariant: org_id is mandatory on detail lookups

Deterministic IDs are **not tenant-scoped**. The same cluster UUID and entity name in two different organizations would produce the **same** recommendation UUID if both orgs had identical cluster topology (unlikely in production but possible in test fixtures).

Therefore every detail or single-record query **must** constrain results to the caller's `org_id` from `X-Rh-Identity`. Fetching by UUID alone would be an authorization bypass.

### Detail endpoints audited (2026-06-10)

| Endpoint | Model / handler | org_id filter |
|----------|-----------------|---------------|
| `GET /recommendations/openshift/{id}` (native) | `GetNativeRecommendationByID` → `nativeContainerDetailQuery` | `rs.org_id = ?` |
| `GET /recommendations/openshift/{id}` (legacy fallback) | `GetRecommendationSetByID` → `getRecommendationQuery` | `recommendation_sets.org_id = ?` |
| `GET /recommendations/openshift/namespaces/{id}` (native) | `GetNativeNamespaceRecommendationByID` → `nativeNamespaceDetailQuery` | `ns.org_id = ?` |
| `GET /recommendations/openshift/namespaces/{id}` (legacy) | `GetNamespaceRecommendationSetByID` → `getNamespaceRecommendationQuery` | `namespace_recommendation_sets.org_id = ?` |
| `GET /recommendations/openshift/pvcs/detail` | `GetPVCRecommendationDetail` | SQL `WHERE org_id = $1` (composite key lookup; response includes deterministic `id`) |
| `GET /recommendations/openshift/quota/detail` | `GetQuotaRecommendationDetail` | SQL `WHERE org_id = $1` (composite key; response includes deterministic `id`) |
| `GET /recommendations/openshift/cluster-quota/detail` | `GetClusterQuotaRecommendationDetail` | SQL `WHERE org_id = $1` (composite key; response includes deterministic `id`) |
| `GET /recommendations/openshift/nodes/{node}` | `GetNodeUtilizationDetail` | SQL `WHERE org_id = $1` (node name path; response includes deterministic `id`) |
| `GET /recommendations/openshift/vm/detail` | `GetVMRecommendationDetail` | SQL `WHERE org_id = $1` (composite key; response includes deterministic `id`) |
| `GET /recommendations/openshift/namespaces/{id}/history` | Resolves ID via native/legacy namespace detail above | Same org_id path |

List endpoints apply the same org boundary via denormalized `org_id` filters plus optional RBAC cluster filters.

### Regression guard

[`internal/model/recommendation_detail_org_scope_test.go`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/model/recommendation_detail_org_scope_test.go) uses GORM dry-run SQL inspection to assert that legacy and native detail query builders include `org_id` predicates.

---

## When adding new detail endpoints

1. Never query recommendation tables by UUID (or composite surrogate ID) without an `org_id` predicate tied to the authenticated tenant.
2. Prefer filtering `recommendation_sets.org_id` / plugin table `org_id` directly; join `rh_accounts` only when the table has no `org_id` column (e.g. `clusters`).
3. Add a dry-run SQL test alongside existing detail query tests if you introduce a new query builder.
4. Expose a deterministic `id` on list and detail responses using the type-prefixed UUID v5 pattern above.

See also: adversarial review finding #27 in [`docs/audits/adversarial-review.md`](../audits/adversarial-review.md).
