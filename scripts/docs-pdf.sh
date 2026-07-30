#!/usr/bin/env bash
# docs-pdf.sh — Generate local per-section PDF books from the MkDocs site.
#
# Mermaid fences are pre-rendered with mmdc (PNG — WeasyPrint-safe), then
# mkdocs-to-pdf (WeasyPrint) builds the PDF so mkdocs-macros still expand.
#
# Usage:
#   ./scripts/docs-pdf.sh features
#   ./scripts/docs-pdf.sh architecture
#   ./scripts/docs-pdf.sh operations
#   ./scripts/docs-pdf.sh --help
#
# Output (gitignored):
#   dist/pdf/<section>.pdf
#
# Prerequisites: see CONTRIBUTING.md ("Generate PDF books").
# Print CSS: scripts/docs-pdf/styles.scss (#381).

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

Sections:
  features       Features nav → dist/pdf/features.pdf
  architecture   Architecture nav → dist/pdf/architecture.pdf
  operations     Operations nav → dist/pdf/operations.pdf

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
  # unbreakable images previously truncated the entire remaining PDF (#380/#381).
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
            # Light compress only (no quality loss path for already-small diagrams).
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

# Shared mkdocs.yml head; SECTION-specific nav + titles appended by callers.
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

copy_assets() {
  local docs="$1"
  mkdir -p "$docs/assets"
  if [[ -d "$ROOT_DIR/docs-site/assets" ]]; then
    cp -a "$ROOT_DIR/docs-site/assets/." "$docs/assets/" 2>/dev/null || true
  fi
}

init_work_dir() {
  local section="$1"
  WORK_DIR="$WORK_ROOT/$section"
  rm -rf "$WORK_DIR"
  mkdir -p "$WORK_DIR/docs" "$DIST_DIR"
  cp "$ROOT_DIR/macros.py" "$WORK_DIR/macros.py"
  write_puppeteer_config "$WORK_DIR/puppeteer.json"
}

prepare_features() {
  local docs="$WORK_DIR/docs"
  mkdir -p "$docs/features"
  cp -a "$ROOT_DIR/docs-site/features/." "$docs/features/"
  copy_assets "$docs"

  local nav
  nav=$(
    cat <<'NAV'
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
NAV
  )
  write_mkdocs_config "$WORK_DIR/mkdocs.yml" "ROS-OCP Features" "ROS-OCP Features" "features" "$nav"
}

prepare_architecture() {
  local docs="$WORK_DIR/docs"
  mkdir -p "$docs/architecture" "$docs/operations"
  [[ -d "$ROOT_DIR/docs-site/architecture" ]] || die "docs-site/architecture missing — run generate-docs.sh"
  cp -a "$ROOT_DIR/docs-site/architecture/." "$docs/architecture/"
  # Architecture nav also links two operations/ pages.
  for f in adversarial-reviews.md performance-reviews.md; do
    if [[ -f "$ROOT_DIR/docs-site/operations/$f" ]]; then
      cp "$ROOT_DIR/docs-site/operations/$f" "$docs/operations/$f"
    fi
  done
  copy_assets "$docs"

  local nav
  nav=$(
    cat <<'NAV'
  - Architecture Decision Records: architecture/adrs.md
  - Why the Native Engine: architecture/motivation.md
  - Adversarial Reviews: operations/adversarial-reviews.md
  - Performance Reviews: operations/performance-reviews.md
  - Plugin Architecture: architecture/plugin-architecture.md
  - Plugin Execution Phases: architecture/plugin-phases.md
  - Recommendation Engines: architecture/recommendation-engines.md
  - Understanding Your Recommendations: architecture/understanding-recommendations.md
  - Decay Weights: architecture/decay-weights.md
  - Configurability Reference: architecture/configurability.md
  - Recommendation Math: architecture/recommendation-math.md
  - Deterministic Recommendation IDs: architecture/recommendation-ids.md
  - Database Conventions: architecture/database-conventions.md
  - GPU Classification: architecture/gpu-classification.md
  - GPU Catalogs: architecture/gpu-catalogs.md
  - Kafka Schema: architecture/kafka-schema.md
  - Cost Integration: architecture/cost-integration.md
  - API Versioning: architecture/api-versioning.md
  - Notification Codes: architecture/notification-codes.md
  - Native Migration: architecture/native-migration.md
  - HPA/VPA Deployment Modes: architecture/hpa-vpa-deployment-modes.md
  - T-Digest Feasibility Analysis: architecture/koku-tdigest-idea.md
  - Performance Analysis (historical): architecture/performance-analysis.md
  - Requirements Document: architecture/requirements.md
NAV
  )
  write_mkdocs_config "$WORK_DIR/mkdocs.yml" "ROS-OCP Architecture" "ROS-OCP Architecture" "architecture" "$nav"
}

prepare_operations() {
  local docs="$WORK_DIR/docs"
  mkdir -p "$docs/operations"
  [[ -d "$ROOT_DIR/docs-site/operations" ]] || die "docs-site/operations missing — run generate-docs.sh"
  cp -a "$ROOT_DIR/docs-site/operations/." "$docs/operations/"
  # Operations nav also includes top-level docs-site pages.
  local root_page
  for root_page in monitoring.md configuration.md pagination.md query-performance.md known-issues.md; do
    if [[ -f "$ROOT_DIR/docs-site/$root_page" ]]; then
      cp "$ROOT_DIR/docs-site/$root_page" "$docs/$root_page"
    fi
  done
  copy_assets "$docs"

  local nav
  nav=$(
    cat <<'NAV'
  - Monitoring: monitoring.md
  - Performance and Scalability: operations/performance-and-scalability.md
  - UXSNO Benchmark Report: operations/benchmark-report.md
  - Scale Benchmark Report: operations/scale-benchmark-report.md
  - Scale Benchmark Runbook: operations/scale-benchmark-runbook.md
  - Performance Engineering Guide: operations/performance-engineering-guide.md
  - Scale Test Plan (Perf/Scale): operations/scale-test-plan-perfscale.md
  - Deployment Configuration: configuration.md
  - Configuration Reference (Operations): operations/configuration.md
  - API Pagination: pagination.md
  - Query Performance: query-performance.md
  - Upgrade Runbook: operations/upgrade-runbook.md
  - Known Issues: known-issues.md
  - Dual-Write Mode: operations/dual-write-mode.md
NAV
  )
  write_mkdocs_config "$WORK_DIR/mkdocs.yml" "ROS-OCP Operations" "ROS-OCP Operations" "operations" "$nav"
}

render_all_mermaid() {
  local require_any="${1:-0}"
  local count=0
  local f
  while IFS= read -r -d '' f; do
    if has_mermaid "$f"; then
      render_mermaid_file "$f"
      count=$((count + 1))
    fi
  done < <(find "$WORK_DIR/docs" -type f -name '*.md' -print0 | sort -z)
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

maybe_generate_docs() {
  if [[ "${SKIP_GENERATE:-0}" != "1" ]]; then
    echo "Running generate-docs.sh (set SKIP_GENERATE=1 to skip)..."
    "$ROOT_DIR/scripts/generate-docs.sh"
  fi
}

build_section() {
  local section="$1"
  require_cmd mmdc
  require_cmd python3
  check_python_pkgs
  maybe_generate_docs

  init_work_dir "$section"
  echo "Preparing $section work tree..."
  case "$section" in
    features) prepare_features ;;
    architecture) prepare_architecture ;;
    operations) prepare_operations ;;
    *) die "internal: unknown section $section" ;;
  esac

  echo "Pre-rendering Mermaid diagrams..."
  # Features must have diagrams; other sections may not.
  if [[ "$section" == "features" ]]; then
    render_all_mermaid 1
  else
    render_all_mermaid 0
  fi

  build_pdf "$section"
}

main() {
  local section="${1:-}"
  case "$section" in
    -h | --help | "")
      usage
      [[ -n "$section" ]] || exit 1
      exit 0
      ;;
    features | architecture | operations)
      build_section "$section"
      ;;
    *)
      die "unsupported section '$section' (supports: features, architecture, operations). See --help."
      ;;
  esac
}

main "$@"
