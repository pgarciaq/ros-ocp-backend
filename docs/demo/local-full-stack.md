# Local Full-Stack Demo: Cost Management + ROS-OCP Native Engine

> **Last verified:** 2026-09-17
> **Scope:** full local stack on one laptop (Koku backend + koku-ui-onprem + ros-ocp-backend + Kafka/Postgres), 20K-workload NISE dataset, with savings, tags, and business-hours data. Built from the Sep 2026 demo sessions; every gotcha below cost real debugging time.
> **Assumptions:** nise with static ROS generators (tested: local `pgarciaq/nise` phase17, `nise 5.4.2` CLI), Koku `main`-era compose, 60+ GiB free disk, 16+ GB free RAM. Timings on a 22-core/64 GiB laptop: NISE gen ~35 min, ingest ~15–30 min, UI build 0 min (uses existing `node_modules`).

## What you get

ROS Optimizations pages with ~18K workloads of recommendations (container, namespace, node, GPU, quota, PVC, VM, snapshots), dollar savings, tag filtering, and business-hours vs all-hours detail nests — all browsable in `koku-ui-onprem`.

## 0. One-time decisions (read before starting)

- **org + cluster are baked into data.** Pick them once and reuse everywhere in this guide: default org `1234567`, cluster `550e8400-e29b-41d4-a716-446655440001`. The Koku tenant, cost model, tag sync, and every Kafka message must agree.
- **Your UID probably can't map into containers.** Enterprise LDAP UIDs (e.g. 7 digits) exceed the rootless subuid range. Check `grep "^$USER:" /etc/subuid` and set `USER_ID`/`GROUP_ID` to the range start (e.g. `165536`) in Koku's `.env`. Same class of failure appears as `cannot setuid to unmapped uid` (build), `Permission denied` on bind mounts, and Django `app.log` write errors (fix: `chmod -R a+rwX koku/`, dev-only).
- **Port map (do not improvise).** Koku owns `:8000`; ros-api moves to `:8002`; gateway on `:8080` splits `/recommendations/*` → ros-api, everything else → Koku; UI on `:9001`; Koku DB on `:15433` (ros DB keeps `:15432`).
- **Never generate NISE output on tmpfs.** `/tmp` is typically 32 GiB tmpfs; a 20K dataset is ~31 GiB and a full tmpfs produces *torn trailing CSV rows* that poison whole manifests. Generate under `./scripts/samples/` (repo disk).
- **Fresh dates or nothing renders.** UI/API default windows look back ~14 days and GPU freshness gates apply. Always generate with `--start-date` ≈ 30 days ago through today. Stale data is the #1 cause of "empty page, API fine".

## 1. Start infrastructure

```bash
# ros-ocp-backend side (this repo)
cp .env.example .env   # once
docker compose -f scripts/docker-compose.yml up -d db-ros kafka zookeeper kafka-create-topics nginx unleash-edge
go run rosocp.go db migrate up
```

Kafka advertises `kafka:29092`, unresolvable from host processes. For host-run
processor/API work, point it at localhost (revert after the demo — see §8):

```bash
# scripts/docker-compose.yml: KAFKA_ADVERTISED_LISTENERS=PLAINTEXT://localhost:29092
docker compose -f scripts/docker-compose.yml up -d kafka
```

Koku side (`~/dev/koku/koku`): ensure `.env` has `USER_ID`/`GROUP_ID` from the
mapped range plus the `UNLEASH_*` sample tokens from `.env.example`, then:

```bash
export ONPREM=True USER_ID=165536 GROUP_ID=165536 POSTGRES_SQL_SERVICE_PORT=15433
docker compose build koku-base   # one image; everything else reuses it (slow, ~20 min first time)
docker compose build db
docker compose up -d db valkey unleash koku-server masu-server koku-worker koku-beat
```

Known issues and workarounds (all hit during setup):
- Old docker daemons reject port *ranges* (`6001-6020:9000`): temporarily pin
  worker/beat to single ports, revert in §8.
- `trino` dependency failure: start backends with `--no-deps` (ONPREM needs no Trino).
- Koku `:9001` clash is `koku-s4` (S3 mock): `docker stop koku-s4 koku-s4-proxy` (UI needs 9001).
- First boot migrates (~208 migrations); stale volumes from older checkouts
  break migration with "relation already exists" — wipe only if the DB holds
  nothing of value.

## 2. Koku tenant, source, cost model

The stock `create_test_customer.py` is stale vs current schema — provision via API:

```bash
IDENTITY=$(echo -n '{"identity":{"account_number":"10001","org_id":"1234567","type":"User","user":{"username":"user_dev","email":"user_dev@foo.com","is_org_admin":true}},"entitlements":{"cost_management":{"is_entitled":true}}}' | base64 -w0)
# OCP source for the demo cluster (idempotent; 400 duplicate means it exists)
curl -X POST -H "x-rh-identity: $IDENTITY" -H 'Content-Type: application/json' \
  -d '{"name":"demo-ocp","source_type":"OCP","authentication":{"credentials":{"cluster_id":"550e8400-e29b-41d4-a716-446655440001"}},"billing_source":{"data_source":{}}}' \
  http://localhost:8000/api/cost-management/v1/sources/
# Cost model with CPU/memory rates, then assign to the provider UUID from above
curl -X POST -H "x-rh-identity: $IDENTITY" -H 'Content-Type: application/json' \
  -d '{"name":"demo-cost-model","description":"demo rates","source_type":"OCP","rates":[{"metric":{"name":"cpu_core_usage_per_hour"},"tiered_rates":[{"value":0.5,"unit":"USD"}]},{"metric":{"name":"memory_gb_usage_per_hour"},"tiered_rates":[{"value":0.1,"unit":"USD"}]}]}' \
  http://localhost:8000/api/cost-management/v1/cost-models/
curl -X PUT -H "x-rh-identity: $IDENTITY" -H 'Content-Type: application/json' \
  -d '{"name":"demo-cost-model","description":"demo rates","source_type":"OCP","source_uuids":["<provider-uuid>"]}' \
  http://localhost:8000/api/cost-management/v1/cost-models/<model-uuid>/
```

Verify the chain ROS depends on (note `start_date`/`end_date` names — `start`/`end` silently fall back to this month):

```bash
curl -s "http://localhost:5042/api/cost-management/v1/effective_rates/?cluster_id=550e8400-e29b-41d4-a716-446655440001&org_id=1234567&start_date=2026-08-18&end_date=2026-09-17" \
  -H "x-rh-identity: $IDENTITY" | python3 -c "import sys,json; print(len(json.load(sys.stdin)['namespace_aggregates']), 'namespaces')"
```

`namespace_aggregates` comes from `reporting_ocpusagelineitem_daily_summary` —
i.e. it needs Koku cost data. Shortcut used for demos (equivalent numbers,
no 2-hour cost pipeline): aggregate NISE `ocp_pod_usage` CSVs × cost-model
rates and INSERT directly (step 2 of `scripts/demo/demo-enrich.sh`). Only columns
`uuid, usage_start, usage_end, namespace, cluster_id, data_source='Pod'`,
cost/usage numerics are required.

## 3. Generate data

```bash
python3 scripts/gen_benchmark_config.py --containers 20000 \
  --start-date 2026-08-18 --end-date 2026-09-17 --output /tmp/bench20k.yml
mkdir -p /tmp/benchwork && cd /tmp/benchwork   # NOT /tmp root: tmpfs (see §0)
nise report ocp --static-report-file /tmp/bench20k.yml \
  --ocp-cluster-id 550e8400-e29b-41d4-a716-446655440001 --ros-ocp-info -w
```

Expect ~650 files / ~31 GB in ~35 min. Then validate every CSV parses
(ragged rows poison whole manifests — two incidents so far, one from a full
tmpfs, one from NISE itself writing a torn last line):

```bash
python3 -c "
import csv, glob
for f in sorted(glob.glob('/tmp/benchwork/*.csv')):
    with open(f, newline='') as fh:
        r = csv.reader(fh); n = None
        for i, row in enumerate(r, 1):
            if n is None: n = len(row)
            elif len(row) != n: print('RAGGED:', f, i); break
"
```

Known generator gap: `gen_benchmark_config.py` VM/storage/snapshot/quota
blocks are silently ignored by current NISE static mode. Generate those entity
types separately from `nise/examples/ros_ocp/ocp_static_data.yml` (copy it,
move `start_date`/`end_date` to the demo window) — includes a hand-written MIG
fixture pattern: `scripts/demo/mig-fixture.yml` (2 underutilized A100-SXM4 pods,
~2K MiB FB avg; move its `start_date`/`end_date` to the demo window before
use) generates sliceable GPUs — ingest it last.

Serve and publish in ~100-file chunked messages (stays clear of message-size
limits, gives incremental progress); see `scripts/demo/demo-ingest.sh`. Watch
`report_file_status` for `pending` files that never move (stuck previous run?)
and the processor log for `exhausted|fetch blocked|encode`.

Scale notes: set `ROS_MAX_DIGEST_ROWS_PER_CLUSTER=2000000` (default 500K
aborts container recs around ~18K workloads × 30d × dual streams).

## 4. Start ros-ocp services

```bash
export ROS_CSV_ALLOWED_HOSTS=localhost KOKU_MASU_URL=http://localhost:5042 \
  ROS_MAX_DIGEST_ROWS_PER_CLUSTER=2000000
PROMETHEUS_PORT=5005 go run rosocp.go start processor   # BH on by default
API_PORT=8002 PROMETHEUS_PORT=5007 DEVELOPMENT=true \
  ROS_INTERNAL_TAGS_AUTH_REQUIRED=false ROS_TAGS_ENABLED=true ROS_TAGS_SOURCE=api \
  ROS_CSV_ALLOWED_HOSTS=localhost KOKU_MASU_URL=http://localhost:5042 \
  go run rosocp.go start api
```

`ROS_CSV_ALLOWED_HOSTS=localhost` is required for nginx-served CSVs (SSRF
guard fails closed otherwise). The `ROS_INTERNAL_TAGS_AUTH_REQUIRED=false`
dev opening powers the tag-sync and savings-recalc internal endpoints used below.

## 5. Business-hours schedule, tags, savings

```bash
# BH schedule (enables dual-stream digests; re-ingest after setting it —
# digests are ingest-time, recalc cannot backfill them)
curl -X PUT -H "x-rh-identity: $IDENTITY" -H 'Content-Type: application/json' \
  -d '{"timezone":"Europe/Madrid","schedule":{"days":["monday","tuesday","wednesday","thursday","friday"],"start_time":"08:00","end_time":"17:00"},"off_hours_weight":0.0,"enabled":true}' \
  http://localhost:8002/api/cost-management/v1/recommendations/openshift/settings/business-hours
# Tags from NISE namespace labels (bench CSVs carry no container labels) —
# step 3 of scripts/demo/demo-enrich.sh builds the SyncRequest from
# ocp_namespace_label CSVs and POSTs /internal/tags/sync.
# Savings after ingest (or re-run post-hoc any time)
curl -X POST http://localhost:8002/api/cost-management/v1/internal/recalculate-savings \
  -H 'Content-Type: application/json' \
  -d '{"org_id":"1234567","cluster_uuid":"550e8400-e29b-41d4-a716-446655440001","recommendation_types":["container","namespace","node","pvc","quota","cluster-quota"]}'
```

Verify each leg: `estimated_monthly_savings` in a detail response;
`filter[tag:<key>]=<value>` narrows lists (URL-encode brackets!);
`business_hours` nest in detail alongside all-hours.

## 6. Gateway + UI

ros-api requires `x-rh-identity`, which browsers don't send; Koku locally
needs none. `scripts/demo/demo-gateway.py` (stdlib-only reverse proxy on :8080)
routes `/recommendations/*` → ros-api with an injected demo identity and
everything else → Koku — mirroring the on-prem gateway split:

```bash
python3 scripts/demo/demo-gateway.py &   # :8080; DEMO_IDENTITY_FILE or baked-in org-1234567 admin
cd ~/dev/koku/koku-ui
API_TOKEN=false API_PROXY_URL=http://localhost:8080/api/cost-management/v1 npm run start:onprem
```

Open `http://localhost:9001/` → Cost Management → Optimizations.
Recording runbook: fleet → container list/detail (terms, engines, savings,
notifications) → BH nest → namespace/VM/PVC/quota/GPU → history → tag filter.

## 7. Interpreting empty pages (checklist before debugging code)

1. `max(bucket_date)` in digests vs today — stale data blanks default windows.
2. `report_file_status` non-done rows + processor log `exhausted|encode|blocked`.
3. GPU MIG/timeslicing need qualifying workloads (low-FB MIG-capable / underutilized T4), not just GPU rows.
4. VI-gated routes with the gate off return **400** `bad recommendation_id` (detail catch-all fall-through, ADR-0168) — not 404.
5. Bracketed query params must be URL-encoded in curl.

## 8. Teardown (restore everything borrowed)

```bash
scripts/demo/demo-down.sh   # stops API/processor/UI/gateway, docker compose down (both),
                       # reverts the kafka advertised-listener + compose port-range patches,
                       # optionally deletes scripts/samples/{bench20k,entities,fresh-data,nise-data}
```

Never commit: `.env` files (gitignored), `scripts/samples/*` CSVs, `/tmp` scratch.
Re-verify with `git status` that only intended files changed.
