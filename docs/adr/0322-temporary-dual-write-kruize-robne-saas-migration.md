# ADR-0322: Temporary dual-write for Kruize-to-robne SaaS migration

## Status

Accepted — supersedes [ADR-0104](0104-kruize-mutually-exclusive-native.md) and [ADR-0262](0262-shadow-mode-native-engine-explicitly-rejected.md) for the duration of the migration window.

## Context

The native Go engine (robne) has replaced Kruize as the primary recommendation
engine in upstream and on-prem deployments. To upstream robne into the SaaS
production environment (console.redhat.com), Engineering requires a period of
simultaneous operation: both engines process the same CSV data, but only one
engine's recommendations are served to each customer (`org_id`).

ADR-0104 enforced mutual exclusivity between Kruize and native plugins.
ADR-0262 explicitly rejected shadow tables and dual-write to production tables.
Both decisions were correct at the time — they simplified the initial native
engine development and prevented accidental dual-serve bugs. Now that robne is
proven in upstream and on-prem, the migration to SaaS production requires a
controlled dual-write window with per-org routing.

### Why not offline comparison only?

The offline `cmd/compare` CLI ([ADR-0140](0140-kruize-native-comparison-cli.md))
validates recommendation math but cannot prove robne's operational behavior
under real SaaS production load, with real customer data volumes, real Kafka
traffic patterns, and real infrastructure constraints. Engineering needs to
observe robne in production before committing to a full switchover.

## Decision

### 1. Dual-write on SaaS only

Both robne and Kruize process every incoming CSV for every org_id
simultaneously in SaaS deployments. On-prem always runs robne exclusively
(no change from current behavior).

### 2. Separate PostgreSQL schema for Kruize

robne writes to the `public` schema (existing behavior). Kruize writes to a
new `kruize_shadow` schema within the same database. Same table names in both
schemas — GORM models are reused with schema qualification. The schema name is
the sole discriminator; no engine column, prefix, or metadata is needed to tell
the two apart.

### 3. Asynchronous Kruize ingest

robne runs synchronously on the Kafka critical path (download CSV, digest,
recommend, write, ack Kafka). Kruize runs asynchronously in a bounded
goroutine pool off the Kafka critical path. Kruize failures never block,
retry, or fail the Kafka message. Kruize lag behind robne is expected and
acceptable.

### 4. Per-org active engine via Unleash

The `rosocp.robne-enabled` flag on the
[SaaS Unleash instance](https://insights.unleash.devshift.net/projects/default)
controls which engine is "active" (served to customers) per org_id:

- **Org in flag:** robne is active (API reads `public` schema, UI shows
  percentile bands).
- **Org not in flag:** Kruize is active (API reads `kruize_shadow` schema,
  UI shows boxplots).

The same flag drives both backend API read routing and frontend chart mode
(koku-ui-ros). On-prem defaults to robne when Unleash is absent.

### 5. Plugin exclusivity relaxation

Three gates in `internal/plugin/registry.go` enforce mutual exclusivity today.
When `DUAL_WRITE_ENABLED=true`, all three are relaxed:

1. `validateKruizePluginExclusivity()` — returns nil unconditionally.
2. `Enabled()` — returns all candidates regardless of engine mix.
3. `EnabledFor(KruizePluginName)` — returns true implicitly.

When `DUAL_WRITE_ENABLED=false` (default), all three gates behave exactly as
today. No change to existing single-engine deployments.

### 6. PM-driven migration

The PM migrates org_ids from Kruize to robne in batches over weeks by adding
org_ids to `rosocp.robne-enabled`. The PM observes robne performance in
production and makes the go/no-go decision. There is no automated parity
threshold, date cap, or migration completeness gate. The migration decision is
the PM's prerogative.

Possible signals the PM may observe (informational, not automated):

- robne generates X recommendations in S seconds; Kruize generates Y in M
  minutes.
- robne recommendation coverage vs Kruize.
- Customer-reported issues after flip (if any).
- Shadow freshness lag.

### 7. Clean teardown

When migration is complete: `DROP SCHEMA kruize_shadow CASCADE`, set
`DUAL_WRITE_ENABLED=false`, remove recommendation-poller sidecar, remove
Unleash flag, then continue ADR-0163 Phase 2/3 Kruize code removal.

### 8. Core infrastructure, not a plugin

Dual-write is core infrastructure gated by `DUAL_WRITE_ENABLED` env var, not
a new plugin in the `internal/plugin/` system. It orchestrates two existing
engines; it does not own a recommendation domain, does not parse CSVs itself,
and does not register API routes. A plugin that orchestrates other plugins
would be an anti-pattern.

## Alternatives Considered

### Continue offline comparison only (cmd/compare)

Lowest cost, zero production risk. Cannot prove operational behavior under
real SaaS load. Does not satisfy Engineering's requirement to observe robne
with real customer data in production.

### Separate database for Kruize shadow

Full isolation but requires a second connection pool, separate migration
management, and cross-database query complexity. The single-database,
dual-schema approach achieves isolation with simpler operations.

### Engine column discriminator (same tables)

Adding an `engine` column to `recommendation_sets` to distinguish Kruize from
robne rows. Risk of dual-serve bugs (query forgets the filter), inflated
savings calculations, and no clean teardown (must DELETE rows, not DROP
SCHEMA). The existing `engine` column (`'cost'`/`'performance'`) distinguishes
recommendation profiles within the native engine, not engine identity.

### Gradual traffic shift (serve both, blend results)

Extreme complexity. Conflicting recommendation values for the same container.
No clear UX for mixed results. Rejected in ADR-0262 and still not viable.

## Consequences

- Mutual exclusivity (ADR-0104) is temporarily relaxed for SaaS deployments
  with `DUAL_WRITE_ENABLED=true`. It remains enforced everywhere else.
- Shadow mode (rejected in ADR-0262) is now accepted in a scoped form:
  separate schema, not shadow columns in production tables.
- Additional resource cost during the migration window (Kruize sidecar CPU/
  memory + async worker pool goroutines + `kruize_shadow` storage).
- Kruize shadow store may trail robne by minutes to hours. Rollback to
  Kruize-active for an org requires checking shadow freshness.
- On-prem deployments are unaffected. They never enter dual-write mode.

## Related Decisions

- [ADR-0001](0001-native-engine-over-kruize.md): Native engine over Kruize (unchanged).
- [ADR-0104](0104-kruize-mutually-exclusive-native.md): Mutual exclusivity (temporarily superseded by this ADR).
- [ADR-0140](0140-kruize-native-comparison-cli.md): Offline comparison CLI (remains available as fallback).
- [ADR-0259](0259-synchronous-ingest-time-engine-replaces-kruize-experiment-lifecycle.md): Synchronous ingest engine (unchanged).
- [ADR-0262](0262-shadow-mode-native-engine-explicitly-rejected.md): Shadow mode rejection (temporarily superseded by this ADR).

## References

- [internal/plugin/registry.go](../../internal/plugin/registry.go) — exclusivity gates
- [internal/services/report_processor.go](../../internal/services/report_processor.go) — ingest fork point
- [internal/services/parallel_ingest.go](../../internal/services/parallel_ingest.go) — Kruize legacy path (`processLegacyFile`)
- [internal/featureflags/flags.go](../../internal/featureflags/flags.go) — Unleash flag evaluation
- [internal/config/config.go](../../internal/config/config.go) — `DUAL_WRITE_ENABLED`, `KRUIZE_WORKER_CONCURRENCY`
- [Operations: Dual-Write Mode](../operations/dual-write-runbook.md) — SRE playbook
