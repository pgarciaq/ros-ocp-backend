# Kruize Plugin (Legacy)

The `kruize` plugin provides backward compatibility with the legacy Kruize recommendation engine.

## Status

!!! warning "Legacy — Not recommended for new deployments"
    The Kruize plugin is maintained for backward compatibility only. New deployments should use the native engine (default when `ROS_ENABLED_PLUGINS` is empty).

## Overview

The Kruize plugin delegates recommendation computation to an external Kruize (Autotune) Java service. It is **mutually exclusive** with all native plugins — enabling `kruize` alongside any native plugin causes a fatal startup error.

## Configuration

```bash
# Enable Kruize (disables all native plugins):
ROS_ENABLED_PLUGINS=kruize

# Kruize service URL:
KRUIZE_URL=http://kruize:8080
```

## Limitations vs Native Engine

| Capability | Kruize | Native |
|------------|--------|--------|
| GPU recommendations | No | Yes |
| Node recommendations | No | Yes |
| PVC / Snapshot | No | Yes |
| VM recommendations | No | Yes |
| Quota / CRQ | No | Yes |
| Dollar savings | No | Yes |
| Idle detection | No | Yes |
| Business hours | No | Yes |
| Notification codes | Limited | 54+ codes |
| Configurable thresholds | No | Yes (Settings API) |

## Migration

See [Native Migration Guide](../architecture/native-migration.md) for steps to transition from Kruize to the native engine.
