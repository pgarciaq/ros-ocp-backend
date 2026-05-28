# ResourceQuota Recommendations

## Overview

The `quota` plugin compares Kubernetes **ResourceQuota** hard limits and used
consumption against aggregated container recommendation totals per namespace.
It advises operators to **tighten** over-provisioned quotas or **raise** quotas
that may block scaling.

**API:** `GET /api/cost-management/v1/recommendations/openshift/quota/`

**Plugin:** Phase 1 (Produce), priority 35, enabled by default with native plugins.

## Algorithm (signal C)

For each namespace with quota hard limits on the latest `daily_namespace_digests` row:

1. Sum container `rec_*` request/limit values (`term=medium`, `engine=cost`).
2. Compare hard limits against **both** quota `used` metrics and container sums
   (utilization uses the greater of the two).
3. Recommended hard values = sum × headroom (default 120%, `ROS_QUOTA_HEADROOM_PERCENT=20`).
4. Classify:
   - `tighten` — recommended &lt; hard (capacity freed + optional dollar savings)
   - `raise` — utilization ≥ high-risk threshold (default 80%)
   - `optimal` — otherwise

## Configuration

| Variable | Default | Purpose |
|----------|---------|---------|
| `ROS_QUOTA_HEADROOM_PERCENT` | `20` | Extra margin on recommended quota (20% → 12000 basis points) |
| `ROS_QUOTA_HIGH_RISK_THRESHOLD_PERCENT` | `80` | `raise` / high risk when used or rec sum ≥ 80% of hard |
| `ROS_QUOTA_MEDIUM_RISK_THRESHOLD_PERCENT` | `60` | medium risk band |

## Data requirements

- **Hard limits:** `cpu_request_namespace_sum`, `cpu_limit_namespace_sum`,
  `memory_request_namespace_sum`, `memory_limit_namespace_sum` (operator maps to
  `kube_resourcequota{type='hard'}`).
- **Used (optional):** `cpu_request_namespace_used`, `cpu_limit_namespace_used`,
  `memory_request_namespace_used`, `memory_limit_namespace_used`.

## Query parameters

- Filters: `filter[cluster]`, `filter[project]`, `filter[recommendation_type]`, `filter[risk_level]`
- Group by: `group_by[cluster]` or `group_by[project]` (aggregated counts and savings)

See also [namespace recommendations](namespace-recommendations.md) for usage-based
namespace sizing (distinct from ResourceQuota object tuning).
