# ADR-0168: Disabled plugin route guards before Echo catch-all

## Status

Accepted

## Context

The recommendations API uses `/:recommendation-id` as a catch-all route for fetching individual recommendations by UUID. When plugins like GPU, Node, and PVC are disabled via config, requests to `/recommendations/openshift/gpu` would match the `/:recommendation-id` pattern, causing a misleading **400** (invalid UUID parse) instead of a clear **404**.

## Decision

`registerDisabledPluginRouteGuards()` in `server.go` explicitly registers 404 handlers for all URL prefixes of disabled plugins **before** the `/:recommendation-id` catch-all. Disabled plugin URLs return 404 with a message indicating the plugin is not enabled.

## Alternatives Considered

### Echo route priority/ordering alone

Fragile—depends on registration order and Echo's matcher behavior; easy to regress when routes are refactored.

### Middleware to check plugin status

Adds latency to every request, including enabled plugins and unrelated paths.

### Accept 400 errors

Confusing for operators debugging disabled plugins or misconfigured `ROS_ENABLED_PLUGINS`.

## Consequences

- Clear error responses for disabled features.
- Route table grows by ~5–10 entries per disabled plugin.
- Must stay in sync with plugin prefix list—new plugins need disabled guards registered.
- Plugin enable/disable is restart-only (not hot-reloadable).

## Related Decisions

- [ADR-0099](0099-compile-time-in-process-plugins.md): Compile-time plugin trait model.
- [ADR-0110](0110-example-plugin-trait-checklist.md): Plugin registration checklist.

## References

- [internal/api/server.go](../../internal/api/server.go) — `registerDisabledPluginRouteGuards`, `disabledPluginRoute404`
- [internal/api/disabled_plugin_route_guards_test.go](../../internal/api/disabled_plugin_route_guards_test.go)
