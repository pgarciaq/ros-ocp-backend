#!/usr/bin/env bash
# docs-pdf.sh — Generate per-section PDF books from the MkDocs site.
# Published as assets of the rolling `docs-pdf` release by
# .github/workflows/docs-pdf.yml; never committed.
#
# Mermaid fences are pre-rendered with mmdc (PNG — WeasyPrint-safe), then
# mkdocs-to-pdf (WeasyPrint) builds the PDF so mkdocs-macros still expand.
#
# Usage:
#   ./scripts/docs-pdf.sh <section>
#   ./scripts/docs-pdf.sh all
#   ./scripts/docs-pdf.sh --help
#
# Output (gitignored): dist/pdf/<section>.pdf
#
# Prerequisites: see CONTRIBUTING.md ("Generate PDF books").
# Print CSS: scripts/docs-pdf/styles.scss (#381, #383).
# Sections: #380 Features pilot, #381 print CSS, #382 remaining nav books.
#
# Book navs are DERIVED from mkdocs.yml by scripts/docs_pdf_nav.py (#383):
# a book is a top-level nav section, its nav is flat (mkdocs-to-pdf iterates
# only top-level items), and the slugs are a pinned contract because they name
# the release assets. `make docs-pdf-nav-check` fails when a nav page is
# missing or a new section has no slug.
#
# Locked defaults (#382):
#   - Skip standalone Home (index.md); not a section book
#   - Full OpenAPI + Plugin Reference (long books OK)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
PDF_SUPPORT_DIR="$SCRIPT_DIR/docs-pdf"
WORK_ROOT="$ROOT_DIR/.docs-pdf-work"
DIST_DIR="$ROOT_DIR/dist/pdf"

NAV_PY="$SCRIPT_DIR/docs_pdf_nav.py"
SLUG_TABLE=""
ALL_SECTIONS=()

# Populated once die() exists (below), so a missing PyYAML or an invalid
# mkdocs.yml reports as a usage error instead of a Python traceback.
load_sections() {
  local out
  if ! out="$(python3 "$NAV_PY" list 2>&1)"; then
    die "scripts/docs_pdf_nav.py failed — it needs PyYAML (make docs-pdf-install) and a valid mkdocs.yml:
$out"
  fi
  SLUG_TABLE="$out"
  ALL_SECTIONS=()
  while IFS= read -r _slug; do ALL_SECTIONS+=("$_slug"); done < <(printf '%s\n' "$out" | awk 'NF {print $1}')
}

# Prefer google-chrome; allow override for Chromium installs.
: "${DOCS_PDF_CHROME:=}"
if [[ -z "$DOCS_PDF_CHROME" ]]; then
  for candidate in /usr/bin/google-chrome-stable /usr/bin/google-chrome /usr/bin/chromium-browser /usr/bin/chromium; do
    if [[ -x "$candidate" ]]; then
      DOCS_PDF_CHROME="$candidate"
      break
    fi
  done
fi

usage() {
  cat <<EOF
Usage: ./scripts/docs-pdf.sh <section|all>

Sections (→ dist/pdf/<slug>.pdf; derived from mkdocs.yml):
$(printf '%s\n' "$SLUG_TABLE" | sed 's/^/  /')
  all                Build every section above (generate-docs once)

Home (index.md) is not a separate PDF book.

Options:
  -h, --help   Show this help

Environment:
  DOCS_PDF_CHROME   Path to Chrome/Chromium for mmdc (auto-detected if unset)
  DOCS_PDF_MAX_MB   Per-book size cap for the #383 guard (default 15)
  SKIP_GENERATE     If 1, skip ./scripts/generate-docs.sh
EOF
}

die() {
  echo "error: $*" >&2
  exit 1
}

# Mermaid's parser is not stable across versions: an unquoted '(' in a node
# label rendered with one 11.x resolution and failed with the next, and the same
# CLI version can resolve different mermaid builds. A green local build is
# therefore not the CI build. Say so, and keep labels quoted (CONTRIBUTING).
PINNED_MERMAID_CLI="11.12.0"

warn_mermaid_version() {
  local v
  v="$(mmdc --version 2>/dev/null | head -1 | tr -d '[:space:]')"
  [[ -n "$v" ]] || return 0
  if [[ "$v" != "$PINNED_MERMAID_CLI" ]]; then
    echo "NOTE: mmdc $v differs from the pinned $PINNED_MERMAID_CLI used in CI." >&2
    echo "      Mermaid's parser changes between versions, so a book that renders" >&2
    echo "      here can fail in the workflow (and vice versa)." >&2
    echo "      npm install -g @mermaid-js/mermaid-cli@$PINNED_MERMAID_CLI" >&2
  fi
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

check_python_pkgs() {
  python3 -c "import mkdocs" >/dev/null 2>&1 || die "mkdocs not installed (pip install mkdocs-material ...)"
  python3 -c "import mkdocs_to_pdf" >/dev/null 2>&1 || die "mkdocs-to-pdf not installed (pip install mkdocs-to-pdf)"
  python3 -c "import material" >/dev/null 2>&1 || die "mkdocs-material not installed"
  python3 -c "import mkdocs_macros" >/dev/null 2>&1 || die "mkdocs-macros-plugin not installed"
}

has_mermaid() {
  grep -qE '^```mermaid[[:space:]]*$' "$1" 2>/dev/null
}

render_mermaid_file() {
  local md="$1"
  local dir base tmp puppeteer_cfg
  dir="$(dirname "$md")"
  base="$(basename "$md" .md)"
  tmp="$dir/${base}.__pdf.md"
  puppeteer_cfg="$WORK_DIR/puppeteer.json"

  echo "  mmdc  $(basename "$md")"
  (
    cd "$dir"
    mmdc \
      -i "$(basename "$md")" \
      -o "$(basename "$tmp")" \
      -e png \
      -w 900 \
      -H 1200 \
      -b white \
      -p "$puppeteer_cfg" \
      -q
  )
  mv "$tmp" "$md"

  python3 - "$dir" <<'PY'
import sys
from pathlib import Path

try:
    from PIL import Image
except ImportError:
    sys.exit(0)

max_h = 1400
d = Path(sys.argv[1])
for png in d.glob("*.__pdf-*.png"):
    with Image.open(png) as im:
        w, h = im.size
        if h <= max_h:
            im.save(png, optimize=True)
            continue
        nw = max(1, int(w * max_h / h))
        resized = im.resize((nw, max_h), Image.Resampling.LANCZOS)
        resized.save(png, optimize=True)
        print(f"  resized {png.name}: {w}x{h} → {nw}x{max_h}")
PY
}

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
    # No system Chrome: mermaid-cli's own puppeteer supplies the browser it
    # downloaded (CI installs it with the package). Omitting executablePath is
    # what lets that path work.
    cat >"$out" <<'EOF'
{
  "args": ["--no-sandbox", "--disable-gpu", "--disable-dev-shm-usage"]
}
EOF
  fi
}

write_mkdocs_config() {
  local out="$1"
  local site_name="$2"
  local cover_title="$3"
  local pdf_name="$4"
  local nav_yaml="$5"

  cat >"$out" <<EOF
site_name: ${site_name}
site_description: Resource Optimization for OpenShift — ${cover_title} (PDF book)
docs_dir: docs
site_dir: site

theme:
  name: material
  palette:
    scheme: default
    primary: red
    accent: red

plugins:
  - search
  - macros:
      module_name: macros
  - to-pdf:
      enabled_if_env: ENABLE_PDF_EXPORT
      output_path: pdf/${pdf_name}.pdf
      cover_title: ${cover_title}
      cover_subtitle: Resource Optimization for OpenShift
      toc_title: Table of contents
      toc_level: 2
      verbose: true
      html_path: pdf/${pdf_name}.debug.html
      custom_template_path: ${PDF_SUPPORT_DIR}

markdown_extensions:
  - admonition
  - attr_list
  - pymdownx.details
  - pymdownx.superfences
  - pymdownx.highlight:
      anchor_linenums: true
  - pymdownx.inlinehilite
  - pymdownx.tabbed:
      alternate_style: true
  - tables
  - toc:
      permalink: false

nav:
${nav_yaml}
EOF
}

init_work_dir() {
  local section="$1"
  WORK_DIR="$WORK_ROOT/$section"
  rm -rf "$WORK_DIR"
  mkdir -p "$WORK_DIR/docs" "$DIST_DIR"
  cp "$ROOT_DIR/macros.py" "$WORK_DIR/macros.py"
  write_puppeteer_config "$WORK_DIR/puppeteer.json"
}

prepare_section() {
  local section="$1" title rel

  is_known_section "$section" || die "unsupported section '$section'. See --help."
  title="$(python3 "$NAV_PY" title --section "$section")"

  # The book's pages only, at their docs-site-relative paths, plus every
  # non-page file (images, chart PNGs, data) so those paths resolve. A full
  # docs-site copy is wrong here: mkdocs-to-pdf runs on_post_page for EVERY page
  # it is given and filters to the nav afterwards, so a full copy made the
  # one-chapter ui-integration book convert 121 articles (223 s vs 17 s).
  # Links to pages outside the book stay dead inside the PDF (#379).
  while IFS= read -r rel; do
    [[ -f "$ROOT_DIR/docs-site/$rel" ]] || die "missing page: docs-site/$rel"
    mkdir -p "$WORK_DIR/docs/$(dirname "$rel")"
    cp "$ROOT_DIR/docs-site/$rel" "$WORK_DIR/docs/$rel"
  done < <(python3 "$NAV_PY" pages --section "$section")

  (cd "$ROOT_DIR/docs-site" && find . -type f ! -name '*.md' -print0) |
    while IFS= read -r -d '' rel; do
      mkdir -p "$WORK_DIR/docs/$(dirname "$rel")"
      cp "$ROOT_DIR/docs-site/$rel" "$WORK_DIR/docs/$rel"
    done

  write_mkdocs_config "$WORK_DIR/mkdocs.yml" "ROS-OCP $title" "ROS-OCP $title" "$section" \
    "$(python3 "$NAV_PY" nav --section "$section")"
}

render_all_mermaid() {
  local section="$1" require_any="${2:-0}" count=0 rel f
  # Only the book's own pages: the work tree holds every section's Markdown,
  # and rendering all of the site's diagrams for each of eleven books would
  # multiply the cost for output the book never contains.
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

build_pdf() {
  local pdf_name="$1"
  mkdir -p "$DIST_DIR"
  echo "Building PDF with mkdocs-to-pdf (WeasyPrint)..."
  (
    cd "$WORK_DIR"
    ENABLE_PDF_EXPORT=1 mkdocs build --config-file mkdocs.yml --quiet
  )
  local built="$WORK_DIR/site/pdf/${pdf_name}.pdf"
  [[ -f "$built" ]] || die "PDF not produced at $built"
  cp -f "$built" "$DIST_DIR/${pdf_name}.pdf"
  echo "Wrote $DIST_DIR/${pdf_name}.pdf ($(du -h "$DIST_DIR/${pdf_name}.pdf" | awk '{print $1}'))"
}

# verify_pdf_complete SECTION PDF — the #801 silent-truncation guard.
#
# WeasyPrint 62.3 can silently drop everything after an unlucky layout
# point (open upstream bug Kozea/WeasyPrint#2798; the styles.scss height
# override is the workaround). This guard makes the class LOUD.
#
# Invariant: the LAST heading of the last nav leaf's source markdown — the
# book's actual final content — must appear in the SECOND HALF of the
# extracted text. The TOC (first pages) lists every heading with page 0
# when truncation strikes, so a whole-text search is vacuous (the hole the
# first guard version had); and the last leaf's H1 may sit in the FIRST
# half for single-page books or nav labels that shorten the H1 (the second
# version's hole, caught by the all-books run) — the LAST heading is the
# position that must be near the end in every shape.
verify_pdf_complete() {
  local section="$1"
  local pdf="$2"
  local last_path last_heading pages half

  last_path="$(grep -E '^[[:space:]]*- ' "$WORK_DIR/mkdocs.yml" | tail -1 | sed 's/^[[:space:]]*-[[:space:]]*//; s/^[^:]*:[[:space:]]*//; s/^[[:space:]]*//')"
  [[ -n "$last_path" ]] || die "truncation guard: no nav leaf found in $WORK_DIR/mkdocs.yml"
  [[ -f "$WORK_DIR/docs/$last_path" ]] || die "truncation guard: missing source $WORK_DIR/docs/$last_path"

  last_heading="$(python3 - "$WORK_DIR/docs/$last_path" <<'PY'
import re, sys
text = open(sys.argv[1], encoding="utf-8", errors="replace").read()
# Headings only OUTSIDE fenced code blocks — a "# line" inside a fence is
# literal text, not a heading (the historical-book lesson).
headings = []
in_fence = False
for line in text.splitlines():
    if line.lstrip().startswith("```") or line.lstrip().startswith("~~~"):
        in_fence = not in_fence
        continue
    if in_fence:
        continue
    m = re.match(r"^#+\s+(.+?)\s*#*\s*$", line)
    if m:
        headings.append(m.group(1).strip())
if not headings:
    sys.exit(1)
h = headings[-1]
# Strip inline markdown so the heading matches its rendered text.
h = re.sub(r"!?\[([^\]]*)\]\([^)]*\)", r"\1", h)   # links -> labels
h = re.sub(r"\[([^\]]*)\]\[[^\]]*\]", r"\1", h)      # reference links
h = h.replace("**", "").replace("__", "").replace("`", "")
print(h)
PY
)" || die "truncation guard: no heading found in $last_path"

  pages="$(pdfinfo "$pdf" 2>/dev/null | awk '/^Pages:/ {print $2}')"
  [[ -n "$pages" ]] || die "truncation guard: could not read page count of $pdf"
  half=$(( (pages + 1) / 2 ))

  # Heading-LINE match, not substring: the rendered heading appears alone on
  # a line, prefixed by its pdf-order number ("12.8.2 Cons"). A plain
  # substring grep on short headings ('Cons') matches incidental prose in
  # the truncated book — the M1 lesson. (NB: the matcher must run via -c —
  # a heredoc would replace the pipe as stdin.)
  if ! pdftotext -f "$half" -l "$pages" -layout "$pdf" - 2>/dev/null | python3 -c '
import re, sys

def drop_numbering(line):
    # mkdocs-to-pdf numbers chapters two ways: "7. Title" (top-level) and
    # "12.8.2 Title" (nested). The older pattern only knew the dotted form, so
    # a book whose last page is a numbered chapter failed the guard even though
    # the page rendered (getting-started + downloads.md, #383).
    return re.sub(r"^[0-9]+(?:\.[0-9]+)*\.?\s+", "", line.strip())

want = drop_numbering(sys.argv[1])
found = False
# Drain ALL of stdin before exiting: an early sys.exit closes the pipe and
# pdftotext dies with SIGPIPE, which set -o pipefail turns into a spurious
# guard failure (the planned-features Heisenbug).
for line in sys.stdin:
    if drop_numbering(line) == want:
        found = True
sys.exit(0 if found else 1)
' "$last_heading"; then
    die "PDF truncation guard FAILED for $section: last heading '$last_heading' (of $last_path) not found as a rendered heading line in the second half of the PDF — the #801 class (silent WeasyPrint truncation; see styles.scss and Kozea/WeasyPrint#2798)"
  fi
  echo "  truncation guard OK: $section — last heading '$last_heading' present (pages $half-$pages)"
}

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

maybe_generate_docs() {
  if [[ "${SKIP_GENERATE:-0}" != "1" ]]; then
    echo "Running generate-docs.sh (set SKIP_GENERATE=1 to skip)..."
    "$ROOT_DIR/scripts/generate-docs.sh"
  fi
}

build_section() {
  local section="$1"
  local skip_gen="${2:-0}"

  require_cmd mmdc
  require_cmd python3
  check_python_pkgs

  if [[ "$skip_gen" != "1" ]]; then
    maybe_generate_docs
  fi

  init_work_dir "$section"
  echo "Preparing $section work tree..."
  prepare_section "$section"

  echo "Pre-rendering Mermaid diagrams..."
  if [[ "$section" == "features" ]]; then
    render_all_mermaid "$section" 1
  else
    render_all_mermaid "$section" 0
  fi

  build_pdf "$section"
  verify_pdf_complete "$section" "$DIST_DIR/$section.pdf"
  check_pdf_size "$section" "$DIST_DIR/$section.pdf"
}

build_all() {
  require_cmd mmdc
  require_cmd python3
  check_python_pkgs
  maybe_generate_docs

  local section
  local failed=()
  for section in "${ALL_SECTIONS[@]}"; do
    echo ""
    echo "========== Building $section =========="
    # Subshell so die/exit in one section does not abort the whole all-run.
    if ! ( build_section "$section" 1 ); then
      failed+=("$section")
      echo "error: section $section failed" >&2
    fi
  done

  echo ""
  echo "========== Summary =========="
  ls -lh "$DIST_DIR"/*.pdf 2>/dev/null || true
  if [[ "${#failed[@]}" -gt 0 ]]; then
    die "failed sections: ${failed[*]}"
  fi
  echo "All ${#ALL_SECTIONS[@]} section PDFs written under $DIST_DIR"
}

is_known_section() {
  local s="$1"
  local x
  for x in "${ALL_SECTIONS[@]}"; do
    [[ "$x" == "$s" ]] && return 0
  done
  return 1
}

main() {
  local section="${1:-}"
  load_sections
  warn_mermaid_version
  case "$section" in
    -h | --help | "")
      usage
      [[ -n "$section" ]] || exit 1
      exit 0
      ;;
    all)
      build_all
      ;;
    *)
      if is_known_section "$section"; then
        build_section "$section"
      else
        die "unsupported section '$section'. See --help."
      fi
      ;;
  esac
}

main "$@"
