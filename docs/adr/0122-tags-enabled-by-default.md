# ADR-0122: Default ROS_TAGS_ENABLED=true after stabilization

## Status

Accepted

> **Note:** This ADR documents a straightforward or inherited decision kept for completeness and historical traceability. It does not represent a non-obvious architectural fork.

## Context

Opt-in default left tag filters broken in fresh installs.

## Decision

Tags enabled by default once stable.

## Consequences

Tags work out of box. Ops can disable if needed. Reduced support tickets.

## References

- [CHANGELOG](CHANGELOG)
