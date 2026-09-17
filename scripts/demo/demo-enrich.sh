#!/usr/bin/env bash
# demo-enrich.sh — business-hours schedule, cost seeding, tag sync, savings recalc.
# Prerequisites: Koku up with source+cost-model (runbook §2), ros-ocp API up (§4),
# dataset ingested (§3). Idempotent where cheap; re-running re-PUTs same state.
# See docs/demo/local-full-stack.md §2/§5.
set -euo pipefail

ORG="${DEMO_ORG_ID:-1234567}"
CLUSTER="${DEMO_CLUSTER_UUID:-550e8400-e29b-41d4-a716-446655440001}"
ROS="${DEMO_ROS_API:-http://localhost:8002}"
IDENTITY="${DEMO_IDENTITY_JSON:-{\"identity\":{\"account_number\":\"10001\",\"org_id\":\"$ORG\",\"type\":\"User\",\"user\":{\"username\":\"user_dev\",\"email\":\"user_dev@foo.com\",\"is_org_admin\":true}},\"entitlements\":{\"cost_management\":{\"is_entitled\":true}}}}"
IDENTITY_B64="$(echo -n "$IDENTITY" | base64 -w0)"
SAMPLES_DIR="${1:-$(dirname "$0")/samples/bench20k}"

echo "== 1/4 business-hours schedule"
curl -s -m 30 -X PUT -H "x-rh-identity: $IDENTITY_B64" -H 'Content-Type: application/json' \
  -d '{"timezone":"Europe/Madrid","schedule":{"days":["monday","tuesday","wednesday","thursday","friday"],"start_time":"08:00","end_time":"17:00"},"off_hours_weight":0.0,"enabled":true}' \
  "$ROS/api/cost-management/v1/recommendations/openshift/settings/business-hours" | head -c 120
echo

echo "== 2/4 cost summary seeding (shortcut for the Koku cost pipeline)"
python3 - "$SAMPLES_DIR" <<'EOF'
import csv, glob, os, sys, uuid
from collections import defaultdict
agg = defaultdict(lambda: [0.0, 0.0, 0.0, 0.0])
files = sorted(glob.glob(os.path.join(sys.argv[1], '*ocp_pod_usage*.csv')))
for f in files:
    with open(f, newline='') as fh:
        for row in csv.DictReader(fh):
            try:
                k = (row['namespace'], row['interval_start'][:10])
                a = agg[k]
                a[0] += float(row['pod_usage_cpu_core_seconds'] or 0) / 3600
                a[1] += float(row['pod_request_cpu_core_seconds'] or 0) / 3600
                a[2] += float(row['pod_usage_memory_byte_seconds'] or 0) / 3600 / 2**30
                a[3] += float(row['pod_request_memory_byte_seconds'] or 0) / 3600 / 2**30
            except (ValueError, KeyError):
                pass
with open('/tmp/demo-cost-seed.sql', 'w') as out:
    for (ns, day), (cu, cr, mu, mr) in agg.items():
        out.write(
            "INSERT INTO org1234567.reporting_ocpusagelineitem_daily_summary "
            "(uuid, usage_start, usage_end, namespace, cluster_id, data_source, cost_model_rate_type, "
            "cost_model_cpu_cost, cost_model_memory_cost, pod_usage_cpu_core_hours, pod_request_cpu_core_hours, "
            "pod_usage_memory_gigabyte_hours, pod_request_memory_gigabyte_hours) VALUES "
            f"('{uuid.uuid4()}', '{day}', '{day}', '{ns}', '550e8400-e29b-41d4-a716-446655440001', "
            f"'Pod', 'Supplementary', {cu*0.5:.4f}, {mu*0.1:.4f}, {cu:.4f}, {cr:.4f}, {mu:.4f}, {mr:.4f});\n")
print(len(agg), 'namespace-days staged (assumes $0.50 CPU / $0.10 GB cost model)')
EOF
docker exec -i koku-db psql -U postgres -d postgres -v ON_ERROR_STOP=1 -f - < /tmp/demo-cost-seed.sql >/dev/null && echo "cost rows inserted"

echo "== 3/4 tag sync from namespace labels"
python3 - "$SAMPLES_DIR" <<'EOF'
import csv, glob, json, os, sys
from collections import defaultdict
d = sys.argv[1]
nstags = {}
for pat in ('*ocp_namespace_label*.csv',):
    for f in sorted(glob.glob(os.path.join(d, pat))):
        with open(f, newline='') as fh:
            for row in csv.DictReader(fh):
                ns = row['namespace']
                if ns in nstags:
                    continue
                tags = {}
                for kv in (row.get('namespace_labels') or '').split('|'):
                    if ':' in kv:
                        k, v = kv.split(':', 1)
                        tags[k[6:] if k.startswith('label_') else k] = v
                if tags:
                    nstags[ns] = tags
keys = defaultdict(set)
for ns, t in nstags.items():
    for k, v in t.items():
        keys[k].add(v)
payload = {"org_id": os.environ.get("DEMO_ORG_ID", "1234567"),
           "synced_at": "2026-09-17T10:00:00Z",
           "tag_keys": [{"key": k, "values": sorted(v)} for k, v in sorted(keys.items())],
           "namespace_tags": [{"cluster_uuid": os.environ.get(
               "DEMO_CLUSTER_UUID", "550e8400-e29b-41d4-a716-446655440001"),
               "namespace": ns, "tags": t} for ns, t in sorted(nstags.items())]}
json.dump(payload, open('/tmp/demo-tag-payload.json', 'w'))
print(len(nstags), 'namespaces,', len(keys), 'tag keys')
EOF
curl -s -m 30 -X POST "$ROS/api/cost-management/v1/internal/tags/sync" \
  -H 'Content-Type: application/json' -d @/tmp/demo-tag-payload.json | head -c 120
echo

echo "== 4/4 savings recalculation"
curl -s -m 60 -X POST "$ROS/api/cost-management/v1/internal/recalculate-savings" \
  -H 'Content-Type: application/json' \
  -d "{\"org_id\":\"$ORG\",\"cluster_uuid\":\"$CLUSTER\",\"recommendation_types\":[\"container\",\"namespace\",\"node\",\"pvc\",\"quota\",\"cluster-quota\"]}" | head -c 200
echo
echo "OK: enrich complete. Verify savings/tags/BH per runbook §5."
