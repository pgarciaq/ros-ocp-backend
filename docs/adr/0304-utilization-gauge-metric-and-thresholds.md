# ADR-0304: Utilization Gauges Use Peak Usage with Settings-Derived Color Thresholds

## Status

Accepted

## Phase

Phase 16+ (Visual Insights)

## Context

The Visual Insights feature (ADR-0301) adds utilization gauges to PVC and
Cluster Quota breakdown pages. The PVC data model stores three usage metrics per
day: `usage_bytes_min`, `usage_bytes_max`, `usage_bytes_avg` (no percentiles —
unlike containers). The gauge needs to display a single percentage value and
color it green/amber/red.

The recommendation engine has configurable thresholds (e.g.,
`near_full_threshold_bp` for PVC, default 8500 = 85%) stored in the settings
API. Design choices: which metric drives the gauge dial? How are color
thresholds determined?

## Decision

### 1. Gauges use peak usage (`usage_bytes_max / capacity_bytes`), not average

**Rationale:** The gauge answers "how full is this PVC?" — peak exposure is what
matters for capacity planning. This aligns with the engine's classification
logic, which uses max/capacity for `near_full` and `oversized` thresholds
(ADR-0025's asymmetric risk framing: "a full PVC causes an outage").

Using `avg` would create a mismatch: gauge shows green (65% average) while the
engine flags `near_full` because max hit 88%.

### 2. Color thresholds are derived from the settings API, not hardcoded

- Amber = `near_full_threshold_bp / 100` (default 85%)
- Red = `min(amber + 10, 100)` (default 95%)
- Green = below amber

**Rationale:** If an admin customizes their near-full threshold (e.g., to 70%
for a conservative policy), the gauge should reflect their policy, not a
hardcoded default. The +10 percentage point delta for red avoids requiring a
second configurable threshold while providing a meaningful "critical" zone above
"warning."

### 3. This pattern generalizes to all utilization gauges across entity types

- Cluster Quota gauges: same pattern (amber at 85%, red at 95%), using
  `quota_used / quota_hard` per resource.
- Future entity gauges (GPU VRAM, Node headroom) may use raw capacity fields
  without settings API coupling — document exceptions when they arise.

## Consequences

### Positive

- Gauge colors and engine recommendations are always consistent (both driven by
  the same threshold).
- Admin threshold changes automatically propagate to the UI visual without code
  changes.

### Negative

- Changing the near-full recommendation threshold has a surprising UX
  side-effect (gauge colors shift). This is intentional — the gauge is a
  visualization of the engine's assessment, not an independent UI concern.
- The +10pp red offset is not independently configurable. If this becomes a
  requirement, add a `critical_threshold_bp` to the settings API.

## Alternatives Considered

### Hardcode thresholds (70%/90% as originally proposed)

Rejected — creates mismatch between gauge colors and engine behavior when
thresholds are customized.

### Use `usage_bytes_avg`

Rejected — understates risk for bursty workloads. A PVC that averages 60% but
peaks at 92% should show amber, not green.

### Two independent configurable thresholds

Over-engineered for the current need. The derived approach (amber from settings,
red = amber + 10pp) works for all current entity types. Can be extended later.

### No color thresholds (single color gauge)

Defeats the purpose of at-a-glance health assessment.

## References

- [ADR-0301](0301-visual-insights-dashboard.md) — Visual Insights Dashboard
- [ADR-0025](0025-pvc-thresholds-20-oversized-85-near-full.md) — PVC thresholds 20% oversized / 85% near-full
