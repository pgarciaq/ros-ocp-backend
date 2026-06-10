# ADR-0095: Use DetermineCSVType longest-prefix-first + contains fallback

## Status

Accepted

## Context

Suffix-only matching mis-routed namespace vs cluster-quota files with similar names.

## Decision

Try longest prefix match first; fall back to contains matching for nise-generated names.

## Consequences

Correct routing for all CSV naming conventions. Order-dependent matching.

## References

- [internal/utils/utils.go](internal/utils/utils.go)
