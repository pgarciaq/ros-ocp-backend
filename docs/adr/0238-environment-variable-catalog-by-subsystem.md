# ADR-0238: Environment variable catalog organized by subsystem

## Status

Accepted

## Context

ROS exposes 100+ `ROS_*` environment variables through Viper-backed `internal/config.Config` ([ADR-0135](0135-centralized-viper-config.md)). Operators, Helm chart authors, and on-prem deployers need a single inventory grouped by concern (ingest, API, Kafka, plugins, security).

Scattered godoc comments and chart values alone do not stay synchronized as new flags land ([ADR-0157](0157-ros-enabled-plugins-replaces-native-flag.md), [ADR-0159](0159-per-plugin-term-env-vars.md), [ADR-0160](0160-savings-estimates-kill-switch.md)).

## Decision

Maintain the **canonical operator reference** in `docs/operations/configuration.md`, organized by subsystem:

- Ingest and processor
- API and HTTP client
- Database pool and timeouts
- Kafka consumer/producer
- Recommendation engines and plugins
- Security (RBAC, internal auth, SSRF)
- Observability and feature toggles

`config.go` remains the source of truth for defaults and `BindEnv` keys; the operations doc is updated when new env vars ship. This ADR documents the catalog **organization**, not duplicate [ADR-0135](0135-centralized-viper-config.md) Viper mechanics.

## Alternatives Considered

### Auto-generate doc from struct tags

Requires codegen discipline not yet adopted; manual doc allows operator notes and deprecation aliases.

### Single flat README env list

Unusable at 100+ variables; subsystem grouping aids troubleshooting.

### Env vars documented only in Helm chart

Misses bare-metal and docker-compose operators.

## Consequences

- PRs adding env vars should update `docs/operations/configuration.md` in the appropriate subsection.
- Deprecated aliases ([ADR-0241](0241-deprecated-alias-env-vars-backward-compat.md)) must appear in both config binding and ops doc.
- ADR index links here for "where is the full env inventory?" questions.

## Related Decisions

- [ADR-0135](0135-centralized-viper-config.md): Centralized Viper config pattern.
- [ADR-0240](0240-connection-pool-timeout-tuning-surface.md): DB and HTTP tuning knobs.
- [ADR-0241](0241-deprecated-alias-env-vars-backward-compat.md): Deprecated alias env vars.

## References

- [docs/operations/configuration.md](../operations/configuration.md)
- [internal/config/config.go](../../internal/config/config.go)
