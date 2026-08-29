#!/usr/bin/env bash
# generate-librobne-docs.sh — Static HTML API browse for librobne (doc2go).
#
# pkgsite is a live server and cannot ship on GitHub Pages. doc2go writes
# static HTML scoped to ./librobne/... (not internal/, not vendor/).
#
# Usage:
#   ./scripts/generate-librobne-docs.sh [OUT_DIR]
#   make docs-build   # mkdocs → _site, then this script → _site/pkg
#
# Default OUT_DIR: <repo>/_site/pkg
# Pin: DOC2GO_VERSION (default v0.12.2) when the script installs the tool.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="${1:-$ROOT_DIR/_site/pkg}"
DOC2GO_VERSION="${DOC2GO_VERSION:-v0.12.2}"
HOME_PKG="github.com/redhatinsights/ros-ocp-backend/librobne"

if [[ "$OUT" != /* ]]; then
  OUT="$ROOT_DIR/$OUT"
fi

export PATH="${PATH}:$(go env GOPATH 2>/dev/null || true)/bin"

if ! command -v doc2go >/dev/null 2>&1; then
  echo "Installing doc2go ${DOC2GO_VERSION}..."
  go install "go.abhg.dev/doc2go@${DOC2GO_VERSION}"
fi

rm -rf "$OUT"
mkdir -p "$OUT"

echo "Generating librobne HTML docs → $OUT"
doc2go \
  -C "$ROOT_DIR/librobne" \
  -out "$OUT" \
  -home "$HOME_PKG" \
  -rel-link-style directory \
  -pagefind=false \
  ./...

# GitHub Pages for this repo is https://pgarciaq.github.io/ros-ocp-backend/
# (not domain root). doc2go emits href="/" for the current-package breadcrumb
# and the home "Root" link; rewrite to ./ so those stay under /pkg/.
find "$OUT" -name '*.html' -print0 | xargs -0 sed -i 's|href="/"|href="./"|g'

if [[ ! -f "$OUT/index.html" ]] || [[ ! -f "$OUT/engine/index.html" ]]; then
  echo "generate-librobne-docs.sh: expected index.html and engine/index.html under $OUT" >&2
  exit 1
fi

echo "Done. Browse $OUT/index.html (published: https://pgarciaq.github.io/ros-ocp-backend/pkg/)"
