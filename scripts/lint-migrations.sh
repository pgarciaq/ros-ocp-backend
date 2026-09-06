#!/usr/bin/env bash
# lint-migrations.sh — CI guard for non-CONCURRENT indexes on large tables.
#
# golang-migrate wraps each file in a transaction, so CREATE INDEX CONCURRENTLY
# cannot be used in standard migration files. For large tables, use the K8s Job
# pattern documented in docs/operations/large-table-migrations.md.
#
# Usage:
#   ./scripts/lint-migrations.sh [file.up.sql ...]
#   With explicit files, lints exactly those (CI passes changed files vs the
#   base branch). With no args, lints all migrations/*.up.sql — pre-existing
#   violations (000015, 000017, 000179) fail in that mode; that mode is for
#   local full-tree audits, not CI.
#
# deploy/migrations/ is intentionally out of scope: example-only patterns
# (e.g. 000158) that golang-migrate never applies (the image copies only
# migrations/). Do not move live migrations there to dodge this lint.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LARGE_TABLES="${ROS_MIGRATION_LARGE_TABLES:-recommendation_sets,namespace_recommendation_sets,node_recommendations,gpu_container_digests,daily_container_digests,recommendation_history,org_container_keys,snapshot_recommendation_sets,snapshot_inventory}"

fail=0

lint_file() {
  local file="$1"
  local base
  base="$(basename "$file")"

  # Match CREATE [UNIQUE] INDEX without CONCURRENTLY (case-insensitive,
  # allow IF NOT EXISTS). UNIQUE matters: large-table UNIQUE rebuilds
  # (000189, 000193) take the same write locks. Statements are matched whole,
  # not per line: the index name and ON <table> usually sit on different
  # lines, which line-based matching misses entirely.
  local stmt="" line upper
  check_stmt() {
    upper="${stmt^^}"
    if [[ "$upper" =~ CREATE[[:space:]]+(UNIQUE[[:space:]]+)?INDEX ]] && [[ ! "$upper" =~ CONCURRENTLY ]]; then
      local table first
      first="$(echo "$stmt" | grep -m1 -io 'CREATE[^;]*' | head -c 120)"
      for table in ${LARGE_TABLES//,/ }; do
        if [[ "$upper" =~ ON[[:space:]]+${table^^} ]] || [[ "$upper" =~ ON[[:space:]]+\"${table}\" ]]; then
          echo "ERROR: $base creates a non-CONCURRENT index on large table '$table'"
          echo "       $first"
          echo "       Use docs/operations/large-table-migrations.md (K8s Job + commented migration)."
          fail=1
        fi
      done
    fi
  }
  while IFS= read -r line || [[ -n "$line" ]]; do
    stmt+="$line"$'\n'
    if [[ "$line" == *";"* ]]; then
      check_stmt
      stmt=""
    fi
  done < "$file"
  if [[ -n "${stmt//[[:space:]]/}" ]]; then
    check_stmt
  fi
}

if [[ "$#" -gt 0 ]]; then
  files=("$@")
else
  mapfile -t files < <(find "$ROOT/migrations" -maxdepth 1 -name '*.up.sql' | sort)
fi

for f in "${files[@]}"; do
  [[ -f "$f" ]] || continue
  lint_file "$f"
done

if [[ "$fail" -ne 0 ]]; then
  exit 1
fi

echo "migration lint: OK (${#files[@]} file(s) checked)"
