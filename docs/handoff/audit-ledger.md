# Audit Ledger — the scrutiny record

> Every adversarial review of the fork, what it found, and how every
> finding was dispositioned. The record a reviewer needs to decide
> where to spend attention: what has already survived scrutiny, and
> what each round actually changed. All records live in `docs/audit/`.

## The review series

| Record | Date | Reviewer | Verdict → outcome |
|---|---|---|---|
| `adversarial-review-v6-2026-07-04` | 2026-07-04 | due diligence | findings tracked and remediated (collapsible summary in-doc) |
| `adversarial-review-v8-2026-07-07` | 2026-07-07 | due diligence | 17 findings (ARV-1..17): 16 resolved, 1 accepted (ARV-12, in-place slice mutation — all call sites pass fresh literals) |
| `adversarial-review-v9-2026-07-08` | 2026-07-08 | follow-up | v8 findings verified; CHANGELOG auditability gaps → fixed |
| `adversarial-review-v10-2026-07-12` | 2026-07-12 | follow-up | all twelve v9 findings resolved; v8 doc-status finding closed (#286) |
| `adversarial-review-v11-2026-07-20` | 2026-07-20 | follow-up | all v10 findings still resolved — 0 regressions |
| `adversarial-review-v12-2026-07-25` | 2026-07-25 | follow-up | all 9 v11 findings verified — 0 regressions |
| `adversarial-review-v13-2026-08-31` | 2026-08-31 | due diligence | findings tracked as #504–#510, all closed; 3 accepted/docs-when-convenient items stayed in-doc; production auth hardening landed |
| `adversarial-review-v14-2026-09-06` | 2026-09-06 | security-focused | cluster-alias collision analysis (no tenant row leak — display string only); per-area verdicts recorded |
| `adversarial-review-v15-2026-11-01` | 2026-11-01 | ecosystem (phase17) | **verdict BLOCK** — 29 findings (5 High, 20 Medium, 4 Low) incl. authorization gaps → remediated; follow-ups #826–#832 |
| `adversarial-review-v16-2026-11-09` | 2026-11-09 | v15 remediation audit | **verdict CONCERNS** — 7 findings (6 P2, 1 P3) → all closed under tracker **#833**; follow-ups #834–#839 |
| `adversarial-review-phase13-*` (5 records) | 2026-06-14/15 | phase13 due diligence + 4 remediation passes | tag-push integration gap (#23), NetworkPolicy regressions (#24), auth hardening (#27), statement timeouts (#26) — the five-record chain shows the remediation loop working |
| `branch-review-phase17-2026-11-11` | 2026-11-11 | **GPT 6.1 Sol** (integration, zero authorship) | 9 findings (1 P1 tenant isolation, 5 P2, 3 P3) → **all closed** under tracker **#849** (2026-11-14); every close carries a completion report distinguishing verified from asserted claims |
| `branch-residue-adjudication-2026-11-11` | 2026-11-11 | GPT 6.1 Sol | runtime clean — no missing native implementation; OpenAPI debt mapped to #90/#605 (116 subjects: 108 with mixed present implementation/absent specification, 8 specification-focused) |

## The per-issue remediation discipline (what "closed" means here)

Every finding closed under the v16 and phase17 trackers went through
the same cycle, recorded on its issue:

1. **Baseline RED** — a regression test written to fail on the
   unfixed code, for the bug's mechanism (not an incidental property;
   the adversarial-fixtures procedure).
2. **Fix** — minimal, mechanism-verified.
3. **Mutation checks** — each guard/branch removed independently must
   fail its test; vacuous assertions (ones that pass under the
   mutation) were found and fixed twice (#847's collapse assertion,
   #848's duplicate-day expectation — both honestly recorded).
4. **Fresh-context review** — an independent reviewer (gpt-6.1-terra)
   with no authorship; findings verified against code or commands
   before action; review-forced fixes re-verified.
5. **Completion report** — the properly-implement-feature table
   distinguishing verified from asserted claims; CI verified after
   push (every tracker CI promise was confirmed — the last ones on
   2026-11-14).

The honest-review findings that changed the fix (not just confirmed
it) are part of the record — e.g. #845's sub-hour-window P1 (the
first fix would have produced false sleep advice), #846's three
additional silent-delete migrations, #848's duplicate-day negative
reclaim. A reviewer can read each issue to see the scrutiny depth.

## What this record does and does not claim

- **Does:** every *review finding* is either fixed with a
  discriminating regression or explicitly accepted with a recorded
  rationale (the accepted set: ARV-12; v13's three in-doc items; the
  owner-dispositioned #835/#839; the register's open items).
- **Does not:** claim zero bugs. The known-issues register
  enumerates what is known-open, with severities and dispositions.
  The claim is *no known finding is unowned* — every defect class the
  reviews surfaced is either closed, accepted on record, or scheduled.
- **The scrutiny asymmetry, stated honestly:** the serving/engine
  core has the deepest coverage (v15/v16/phase17 all concentrated
  there); the operational tooling (runbooks, CI wiring) has lighter
  review coverage. The known-issues register reflects that
  distribution.
