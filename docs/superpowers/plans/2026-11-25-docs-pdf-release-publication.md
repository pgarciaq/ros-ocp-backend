# Documentation PDF Books: Release Publication Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Publish the existing per-section PDF books as public assets on a rolling `docs-pdf` GitHub Release, without committing binaries and without coupling the PDF build to the HTML Pages deploy.

**Architecture:** Phase 0 makes the books publishable. A print-CSS fallback stops the accidental ~19.5 MB-per-weight Noto Sans CJK embedding (466 MB → ~25–40 MB), and a script-level size/font guard makes its return loud. The hardcoded per-section navs are replaced by a generator that reads `mkdocs.yml`, so books cannot drift from the site nav, plus a `verify` gate for pages that move (mkdocs only warns). Phase 1 adds a separate matrix workflow: one book per job, the existing truncation guard per book, then a publish job that replaces the `docs-pdf` release and uploads all assets. A Downloads page links them. HTML/Pages stays canonical and independent.

**Tech Stack:** Bash, Python 3.12, MkDocs Material 9.5.49, mkdocs-macros 1.5.0, mkdocs-to-pdf 0.11.2 / WeasyPrint 62.3, Mermaid CLI 11.12.0 (Chrome for Testing), GitHub Actions, `gh release`.

**Spec:** GitHub [#379](https://github.com/pgarciaq/ros-ocp-backend/issues/379) (locked decisions: one PDF per nav section, Mermaid pre-render, local-only v1) and [#383](https://github.com/pgarciaq/ros-ocp-backend/issues/383) (this delivery: CI artifacts/downloads). Measurements in §Evidence; they are the spec for the size and nav requirements.

## Evidence (measured 2026-11-25, this checkout)

| Fact | Value | How it was obtained |
|---|---|---|
| Local pipeline works on `HEAD` | 47 s for one book | `SKIP_GENERATE=1 ./scripts/docs-pdf.sh ui-integration` |
| Current output | 466 MB / 11 books, 19–60 MB each | `du -sh dist/pdf` |
| Cause of size | `mkdocs-to-pdf 0.11.2 styles/_fonts.scss` names `'Noto Sans CJK JP'` after Roboto; one em dash/arrow forces the full face | two `FontFile2` streams of 19,549,439 bytes in `ui-integration.pdf` (39 MB of its 39.5 MB) |
| Measured fix | `ui-integration` 38 MB → **0.4 MB**; `features` 44 MB → **6.1 MB** | `weasyprint -s /tmp/opencode/nocjk.css …` on the generated debug HTML |
| docs-site CJK text | none | `grep -rP '[\x{3040}-\x{30ff}\x{4e00}-\x{9fff}]' docs-site` |
| Nav drift | 12 pages in `mkdocs.yml` absent from every book; 1 dead nav path | built `features.pdf` lacks “Hosted Control Plane”; `planned-features/replica-count-optimization.md` moved to `features/` in `ffc31ea9` (2026-11-03) |
| Fork default branch | frozen at `2ab1e1fa` (2026-05-18), no `docs.yml` | `git log -1 pgarciaq/main`; `git show pgarciaq/main:.github/workflows/docs.yml` fails |
| Pages config | `build_type: workflow` (no `gh-pages` branch) | `gh api repos/pgarciaq/ros-ocp-backend/pages` |

## Global Constraints

- No PDF binaries in git. `dist/pdf/` and `.docs-pdf-work/` stay ignored (#379).
- One book per `mkdocs.yml` top-level **section** (entries with children). `Home` is not a book. Book slugs are a pinned contract: `getting-started, features, planned-features, architecture, testing, plugin-reference, api, operations, security, ui-integration, historical`.
- Book navs are **flat** lists of pages. mkdocs-to-pdf iterates only top-level nav items (`Navigation.__iter__` yields `self.items`), so a nested section in a book nav silently renders no content.
- A book copies the **whole `docs-site/` tree** so relative images and cross-section links resolve, but Mermaid is pre-rendered and PDF content is built **only from that book's nav pages** (mkdocs-to-pdf renders nav pages only; `render_all_mermaid` is filtered to them).
- Mermaid is pre-rendered to **PNG** by `mmdc`; never SVG (WeasyPrint cannot paint `foreignObject`).
- Print CSS rules from #381/#801 stay: tall content must remain breakable, and `verify_pdf_complete` stays the truncation gate.
- Toolchain pins (CI and local installs): `mkdocs==1.6.1`, `mkdocs-material==9.5.49`, `mkdocs-macros-plugin==1.5.0`, `mkdocs-to-pdf==0.11.2`, `weasyprint==62.3`, `pillow==12.3.0`, `@mermaid-js/mermaid-cli@11.12.0`, Node 22, Python 3.12.
- `scripts/docs_pdf_nav.py` needs PyYAML: locally it arrives with mkdocs; the three CI jobs that run it install `pyyaml==6.0.2` explicitly.
- The PDF workflow is separate from `docs.yml`: a PDF failure must never block the HTML Pages deploy.
- Consumers download without a GitHub login (release assets of a public repo).
- `docs-site/contributing.md` is derived from `CONTRIBUTING.md`; edit the root file, then `make docs-generate`, and commit the regenerated page.
- Commits and pushes require explicit approval (AGENTS.md). The commit steps below run only after that go-ahead.

## Review Focus

1. **A nav page that moves or is deleted.** mkdocs 1.6 warns and drops the nav entry; the book silently loses a chapter (already true for 12 pages). Guard: `docs_pdf_nav.py verify`, wired into `docs-lint.yml`; test in Task 2.
2. **A new top-level nav section with no slug.** The book would appear under a guessed filename while links/Downloads still point elsewhere. Guard: `verify` fails on unmapped multi-page sections; test in Task 2.
3. **The CJK font returning** (plugin upgrade, a new glyph). Guard: per-book size ≤ 15 MB and no `CJK` face in `pdffonts`; mutation check in Task 1.
4. **A nested nav group in a book.** Renders an empty book (top-level iteration above). Guard: flattening test in Task 2.
5. **Partial publish.** A failed matrix leg must not publish a subset; `publish` needs all `build` legs, and the workflow re-asserts the asset count against the generator. Verified in Task 3/5.
6. **Rolling-release URL stability.** Asset URLs are tag-based; recreating the tag must keep `…/releases/download/docs-pdf/<slug>.pdf` stable. Checked by `curl` in Task 5.

## File Structure

- `scripts/docs-pdf/styles.scss` — append the print font fallback (Task 1).
- `scripts/docs-pdf/requirements.txt` — new; pinned toolchain (Task 1).
- `Makefile` — `docs-pdf-install` installs the pins; new `docs-pdf-nav-check` target (Tasks 1–2).
- `scripts/docs-pdf.sh` — size/font guard, generated sections/nav, full-tree copy, Chrome-optional puppeteer config (Tasks 1–3).
- `scripts/docs_pdf_nav.py` — new; derives book navs from `mkdocs.yml` (Task 2).
- `scripts/docs_pdf_nav_test.go` — new; pins slugs, coverage, flattening, and `verify` failures (Task 2).
- `.github/workflows/docs-lint.yml` — add the `verify` step (Task 2).
- `.github/workflows/docs-pdf.yml` — new; matrix build + rolling release (Task 3).
- `docs-site/downloads.md` — new; links to the release assets (Task 4).
- `mkdocs.yml` — nav entry for Downloads (Task 4).
- `CONTRIBUTING.md` + regenerated `docs-site/contributing.md` — publication wording (Task 4).

---

### Task 1: Pinned toolchain, no CJK embedding, per-book guards

**Files:**
- Create: `scripts/docs-pdf/requirements.txt`
- Modify: `scripts/docs-pdf/styles.scss` (append after line 97)
- Modify: `scripts/docs-pdf.sh` (`build_section` at 611–636; add `check_pdf_size` near `verify_pdf_complete` at 526–602)
- Modify: `Makefile:258-264`

**Interfaces:**
- Produces: `check_pdf_size SECTION PDF` — dies with `PDF size guard FAILED for SECTION` when the file exceeds `${DOCS_PDF_MAX_MB:-15}` MB, and with `PDF font guard FAILED for SECTION` when `pdffonts` reports a `CJK` face. Called by `build_section` after `verify_pdf_complete`.
- Produces: `scripts/docs-pdf/requirements.txt`, consumed by `make docs-pdf-install` and the Task 3 workflow.

- [ ] **Step 1: Record the pre-fix failure (mutation baseline)**

Run: `SKIP_GENERATE=1 ./scripts/docs-pdf.sh ui-integration && ls -l dist/pdf/ui-integration.pdf && pdffonts dist/pdf/ui-integration.pdf | grep -c CJK`
Expected today: ~38 MB and `2` (Noto Sans CJK JP regular + bold).

- [ ] **Step 2: Add the print font fallback to `scripts/docs-pdf/styles.scss`**

Append exactly:

```css
/* --- #383: keep the print stack off Noto Sans CJK JP ---
 * mkdocs-to-pdf's _fonts.scss names 'Noto Sans CJK JP' after Roboto. A single
 * em dash / arrow / >= missing from the bundled Roboto makes WeasyPrint embed
 * the full ~19.5 MB CJK face PER WEIGHT (39 MB of a 39.5 MB book). DejaVu Sans
 * covers those glyphs and is subset to a few KB. docs-site carries no CJK text.
 * Keep this override until the pinned plugin stops naming a CJK face.
 */
@media print {
  html, body, h1, h2, h3, h4, h5, h6, label {
    font-family: "Roboto", "DejaVu Sans", sans-serif !important;
  }

  pre, code, var, samp, kbd, tt {
    font-family: "Roboto Mono", "DejaVu Sans Mono", monospace !important;
  }
}
```

- [ ] **Step 3: Rebuild and verify the fix**

Run: `SKIP_GENERATE=1 ./scripts/docs-pdf.sh ui-integration && ls -l dist/pdf/ui-integration.pdf && (pdffonts dist/pdf/ui-integration.pdf | grep CJK || echo "no CJK")`
Expected: ≤ 2 MB and `no CJK`.

- [ ] **Step 4: Add the guard to `scripts/docs-pdf.sh`**

Add above `build_section`:

```bash
# check_pdf_size SECTION PDF — the #383 size/font guard.
#
# The 466 MB book set came from one cause: mkdocs-to-pdf naming 'Noto Sans CJK
# JP' in its print font stack, so WeasyPrint embedded the full ~19.5 MB face
# per weight for a single em dash. Sizes are now sub-10 MB; a book over the cap
# or carrying a CJK face means that class is back (see styles.scss).
check_pdf_size() {
  local section="$1" pdf="$2" bytes limit_mb
  limit_mb="${DOCS_PDF_MAX_MB:-15}"
  require_cmd pdffonts
  bytes="$(wc -c <"$pdf")"
  if (( bytes > limit_mb * 1024 * 1024 )); then
    die "PDF size guard FAILED for $section: $((bytes / 1024 / 1024)) MB > ${limit_mb} MB — the Noto Sans CJK embedding is back (scripts/docs-pdf/styles.scss, #383)"
  fi
  if pdffonts "$pdf" 2>/dev/null | grep -qi 'CJK'; then
    die "PDF font guard FAILED for $section: a CJK face is embedded — scripts/docs-pdf/styles.scss lost its Roboto/DejaVu override (#383)"
  fi
  echo "  size guard OK: $section — $((bytes / 1024 / 1024)) MB, no CJK face"
}
```

Call it in `build_section` immediately after `verify_pdf_complete "$section" "$DIST_DIR/$section.pdf"`.

- [ ] **Step 5: Prove each guard fails for the right reason**

Size branch: `DOCS_PDF_MAX_MB=1 SKIP_GENERATE=1 ./scripts/docs-pdf.sh ui-integration`
Expected: non-zero exit, `PDF size guard FAILED for ui-integration: 0 MB > 1 MB — the Noto Sans CJK embedding is back`.

Font branch (isolate it from the size check): remove the Step 2 block, then
`DOCS_PDF_MAX_MB=100 SKIP_GENERATE=1 ./scripts/docs-pdf.sh ui-integration`
Expected: non-zero exit, `PDF font guard FAILED for ui-integration: a CJK face is embedded`. Restore the block and rebuild once to confirm green.

- [ ] **Step 6: Pin the toolchain**

Create `scripts/docs-pdf/requirements.txt`:

```
mkdocs==1.6.1
mkdocs-material==9.5.49
mkdocs-macros-plugin==1.5.0
mkdocs-to-pdf==0.11.2
weasyprint==62.3
pillow==12.3.0
```

Replace the body of `docs-pdf-install` (`Makefile:258-264`) with:

```make
docs-pdf-install: ## Install pinned PDF book dependencies (WeasyPrint + mkdocs-to-pdf)
	pip install --quiet -r scripts/docs-pdf/requirements.txt
	@command -v mmdc >/dev/null 2>&1 || \
		echo "NOTE: install Mermaid CLI: npm install -g @mermaid-js/mermaid-cli@11.12.0"
	@command -v google-chrome-stable >/dev/null 2>&1 || \
	 command -v google-chrome >/dev/null 2>&1 || \
	 command -v chromium >/dev/null 2>&1 || \
		echo "NOTE: install Chrome/Chromium for mmdc (or set DOCS_PDF_CHROME)"
```

- [ ] **Step 7: Commit**

```bash
git add scripts/docs-pdf/requirements.txt scripts/docs-pdf/styles.scss scripts/docs-pdf.sh Makefile
git commit -m "fix(#383): the PDF books drop the 39 MB CJK face and gain a size guard"
```

---

### Task 2: Derive book navs from `mkdocs.yml`

**Files:**
- Create: `scripts/docs_pdf_nav.py`
- Create: `scripts/docs_pdf_nav_test.go` (`package scripts_test`)
- Modify: `scripts/docs-pdf.sh` — delete `ALL_SECTIONS` (34–46), `copy_site_path` (231–245), `prepare_getting_started`…`prepare_historical` (256–477) and `prepare_section` (479–494); replace with a generated `prepare_section`
- Modify: `Makefile` — add `docs-pdf-nav-check`
- Modify: `.github/workflows/docs-lint.yml` — add the verify step after “Internal docs links (strict)”

**Interfaces:**
- Consumes: `mkdocs.yml` `nav:` and `docs_dir`.
- Produces: CLI contract used by the bash script and CI:
  - `python3 scripts/docs_pdf_nav.py list --json` → `[{"slug": str, "title": str, "pages": int}, …]` in nav order.
  - `… list --slugs` → one slug per line.
  - `… nav --section SLUG` → flat YAML nav lines (2-space indent, no `nav:` key, every leaf a page).
  - `… title --section SLUG` → the mkdocs section label.
  - `… pages --section SLUG` → leaf paths relative to `docs_dir`, one per line.
  - `… verify [--mkdocs PATH] [--site-root DIR]` → exit 0, or exit 1 with one line per problem: `missing page: <rel> (nav: <label>)` / `unmapped nav section: <title>` / `duplicate slug: <slug>`.
- Produces (bash): `prepare_section SLUG` copies the whole `docs-site/` tree into the work docs dir, copies `macros.py`, and writes the generated nav into the per-book `mkdocs.yml`.

- [ ] **Step 1: Write the failing tests**

Create `scripts/docs_pdf_nav_test.go` with `package scripts_test` and these cases (each runs `python3 scripts/docs_pdf_nav.py` via `exec.Command`, with an `exec.LookPath("python3")` guard as in `scripts/gen_benchmark_config_test.go:51`):

1. `TestDocsPdfNavSlugContract` — `list --json` decodes to exactly, in order:
   `getting-started, features, planned-features, architecture, testing, plugin-reference, api, operations, security, ui-integration, historical` with titles matching `mkdocs.yml` (`Getting Started`, `Features`, `Features (planned)`, `Architecture`, `Testing`, `Plugin Reference`, `API Specification`, `Operations`, `Security & Compliance`, `UI Integration`, `Historical`).
2. `TestDocsPdfNavCoversRecentlyAddedPages` — table of (slug, path) pairs that must appear in `pages --section`:
   `features/hosted-control-plane.md`; `planned-features/hosted-control-plane-fleet-optimization.md`; `planned-features/native-recommendation-processing.md`; `planned-features/robne-saas-clowder-cutover.md`; `architecture/plugin-traits.md`; `architecture/librobne.md`; `architecture/ecosystem-comparison.md`; `plugin-reference/slo.md`; `plugin-reference/hcp.md`; `historical/feature-status-archive.md`; `pagination.md` under `api`. This is the regression for the 2026-07-30 hardcoded navs.
3. `TestDocsPdfNavFlattensNestedSections` — `pages --section planned-features` contains `planned-features/local-mode.md` exactly once, and `nav --section planned-features` has no line whose value is empty (no `- Label:` group line).
4. `TestDocsPdfNavVerifyFailsOnMissingPage` — in `t.TempDir()`, write `mkdocs.yml` with `docs_dir: docs` and a block-style nav:
   ```yaml
   nav:
     - Area:
         - Missing: missing.md
   ```
   with an empty `docs/` dir. Expect exit 1 and stderr containing `missing page: missing.md`.
5. `TestDocsPdfNavVerifyFailsOnUnmappedSection` — same layout, but the nav is `- New Area:` with two leaf files `a.md`, `b.md` that exist in `docs/`. Expect exit 1 and stderr containing `unmapped nav section: New Area`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./scripts/ -run 'TestDocsPdfNav' -v`
Expected: FAIL — `scripts/docs_pdf_nav.py` does not exist (exec error), or `no such file`.

- [ ] **Step 3: Implement `scripts/docs_pdf_nav.py`**

Module skeleton (bodies are the implementer's, the signatures are fixed):

```python
#!/usr/bin/env python3
"""Derive per-section PDF book navs from mkdocs.yml (#383).

A book is a top-level nav SECTION (has children). Its nav is FLAT: mkdocs-to-pdf
iterates only top-level nav items, so a nested group would render as an empty
book. Slugs are a pinned contract (release asset names and download URLs).
"""
from __future__ import annotations

import argparse, json, re, sys
from dataclasses import dataclass
from pathlib import Path

import yaml

SLUGS = {
    "Getting Started": "getting-started",
    "Features": "features",
    "Features (planned)": "planned-features",
    "Architecture": "architecture",
    "Testing": "testing",
    "Plugin Reference": "plugin-reference",
    "API Specification": "api",
    "Operations": "operations",
    "Security & Compliance": "security",
    "UI Integration": "ui-integration",
    "Historical": "historical",
}

@dataclass(frozen=True)
class Book:
    slug: str
    title: str
    leaves: tuple[tuple[str, str], ...]  # (label, path relative to docs_dir)

def load_config(path: Path) -> dict: ...      # tolerant SafeLoader for !!python/name tags
def books(config: dict) -> list[Book]: ...    # skip top-level pages (Home); flatten children
def leaf_label(label: str | None, path: str) -> str: ...  # index.md -> "Overview", else stem title-cased
def cmd_list(args) -> int: ...
def cmd_nav(args) -> int: ...
def cmd_title(args) -> int: ...
def cmd_pages(args) -> int: ...
def cmd_verify(args) -> int: ...             # missing page / unmapped section / duplicate slug
def main(argv: list[str]) -> int: ...
if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
```

Implementation notes the signatures do not fix:
- `load_config` must tolerate `format: !!python/name:pymdownx.superfences.fence_code_format` — subclass `yaml.SafeLoader` and `add_multi_constructor("tag:yaml.org,2002:python/name:", lambda loader, suffix, node: None)` plus `add_multi_constructor("!", …)`.
- `books` reads `config["nav"]`; an item is a section when its value is a list. `docs_dir` comes from `config.get("docs_dir", "docs-site")`.
- `cmd_nav` prints `  - {label}: {path}` (two leading spaces) for each leaf, in order.
- `cmd_verify` reports every problem (do not stop at the first), then returns 1; 0 when clean.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./scripts/ -run 'TestDocsPdfNav' -v`
Expected: PASS (all five).

- [ ] **Step 5: Rewire `scripts/docs-pdf.sh`**

- Replace `ALL_SECTIONS=(…)` with:

```bash
NAV_PY="$SCRIPT_DIR/docs_pdf_nav.py"
ALL_SECTIONS=()
while IFS= read -r s; do ALL_SECTIONS+=("$s"); done < <(python3 "$NAV_PY" list --slugs)
```

- Delete `copy_site_path`, all `prepare_*` functions, and the `case` in `prepare_section`. New body:

```bash
prepare_section() {
  local section="$1" title
  python3 "$NAV_PY" list --slugs | grep -qx "$section" || die "unsupported section '$section'. See --help."
  title="$(python3 "$NAV_PY" title --section "$section")"
  # Full tree: relative images (features/*.png, architecture/charts/*) and
  # cross-section links resolve; mkdocs-to-pdf renders only nav pages.
  mkdir -p "$WORK_DIR/docs"
  cp -a "$ROOT_DIR/docs-site/." "$WORK_DIR/docs/"
  write_mkdocs_config "$WORK_DIR/mkdocs.yml" "ROS-OCP $title" "ROS-OCP $title" "$section" \
    "$(python3 "$NAV_PY" nav --section "$section")"
}
```

- Delete `copy_assets` (the full-tree copy supersedes it) and update `usage` to list sections dynamically:

```bash
usage() {
  cat <<EOF
Usage: ./scripts/docs-pdf.sh <section|all>

Sections (→ dist/pdf/<slug>.pdf; derived from mkdocs.yml):
$(python3 "$NAV_PY" list --slugs | sed 's/^/  /')

Home (index.md) is not a separate PDF book.

Options:
  -h, --help   Show this help

Environment:
  DOCS_PDF_CHROME     Path to Chrome/Chromium for mmdc (auto-detected if unset)
  DOCS_PDF_MAX_MB     Per-book size cap for the #383 guard (default 15)
  SKIP_GENERATE       If 1, skip ./scripts/generate-docs.sh
EOF
}
```

- Rewrite `render_all_mermaid` (496–510) to render only the book's own pages — the full-tree copy brings every section's Markdown, and rendering all 46 diagrams for each of 11 books would multiply the cost. New signature `render_all_mermaid SECTION REQUIRE_ANY`:

```bash
render_all_mermaid() {
  local section="$1" require_any="${2:-0}" count=0 rel f
  while IFS= read -r rel; do
    f="$WORK_DIR/docs/$rel"
    if [[ -f "$f" ]] && has_mermaid "$f"; then
      render_mermaid_file "$f"
      count=$((count + 1))
    fi
  done < <(python3 "$NAV_PY" pages --section "$section")
  echo "Pre-rendered Mermaid in $count Markdown file(s)."
  if [[ "$require_any" == "1" && "$count" -eq 0 ]]; then
    die "expected at least one Mermaid diagram in this section"
  fi
}
```

Update both `build_section` call sites to pass the section (`render_all_mermaid "$section" 1` for `features`, else `render_all_mermaid "$section" 0`).

- Replace the body of `is_known_section` (keep `main`’s early validation) with `python3 "$NAV_PY" list --slugs | grep -qx "$1"`, and update the header comment at 18–24 to say navs are generated.

- [ ] **Step 6: Add the gate to Make and CI**

```make
.PHONY: docs-pdf-nav-check
docs-pdf-nav-check: ## Verify every mkdocs.yml nav page exists and every section has a PDF slug (#383)
	python3 scripts/docs_pdf_nav.py verify
```

`.github/workflows/docs-lint.yml`: add `scripts/docs_pdf_nav.py` to both `paths:` lists, and add these steps after “Internal docs links (strict)”:

```yaml
      - uses: actions/setup-python@v5.4.0
        with:
          python-version: '3.12'

      - name: Install PyYAML for the nav gate
        run: python3 -m pip install --quiet pyyaml==6.0.2

      - name: PDF nav coverage (strict)
        run: python3 scripts/docs_pdf_nav.py verify
```

- [ ] **Step 7: Rebuild two books and check the drift is gone**

Run: `SKIP_GENERATE=1 ./scripts/docs-pdf.sh features && SKIP_GENERATE=1 ./scripts/docs-pdf.sh historical`
Expected: both pass the truncation guard and the size guard; then
`pdftotext -layout dist/pdf/features.pdf - | grep -q '^ *[0-9.]* *Hosted Control Plane *$'` succeeds and `pdftotext -layout dist/pdf/historical.pdf - | grep -q 'Feature Status Archive'` succeeds.

- [ ] **Step 8: Commit**

```bash
git add scripts/docs_pdf_nav.py scripts/docs_pdf_nav_test.go scripts/docs-pdf.sh Makefile .github/workflows/docs-lint.yml
git commit -m "fix(#383): derive PDF book navs from mkdocs.yml so books cannot drift"
```

---

### Task 3: Matrix workflow and rolling release

**Files:**
- Create: `.github/workflows/docs-pdf.yml`
- Modify: `scripts/docs-pdf.sh` — `write_puppeteer_config` (155–167) must work without an explicit Chrome path

**Interfaces:**
- Consumes: `scripts/docs_pdf_nav.py list --json` (matrix), `scripts/docs-pdf.sh SECTION` (Task 1/2), `scripts/docs-pdf/requirements.txt` (Task 1).
- Produces: release `docs-pdf` with assets `<slug>.pdf` at `https://github.com/pgarciaq/ros-ocp-backend/releases/download/docs-pdf/<slug>.pdf`.

- [ ] **Step 1: Make the puppeteer config accept the bundled Chrome**

`write_puppeteer_config` currently dies when no Chrome candidate is found. Change it to emit a config without `executablePath` in that case (mermaid-cli's puppeteer then uses the Chrome for Testing it downloaded):

```bash
write_puppeteer_config() {
  local out="$1"
  local chrome="${DOCS_PDF_CHROME:-}"
  if [[ -n "$chrome" && -x "$chrome" ]]; then
    cat >"$out" <<EOF
{
  "executablePath": "$chrome",
  "args": ["--no-sandbox", "--disable-gpu", "--disable-dev-shm-usage"]
}
EOF
  else
    cat >"$out" <<'EOF'
{
  "args": ["--no-sandbox", "--disable-gpu", "--disable-dev-shm-usage"]
}
EOF
  fi
}
```

- [ ] **Step 2: Write `.github/workflows/docs-pdf.yml`**

```yaml
name: Docs PDF books

# Rolling publication for #383. Deliberately separate from docs.yml: a PDF
# failure must never block the HTML Pages deploy. Books are release assets —
# never committed, never in a gh-pages branch, downloadable without a login.
on:
  workflow_dispatch:
  push:
    branches: [main, 'pgarciaq-rosocp-superpowers-*']
    paths:
      - 'docs-site/**'
      - 'mkdocs.yml'
      - 'macros.py'
      - 'CONTRIBUTING.md'
      - 'CHANGELOG.md'
      - 'scripts/docs-pdf.sh'
      - 'scripts/docs-pdf/**'
      - 'scripts/docs_pdf_nav.py'
      - 'scripts/generate-docs.sh'
      - '.github/workflows/docs-pdf.yml'

permissions:
  contents: read

concurrency:
  group: docs-pdf
  # Never cancel mid-publish: the publish job deletes and recreates the release.
  cancel-in-progress: false

jobs:
  plan:
    runs-on: ubuntu-latest
    outputs:
      sections: ${{ steps.sections.outputs.json }}
    steps:
      - uses: actions/checkout@v4.2.2
      - name: Install PyYAML for the nav generator
        run: python3 -m pip install --quiet pyyaml==6.0.2
      - name: Derive the section matrix from mkdocs.yml
        id: sections
        run: |
          set -euo pipefail
          json="$(python3 scripts/docs_pdf_nav.py list --json |
            python3 -c 'import json,sys; print(json.dumps([b["slug"] for b in json.load(sys.stdin)]))')"
          echo "json=${json}" >> "$GITHUB_OUTPUT"

  build:
    needs: plan
    strategy:
      fail-fast: false
      matrix:
        section: ${{ fromJson(needs.plan.outputs.sections) }}
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4.2.2

      - uses: actions/setup-python@v5.4.0
        with:
          python-version: '3.12'
          cache: pip
          cache-dependency-path: scripts/docs-pdf/requirements.txt

      - name: Install WeasyPrint runtime, poppler and DejaVu fonts
        run: |
          sudo apt-get update -qq
          sudo apt-get install -y -qq \
            libpango-1.0-0 libpangoft2-1.0-0 libharfbuzz-subset0 \
            poppler-utils fonts-dejavu-core

      - name: Install the pinned PDF toolchain
        run: pip install -r scripts/docs-pdf/requirements.txt

      - uses: actions/setup-node@v4
        with:
          node-version: '22'

      - name: Cache Mermaid CLI's Chrome for Testing
        uses: actions/cache@v4
        with:
          path: ~/.cache/puppeteer
          key: puppeteer-${{ runner.os }}-mermaid-cli-11.12.0

      - name: Install Mermaid CLI (downloads Chrome for Testing)
        run: npm install -g @mermaid-js/mermaid-cli@11.12.0

      - name: Refresh generated docs-site pages
        run: ./scripts/generate-docs.sh

      - name: Build the ${{ matrix.section }} book
        run: SKIP_GENERATE=1 ./scripts/docs-pdf.sh "${{ matrix.section }}"

      - uses: actions/upload-artifact@v4
        with:
          name: pdf-${{ matrix.section }}
          path: dist/pdf/${{ matrix.section }}.pdf
          if-no-files-found: error
          retention-days: 7

  publish:
    needs: build
    runs-on: ubuntu-latest
    permissions:
      contents: write
    steps:
      - uses: actions/checkout@v4.2.2

      - name: Install PyYAML for the nav generator
        run: python3 -m pip install --quiet pyyaml==6.0.2

      - uses: actions/download-artifact@v4
        with:
          pattern: pdf-*
          path: dist/pdf
          merge-multiple: true

      - name: Every section must have produced exactly one book
        run: |
          set -euo pipefail
          expected="$(python3 scripts/docs_pdf_nav.py list --slugs | wc -l)"
          actual="$(ls -1 dist/pdf/*.pdf | wc -l)"
          if [[ "$expected" != "$actual" ]]; then
            echo "docs-pdf: expected ${expected} books, got ${actual}" >&2
            ls -lh dist/pdf >&2
            exit 1
          fi

      - name: Publish the rolling docs-pdf release
        env:
          GH_TOKEN: ${{ github.token }}
        run: |
          set -euo pipefail
          # Recreate so the tag names the commit the books were built from;
          # asset URLs stay stable because they are keyed by the tag name.
          gh release delete docs-pdf --yes --cleanup-tag || true
          notes="$(mktemp)"
          {
            echo "Per-section PDF books generated from \`${GITHUB_SHA}\` on \`${GITHUB_REF_NAME}\`."
            echo
            echo "| Book | Size |"
            echo "|------|------|"
            for f in dist/pdf/*.pdf; do
              printf '| [%s](%s) | %s |\n' \
                "$(basename "${f%.pdf}")" "$(basename "$f")" "$(du -h "$f" | cut -f1)"
            done
            echo
            echo "HTML documentation: https://pgarciaq.github.io/ros-ocp-backend/"
          } > "$notes"
          gh release create docs-pdf \
            --target "$GITHUB_SHA" \
            --title "Documentation PDF books" \
            --notes-file "$notes" \
            --latest=false
          gh release upload docs-pdf dist/pdf/*.pdf --clobber
```

- [ ] **Step 3: Lint the workflow locally**

Run: `actionlint .github/workflows/docs-pdf.yml`
Expected: no findings.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/docs-pdf.yml scripts/docs-pdf.sh
git commit -m "feat(#383): build and publish the PDF books as release assets"
```

---

### Task 4: Downloads page and publication wording

**Files:**
- Create: `docs-site/downloads.md`
- Modify: `docs-site/index.md` — add to “Quick links”: `- [PDF books](downloads.md) — Per-section PDF downloads (release assets)`
- Modify: `mkdocs.yml` — add `- Downloads: downloads.md` at the end of the Getting Started section (after `Testing & Quality`)
- Modify: `CONTRIBUTING.md:1311-1357`
- Regenerate: `docs-site/contributing.md` (via `make docs-generate`)

**Interfaces:**
- Consumes: the release asset URLs from Task 3.

- [ ] **Step 1: Write `docs-site/downloads.md`**

```markdown
# Documentation PDF Books

> **Last verified:** 2026-11-25

Every top-level section of this site is also published as a PDF book, generated
from the same Markdown by `scripts/docs-pdf.sh` (Mermaid pre-rendered, WeasyPrint
print CSS). Downloads are GitHub Release assets: no login required, and no PDF
is committed to the repository.

| Book | PDF |
|------|-----|
| Getting Started | [getting-started.pdf](https://github.com/pgarciaq/ros-ocp-backend/releases/download/docs-pdf/getting-started.pdf) |
| Features | [features.pdf](https://github.com/pgarciaq/ros-ocp-backend/releases/download/docs-pdf/features.pdf) |
| Features (planned) | [planned-features.pdf](https://github.com/pgarciaq/ros-ocp-backend/releases/download/docs-pdf/planned-features.pdf) |
| Architecture | [architecture.pdf](https://github.com/pgarciaq/ros-ocp-backend/releases/download/docs-pdf/architecture.pdf) |
| Testing | [testing.pdf](https://github.com/pgarciaq/ros-ocp-backend/releases/download/docs-pdf/testing.pdf) |
| Plugin Reference | [plugin-reference.pdf](https://github.com/pgarciaq/ros-ocp-backend/releases/download/docs-pdf/plugin-reference.pdf) |
| API Specification | [api.pdf](https://github.com/pgarciaq/ros-ocp-backend/releases/download/docs-pdf/api.pdf) |
| Operations | [operations.pdf](https://github.com/pgarciaq/ros-ocp-backend/releases/download/docs-pdf/operations.pdf) |
| Security & Compliance | [security.pdf](https://github.com/pgarciaq/ros-ocp-backend/releases/download/docs-pdf/security.pdf) |
| UI Integration | [ui-integration.pdf](https://github.com/pgarciaq/ros-ocp-backend/releases/download/docs-pdf/ui-integration.pdf) |
| Historical | [historical.pdf](https://github.com/pgarciaq/ros-ocp-backend/releases/download/docs-pdf/historical.pdf) |

The `docs-pdf` release is rebuilt whenever `docs-site/` or `mkdocs.yml` changes;
its notes carry the commit and the build date. To build the books locally, see
[Contributing](contributing.md#generate-pdf-books).
```

- [ ] **Step 2: Update `CONTRIBUTING.md`**

- In “Generate PDF books (local only)” (1311–1317): drop “and **not** published to GitHub Pages”; state that HTML is canonical and the books are published as assets of the rolling `docs-pdf` release by `.github/workflows/docs-pdf.yml`, with [#383](https://github.com/pgarciaq/ros-ocp-backend/issues/383) as the reference.
- Replace the “Work tree” paragraph’s nav sentence (“Nav mirrors `mkdocs.yml` (hardcoded in the script)”) with: “Navs are generated from `mkdocs.yml` by `scripts/docs_pdf_nav.py`; `make docs-pdf-nav-check` fails when a nav page is missing or a section has no slug.”
- Replace the “File size” row of the known-limitations table with: “Books are now single-digit MB (a 34-page text book measures ~0.4 MB, a diagram-heavy one ~6 MB). A book over 15 MB fails the build: that means the Noto Sans CJK fallback is back (#383).”
- Rename the heading from “Generate PDF books (local only)” to “Generate PDF books” (the anchor becomes `#generate-pdf-books`; `downloads.md` links there).

- [ ] **Step 3: Regenerate the derived page and run the docs gates**

Run: `make docs-generate && make docs-lint docs-drift docs-check-generated docs-pdf-nav-check`
Expected: all exit 0, and `git status` shows `docs-site/contributing.md` modified.

- [ ] **Step 4: Commit**

```bash
git add docs-site/downloads.md docs-site/contributing.md mkdocs.yml CONTRIBUTING.md
git commit -m "docs(#383): link the PDF books from a Downloads page"
```

---

### Task 5: End-to-end verification, report, close

**Files:** none (verification and tracking only).

- [ ] **Step 1: Push the branch and watch the PDF workflow**

Push to `pgarciaq` (after approval), then: `gh run list --workflow=docs-pdf.yml --limit 1` and `gh run watch <id>`.
Expected: `plan`, 11 `build` legs and `publish` green.

- [ ] **Step 2: Verify the published surface**

```bash
gh release view docs-pdf --json tagName,assets --jq '.tagName, (.assets[] | "\(.name) \(.size)")'
for s in getting-started features planned-features architecture testing plugin-reference api operations security ui-integration historical; do
  printf '%-18s ' "$s"; curl -sIL -o /dev/null -w '%{http_code}\n' \
    "https://github.com/pgarciaq/ros-ocp-backend/releases/download/docs-pdf/$s.pdf"
done
```
Expected: 11 assets with byte sizes, every URL `200`, each size ≤ 15 MB (typical 0.4–7 MB).

- [ ] **Step 3: Verify content, not just size**

```bash
curl -sL -o /tmp/opencode/features.pdf \
  https://github.com/pgarciaq/ros-ocp-backend/releases/download/docs-pdf/features.pdf
curl -sL -o /tmp/opencode/historical.pdf \
  https://github.com/pgarciaq/ros-ocp-backend/releases/download/docs-pdf/historical.pdf
pdffonts /tmp/opencode/features.pdf | grep CJK || echo "no CJK"
pdftotext -layout /tmp/opencode/features.pdf - | grep -q 'Hosted Control Plane' && echo "features OK"
pdftotext -layout /tmp/opencode/historical.pdf - | grep -q 'Feature Status Archive' && echo "historical OK"
git status --porcelain | grep -i '\.pdf' || echo "no PDFs tracked"
```
Expected: `no CJK`, both `OK` lines, and `no PDFs tracked`.

- [ ] **Step 4: Confirm the HTML deploy is unaffected**

`gh run list --workflow=docs.yml --limit 1` → green; `curl -sI https://pgarciaq.github.io/ros-ocp-backend/ | head -1` → 200.

- [ ] **Step 5: Close with the completion report**

Post the AGENTS-style table on #383 (verified vs asserted, each row naming the command or observation), close #383, then close #379 (its last child). Record in the report that IQE coverage does not apply (docs tooling only) and that the release is rolling (`docs-pdf` tag moves with each successful publish).

---

## Out of scope

- EPUB, PDFs in git, a `gh-pages` branch, Pages-hosted `/pdf/` (#383 keeps release assets).
- A dedicated container image bundling WeasyPrint + Chromium: apt/npm install adds roughly half a minute per leg; revisit only if that becomes the complaint.
- Pixel-perfect Material layout, landscape pages, wide-table reflow.
- Cross-book links: a link to a section not in the current book stays a dead link inside the PDF (unchanged from v1).
- Scheduled refresh: the workflow is push/manual; a nightly schedule can be added later without changing the pipeline.
