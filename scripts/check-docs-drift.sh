#!/usr/bin/env bash
# check-docs-drift.sh — Lightweight Class A docs footgun checks (#418).
# Not a full docs linter. Fail closed on known high-churn lies.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

fail=0

err() {
  echo "docs-drift FAIL: $*" >&2
  fail=1
}
ok() {
  echo "docs-drift OK: $*"
}

# --- ROS_TAGS_SOURCE: chart/deployed default is api; binary unset may be db ---
# Positive: configuration.md must state api as chart default near TAGS_SOURCE docs.
if ! rg -q 'ROS_TAGS_SOURCE' docs-site/configuration.md; then
  err "docs-site/configuration.md missing ROS_TAGS_SOURCE"
elif ! rg -q '`api` \(chart default\)' docs-site/configuration.md; then
  err "docs-site/configuration.md must document \`api\` (chart default) for tags source"
else
  ok "configuration.md states api (chart default) for tags"
fi

# Forbidden: claiming the *chart* default is db (binary unset / advanced is the db case).
# Do not match table rows that list both "api (chart default)" and a separate db column.
if rg -n \
  -e 'chart default:\s*`db`' \
  -e 'chart default[^|\n]{0,15}`db`\)' \
  -e 'ROS_TAGS_SOURCE=db`? \(chart' \
  -e '\(chart default\) `db`' \
  -e 'chart / deployed default[^|\n]{0,20}`db`' \
  docs-site/configuration.md \
  docs-site/operations/configuration.md \
  docs-site/features/tag-filtering.md \
  docs-site/architecture/configurability.md 2>/dev/null; then
  err "docs claim chart default ROS_TAGS_SOURCE=db (chart/deployed default is api; db is binary unset / advanced)"
else
  ok "no false 'chart default = db' claim for ROS_TAGS_SOURCE"
fi

# Getting Started / Configuration freshness stamps (acceptance for #413/#418).
for f in docs-site/quickstart.md docs-site/configuration.md; do
  if rg -q '^> \*\*Last verified:\*\* [0-9]{4}-[0-9]{2}-[0-9]{2}' "$f"; then
    ok "$f has Last verified stamp"
  else
    err "$f missing '> **Last verified:** YYYY-MM-DD' near top"
  fi
done

# --- Plugin registration: blank import belongs in plugins.go, not registry.go ---
if ! rg -q '### Adding a plugin' docs-site/development.md; then
  err "docs-site/development.md missing '### Adding a plugin' section"
elif ! awk '/### Adding a plugin/{p=1} p&&/^### / && !/### Adding a plugin/{exit} p' docs-site/development.md \
  | rg -q 'plugins\.go'; then
  err "development.md 'Adding a plugin' must mention plugins.go (blank import)"
elif awk '/### Adding a plugin/{p=1} p&&/^### / && !/### Adding a plugin/{exit} p' docs-site/development.md \
  | rg -q 'Do \*\*not\*\* edit.*registry\.go|not edit.*registry\.go'; then
  ok "development.md Adding a plugin mentions plugins.go and warns off registry.go"
else
  # plugins.go present is the hard requirement; registry warning is soft-ok if phrasing drifts
  if awk '/### Adding a plugin/{p=1} p&&/^### / && !/### Adding a plugin/{exit} p' docs-site/development.md \
    | rg -q 'registry\.go'; then
    ok "development.md Adding a plugin mentions plugins.go (and registry.go)"
  else
    ok "development.md Adding a plugin mentions plugins.go"
  fi
fi

# --- README plugin table covers every production plugin in plugins.go (#529) ---
# plugins.go blank imports are the source of truth; the example plugin is
# intentionally excluded from production. Endpoint tables are prose and stay
# manually maintained — do not try to lint them here.
while IFS= read -r p; do
  [ "$p" = "example" ] && continue
  if rg -q "^\| \`$p\` \|" README.md; then
    ok "README plugin table covers $p"
  else
    err "README plugin table missing plugin $p (see internal/plugins/plugins.go)"
  fi
done < <(sed -n 's|.*internal/plugins/\([a-z-]*\)".*|\1|p' internal/plugins/plugins.go | sort -u)

# Convention documented for contributors
if ! rg -q 'Last-verified convention' CONTRIBUTING.md; then
  err "CONTRIBUTING.md missing 'Last-verified convention' section"
else
  ok "CONTRIBUTING.md documents Last-verified convention"
fi

if [ "$fail" -ne 0 ]; then
  echo "docs-drift: one or more checks failed" >&2
  exit 1
fi
echo "docs-drift: all checks passed"
