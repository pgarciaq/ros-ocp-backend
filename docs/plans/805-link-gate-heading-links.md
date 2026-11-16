# #805 — Link Gate: Slug the Rendered Heading Text (Strip Link Syntax) — Implementation Plan

> **For agentic workers:** superpowers:test-driven-development governs every step.
> Owner decisions 2026-11-15: link destinations stripped before slugging, label
> kept (inline links, reference-style, and images per the issue's close
> criterion); **dated audit records under `docs/performance/` stay in the
> gate's scope** (the fix makes the 12 affected headings pass; no new
> per-file-pattern exclusion machinery).

**Repo:** ros-ocp-backend, branch `pgarciaq-rosocp-superpowers-phase17`.
Tracker: #857.

## The verified mechanism

`github_slug` (`scripts/check-internal-links.sh:83`) slugs the raw Markdown
line; GitHub slugs the rendered text. A heading embedding
`[label](url)` keeps the URL's characters in the gate's slug and only the
label in GitHub's — e.g. `docs/performance/ros-ocp-backend-audit-2026-09.md:141`
gate-computes `…node-514httpsgithubcom…` where GitHub publishes
`…node-514-cluster-wide…`. Latent today (nothing links to those anchors),
but the guard fails the wrong way when one appears: a correct link is
rejected, and the message steers the author to a slug that 404s.

Baseline (before the fix): `make docs-links` → "scanned 1148 relative links
in 106 files … OK" — green, unchanged by the fix.

## Mechanism (the fix)

In `github_slug`, strip Markdown link syntax FIRST, keeping the label:

- inline links `[label](url)` → `label`
- reference-style links `[label][ref]` → `label`
- images `![alt](url)` → `alt` (per the issue's close criterion; zero
  occurrences in the corpus — behavior follows the issue's instruction, not
  a live oracle)

The label then flows through the existing lowercase → HTML-strip →
character-filter → hyphenation pipeline. Only `headings()` calls the
slugger (link targets are parsed, not slugged), so the change is isolated.

**Scope:** the exclusion list is unchanged (owner decision — dated audit
records stay in scope; the fix makes their headings pass).

## Tests — `scripts/check_internal_links_test.go`

Following the `lint_migrations_test.go` convention (package `scripts_test`,
execs the script). The script resolves its root from cwd
(`root = Path(".").resolve()`), so each test builds a fixture tree in
`t.TempDir()` and runs the script with `cmd.Dir` set to it — zero script
changes for testability.

1. **The RED:** a fixture page with a heading containing an inline link,
   plus a correct link (from another page) to GitHub's published anchor
   (the label-only slug). **Pre-fix:** the gate computes the
   URL-contaminated slug, rejects the correct link, exits 1 with the
   anchor-mismatch message — the test (asserting exit 0) fails, naming the
   anchor mismatch.
2. **Duplicates:** four repeated headings → `x`, `x-1`, `x-2`, `x-3` (the
   live-oracle-verified suffix rule).
3. **Cross-group collision:** a literal heading whose text equals another
   heading's generated `-N` slug (e.g. a heading literally named
   `Details-1` alongside two `Details` headings) — the gate must not
   confuse them.
4. **Reference-style links in headings:** `[label][ref]` → label-only slug;
   the ref definition line is not a heading.
5. **Images in headings:** `![alt](url)` → alt-only slug (per the issue's
   instruction).
6. **A negative control:** a genuinely-broken anchor still fails (the gate
   keeps its teeth).

## Mutations run

- **M1** (revert the link stripping): the heading-with-link,
  reference-style, and image fixtures all fail.
- **M2** (strip the whole link including the label): the
  heading-with-link fixture fails — the slug lost the label.
- **M3** (revert the anchor-keyed dedupe to the original per-text
  counter): the cross-group collision fixture fails while the plain
  duplicate fixture still passes — the tests discriminate.

Two honest notes from the mutation runs: the first M3 attempt was
botched (it accidentally re-implemented the fix and passed — caught and
redone faithfully against the actual file text); and `go test` caching
initially masked a rerun — the script is read at runtime so the cache
cannot see mutations; mutation runs must use `-count=1`.

## Development findings (found live, recorded)

- **The harness discovery:** the script's bash wrapper `cd`s to its own
  repo root (resolved from the script's location), so `cmd.Dir` cannot
  point it at a fixture tree — the first harness silently scanned the
  REPO (everything passed vacuously, including the broken-anchor
  control!). The working harness copies the script into the fixture
  tree, making ROOT_DIR the tree. The vacuous-pass control is what
  exposed it.
- **A second latent defect, same class, found by the fixtures:** the
  duplicate-anchor rule keyed its counter by each heading's base slug
  (per text), while GitHub's slugger dedupes against every anchor
  already emitted — a literal heading whose text equals another
  heading's generated `-N` anchor collided instead of getting its own
  suffix (the gate would reject a correct `#details-1-1` link). Fixed
  with the used-anchor-set rule; zero occurrences in the corpus (the
  issue noted the case as unverified). The review later PROVED the
  fixed rule equivalent to github-slugger (a 20k-trial differential
  against a verbatim port).

## Fresh-context review — clean verdict; the actionable P3s handled

The reviewer verified against a **live GitHub oracle** (fetched the
rendered audit page; 36/36 published anchors match the fixed gate) and
proved the dedupe rewrite equivalent to github-slugger by porting
BananaSlug and running a 20,000-trial differential plus targeted
order-swaps (literal -1 before/after duplicates, three-way, empty-slug).
No P1/P2. Nine P3s:

- **F2 (a regression this fix introduced):** link stripping applied
  inside code spans — `` ## Run `[foo](bar)` now `` slugged
  `run-foo-now` where GitHub renders the literal text
  (`run-foobar-now`). **Fixed:** code spans are stashed and restored
  around the strip; pinned by `CodeSpanInHeadingIsLiteral`. Zero
  occurrences in the corpus either way.
- **F5 (the unpinned direction — the actual #805 failure mode):** no
  fixture asserted the URL-contaminated anchor is REJECTED (a fail-open
  slugger emitting both forms would pass the suite vacuously).
  **Fixed:** `RejectsURLContaminatedAnchor`.
- **F6/F7/F8 (test hygiene):** the contradictory harness comment fixed;
  the stray chmod of the repo's script removed (the test runs the temp
  copy); `DOCS_LINKS_SOFT` cleared in the test env so soft mode cannot
  make fixtures vacuous (the negative control already caught it — now
  every test is meaningful alone).
- **F1/F3/F4 (documented corners, no action):** CommonMark title forms
  beyond plain double quotes, nested balanced brackets in labels, and
  pathological ref-then-inline adjacency — all zero-occurrence, all
  fail loudly when hit; the untouched INLINE_LINK_RE shares the title
  limitation.
- **F9 (pre-existing, filed as #869):** the `[^\w\- ]` filter keeps
  unicode word characters GitHub strips (observed live: a `²` in the
  audit page's :290 heading) — same failure class, latent, predates
  this change.

## Deliverables

- The `github_slug` link-stripping + the anchor-keyed dedupe
  (`scripts/check-internal-links.sh`).
- `scripts/check_internal_links_test.go` (the six fixtures).
- This plan doc; the CHANGELOG entry (the #778 precedent); completion
  report per properly-implement-feature.
- Baseline: `make docs-links` green before and after — "scanned 1148
  relative links in 106 files … OK", unchanged.
