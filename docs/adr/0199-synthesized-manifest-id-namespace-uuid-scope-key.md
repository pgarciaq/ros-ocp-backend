# ADR-0199: Synthesized manifest ID namespace UUID and scope key derivation

## Status

Accepted

## Context

Legacy Kafka messages without `manifest_id` need deterministic IDs for dedup and tracking ([ADR-0050](0050-uuid-v5-deterministic-recommendation-ids.md), [ADR-0165](0165-defer-recommendations-for-synthesized-manifests.md)). Synthesis inputs must produce stable IDs across retries while distinguishing different logical batches.

## Decision

Synthesis uses **UUID v5** with a dedicated namespace UUID (distinct from the recommendation-ID namespace).

**Scope key derivation:**

1. Prefer `date=YYYY-MM-DD` from object storage keys.
2. Fall back to SHA-256 of sorted file list.

Final ID prefixed `synth-` for easy identification. Quiet period default 30s (`ROS_SYNTH_MANIFEST_QUIET_PERIOD`).

## Consequences

- Same file set + same date = same manifest ID (dedup across retries).
- Different dates = different IDs (no cross-day collision).
- `synth-` prefix enables debouncer path ([ADR-0165](0165-defer-recommendations-for-synthesized-manifests.md)).
- Namespace UUID is a constant in code (not configurable).

## Alternatives Considered

### Random UUID

No dedup on retry. Rejected.

### Hash of file contents

Requires reading all files before assigning ID. Rejected.

### Kafka offset-based

Not available in message headers for legacy payloads. Rejected.

## Related Decisions

- [ADR-0050](0050-uuid-v5-deterministic-recommendation-ids.md): UUID v5 deterministic ID pattern.
- [ADR-0165](0165-defer-recommendations-for-synthesized-manifests.md): Quiet-period debouncer for synthesized manifests.
- [ADR-0166](0166-report-file-status-manifest-gating.md): Per-file manifest gating.

## References

- [internal/services/report_file_tracker.go](../../internal/services/report_file_tracker.go)
