# ADR-0135: Centralize config in internal/config.Config (Viper)

## Status

Accepted

## Context

Scattered `os.Getenv` calls made auditing and testing configuration difficult.

## Decision

Single Viper-loaded Config struct at startup. All code reads from Config.

### Two-tier validation

1. **Fatal validation** — `ValidateTagAuthConfig` ([ADR-0150](0150-validate-sa-allowlist-at-startup.md)): process refuses to start when api-mode tag auth is enabled with an empty SA allowlist in production.
2. **Non-fatal startup warnings** — `ValidateConfig()` in `config_validation.go`: logs WARN (not panic) for misconfiguration that does not block startup:
   - CORS allowing all origins in production
   - Internal tags auth enabled without SA allowlist (when not fatal)
   - Org allowlist set without internal auth enabled

Warnings are emitted at startup via `cmd/start.go` for operator visibility.

## Consequences

Testable config. Single point of validation. Type-safe access. Fatal vs warn split avoids silent security misconfig while allowing gradual hardening in dev.

## Related Decisions

- [ADR-0150](0150-validate-sa-allowlist-at-startup.md): Fatal SA allowlist validation.

## References

- [internal/config/config.go](internal/config/config.go)
- [internal/config/config_validation.go](internal/config/config_validation.go)
