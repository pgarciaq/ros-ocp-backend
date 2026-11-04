#!/usr/bin/env bash
# check-generated-docs.sh — Guards the docs-site pages that
# scripts/generate-docs.sh writes (#777).
#
# The generator materializes docs-site/ pages that are ALSO committed. When the
# two disagree, the build silently publishes the generated side and the content
# of the committed side is lost. #777 was that failure on known-issues: the
# internal stub docs/known-issues.md was copied over the canonical page, so the
# published page lost its limitations table and pointed its "full guide" link at
# itself.
#
# Pre-build (default) — run before mkdocs build:
#   * generate-docs.sh must be a no-op for every gated page (byte comparison),
#   * docs-site/known-issues.md must still be the real page (caveats table
#     present, stub marker absent).
#
# Post-build — run after mkdocs build:
#   ./scripts/check-generated-docs.sh --site-dir _site
#   asserts the rendered page still carries the caveats table.
#
# Exemption: docs-site/changelog.md is regenerated over a committed copy that
# has diverged both ways in content (#800). It cannot pass the no-op check until
# that reconciliation lands; add it to GATED_PAGES when #800 closes.
#
# NOTE: the pre-build mode runs the generator, so it also refreshes the
# (ungated) changelog copy — the same side effect as `make docs-generate`.
#
# Requires bash 4+ and GNU coreutils (`sha256sum`), matching CI (ubuntu-24.04)
# and the documented Linux dev environment.
#
# Usage:
#   ./scripts/check-generated-docs.sh
#   ./scripts/check-generated-docs.sh --site-dir _site
#   SKIP_GENERATE=1 ./scripts/check-generated-docs.sh   # generator already ran
#   make docs-check-generated
#   make docs-check-rendered

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT_DIR"

KNOWN_ISSUES="docs-site/known-issues.md"
RENDERED_KNOWN_ISSUES="known-issues/index.html"

# Pages the generator writes that are also committed. docs-site/development.md
# is only written when missing, so it is a no-op while the committed file exists.
GATED_PAGES=(
    "$KNOWN_ISSUES"
    "docs-site/contributing.md"
    "docs-site/development.md"
)

fail=0

err() {
    echo "generated-docs FAIL: $*" >&2
    fail=1
}

ok() {
    echo "generated-docs OK: $*"
}

usage() {
    sed -n '/^# Usage:/,/^#   make docs-check-rendered/p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
}

checksum() {
    sha256sum "$1" | cut -d' ' -f1
}

# The canonical Known Limitations page must be the real page, from any route:
# never generated from the internal stub, and never replaced by a stub commit.
check_canonical_page() {
    if [ ! -f "$KNOWN_ISSUES" ]; then
        err "$KNOWN_ISSUES is missing — it is the canonical page and must be committed"
        return
    fi

    if grep -q '^## Current caveats' "$KNOWN_ISSUES"; then
        ok "$KNOWN_ISSUES carries '## Current caveats'"
    else
        err "$KNOWN_ISSUES has no '## Current caveats' section — the canonical page was replaced"
    fi

    if grep -q 'Why this page is a stub' "$KNOWN_ISSUES"; then
        err "$KNOWN_ISSUES contains the internal stub's text ('Why this page is a stub') — docs/known-issues.md must never be copied over it"
    fi

    local rows
    rows=$(awk '
        /^## Current caveats/ { inside = 1; next }
        inside && /^## /     { inside = 0 }
        inside && /^\| /     { n++ }
        END                  { print n + 0 }
    ' "$KNOWN_ISSUES")
    if [ "$rows" -lt 13 ]; then
        err "$KNOWN_ISSUES has $rows caveat table lines (header included) — expected at least 13"
    else
        ok "$KNOWN_ISSUES has $rows caveat table lines (header included)"
    fi
}

run_prebuild() {
    local page
    for page in "${GATED_PAGES[@]}"; do
        if [ ! -f "$page" ]; then
            err "$page is missing — it is committed and must exist"
        fi
    done

    if [ "${SKIP_GENERATE:-0}" = "1" ]; then
        echo "generated-docs: SKIP_GENERATE=1 — generate step and byte comparison skipped;" \
            "the caller asserts the committed copies are fresh"
        check_canonical_page
        return
    fi

    declare -A before=()
    for page in "${GATED_PAGES[@]}"; do
        [ -f "$page" ] || continue
        before["$page"]=$(checksum "$page")
    done

    ./scripts/generate-docs.sh

    for page in "${GATED_PAGES[@]}"; do
        [ -n "${before[$page]:-}" ] || continue
        if [ "${before[$page]}" != "$(checksum "$page")" ]; then
            if [ "$page" = "$KNOWN_ISSUES" ]; then
                err "generate-docs.sh rewrote $page — that page is hand-maintained; docs/known-issues.md must never be copied over it (#777)"
            else
                err "$page changed during generation — it is derived from a repo-root source; run './scripts/generate-docs.sh' and commit the result"
            fi
        else
            ok "$page unchanged by generation"
        fi
    done

    check_canonical_page
}

run_rendered() {
    local site_dir="${1:-}"
    local page="$site_dir/$RENDERED_KNOWN_ISSUES"

    if [ ! -f "$page" ]; then
        err "missing rendered page $page — run 'mkdocs build --config-file mkdocs.yml --site-dir $site_dir' first"
        return
    fi

    if grep -q 'Current caveats' "$page"; then
        ok "built $page carries the 'Current caveats' section"
    else
        err "built $page lost the 'Current caveats' section — the internal stub replaced the canonical page (#777)"
    fi

    if grep -q 'Why this page is a stub' "$page"; then
        err "built $page contains the internal stub's text ('Why this page is a stub')"
    fi

    local rows
    rows=$(grep -o '<tr' "$page" | wc -l || true)
    rows=${rows:-0}
    if [ "$rows" -lt 20 ]; then
        err "built $page has $rows <tr> rows — expected the limitations table (at least 20)"
    else
        ok "built $page has $rows <tr> rows"
    fi

    if grep -qE 'href="\.\./agents/' "$page"; then
        err "built $page links ../agents/ — an internal-only path reached the site (copied from docs/)"
    fi
}

case "${1:-}" in
    "")
        run_prebuild
        ;;
    --site-dir)
        if [ -z "${2:-}" ]; then
            echo "generated-docs: --site-dir needs a directory" >&2
            exit 2
        fi
        run_rendered "$2"
        ;;
    -h | --help)
        usage
        exit 0
        ;;
    *)
        echo "generated-docs: unknown argument '$1'" >&2
        usage >&2
        exit 2
        ;;
esac

if [ "$fail" -ne 0 ]; then
    echo "generated-docs: one or more checks failed" >&2
    exit 1
fi
echo "generated-docs: all checks passed"
