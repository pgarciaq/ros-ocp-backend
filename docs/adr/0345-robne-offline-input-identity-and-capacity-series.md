# ADR-0345: robne offline input identity and capacity series assembly

## Status

Proposed

## Phase

robne CLI #700 (HCP capacity support; design under owner review)

## Context

`robne recommend` runs offline on hand-copied KMO payloads (a tarball
or a reports directory). Two gaps appear once the HCP fleet-capacity
CSV enters the CLI:

1. **No identity in the rows.** The capacity CSV deliberately carries no
   cluster id (the backend binds it from trusted Kafka metadata, which
   the CLI never sees). For capacity-only input the existing resolver
   (`resolveClusterIDFromLoad`) finds nothing: container/namespace rows
   are the only csvID source today.
2. **No series concept.** The KMO monthly file
   (`ros-openshift-hcp-capacity-YYYYMM.csv`) accumulates hourly rows
   across uploads, and `writeReport` dedups by full row *string* — so a
   recollected window with changed values appends a second row with the
   same `observed_at`. The backend absorbs this via `ON CONFLICT DO
   UPDATE` (last-wins); the CLI needs a deterministic rule.

## Decision

**Identity.** The manifest fills the csvID slot when rows carry no ids;
then the existing `resolveClusterIDs` decides unchanged: YAML
`cluster_uuid` overrides, and manifest-vs-YAML disagreement is the same
hard error (reworded to say manifest). Manifest-vs-row mismatch stays a
stderr warning (tripwire for hand-copied payloads), not an error — the
offline support flow should advise with a warning rather than refuse.
No manifest and no YAML renders the `"unknown"` sibling label (the
envelope `cluster_id` stays empty, as today).

**Series.** All capacity CSVs in one input merge (single-cluster rule);
files in sorted filename order; stable sort newest-first; dedup by
`observed_at` keeping the LAST occurrence in read order — converging
with backend last-write-wins. Duplicates arise only from overlapping
inputs or recollected KMO windows. Latest = series[0], independent of
`--now` (age surfaces via the staleness warning); table/CSV rendering,
the explain default, and diff all consume it.

## Consequences

### Positive

- Capacity-only input works with no new flags; identity comes from the
  artifact the operator already ships.
- CLI-vs-service derivations agree on identical inputs; the residual
  divergences are the clock and month-boundary truncation (locked in
  #700).
- Misattributed payloads warn instead of silently advising the wrong
  fleet.

### Negative

- Overlapping inputs (two payloads merged in one dir) silently merge;
  single-cluster-per-input remains a user-upheld invariant.
- Duplicate-`observed_at` rows resolve deterministically but without
  provenance of which write won.

## Alternatives Considered

### Manifest-first with YAML fallback

Rejected — inverts the established `resolveClusterIDs` precedence (YAML
overrides file content) for no offline-specific reason; explicit local
config stays the override.

### First-wins dedup

Rejected — prefers the older write on recollects, diverging from the
backend's last-wins convergence.

## References

- [#700](https://github.com/pgarciaq/ros-ocp-backend/issues/700),
  [design](../superpowers/specs/2026-10-26-robne-hcp-capacity-design.md)
- `resolveClusterIDs` (`cmd/robne/ratecard.go`)
- KMO `writeReport` / `readCSV` (koku-metrics-operator
  `internal/collector/report.go`)
- [ADR-0305](0305-robne-cli-standalone-binary.md),
  [ADR-0336](0336-robne-json-entity-sibling-arrays.md)
