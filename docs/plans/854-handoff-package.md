# #854 — Engineering Hand-Off Package — Implementation Plan

> **For agentic workers:** this is a documentation deliverable. Owner
> instruction 2026-11-14: comprehensive and exhaustive — ROS engineering
> colleagues should be 100% sure replacing legacy ros-ocp-backend with
> the native engine is the right call; Cost Management (Koku) colleagues
> should have no doubts about the contributions to nise, koku,
> koku-metrics-operator, etc.

**Repo:** ros-ocp-backend, branch `pgarciaq-rosocp-superpowers-phase17`,
post `189e4201`. Tracked as #854.

## Deliverable structure (a directory with an index — the size demands it)

`docs/handoff/`:
1. **README.md** — the entry point: what the fork is, the two audiences,
   the reading order, the headline evidence.
2. **per-area-summary.md** — the fork's delta per subsystem along the
   data path (the 9 owner-agreed areas + 3 cross-cutting subsections),
   ~a page each: what changed since March 2026, why, the gotchas a
   reviewer should probe, the pointers (issues, ADRs, audit docs).
3. **native-engine-case.md** — the ROS-audience document: the
   kruize→native transition, what the native engine is, the correctness
   evidence (test corpus, review history, parity validation, IQE
   validation, benchmarks), what is legacy/dead, what the residue
   adjudication closed, and the honest limits.
4. **cross-repo-contributions.md** — the Koku-audience document: every
   contribution to the sibling repos (nise, koku, koku-metrics-operator,
   iqe plugins, koku-service-operator, cheatsheet) — what changed, why,
   the review/CI status of each.
5. **audit-ledger.md** — which adversarial reviews ran (V6→V16, the
   phase17 branch review, the residue adjudication), what each found,
   how every finding was dispositioned — the scrutiny record.
6. **known-issues-register.md** — the consciously accepted/deferred
   items with disposition status: #834–#839 (incl. owner dispositions),
   #851, #90/#605 (OpenAPI debt), the older labeled backlog.

## Gathering plan (exhaustive, verified — not from memory)

- The ADR corpus (docs/adr/) — index + one-line purpose each.
- The audit records (docs/audit/) — verdict + findings count + how
  dispositioned, each read at summary level.
- Git history since March 2026 — commit counts, per-area touch stats.
- Cross-repo states — git logs in ~/dev/koku/{nise,koku,
  koku-metrics-operator,iqe-*}, the fork branches, CI status.
- The issue tracker — the backlog items with their dispositions.
- Counts that anchor claims: migrations (222), test packages (83),
  the #853 plan evidence, the IQE validation, the scale-benchmark range.

## Accuracy rule

Every claim in the package is either verified against a source (named
in place) or explicitly labeled as an estimate/assertion. A fresh-context
accuracy review (gpt-6.1-terra) checks the package against the sources
before the owner sees the final draft — a document that asks for 100%
confidence must itself be 100% traceable.

## Non-goals

New code changes (gaps found while writing become issues); the #90/#605
OpenAPI work stream; the backlog items themselves.
