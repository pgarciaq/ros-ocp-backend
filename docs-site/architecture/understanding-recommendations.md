# Understanding Your Recommendations

> **Last verified:** 2026-08-05

This page explains **why** the native ROS engine produces each recommendation type.
It is written for platform engineers and cluster administrators — not engine developers.

For implementation details, see [ADR-0296](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0296-recommendation-explanation-factors-typed-columns.md)
and the [Recommendation Engines](recommendation-engines.md) reference.

## How to read explanation data

Detail endpoints accept an optional query parameter:

```http
GET /recommendations/openshift/{uuid}?include=explanation
```

When requested, the response includes a nested `explanation` object alongside each
recommendation engine block. Explanation is **opt-in** — default responses are unchanged.

Explanation factors are computed **at recommendation time** and stored in typed database
columns (`expl_*`). They reflect the values that actually drove the persisted
recommendation, not a live recompute from digests.

If explanation columns are NULL (rows created before this feature shipped, or before
backfill completes), the `explanation` object may be absent even when requested.

## Confidence score

The `confidence_level` field (0.0–1.0) measures data completeness:

```
confidence_level = data_days / window_days   (capped at 1.0)
```

- **High (≥ 0.85):** Most of the analysis window has telemetry.
- **Medium (0.5–0.85):** Partial window — recommendations are directionally correct but may shift as more data arrives.
- **Low (< 0.5):** Sparse data — treat as indicative only.

`data_days` in the explanation object shows how many days contributed to the term window.

## Container CPU and memory

The cost engine sizes for savings; the performance engine sizes for headroom.

| Factor | Cost engine | Performance engine |
|--------|-------------|-------------------|
| CPU percentile | P60 (decay-weighted) | P98 |
| Memory percentile | P95 | P100 (max) |

Additional factors in `explanation`:

- **Adaptive margin** (`cpu_adaptive_margin_basis_points`, `mem_adaptive_margin_basis_points`) — extra headroom when usage is volatile (high P95/P50 ratio). 10,000 basis points = 100%.
- **OOM bump** (`oom_count_sum`, `oom_bump_applied`) — memory request increased when OOM events were observed in the window.
- **CPU floor** (`cpu_floor_applied`) — minimum 25 mCPU applied.
- **Idle** (`is_idle`) — container classified as idle; recommendation may reflect idle policy.

### Why is my memory recommendation much higher than current usage?

Common causes:

1. **OOM events** triggered a logarithmic bump on top of the P95 baseline.
2. **Performance engine** uses P100 memory (peak), not average usage.
3. **Adaptive margin** widened because daily usage was spiky (P95 ≫ P50).
4. **Current request** is below observed usage — the recommendation reflects usage, not the current YAML.

## Namespace recommendations

Namespace rows aggregate container digests across all workloads in the project.
Explanation columns describe the **aggregated window**, using the same factor names
as container recommendations.

## Node CPU and memory

Node recommendations right-size cluster capacity based on observed pod scheduling
and utilization:

- **Target utilization** — cost engine targets ~80% CPU; performance ~55%.
- **Sizing formula** — which branch fired: `target_util`, `headroom_2x`, or `idle`.
- **Consolidation** — whether instance-type consolidation changed the suggestion.
- **Headroom / imbalance** — pod scheduling pressure and CPU/memory imbalance signals.

## PVC / storage

PVC recommendations classify volumes as orphaned, oversized, near-full, or healthy.

Explanation includes configured thresholds and the classification reason. Existing
columns `usage_ratio` and `growth_bytes_per_day` are assembled into `explanation`
without duplication.

## Quota and cluster resource quota

Quota recommendations compare hard limits, observed usage, and aggregated child
recommendations:

- **Headroom** — basis points added above the computed base.
- **Risk level** — derived band (e.g. tight, optimal, underutilized).
- **Recommendation reason** — branch taken: `tighten`, `raise`, or `optimal`.

## Virtual machines

VM explanation captures sizing branch (`abandoned`, `idle`, `active_downsize`, `active`),
margins, hysteresis (downsize blocked), and whether guest-agent memory was used.

## GPU (container MIG)

GPU explanation includes utilization averages, recommended MIG profile, and whether
the workload is memory-bound. Values are persisted at classification time.

## GPU time-slicing (node level)

Node time-slicing explanation includes candidate/impacted container counts and the
classification rule that qualified the node for replica sharing.

## Volume snapshots

Snapshot recommendations are rule-based (not percentile sizing). Explanation includes
which threshold fired (`orphan_age_days`, `stale_age_days`, etc.) and a human-readable
classification rule. Existing fields (`age_days`, `source_pvc_exists`, `restored_pvc_count`)
are included in `explanation` from their canonical columns.

## Related documentation

- [Recommendation Engines](recommendation-engines.md) — algorithm reference
- [Recommendation Math](recommendation-math.md) — percentile and margin formulas
- [UI Integration Guide](../ui-integration-guide.md) — frontend `?include=explanation` usage
- [Query Parameters](../plugin-reference/query-parameters.md) — `include` parameter reference
