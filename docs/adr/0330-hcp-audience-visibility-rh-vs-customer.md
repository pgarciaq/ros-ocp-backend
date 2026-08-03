# ADR-0330: HCP audience visibility — RH-internal and customer paths

## Status

Accepted

## Phase

HCP / fleet FinOps (pre-implementation)

## Context

Customers usually cannot install robne on **ROSA HCP management** clusters.
Red Hat can. Product must not conflate:

- metrics **availability** (RH may have management digests), with
- what is **safe to show** in a customer Optimizations UI.

Self-managed HyperShift/ACM customers often run robne on **both** planes in the
**same** customer org (Cost Management auth already scopes clusters).

## Decision

### Modes

| Mode | Meaning |
|------|---------|
| **M1** Self-managed dual-plane | Customer has management + hosted in **same org** |
| **M2** ROSA hosted-only | Customer has hosted only; no management digests |
| **M3** ROSA RH-operated management | RH has management digests; customer has hosted |
| **M4** RH-assisted (future) | Controlled share of signals to customer |

### Visibility (accepted)

Ship **both**:

1. **RH-internal:** full W1 (CP rightsizing) + full W2 (cross-plane causality) for operating the fleet.
2. **Customer-facing:**
   - **W0** on hosted (topology annotate/suppress) — always in scope when we detect hosted.
   - **W1** CP resize of RH-managed shared CP — **not** in customer UI by default.
   - **W2** — **advisory subset** only: e.g. “worker pressure low / don’t add workers first; contact provider” — **never** “resize etcd on RH management,” no raw management digests, no sibling HC names.

### Join / org identity

- **M1:** No special mapping service. Auth → customer org; Sources/API list that org’s clusters; ROS/Cost payloads carry `cluster_uuid` / ClusterVersion id. Correlator joins sources **inside that org**.
- **M3:** Raw management metrics stay in RH tenancy (**J1**). Optional sanitized advice row in customer org (**J2**). **Reject** placing RH management source under customer `org_id` (**J3**).
- **Join key (technical):** `HostedCluster.spec.clusterID` ↔ hosted ClusterVersion / ROS `cluster_uuid` — **lab confirmed** (#401).

### Product copy rules

1. Never tell a ROSA HCP customer to resize Red Hat–managed shared CP without written policy exception.
2. Never expose RH management cluster names, node IPs, or sibling HC names in customer UI.
3. Self-managed (M1) may use full W1/W2 language; templates branch on mode.

## Alternatives Considered

### Customer-visible full W1/W2 for ROSA

Rejected without policy — implies acting on RH-operated CP and leaks fleet topology.

### RH-internal only forever (no customer advisory)

Rejected by product: customer still benefits from W0 + “don’t add workers first” when evidence exists.

### Dual-write raw management digests into customer org

Rejected (data residency / tenancy).

## Consequences

- UI/API recommendation templates must be **mode-aware**.
- R3/W2 design assumes correlator sees both planes in M1 or in M3-RH; customer path uses advisory subset.
- On-prem Cost Management: M3 assumed **cloud/RH-SRE first** unless explicitly expanded later.
- Tracking: #397; correlator metric ADR remains #400 (after R3).

## Related Decisions

- [ADR-0328](0328-hcp-cluster-topology-detection-w0.md)
- [ADR-0331](0331-management-cp-rightsizing-filters-and-guardrails.md)
- Planned feature § Audience & visibility model

## References

- Epic #384, design #397, R3 #387, W2 backlog #404
