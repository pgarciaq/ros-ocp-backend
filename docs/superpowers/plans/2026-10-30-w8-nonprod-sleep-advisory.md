# W8 Non-Prod HC Sleep — Advisory v1 (stage a) Design + Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Detect **declared** non-prod HostedClusters whose user workloads follow a business-hours usage signature (active during the org's configured business hours, idle outside) and emit a **high-confidence** advisory recommending an off-hours worker sleep schedule — **served to users via a new advisories endpoint**, which also surfaces the existing zombie/headroom/api-tax advisories for the first time. Copy complies with ADR-0333 (worker-capacity reclaim only; the control plane keeps running; wake ≈ 20 min full reprovision). **Advisory only — ROS never sleeps anything** (owner shape decision 2026-10-30: (a) now; (b) runbook+`oc` remote / in-cluster endpoint local; (c) service-mode bridge = #714).

**Architecture, three layers:**
1. **Declaration**: an admin labels the HostedCluster on the management cluster (`cost-management.io/hc-tier=non-prod`); the operator (KMO today, robne-operator after the soft-split) — which **already reads HostedCluster objects** for the topology CSV feeding `manifest_hcp_snapshots` — gains one column carrying the tier label; ROS ingests it as a new snapshot column.
2. **Detection**: a new correlator pass in `internal/engine/correlate` (mirroring the zombie contract): enumerate fresh complete HC candidates, gate on the declared tier, evaluate the business-hours signature from the dual digests (`daily_container_digests`, `schedule_type` `all_hours` vs `business_hours`), write `hcp_correlation_advisories` rows with verdict `schedule_nonprod_sleep`, confidence `high` (declared + signature-confirmed). Rides `RunCycle` (hourly since #711); shares sweep, metrics, and the never-fail contract.
3. **Serving**: `GET` endpoint reading `hcp_correlation_advisories` generically — all verdicts (zombie, headroom, api-tax, sleep) — behind the standard v1 identity/RBAC middleware, org-scoped, documented in `openapi.json`.

**Tech Stack:** Go 1.26 (`internal/engine/correlate`, `internal/api`, migrations), koku-metrics-operator (cross-repo: topology CSV column), ADR-0348, docs-site.

**Issues:** #396 (W8 wedge — promoted 2026-10-30; this plan + children). ADR-0333 (copy constraints + Family J reconciliation). #714 (stage c). #674 (NodePool-zero short lane — interplay note). #665 (cost-groups sync — the synced exclusion set is shared by the signature evaluation).

## Global Constraints

- **Advisory only.** No code path mutates HostedCluster/NodePool. The recommendation names the *action* (NodePool→0 + pause with finite `nodeDrainTimeout`, per the #396 experiment runbook) but never performs it.
- **ADR-0333 copy constraints, verbatim carry-over:** the reclaim is **worker capacity only — the control plane keeps running**; `pausedUntil` is never framed as the saving mechanism; wake is stated as ~20 min full reprovision; no dollar amounts (Family H postponed, #627).
- **Classification is DECLARED in v1** (owner decision 2026-10-30). The label is the declaration; **the BH signature still gates**: a labeled non-prod HC with overnight-active workloads gets NO advisory (sleeping it would break those workloads). Declaration classifies; usage data proves sleeping is safe. The inference-based discovery advisory for unlabeled clusters (hedged copy, medium confidence) is **v1.1, deferred** — rationale: a prod cluster quiet at night is indistinguishable from a dev cluster in usage data; only a declaration separates them.
- **Predicate partition (no overlap with the zombie rule):** zombie = idle **always**; sleep candidate = declared non-prod **and** active in BH **and** idle outside them; an around-the-clock cluster matches neither.
- **Thresholds are config-derived** (per-org policy, mirroring `ZombieIdleCPUFloorMC`); defaults documented; never hardcoded in tests.
- **Requires a configured business-hours schedule** (owner-confirmed: no invented schedules). No configured schedule → no evaluation. The advisory says "outside *your* business hours".
- **Never-fail-on-evidence** shared with the zombie pass: evidence problems are silence; only infrastructure errors propagate.
- **Cross-repo discipline** (docs/agents/cross-repo-contracts.md): the operator CSV column and the ROS ingestion land as separate children with the contract pinned on both sides; unique-tag lab images for E2E.
- `make lint` and `make test-short` green at every task boundary.

## Review Focus (each pinned by its test)

1. **Declared + BH signature → fires** — labeled non-prod, workloads active in BH and idle outside on every covered day → `schedule_nonprod_sleep`, high confidence, `n_signals` carrying declaration source + signature stats + node-hours estimate. Fixtures use **distinct** BH/outside values. Pinned by `TestSleepCandidate_DeclaredAndBHSignatureFires`.
2. **Declared but overnight-active → NO advisory** — the declaration does not override the data gate; sleeping overnight workloads would break them. Pinned by `TestSleepCandidate_DeclaredOvernightActiveIsSilent`.
3. **Unlabeled → never fires in v1** — no declaration, no advisory, regardless of signature. Pinned by `TestSleepCandidate_UnlabeledIsSilent`.
4. **Zombie partition** — an always-idle cluster routes to the zombie rule, never the sleep advisory, and vice versa. Pinned by `TestSleepCandidate_ZombiePartition`.
5. **Unknown days are silence** — days without measurable digests read unknown, never idle-by-absence. Pinned by `TestSleepCandidate_UnknownDaysSilent`.
6. **Config-derived thresholds** — tests run non-default floors so a hardcoded regression fails for the right reason. Pinned by `TestSleepCandidate_NonDefaultFloors`.
7. **Advisory lifecycle** — in-place refresh on hourly reruns (same window PK), sweep expiry, metrics counter. Pinned by `TestSleepCandidate_AdvisoryLifecycle`.
8. **Serving endpoint** — returns all verdicts org-scoped under the standard identity middleware; shape documented in `openapi.json`; empty/expired states clean. Pinned by handler integration tests.

## Tasks

### Task 1: ADR-0348 + planned page (child: backend advisory)

- [ ] `docs/adr/0348-w8-nonprod-sleep-advisory.md`: advisory-first decision; **declared-classification-first policy** with the overnight-quiet-prod rationale (and inference discovery deferred to v1.1); ADR-0333 copy-constraint carry-over and the "Family J stays separate" reconciliation (mechanism validated by #396); the action-capabilities-by-mode matrix (remote = runbook + `oc` only — robne k8s enhancement decided against 2026-10-30; local = in-cluster endpoint; service = #714 bridge). Update both ADR indexes.
- [ ] Planned page W8 row → In implementation (done at promotion; cite this plan and children).
- [ ] `make docs-lint docs-drift docs-sync-check`.

### Task 2: Operator topology CSV — tier label column (child: operator, cross-repo)

- [ ] koku-metrics-operator: the HCP topology report gains the HostedCluster's `cost-management.io/hc-tier` label value (empty when absent); nise generator counterpart if the topology CSV has one. Contract: column name + semantics pinned in the child; fork-branch delivery per the fork strategy.

### Task 3: ROS ingestion — snapshot tier column (child: backend advisory)

- [ ] Migration: `manifest_hcp_snapshots` gains `hc_tier text NOT NULL DEFAULT ''`; the topology-CSV parser maps the new column; ingest tests with the contract fixture (labeled, unlabeled, empty).

### Task 4: Detection + advisory write (child: backend advisory)

- [ ] `internal/engine/correlate/policy.go`: `SleepBHActivityFloorMC`, `SleepOutsideBHIdleFloorMC` — per-org tunable, clamp-validated.
- [ ] `internal/engine/correlate/sleep.go`: shared candidate enumeration (reuse `enumerateZombieCandidates`); BH schedule resolution per org (none → silent skip); signature evaluation with the platform-exclusion set shared with the zombie rule (incl. the synced cost-groups set); reclaim estimate = worker nodes × non-BH hours (node-hours/week, no $); advisory write mirroring `writeZombieAdvisory` semantics with `n_signals = {hc, declared_tier, bh_active_peak_mc, outside_bh_peak_mc, bh_schedule, worker_nodes, reclaim_node_hours_per_week, action: "nodepool-scale-to-zero + pausedUntil (runbook); CP keeps running; wake ≈ 20 min reprovision"}`.
- [ ] Wire into `RunCycle` after the zombie pass; metrics counter via the existing registry.
- [ ] Review Focus tests 1–7.

### Task 5: Serving endpoint (child: endpoint)

- [ ] Route + handler + serializer for `hcp_correlation_advisories` (all verdicts) behind the v1 identity middleware, org-scoped; `openapi.json` updated; handler integration tests (Review Focus 8); IQE/constants note for the downstream test suite.

### Task 6: Docs + changelog (child: docs)

- [ ] docs-site feature page: the advisory user-facing — the classification section the owner required (what the label means, what the BH signature means and does **not** mean, CP-keeps-running, 20-min wake); the advisories endpoint documented; Class A page with Last-verified.
- [ ] `docs-site/changelog.md`; cross-links #396/#714.

## Deferred (explicit non-goals of v1)

- **Inference-based discovery advisory** for unlabeled clusters (hedged copy, medium confidence) — v1.1.
- **UI card** (koku-ui child — filed once the endpoint shape ships).
- **Service-mode declaration channel** (customers can't label RH-operated HostedClusters; koku-side curated list per the cost-groups-sync pattern, or #714's pull channel).
- Any action/orchestration (#714); dollar attribution (#627); robne CLI cluster capabilities (decided against).
