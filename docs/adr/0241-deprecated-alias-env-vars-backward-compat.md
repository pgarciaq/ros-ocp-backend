# ADR-0241: Deprecated alias env vars maintained for backward compatibility

## Status

Accepted

## Context

Configuration evolved across major releases: stale archive naming, native engine flag replacement ([ADR-0157](0157-ros-enabled-plugins-replaces-native-flag.md)), and staleness threshold renames ([ADR-0161](0161-staleness-threshold-hours-alias.md)). Helm charts and long-lived on-prem deployments still set older variable names.

Breaking env renames without aliases causes silent misconfiguration (old var ignored, default used) or failed pod startup in strict environments.

## Decision

Renamed environment variables keep **backward-compatible aliases** bound via Viper `BindEnv` to the same config field:

| Legacy alias | Canonical name |
|--------------|----------------|
| `ROS_STALE_ARCHIVE_DAYS` | `ROS_STALE_CLEANUP_DAYS` |
| `ROS_USE_NATIVE_ENGINE` | Superseded by `ROS_ENABLED_PLUGINS` ([ADR-0157](0157-ros-enabled-plugins-replaces-native-flag.md)) |

Old names continue to work silently. **No deprecation warning is logged** at startup to avoid log noise in charts that have not migrated labels yet.

Helm chart maintainers may still ship old names until chart major version bumps; documentation lists canonical names first in `docs/operations/configuration.md`.

## Alternatives Considered

### Log warning on alias use

Noisy in multi-replica deployments; operators treat as errors.

### Remove aliases after one release

Breaks downstream charts without coordinated bump.

### Accept both with precedence canonical-wins

Adds complexity; single field binding is sufficient when names map to same key.

## Consequences

- New docs and examples must use canonical names only.
- Code reviewers should reject new features that introduce additional aliases without ADR.
- `ROS_USE_NATIVE_ENGINE=true` behavior maps to native plugin set, not a separate code path.

## Related Decisions

- [ADR-0135](0135-centralized-viper-config.md): Centralized Viper config.
- [ADR-0157](0157-ros-enabled-plugins-replaces-native-flag.md): Plugin env replaces native flag.
- [ADR-0161](0161-staleness-threshold-hours-alias.md): Staleness threshold alias.

## References

- [internal/config/config.go](../../internal/config/config.go)
- [docs/operations/configuration.md](../operations/configuration.md)
