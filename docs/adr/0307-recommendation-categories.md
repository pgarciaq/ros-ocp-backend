# 0307 — Recommendation Categories (undersized/oversized/optimized)

**Status:** Accepted
**Date:** 2026-06-30
**Domain:** Data Model / API Design
**Phase:** 15+
**Issue:** [#81](https://github.com/pgarciaq/ros-ocp-backend/issues/81)

## Context

Clients currently infer recommendation direction (undersized, oversized, or
optimized) from `variation_cpu_pct` and `variation_memory_pct` fields in the API
response. This is error-prone — each consumer reimplements threshold logic — and
prevents efficient server-side filtering since the classification is never
persisted.

The API has no unified way to answer "show me all undersized workloads" without
the client fetching all recommendations and computing the answer locally.

## Decision

### Scope

| Entity | Needs new column? | Rationale |
|--------|:-:|------|
| Containers | ✅ | Derived from `variation_cpu_pct` / `variation_memory_pct` |
| Namespaces | ✅ | Same variation logic |
| PVCs | ❌ | Already have `recommendation_type` — map in serialization |
| Quotas | ❌ | Already have `is_oversized` — map directly |
| VMs | ❌ | Already have `Classification` enum — map directly |
| GPUs | ❌ | Already have classification — map directly |
| Snapshots | ❌ | Cleanup actions, not sizing recommendations |

### Classification logic

Thresholds use a ±10% dead zone:

- **undersized:** `max(variation_cpu_pct, variation_memory_pct) > +10%`
- **oversized:** `min(variation_cpu_pct, variation_memory_pct) < -10%` AND
  neither variation is > +10%
- **optimized:** both variations between -10% and +10%

When CPU and memory disagree (one says undersized, other says oversized), the
conservative rule applies: **undersized wins**. It is worse to starve a workload
than to waste resources.

Example: CPU variation = -20% (oversized), memory variation = +15% (undersized)
→ overall category = `undersized`.

### Enum values

`undersized`, `oversized`, `optimized`

`idle` and `abandoned` from the original proposal are excluded. `idle_state`
remains a separate orthogonal field answering "should this workload exist?" while
`category` answers "how should it be resized?"

### Persistence

New `category TEXT` column on:
- `recommendation_sets` (containers)
- `namespace_recommendation_sets` (namespaces)

Computed during native engine recommendation run, persisted alongside other
results. Only populated going forward — existing recommendations get `NULL`
(treated as "unclassified" by UI).

Index: `(org_id, category) WHERE category IS NOT NULL` for efficient filtering.

### API additions

Response includes `"category": "undersized"` (or `"oversized"` / `"optimized"` / `null`).

Per-resource breakdown (containers only):
```json
{
  "category": "undersized",
  "category_cpu": "oversized",
  "category_memory": "undersized"
}
```

Filter: `?filter[category]=undersized` on container and namespace list endpoints.

### Migration

Standard `AddColumn` — no `set_pg_extended_mode` needed:
```sql
ALTER TABLE recommendation_sets ADD COLUMN category TEXT;
ALTER TABLE namespace_recommendation_sets ADD COLUMN category TEXT;
CREATE INDEX idx_rec_sets_category
  ON recommendation_sets (org_id, category) WHERE category IS NOT NULL;
CREATE INDEX idx_ns_rec_sets_category
  ON namespace_recommendation_sets (org_id, category) WHERE category IS NOT NULL;
```

## Alternatives considered

**Option A — Compute at serialization time:** Correct but prevents efficient
server-side filtering. Every list query would need to compute the category for
all rows before applying the filter predicate, negating index benefits.

**Include idle/abandoned as category values:** Rejected because idle/abandoned is
an orthogonal concern already handled by `idle_state`. A workload can be both idle
AND undersized (its last non-idle usage was undersized). Mixing these into one enum
forces false exclusion.

**Majority rule for overall category:** When CPU says oversized and memory says
undersized, take the majority. Rejected because undersized is the safer
conservative default — it is preferable to recommend scaling up when uncertain
rather than risk OOM kills or CPU throttling.

## Consequences

### Positive

- Server-side `filter[category]` enables the UI to show focused views without
  client-side re-classification.
- Migration adds a nullable column — non-breaking for existing clients.
- Unifies category semantics across all entity types via API serialization mapping.
- Conservative "undersized wins" rule aligns with safety-first operational culture.

### Negative

- Old recommendations have `NULL` category until the engine re-runs for that
  workload (typically within one reconcile cycle).
- Adds one column per recommendation table, marginally increasing storage.

### Neutral

- `idle_state` remains unchanged and orthogonal.
- PVC/VM/GPU/quota existing classification fields remain unchanged — the API
  serialization layer maps their existing fields to the unified `category`
  response field without DB changes.
