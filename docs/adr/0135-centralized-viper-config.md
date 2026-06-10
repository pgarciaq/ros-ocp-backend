# ADR-0135: Centralize config in internal/config.Config (Viper)

## Status

Accepted

## Context

Scattered `os.Getenv` calls made auditing and testing configuration difficult.

## Decision

Single Viper-loaded Config struct at startup. All code reads from Config.

## Consequences

Testable config. Single point of validation. Type-safe access.

## References

- [internal/config/config.go](internal/config/config.go)
