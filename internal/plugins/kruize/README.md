# Kruize legacy plugin (`kruize`)

This package registers the **legacy Kruize recommendation engine** as a named plugin.

## Behavior

- **Disabled by default.** Unlike native plugins, `kruize` is off unless listed in `ROS_ENABLED_PLUGINS` (see [`EnabledFor`](../../plugin/registry.go)).
- **Mutual exclusivity.** When `kruize` is enabled, the plugin registry returns only Kruize-backed plugins so native ingest/plugins do not run in parallel (duplicate or conflicting recommendations).
- **Marker only (PR4).** Processing still lives in `internal/services/` and `internal/utils/kruize/`. Route wiring remains driven by `cfg.UseNativeEngine` in [`internal/api/server.go`](../../../api/server.go).

## Configuration

- Enable via environment: `ROS_ENABLED_PLUGINS=kruize` (optionally alongside names that will be filtered out by exclusivity, e.g. `kruize,container` → only `kruize` is active).
- Kruize runtime behavior continues to use **`ROS_USE_NATIVE_ENGINE=false`** (or equivalent config) so the non-native path is used.

## Tests

Unit tests for [`KruizePlugin`](plugin.go) live in this directory. Registry mutual exclusivity with other built-in plugins is covered by [`internal/plugins/kruize_registry_integration_test.go`](../kruize_registry_integration_test.go) (`package plugins_test`) because blank-importing `internal/plugins` from `kruize` tests would create an import cycle.

## Future work

Migrate Kruize-specific HTTP routes from the `UseNativeEngine` branch into this plugin via [`APIProvider`](../../plugin/plugin.go).
