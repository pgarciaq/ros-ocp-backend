# ADR-0328: HCP cluster topology detection (W0)

## Status

Accepted

## Phase

HCP / fleet FinOps (pre-implementation)

## Context

OpenShift clusters may be **dedicated** (local control plane), **hosted** (HCP data
plane — workers only, CP on a management cluster), or **management** (runs HostedCluster
control planes). ROS today is per-cluster and worker/workload focused. Without topology
awareness, node and infra narratives can mislead (e.g. treating worker-only inventories
as “missing masters”).

Lab research (R1, epic #384): both hosted and management can appear **worker-only**.
Therefore “no master nodes” is **not** a sufficient hosted signal.

## Decision

1. **Classify** each cluster as one of: `dedicated` | `hosted` | `management` | `unknown`.
2. **Prefer documented signals over heuristics**, ranked:

   | Role | High-confidence signals |
   |------|-------------------------|
   | hosted | `Infrastructure.status.controlPlaneTopology == External`; `Infrastructure` label `hypershift.openshift.io/managed=true` |
   | management | ≥1 `HostedCluster` CR; namespace label `hypershift.openshift.io/hosted-control-plane=true`; HyperShift operator namespace |
   | dedicated | Non-External topology **and** master/control-plane node roles **and** no HostedCluster APIs/CRs |
   | unknown | Conflicting or missing signals → **do not** aggressively suppress |

3. **Hybrid emission / policy:**
   - **Operator** emits local facts into `manifest.json` (topology fields, HC counts / HCP ns list, node role summary).
   - **Backend** classifies and applies suppress/annotate policy.
   - Cross-plane join remains backend-only (W2 / R3) — not an operator concern.

4. **W0 product behavior (suppress + light annotate):**
   - On **hosted:** suppress master/CP-capacity narratives; annotate worker consolidation (“CP elsewhere”).
   - **Leave** worker container/ns/PVC/GPU/VM rightsizing unchanged.
   - **Do not** deep-rewrite node consolidation algorithms in W0.

5. **ROSA vs self-managed** columns stay in the detection matrix (same signals; different who can install on management).

## Alternatives Considered

### Backend-only detection from digests

Rejected: cannot see Infrastructure / HostedCluster CRs from container digests alone;
would force weak heuristics (no masters).

### Operator-only classification

Rejected: cannot join planes; product policy and UI live in robne.

### Heuristic “no masters ⇒ hosted”

Rejected: lab management cluster is also worker-only → false positives.

## Consequences

- New manifest fields + `clusters` table columns (implementation later).
- Node plugin / notifications gain topology-aware annotate/suppress (W0.3).
- Unlocks correct UX before W1/W2.
- `unknown` must remain safe (no harmful suppressions).

## Related Decisions

- [ADR-0329](0329-ros-auto-include-hypershift-hcp-namespaces.md): ROS collection of HCP namespaces (W1 ingest).
- [ADR-0330](0330-hcp-audience-visibility-rh-vs-customer.md): Who sees what (RH vs customer).
- Planned feature: `docs-site/planned-features/hosted-control-plane-fleet-optimization.md`
- Tracking: epic #384, R1 #385, W0 #402

## References

- Lab packs under `robnehcp/` (hosted Infrastructure External; management HostedCluster / HCP ns labels)
- Issue #406 / #407 / #408 (design children — coding deferred)
