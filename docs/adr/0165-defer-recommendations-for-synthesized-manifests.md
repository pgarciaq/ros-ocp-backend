# ADR-0165: Defer recommendation engines for synthesized manifests until quiet period expires

## Status

Accepted

## Context

Legacy Kafka messages may omit `metadata.manifest_id`. Finding #32 (adversarial review v2.0) added UUID v5 synthesis of deterministic `synth-*` manifest IDs in `report_file_tracker.go` (same deterministic-ID pattern as [ADR-0050](0050-uuid-v5-deterministic-recommendation-ids.md), but for manifest tracking—not recommendation primary keys).

When multiple CSV files share a synthesized manifest ID, registering each file could trigger recommendation engines on partial data. Running engines after the first file produces recommendations from incomplete ingest batches.

Real manifest IDs supplied by the publisher represent a complete, intentional batch boundary and do not need debouncing.

## Decision

Defer recommendation engine execution for synthesized (`synth-*`) manifest IDs using a quiet-period timer keyed by manifest ID.

- On each new file registration for a synthesized manifest, reset the timer.
- Only after `ROS_SYNTH_MANIFEST_QUIET_PERIOD` (default 30s) of silence with no new file activity does the engine fire.
- Real (non-synthetic) manifest IDs bypass the debouncer and run recommendations immediately.

### Implementation notes

- A per-manifest **generation counter** increments on every timer reset; stale `time.AfterFunc` callbacks compare generation and exit early, preventing double-firing from the classic Go `timer.Stop()` race.
- The debouncer registers via `asyncjobs.RegisterShutdownHook()` (`ShutdownSynthManifestDebouncers`) for graceful drain on SIGTERM; shutdown sets a flag and bumps generation so pending callbacks are skipped.
- `InitSynthManifestDebouncer` wires debouncer lifecycle to the processor or API parent context.
- Prometheus metric `rosocp_manifest_recommendation_deferred_total` tracks deferral frequency.

## Alternatives Considered

### Run immediately on each file

Simplest path—no timer state—but produces incorrect recommendations when files arrive over seconds or minutes for the same legacy payload scope.

### Fixed delay without reset

A one-shot delay after the first file might fire before slow uploads finish when files arrive gradually across the quiet window.

### Batch by time window

Aggregate all files in a sliding window before triggering. Similar outcome to quiet-period debouncing but requires window bookkeeping and duplicate detection; the reset-on-activity timer achieves the same goal with less state.

## Consequences

- Adds ~30s latency for synthesized manifests only; real manifest IDs are unaffected.
- Prevents incorrect recommendations from partial multi-file ingest.
- Adds complexity: debouncer goroutines per active synthesized manifest, generation counter, shutdown hook.
- Operators can tune latency via `ROS_SYNTH_MANIFEST_QUIET_PERIOD`.

## Related Decisions

- [ADR-0050](0050-uuid-v5-deterministic-recommendation-ids.md): UUID v5 deterministic ID pattern; manifest synthesis uses a distinct namespace from recommendation IDs.
- [ADR-0162](0162-housekeeper-graceful-shutdown.md): Graceful shutdown patterns; debouncer uses `asyncjobs` shutdown hooks in API/processor mode.
- [ADR-0088](0088-kafka-s3-pipeline-both-modes.md): Kafka ingest pipeline and manifest-scoped file tracking.

## References

- [internal/services/manifest_recommendation_debouncer.go](../../internal/services/manifest_recommendation_debouncer.go)
- [internal/services/report_file_tracker.go](../../internal/services/report_file_tracker.go)
