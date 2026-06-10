# ADR-0069: Use filter[term] normalized to short_term/medium_term/long_term

## Status

Accepted

## Context

Term names vary in user input; SQL layer needs canonical values.

## Decision

Accept flexible input, normalize to `short_term`/`medium_term`/`long_term` internally.

## Consequences

Flexible input. Consistent internal handling. Documented canonical values.

## References

- [internal/api/queryparams/](internal/api/queryparams/)
