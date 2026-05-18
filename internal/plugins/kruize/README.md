# Kruize legacy plugin (`kruize`)

This package registers the **legacy Kruize recommendation engine** as a named plugin.

## Behavior

- **Disabled by default.** Unlike native plugins, `kruize` is off unless listed in `ROS_ENABLED_PLUGINS` or activated via the deprecated compat flag (see below).
- **Mutual exclusivity.** When `kruize` is enabled, [`plugin.Enabled()`](../../plugin/registry.go) returns only Kruize-backed plugins so native ingest hooks and CSV ingestors do not run in parallel (duplicate or conflicting recommendations).
- **Processing wiring.** CSV ingestion and HTTP routing for legacy vs native behavior key off [`plugin.EnabledFor(plugin.KruizePluginName)`](../../plugin/registry.go) (same signal this plugin uses in [`plugin.go`](plugin.go)).

## Configuration

**Preferred:** enable Kruize-only operation with:

```bash
ROS_ENABLED_PLUGINS=kruize
```

That turns off all native plugins automatically.

**Deprecated (backward compatible):** `ROS_USE_NATIVE_ENGINE=false` forces `ROS_ENABLED_PLUGINS=kruize` at startup (overwriting any prior allowlist) and logs a deprecation warning — see [`ApplyLegacyUseNativeEngineEnv`](../../plugin/registry.go).

When the **native** engine is on (`ROS_USE_NATIVE_ENGINE` unset/true), **`ROS_ENABLED_PLUGINS` cannot include `kruize`** alongside native domains: if listed, `kruize` is stripped and a warning is logged.

Prefer migrating deploy manifests to `ROS_ENABLED_PLUGINS=kruize` for legacy-only installs.

No dual configuration is required once operators adopt `ROS_ENABLED_PLUGINS`; keep only **either** an explicit plugin allowlist **or** the deprecated flag until migrations finish.

## Tests

Unit tests for [`KruizePlugin`](plugin.go) live in this directory. Registry mutual exclusivity with other built-in plugins is covered by [`internal/plugins/kruize_registry_integration_test.go`](../kruize_registry_integration_test.go) (`package plugins_test`) because blank-importing `internal/plugins` from `kruize` tests would create an import cycle.

## Future work

Additional legacy routes can migrate behind [`APIProvider`](../../plugin/plugin.go) trait implementations as domains move fully under plugins.
