#!/usr/bin/env bash
# demo-ingest.sh — publish sample CSVs to Kafka in ~100-file chunks.
# Usage: scripts/demo-ingest.sh <samples-subdir> [org_id]
# Example: scripts/demo-ingest.sh bench20k 1234567
# See docs/demo/local-full-stack.md §3. Re-publishing identical files is safe
# (digest upserts are idempotent); to FORCE reprocessing, reset statuses first:
#   UPDATE report_file_status SET status='pending';
set -euo pipefail

SUBDIR="${1:?samples subdir, e.g. bench20k}"
ORG="${2:-1234567}"
CLUSTER="${DEMO_CLUSTER_UUID:-550e8400-e29b-41d4-a716-446655440001}"
NGINX_BASE="${DEMO_NGINX_BASE:-http://localhost:8888}"
KAFKA_BOOTSTRAP="${DEMO_KAFKA_BOOTSTRAP:-localhost:29092}"

cd "$(dirname "$0")/../.."
REQ="demo-$(date -u +%H%M%S)-$$"
i=0
ls "scripts/samples/$SUBDIR" | sort | split -n l/7 -d --filter="cat > /tmp/demo-chunk-$REQ-\$FILE" /dev/stdin
for chunk in /tmp/demo-chunk-"$REQ"-*; do
  i=$((i + 1))
  FILES=$(sed "s|^|$NGINX_BASE/$SUBDIR/|" "$chunk" | python3 -c "import sys,json; print(json.dumps([l.strip() for l in sys.stdin]))")
  echo "{\"request_id\":\"$REQ-$i\",\"b64_identity\":\"test\",\"metadata\":{\"org_id\":\"$ORG\",\"source_id\":\"1\",\"cluster_uuid\":\"$CLUSTER\",\"cluster_alias\":\"bench\"},\"files\":$FILES}" \
    | docker compose -f scripts/docker-compose.yml exec -T kafka kafka-console-producer \
      --topic hccm.ros.events --broker-list "$KAFKA_BOOTSTRAP" 2>/dev/null
  echo "chunk $i sent ($(wc -l < "$chunk") files)"
done
rm -f /tmp/demo-chunk-"$REQ"-*
echo "OK: all chunks published. Watch report_file_status for pending/done; conversions stall on 'processing'."
