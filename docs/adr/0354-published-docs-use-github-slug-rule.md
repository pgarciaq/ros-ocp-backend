# ADR-0354: Published Docs Use GitHub's Heading Slug Rule

## Status

Accepted — implemented in #802 (Option B)

## Phase

Documentation — `docs-site/` rendering and the two link gates

## Context

Two Markdown renderers publish this repository's documents, and each derived
heading ids with a different rule:

- **GitHub** renders `docs/**`, `README.md`, `CONTRIBUTING.md`, `AGENTS.md`, and
  any `docs-site/` page a reader opens in the repo. It drops every character
  outside `[\w\- ]` and turns **each** space into a hyphen, so
  `Idle / zombie detection` is `#idle--zombie-detection`.
- **MkDocs** rendered `docs-site/` with Python-Markdown's default slugifier,
  which strips punctuation and then collapses `[- ]+` into a **single** hyphen,
  so the same heading was `#idle-zombie-detection`.

Consequences of the split:

1. A link written and tested on the GitHub blob page pointed at a section that
   did not exist on the published site (`#802`): 28 distinct dead anchors across
   18 pages, each landing the reader at the top of the target page.
2. Neither gate could see it. `scripts/check-docs-lint.sh` resolved relative
   *targets* and discarded the fragment (`resolve_target()`), and its
   `is_external()` treats every `href` starting with `#` as out of scope, so
   same-page anchors were never looked at either.
3. `scripts/check-internal-links.sh` (#778) *did* validate anchors — with
   GitHub's rule, because `docs/` is GitHub-rendered. Reusing it for `docs-site/`
   would have hidden real breaks and invented false ones.

The authoring trap was therefore structural: the form that works on the renderer
an author tests on is the wrong form on the renderer that publishes.

## Decision

1. **One slug rule.** `mkdocs.yml` pins GitHub's rule for the `toc` extension:

   ```yaml
   - toc:
       permalink: true
       slugify: !!python/object/apply:pymdownx.slugs.slugify
         kwds:
           case: lower
   ```

   `pymdownx.slugs.slugify` strips `[^\w\- ]` and replaces each space with a
   hyphen — the same transformation GitHub applies to rendered heading text.
   A heading now has the same anchor on the published site and in the repo.
2. **Both trees are gated with that rule.** `scripts/check-docs-lint.sh` gained a
   fragment pass: every `#fragment`, same-page or cross-page, must be an id the
   renderer publishes. It uses `pymdownx` directly when installed and falls back
   to a stdlib re-implementation, guarded by a corpus self-test whose expected
   ids were read from built HTML. `scripts/check-internal-links.sh` keeps
   GitHub's rule for `docs/**`; the two gates now agree on the rule and differ
   only in tree.
3. **Duplicate suffixes are replicated, not assumed.** Python-Markdown appends
   `_1`, `_2` to repeated heading ids; the gate does the same so a page with two
   identical headings still validates.
4. **The migration is a one-time id move.** Every in-site link whose fragment
   changed was rewritten in the same change, so no in-repo link is left pointing
   at an id that no longer exists.

## Alternatives Considered

- **Option A — repair the 28 anchors and add the fragment gate, without the
  `slugify:` override.** Cheapest, and no published URL moves. Rejected: it
  leaves the two-renderer split intact, so the authoring trap recurs on every
  new page and only CI stands between an author and a silent break.
- **Teach the gate the MkDocs rule only (status quo oracle).** Rejected: it
  validates links against the site but not against the form authors write after
  checking a heading on GitHub, which is where the dead anchors came from.
- **Normalise with an aliasing plugin that emits both ids.** Rejected: two ids
  per heading keeps both forms alive but adds a plugin dependency and leaves two
  vocabularies for authors to choose from; the ambiguity is the defect.

## Consequences

- **492 of 3,508 headings across 55 of 120 pages changed id once.** Measured by
  computing both slugs for every heading under `docs-site/`. In-repo links were
  migrated mechanically; the in-repo side is closed.
- **External deep links into those anchors break, and this repository cannot
  inventory them** — release notes, support replies, IQE docs, and UI docs live
  elsewhere. Accepted by the owner on 2026-11-07 as the price of a single
  vocabulary; the alternative (Option A) was rejected for that reason, not for
  cost.
- **The generated changelog started resolving.** `docs-site/changelog.md` is
  generated from `CHANGELOG.md` with GitHub blob rewrites, so it already emitted
  GitHub-form anchors; under the old slugifier its own links were dead. The
  last remaining `mkdocs build` anchor warning was that one, and it is gone
  without touching the generator (#800).
- **One heading started publishing.** `docs-site/architecture/configurability.md`
  had `### Idle — admin-only …` immediately after a table row with no blank
  line, so the renderer emitted it as a table cell and no anchor existed. The
  migration surfaced it because the computed id was absent from the built page.
- **Duplicate-heading suffixes still differ between renderers** (`_1` here,
  `-1` on GitHub). A link to the *second* of two identically-titled headings is
  therefore renderer-specific. No such link exists today; the gate would reject
  a GitHub-form one.
- **`mkdocs build` is the authoritative oracle.** The gate is the fast signal;
  the build prints `INFO … does not contain an anchor` for anything the two
  disagree on, and after this change that list is empty.

## Verification

- `mkdocs build --config-file mkdocs.yml` on the final tree: exit 0, **0**
  anchor warnings (baseline on the commit before this change: 29 — 28 owned by
  #802 and the generated changelog link).
- Built-HTML cross-check: for all **120** `docs-site/` pages, every id computed
  by the gate's slug function appears as an `id=` attribute in the rendered page
  (0 mismatches), including duplicate suffixes.
- Gate mutation tests: a same-page fragment and a cross-page fragment rewritten
  to their old MkDocs form each fail the check by name, e.g.
  `docs-site/pagination.md: query-performance.md#pagination-architecture-org_container_keys
  (no heading … in query-performance.md)`; the stdlib fallback also passes the
  corpus self-test with `pymdownx` unimportable.
- `make docs-lint`, `make docs-drift`, `scripts/check-internal-links.sh` all pass.

## Related Decisions

- `scripts/check-internal-links.sh` (#778) applies the same rule to `docs/**`;
  the two gates differ in tree, not in slug vocabulary.
- **#805 is the remaining defect in that gate**: it slugs the raw Markdown line,
  so a heading that contains a link gets the link *destination* inside its id
  instead of the label. The fragment pass added here slugs rendered text, which
  is the form this ADR standardises on.
- `docs/agents/docs-site-sync.md` records which `docs/` pages are public stubs;
  it is unaffected by the slug rule.
