#!/usr/bin/env bash
# check-docs-sync.sh — List known parallel docs/ ↔ docs-site pairs and report
# which differ (Policy A dual-tree discipline). Exit 0 always unless --fail-on-diff.
#
# Diffs are expected for link style (relative vs GitHub) and intentional depth.
# Use this as a PR checklist: when you change Class A facts on one side, check
# the paired file. Prefer docs-site for public contracts, then mirror into docs/.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

FAIL_ON_DIFF=0
if [[ "${1:-}" == "--fail-on-diff" ]]; then
  FAIL_ON_DIFF=1
fi

# internal_path|public_path|notes
PAIRS=(
  "docs/architecture/configurability.md|docs-site/architecture/configurability.md|Class A env defaults"
  "docs/architecture/recommendation-engines.md|docs-site/architecture/recommendation-engines.md|Class A engine contracts"
  "docs/architecture/cost-integration.md|docs-site/architecture/cost-integration.md|Class A Koku contracts"
  "docs/architecture/plugin-architecture.md|docs-site/architecture/plugin-architecture.md|near-parity architecture"
  "docs/architecture/native-migration.md|docs-site/architecture/native-migration.md|near-parity architecture"
  "docs/architecture/notification-codes.md|docs-site/api-reference/notification-codes.md|codes matrix"
  "docs/ui-integration-guide.md|docs-site/ui-integration-guide.md|UI/API contracts"
  "docs/upgrade-runbook.md|docs-site/operations/upgrade-runbook.md|ops upgrade"
  "docs/operations/configuration.md|docs-site/configuration.md|Class A config (depth differs)"
  "docs/operations/configuration.md|docs-site/operations/configuration.md|ops mirror of configuration"
  "docs/operations/monitoring.md|docs-site/monitoring.md|ops monitoring"
  "docs/operations/query-performance.md|docs-site/query-performance.md|ops query perf"
  "docs/known-issues.md|docs-site/known-issues.md|known issues"
  "docs/testing/validating-native-engine.md|docs-site/testing/validating-native-engine.md|testing"
  "docs/testing/iqe-requirements-registration.md|docs-site/testing/iqe-requirements-registration.md|testing"
  "docs/features/idle-detection.md|docs-site/plugin-reference/idle-detection.md|depth differs intentionally"
  "docs/features-business-hours.md|docs-site/plugin-reference/business-hours.md|depth differs intentionally"
  "docs/features/quota-recommendations.md|docs-site/plugin-reference/quota.md|plugin-ref"
  "docs/features/cluster-resource-quota.md|docs-site/plugin-reference/cluster-quota.md|plugin-ref"
  "docs/operations/api-query-parameters.md|docs-site/plugin-reference/query-parameters.md|query params"
)

same=0
diff=0
missing=0

printf '%-7s  %-55s  %s\n' "STATUS" "INTERNAL" "PUBLIC"
printf '%s\n' "--------------------------------------------------------------------------------"

for entry in "${PAIRS[@]}"; do
  IFS='|' read -r internal public notes <<<"$entry"
  if [[ ! -f "$internal" ]]; then
    printf '%-7s  %-55s  %s\n' "MISSING" "$internal" "(internal absent)"
    missing=$((missing + 1))
    continue
  fi
  if [[ ! -f "$public" ]]; then
    printf '%-7s  %-55s  %s\n' "MISSING" "$internal" "$public (public absent)"
    missing=$((missing + 1))
    continue
  fi
  if cmp -s "$internal" "$public"; then
    printf '%-7s  %-55s  %s\n' "SAME" "$internal" "$public"
    same=$((same + 1))
  else
    # rough size hint
    a=$(wc -l <"$internal" | tr -d ' ')
    b=$(wc -l <"$public" | tr -d ' ')
    printf '%-7s  %-55s  %s  (%sl ↔ %sl; %s)\n' "DIFF" "$internal" "$public" "$a" "$b" "$notes"
    diff=$((diff + 1))
  fi
done

echo
echo "docs-sync-check: SAME=$same DIFF=$diff MISSING=$missing"
echo "Note: DIFF is normal (link style, Last verified, intentional depth)."
echo "When editing Class A facts, update both sides of the pair — see docs/agents/docs-site-sync.md"

if [[ "$FAIL_ON_DIFF" -eq 1 && ( "$diff" -gt 0 || "$missing" -gt 0 ) ]]; then
  echo "docs-sync-check: --fail-on-diff set and differences found" >&2
  exit 1
fi
exit 0
