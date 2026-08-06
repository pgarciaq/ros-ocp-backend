# `_example` plugin (template)

This package is the **Phase 1 sample plugin** described as `internal/plugins/_example` in [plugin-architecture.md](../../docs/architecture/plugin-architecture.md).

**Go toolchain note:** `go build` and `go test` **skip** directories whose names start with `_`. This repository therefore keeps the compilable template at `internal/plugins/example` (import path `…/internal/plugins/example`). The stable plugin id remains `"_example"` via [`ExamplePlugin.Name`](plugin.go).

The package is **non-functional**: it registers in `init()` but [`ExamplePlugin.Enabled`](plugin.go) is always `false`, so it never appears in [`plugin.Enabled()`](../../internal/plugin/registry.go).

## How to add a plugin

**Preferred:** scaffold with the generator (live Plugin + APIProvider + RetentionProvider; other traits commented):

```bash
make new-plugin NAME=myplugin
# or: go run ./cmd/newplugin -name myplugin
```

See [Local Development](../../docs-site/development.md#adding-a-plugin) and [#410](https://github.com/pgarciaq/ros-ocp-backend/issues/410).

**Manual / from this template:**

1. Copy this directory to `internal/plugins/<yourname>/` (use a lowercase stable name; it must match `ROS_ENABLED_PLUGINS` / `ROS_DISABLED_PLUGINS` entries). Hyphenated ids are fine (`cluster-quota`); the Go package name must strip hyphens (`clusterquota`).
2. Rename `ExamplePlugin`, update [`Name()`](plugin.go), and set [`Enabled()`](plugin.go) to `plugin.EnabledFor(p.Name())` (do **not** leave the always-`false` stub).
3. Implement only the **trait interfaces** you need (see below). Real plugins typically define a struct with methods only for the traits they support — this template implements every trait as a compile-time check, which is intentional for the example only.
4. Add a blank import in [`internal/plugins/plugins.go`](../plugins.go) so `init()` runs and calls `plugin.Register`. Do **not** edit `internal/plugin/registry.go` for the plugin list (that file owns registry helpers, not the list of plugins).
5. Ship SQL as numbered files under the repo root `migrations/` directory with a `-- plugin: <yourname>` header (no per-plugin migrate subtrees).

## Trait interfaces (`internal/plugin`)

Full prose + matrix: [Plugin traits catalog](../../docs-site/plugin-reference/traits.md) (public: [traits](https://pgarciaq.github.io/ros-ocp-backend/plugin-reference/traits/)). Design / Go signatures: [Plugin Architecture §4 / §9](../../docs/architecture/plugin-architecture.md).

| Interface | Role |
|-----------|------|
| [`Plugin`](../../internal/plugin/plugin.go) | Required: stable `Name()`, `Enabled()`, and optionally `Phase()` / `Priority()` (embed [`BasePlugin`](../../internal/plugin/phases.go) for Phase 1 and priority 50). |
| [`CSVIngestor`](../../internal/plugin/plugin.go) | Own CSV parsing for one or more logical types; [`SupportedCSVTypes`](../../internal/plugin/plugin.go) + [`IngestCSV`](../../internal/plugin/plugin.go) returning [`ingestion.MetricRow`](../../internal/ingestion/models.go). |
| [`IngestHook`](../../internal/plugin/plugin.go) | Run after ingest; [`HookAfterCSVTypes`](../../internal/plugin/plugin.go) selects which CSV kinds trigger [`AfterIngest`](../../internal/plugin/plugin.go). |
| [`APIProvider`](../../internal/plugin/plugin.go) | Register Echo routes on the authenticated group via [`RegisterRoutes`](../../internal/plugin/plugin.go). |
| [`APIEnricher`](../../internal/plugin/plugin.go) | Post-process another handler’s payload with [`EnrichResponse`](../../internal/plugin/plugin.go). |
| [`RetentionProvider`](../../internal/plugin/plugin.go) | Declare [`RetentionTables`](../../internal/plugin/plugin.go) and implement [`SweepRetention`](../../internal/plugin/plugin.go). |
| [`MigrationProvider`](../../internal/plugin/plugin.go) | **Reserved** — document [`OwnedTables`](../../internal/plugin/plugin.go); not consumed by dispatch. DDL stays in root `migrations/`. |
| [`TermProvider`](../../internal/plugin/plugin.go) | Configurable short/medium/long terms via [`DefaultTerms`](../../internal/plugin/plugin.go) / [`MaxWindowDays`](../../internal/plugin/plugin.go). |

Shared dependencies today come from [`config.GetConfig()`](../../internal/config/config.go) and [`logging.GetLogger()`](../../internal/logging/logging.go) like other packages. [`plugin.PluginContext`](../../internal/plugin/context.go) is reserved for future lifecycle wiring (typed config injection) but **is not** passed by the dispatch layer yet.

## Environment variables

- `ROS_ENABLED_PLUGINS` — comma-separated allowlist (when set, only those plugins run).
- `ROS_DISABLED_PLUGINS` — comma-separated blocklist when the allowlist is unset.

The `kruize` plugin defaults **off**; when it is enabled alongside others, the registry keeps **only** `kruize` and drops native plugins (see [`plugin.Enabled`](../../internal/plugin/registry.go)).

**Execution order:** [`plugin.Enabled`](../../internal/plugin/registry.go) sorts by phase, then priority (lower first), then name. `ROS_ENABLED_PLUGINS` list order does not matter. See [plugin-phases.md](../../docs/architecture/plugin-phases.md).
