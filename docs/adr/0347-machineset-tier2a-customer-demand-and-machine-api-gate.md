# ADR-0347: MachineSet Tier 2a advisory node counts and Machine API gate

## Status

Accepted (2026-10-30; owner confirmed customer demand and approved the aligned scope)

## Phase

17

## Supersedes

This ADR supersedes the deferral decision in [ADR-0278](0278-machineset-tier2-engine-deferred-scope-criteria.md). ADR-0278 remains unchanged as the historical record of the earlier decision.

## Context

Customer demand for MachineSet recommendations is confirmed: customers want a
node-count target and persisted recommendation detail, history, and
notifications. Tier 1 already exposes query-time MachineSet aggregation, but
its counts are based on nodes with recommendation rows; they are not an
authoritative MachineSet desired count and can omit members without eligible
node data.

The earlier Tier 2a spec and public requirements disagree on the replica
formula and minimum floor. In addition, the Koku Metrics Operator currently
attempts best-effort Machine-to-MachineSet mapping, but its checked-in RBAC
does not grant `machine.openshift.io` access. Its standard Machine API
Operator metrics document status counts but not a desired `.spec.replicas`
gauge. Machine API resources are also not present on every OpenShift topology.

## Decision

**Tier 2a is an advisory MachineSet node-count recommendation.** It reuses the
existing MachineSet-first node-consolidation result (`node_count_reduction`)
and pod-headroom gate. It is not scheduler placement simulation, a PDB
guarantee, or an actuator.

**MachineSet count source.** The Koku Metrics Operator will collect a
timestamped MachineSet API snapshot. `.spec.replicas` is the desired node
count; `.status.replicas` and `.status.availableReplicas` are separate
observed/availability values, not substitutes for the desired count. The
standard MAO status metrics may support history where verified, but the design
must not assume `machine_api_machine_set_spec_replicas` is an available
standard series.

**Readiness gate.** The Koku Metrics Operator needs least-privilege read-only
`list` permission for `machines` and `machinesets` in the `machine.openshift.io`
API group in both its RBAC source and OLM permissions. Scope list calls to the
`openshift-machine-api` namespace; the current supported IPI scope uses that
namespace. The collector uses list operations; do not grant `get` unless
implementation adds individual-object reads. If OLM cannot express a
namespace-scoped binding, resolve and review the least-privilege alternative
before implementation. A recommendation target
is emitted only when desired, observed, and available counts agree with the
member-node inventory and all members have fresh, eligible node
recommendations. If the API is absent, MachineSet recommendations are not
applicable and node-level recommendations continue. If reads fail, counts
disagree, or coverage is incomplete/stale, the MachineSet target is withheld
and the data state is reported as unavailable/incomplete; it must not silently
fall back to recommendation-row counts or infer a MachineSet from
`instance_type`.

The list API must expose per-cluster collection state in its metadata, keyed by
cluster UUID. Empty `data` must distinguish a valid no-recommendation result
from an unsupported or incomplete collection; an unavailable cluster must not
suppress valid rows from other clusters.

The report must carry a cluster-level collection state (`available`,
`not_applicable`, or `incomplete`) even when no MachineSet rows are present.
Serialize an allowlisted projection only: identity/counts/timestamp and the
Machine-to-Node mapping needed by ROS. Do not upload full Machine/MachineSet
`providerSpec`, user-data, or credential references.

**Topology scope.** Keep the currently documented IPI support scope until
other topologies are compatibility-tested. `UPI` describes an installation
method, not Machine API availability by itself. Runtime API discovery and
successful authorized reads are required; `platform: none` and other
Machine-API-absent clusters do not receive MachineSet targets.

**Recommendation math and floor.** Use the existing `node_count_reduction`
sum against the verified current member count, with a configurable minimum
that defaults to 1. Do not use the conflicting utilization-ratio formula, and
do not claim a generic floor of 2 guarantees HA; availability depends on
topology and workloads. The target is a desired number of Machine/node
instances to set on the MachineSet, not application-pod replicas.

**Persistence justification.** The dedicated recommendation and history
records are justified by the confirmed customer need for persisted detail,
history, and notifications—not by an unverified performance claim about the
Tier 1 query-time aggregation.

Persist MachineSet namespace/UID and the source snapshot timestamp with both
the current recommendation and history. The history writer must use a
retry-safe history identity (snapshot identity plus a deterministic
recommendation fingerprint); `recorded_at = now()` alone is not sufficient.
The migration and retry behavior must be tested before release.

**Bare Metal Operator.** BareMetalHost inventory and hardware/provisioning
health may be useful future context for bare-metal recommendations, but BMO
does not supply MachineSet desired count or reliable MachineSet membership.
It is not a fallback for this feature.

**Separate research.** PDB eviction feasibility and scheduler placement
feasibility remain separate investigations in [#709](https://github.com/pgarciaq/ros-ocp-backend/issues/709)
and [#710](https://github.com/pgarciaq/ros-ocp-backend/issues/710). Tier 2a
does not include those simulations or automatic application. Tier 2b remains
independent and is not a Tier 2a prerequisite.

**Notifications.** Codes 88–90 are planned candidates for heterogeneous
capacity, reduction recommended, and no reduction recommended. They are not
registered definitions until uniqueness is verified across consumers and the
implementation adds the coordinated catalog/migration changes. The no-change
message must not call the fleet “optimal.”

## Alternatives considered

### Keep Tier 2 deferred

This preserves the earlier scope, but conflicts with confirmed demand for a
node-count target and persisted detail/history/notifications. The remaining
data-source and RBAC gates are implementation prerequisites, not reasons to
ignore the confirmed requirement.

### Use utilization-ratio replica math and a universal two-node floor

Rejected for Tier 2a because it conflicts with the existing tested
MachineSet-first consolidation results, and a count of two alone does not
establish workload or zone availability.

### Use BareMetalHost inventory when Machine API is absent

Rejected as a fallback: BMHs describe physical hosts and provisioning state,
not necessarily cluster Nodes, MachineSet membership, or a desired pool size.

## Consequences

- Tier 2a is approved in scope but implementation is gated on KMO collection,
  RBAC, freshness/coverage behavior, and aligned API semantics.
- MachineSet recommendations remain unavailable on clusters without a usable
  Machine API; node-level recommendations remain available.
- The internal spec, public requirements, and #47 comment must use the same
  source, count semantics, topology caveat, and non-goals.
- PDB and affinity/scheduling simulation can be considered after the
  investigations close with an evidenced recommendation.

## Related decisions

- [ADR-0170](0170-machineset-tier1-aggregation.md): shipped Tier 1 MachineSet aggregation.
- [ADR-0194](0194-node-consolidation-precedence-pod-scheduling-gate.md): MachineSet-first grouping and pod-scheduling headroom gate.
- [ADR-0278](0278-machineset-tier2-engine-deferred-scope-criteria.md): superseded deferral decision.
