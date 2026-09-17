#!/usr/bin/env bash
# demo-down.sh — tear down the local demo stack and revert borrowed config.
# See docs/demo/local-full-stack.md §8. Keeps the built images (fast restart).
set -euo pipefail

echo "== stopping host processes (review PIDs before confirming) =="
ps aux | grep -E "[e]xe/rosocp start|[g]ateway.py|[w]ebpack" | awk '{print $2, $11, $12, $13}' | head -8
echo "Kill API/processor/gateway/UI yourself (kill <pid>), or press enter to pkill them now."
echo "(UI note: use pipe-safe patterns; never 'pkill -f' with a self-matching string.)"

echo "== stopping compose stacks =="
(cd "$(dirname "$0")" && docker compose -f scripts/docker-compose.yml down) 2>/dev/null || true

echo "== reverting borrowed repo edits (demo-only tweaks) =="
echo "  - ros-ocp-backend scripts/docker-compose.yml kafka advertised-listener (if patched)"
echo "  - koku docker-compose.yml worker port ranges (if pinned to single ports)"
echo "Run: git diff --stat in each repo and git checkout -- <file> as needed."

echo "== optional: delete generated data =="
echo "  rm -rf scripts/samples/bench20k scripts/samples/entities scripts/samples/fresh-data scripts/samples/nise-data"
echo "Done. Verify: git status clean (except .env, which is gitignored)."
