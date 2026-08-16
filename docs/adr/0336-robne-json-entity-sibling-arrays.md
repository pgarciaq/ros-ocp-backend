# ADR-0336: robne JSON envelope uses per-entity sibling arrays

## Status

Accepted

## Phase

CLI 2b ([#472](https://github.com/pgarciaq/ros-ocp-backend/issues/472))

## Context

[#470](https://github.com/pgarciaq/ros-ocp-backend/issues/470) froze `robne recommend --format json` as a versioned envelope whose `recommendations` array is **container** rows (`containerOut`). Phase 3 `robne diff` ([#480](https://github.com/pgarciaq/ros-ocp-backend/issues/480)) consumes that shape.

[#472](https://github.com/pgarciaq/ros-ocp-backend/issues/472) adds other-entity CSVs. Namespace recs are a different DTO (no workload/container/idle_state). Two shapes in one array would force a tagged union or omitted fields that break CSV headers and confuse `jq '.recommendations[]'`.

## Decision

Keep `recommendations` as the **container-only** array (always present, never `null`).

When `--plugins` includes `namespace`, bump `version` to **2** and emit sibling `namespace_recommendations` (always an array, never `null`). Container-only runs stay **`version` 1** with no sibling key so existing goldens and Phase 3 diffs of container envelopes stay valid.

`--format csv` and `table` stay one entity per stream. Mixing any of container, namespace, node, GPU, PVC, VM, quota, cluster_quota, and snapshot requires `--format json`.

Later entities add further sibling arrays and keep bumping `version` when that plugin is on. Node is **3** (`node_recommendations`); GPU is **4** (`gpu_recommendations` and `gpu_timeslicing_recommendations`); PVC is **5** (`pvc_recommendations`); VM is **6** (`vm_recommendations`). VM timeslicing is a column on each VM row (`recommended_time_slice_count`), not a second sibling (GPU’s node×model timeslicing array is a different grain). Quota is **7** (`quota_recommendations`). Cluster quota is **8** (`cluster_quota_recommendations`). Snapshot is **9** (`snapshot_recommendations`, [#478](https://github.com/pgarciaq/ros-ocp-backend/issues/478)). Do not stuff mixed entity rows into `recommendations`.

CLI-owned DTOs (`containerOut`, `namespaceOut`, `nodeOut`, `gpuOut` / `gpuTimeslicingOut`, `pvcOut`, `vmOut`, `quotaOut`, `clusterQuotaOut`, `snapshotOut`). Do not add CLI `json` tags on engine rec types (`vm.VMRecommendation` already has some GPU field tags in librobne — do not add more).

## Consequences

### Positive

- Container `jq` and CSV headers do not change.
- Each entity can grow columns without a discriminated union.
- Explicit `--plugins container` output is still envelope v1. Implicit default is **all shipped plugins** (skip missing files / empty Path A tables).

### Negative

- Consumers that want every entity must read multiple keys.
- `version` is no longer a single global constant for every run.

### Neutral

- `--output postgres://` upserts recs for shipped 2b plugins ([#473](https://github.com/pgarciaq/ros-ocp-backend/issues/473)) and INSERTs other-entity daily digests ([#481](https://github.com/pgarciaq/ros-ocp-backend/issues/481)) — not snapshot. `--input postgres://` SELECTs stored days for listed plugins ([#482](https://github.com/pgarciaq/ros-ocp-backend/issues/482)) and prints those recs on stdout; empty own-table SELECT is an error when the plugin is **explicit**; implicit default skips. Path A does not SELECT snapshot (explicit `--plugins snapshot` is a hard error). YAML entity settings blocks stay reserved.

## References

- [CLI spec §5](../plans/robne-cli-spec.md)
- [#470](https://github.com/pgarciaq/ros-ocp-backend/issues/470), [#472](https://github.com/pgarciaq/ros-ocp-backend/issues/472), [#478](https://github.com/pgarciaq/ros-ocp-backend/issues/478)
- [ADR-0305](0305-robne-cli-standalone-binary.md)
