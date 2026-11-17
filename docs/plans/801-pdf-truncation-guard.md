# #801 — Docs PDF Truncation: the WeasyPrint height-overflow Bug and Its Workaround — Implementation Plan

> **For agentic workers:** the diagnosis is complete and empirically
> verified; this cycle ships the workaround and a build-time guard so the
> silent-truncation class can never ship again unnoticed.

**Repo:** ros-ocp-backend, branch `pgarciaq-rosocp-superpowers-phase17`.
Tracker: #857 (docs infrastructure, P2 — the last one).

## The verified root cause (a three-part chain)

1. **The Material theme's CSS** sets `html, body { height: 100% }` — a
   browser-screen style, meaningless in paged media, carried into every
   book's HTML.
2. **WeasyPrint 62.3's open bug [Kozea/WeasyPrint#2798](https://github.com/Kozea/WeasyPrint/issues/2798)**
   (filed 2026-06-09, still open): when a height-styled box's *bottom
   decoration* — a border, padding, or an `<hr/>` — overflows the page
   bottom, `block_container_layout` **stops rendering the rest of the
   document silently**. Our unlucky decoration: the `<hr/>` after section
   10.20's paragraph in the upgrade-runbook. The upstream reporter's
   minimal repro is the same shape (`height:100%` + `<hr/>` + content
   that vanishes).
3. The mkdocs-to-pdf plugin's own `article { min-height: 100vh }`
   compounds the height-styling.

**The bisect evidence (why this diagnosis fits everything):** the drop is
content-anchored (every failing variant — full doc, single/pair/multi
article deletions, 43–99 pages — ends at the exact same line, the 10.20
heading); not size- or page-count-dependent (a 43-page doc failed; a
74-page ragged cut that shifted the layout dodged the unlucky point and
rendered the tail); not the assembly (a direct WeasyPrint render of the
complete debug HTML truncates identically); not avoid-break (a blanket
`!important` override of both break properties didn't help — a different
mechanism than #381's lesson).

## The fix (verified by direct render of the real book HTML)

One override in `scripts/docs-pdf/styles.scss` (the print CSS the build
injects after the plugin defaults):

```css
html, body { height: auto !important; min-height: auto !important; }
article, section + section { min-height: auto !important; }
```

**Verified:** 108 pages (was 99), section 12's content on the last page,
the Known Limitations caveats table complete (every row — the "cold
start" grep initially missed only because pdftotext split the phrase
across lines), real TOC page numbers (11 → 101, 12 → 104 — were `0`).
The height:100% is a screen style with no paged-media meaning; removing
it is safe, and short articles no longer force-fill to 100vh.

## The regression guard (the silent class made loud)

`scripts/docs-pdf.sh` gains `verify_pdf_complete`: after each book build,
extract the last nav leaf's title from the work dir's `mkdocs.yml`,
`pdftotext` the PDF (newlines stripped — wrap-safe), and **fail the
build** if the title is absent. The upstream bug is open, so the
workaround is load-bearing; a future WeasyPrint upgrade or CSS change
that reintroduces the truncation must fail loudly, not ship a quiet
98-page book. The guard covers every book (`all` and single-section
builds).

## Verification

- Rebuild `operations`: pages > 99, sections 11–12 render (the caveats
  rows, the dual-write content), the TOC numbers non-zero, the guard
  passes.
- Rebuild **all eleven books** (`docs-pdf.sh all`): each passes the
  truncation guard. The other books were dated 2026-07-30 and never
  rebuilt — their truncation status was unknown (the issue's scope
  note); this verifies them all under the fix.
- Mutations: **M1** (the override removed from the compiled output —
  rebuild without the styles.scss rule) → the truncation returns AND the
  guard fails the build (both halves proven). **M2** (the guard broken —
  e.g. matching a title that isn't the last section) → caught by
  asserting the guard against a deliberately-truncated PDF.

## Deliverables

- The `styles.scss` override (with the upstream-bug comment).
- The `verify_pdf_complete` guard in `docs-pdf.sh`.
- The rebuilt `dist/pdf/` books (gitignored — verified, not committed).
- The plan doc; the CHANGELOG entry (with the site-copy regeneration —
  the #800 discipline is live); the completion report per
  properly-implement-feature.

## Mutations run (recorded live)

- **M1 (the override removed, rebuild):** the truncation returns (99
  pages, ends at 10.20, TOC page 0) — AND the first guard version
  PASSED anyway, exposing its hole: it searched the whole extracted
  text, where the TOC lists every section title (with page 0) even in
  a truncated book. **The guard was fixed** to search only the SECOND
  HALF of the PDF (the TOC is never there; the last section always
  is), and the mutation re-run: the truncation returns AND the guard
  fails the build with the mechanism-naming message. Both halves of
  M1 proven — and the mutation doubled as the guard's own broken-guard
  test (M2's role: a weakened guard against a known-truncated PDF —
  performed live rather than simulated).
- **The fix restored and re-verified through the real build path:**
  108 pages, the guard passes ("last section 'Dual-Write Mode' present
  (pages 54-108)"), the caveats table complete, real TOC numbers.

## The honest lesson

The first guard version shipped green against the very bug it existed
to catch — the TOC lists every title, truncated or not. The mutation
caught it before anything shipped. Recorded here and in the guard's
comment: the second-half rule is the load-bearing detail.

## The guard's own hardening (found live during verification — recorded)

The guard went through three versions, each hole caught by a real run:

1. **v1 (whole-text title search):** the TOC lists every title with page 0
   when truncation strikes — vacuous. Caught by M1.
2. **v2 (second-half nav-label search):** nav labels shorten the H1
   ("Frontend Integration Guide" vs "ROS UI Integration Guide"), and
   single-page books put the H1 in the first half — three false
   failures (architecture, testing, ui-integration) in the all-books
   run.
3. **v3 (last heading of the last leaf's source, as a rendered heading
   LINE in the second half):** correct invariant, but three
   implementation bugs found live:
   - The matcher's heredoc form (`python3 - arg <<PY`) replaced the
     pdftotext pipe as stdin — switched to `-c`.
   - **The SIGPIPE Heisenbug:** the matcher exited early on the match →
     pdftotext died with SIGPIPE → `set -o pipefail` turned it into a
     spurious guard failure, dependent on WHERE the heading sat (books
     with late headings passed; planned-features' early match failed).
     Fixed by draining stdin before exiting.
   - **The fenced-heading extractor bug:** the last "-heading" of
     historical's scale-benchmark-report.md is a `# line` INSIDE a code
     fence — literal text, not a heading. The extractor now skips
     fences; the real last heading is "Appendix: Prometheus Queries".

Also found and fixed: **the dangling nav entry** — planned-features'
nav listed `librobne-scalability.md`, folded into local-mode.md by
3f57d350 but never removed from the PDF nav. The v3 guard's
missing-source check caught it.

## Final verification state

- All **11 books** rebuilt with the fix and passing the v3 guard
  (verify-all: 11/11 OK).
- **M1 (the override removed, rebuild):** operations fails the guard
  with the mechanism-naming message (the truncation returns — 99
  pages); the fix restored → passes (108 pages). Both halves proven.
  ui-integration under M1 still passes — consistent with the upstream
  bug being layout-dependent (that single-page book doesn't hit the
  unlucky decoration point); the operations catch is the proof.
- **M2:** the guard's broken versions WERE the mutation tests — v1
  (vacuous pass on a truncated book), the SIGPIPE spurious-fail, and
  the fence extractor all caught live against real builds.
