# HCP / Fleet Control-Plane Optimization — Design Plan (no code yet)

**Status:** Design / research documentation — **implementation coding deferred**  
**Planned feature (public):** [hosted-control-plane-fleet-optimization.md](../docs-site/planned-features/hosted-control-plane-fleet-optimization.md)  
**Epic:** [#384](https://github.com/pgarciaq/ros-ocp-backend/issues/384)

This plan freezes **what we will build** and **decisions already accepted**. Coding starts only after an explicit greenlight per wedge.

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
| **controlplane** | Planned `recommendation_type` value tagging W1 output; new API surface, coordination rides along |
| **`cost_management_optimizations`** | Namespace label gating container + ns-quota collection (frozen meaning); HCP namespaces auto-included without it |

---

## MVP features to implement (later coding)

### W0 — Topology (ADR-0328) — design greenlit; code deferred

| Slice | Design issue | Deliverable (when coding) |
|-------|--------------|---------------------------|
| W0.1 | #406 | Operator: manifest topology facts |
| W0.2 | #407 | Backend: classify + persist `cluster_topology` |
| W0.3 | #408 | Node path: suppress/annotate hosted |

**Out of W0:** consolidation algorithm rewrite; cross-plane join; CP rightsizing.

### W1 — Management CP rightsizing (ADR-0331) — design accepted; code after ADR-0329 ships in env

| Prerequisite | Notes |
|--------------|-------|
| ADR-0329 / #405 | Operator collects HCP ns |
| Skeleton #403 | Impl children only after coding greenlight |

### W2 — Thin causality (ADR-0332) — **R3 complete: GO with caveats**; code deferred

| Gate | Tracker |
|------|---------|
| R3 research | #387 — **done** (go with caveats) |
| Visibility | ADR-0330 / #397 |
| Wedge backlog | #404 |
| Correlator ADR | #400 → ADR-0332 |

**Caveats:** needs new SLO series in operator; refined PromQL (exclude WATCH); management collection (RH will run on ROSA/ARO when ready); prefer suppress “add workers” over chatty CP blame.

### W3 — Unused HostedCluster (ADR-0333) — **R4 complete: GO with caveats**; code deferred

| Gate | Tracker |
|------|---------|
| R4 research | #388 — **done** |
| Wedge backlog | #391 |
| Platform questions | #398 (any future official HCP sleep?) |

**Plain English:** If the hosted cluster looks unused for days but the control plane is still running on management, advise review/delete. Do **not** recommend `pausedUntil` to save money (it does not stop the control plane). ROSA HCP has no native hibernate today.

### W4 — Fleet headroom (ADR-0334) — **R5 complete: GO narrow**; code deferred

| Gate | Tracker |
|------|---------|
| R5 research | #389 — **done** (docs/sizing only; no lab capacity claims) |
| Wedge backlog | #392 |
| Baseline refresh | #409 (sizing guide URL in issue) |

**Plain English:** Estimate “room for more hosted clusters” using OpenShift’s published packing math and/or Multicluster Engine capacity metrics. Do **not** invent a universal number from one lab. Label HA vs single-replica, and say cloud/arch/version/API load change the answer.

### W5 — API tax (ADR-0335) — **R6 complete: GO with caveats**; code deferred

| Gate | Tracker |
|------|---------|
| R6 research | #390 — **done** |
| Wedge backlog | #393 |

**Plain English:** Find chatty service accounts / slow webhooks via a **small top-N digest** (OpenShift `APIRequestCount` + webhook Prom rollups) on **hosted and management**. Do not ship full per-user Prometheus history. Prefer “tune that operator/webhook” before add-nodes / blind CP blame.

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

1. Product owner says “implement W0” (or W1 after operator ADR-0329 available; or W2 after SLO series design).
2. Follow ADRs + this plan + planned-feature page — do not re-litigate locked decisions without a new ADR.
3. Open implementation PRs against the design issues; keep research issues for R4+.

---

## Document history

| Date | Change |
|------|--------|
| 2026-08-03 | Initial design plan; ADRs 0328–0331; coding explicitly deferred |
| 2026-08-04 | R3 complete; ADR-0332; W2 GO with caveats; #400 satisfied |
| 2026-08-04 | Closed #385/#386/#397; R4 complete; ADR-0333; W3 GO with caveats |
| 2026-08-04 | R5 complete (docs/sizing only); ADR-0334; W4 GO narrow |
| 2026-08-04 | R6 complete; ADR-0335; W5 GO (top-N digest; cross-hooks) |
