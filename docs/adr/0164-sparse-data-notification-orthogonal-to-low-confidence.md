# ADR-0164: Add SPARSE_DATA notification orthogonal to LOW_CONFIDENCE

## Status

Accepted

## Context

The native engine generates recommendations even with very sparse data (e.g., a
single 15-minute metric interval). The existing LOW_CONFIDENCE notification
(code 1) fires based on *relative* window coverage — it compares data days
against the term's window size (`confidence_level < 0.5`). However, for the
short term with `WindowDays: 1` and `MinDataDays: 1`, a single day of data
yields `confidence = 1.0`, so LOW_CONFIDENCE never fires despite the data being
objectively sparse.

This gap became visible when comparing native engine output to legacy Kruize:
Kruize silently drops containers without enough data (its internal minimum is
higher than one interval), while the native engine produces recommendations —
a deliberate and correct design choice. But users receiving those recommendations
have no signal that the recommendation is based on a very small observation
window.

## Decision

Add a new notification code **SPARSE_DATA (code 77)** that fires based on
*absolute* data volume, orthogonal to LOW_CONFIDENCE.

### SPARSE_DATA vs LOW_CONFIDENCE

These two notifications answer different questions:

| | LOW_CONFIDENCE (code 1) | SPARSE_DATA (code 77) |
|---|---|---|
| **Question** | How reliable is this recommendation *relative to the term's window*? | Is there enough *absolute* data to trust any recommendation? |
| **Trigger** | `confidence_level < 0.5` (data covers less than half the term window) | `data_days <= sparse_data_threshold` (default 2) |
| **Severity** | WARNING | INFO |

A recommendation can be in any of four states:

- **Neither**: 10 days of data in a 15-day window — plenty of data, good coverage
- **SPARSE_DATA only**: 1 day in a 1-day window — full coverage but objectively sparse
- **LOW_CONFIDENCE only**: 3 days in a 15-day window — not sparse, but poor window coverage
- **Both**: 1 day in a 7-day window — sparse AND low window coverage

### Threshold configurability

`sparse_data_threshold` is added to `SizingThresholdSettings` (default: 2),
following the existing three-tier precedence (env lock → tenant DB → default).
Admin env vars: `ROS_CONTAINER_SPARSE_DATA_THRESHOLD`,
`ROS_NAMESPACE_SPARSE_DATA_THRESHOLD`. Valid range: 1–30.

Node and PVC evaluators use the compiled default (`defaultSparseDataThreshold = 2`)
since they don't use `SizingThresholdSettings`.

### Scope

SPARSE_DATA fires on container, namespace, node, and PVC recommendations.
GPU recommendations are excluded (they use a separate tiered confidence model).

## Alternatives Considered

### Repurpose LOW_CONFIDENCE with an absolute threshold

Change LOW_CONFIDENCE's trigger from relative (confidence vs window) to absolute
(data days). Rejected because:

- Loses the relative signal — a medium-term rec with 2/7 days is "low confidence"
  for a different reason than a short-term rec with 1/1 day
- `LowConfidenceThreshold` is already a configurable per-tenant setting exposed
  via the Settings API; changing its semantics silently changes customer-tuned
  behavior
- The migration seed says "Less than 4 days of data available" — changing it is
  a behavioral breaking change for existing consumers

### Lower the short-term MinDataDays to require more data

Increase `MinDataDays` for the short term from 1 to 2+. Rejected because:

- Users explicitly chose a 1-day window for fast feedback on new workloads
- Blocking recommendations delays visibility without clear benefit
- The notification approach gives information without removing recommendations

### Add a confidence penalty for sparse data

Reduce `confidence_level` when data is sparse so LOW_CONFIDENCE fires. Rejected
because it conflates two independent quality dimensions — window coverage and
absolute data volume — making thresholds harder to reason about.

## Consequences

- Users see an informational signal when recommendations are based on very few
  data points, even when window coverage is high
- LOW_CONFIDENCE behavior is unchanged; existing tenant tuning is preserved
- Notification catalog endpoints automatically include code 77 for container,
  namespace, node, and PVC plugins
- UI can filter or style SPARSE_DATA differently from WARNING-severity
  notifications (it's INFO severity)

## References

- [Notification codes architecture](../architecture/notification-codes.md)
- [Configurability — threshold settings](../architecture/configurability.md)
- Migration: `000143_add_sparse_data_notification.up.sql`
