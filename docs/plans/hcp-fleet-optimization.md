# HCP / Fleet Control-Plane Optimization — Design Plan and Roadmap

**Status (reconciled 2026-10-14):** W0–W2 shipped; W3's usage and NodePool-zero core is implemented, with validation and cost follow-ups remaining. W4/W5 research is complete but implementation is deferred; W6–W8 remain backlog.<br>
**Planned feature (public):** [hosted-control-plane-fleet-optimization.md](../docs-site/planned-features/hosted-control-plane-fleet-optimization.md)  
**Epic:** [#384](https://github.com/pgarciaq/ros-ocp-backend/issues/384)

This plan records **decisions already accepted** and the current delivery state. New coding still requires an explicit per-wedge greenlight; the shipped W0–W3 work had its own recorded greenlights. Do not treat this plan as blanket approval for future wedges.

---

## Accepted ADRs (locked now)

| ADR | Decision |
|-----|----------|
| [0328](../adr/0328-hcp-cluster-topology-detection-w0.md) | W0 topology: signals, hybrid operator→backend classify, suppress+annotate |
| [0329](../adr/0329-ros-auto-include-hypershift-hcp-namespaces.md) | Operator auto-includes HCP namespaces in ROS queries |
| [0330](../adr/0330-hcp-audience-visibility-rh-vs-customer.md) | Both RH-internal full path and customer advisory; M1 join via CM auth |
| [0331](../adr/0331-management-cp-rightsizing-filters-and-guardrails.md) | W1 label filters + CP guardrails |
| [0332](../adr/0332-thin-cross-plane-causality-w2.md) | W2 thin causality: GO with caveats; join + metrics + precision-first algorithm |
| [0333](../adr/0333-unused-hostedcluster-lifecycle-w3.md) | W3 unused HostedCluster: GO with caveats; delete/review; never sell `pausedUntil` as cost save |
| [0334](../adr/0334-fleet-admission-headroom-w4.md) | W4 headroom: GO narrow — OCP sizing / MCE gauges; NO universal lab-calibrated N |
| [0335](../adr/0335-api-tax-operator-webhook-w5.md) | W5 API tax: GO — thin top-N + webhooks; both planes; hooks into W1/W2/W4/node advice |

**#400 (correlator ADR):** Satisfied by ADR-0332 for thin MVP.

---

## Current roadmap reconciliation (2026-10-14)

| Track | Current state | Remaining gate / follow-up |
|---|---|---|
| W0 topology | Shipped | No new W0 build is queued |
| W1 management CP rightsizing | Shipped with HCP namespace auto-include, association, guardrails, and `/hcp` surface | Full REST/UI automation remains separately tracked (#640, #654) |
| W2 thin correlation | Shipped: bounded SLO rollups, policy settings, and hourly advisories | Full causality remains out of scope for the thin MVP |
| W3 unused HostedCluster | Usage/CPU rule and NodePool-zero short lane are implemented (#658, #664, #673, #674) | #663 aged-evidence/falsification, #661 cost estimate, and #665 cost-group follow-up remain open or gated |
| Tier B management reliability (#681) | Crash-loop and node-pressure plumbing is implemented and reviewed | Live code-87 firing has not been observed; current HCP nodes report no pressure |
| W4 / W5 | Research GO narrow / GO (ADRs 0334/0335) | Still post-MVP; W4 must use published sizing or verified MCE gauges, not a lab-derived universal N; W5 remains gated on APIRequestCount/operator support (#657) |
| M3 bridge | Investigation #620 remains open | Needs an owner, written authorization/data-sharing policy, and trusted org-routing decision |
| W6–W8 | Backlog | No implementation greenlight |

The formal GitHub sub-issue tree contains the original #385–#400 scaffolding. Later work is linked through issue bodies, comments, and `epic:hcp-fleet` labels; the latest full map comment on #384 predates the October work and is stale.

### Validation availability

Cluster-specific details remain in the private lab `ACCESS.md` files. The 2026-10-14 read-only checks found:

- The new HCP lab has management plus `hc01`/`hc02`; both HostedClusters are Available and both NodePools are 2/2. All management and hosted nodes were Ready, with Memory/Disk/PID pressure false at the time checked.
- KMO 4.4.1 is packaging data. On the latest read-only check, upload is enabled on the HCP management and hosted CRs; all three have `source_defined=true` and report `202 Accepted` uploads to the SNO gateway. The SNO's own KMO upload is also enabled. This verifies ingress acceptance, not yet ROS HCP API rows/processing; the earlier ACCESS snapshot was stale for the HCP upload toggle.
- The HCP environment is ARM64, Agent-platform, SingleReplica, and explicitly off-matrix. Use it for plumbing/falsification, not support claims or portable sizing estimates.
- The new HCP lab is too fresh for the 14-day W3 idle window. The prior #678 contrast was cut short at about 34 hours and established digest absence, not an aged-HC firing proof.

**Consequence:** HCP KMO uploads are now accepted by the SNO ingress, and the owner confirms Cost Management data is visible there. The downstream ROS HCP endpoint/hosted-cluster attribution still needs an explicit check. #640/#654 have data to exercise, but automated IQE remains blocked by fork runner availability and UI coverage needs its test environment. Do not change upload settings, create sources, scale workloads, or induce pressure as part of this reconciliation.

---

## Glossary (plain English)

| Term | Meaning |
|------|---------|
| **HCP** | Hosted control plane (HyperShift): the cluster's control-plane pods run on a *management* cluster, not alongside its workers |
| **HC** | HostedCluster: the OpenShift API object representing one hosted control plane; its `spec.clusterID` is the cross-plane attribution key |
| **HCP namespace** | Per-HC namespace on management shaped `{hc.namespace}-{hc.name}` (live lab: `hc01-infra-hc01`); membership is the W1 filter |
| **Management / hosted cluster** | Management hosts others' control planes (live lab: `hcp-mgmt`); hosted runs workloads with `controlPlaneTopology=External` (live lab: `hc01`) |
| **W0** | Detect dedicated vs hosted vs management; fix misleading narratives |
| **W1** | Rightsize HyperShift CP pods on the **management** cluster |
| **W2** | Prove hosted “API slow” is (or isn’t) management CP — thin causality |
| **W3** | Unused HostedCluster lifecycle: advise review/delete, never sell `pausedUntil` as savings |
| **W4** | Fleet admission headroom: GO narrow — published sizing math / MCE gauges, no universal lab number |
| **W5** | API tax: thin top-N digest of chatty service accounts / slow webhooks, both planes |
| **W6–W8** | Post-MVP placeholders (#394–#396), no design depth until promoted |
| **R1** | Topology research → ADR-0328 |
| **R2** | Management-as-workload research (filter rules; refreshed by #583 for the new lab) → ADR-0331 + ingest ADR-0329 |
| **R3** | Causality research → ADR-0332 (GO with caveats for W2) |
| **R4** | Unused HC / lifecycle research → ADR-0333 (GO with caveats for W3) |
| **R5** | Fleet admission headroom research → ADR-0334 (GO narrow) |
| **R6** | API tax research → ADR-0335 (GO) |
| **M1** | Customer runs both planes in one org |
| **M2** | Customer hosted-only (typical ROSA without RH management robne) |
| **M3** | RH runs management robne; customer runs hosted |
| **M4** | RH-assisted future: controlled share of signals to customer |
| **J1/J2/J3** | M3 tenancy: raw metrics stay RH-side (J1) vs sanitized customer advice (J2); placing RH sources under customer `org_id` rejected (J3) |
| **J4** | Fallback: never join; customer gets W0 only (used if policy blocks J1/J2) |
| **Layers A/B/C** | Platform-wide split: A = collection/operator (#587), B = generation/server+CLI (#588), C = serving/API (#589). Independent issues, shippable alone |
| **child-0** | W1.1's first acceptance item: name the audience enforcement point before any guardrail code, or return the question to ADR-0330 |
| **`hcp`** | Shipped plugin, route, `recommendation_type`, and settings identifier for HCP recommendations |
| **`cost_management_optimizations`** | Namespace label gating container + ns-quota collection (frozen meaning); HCP namespaces auto-included without it |

---

## Delivery status by wedge

### W0 — Topology (ADR-0328) — shipped

| Slice | Issue | Delivered behavior |
|-------|--------------|---------------------------|
| W0.1 | #406 | Operator topology facts |
| W0.2 | #407 | Backend classification + persisted `cluster_topology` |
| W0.3 | #408 | Hosted node-path suppression/annotation |

**Out of W0:** consolidation algorithm rewrite; cross-plane join; CP rightsizing.

### W1 — Management CP rightsizing (ADR-0331) — shipped

| Prerequisite | Notes |
|--------------|-------|
| ADR-0329 / #405 | HCP namespace collection and auto-include shipped |
| #584 / #590 / #630–#635 | Guardrails, server/CLI wiring, association persistence, history, and pre-migration reads shipped |

Guardrail profile (locked #584, W1.1 greenlit): `max(100m CPU / 128MiB absolute, 70% of window-median current request)` on cost+perf; replica recs suppressed for HCP groups; detect-and-route (no rows leave the pipeline). The `hcp` identifier is consistent across plugin, route, `recommendation_type`, and settings; CLI and server wiring are delivered (#590/#630/#631).

### W2 — Thin causality (ADR-0332) — shipped (thin MVP)

| Gate | Tracker |
|------|---------|
| R3 research | #387 — **done** (go with caveats) |
| Visibility | ADR-0330 / #397 |
| Wedge backlog | #404 (fuller causality remains out of scope) |
| Correlator ADR / implementation | #400 → ADR-0332; #624/#625/#628 and #644–#646 shipped |

**Shipped guardrails and limits:** W2 is a thin, advisory-only join with WATCH excluded and missing/stale evidence silent. Broader causality remains out of scope; M3 customer publication still requires the separate ownership/authorization/routing decision in #620. HCP packages are now accepted at the SNO ingress; confirm downstream processing and HC association before treating this lab as W2 end-to-end evidence.

### W3 — Unused HostedCluster (ADR-0333) — core implemented; evidence and optional slices remain

| Gate | Tracker |
|------|---------|
| R4 research | #388 — **done** |
| Wedge backlog | #391 (remains open as the W3 umbrella) |
| Platform questions | #398 (any future official HCP sleep?) |

**Implemented core:** usage/CPU idle evaluation and the NodePool-zero short lane emit the existing `review_unused_hosted_cluster` advisory (#658/#664/#673/#674). This is advisory-only; it does **not** recommend `pausedUntil`, hibernate, or auto-delete.

**Remaining:** #663 is an aged-evidence/API-profile falsification tracker whose original API-threshold question predates #664; #661's CP-waste estimate is gated on Family H; #665's cost-group augmentation has a built child (#675), but its live-consumption/closure decision remains outstanding. The #678 lab experiment found that scaled-to-zero user namespaces can disappear from digests; the rule must retain full-coverage/unknown semantics rather than infer idleness from missing data.

### W4 — Fleet headroom (ADR-0334) — **R5 complete: GO narrow**; code deferred

| Gate | Tracker |
|------|---------|
| R5 research | #389 — **done** (docs/sizing only; no lab capacity claims) |
| Wedge backlog | #392 |
| Baseline refresh | #409 (sizing guide URL in issue) |

**Plain English:** Estimate “room for more hosted clusters” using OpenShift’s published packing math and/or Multicluster Engine capacity metrics. Do **not** invent a universal number from one lab. Label HA vs single-replica, and say cloud/arch/version/API load change the answer.

The new lab resolves the prior one-HC setup gap, but is an off-matrix two-HC ARM64/SingleReplica sample and is not a portable capacity calibration. A read-only Prometheus query on MCE 2.17.3 returned request/low/average capacity gauges of 8, medium of 4, and high of 2 with two current HCs (illustrative headroom 6/6/6/2/0 respectively). This verifies gauge plumbing only; the SingleReplica/off-matrix lab does not validate the HA sizing assumptions. W4 remains postponed pending an explicit implementation greenlight.

### W5 — API tax (ADR-0335) — **R6 complete: GO with caveats**; code deferred

| Gate | Tracker |
|------|---------|
| R6 research | #390 — **done** |
| Wedge backlog | #393 |

**Plain English:** Find chatty service accounts / slow webhooks via a **small top-N digest** (OpenShift `APIRequestCount` + webhook Prom rollups) on **hosted and management**. Do not ship full per-user Prometheus history. Prefer “tune that operator/webhook” before add-nodes / blind CP blame.

The webhook half and reliability-adjacent work are distinct from the APIRequestCount slice. #657 remains gated because the operator has no APIRequestCount read path and the current lab has not established APIRequestCount data availability.

### Post-MVP wedges (placeholders only)

W6–W8: #394–#396 — no design depth until promoted. (#391–#393 design unlocked; coding still postponed.)

---

## Research status

| ID | Topic | Status |
|----|-------|--------|
| R1 #385 | Topology | Draft complete → ADR-0328 |
| R2 #386 | Management-as-workload | Draft complete → ADR-0331; ingest → ADR-0329 |
| #401 | Lab CSV/join | **Closed** — join PASS; Prom PASS; ROS CSV N/A (no operator) |
| #397 | Audience | Decisions → ADR-0330 |
| R3 #387 | Causality | **Complete** → ADR-0332 — **GO with caveats** for W2 |
| R4 #388 | Unused HC / lifecycle | **Complete** → ADR-0333 — **GO with caveats** for W3 |
| R5 #389 | Fleet admission headroom | **Complete** → ADR-0334 — **GO narrow** (docs/MCE; no lab N) |
| R6 #390 | API tax | **Complete** → ADR-0335 — **GO** (thin top-N digest; both planes) |

---

## Lab evidence summary

- Hosted: `controlPlaneTopology=External`, ClusterVersion `clusterID` = HC `spec.clusterID`
- Management (new lab, #583): HCP ns `hc01-infra-hc01` == filter — 39 CP/operator workloads, zero tenants/virt-launcher; ROS CSVs carry no pod-label columns, so W1 rules use namespace + workload pinning, not label matching; Prom requests present
- Management: no koku-metrics-operator in lab → ROS CSV not available
- Manual `cost_management_optimizations=true` applied for experiment; product path is auto-include (ADR-0329)
- **R3:** Prometheus CR Available on **both** planes; hosted API duration histogram query returns data (raw p99 needs PromQL hygiene — WATCH exclusion)
- **2026-09-14 live HCP lab (`hpe-apollo-cn99xx-16`, ARM64, off-matrix):** mgmt `hcp-mgmt` 4.22.12 compact + hosted `hc01` 4.22.12 (SingleReplica, 2 ARM64 workers, Agent platform). `controlPlaneTopology=External` confirmed on hosted. Hosted CP namespace is `hc01-infra-hc01` (not `clusters-hc01`); `SupportedHostedCluster=True`. Prometheus Available on both planes (mgmt 2 replicas, hosted 1). Runbook: `~/rh/kcli/hc01-hpe-apollo-cn99xx-16/ACCESS.md`.
- Lab operating lessons (deploy/RBAC, DNS, MCO drains, registry): `docs/agents/hcp-lab-operations.md`.

---

## Explicit non-goals (near term)

- Customer CTA to resize RH-managed shared CP
- Auto-remediation of control planes
- Replacing HyperShift runbooks
- Coding before wedge greenlight
- Full causality (webhooks, zombies, admission, noisy neighbor) in W2 v1

---

## When coding may start

1. Product owner gives an explicit per-wedge implementation greenlight. W0–W3 core work already has recorded greenlights; this is not blanket approval for future wedges.
2. Follow ADRs + this plan + planned-feature page — do not re-litigate locked decisions without a new ADR.
3. For W4/W5 and later wedges, satisfy the remaining evidence/ownership gates first; keep research issues distinct from implementation issues.

---

## Document history

| Date | Change |
|------|--------|
| 2026-08-03 | Initial design plan; ADRs 0328–0331; coding explicitly deferred |
| 2026-08-04 | R3 complete; ADR-0332; W2 GO with caveats; #400 satisfied |
| 2026-08-04 | Closed #385/#386/#397; R4 complete; ADR-0333; W3 GO with caveats |
| 2026-08-04 | R5 complete (docs/sizing only); ADR-0334; W4 GO narrow |
| 2026-08-04 | R6 complete; ADR-0335; W5 GO (top-N digest; cross-hooks) |
| 2026-10-14 | Reconciled delivery status, open evidence gates, and current HCP/SNO lab boundaries |
