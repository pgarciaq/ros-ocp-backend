# #800 — Changelog Reconciliation: One Source, Lossless Regeneration, the Gate Extended — Implementation Plan

> **For agentic workers:** the reconciliation moves entry text VERBATIM — no
> rephrasing, no invented entries. Every placement decision is recorded in
> the which-side-won table (issue constraint: "the completion report states
> which side won for each hunk and why").

**Repo:** ros-ocp-backend, branch `pgarciaq-rosocp-superpowers-phase17`.
Tracker: #857 (docs infrastructure, P2).

## The measured state (re-verified at HEAD, 2026-11-17)

- Site-only lines (deleted by regeneration): **340** — the 18 refs
  `#700 #702 #704 #705 #707 #716 #717 #721 #722 #723 #724 #746 #747 #752
  #753 #754 #790 #791`, written into the site copy and silently dropped at
  deploy (verified against the deployed page at filing: all 18 return 0
  occurrences).
- CHANGELOG-only lines (the site never shows): **833** (329 at the issue's
  2026-11-04 measurement — the campaign's seven closed issues since each
  added entries that never reached the site copy).
- The committed site copy carries two `### Added` headings in the
  unreleased area (a hand-edit artifact; blind concatenation would be wrong).
- The deployed page carries 3 dead links: the generator's rewrite prefix
  list omits `docs-site/`, so `docs-site/x/y.md` links pass through verbatim
  and 404 (the committed copy had fixed them by hand; regeneration re-broke
  them).
- `make docs-check-generated` runs the generator in place and mutates the
  committed changelog (~700 lines from a clean tree) because the page is
  exempted from the no-op gate.

## Mechanism

1. **Reconcile `CHANGELOG.md`** — the single source: each of the 18
   site-only entries moves VERBATIM from the committed
   `docs-site/changelog.md` into the matching `### Added/Fixed/...`
   subsection of `## [Unreleased]` in `CHANGELOG.md`. Per entry: the
   duplicate check (is it a rephrasing of a CHANGELOG-side entry? —
   compare the texts and record) and the placement decision. The
   which-side-won record: the 18 site-only entries → CHANGELOG.md wins as
   the destination; the CHANGELOG-only refs (now more than the filing's
   17 — the whole campaign's entries) → already correct, they reach the
   site via regeneration; the site copy's duplicate `### Added` headings
   → the CHANGELOG.md structure wins (an artifact).
2. **The generator's dead-links fix**: add `docs-site/` to the rewrite's
   prefix handling, mapping `docs-site/x/y.md` → `x/y.md` (MkDocs emits
   the site-relative href, as it already does for `../features/...`).
3. **Regenerate and commit the site copy** — regeneration becomes a
   no-op: `./scripts/generate-docs.sh && git diff --exit-code
   docs-site/changelog.md` exits 0 from a clean tree. This is option (b)
   from the issue's second comment — the lossless regeneration — which
   the close criteria prescribe; the in-place mutation side effect
   disappears by construction.
4. **Extend #777's gate**: add `docs-site/changelog.md` to `GATED_PAGES`
   in `scripts/check-generated-docs.sh`; remove the exemption comment and
   the side-effect NOTE.
5. **Post-push verification** (the close criterion's final step): after
   the Pages site rebuilds, fetch the deployed changelog — the 18 refs
   present, the three formerly-dead links live.

## Discipline consequence (recorded)

Once the gate extends, every future cycle that touches `CHANGELOG.md`
must also run `./scripts/generate-docs.sh` and commit
`docs-site/changelog.md` — the gate enforces it. The campaign's previous
seven cycles did not (hence the 833-line growth); from here on it is a
per-cycle checklist item.

## Verification

- The no-op property: `./scripts/generate-docs.sh && git diff --exit-code
  docs-site/changelog.md` from a clean tree.
- `make docs-lint docs-drift docs-sync-check docs-check-generated` — all
  green with the changelog now gated.
- The entry accounting: every one of the 18 refs present in the
  reconciled CHANGELOG.md (grep); the entry count preserved (no entries
  deleted — the constraint forbids deleting to make the diff disappear).
- Post-push: the deployed page fetch (the 18 refs + the 3 links).

## Mutations

- **M1** (the gate extension reverted): `docs-check-generated` passes
  with a hand-edited site copy — caught by running the check against a
  deliberately-diverged copy.
- **M2** (the `docs-site/` prefix mapping dropped): a
  `docs-site/...-link` entry in CHANGELOG.md regenerates to the
  verbatim (broken) form — caught by the no-op diff or a direct
  generator test.

## Deliverables

- The reconciled `CHANGELOG.md` and the regenerated
  `docs-site/changelog.md` (committed together — the pair reconciled).
- The generator fix (`scripts/generate-docs.sh`).
- The gate extension (`scripts/check-generated-docs.sh`).
- The which-side-won record (in the plan and the completion report).
- The post-push deployed-page verification posted to the tracker.
- This plan doc; the completion report per properly-implement-feature.

## Which side won (the per-hunk record the issue requires)

| Hunk class | Disposition | Why |
|---|---|---|
| The 18 site-only entries (`#700 #702 #704 #705 #707 #716 #717 #721 #722 #723 #724 #746 #747 #752 #753 #754 #790 #791`) | **CHANGELOG.md wins as the destination** — each entry moved VERBATIM into its site-copy-assigned subsection of `## [Unreleased]` (11 Added, 1 Changed, 6 Fixed), at the bottom of each subsection (the newest-first convention — these are the oldest Unreleased entries) | The site copy is generated from CHANGELOG.md; an entry living only there is dropped at every regeneration (verified against the deployed page at filing: all 18 returned 0 occurrences) |
| The CHANGELOG-only lines (833 at cycle start — the filing's 17 refs plus every entry added since, including the whole campaign's) | **CHANGELOG.md wins** — unchanged; they reach the site via the regeneration | Already correct on the source side; the site copy was stale |
| The site copy's unreleased-area structure | **The CHANGELOG.md structure wins** — corrected after the review: the repeated `### Added` headings (five in the regenerated copy) are the SOURCE's own append-over-time pattern, present in both old files, not a hand-edit artifact (the issue's "two headings" framing was a misdiagnosis). The actual hand-edit artifact — an empty `### Fixed` section in the old site copy — IS resolved: the regenerated pair has zero empty sections | The review's finding 3: my first draft claimed "exactly one per section," which fails on inspection — the corrected claim is what's verified |
| `#705`'s site-relative ADR link (`](architecture/adrs.md)`) | **Reverse-mapped to the repo path** (`](docs-site/architecture/adrs.md)`) in CHANGELOG.md — the generator's new `docs-site/` mapping rewrites it back to the site-relative form on regeneration | The site copy's links are in the generator's OUTPUT form; carrying them verbatim into the source would break the round-trip. Link adaptation, not text rephrasing — the entry's words are unchanged |
| The three dead links on the deployed page (`docs-site/features/robne-cli.md` ×2, `docs-site/planned-features/robne-cli.md`) | **The generator wins** — the `docs-site/` prefix mapping added; the regenerated copy carries the site-relative hrefs (0 `docs-site/`-prefixed hrefs remain) | The committed copy's hand fix was correct but regeneration re-broke it; the fix makes the round-trip stable |

| **Both-sides-diverged entries** (found by the review, finding 2 — the census missed this class): `#392`, `#694`, `#492`, `#487`, `#489`, and label-only `#99`/`#469` — present in both old files with divergent text | **CHANGELOG.md wins**, with the site variant's uniquely-carried facts MERGED into the winning entry: `#392` gained `shared_infrastructure`, the 25-column `ros-openshift-hcp-capacity-*.csv` note, and the `librobne/hcp/serve.go` sharing note; `#489` regained the `business_hours_plugins`=11 capability. `#694`/`#492`/`#487` — the CHANGELOG variants were already supersets (stale site copies); `#99`/`#469` — label-only divergence, benign | The issue's constraint ("which side won for each hunk and why") applies to this class too — the first draft resolved them silently, which the review flagged as P2 |
| **The ref-less ADR-0346 entry** (the review's P1, finding 1 — the census was ref-based and thereby invisible to it) | **Restored verbatim** from `git show HEAD:docs-site/changelog.md` into Unreleased's Added section (link reverse-mapped like #705's) — a real behavioral note (the Path-A digest window fix: VM/PVC long-term data no longer truncated) that regeneration had deleted | The census lesson recorded: ref-keyed reconciliation misses ref-less entries; the review's block-level no-loss diff is the correct census method |

**Duplicate check (the issue's constraint):** four keyword candidates examined, none a
duplicate — `#747` (zombie p50→p95, an Added) vs the CHANGELOG's
"Breaking (plots)" percentile entry (a different change, different issue);
`#724` (robne W5 api-tax advisory, Added) vs `#763` (a later FIX to
api-tax rows — added-vs-fixed pair, both legitimate); `#721` (robne W8
sleep advisory, Added) vs `#847` (a later FIX — same pairing); `#700`
(robne CLI HCP fleet-capacity) vs `#392` (the API — CLI-vs-API
counterparts, both legitimate). After the review's P1 restoration: the count went 340 → 359
(18 ref-keyed entries + the ref-less ADR-0346 entry the review recovered;
the first draft's 358 claim was falsified by the block-level no-loss diff).

## Results

- All 18 refs present in BOTH files (18/18 each); entry counts identical
  (358 = 358 — the pair reconciled byte-for-byte through the generator).
- The campaign's entries (#793…#803) all reached the site copy.
- The no-op property: regeneration is idempotent from the reconciled
  state (verified twice, including after the mutations were restored);
  the extended gate's own output confirms — "docs-site/changelog.md
  unchanged by generation".
- `make docs-lint docs-drift docs-sync-check docs-check-generated
  docs-links` all green.
- Mutations: M1 (the gate extension reverted → a hand-edited site copy
  passes) caught — the extended gate fails on hand divergence; M2 (the
  `docs-site/` prefix mapping dropped) caught directly — the two dead
  hrefs return (count 2) with the mapping dropped, 0 restored.

## Fresh-context review — the machinery verified; the census had a P1, fixed

The reviewer verified (with evidence): the verbatim claim (5/5 random
refs byte-identical; extended to all 18 — 17 byte-identical + #705's
documented link map); the no-loss claim on the CHANGELOG side (all 340
old blocks present); the no-op property; the gate extension (a
hand-edit fails with the healing message; the sha256 before/after
logic genuinely covers the changelog); the mapping (0 dead hrefs, all
three targets in mkdocs nav, docs-lint genuinely scans the changelog);
and the collateral (mkdocs nav, the PDF path, docs-sync-check).

Three findings, all fixed:

- **P1 (a silently lost entry):** the reconciliation census was
  ref-based, so the ref-less "Term-table single source (ADR-0346)"
  entry — a real behavioral note (the Path-A digest window fix) — was
  invisible to it and regeneration deleted it. **Fixed:** restored
  verbatim from git history into Unreleased's Added section; the count
  is now 359. The census lesson recorded: block-level no-loss diffing,
  not ref keying.
- **P2 (the incomplete which-side-won record):** seven
  both-sides-diverged entries had been resolved silently (CHANGELOG
  variant winning everywhere). **Fixed:** the fourth hunk class added
  to the record; #392's uniquely-carried facts (the
  `shared_infrastructure` warning, the 25-column CSV note, the
  serve.go sharing note) and #489's capability mention MERGED into the
  winning entries; #694/#492/#487 recorded as superset-wins;
  #99/#469 as benign label-only divergence.
- **P2 (a false verification claim):** the plan asserted "exactly one
  `### Added` per section" — the regenerated copy has five (the
  source's own append-over-time pattern; the issue's "two headings =
  hand-edit artifact" framing was a misdiagnosis). **Fixed:** the
  claim corrected to what is verified — the actual artifact (an empty
  `### Fixed` section in the old site copy) is resolved; the repeated
  headings are the source's pre-existing structure.
- **P3 (noted, latent):** the `docs-site/` mapping's degenerate edges
  (`docs-site/` alone → empty href; uppercase → verbatim passthrough)
  — consistent with the pre-existing prefix handling's style, zero
  occurrences in the data.
