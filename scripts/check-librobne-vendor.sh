#!/usr/bin/env bash
# check-librobne-vendor.sh — Fail if vendor librobne drifted from ./librobne (#510).
#
# Parent go.mod has replace => ./librobne. Nested tests (go test -C librobne) use
# ./librobne. Parent go test / go build with vendor/ present use -mod=vendor.
# The product Dockerfile + .dockerignore do not COPY vendor/; that image uses
# replace. This script does not inspect Containerfiles — see
# docs-site/architecture/librobne.md (vendor vs replace).
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

LIBROBNE_VENDOR="vendor/github.com/redhatinsights/ros-ocp-backend/librobne"

if ! command -v go >/dev/null 2>&1; then
  echo "librobne-vendor-check FAIL: go not on PATH" >&2
  exit 1
fi

go mod vendor

if git diff --exit-code -- "$LIBROBNE_VENDOR"; then
  echo "librobne-vendor-check OK: ${LIBROBNE_VENDOR} matches go mod vendor"
  exit 0
fi

echo "librobne-vendor-check FAIL: ${LIBROBNE_VENDOR} drifted from ./librobne" >&2
echo "Fix: go mod vendor && git add ${LIBROBNE_VENDOR}" >&2
echo "Tests and librobne/go.mod stay only under ./librobne; vendor has production .go files." >&2
exit 1
