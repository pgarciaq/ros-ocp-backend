# robne CLI

Phase 1 binary and samples. Parent [#99](https://github.com/pgarciaq/ros-ocp-backend/issues/99);
implementation [#469](https://github.com/pgarciaq/ros-ocp-backend/issues/469);
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

`--now` is the decay/staleness clock (default: max `interval_end`). It does not slide
term windows. Spec §3. Pin it in CI; JSON includes the resolved `now`.

`--format json` writes a versioned envelope (`version`, `cluster_id`, `now`,
`skipped_rows`, `recommendations`) with snake_case row keys matching CSV.
`estimated_savings_cents` is JSON `null` when unset. Spec §5 / [#470](https://github.com/pgarciaq/ros-ocp-backend/issues/470).

Shell completion: `./bin/robne completion bash` (also zsh, fish, powershell).
