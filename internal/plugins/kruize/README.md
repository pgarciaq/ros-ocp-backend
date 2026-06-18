# Kruize legacy plugin (`kruize`)

This package registers the **legacy Kruize recommendation engine** as a named plugin.

## Behavior

- **Disabled by default.** Unlike native plugins, `kruize` is off unless listed in `ROS_ENABLED_PLUGINS`.
- **Mutual exclusivity.** When `kruize` is enabled, [`plugin.Enabled()`](../../plugin/registry.go) returns only Kruize-backed plugins so native ingest hooks and CSV ingestors do not run in parallel (duplicate or conflicting recommendations).
- **Processing wiring.** CSV ingestion and HTTP routing for legacy vs native behavior key off [`plugin.EnabledFor(plugin.KruizePluginName)`](../../plugin/registry.go) (same signal this plugin uses in [`plugin.go`](plugin.go)).

## Configuration

**Preferred:** enable Kruize-only operation with:

```bash
ROS_ENABLED_PLUGINS=kruize
```

That turns off all native plugins automatically.

To use the legacy Kruize engine, set `ROS_ENABLED_PLUGINS=kruize` in your deployment
manifest. The native engine plugins will not load when Kruize is the only enabled plugin.

No dual configuration is required; use `ROS_ENABLED_PLUGINS=kruize` to select the legacy engine. (`ROS_USE_NATIVE_ENGINE` has been removed; see ADR-0157.)

## Tests

Unit tests for [`KruizePlugin`](plugin.go) live in this directory. Registry mutual exclusivity with other built-in plugins is covered by [`internal/plugins/kruize_registry_integration_test.go`](../kruize_registry_integration_test.go) (`package plugins_test`) because blank-importing `internal/plugins` from `kruize` tests would create an import cycle.

## Future work

Additional legacy routes can migrate behind [`APIProvider`](../../plugin/plugin.go) trait implementations as domains move fully under plugins.
