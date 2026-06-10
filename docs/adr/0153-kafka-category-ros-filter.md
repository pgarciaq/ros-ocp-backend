# ADR-0153: Consume hccm.ros.events with category=ros filter

## Status

Accepted

## Context

Same topic carries cost-management and ROS messages; processing wrong category wastes resources.

## Decision

Filter on `category=ros` in consumer; ignore cost-management CSV categories.

## Consequences

Clean separation. No wasted processing. Must match operator's category field.

## References

- [docs/architecture/kafka-schema.md](docs/architecture/kafka-schema.md)
