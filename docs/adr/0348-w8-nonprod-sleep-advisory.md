# ADR-0348: W8 Non-Prod HC Sleep — Advisory-First, Declared Classification

## Status

Accepted (design) — implementation tracked in #716 (#715 operator side, #717 serving, #718 docs)

## Phase

HCP fleet FinOps — W8 non-prod sleep schedules (Family J); reconciles ADR-0333's "Family J stays separate" line

## Context

W8 was promoted (#396, 2026-10-30) with both criteria met: the business-hours feature shipped (dual digests — container recommendations on business-hours and all-hours streams), and the SingleReplica-HC hibernate/resume experiment validated the mechanism (two-step sleep: `pausedUntil` + NodePool→0 with a finite `nodeDrainTimeout`; resume by full reprovision, ~18–20 min; worker capacity reclaimed only — the CP keeps running).

Owner shape decision (2026-10-30): **(a) advisory first — this ADR**; (b) remote action = runbook + `oc` (a robne k8s-client enhancement was considered and rejected — the audience already holds `oc`, the runbook encodes the gotchas, and scripted-with-guardrails is the controller's job); local action = in-cluster endpoint possible; **(c) scheduled orchestration = #714** (service-mode needs an operator-side bridge — no reverse channel exists from a central Cost Management instance to a reporting cluster).

## Decision

### 1. Advisory-only, declared-gated

ROS recommends; it never sleeps anything. The advisory fires only when **both** gates pass:
- **Declaration**: the HostedCluster carries `insights.cost-management/hc-tier=non-prod`.
- **Signature**: user workloads are active during the org's business hours and idle outside them (dual digests).

The declaration *classifies*; the usage data *proves sleeping is safe*. A labeled non-prod HC running overnight workloads gets **no** advisory — sleeping it would break those workloads.

### 2. Classification is declared in v1 (inference discovery deferred)

The label joins the established ROS label family (`insights.cost-management/optimizations` on namespaces is already read by KMO's collectors) and is valid unchanged upstream (koku — the Insights cost management upstream) and downstream (Red Hat Insights / Lightspeed Cost Management).

**Why not inference:** the overnight-quiet-prod trap — a prod cluster quiet at night is *indistinguishable* from a dev cluster in usage data. Only a declaration separates them. An inference-based discovery advisory for unlabeled clusters (hedged copy, medium confidence) is a v1.1 follow-up.

### 3. Tier read rule — latest-wins, including empty

The tier is the label value on the **most-recent complete snapshot row** for that HC.

**Why latest-wins (documented per owner request, 2026-10-31):** snapshot rows are repeated observations of one admin-controlled label, not independent votes. **Majority-wins was rejected because it fails in the dangerous direction**: a `non-prod → prod` relabel near the window's end leaves the old value in the majority for up to the full window — the advisory would keep firing on a cluster the administrator just declared production. **Latest-non-empty was rejected for the same reason in reverse**: label removal (or an operator regression that stops emitting the field) must stop the advisory immediately; a latest-non-empty hybrid would keep recommending sleep for a cluster the administrator just un-declared. ADR-0339's sibling precedent: #621's all-rows-agree strictness governs *identity* (an HC's ID must be unambiguous), not mutable admin labels — recency is the correct resolver for intent.

### 4. Business-hours schedule constraint — org/cluster level only

The signature resolves against the **deepest non-namespace schedule**: the cluster-scoped row if present, else the org-wide default. Namespace-scoped schedules do **not** define an HC's business hours for sleep purposes — with per-namespace windows, "the HC's business hours" is ill-defined. The BH feature has no granularity gate today (all three row scopes are open to any org); this is a **rule-side v1 constraint**, not a BH-feature change. An org with no org/cluster-level schedule gets no sleep advisory — no schedule is invented.

### 5. Signature predicate and the zombie partition

Per covered day: user-namespace (platform exclusions shared with the zombie rule, including the synced cost-groups set) workloads at or above `SleepBHActivityFloorMC` in the business-hours stream = active; outside-BH CPU (all-hours − business-hours) below `SleepOutsideBHIdleFloorMC` = idle outside. A day counts as covered only when **both** digest streams exist — partial BH coverage (schedule enabled mid-window) reads **unknown, never zero-activity**.

**Partition with the zombie rule (ADR-0349 §3):** idle-always → zombie; active-in-BH-and-idle-outside → sleep candidate; around-the-clock → neither. Both rules use day-truncated windows, so the predicates are the only thing preventing a same-PK clobber — pinned by test.

### 6. Window and policy

Day-truncated window, `SleepWindowDays` (default 14), per-org tunable alongside the existing correlator policy fields. All thresholds config-derived, defaults documented, never hardcoded in tests.

### 7. Copy constraints (ADR-0333 carry-over, verbatim)

The reclaim is **worker capacity only — the control plane keeps running**; `pausedUntil` is never framed as the saving mechanism (it pauses reconciliation only — validated by the #396 experiment); wake is stated as ~20 minutes of full reprovision; **no dollar amounts** (Family H attribution postponed, #627). The reclaim estimate is node-hours/week: worker nodes (pool replicas from `hosted_nodepool_rollups`) × non-BH hours of the resolved schedule.

### 8. ADR-0333 Family J reconciliation

0333's "Family J stays separate: no reliable native HCP hibernate yet" is revisited: the mechanism is now validated on-prem (Agent platform, 4.22). The advisory names the action (NodePool→0 + pause with finite `nodeDrainTimeout`, per the runbook) but 0333's product-copy table otherwise carries over unchanged.

## Consequences

- Future correlator rules remain under the ADR-0349 partition obligation.
- The declared channel is operator-borne (`hcTier` JSON field on the topology emission, #715); no channel exists for RH-operated management clusters (service mode) until #714 or a koku-side list — deliberate, on-prem first.
- Serving: verdict `schedule_nonprod_sleep` with the ADR-0349 field table (`declared_tier`, `bh_active_peak_cpu_mc`, `outside_bh_peak_cpu_mc`, `worker_node_count`, `reclaim_node_hours_per_week`).
- Inference discovery (v1.1), the in-cluster action endpoint (b-local), and orchestration (c) are all explicit non-goals of v1.

## Related Decisions

- [ADR-0333](0333-unused-hostedcluster-lifecycle-w3.md) — advisory copy constraints; the pausedUntil no-go this ADR's experiment confirmed
- [ADR-0339](0339-w3-idle-leg-workload-cpu-primary.md) — the workload-CPU evidence model the signature reuses
- [ADR-0349](0349-hcp-advisories-serving-api-contract.md) — the serving contract and the partition invariant
- #396 (W8 umbrella + the hibernate experiment), #715/#716/#717/#718 (children), #714 (stage c)
