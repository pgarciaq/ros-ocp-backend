# ADR-0104: Make Kruize mutually exclusive with native plugins

## Status

Accepted

## Context

Dual-engine runs would duplicate recommendations and savings.

## Decision

`ROS_ENABLED_PLUGINS=kruize` disables all native plugins and vice versa.

## Alternatives Considered

### Dual-write transition period (Kruize + native in parallel)
Running both engines during migration would enable A/B comparison, but doubles Kafka consumer CPU, produces conflicting recommendation rows for the same container, and inflates savings calculations when both engines write to `recommendation_sets`.

### Per-plugin feature flags (mix Kruize and native plugins)
Granular toggles (e.g., native containers + Kruize VMs) explode combinatorial test matrices and leave ambiguous ownership when notification codes differ between engines; startup validation in `registry.go` rejects mixed configurations.

### Gradual migration with shadow-mode native engine
Computing native recommendations without serving them would validate parity safely, but maintaining divergent shadow state doubles ingest work indefinitely and delays the JSONB-to-relational cutover that motivated ADR-0001.

## Consequences

Clean migration path. No accidental dual-write. Configuration validates at startup.

## References

- [internal/plugin/registry.go](internal/plugin/registry.go)
