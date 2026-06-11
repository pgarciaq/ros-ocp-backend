# ADR-0210: Per-feature opt-out under global ROS_SETTINGS_LOCKED

## Status

Accepted

## Context

SaaS deployments may need to lock most settings but allow one domain to remain editable. A single global lock is insufficient when operators want incremental self-service rollout.

## Decision

Global `ROS_SETTINGS_LOCKED=true` returns `locked_fields: ["*"]` for all domains UNLESS per-type env vars opt that type back in:

- `ROS_SETTINGS_LOCKED_CONTAINER=false`
- `ROS_SETTINGS_LOCKED_GPU=false`
- `ROS_SETTINGS_LOCKED_IDLE=false`
- (and analogous vars for other domains)

`IsSettingsLocked(type)` checks type-specific override first, then falls back to global.

## Alternatives Considered

### All-or-nothing lock

Too coarse for phased operator enablement.

### Per-field locking

Too complex for current needs; deferred.

## Consequences

- Settings PUT returns 409 Locked when locked.
- API GET responses include `locked_fields` array.
- Per-type unlocking allows incremental rollout of self-service settings.

## Related Decisions

- [ADR-0083](0083-capabilities-endpoint-locked-settings.md): Capabilities endpoint listing locked fields.
- [ADR-0208](0208-settings-scope-org-wide-only-except-business-hours.md): Settings scope model.

## References

- [internal/config/settings_lock.go](../../internal/config/settings_lock.go)
- [internal/api/handlers_capabilities.go](../../internal/api/handlers_capabilities.go)
