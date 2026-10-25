# ADR-0334: Fleet admission headroom (“N more HostedClusters”) — W4

## Status

Accepted (design) — W4 implementation greenlit 2026-10-24; amended 2026-10-25 (see Amendment)

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
| Minimum resource requirement | ~**5.5 vCPU** + ~**19 GiB** memory per HA HCP (a separate minimum, not the pod-request baseline above) |
| etcd storage | Three **8 GiB** PVs (HA) |
| Pod density limit | `maxPods` / ~75 — default 250 often caps ~3 HC/node even with spare CPU |
| Load sensitivity | ~**+9 vCPU** and **+2.5 GiB** per **+1000 QPS** of API stress (measured profile; not universal physics) |
| Idle / medium / high examples | Low &lt;50 QPS ≈ 2.9 vCPU / 11.1 GiB; medium 1000 QPS ≈ 11.9 / 13.6; high 2000 ≈ 20.9 / 16.1 |

Capacity is the **minimum** of request-based, usage/QPS-based, and `maxPods`-based limits × eligible workers.

### What MCE actually provides (important — not ROS-style advice)

Multicluster Engine’s hypershift addon exposes **Prometheus gauges** (numbers for dashboards / alerts). It does **not** ship Cost Management–style recommendation cards (“you should do X”).

Documented metric meaning (from the same sizing guide):

| Metric | Number of what? | Based on what? |
|--------|-----------------|----------------|
| `mce_hs_addon_request_based_hcp_capacity_gauge` | **Max HostedClusters** the management cluster can host | HA control-plane **CPU/memory requests** packing |
| `mce_hs_addon_low_qps_based_hcp_capacity_gauge` | Max HCs | Assumes ~**50 QPS** API load per HC (usage model) |
| `mce_hs_addon_medium_qps_based_hcp_capacity_gauge` | Max HCs | Assumes ~**1000 QPS** per HC |
| `mce_hs_addon_high_qps_based_hcp_capacity_gauge` | Max HCs | Assumes ~**2000 QPS** per HC |
| `mce_hs_addon_average_qps_based_hcp_capacity_gauge` | Max HCs | **Observed average QPS** of existing HCs (or low if none) |

So: **MCE provides a capacity estimate (max HCs), not advice.**  
Our job, when MCE is present: read that number, compute **headroom ≈ max − current HC count**, and wrap it in FinOps language + caveats + pressure warnings. **Do not rebuild a competing capacity engine.**

Where MCE gauges are absent: fallback to the same **published request + maxPods** math (weaker), still not a lab-calibrated oracle.

### Locked product rules (post-R5 discussion)

1. **Prefer MCE number + our framing** over inventing our own capacity model.  
2. **Assume the sizing-guide scenario (HA)** for numeric estimates. If the real HostedCluster topology is not that (e.g. `SingleReplica`), **still show the recommendation only with an explicit warning** that the guide assumptions do not match.  
3. **Audience default:** W4 is for **self-managed management** and **RH-internal** fleet ops. **Do not** invent customer-facing “N more on Red Hat’s ROSA/ARO management plane.”  
4. Under API/etcd/worker **pressure**, prefer “grow capacity before adding clusters” and **suppress fake precision**.  
5. **Periodic refresh:** [#409](https://github.com/pgarciaq/ros-ocp-backend/issues/409) — refresh baselines from the sizing guide when OCP versions move.  
6. **Priority:** W4 stays after W0/W1 (and usually after W3) on the ladder — not MVP-blocking.

### What ROS / robne should compute (when coding)

**Inputs (management cluster):**

| Input | Why |
|-------|-----|
| Count of HostedClusters | Current occupancy → headroom = max − current |
| MCE capacity gauges (preferred) | Max HCs from platform |
| Else: published 5 CPU / 18 GiB + ~75 pods + node allocatable/`maxPods` | Fallback packing |
| HA vs not | Warning if not sizing-guide scenario |
| Pressure signals | Drop precise N when already hot |

**Outputs (advisory copy):**

```text
“MCE/request packing estimates this management cluster can host about MAX
 HostedClusters (HA sizing-guide assumptions). You have CURRENT → headroom
 about MAX−CURRENT similar clusters. Not a guarantee; cloud/arch/version/API
 load change this.”

If topology ≠ HA sizing-guide scenario:
  add WARNING: “Estimate assumes highly available hosted control planes per
  OpenShift sizing guide; this fleet does not match — treat the number as
  illustrative only.”

If management already under pressure:
  “Grow management capacity before adding HostedClusters; exact remaining
   count is unreliable under load.”
```

**Never:** present N without naming method and assumptions.

### Lab clusters

**Not required for R5 go/no-go.** Optional later only to verify we can *read* HC count and (if present) MCE gauges — never to calibrate a global N.

### Audience

| Audience | Message |
|----------|---------|
| Self-managed / RH fleet ops | Full headroom helper (MCE preferred) |
| ROSA/ARO customer | **No** customer-facing “N more on RH management”; provider owns that plane |

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

## Amendment (2026-10-24: W4 design approval; 2026-10-25: freshness clock and mechanism refinements)

The owner greenlit W4 implementation and approved the design in
`docs/superpowers/specs/2026-10-24-w4-fleet-capacity-advisory-design.md`
(plan: `docs/superpowers/plans/2026-10-24-w4-fleet-capacity-advisory.md`).
The original decision's rationale stands: no universal lab-calibrated N, and
every displayed number names its method and assumptions. The rules below
supersede the corresponding implementation rules recorded above; the original
text is preserved unchanged for history.

- **Native engine supersedes "prefer MCE" (locked rule 1):** W4 ships a
  ROS-native published-sizing calculation from raw inputs, computed by shared
  `librobne/hcp` math. MCE's five capacity gauges remain an optional,
  separately labeled reference — never blended with native results. "Do not
  rebuild a competing capacity engine" is superseded as the implementation
  rule; the anti-goal it protected (unlabeled, lab-calibrated formulas) still
  stands.
- **Audience narrowed (locked rule 3):** the boundary is **cross-tenant M3
  delivery**, not customer visibility per se. Self-managed operator-tenant
  advice — including customer-visible UI in the operator's own org — is in
  scope; #620 governs only the cross-tenant bridge.
- **Version floor:** OCP management 4.17+ / MCE 2.6+, restricted to pairs in
  the official support matrix. The runtime gate is capability detection; a
  known `ocp_version` below the floor serves with an explicit
  below-documented-floor warning, never suppression.
- **Occupancy:** count every visible HostedCluster object (Available,
  Progressing, Degraded, paused/`pausedUntil`, NodePool-zero, deleting) — a
  conservative occupancy that intentionally exceeds the Ready-HCP set MCE's
  average-QPS gauge uses; that scenario's basis is labeled. HC list failure
  is incomplete, never zero. The W4 collector emits nothing on clusters
  without the HyperShift APIs (not an HCP cluster — not "incomplete").
- **Average-QPS semantics:** native average is the plain mean Σ/N of
  Ready-HCP QPS (MCE's gauge seeds its total with the 50-QPS floor — an
  upstream quirk not replicated). Failed per-HCP queries make the scenario
  unavailable, never the 50 basis.
- **Freshness clock (amended 2026-10-25):** a snapshot is stale when its age
  exceeds 2× the effective cadence, effective cadence = max(hourly capture,
  upload cycle); both intervals travel in the snapshot. Two bare collection
  intervals were rejected: the default 360-minute upload cycle would leave
  `latest` stale most of the time.
- **Capture cadence and window:** hourly, beside the SLO/APITax/Nodepool
  snapshot collectors; `observed_at` is the report-window end; backfilled
  windows are skipped (Kubernetes inventory has no past).
- **Data sources:** Kubernetes API for nodes (allocatable, maxPods, labels,
  taints), HC/HCP inventory, and pressure — the nodepool/topology-collector
  precedent, and how MCE itself computes; Prometheus only for per-HCP QPS
  rates and the MCE gauges. No kube-state-metrics allowlist dependence.
- **Availability warning basis:** keys on the hosted control planes'
  `ControllerAvailabilityPolicy` aggregate (`highly_available` / `mixed` /
  `single_replica` / `unknown`), never the management cluster's
  `controlPlaneTopology`; compact/shared-infrastructure clusters carry the
  OCP sizing-guide §3.2.5 caveat.
- **Pressure:** affirmative management pressure suppresses precise added-HC
  headroom; unknown pressure is shown as "not assessed" — never silently
  healthy, never a blanket feature block.
- **Transport and history:** a bounded `ros-openshift` CSV through the
  existing operator packaging and typed plugin ingest; history retained via
  the core history sweep at `ROS_HISTORY_RETENTION_DAYS` (90 days). robne
  CLI support is a separate follow-up
  ([#700](https://github.com/pgarciaq/ros-ocp-backend/issues/700)).

## Related Decisions

- [ADR-0328](0328-hcp-cluster-topology-detection-w0.md) — management detection
- [ADR-0330](0330-hcp-audience-visibility-rh-vs-customer.md) — who sees management packing
- [ADR-0333](0333-unused-hostedcluster-lifecycle-w3.md) — unused HC is different (delete), not headroom
- Research #389; wedge #392; epic #384; maintenance #409

## References

- [OKD: Sizing guidance for hosted control planes](https://docs.okd.io/latest/hosted_control_planes/hcp-prepare/hcp-sizing-guidance.html)
- [OCP 4.22: Sizing guidance for hosted control planes](https://docs.redhat.com/en/documentation/openshift_container_platform/4.22/html/hosted_control_planes/preparing-to-deploy-hosted-control-planes#hcp-sizing-guidance_hcp-prepare)
- OCP hosted control planes “Preparing to deploy” sizing chapters (same model)
- Red Hat article on scalable ROSA HCP management environments (request footprint + buffer for new installs)
- MCE hypershift addon capacity gauge metric names (listed above)
