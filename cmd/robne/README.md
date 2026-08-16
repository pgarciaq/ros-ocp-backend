# robne CLI

Phase 1+2a+pgdigest+2d binary and samples. Parent [#99](https://github.com/pgarciaq/ros-ocp-backend/issues/99);
Phase 1 [#469](https://github.com/pgarciaq/ros-ocp-backend/issues/469);
Phase 2a [#471](https://github.com/pgarciaq/ros-ocp-backend/issues/471);
pgdigest INSERT [#463](https://github.com/pgarciaq/ros-ocp-backend/issues/463);
digest SELECT [#474](https://github.com/pgarciaq/ros-ocp-backend/issues/474);
contract [`docs/plans/robne-cli-spec.md`](../../docs/plans/robne-cli-spec.md).

```bash
make robne
./bin/robne recommend --input ./ocp_ros_usage.csv --no-user-config --format table
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
`max(bucket_date)` for Postgres `--input`). It does not slide term windows (those
stay on each container’s latest digest day). With Postgres it also sets the
inclusive SELECT end. Spec §3 / §5.

`--format json` writes a versioned envelope (`version`, `cluster_id`, `now`,
`skipped_rows`, `recommendations`) with snake_case row keys matching CSV.
`estimated_savings_cents` is JSON `null` when unset. Spec §5 / [#470](https://github.com/pgarciaq/ros-ocp-backend/issues/470).

`--output postgres://…` (or `postgresql://`) upserts today’s container `all_hours`
digests, SELECTs `[end − MaxWindowDays, end]`, then upserts recs. `--apply-schema`
on empty or behind; omit it when already at head. YAML `org_id` plus RFC 4122
`cluster_uuid` are required. `PG*` env and `--pg-url-file` keep the password off argv.

`--input postgres://…` recomputes from stored digests (no CSV). Do not pass
`--apply-schema` on that path. If `--output` is also Postgres, it must be the same
database (rec upsert only). `robne validate` stays files-only.
Spec §5 / [#471](https://github.com/pgarciaq/ros-ocp-backend/issues/471) /
[#463](https://github.com/pgarciaq/ros-ocp-backend/issues/463) /
[#474](https://github.com/pgarciaq/ros-ocp-backend/issues/474). Same container-day
is last-write-wins (not a merge of partial hours).

```bash
robne recommend --input ./ocp_ros_usage.csv --config robne.yaml \
  --output postgres://localhost:5432/robne?sslmode=disable --apply-schema

robne recommend --input postgres://localhost:5432/robne?sslmode=disable \
  --config robne.yaml --now 2026-08-07T02:00:00Z --format table
```

Shell completion: `./bin/robne completion bash` (also zsh, fish, powershell).
