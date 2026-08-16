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
cwd or `--config` / `--rate-card`. YAML **replaces whole top-level keys** (a project
`sizing:` drops user `sizing:` keys not repeated). Rate card **merges by cluster id**
(later file replaces that cluster object). `ROBNE_NO_USER_CONFIG=1` skips home files.

Public page: [`docs-site/planned-features/robne-cli.md`](../../docs-site/planned-features/robne-cli.md)
(section *Config overlay*). Contract: [`docs/plans/robne-cli-spec.md`](../../docs/plans/robne-cli-spec.md) §§2 and 6.
