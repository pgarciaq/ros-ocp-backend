# Design: Emission — Recommendation CRs + Events (Spec 1 of 3)

## Status

**Draft — §§1–4 approved, §5 proposed (awaiting approval).** Part of the full-scope program
(emission + Local actuation + Machine API-mediated spot) decomposed into three
sub-specs, each with its own spec → plan cycle. This file is Spec 1.

| Date | Event |
|------|-------|
| 2026-09-16 | Scope locked: full (emission + actuation + spot); 3-spec decomposition |
| 2026-09-16 | Spot mechanism locked: Machine API-mediated (no direct cloud write creds) |
| 2026-09-16 | Emission scope locked: Rec CRs + aggregated events |
| 2026-09-16 | Emission approach locked: per-workload CRs (bounded) + namespace rollups |
| 2026-09-16 | §1 approved (per-workload grain + rollup CR, on advice) |
| 2026-09-16 | §2 approved with correction: central cannot write in-cluster CRs (see §2) |
| 2026-09-16 | §§1–2 written to this file |
| 2026-09-16 | §3 approved: consumer contracts + loop-prevention |
| 2026-09-16 | §4 approved: guardrails shared with Spec 2 |

## Context and non-goals

The ROS native engine is advisory-only today (ADR-0276: "no cluster mutations,"
premised on central SaaS having no write path). This spec changes nothing about
actuation — it only specifies how recommendations and opportunities become
visible to external automation (EDA, ACM policies, Kyverno, GitOps). No new ADR
required: emission is purely additive.

Actuation (Local operator apply, autoscaler bounds) is Spec 2 and needs an
ADR-0276 successor. Spot provisioning (Machine API-mediated) is Spec 3 and
needs in-cluster-RBAC security review. Neither is designed here.

## Section 1 — CRD shape and lifecycle (approved)

Two CRDs, both **namespaced** (simple RBAC and tenancy). Nothing
cluster-scoped in v1; fleet rollup stays central's job via the Hybrid push.

### `ResourceRecommendation` — one per workload

Actuation patches whole pod specs, so workload grain (not per-container —
that would multiply objects 2–4× for precision no consumer can use).
Per-container sizing rides inside as a list.

- `spec` — human/policy knobs only:
  - `enginePreference: cost | performance`
  - `term: short | medium | long`
  - `paused: true` — opt-out of all automation for this workload
    (the per-workload kill-switch that Spec 2 will depend on).
- `status` — machine-written each cycle:
  - per-container requests/limits per engine + term
  - `confidence`, `dataDays`, `notificationCodes`
  - `observedGeneration` of the target workload (staleness detection)
  - `expiresAt` — TTL refreshed each cycle.
- GC: `ownerReference` to the target workload (workload deleted → CR dies
  free) plus a reaper for `expiresAt`, so stale CRs can never drive action
  after data stops flowing.

### `ResourceOptimizationOpportunity` — one per namespace

Rollup: actionable-workload refs, counts by class, total estimated savings.
This object **is** the event payload for EDA/HCC — one webhook per namespace
per cycle, never a per-container firehose. It is also the cheapest read for
dashboards and alert rules.

## Section 2 — emission paths and transports per mode (approved, with correction)

Rule: **whoever computes, writes.** No cross-boundary write credentials
anywhere.

| Mode | CR writer | Event transport | Fidelity |
|------|-----------|-----------------|----------|
| Local | Operator engine loop → etcd each cycle | K8s Events on the CR + summary metrics → Alertmanager webhook → customer EDA | Full, per-cycle |
| Hybrid | Same on-cluster write as Local | Same local transports, plus existing JSON push central can fan out from | Full locally; central re-emits fleet rollups |
| Remote | Operator **emit-only task**: polls central API, writes CRs locally (no engine) | K8s Events + Alertmanager webhook, same as Local | Full, at API-poll cadence |
| SaaS | None in-cluster (no write path). CRs only if customer GitOps/ACM pulls central API (documented recipe, not our code) | Central produces Kafka messages; HCC Notifications webhook → customer EDA | **Aggregated rollups only**, rate-limited |

Correction log: an earlier draft had central writing CRs for Remote/SaaS.
Central has no cluster write path — signals flow outward, objects do not.

Protocol decisions:

- K8s Events are **signals** (`ResourceRecommendation X updated`), never
  payloads (size caps, dedup semantics). Consumers read the CR.
- The namespace rollup (§1) is the **only cross-boundary payload shape** —
  HCC, Kafka, and Alertmanager webhooks carry the identical schema, one
  versioned contract to test and document. This enforces the SaaS
  aggregated-only constraint structurally, not by convention.

## Section 3 — consumer contracts and loop-prevention (approved)

What ACM policies, Kyverno rules, and EDA playbooks may assume — and the
rules that keep automation from oscillating or acting on rot:

1. **Read `status`, honor `spec`.** Consumers must never write `status`,
   and must stop when `spec.paused: true`. A policy that can't name its
   off-switch doesn't ship.
2. **Check `observedGeneration` against the live workload.** If the
   workload changed since the recommendation was computed, the CR is
   advisory history, not an instruction. Requeue, don't apply.
3. **Confidence gates are mandatory, values are per-consumer.** The CR
   carries `confidence`, `dataDays`, and `notificationCodes` (e.g.
   SPARSE_DATA); each consumer declares its floor (suggested default:
   skip low-confidence and sparse-data rows). The contract guarantees
   the *fields*, not the *thresholds*.
4. **Apply-then-suppress.** After actuating, the consumer must see its own
   change reflected: next cycle's engine observes the new requests,
   recomputes, and either confirms (workload now optimal → CR ages out)
   or revises. No consumer may re-emit or re-apply off a CR whose
   `observedGeneration` already matches the applied state — the
   anti-oscillation rule.
5. **Events are idempotent triggers.** Receiving the same namespace-rollup
   twice must be a no-op: consumers diff desired (CR) vs live (cluster)
   before acting. Playbooks that blindly patch on every webhook are
   non-conformant.
6. **Engine/term pinning.** Consumers declare which engine
   (`cost`/`performance`) and term they follow; mixing cost-sizing from
   one cycle with performance-sizing from the next is forbidden. The CR
   carries all engines/terms precisely so different consumers can pin
   differently without new objects.

## Section 4 — guardrails shared with Spec 2 (approved)

Designed once here (emission must carry them); Spec 2 will depend on them:

1. **`spec.paused` semantics.** Per-workload opt-out, honored by all
   consumers (§3.1). Namespaces opt out via a label
   (`robne.io/automation: disabled`) that suppresses CR creation
   entirely — no objects, no events, no cost. No global kill-switch
   beyond the operator's existing plugin toggles; blast radius is always
   workload- or namespace-scoped.
2. **Dry-run / diff first.** Every consumer path must support a dry-run
   mode reporting *would-change* without patching, and the rollup CR
   carries `pendingActionCount` vs `appliedActionCount` so dashboards show
   the queue. Actuation without a visible queue is forbidden.
3. **Audit trail.** Every applied change traceable to the authorizing CR
   version: consumers annotate patched workloads
   (`robne.io/applied-from`, `robne.io/applied-generation`, timestamp).
   History tables close the loop — adoption/stability metrics become the
   automation scoreboard for free.
4. **Gates compose in order:** paused → generation → confidence → pinning
   → dry-run → apply → audit. Skipping straight to apply violates the
   contract.

## Section 5 — testing, rollout, docs (proposed)

1. **Testing per mode.** Local/Hybrid: envtest-based controller tests (CR
   lifecycle, TTL reaper, ownerRef GC) + a chaos case (workload edited
   mid-cycle → consumer must requeue per §3.2). Remote emit-only:
   contract tests against recorded central API fixtures. SaaS: HCC
   payload schema tests + rate-limit tests (burst of N namespaces →
   exactly N rollup events, zero per-container leakage). Cross-mode: the
   rollup schema has one golden-file test shared by all three transports.
2. **Rollout.** Emission ships dark first: CRs written but no events fired
   (operator/central flag), one release to validate object counts and
   etcd impact against the librobne scale estimates — *then* events
   enabled per transport. SaaS last (HCC integration review). No
   migration: purely additive, nothing existing changes behavior.
3. **Docs.** Public contract (`docs-site/`) for the two CRDs + rollup
   schema with `Last verified` dating; consumer recipes (ACM Policy
   sample, EDA rulebook sample, Kyverno mutate sample) as docs, not
   code. Internal (`docs/`): this spec plus the Spec 2/3 follow-ups.
