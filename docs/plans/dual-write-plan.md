# Dual-Write Implementation Plan: Kruize-to-robne SaaS Migration

This document describes the dual-write infrastructure that allows robne and Kruize to run simultaneously during the SaaS migration. It is the implementation plan for [ADR-0322](../adr/0322-temporary-dual-write-kruize-robne-saas-migration.md).

## Motivation

The native Go engine (robne) has replaced Kruize as the primary recommendation engine in upstream and on-prem deployments. To upstream robne into the SaaS production environment (console.redhat.com), Engineering requires a period of simultaneous operation: both engines process the same CSV data, but only one engine's recommendations are served to each customer (`org_id`).

This dual-write window allows the SaaS team to:
1. Validate that robne generates sensible recommendations on real production data
2. Compare robne's output against Kruize's output for the same clusters
3. Gradually migrate customers from Kruize to robne with rollback capability
4. Observe robne's operational behavior (latency, resource usage, error rates) under real SaaS load

## Single Unleash Flag: `rosocp.engine-mode`

Instead of multiple boolean flags, a single Unleash flag with four string variants controls the entire dual-write lifecycle:

| Variant | robne runs? | Kruize runs? | API displays | Use case |
|---------|-------------|--------------|--------------|----------|
| `kruize-only` | No | Yes | Kruize | Default. Current SaaS behavior. |
| `dual-write-kruize` | Yes | Yes | Kruize | Validate robne silently. Both engines process data, but users see Kruize results. |
| `dual-write-robne` | Yes | Yes | robne | Validate robne visibly. Both engines process data, users see robne results. Kruize is the silent fallback. |
| `robne-only` | Yes | No | robne | Migration complete for this org. Kruize no longer runs. |

### Why a single flag with variants (not two boolean flags)

Two boolean flags (`rosocp.robne-runs` + `rosocp.display-engine`) create four combinations, but one of them is invalid (robne displayed when robne doesn't run). A single flag with four valid variants eliminates this invalid state and makes the migration path linear and unambiguous.

### Per-org assignment (not percentage-based)

The flag uses Unleash variant **overrides** to assign specific variants to specific `org_id` values. This is not a gradual rollout with random percentage-based assignment. The PM explicitly controls which orgs are in which mode.

This means:
- Org A can be in `kruize-only` while Org B is in `dual-write-kruize`
- The PM can move individual orgs forward or backward in the migration path
- There is no randomness -- every org's mode is deterministic

### Helper functions

```go
// internal/featureflags/flags.go

func EngineMode(orgID string) string     // Returns the variant string
func RobneRuns(orgID string) bool        // true for dual-write-kruize, dual-write-robne, robne-only
func KruizeRuns(orgID string) bool       // true for kruize-only, dual-write-kruize, dual-write-robne
func DisplaysRobne(orgID string) bool    // true for dual-write-robne, robne-only
```

## Migration Path

The PM drives the migration through these stages:

```
Stage 1: kruize-only (all orgs)
  |
  | PM adds select orgs to dual-write-kruize
  v
Stage 2: dual-write-kruize (select orgs)
  |  Both engines run. Users see Kruize.
  |  Engineering validates robne output.
  |
  | PM flips select orgs to dual-write-robne
  v
Stage 3: dual-write-robne (select orgs)
  |  Both engines run. Users see robne.
  |  Engineering monitors for customer issues.
  |
  | PM flips select orgs to robne-only
  v
Stage 4: robne-only (select orgs)
  |  Only robne runs. Kruize is off.
  |
  | PM moves remaining orgs through stages 2-4
  v
Stage 5: robne-only (all orgs)
     Clean teardown: drop Kruize schema,
     remove flag, continue Kruize code removal.
```

## Architecture

### Dual-write execution

When both engines run (variants `dual-write-kruize` and `dual-write-robne`):

1. CSV arrives via Kafka
2. robne processes synchronously on the Kafka critical path (digest, recommend, write, ack)
3. Kruize processes asynchronously in a bounded goroutine pool
4. Kruize failures never block, retry, or fail the Kafka message
5. Kruize results are written to a separate `kruize_shadow` PostgreSQL schema

### API read routing

The API checks `DisplaysRobne(orgID)` to determine which engine's results to return:
- `true` (dual-write-robne, robne-only): Read from `public` schema (robne tables)
- `false` (kruize-only, dual-write-kruize): Read from `kruize_shadow` schema (Kruize tables)

### Comparison metrics

During dual-write, Prometheus metrics track:
- Recommendation count per engine per org
- Processing latency per engine
- Divergence count (cases where the two engines disagree on idle/active classification)

## Placement in the Upstreaming Plan

Dual-write is **Phase 1.5** in the [robne upstreaming plan](robne-upstreaming-plan.md). It must land:
- **After Phase 1** (container recommendations) -- there must be something to dual-write
- **Before Phase 2** (namespace recommendations) -- so the SaaS team can validate robne on real traffic before more features are added

### PRs

| PR ID | Repository | Description |
|-------|------------|-------------|
| ROS-0.8 | ros-ocp-backend | Feature flags infrastructure (`rosocp.engine-mode` helpers) |
| ROS-1.5.1 | ros-ocp-backend | Dual-write orchestration (poller, engine filter, comparison metrics) |
| IQE-ROS-1.5.1 | iqe-ros-ocp-plugin | Dual-write IQE tests |

## Related Issues

- #346: Implement dual-write orchestration for container recommendations
- #347: Implement engine-mode Unleash flag with per-org variants
- #348: Add dual-write comparison metrics (Prometheus)
- #349: Implement kruize_shadow schema for dual-write isolation
- #350: Add API read routing based on engine-mode flag
- #351: Implement Kruize async worker pool for dual-write
- #352: Add dual-write integration tests and IQE coverage

## Clean Teardown

When migration is complete (all orgs on `robne-only`):

1. `DROP SCHEMA kruize_shadow CASCADE` -- removes all Kruize data
2. Remove `rosocp.engine-mode` Unleash flag
3. Remove dual-write orchestration code
4. Continue Kruize code removal (ADR-0163 Phase 2/3)

## On-Prem

On-prem deployments are unaffected. They always run robne exclusively (`robne-only` is the implicit default when Unleash is absent). The dual-write infrastructure is never activated on-prem.
