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

Per covered day: user-namespace (platform exclusions shared with the zombie rule, including the synced cost-groups set) workloads at or above `SleepBHActivityFloorMC` in the business-hours stream = active; outside-BH CPU (the off-window p50 sum, measured directly — **amended #760**, see below) below `SleepOutsideBHIdleFloorMC` = idle outside. A day counts as covered only when **both** digest streams exist — partial BH coverage (schedule enabled mid-window) reads **unknown, never zero-activity**.

**Amendment (#761):** the both-streams rule is **per user workload, not merely per day**. A user workload with exactly one stream measured — the audit's night-only CronJob (`All:2000, AllMeasured:true, BHMeasured:false`) — makes the day **unknown**, never silently skipped while a sibling workload establishes the day's coverage. The missing BH row is a *normal producer shape*: `GroupCSVRowsForStream` drops weight-≤0 rows for the BH stream (the `off_hours_weight=0` fast path), and weight is time-based, not usage-based — a workload present-but-idle during BH still gets a zero-valued BH row, so BH-row-absent means zero rows during BH: night-only, silent-during-BH, or a reporting gap. The gate is conservative because absence of a BH row alone does **not** prove a specific off-hours magnitude; it reads unknown, and the write path treats unknown and veto identically (silence). **The conservative trade-off:** a single-stream user workload whose all-hours total is provably below `SleepOutsideBHIdleFloorMC` cannot actually break the signature (outside ≤ all), yet still silences the day — accepted deliberately so the coverage rule stays threshold-free (coverage is data completeness, not policy tuning; a floor-dependent gate would silently change meaning when thresholds are tuned). System-namespace workloads keep their exclusion ahead of the stream check — a platform workload's stream shape is irrelevant. The symmetric shape (BH row without an all-hours row) is structurally near-impossible from this producer (both streams derive from the same CSV rows; all-hours has no filter) and reads unknown for free.

**Amendment (#760) — the evidence model replaces the formula:** the original §5 formula computed outside-BH CPU as `all-hours p50 sum − BH p50 sum`. That subtraction was never valid: percentiles are not additive, and the complement of a percentile is not the difference of percentiles — a constant 300m workload reads both p50s as 300, outside as 0, and received high-confidence sleep advice while running all night (V15-10). The off-hours signal does not exist in the two-stream digest model, so it is now **measured and stored**: a thin `hcp_sleep_evidence` table (migration 000219) carries, per container-day, the p50 of in-window samples and the p50 of off-window samples — **exact complements**, computed through the canonical weighted digest machinery from the all-hours groups' complete samples, written only when a schedule is enabled (the BH-stream condition). The predicate reads the off-window p50 sum **directly** — no subtraction anywhere, which also eliminates the container-grain masking instance of the same defect (a BH-active container's p50 subtracted against a night-only container's within one workload's sums). Design choice: a purpose-built side table rather than a third `off_hours` digest stream — two payload columns instead of ~20, no write amplification on the hot `daily_container_digests` table, and zero blast radius on its unfiltered readers (the zombie rule and the correlate C-leg deliberately sum across streams; a third stream would silently recalibrate them). Per-**container** grain, not per-workload: the staged ingest path flushes in key chunks (a workload's containers can split across chunks), and replacement upserts at container grain keep replayed manifests idempotent — incremental workload sums would double on replay. Pruned symmetrically with the BH digests when schedules are removed. **Cold start:** historical days predate the writer, so the 14-day window reads unknown until evidence accumulates post-deploy (sleep advisories silent for up to 14 days, closable per scope by the existing `reship_ros` rebuild — which also bounds the schedule-*change* window: changed schedules leave stale-split evidence until the reship rebuild completes, the same staleness bound the BH detail nest accepts). The BH-activity leg is now the pure in-window p50 (today's BH digest with `off_hours_weight>0` blends off-hours samples at fractional weight — the evidence legs are exact complements by construction). The #761 workload-grain coverage gate carries over unchanged: either leg unmeasured reads unknown.

Two boundary clarifications from the fix-pass review. (1) **The split grain is the CLUSTER schedule, not the namespace-resolved one:** the sleep predicate resolves the HC's cluster/org schedule, and its question is "is anything active outside THAT window?" — a wider namespace override would split the off leg at the wider window and under-veto hours the advice would sleep away; the cluster split is safety-correct (a narrower namespace override only makes the off leg more inclusive — conservative). (2) **Days the schedule does not apply to** (a weekday schedule's weekends: no in-window hours exist, so the producer emits no BH evidence) gate on the off leg alone — the BH-activity requirement is vacuous (there are no business hours to be active in), and off-window activity still vetoes because the advice sleeps those hours. Without this, the digest-stream model read every weekend as unknown and the sleep rule never fired for weekday schedules at all — a test-masked defect (seeders wrote weekend BH rows the producer would never emit). Retention: the table is swept by the date-based retention (digest-month cutoff) and cleaned with its source by the housekeeper — the migration's "bounded" claim is enforced, not aspirational.

**Amendment (#826) — the off-leg statistic is p95, not p50.** The off-window evidence leg stores and gates on the **p95** of the off-window samples (migration 000220; the BH-activity leg keeps its p50). A median hides minority-of-window activity: a workload running 2000m for 6 of 16 off-hours reads off-p50 = 0, and two containers alternating across the off-window leave the workload active 14 of 16 hours while each container's p50 is 0 — both produced high-confidence sleep advice (V16-02). The choice follows the house convention rather than a new trade-off: **CPU-usage activity evidence gates on p95 everywhere** — the zombie rule made the identical p50→p95 migration for the identical reason in #747 ("the p50 reads partial-day activity as idle"), the C-leg gates on p95, and the container sizing engine's usage statistic is p95; p99 is reserved for latency SLIs (the H leg, api-tax). The workload grain SUMs the per-container p95s. **What the statistic proves (stated honestly, #826 review correction): a per-container p95 bounds that container's *sustained* activity — it catches any activity present in more than ~1/16 of the window's samples (at 16 hourly samples: ≥2 hours; a single-hour burst reads p95=0, verified against the counting-sort core). The sum across containers therefore catches any workload whose activity concentrates in one container for ≥2 hours, or spreads across containers at ≥2 hours each — but a fleet rotating one active hour per container (16 containers × 1 hour) evades it while the workload is active throughout the window. That residual is the hour-grain coverage question, tracked separately (see #835), not a claim this amendment makes. No threshold change: p95 ≥ p50 always, so the veto is strictly more sensitive against the same `SleepOutsideBHIdleFloorMC` — the floor remains the sensitivity knob. Cold-start round 2: pre-#826 rows carry no p95, read off-unmeasured, and the #761 gate makes those days unknown until re-ingestion (the reship bound, as before).

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
