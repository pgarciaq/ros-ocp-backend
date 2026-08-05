# Plugin Interfaces

> **Last verified:** 2026-08-06

This page documents the core `Plugin` interface that all recommendation plugins implement.

## Interface Definition

Every plugin must implement the `Plugin` interface defined in [`internal/plugin/plugin.go`](../../internal/plugin/plugin.go):

```go
type Plugin interface {
    Name() string
    Enabled() bool
    Phase() int
    Priority() int
}
```

## Methods

| Method | Return | Description |
|--------|--------|-------------|
| `Name()` | `string` | Unique plugin identifier (e.g., `"container"`, `"gpu"`, `"node"`) |
| `Enabled()` | `bool` | Whether the plugin is active based on `ROS_ENABLED_PLUGINS` / `ROS_DISABLED_PLUGINS` |
| `Phase()` | `int` | Execution phase (1=Produce, 2=Enrich, 3=Optimize) |
| `Priority()` | `int` | Ordering within a phase (lower runs first) |

## BasePlugin

Embed `plugin.BasePlugin` for default Phase 1 and Priority 50:

```go
type MyPlugin struct {
    plugin.BasePlugin
}
```

## Registration

Plugins self-register via `init()`:

```go
func init() {
    plugin.Register(&MyPlugin{})
}
```

Then add a blank import in [`internal/plugins/plugins.go`](../../internal/plugins/plugins.go).

## Related Documentation

- [Plugin Execution Phases](../architecture/plugin-phases.md) — phase ordering and barriers
- [Plugin Architecture](../architecture/plugin-architecture.md) — design overview
- [Example Plugin](example.md) — template for creating new plugins
