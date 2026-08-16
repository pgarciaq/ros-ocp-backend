# robne CLI

Phase 1+2a+pgdigest+2d+2b-stdout+2c rec-upsert+other-entity digest INSERT+Path A SELECT+snapshot-stdout binary and samples. Parent [#99](https://github.com/pgarciaq/ros-ocp-backend/issues/99);
Phase 1 [#469](https://github.com/pgarciaq/ros-ocp-backend/issues/469);
Phase 2a [#471](https://github.com/pgarciaq/ros-ocp-backend/issues/471);
pgdigest INSERT [#463](https://github.com/pgarciaq/ros-ocp-backend/issues/463);
digest SELECT [#474](https://github.com/pgarciaq/ros-ocp-backend/issues/474);
namespace + node/GPU + PVC + VM + quota + cluster_quota files → stdout [#472](https://github.com/pgarciaq/ros-ocp-backend/issues/472) (remaining 2b under #472 is none); other-entity rec PG [#473](https://github.com/pgarciaq/ros-ocp-backend/issues/473) **shipped**; other-entity digest INSERT [#481](https://github.com/pgarciaq/ros-ocp-backend/issues/481) **shipped**; other-entity Path A SELECT [#482](https://github.com/pgarciaq/ros-ocp-backend/issues/482) **shipped**; snapshot stdout [#478](https://github.com/pgarciaq/ros-ocp-backend/issues/478) **shipped**; Phase 3 [#480](https://github.com/pgarciaq/ros-ocp-backend/issues/480);
contract [`docs/plans/robne-cli-spec.md`](../../docs/plans/robne-cli-spec.md).

```bash
make robne
./bin/robne recommend --input ./ocp_ros_usage.csv --plugins container --no-user-config --format table
./bin/robne recommend --input ./csvs/ --plugins namespace --no-user-config --format json
./bin/robne recommend --input ./ocp_ros_usage.csv --plugins node --no-user-config --format json
./bin/robne recommend --input ./ocp_ros_usage.csv --plugins gpu --no-user-config --format json
./bin/robne recommend --input ./ocp_storage_usage.csv --plugins pvc --no-user-config --format json
./bin/robne recommend --input ./ocp_ros_vm_usage.csv --plugins vm --no-user-config --format json
./bin/robne recommend --input ./ocp_ros_namespace_usage.csv --plugins quota --no-user-config --format json
./bin/robne recommend --input ./ocp_ros_cluster_quota.csv --plugins cluster_quota --no-user-config --format json
./bin/robne recommend --input ./ocp_snapshot_inventory.csv --plugins snapshot --no-user-config --format json
./bin/robne validate --input ./metrics.tar.gz --no-user-config
```

| File | Copy to |
|------|---------|
| `robne.yaml.sample` | `./robne.yaml` or `~/.config/robne/robne.yaml` |
| `rate-card.json.sample` | `./rate-card.json` or `~/.config/robne/rate-card.json` |

**Overlay:** at most one user file (first of XDG / `~/.config/robne/` / `~/.*`) plus
cwd or `--config` / `--rate-card`. YAML **replaces whole top-level keys**. A project
`sizing:` must repeat every sizing field (or omit the key); a partial block is an error.
Rate card **merges by cluster id** (later file replaces that cluster object).
`ROBNE_NO_USER_CONFIG=1` skips home files.

Public page: [`docs-site/features/robne-cli.md`](../../docs-site/features/robne-cli.md)
(section *Config overlay*). Contract: [`docs/plans/robne-cli-spec.md`](../../docs/plans/robne-cli-spec.md) §§2, 3, and 6.

`--now` is the decay/staleness clock (default: max `interval_end` for files, or
`MaxAnyDigestDate` for Postgres `--input`). Path B after file INSERT still uses
container `max(bucket_date)` for the container recompute window. It does not slide term windows (those
stay on each container’s latest digest day). With Postgres it also sets the
inclusive SELECT end. Spec §3 / §5.

`--format json` writes a versioned envelope (`version`, `cluster_id`, `now`,
`skipped_rows`, `recommendations`) with snake_case row keys matching CSV.
`estimated_savings_cents` is JSON `null` when unset. Spec §5 / [#470](https://github.com/pgarciaq/ros-ocp-backend/issues/470).
`--plugins namespace` (or YAML `plugins` including `namespace`) parses NISE
`*ocp_ros_namespace_usage.csv` and operator `ros-openshift-namespace-*.csv`,
bumps `version` to **2**, and adds sibling `namespace_recommendations` (always an
array, never `null`). `--plugins node` / `gpu` read the **same container ROS
CSV** (optional allocatable/DCGM columns), bump `version` to **3** / **4**, and
add `node_recommendations` or `gpu_recommendations` plus
`gpu_timeslicing_recommendations`. `--plugins pvc` parses NISE
`*ocp_storage_usage.csv` / operator `ros-openshift-storage-*.csv` /
`cm-openshift-storage-usage`, bumps `version` to **5**, and adds
`pvc_recommendations`. YAML `pvc:` stays reserved. `--plugins vm` parses
`*ocp_ros_vm_usage.csv` / `ros-openshift-vm-usage-*` (classified before
`ocp_ros_usage`), optional pvc/gpu companions degrade if missing or malformed,
bumps `version` to **6**, and adds `vm_recommendations`. Timeslicing is a column
on the VM row, not a second sibling. YAML `vm:` stays reserved. `--plugins quota`
reads the **same namespace ROS CSV** (optional `quota_name`; named-quota sums are
ResourceQuota hard), bumps `version` to **7**, and adds `quota_recommendations`.
YAML `quota:` stays reserved. `--plugins cluster_quota` parses NISE
`*ocp_ros_cluster_quota.csv` / operator `ros-openshift-cluster-quota-*`
(classified before namespace), bumps `version` to **8**, and adds
`cluster_quota_recommendations`. Empty `namespaces` sums all in-memory
namespace quota recs; memory is **bytes**. YAML `cluster_quota:` stays reserved.
`--plugins snapshot` parses NISE `ocp_snapshot_inventory` / operator
`ros-openshift-snapshot-*` / `cm-openshift-snapshot-inventory` (classified before
blanket `cm-openshift-*`), bumps `version` to **9**, and adds
`snapshot_recommendations`. YAML `snapshot:` stays reserved. Files-only (no PG
persist / no Path A SELECT). Default
`--plugins` is **all shipped plugins** (omit the flag and YAML `plugins:`). Implicit
default skips missing dedicated CSVs / empty Path A tables; an explicit list errors.
CSV/table are one entity per stream; mixing requires JSON (a container ROS file also
enables `node` — pin `--plugins container` for table/csv). `--output postgres://` upserts recs
for shipped 2b plugins ([#473](https://github.com/pgarciaq/ros-ocp-backend/issues/473))
and INSERTs other-entity daily digests ([#481](https://github.com/pgarciaq/ros-ocp-backend/issues/481)). `--input postgres://` SELECTs stored days for listed plugins ([#474](https://github.com/pgarciaq/ros-ocp-backend/issues/474) containers, [#482](https://github.com/pgarciaq/ros-ocp-backend/issues/482) other entities); empty own-table SELECT is an error when the plugin is **explicit**; implicit default skips. Explicit `--plugins snapshot` with Path A is a hard error. YAML `node:` / `gpu:` / `pvc:` / `vm:` / `quota:` / `cluster_quota:` / `snapshot:` stay reserved (plugins are unlocked). [#472](https://github.com/pgarciaq/ros-ocp-backend/issues/472) / [#478](https://github.com/pgarciaq/ros-ocp-backend/issues/478) / [ADR-0336](../../docs/adr/0336-robne-json-entity-sibling-arrays.md).

`--output postgres://…` (or `postgresql://`) upserts today’s container `all_hours`
digests and other-entity daily rows (namespace, node, GPU, PVC, VM + GPU devices,
quota, cluster quota), SELECTs `[end − MaxWindowDays, end]` for listed plugins,
then upserts recs for containers
and shipped 2b plugins (namespace, node, GPU MIG + time-slicing, PVC, VM, quota,
cluster_quota). Other-entity days are last-write-wins (not ingest merge).
`--apply-schema`
on empty or behind; omit it when already at head. YAML `org_id` plus RFC 4122
`cluster_uuid` are required. `PG*` env and `--pg-url-file` keep the password off argv.

`--input postgres://…` recomputes from stored digests for listed plugins (no CSV).
Do not pass `--apply-schema` on that path. If `--output` is also Postgres, it must
be the same database (rec upsert only; no digest INSERT on Path A). Nested quota/CRQ
reconstruct supporting container and quota days even when those plugins are off.
`robne validate` stays files-only.
Spec §5 / [#471](https://github.com/pgarciaq/ros-ocp-backend/issues/471) /
[#463](https://github.com/pgarciaq/ros-ocp-backend/issues/463) /
[#474](https://github.com/pgarciaq/ros-ocp-backend/issues/474) /
[#481](https://github.com/pgarciaq/ros-ocp-backend/issues/481) /
[#482](https://github.com/pgarciaq/ros-ocp-backend/issues/482). Same container-day
(and other-entity day) is last-write-wins (not a merge of partial hours).

```bash
robne recommend --input ./ocp_ros_usage.csv --config robne.yaml \
  --output postgres://localhost:5432/robne?sslmode=disable --apply-schema

robne recommend --input postgres://localhost:5432/robne?sslmode=disable \
  --config robne.yaml --now 2026-08-07T02:00:00Z --plugins container --format table
```

Shell completion: `./bin/robne completion bash` (also zsh, fish, powershell).
