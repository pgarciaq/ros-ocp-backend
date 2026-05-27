# `_example` plugin (template)

This package is the **Phase 1 sample plugin** described as `internal/plugins/_example` in [plugin-architecture.md](../../docs/architecture/plugin-architecture.md).

**Go toolchain note:** `go build` and `go test` **skip** directories whose names start with `_`. This repository therefore keeps the compilable template at `internal/plugins/example` (import path `…/internal/plugins/example`). The stable plugin id remains `"_example"` via [`ExamplePlugin.Name`](plugin.go).

The package is **non-functional**: it registers in `init()` but [`ExamplePlugin.Enabled`](plugin.go) is always `false`, so it never appears in [`plugin.Enabled()`](../../internal/plugin/registry.go).

## How to add a plugin

1. Copy this directory to `internal/plugins/<yourname>/` (use a lowercase stable name; it must match `ROS_ENABLED_PLUGINS` / `ROS_DISABLED_PLUGINS` entries).
2. Rename `ExamplePlugin`, update [`Name()`](plugin.go), and set [`Enabled()`](plugin.go) to `plugin.EnabledFor("<yourname>")` unless you need custom rules.
3. Implement only the **trait interfaces** you need (see below). Real plugins typically define a struct with methods only for the traits they support.
4. Add a blank import in the relevant `main` package when you are ready for production registration, e.g. `_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins/<yourname>"`.
5. Ship SQL as numbered files under the repo root `migrations/` directory with a `-- plugin: <yourname>` header (no per-plugin migrate subtrees).

## Trait interfaces (`internal/plugin`)

| Interface | Role |
|-----------|------|
| [`Plugin`](../../internal/plugin/plugin.go) | Required: stable `Name()`, `Enabled()`, and optionally `Phase()` / `Priority()` (embed [`BasePlugin`](../../internal/plugin/phases.go) for Phase 1 and priority 50). |
| [`CSVIngestor`](../../internal/plugin/plugin.go) | Own CSV parsing for one or more logical types; [`SupportedCSVTypes`](../../internal/plugin/plugin.go) + [`IngestCSV`](../../internal/plugin/plugin.go) returning [`ingestion.MetricRow`](../../internal/ingestion/models.go). |
| [`IngestHook`](../../internal/plugin/plugin.go) | Run after ingest; [`HookAfterCSVTypes`](../../internal/plugin/plugin.go) selects which CSV kinds trigger [`AfterIngest`](../../internal/plugin/plugin.go). |
| [`APIProvider`](../../internal/plugin/plugin.go) | Register Echo routes on the authenticated group via [`RegisterRoutes`](../../internal/plugin/plugin.go). |
| [`APIEnricher`](../../internal/plugin/plugin.go) | Post-process another handler’s payload with [`EnrichResponse`](../../internal/plugin/plugin.go). |
| [`RetentionProvider`](../../internal/plugin/plugin.go) | Declare [`RetentionTables`](../../internal/plugin/plugin.go) and implement [`SweepRetention`](../../internal/plugin/plugin.go). |
| [`MigrationProvider`](../../internal/plugin/plugin.go) | Document [`OwnedTables`](../../internal/plugin/plugin.go) for DDL owned by this domain. |

Shared dependencies today come from [`config.GetConfig()`](../../internal/config/config.go) and [`logging.GetLogger()`](../../internal/logging/logging.go) like other packages. [`plugin.PluginContext`](../../internal/plugin/context.go) is reserved for future lifecycle wiring (typed config injection) but **is not** passed by the dispatch layer yet.

## Environment variables

- `ROS_ENABLED_PLUGINS` — comma-separated allowlist (when set, only those plugins run).
- `ROS_DISABLED_PLUGINS` — comma-separated blocklist when the allowlist is unset.

The `kruize` plugin defaults **off**; when it is enabled alongside others, the registry keeps **only** `kruize` and drops native plugins (see [`plugin.Enabled`](../../internal/plugin/registry.go)).

**Execution order:** [`plugin.Enabled`](../../internal/plugin/registry.go) sorts by phase, then priority (lower first), then name. `ROS_ENABLED_PLUGINS` list order does not matter. See [plugin-phases.md](../../docs/architecture/plugin-phases.md).
