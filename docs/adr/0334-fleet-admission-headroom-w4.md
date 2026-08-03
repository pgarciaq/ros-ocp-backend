# ADR-0334: Fleet admission headroom (“N more HostedClusters”) — W4

## Status

Accepted (design) — **implementation coding deferred** until explicit W4 greenlight

## Phase

HCP / fleet FinOps (research R5 complete — **docs/sizing sources only**, not lab capacity claims)

## Context (plain English)

People want to know: **how many more hosted clusters can this management cluster take before things get bad?**

Measuring one lab cluster and publishing “you can add N more” is a **bad idea**:

- CPU architecture and speed differ (x86_64, ARM, …)
- OpenShift / HyperShift versions differ
- Single-replica vs highly available control planes differ
- Cloud instance types vs bare metal differ
- How chatty each hosted cluster’s API is differs a lot
- Node `maxPods` often limits density before “free CPU” does

So R5 answers: what can we honestly recommend using **published sizing guidance and defaults**, without pretending our lab is the universe?

## Decision

### Go / no-go for W4

| Verdict | Meaning |
|---------|---------|
| **GO (narrow)** | Ship a **cautious packing / pressure helper** grounded in OpenShift HCP sizing guidance (and prefer existing Multicluster Engine capacity metrics when present) |
| **NO-GO** | A universal, architecture-independent “you can add **N** more clusters” number derived from our own lab measurements |
| **NO-GO** | Claiming load-based QPS formulas apply unchanged to every cloud SKU / arch / version without labeling the source assumptions |

### What OpenShift already documents (source of truth)

From OKD/OCP **Sizing guidance for hosted control planes** (HA topology; load examples measured on bare metal; cloud may differ):

| Idea | Documented baseline (HA) |
|------|---------------------------|
| Pods per HC | ~78 pods (~75 for `maxPods` planning) |
| Request packing | ~**5 vCPU** + ~**18 GiB** memory **requests** per HC |
| etcd storage | Three **8 GiB** PVs (HA) |
| Pod density limit | `maxPods` / ~75 — default 250 often caps ~3 HC/node even with spare CPU |
| Load sensitivity | ~**+9 vCPU** and **+2.5 GiB** per **+1000 QPS** of API stress (measured profile; not universal physics) |
| Idle / medium / high examples | Low &lt;50 QPS ≈ 2.9 vCPU / 11.1 GiB; medium 1000 QPS ≈ 11.9 / 13.6; high 2000 ≈ 20.9 / 16.1 |

Capacity is the **minimum** of request-based, usage/QPS-based, and `maxPods`-based limits × eligible workers.

### Prefer not reinventing: MCE already exposes gauges

Multicluster Engine / hypershift addon documents metrics such as:

- `mce_hs_addon_request_based_hcp_capacity_gauge`
- `mce_hs_addon_low_qps_based_hcp_capacity_gauge`
- `mce_hs_addon_medium_qps_based_hcp_capacity_gauge`
- `mce_hs_addon_high_qps_based_hcp_capacity_gauge`
- `mce_hs_addon_average_qps_based_hcp_capacity_gauge`

**Product implication:** For environments with MCE, W4 should **surface / explain** those estimates (with caveats), not invent a competing oracle. Where MCE is absent, a **request + maxPods packing estimate** using the same published baselines is acceptable as a weaker advisory.

### What ROS / robne should compute (when coding)

**Inputs (management cluster):**

| Input | Why |
|-------|-----|
| Count of HostedClusters | Current occupancy |
| Sum of CP pod **requests** (or published 5 CPU / 18 GiB per HA HC) | Request packing |
| Worker / control-plane node allocatable CPU, memory, `maxPods` | Ceiling |
| Optional: MCE capacity gauges | Prefer when available |
| Optional: API QPS / pressure | Only to pick low/medium/high **bands**, not fake precision |
| Topology: HA vs `SingleReplica` | Single-replica uses different footprint — do not apply HA table blindly |

**Outputs (advisory copy):**

```text
“Schedule-style headroom (request + maxPods math from OCP HCP sizing guide,
 labeled for HA / this OpenShift major): about N more clusters of similar
 request footprint — not a guarantee. Cloud/arch/version and API load can
 change this. Load-based numbers in the guide were measured on bare metal.”

OR if workers / etcd / API already hot:
“Management plane is already under pressure — grow capacity before adding
 HostedClusters. Exact remaining count is unreliable under load.”
```

**Never:** present N without naming method and assumptions.

### Lab clusters

**Not required for R5 go/no-go.** Optional later only to verify we can *read* node allocatable, HC count, and (if present) MCE gauges — never to calibrate a global N.

### Audience

| Audience | Message |
|----------|---------|
| Self-managed / RH fleet ops | Full packing helper + MCE gauges when available |
| ROSA/ARO customer | Usually **cannot** see RH management packing; do not invent customer-facing “N more on Red Hat’s plane.” RH-internal tooling may use this. |

## Alternatives Considered

### Calibrate N from our lab

Rejected — user’s concern is correct; not portable across arch/version/CPU.

### Only “red/yellow/green” pressure, never a number

Accepted as **fallback** when inputs incomplete; still allow a **labeled** request/`maxPods` estimate when inputs are complete.

### Wait for HyperShift to own all UX

MCE gauges already exist — we should integrate/explain, not ignore. Still room for FinOps framing (cost of growing management workers vs adding HCs).

## Consequences

- W4 backlog #392 may be promoted after coding greenlight; still no code now.
- Depends on management-plane inventory (W0) and CP digests (#405) for request sums; MCE metrics path is alternate/preferred.
- Product must version-pin or link the sizing guide assumptions.
- Ask #398 whether MCE gauges are the blessed long-term API for capacity.

## Related Decisions

- [ADR-0328](0328-hcp-cluster-topology-detection-w0.md) — management detection
- [ADR-0330](0330-hcp-audience-visibility-rh-vs-customer.md) — who sees management packing
- [ADR-0333](0333-unused-hostedcluster-lifecycle-w3.md) — unused HC is different (delete), not headroom
- Research #389; wedge #392; epic #384

## References

- [OKD: Sizing guidance for hosted control planes](https://docs.okd.io/latest/hosted_control_planes/hcp-prepare/hcp-sizing-guidance.html)
- OCP hosted control planes “Preparing to deploy” sizing chapters (same model)
- Red Hat article on scalable ROSA HCP management environments (request footprint + buffer for new installs)
- MCE hypershift addon capacity gauge metric names (listed above)
