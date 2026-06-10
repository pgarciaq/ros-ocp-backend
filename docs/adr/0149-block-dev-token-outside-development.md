# ADR-0149: Block ROS_TAGS_DEV_TOKEN outside development

## Status

Accepted

## Context

Static dev bearer token in production bypasses all auth.

## Decision

Startup validation fails fatally if dev token set in non-development mode.

## Consequences

Can't accidentally deploy with dev token. Clear startup failure.

## References

- [internal/tags/auth_config.go](internal/tags/auth_config.go)
