# ADR-0025: Use PVC thresholds 20% oversized / 85% near-full with min trend days

## Status

Accepted

## Context

Storage risk is asymmetric — full PVC causes outage, oversized PVC wastes money.

## Decision

Flag oversized at 20% unused capacity; near-full at 85% used. Require minimum trend days before recommending.

## Consequences

Conservative on near-full alerts. Liberal on oversized flags. Trend minimum prevents false positives.

## References

- [internal/engine/pvc_recommend.go](internal/engine/pvc_recommend.go)
