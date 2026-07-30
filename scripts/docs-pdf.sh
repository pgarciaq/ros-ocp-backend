#!/usr/bin/env bash
# docs-pdf.sh — Generate local per-section PDF books from the MkDocs site.
#
# Mermaid fences are pre-rendered with mmdc (PNG — WeasyPrint-safe), then
# mkdocs-to-pdf (WeasyPrint) builds the PDF so mkdocs-macros still expand.
#
# Usage:
#   ./scripts/docs-pdf.sh features
#   ./scripts/docs-pdf.sh --help
#
# Output (gitignored):
#   dist/pdf/features.pdf
#
# Prerequisites: see CONTRIBUTING.md ("Generate PDF books").

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
PDF_SUPPORT_DIR="$SCRIPT_DIR/docs-pdf"
WORK_ROOT="$ROOT_DIR/.docs-pdf-work"
DIST_DIR="$ROOT_DIR/dist/pdf"

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
  cat <<'EOF'
Usage: ./scripts/docs-pdf.sh <section>

Sections (pilot):
  features   Features nav section → dist/pdf/features.pdf

Options:
  -h, --help   Show this help

Environment:
  DOCS_PDF_CHROME   Path to Chrome/Chromium for mmdc (auto-detected if unset)
  SKIP_GENERATE     If 1, skip ./scripts/generate-docs.sh
EOF
}

die() {
  echo "error: $*" >&2
  exit 1
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

# Returns 0 if file contains a mermaid fence.
has_mermaid() {
  grep -qE '^```mermaid[[:space:]]*$' "$1" 2>/dev/null
}

# Pre-render Mermaid in a Markdown file in place via mmdc.
render_mermaid_file() {
  local md="$1"
  local dir base tmp puppeteer_cfg
  dir="$(dirname "$md")"
  base="$(basename "$md" .md)"
  # Avoid leading-dot names: mmdc derives artefact image names from the output
  # basename (e.g. foo.__pdf-1.png). Use PNG — Mermaid SVGs often contain
  # foreignObject/HTML that WeasyPrint cannot paint (blank diagram pages).
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

  # Cap very tall Mermaid PNGs so they fit on an A4 content area. Oversized
  # unbreakable images previously truncated the entire remaining PDF.
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
            continue
        nw = max(1, int(w * max_h / h))
        im.resize((nw, max_h), Image.Resampling.LANCZOS).save(png)
        print(f"  resized {png.name}: {w}x{h} → {nw}x{max_h}")
PY
}

write_puppeteer_config() {
  local out="$1"
  local chrome="${DOCS_PDF_CHROME:-}"
  if [[ -z "$chrome" || ! -x "$chrome" ]]; then
    die "Chrome/Chromium not found. Install google-chrome or set DOCS_PDF_CHROME=/path/to/chrome"
  fi
  cat >"$out" <<EOF
{
  "executablePath": "$chrome",
  "args": ["--no-sandbox", "--disable-gpu", "--disable-dev-shm-usage"]
}
EOF
}

write_features_mkdocs() {
  local out="$1"
  # docs_dir is relative to this config file's directory (WORK_DIR).
  cat >"$out" <<EOF
site_name: ROS-OCP Features
site_description: Resource Optimization for OpenShift — Features (PDF book)
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
      # Relative to site_dir; copied to dist/pdf/ after build.
      output_path: pdf/features.pdf
      cover_title: ROS-OCP Features
      cover_subtitle: Resource Optimization for OpenShift
      toc_title: Table of contents
      toc_level: 2
      verbose: true
      html_path: pdf/features.debug.html
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
  - Overview: features/index.md
  - Container Right-Sizing: features/container-recommendations.md
  - Namespace Quota Optimization: features/namespace-recommendations.md
  - Node Consolidation: features/node-recommendations.md
  - GPU MIG Profiling: features/gpu-mig.md
  - GPU Workload Classification: features/gpu-classification.md
  - GPU Time-Slicing: features/gpu-time-slicing.md
  - PVC Right-Sizing: features/pvc-rightsizing.md
  - ResourceQuota Recommendations: features/quota-recommendations.md
  - ClusterResourceQuota Recommendations: features/cluster-resource-quota.md
  - Snapshot Staleness: features/snapshot-staleness.md
  - Business Hours: features/business-hours.md
  - Configurable Thresholds: features/configurable-thresholds.md
  - Dual Engine (Cost vs Performance): features/dual-engine.md
  - Savings Estimations: features/savings-estimations.md
  - History & Quality: features/history-and-quality.md
  - Tag Filtering: features/tag-filtering.md
  - Idle / Zombie Detection: features/idle-detection.md
  - Usage Percentile-Band Plots: features/percentile-band-plots.md
  - Visual Insights: features/visual-insights.md
  - Virtual Machine Recommendations: features/virtual-machines.md
EOF
}

prepare_features_tree() {
  local docs="$WORK_DIR/docs"
  rm -rf "$WORK_DIR"
  mkdir -p "$docs/features" "$docs/assets" "$DIST_DIR"

  # Copy Features pages + shared assets referenced by feature docs.
  cp -a "$ROOT_DIR/docs-site/features/." "$docs/features/"
  if [[ -d "$ROOT_DIR/docs-site/assets" ]]; then
    cp -a "$ROOT_DIR/docs-site/assets/." "$docs/assets/" 2>/dev/null || true
  fi
  # Feature-local images that live beside markdown (e.g. percentile chart PNG).
  # Already included via features/ copy.

  # macros.py must be importable as module_name: macros (cwd = WORK_DIR).
  cp "$ROOT_DIR/macros.py" "$WORK_DIR/macros.py"
  write_puppeteer_config "$WORK_DIR/puppeteer.json"
  write_features_mkdocs "$WORK_DIR/mkdocs.yml"
}

render_all_mermaid() {
  local count=0
  local f
  while IFS= read -r -d '' f; do
    if has_mermaid "$f"; then
      render_mermaid_file "$f"
      count=$((count + 1))
    fi
  done < <(find "$WORK_DIR/docs" -type f -name '*.md' -print0 | sort -z)
  echo "Pre-rendered Mermaid in $count Markdown file(s)."
  [[ "$count" -gt 0 ]] || die "expected at least one Mermaid diagram in Features docs"
}

build_pdf() {
  mkdir -p "$DIST_DIR"
  echo "Building PDF with mkdocs-to-pdf (WeasyPrint)..."
  (
    cd "$WORK_DIR"
    ENABLE_PDF_EXPORT=1 mkdocs build --config-file mkdocs.yml --quiet
  )
  local built="$WORK_DIR/site/pdf/features.pdf"
  [[ -f "$built" ]] || die "PDF not produced at $built"
  cp -f "$built" "$DIST_DIR/features.pdf"
  echo "Wrote $DIST_DIR/features.pdf ($(du -h "$DIST_DIR/features.pdf" | awk '{print $1}'))"
}

build_features() {
  require_cmd mmdc
  require_cmd python3
  check_python_pkgs

  WORK_DIR="$WORK_ROOT/features"

  if [[ "${SKIP_GENERATE:-0}" != "1" ]]; then
    echo "Running generate-docs.sh (set SKIP_GENERATE=1 to skip)..."
    "$ROOT_DIR/scripts/generate-docs.sh"
  fi

  echo "Preparing Features work tree..."
  prepare_features_tree

  echo "Pre-rendering Mermaid diagrams..."
  render_all_mermaid

  build_pdf
}

main() {
  local section="${1:-}"
  case "$section" in
    -h | --help | "")
      usage
      [[ -n "$section" ]] || exit 1
      exit 0
      ;;
    features)
      build_features
      ;;
    *)
      die "unsupported section '$section' (pilot supports: features). See --help."
      ;;
  esac
}

main "$@"
