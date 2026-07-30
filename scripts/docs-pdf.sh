#!/usr/bin/env bash
# docs-pdf.sh — Generate local per-section PDF books from the MkDocs site.
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
# Print CSS: scripts/docs-pdf/styles.scss (#381).
# Sections: #380 Features pilot, #381 print CSS, #382 remaining nav books.
#
# Locked defaults (#382):
#   - Skip standalone Home (index.md); not a section book
#   - Hardcoded nav mirrors of mkdocs.yml (explicit drift control)
#   - Full OpenAPI + Plugin Reference (long books OK)
#   - Slugs: getting-started, features, planned-features, architecture,
#     testing, plugin-reference, api, operations, security, ui-integration

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
PDF_SUPPORT_DIR="$SCRIPT_DIR/docs-pdf"
WORK_ROOT="$ROOT_DIR/.docs-pdf-work"
DIST_DIR="$ROOT_DIR/dist/pdf"

ALL_SECTIONS=(
  getting-started
  features
  planned-features
  architecture
  testing
  plugin-reference
  api
  operations
  security
  ui-integration
)

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
Usage: ./scripts/docs-pdf.sh <section|all>

Sections (→ dist/pdf/<slug>.pdf):
  getting-started    Getting Started
  features           Features
  planned-features   Features (planned)
  architecture       Architecture
  testing            Testing
  plugin-reference   Plugin Reference
  api                API Specification
  operations         Operations
  security           Security & Compliance
  ui-integration     UI Integration
  all                Build every section above (generate-docs once)

Home (index.md) is not a separate PDF book.

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

# Copy a relative path from docs-site into the work docs tree (file or directory).
copy_site_path() {
  local rel="$1"
  local src="$ROOT_DIR/docs-site/$rel"
  local dst="$WORK_DIR/docs/$rel"
  if [[ -d "$src" ]]; then
    mkdir -p "$dst"
    cp -a "$src/." "$dst/"
  elif [[ -f "$src" ]]; then
    mkdir -p "$(dirname "$dst")"
    cp "$src" "$dst"
  else
    die "missing docs-site path: $rel (run generate-docs.sh)"
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

prepare_getting_started() {
  local docs="$WORK_DIR/docs"
  local f
  for f in whats-new.md changelog.md quickstart.md contributing.md development.md testing.md; do
    copy_site_path "$f"
  done
  copy_assets "$docs"
  write_mkdocs_config "$WORK_DIR/mkdocs.yml" "ROS-OCP Getting Started" "ROS-OCP Getting Started" "getting-started" "$(
    cat <<'NAV'
  - What's New: whats-new.md
  - Changelog: changelog.md
  - Quick Start Tutorial: quickstart.md
  - Contributing: contributing.md
  - Local Development: development.md
  - Testing & Quality: testing.md
NAV
  )"
}

prepare_features() {
  copy_site_path "features"
  copy_assets "$WORK_DIR/docs"
  write_mkdocs_config "$WORK_DIR/mkdocs.yml" "ROS-OCP Features" "ROS-OCP Features" "features" "$(
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
  )"
}

prepare_planned_features() {
  copy_site_path "planned-features"
  copy_assets "$WORK_DIR/docs"
  write_mkdocs_config "$WORK_DIR/mkdocs.yml" "ROS-OCP Features (planned)" "ROS-OCP Features (planned)" "planned-features" "$(
    cat <<'NAV'
  - Overview: planned-features/index.md
  - MachineSet Recommendations: planned-features/machineset-recommendations.md
  - Autoscaler Optimization: planned-features/autoscaler-optimization.md
  - Seasonality & Proactive Recs: planned-features/seasonality.md
  - Java & JVM Optimization: planned-features/java-jvm.md
  - HPA Recommendations: planned-features/hpa-recommendations.md
  - VPA Recommendations: planned-features/vpa-recommendations.md
  - Network Optimization: planned-features/network.md
  - Cross-Cluster VM Placement: planned-features/cross-cluster-vm-placement.md
  - Replica Count Optimization: planned-features/replica-count-optimization.md
  - Local Mode: planned-features/local-mode.md
  - robne CLI: planned-features/robne-cli.md
  - librobne Scalability: planned-features/librobne-scalability.md
NAV
  )"
}

prepare_architecture() {
  copy_site_path "architecture"
  # Architecture nav also links two operations/ pages.
  copy_site_path "operations/adversarial-reviews.md"
  copy_site_path "operations/performance-reviews.md"
  copy_assets "$WORK_DIR/docs"
  write_mkdocs_config "$WORK_DIR/mkdocs.yml" "ROS-OCP Architecture" "ROS-OCP Architecture" "architecture" "$(
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
  )"
}

prepare_testing() {
  copy_site_path "testing"
  copy_site_path "architecture/test-plan.md"
  copy_assets "$WORK_DIR/docs"
  write_mkdocs_config "$WORK_DIR/mkdocs.yml" "ROS-OCP Testing" "ROS-OCP Testing" "testing" "$(
    cat <<'NAV'
  - Validating the Native Engine: testing/validating-native-engine.md
  - Test Data Recipes: testing/test-data-recipes.md
  - IQE Requirement Registration: testing/iqe-requirements-registration.md
  - TDD Test Plan: architecture/test-plan.md
NAV
  )"
}

prepare_plugin_reference() {
  copy_site_path "plugin-reference"
  copy_assets "$WORK_DIR/docs"
  write_mkdocs_config "$WORK_DIR/mkdocs.yml" "ROS-OCP Plugin Reference" "ROS-OCP Plugin Reference" "plugin-reference" "$(
    cat <<'NAV'
  - Overview: plugin-reference/index.md
  - plugin (interfaces): plugin-reference/plugin.md
  - container: plugin-reference/container.md
  - business-hours: plugin-reference/business-hours.md
  - idle-detection: plugin-reference/idle-detection.md
  - gpu: plugin-reference/gpu.md
  - node: plugin-reference/node.md
  - pvc: plugin-reference/pvc.md
  - quota: plugin-reference/quota.md
  - cluster-quota: plugin-reference/cluster-quota.md
  - namespace: plugin-reference/namespace.md
  - snapshot: plugin-reference/snapshot.md
  - vm: plugin-reference/vm.md
  - kruize: plugin-reference/kruize.md
  - example (template): plugin-reference/example.md
  - Query Parameters: plugin-reference/query-parameters.md
NAV
  )"
}

prepare_api() {
  copy_site_path "openapi.md"
  copy_site_path "api-reference"
  copy_assets "$WORK_DIR/docs"
  write_mkdocs_config "$WORK_DIR/mkdocs.yml" "ROS-OCP API Specification" "ROS-OCP API Specification" "api" "$(
    cat <<'NAV'
  - OpenAPI: openapi.md
  - Notification Codes API: api-reference/notification-codes.md
  - Quota Trend: api-reference/quota-trend.md
  - OOM Timeline: api-reference/oom-timeline.md
NAV
  )"
}

prepare_operations() {
  copy_site_path "operations"
  local root_page
  for root_page in monitoring.md configuration.md pagination.md query-performance.md known-issues.md; do
    copy_site_path "$root_page"
  done
  copy_assets "$WORK_DIR/docs"
  write_mkdocs_config "$WORK_DIR/mkdocs.yml" "ROS-OCP Operations" "ROS-OCP Operations" "operations" "$(
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
  )"
}

prepare_security() {
  copy_site_path "security"
  copy_assets "$WORK_DIR/docs"
  write_mkdocs_config "$WORK_DIR/mkdocs.yml" "ROS-OCP Security & Compliance" "ROS-OCP Security & Compliance" "security" "$(
    cat <<'NAV'
  - Overview: security/index.md
  - Compliance Architecture: security/compliance-architecture.md
  - Hardening Guide: security/hardening-guide.md
NAV
  )"
}

prepare_ui_integration() {
  copy_site_path "ui-integration-guide.md"
  copy_assets "$WORK_DIR/docs"
  write_mkdocs_config "$WORK_DIR/mkdocs.yml" "ROS-OCP UI Integration" "ROS-OCP UI Integration" "ui-integration" "$(
    cat <<'NAV'
  - Frontend Integration Guide: ui-integration-guide.md
NAV
  )"
}

prepare_section() {
  case "$1" in
    getting-started) prepare_getting_started ;;
    features) prepare_features ;;
    planned-features) prepare_planned_features ;;
    architecture) prepare_architecture ;;
    testing) prepare_testing ;;
    plugin-reference) prepare_plugin_reference ;;
    api) prepare_api ;;
    operations) prepare_operations ;;
    security) prepare_security ;;
    ui-integration) prepare_ui_integration ;;
    *) die "internal: unknown section $1" ;;
  esac
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
    render_all_mermaid 1
  else
    render_all_mermaid 0
  fi

  build_pdf "$section"
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
