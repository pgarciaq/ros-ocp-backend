#!/usr/bin/env bash
# lint-migrations.sh — CI guard for non-CONCURRENT indexes on large tables.
#
# golang-migrate wraps each file in a transaction, so CREATE INDEX CONCURRENTLY
# cannot be used in standard migration files. For large tables, use the K8s Job
# pattern documented in docs/operations/large-table-migrations.md.
#
# Usage:
#   ./scripts/lint-migrations.sh [file.up.sql ...]
#   With no args, lints all migrations/*.up.sql changed vs origin/main.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LARGE_TABLES="${ROS_MIGRATION_LARGE_TABLES:-recommendation_sets,namespace_recommendation_sets,node_recommendations,gpu_container_digests,daily_container_digests,recommendation_history,org_container_keys,snapshot_recommendation_sets,snapshot_inventory}"

fail=0

lint_file() {
  local file="$1"
  local base
  base="$(basename "$file")"

  # Match CREATE INDEX without CONCURRENTLY (case-insensitive, allow IF NOT EXISTS).
  while IFS= read -r line; do
    local upper="${line^^}"
    if [[ "$upper" =~ CREATE[[:space:]]+INDEX ]] && [[ ! "$upper" =~ CONCURRENTLY ]]; then
      for table in ${LARGE_TABLES//,/ }; do
        if [[ "$upper" =~ ON[[:space:]]+${table^^} ]] || [[ "$upper" =~ ON[[:space:]]+\"${table}\" ]]; then
          echo "ERROR: $base creates a non-CONCURRENT index on large table '$table'"
          echo "       $line"
          echo "       Use docs/operations/large-table-migrations.md (K8s Job + commented migration)."
          fail=1
        fi
      done
    fi
  done < "$file"
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
