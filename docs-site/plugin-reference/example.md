# Example Plugin (Template)

The `_example` plugin serves as a reference implementation for creating new ROS plugins. It is **always disabled** in production builds (name starts with `_`).

## Purpose

Use this as a starting template when implementing a new recommendation domain. It demonstrates:

- Implementing the `Plugin` interface
- Embedding `BasePlugin` for default phase and priority
- Self-registration via `init()`
- Ingest hook patterns
- Recommendation output structure

## Location

Source: [`internal/plugins/_example/`](../../internal/plugins/_example/)

## Creating a New Plugin

1. Copy `internal/plugins/_example/` to `internal/plugins/<your-plugin>/`
2. Rename the struct and update `Name()` to return your plugin's identifier
3. Implement your recommendation logic
4. Add a blank import in `internal/plugins/plugins.go`
5. Override `Phase()` and `Priority()` if needed (defaults: Phase 1, Priority 50)

## Plugin Lifecycle

```
init() → plugin.Register(&MyPlugin{})
         ↓
Registry.Enabled() filters by ROS_ENABLED_PLUGINS / ROS_DISABLED_PLUGINS
         ↓
ExecuteInPhases() calls plugins in sorted order (phase → priority → name)
```

## Related Documentation

- [Plugin Interfaces](plugin.md) — the `Plugin` interface contract
- [Plugin Execution Phases](../architecture/plugin-phases.md) — ordering and barriers
- [Plugin Architecture](../architecture/plugin-architecture.md) — design overview
