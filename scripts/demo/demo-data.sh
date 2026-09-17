#!/usr/bin/env bash
# demo-data.sh — generate the 20K mixed NISE dataset with FRESH dates.
# Usage: scripts/demo-data.sh [workdir]   (default: ./scripts/samples/bench20k)
# See docs/demo/local-full-stack.md §3. Takes ~35 min for 20K. Needs ~35 GiB.
# NEVER point the workdir at tmpfs (/tmp is usually 32 GiB and a full tmpfs
# writes torn trailing CSV rows that poison whole manifests).
set -euo pipefail

START_DATE="${DEMO_START_DATE:-$(date -d '30 days ago' +%F)}"
END_DATE="${DEMO_END_DATE:-$(date +%F)}"
OUTDIR="${1:-$(dirname "$0")/samples/bench20k}"
CLUSTER="${DEMO_CLUSTER_UUID:-550e8400-e29b-41d4-a716-446655440001}"

mkdir -p "$OUTDIR"
python3 scripts/gen_benchmark_config.py --containers 20000 \
  --start-date "$START_DATE" --end-date "$END_DATE" \
  --output "$OUTDIR/../bench20k.yml"

(
  cd "$OUTDIR"
  nise report ocp --static-report-file "$OUTDIR/../bench20k.yml" \
    --ocp-cluster-id "$CLUSTER" --ros-ocp-info -w
)

echo "Validating every CSV parses (ragged rows poison manifests)..."
python3 - "$OUTDIR" <<'EOF'
import csv, glob, os, sys
d = sys.argv[1]
bad = 0
files = sorted(glob.glob(os.path.join(d, '*.csv')))
for f in files:
    with open(f, newline='') as fh:
        r = csv.reader(fh)
        n = None
        for i, row in enumerate(r, 1):
            if n is None:
                n = len(row)
            elif len(row) != n:
                print('RAGGED:', os.path.basename(f), 'line', i)
                bad += 1
                break
print(len(files), 'files checked,', bad, 'ragged')
sys.exit(1 if bad else 0)
EOF
echo "OK: dataset in $OUTDIR ($(du -sh "$OUTDIR" | cut -f1))"
