# ADR-0234: No soft-delete cluster state — cleanup via Sources destroy events

## Status

Accepted

## Context

Multi-tenant SaaS and on-prem deployments need predictable cluster lifecycle semantics for list APIs, fleet summaries, and retention. A common pattern is `active | stale | deleted` enum columns with soft-delete timestamps.

ROS already purges tenant data on Sources Kafka destroy events ([ADR-0156](0156-sources-destroy-events-cleanup.md)). Staleness is computed from reporting freshness ([ADR-0224](0224-stale-marking-precedence-last-reported-at-overrides-digest-age.md), [ADR-0225](0225-filter-stale-tri-state-semantics.md)), not from a cluster state machine.

## Decision

Clusters have **no** `active/stale/deleted` lifecycle enum or soft-delete flag. A cluster disappears from API responses when:

1. Its source is destroyed via Sources Kafka event (housekeeper cleanup), or
2. Retention sweeps remove stale recommendation data and the cluster has no remaining rows ([ADR-0203](0203-retention-side-effects-beyond-partition-drop.md)).

There is no intermediate "deleted but visible" state in the cluster model.

## Alternatives Considered

### Soft-delete with tombstone rows

Complicates list queries and RBAC filtering; duplicates staleness semantics already on recommendations.

### Mark stale clusters inactive in DB

Conflates "not reporting" (staleness) with "source removed" (destroy); operators need both concepts separately.

### Hard-delete cluster row on first stale digest

Would drop reship/recovery paths when cluster temporarily stops reporting.

## Consequences

- API consumers use `filter[stale]` on recommendations, not cluster status fields.
- Source destroy is the only authoritative "cluster removed" signal for data purge.
- Re-added sources with the same cluster UUID behave as a fresh reporting cluster after upsert ([ADR-0233](0233-cluster-upsert-on-every-kafka-payload.md)).

## Related Decisions

- [ADR-0156](0156-sources-destroy-events-cleanup.md): Sources destroy events for tenant cleanup.
- [ADR-0203](0203-retention-side-effects-beyond-partition-drop.md): Retention side effects beyond partition drop.
- [ADR-0225](0225-filter-stale-tri-state-semantics.md): Stale filter tri-state semantics.

## References

- [internal/housekeeper/sources_destroy.go](../../internal/housekeeper/sources_destroy.go)
- [internal/model/cluster.go](../../internal/model/cluster.go)
