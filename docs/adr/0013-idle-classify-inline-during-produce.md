# ADR-0013: Classify idle inline during container produce, not as separate plugin

## Status

Accepted

## Context

Separate idle plugin would require second DB pass and couldn't guarantee ordering before namespace rollup.

## Decision

Idle classification runs inline during container Produce phase.

## Consequences

Single pass. Guaranteed ordering. Can't disable idle detection independently of container plugin.

## References

- [internal/engine/recommend_all.go](internal/engine/recommend_all.go)
