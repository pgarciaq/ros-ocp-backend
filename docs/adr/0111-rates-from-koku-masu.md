# ADR-0111: Source all rates from Koku Masu effective_rates

## Status

Accepted

## Context

Duplicating cost model configuration in ROS would diverge from Koku source of truth.

## Decision

ROS fetches rates from Koku's internal Masu endpoint. No rate CRUD in ROS.

## Consequences

Single source of truth. Dependency on Koku availability. Kill-switch if unavailable.

## References

- [internal/costdata/provider.go](internal/costdata/provider.go)
