# ADR-0336: robne JSON envelope uses per-entity sibling arrays

## Status

Accepted

## Phase

CLI 2b ([#472](https://github.com/pgarciaq/ros-ocp-backend/issues/472))

## Context

[#470](https://github.com/pgarciaq/ros-ocp-backend/issues/470) froze `robne recommend --format json` as a versioned envelope whose `recommendations` array is **container** rows (`containerOut`). Phase 3 `robne diff` consumes that shape.

[#472](https://github.com/pgarciaq/ros-ocp-backend/issues/472) adds other-entity CSVs. Namespace recs are a different DTO (no workload/container/idle_state). Two shapes in one array would force a tagged union or omitted fields that break CSV headers and confuse `jq '.recommendations[]'`.

## Decision

Keep `recommendations` as the **container-only** array (always present, never `null`).

When `--plugins` includes `namespace`, bump `version` to **2** and emit sibling `namespace_recommendations` (always an array, never `null`). Container-only runs stay **`version` 1** with no sibling key so existing goldens and Phase 3 diffs of container envelopes stay valid.

`--format csv` and `table` stay one entity per stream. Mixing any of container, namespace, node, and GPU requires `--format json`.

Later 2b entities (PVC, VM, quota, …) add further sibling arrays and keep bumping `version` when that plugin is on. Node is **3** (`node_recommendations`); GPU is **4** (`gpu_recommendations` and `gpu_timeslicing_recommendations`). Do not stuff mixed entity rows into `recommendations`.

CLI-owned DTOs (`containerOut`, `namespaceOut`, `nodeOut`, `gpuOut` / `gpuTimeslicingOut`). Do not add `json` tags on engine rec types.

## Consequences

### Positive

- Container `jq` and CSV headers do not change.
- Each entity can grow columns without a discriminated union.
- Default `--plugins container` output is still envelope v1.

### Negative

- Consumers that want every entity must read multiple keys.
- `version` is no longer a single global constant for every run.

### Neutral

- `--output postgres://` remains container persist until [#473](https://github.com/pgarciaq/ros-ocp-backend/issues/473). File-only plugins plus `--output` warn on stderr and still print those recs on stdout.

## References

- [CLI spec §5](../plans/robne-cli-spec.md)
- [#470](https://github.com/pgarciaq/ros-ocp-backend/issues/470), [#472](https://github.com/pgarciaq/ros-ocp-backend/issues/472)
- [ADR-0305](0305-robne-cli-standalone-binary.md)
