#!/usr/bin/env bash
# check-docs-sync.sh — one-sided drift signal for the parallel docs/ ↔
# docs-site pairs (#816).
#
# The failure mode this catches: someone edits one side of a registered pair
# and not the other. Content DIFF is expected for every pair (link style,
# front-matter, intentional depth) and stays a non-signal checklist line;
# ONE-SIDED is the signal: exactly one side of a pair changed on this branch
# vs the merge base with the default branch.
#
# Stub pairs (flags column 'stub' in the registry) are public-led by design:
# a public-only edit is silent; an internal-only edit (someone touched the
# stub) is reported.
#
# Options:
#   --root DIR       run against another checkout (tests)
#   --pairs FILE     another pair registry (tests)
#   --base REF       merge-base ref (default: origin/main, falling back to main)
#   --fail-on-diff   exit 1 when any pair differs or is missing (inventory mode)
#
# Exit 0 always otherwise — ONE-SIDED is advisory (#816 close criterion:
# advisory-first; flip to blocking only if it earns trust).
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PAIRS_FILE=""
BASE_REF=""
FAIL_ON_DIFF=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --fail-on-diff) FAIL_ON_DIFF=1 ;;
    --root) ROOT_DIR="${2:-}"; shift ;;
    --pairs) PAIRS_FILE="${2:-}"; shift ;;
    --base) BASE_REF="${2:-}"; shift ;;
    *)
      echo "check-docs-sync.sh: unknown option: $1" >&2
      exit 2
      ;;
  esac
  shift
done

if [[ -z "$PAIRS_FILE" ]]; then
  PAIRS_FILE="$ROOT_DIR/scripts/docs-sync-pairs.txt"
fi

cd "$ROOT_DIR"

# Resolve the merge base for one-sided detection. An explicit --base must
# resolve or the detection is skipped with a notice (never a failure); the
# default tries origin/main then main.
MERGE_BASE=""
resolve_base() {
  local ref="$1"
  git merge-base HEAD "$ref" 2>/dev/null || true
}
if [[ -n "$BASE_REF" ]]; then
  MERGE_BASE="$(resolve_base "$BASE_REF")"
else
  MERGE_BASE="$(resolve_base origin/main)"
  if [[ -z "$MERGE_BASE" ]]; then
    MERGE_BASE="$(resolve_base main)"
  fi
fi
if [[ -z "$MERGE_BASE" ]]; then
  echo "docs-sync-check: no merge base (explicit: ${BASE_REF:-none}; tried origin/main, main) — one-sided drift detection skipped"
fi

# side_touched PATH: 0 when PATH changed between the merge base and HEAD.
side_touched() {
  [[ -n "$MERGE_BASE" ]] || return 1
  git diff --name-only "$MERGE_BASE" HEAD -- "$1" 2>/dev/null | grep -q .
}

same=0
diff=0
missing=0
one_sided=0

printf '%-9s  %-55s  %s\n' "STATUS" "INTERNAL" "PUBLIC"
printf '%s\n' "------------------------------------------------------------------------------------------------"

while IFS='|' read -r internal public flags notes; do
  case "$internal" in ''|\#*) continue ;; esac
  if [[ ! -f "$internal" ]]; then
    printf '%-9s  %-55s  %s\n' "MISSING" "$internal" "(internal absent)"
    missing=$((missing + 1))
    continue
  fi
  if [[ ! -f "$public" ]]; then
    printf '%-9s  %-55s  %s\n' "MISSING" "$internal" "$public (public absent)"
    missing=$((missing + 1))
    continue
  fi
  if cmp -s "$internal" "$public"; then
    printf '%-9s  %-55s  %s\n' "SAME" "$internal" "$public"
    same=$((same + 1))
  else
    # rough size hint
    a=$(wc -l <"$internal" | tr -d ' ')
    b=$(wc -l <"$public" | tr -d ' ')
    printf '%-9s  %-55s  %s  (%sl ↔ %sl; %s)\n' "DIFF" "$internal" "$public" "$a" "$b" "$notes"
    diff=$((diff + 1))
  fi

  # One-sided drift (#816): exactly one side changed on this branch.
  if [[ -n "$MERGE_BASE" ]]; then
    in_touched=false
    pub_touched=false
    if side_touched "$internal"; then in_touched=true; fi
    if side_touched "$public"; then pub_touched=true; fi
    if $in_touched && ! $pub_touched; then
      if [[ "$flags" == *stub* ]]; then
        printf '%-9s  %-55s  %s\n' "ONE-SIDED" "$internal" "stub pair: the stub itself changed — public not: $public"
      else
        printf '%-9s  %-55s  %s\n' "ONE-SIDED" "$internal" "internal changed; public not: $public"
      fi
      one_sided=$((one_sided + 1))
    elif $pub_touched && ! $in_touched; then
      if [[ "$flags" == *stub* ]]; then
        # Public-led by design: the stub follows the public side or stays.
        :
      else
        printf '%-9s  %-55s  %s\n' "ONE-SIDED" "$internal" "public changed; internal not: $public"
        one_sided=$((one_sided + 1))
      fi
    fi
  fi
done <"$PAIRS_FILE"

echo
echo "docs-sync-check: SAME=$same DIFF=$diff MISSING=$missing ONE-SIDED=$one_sided"
echo "Note: DIFF is normal (link style, Last verified, intentional depth)."
echo "ONE-SIDED is the signal: exactly one side of the pair changed on this branch (#816) — check the pair."
echo "Stub pairs never become SAME — their facts live on the docs-site side only; do not copy back."
echo "When editing Class A facts, update both sides of the pair — see docs/agents/docs-site-sync.md"

if [[ "$FAIL_ON_DIFF" -eq 1 && ( "$diff" -gt 0 || "$missing" -gt 0 ) ]]; then
  echo "docs-sync-check: --fail-on-diff set and differences found" >&2
  exit 1
fi
exit 0
