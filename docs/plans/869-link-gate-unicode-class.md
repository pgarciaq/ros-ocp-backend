# #869 — Link Gate: github-slugger's Exact Character Class (the Unicode Divergence) — Implementation Plan

> **For agentic workers:** the fix is a faithful PORT of the reference
> algorithm, not an approximation — the close criterion demands exactly that,
> and the verification is the live-GitHub oracle.

**Repo:** ros-ocp-backend, branch `pgarciaq-rosocp-superpowers-phase17`.
Filed from: #805's review (F9 — observed live: the audit page's :290 heading,
the gate computing `…on²--extra-copy…` where GitHub publishes `…on--extra-copy…`).

## The verified mechanism

The gate's `github_slug` filters with Python's `[^\w\- ]` (a KEEP list).
GitHub's slugger (github-slugger, the reference) uses a GENERATED REMOVAL
class — and the two disagree in **both directions** (verified empirically):

- `²` (U+00B2): Python `\w` keeps it (`.isalnum()` includes No); GitHub's
  class strips it (`\u00AB-\u00B4`) — the live observed divergence.
- `🚀`: Python `\w` strips it; GitHub's class KEEPS it (emoji are not in
  the removal class — anchors keep emoji, URL-escaped) — the opposite
  divergence, newly found by this cycle's recon.
- `é`, `中`: both keep ✓ (the must-not-regress direction).

## The fix

Replace the keep-list filter with github-slugger's exact removal class:

1. **The reference** (fetched from Flet/github-slugger master): lowercase →
   `value.replace(regex, '')` → spaces to hyphens. The regex is a
   generated union: one BMP character class plus surrogate-pair
   alternatives for the astral planes (JavaScript UTF-16 semantics).
2. **The port**: the BMP class embeds verbatim in Python (a plain
   character class). The surrogate alternatives translate mechanically to
   code-point ranges: `0x10000 + ((high − 0xD800) << 10) + (low − 0xDC00)`.
   A one-off generator performs the translation and emits the final
   Python class; the translation is verified against github-slugger's
   behavior on test strings (superscript, emoji, accented, CJK, the real
   audit-page heading) BEFORE the class is embedded in the gate script.
3. **The gate pipeline order** (unchanged elsewhere): link-syntax strip →
   code-span stash → HTML strip → lowercase → **remove the github-slugger
   class** → spaces to hyphens.
4. A comment records the class's provenance (the generated module, the
   translation step) and the regeneration procedure.

## Fixtures (check_internal_links_test.go)

- **Superscript**: a heading with `²` — the anchor must drop it (the live
  case; the old gate kept it).
- **Accented + CJK**: `é`/`中` headings — the anchors must keep them (the
  must-not-regress direction; both old and new agree).
- **Emoji**: a heading with `🚀` — the anchor must KEEP it (GitHub keeps
  emoji; the old gate stripped it — the opposite divergence, pinned by
  the fixture). The link's fragment carries the URL-encoded emoji
  (`%F0%9F%9A%80`), matching how authors write such links.

## Verification

- `make docs-links` green with the 1148-link baseline unchanged (nothing
  links to the `²` heading — the anchor-set change is invisible to the
  link checks; verified by running).
- **The live-GitHub oracle**: diff the fixed gate's anchors against the
  rendered audit page's published `user-content-*` ids (the #805 review's
  method — that run was 36/36 except the `²` case; post-fix: a full
  match).
- Mutations: **M1** (revert to the `\w` filter) — the superscript and
  emoji fixtures fail. **M2** (drop the astral ranges from the ported
  class) — the emoji fixture fails (emoji become strippable → the anchor
  diverges).

## Deliverables

- The ported class + the filter change (`scripts/check-internal-links.sh`).
- The three fixtures (`scripts/check_internal_links_test.go`).
- The plan doc; the CHANGELOG entry (with the site-copy regeneration —
  the #800 discipline); the completion report per
  properly-implement-feature.
