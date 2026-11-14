# Known-Issues Register — accepted, deferred, and scheduled

> What is consciously open, as of 2026-11-14. Nothing here is a
> surprise finding — every item is tracked; most are labeled and
> severity-graded (a few carry no labels — noted inline). The fork's
> position is "no known finding is unowned", not "zero bugs".
> **196 issues were open at snapshot** (`gh issue list --state open`);
> the notable ones are enumerated here by class. This register is a
> map, not the tracker — the issue list is the authoritative state.

## Owner-dispositioned (decisions, not defects)

| Issue | Disposition |
|---|---|
| #835 — hour-grain rotation fleet evades the sleep p95 | **Open defect with an adopted fix direction** (2026-11-11): the current off-leg p95 CAN be evaded by a fleet rotating one active hour per container — high-confidence sleep advice for a workload active outside business hours is possible in that shape. The owner adopted the hour-grain activity signal as the design; **not yet implemented** — scheduled behind the P2 queue |
| #839 — disabled cluster override falls back to the org schedule | **Retained with a documented bound** (2026-11-11): closed as accepted risk |
| #760-lifecycle — already-issued sleep advisories survive schedule changes to expiry | Accepted trade, recorded in ADR-0348's amendment chain |

## Correctness, open (P2/P3 graded)

| Issue | Severity | What |
|---|---|---|
| #822 | unlabeled (P2-suspected) | Stream-path digest flush can replace a complete container-day digest with a fragment |
| #821 | P2 | Nise: same-named VMs in different namespaces overwrite each other (test-data quality) |
| #812 | P3 | Savings-recalc coalescing guard drops overlapping per-cluster callbacks (#769 residual) |
| #804 | P3 | `logKafkaMessageReceived`: unguarded batch-start write — data race across partition goroutines |
| #803 | P3 | Per-message commit protocol: failed-but-uncommitted reports can be acked by later commits |
| #851 | P3 | Timestamp text-cast in page-sort can reorder across DST boundaries (the #843/#845 numeric-sort sibling) |
| #836 | unlabeled (P2-suspected, pending uploader recon) | Partial multi-tarball failure can duplicate report data on retry |
| #815 | P2 | Tags: db mode reads a per-run snapshot — arrays belong to the last summarized cluster |
| #814 | P3 | Tags: `resolved_tags` collapses Koku's multi-value tags to one arbitrary value |
| #834 | unlabeled | Tag-warning scope is cluster-grain; Koku scopes observed tags by project and node too |

## Security and grants, open (questions, not demonstrated leaks)

| Issue | What |
|---|---|
| #797 | Cluster-alias resolution uses an org-unscoped subquery — cross-tenant alias **existence inference** (no row disclosure demonstrated) |
| #789 | Should fleet-summary/savings-summary aggregates respect openshift.node grants? (currently no node filter) |

## Parity, open

#793 — robne cannot configure HCP term windows the server serves (and
reports HCP as having none). Parity is enforced by construction and
test, not proven exhaustive.

## Cutover wiring, open

The SaaS Clowder cutover epic #450–#452, operator readiness/
condition truthfulness #456, and the consumer-migration dependencies
in `docs-site/architecture/native-migration.md`.

## Nise data quality, open

#821 (above), #824 (namespace quota storage accepts only bytes),
#825 (quota fixture defaults), #837 (pod_pvc_map cannot disambiguate
same-named claims on two volumes), #838 (cross-namespace claim name
suppresses VM volume synthesis). These degrade *fixture fidelity*, not
production behavior — but they can mask real parsing paths, so they
are scheduled rather than dismissed.

## Operator (KMO), open

#811 (ErrNoData skips opt-in-independent hooks and cost VM output),
#809 (HCP inventory reads unbounded — a stalled API response can wedge
the hook), #808 (container payload includes namespaces outside the
opt-in filter), #807 (namespace labels as tag source without a CM
sync), #813 (the lint gate cannot fail — config issue, tech-debt).

## Documentation/tooling debt, open

#816 (docs-sync-check reports content diff, not one-sided drift),
#805 (internal link gate: headings with links get a slug GitHub does
not publish), #682 (port remaining chart fixes to koku-service-operator).

## Spec debt (its own work stream)

**#90/#605** — OpenAPI specification debt mapped by the residue
adjudication: 116 subjects (108 with mixed present implementation/
absent specification, 8 specification-focused). The list-surface portion the remediation
cycles touched was completed (#855, 2026-11-14); the remainder is
scheduled scope, not structural risk.

## Postponed/backlog features (not defects)

The HCP fleet roadmap tail (#714 service-mode sleep/wake orchestration,
#691 farm grouping, #661 idle-CP waste, #657/#648 api-tax digests),
the postponed engine items (#708 decoupling recommendations from CSV
ingestion, #672 node role-label refinement, #669 Track I validation),
#709/#710 (PDB/placement investigations), #649 (mutation-testing
adoption proposal).

## What is NOT in this register

Closed findings — see the [audit ledger](audit-ledger.md). The
v16 follow-ups #834–#839 appear above where still open; #836–#838's
siblings #850/#852/#853 are closed with reports.
