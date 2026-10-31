# MachineSet Tier 2a Documentation Alignment Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Align issue #47, the internal Tier 2a spec, public requirements, and ADRs around Machine API-backed node-count recommendations and their availability limits.

**Architecture:** Tier 2a remains advisory and derives a recommended node count from the existing MachineSet-first node-consolidation results. A timestamped MachineSet API snapshot provides desired count (`.spec.replicas`), observed Machine count (`.status.replicas`), and available Machine count (`.status.availableReplicas`). Emit a target only when these counts agree with linked Node inventory and node-recommendation coverage is complete; otherwise withhold it. Bare Metal Operator inventory is not a fallback for MachineSet membership or desired count. PDB and placement simulation stay separate investigations (#709 and #710).

**Tech Stack:** Markdown documentation, GitHub issue comments via `gh`, repository documentation checks.

**Spec:** `docs/features/machineset-recommendations.md`, aligned with `docs-site/planned-features/machineset-recommendations.md`, `docs-site/architecture/requirements.md`, and the approved decisions in this session.

## Global Constraints

- Do not overwrite issue #47’s body; record alignment in a comment.
- Customer-facing language uses **node count** and explains that MachineSet `.spec.replicas` is the desired number of Machine/node instances, not application-pod replicas.
- Treat `.spec.replicas` as the desired node-count baseline; distinguish it from `.status.replicas` and `.status.availableReplicas`, which count Machines.
- Do not claim `machine_api_machine_set_spec_replicas` is a standard, currently available OpenShift metric; the documented MAO metrics and the MachineSet API are separate sources.
- For a target, require desired, observed, and available counts to agree with the member-node inventory and complete fresh recommendation coverage; on mismatch or partial coverage, return no target.
- Document least-privilege KMO `list` permissions for `machines` and `machinesets` in both RBAC source and OLM permissions as a required implementation dependency; the collector uses list operations, so do not add `get` unless implementation needs it.
- Scope Machine API list requests to `openshift-machine-api`; if OLM cannot express a namespace-scoped binding, resolve and review the least-privilege alternative before implementation.
- The KMO snapshot is an allowlisted projection only (identity, replica/status counts, timestamp, and node mapping); do not export full provider specs or credential references.
- Persist MachineSet namespace/UID and the source snapshot timestamp; require retry-safe history keyed by snapshot identity and a deterministic recommendation fingerprint.
- If the Machine API resource is absent, MachineSet recommendations are not applicable and node-level recommendations continue. If it is present but reads fail or data is stale/incomplete, mark MachineSet data unavailable and withhold the target.
- Expose per-cluster collection status in MachineSet list metadata keyed by `cluster_uuid`, so empty results distinguish no recommendation from unsupported/incomplete collection and one cluster does not hide valid rows for another.
- Preserve the currently documented IPI support scope unless compatibility is validated for more topologies; explain that UPI alone does not prove either Machine API presence or absence.
- BMO/BareMetalHost inventory is optional bare-metal context, not a substitute for MachineSet desired count or membership.
- Tier 2a does not include PDB/affinity simulation or automatic application; track those separately in #709 and #710. Tier 2b remains independent.
- Keep accepted ADR-0278 as historical record; add a new ADR that supersedes its deferral decision rather than rewriting it.
- Do not commit or push before explicit authorization; the user authorized committing and pushing this documentation alignment on 2026-10-31.

## Review Focus

- **API absent vs. RBAC/read failure:** public/internal text must distinguish not-applicable from incomplete data and never silently infer a target.
- **Desired vs. observed/available counts:** field descriptions must not conflate `.spec.replicas` with Machine or Ready/Available status.
- **Prometheus metric availability:** remove unverified desired-count metric assumptions; identify any status metrics only with verified names and scope.
- **UPI/platform topology:** do not use “UPI” alone as the Machine API capability test; say capability depends on the actual API/platform.
- **BMO relationship:** do not imply BareMetalHost inventory maps to a MachineSet or cluster Node without an explicit, verified join.

---

### Task 1: Record the revised decision in a superseding ADR

**Files:**
- Create: `docs/adr/0347-machineset-tier2a-customer-demand-and-machine-api-gate.md`
- Modify: `docs/adr/README.md`
- Reference: `docs/adr/0278-machineset-tier2-engine-deferred-scope-criteria.md`

- [x] Add an accepted ADR superseding ADR-0278’s deferral in light of confirmed customer demand, while preserving the original ADR unchanged.
- [x] Record the bounded Tier 2a advisory model, the `.spec.replicas` baseline, Machine API/RBAC/data-coverage gates, BMO non-fallback decision, and separate PDB/placement investigations.
- [x] Add ADR-0347 to the index and verify its links/status/number against neighboring ADRs.

### Task 2: Reconcile the internal implementation spec

**Files:**
- Modify: `docs/features/machineset-recommendations.md`

- [x] Replace “ready to implement” with the correct readiness state and explicit KMO data/RBAC dependency.
- [x] Align Tier 2a math with the existing MachineSet-first `node_count_reduction` model; remove the conflicting utilization-ratio/HA-floor claims and state a default minimum of 1 without promising HA.
- [x] Define `desired_node_count` from `.spec.replicas`, `observed_machine_count` from `.status.replicas`, and `available_machine_count` from `.status.availableReplicas`; emit a target only when these agree with linked Node inventory and fresh node-recommendation coverage.
- [x] Persist the MachineSet namespace/UID and source snapshot time; make history writes retry-safe rather than relying on `recorded_at = now()`.
- [x] Clarify public response naming in node-count terms and document `.spec.replicas` precisely.
- [x] Keep PDB/affinity simulation and auto-apply out of Tier 2a, and reference #709/#710 as investigations.
- [x] Treat notification IDs 88–90 as planned candidates, not registered codes: heterogeneous capacity, reduction recommended, and no reduction recommended. Do not label the no-change result “optimal.”

### Task 3: Align the public feature page and requirements

**Files:**
- Modify: `docs-site/planned-features/machineset-recommendations.md`
- Modify: `docs-site/features/node-recommendations.md`
- Modify: `docs-site/planned-features/autoscaler-optimization.md`
- Modify: `docs-site/planned-features/index.md`
- Modify: `docs-site/architecture/requirements.md`

- [x] State the current supported topology scope, require the relevant Machine API resources and successful collection, and distinguish `platform: none`, UPI as an install method, and actual API capability without broadening support by assumption.
- [x] Explain that `.spec.replicas` is the desired number of Machine/node instances, not application-pod replicas; distinguish status counts.
- [x] Correct REQ-8c.4/8c.5 so the desired count is sourced from the MachineSet API snapshot, not an unverified standard Prometheus series; retain only verified status-series claims.
- [x] State that absent Machine API means no MachineSet target, read/coverage errors withhold the target, and node-level recommendations remain available.
- [x] Expose per-cluster collection state in list metadata so empty results distinguish no recommendation from unsupported/incomplete collection.
- [x] Describe BMO data as optional physical-host inventory/health context that does not replace MachineSet membership or desired count.
- [x] Keep Tier 2a advisory; move PDB/placement simulation out of its acceptance scope and link #709/#710 where appropriate.
- [x] Remove contradictory public claims that Tier 2 is safe-to-auto-execute or that PDB/placement simulation is already part of Tier 2a; leave historical archive pages unchanged.
- [x] Preserve the existing public/internal documentation distinction and update any encouraged Last verified stamp on edited feature pages.

### Task 4: Align issue #47 without replacing its body

**Files:**
- GitHub issue: `pgarciaq/ros-ocp-backend#47`

- [x] Post a comment summarizing the agreed Tier 2a scope, Machine API snapshot/RBAC prerequisite, fail-closed unavailable-data behavior, BMO non-fallback, notification candidates, and links to #709/#710.
- [x] Verify the comment is present and the issue body remains unchanged.

### Task 5: Verify documentation consistency

**Files:** all documentation files changed in Tasks 1–3

- [x] Run `make docs-lint docs-drift docs-sync-check` from the backend repository.
- [x] Re-read the changed ADR/spec/public sections and check every Review Focus item above against the final wording.
- [x] Run `gh issue view 47 --repo pgarciaq/ros-ocp-backend --json body,comments,url` and confirm the new comment exists without a body replacement.
- [x] Report any remaining operator implementation dependency; do not claim that KMO RBAC or MachineSet collection has been implemented by these documentation changes.
