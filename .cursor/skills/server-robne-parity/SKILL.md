---
name: server-robne-parity
description: >-
  Validates that the robne CLI and the ros-ocp-backend server produce identical
  recommendations from the same input data. Use when an engine or digest change
  lands that must behave the same on both paths, when re-running the parity
  comparison that gates a cross-implementation divergence fix (#705/#706), when
  asked to compare server output against robne output, or when robne CLI
  results look inconsistent with the API or UI for the same cluster data.
---

# Server vs robne CLI Parity Validation

Run robne (files→stdout) over the **same payloads the server ingested** and
diff field-by-field against the live recommendation APIs. First run (2026-10-27):
8/10 entity types identical; VM and PVC divergent (#705 — term-window slicing).

Comparator: `compare_parity.py` in this directory (per-entity field mappings,
unit conversions, display-rounding tolerances; exit 1 on divergence).

## When to Apply

- An engine, digest, or term-window change ships that must stay consistent
  across server and CLI (acceptance gate for #705-class fixes).
- Before a release that touches `librobne` or the processor recommend path.
- Robne output looks wrong versus the API/UI for the same cluster.

## Prerequisites

- Cluster access (`oc` logged in; read the cluster's `ACCESS.md`).
- robne built (`make robne`); a payload on disk — a surviving chart E2E temp
  dir (`/tmp/e2e-*`, one cluster each) or freshly generated (next section).
- For PVC / cluster-quota legs: the E2E flows package only the CSVs they assert
  on — inject those payloads manually (fresh UUID cluster; see Procedure).

## Generating Fresh Payloads

E2E temp dirs live in `/tmp` and get cleaned — when none survive, generate.
Parity requires the **same bytes** to reach robne and the server: generate
once, upload that exact directory, and point robne at it.

1. **Template per entity** (nise repo `examples/`, or the chart's
   `tests/fixtures/nise_templates/` for cluster-quota):

   | Entity | Template |
   |---|---|
   | container / namespace / node / quota | `ros_ocp_e2e/ocp_report_ros_0.yml` |
   | gpu-mig | `ros_ocp_e2e/ocp_report_gpu_mig_ros.yml` |
   | gpu-timeslicing | `ros_ocp_e2e/ocp_report_gpu_timeslicing.yml` |
   | vm (+ pvc/gpu companions) | `ocp_vm_recommendations/vm_static_data.yml` |
   | pvc | `ros_ocp_e2e/ocp_report_pvc_rightsizing.yml` (**window-insensitive by construction** — monotonic growth gives identical values per term, so it cannot manifest the #705 medium-window bug; a window-sensitive fixture is required to detect it — recipe in #706's run records) |
   | cluster-quota | `ros_ocp_e2e/ocp_report_cluster_quota.yml` (the chart CRQ flow's template; the flow also enriches the CSV — `e2e_helpers.enrich_cluster_quota_csv_files`) |
   | snapshot | `ros_ocp_e2e/ocp_report_snapshot_classification.yml` |
   | hcp capacity | `ros_ocp_e2e/ocp_report_hcp_capacity.yml` |

2. **Generate** (inside a scratch dir; output lands in cwd). Use the chart
   repo's pinned venv nise (`~/dev/koku/cost-onprem-chart/tests/.venv/bin/nise`)
   or the nise checkout's (`~/dev/koku/nise/.venv/bin/nise`):
   `nise report ocp --static-report-file <tpl> --ocp-cluster-id <uuid4> -s <start> -e <end> --ros-ocp-info -w`
   Pass `-s`/`-e` explicitly — nise defaults end to **today**, and term windows
   anchor to the data's last day. For a fresh run use a window ending
   yesterday (the E2E flows use now−14d … now−1d) unless deliberately
   straddling a window boundary (#705 fixtures do). Snapshot-relative offsets
   (`creation_days_ago`) anchor to the window **start**, so old snapshots age
   as the window drifts. nise exits 0 silently — verify the expected CSV
   appeared with `ls` (primary CSV per entity: snapshot
   `ocp_snapshot_inventory.csv`, pvc `ocp_storage_usage.csv`, vm
   `ocp_ros_vm_usage.csv`, gpu `ocp_ros_usage.csv` + `ocp_gpu_usage.csv`,
   cluster-quota `ocp_ros_cluster_quota.csv`, hcp
   `ros-openshift-hcp-capacity-*.csv`).
3. **Upload that same directory** via the injection recipe (Procedure step 6).
4. **Real-cluster (KMO-collected) data cannot be generated** — retrieve the
   payload from the cluster's object storage instead, or use saved testdata
   (e.g. `cmd/robne/testdata/hcp-capacity/` for the real HCP leg).

## Procedure

0. **Align versions.** The verdict is only meaningful when robne and the
   deployed server share engine code: compare `robne version` with the
   deployed image tag (`oc get deploy cost-onprem-ros-processor -n cost-onprem
   -o jsonpath='{.spec.template.spec.containers[0].image}'`); if the delta
   touches `librobne` or the recommend path, `make robne` (or rebuild/redeploy
   the server) first. Match the cluster's ACCESS.md to your **current oc
   context** — several lab ACCESS.md files name the same API host.
1. **Pick payload + cluster.** One robne input per cluster (one-cluster rule).
   Payload dirs carry the cluster id in their CSV filenames.
2. **Pin the clock.** Query the server's stamp, then pass it to robne so
   decay/staleness match (second granularity; microseconds are dropped):
   `SELECT MAX(updated_at) FROM <table> WHERE cluster_uuid='<uuid>';`
   → `robne recommend --input <dir> --plugins <p> --format json --no-user-config --now <stamp>`.
   Stamp tables: container `recommendation_sets`, node `node_recommendations`, namespace
   `namespace_recommendation_sets`, gpu-mig `gpu_mig_recommendation_sets`,
   gpu-timeslicing `node_gpu_timeslicing_recommendations`, vm
   `vm_recommendations`, pvc `pvc_recommendation_sets`, cluster-quota
   `cluster_quota_recommendation_sets`, snapshot `snapshot_recommendation_sets`,
   hcp `hcp_capacity_history` (stamp column `observed_at`, not `updated_at`). DB access:
   `oc exec -n cost-onprem cost-onprem-database-0 -- env PGPASSWORD="$ROS_PASS" psql -U "$ROS_USER" -d costonprem_ros -t -c "…"`
   with `ROS_USER`/`ROS_PASS` from secret `cost-onprem-db-credentials`
   (keys `ros-user`/`ros-password`).
3. **Fetch server rows** through the gateway (Bearer JWT + identity — the
   JWT lives ~300 s, refresh between batches; the cluster cert is
   self-signed and filters contain brackets, so always `curl -skg`).
   For the container family and PVC, fetch **per term** and merge — the
   default projection serves a subset:
   `filter[term]=short_term|medium_term|long_term`. Container-family
   fetches must pin `memory-unit=MiB` (the server's byte default produces
   exact-2^20-ratio garbage in the comparator). The container list route
   is the base `/recommendations/openshift` (or `/container` — singular,
   like `/vm`; the plural forms fall into detail routes). The VM list
   caps `limit` at 100 — paginate for larger sets. For the HCP real-data
   leg, the testdata manifest carries a placeholder cluster id — identify
   the real cluster empirically from `hcp_capacity_history`.
4. **Compare**: `python3 compare_parity.py --entity <e> --robne <r.json> --server <s1.json> [s2.json …]`.
   Entities: container, namespace, node, gpu-mig, gpu-timeslicing, vm, pvc,
   cluster-quota, snapshot, hcp.
5. **Interpret.** Exit 0 = parity. Diffs/robne-only keys = real divergence
   (confirm via explicit term filters or the DB, then file/update the tracker —
   #705 family). "Unserved variants" = the API projection lacks them (non-fatal;
   refetch per-term). HCP server-only observations are benign (history grows).
6. **Manual injection** (PVC/CRQ only): generate with nise, then register a
   source and upload via the chart helpers —
   `tests/e2e_helpers.py: register_source`, `utils.create_upload_package_from_files`,
   `conftest.obtain_jwt_token`, gateway `/api/ingress/v1/upload`. Use a fresh
   `uuid.uuid4()` cluster id — the processor validates `Cluster_uuid` as UUID
   and poisons anything else.
7. **Restore credentials after any chart pytest run** — the session-scoped lab
   user sync resets `admin`/`viewer` passwords (see the cluster ACCESS.md).

## Common Mistakes (all hit on the first run)

| Trap | Fix |
|---|---|
| `curl` silently empty on `filter[cluster_uuid]` | URL globbing — always `curl -g` |
| Port-forward to ros-api 404s | Container listens on 8000; prefer the gateway route (which also strips `/api`) |
| Gateway 401 with identity only | Bearer JWT (client-credentials, `cost-management-operator`) **and** `x-rh-identity` |
| e2e flows silently deselected | They carry no `ros` marker: `--extended` **without** `--ros` |
| Uploads produce no server recs | Non-UUID cluster id → Kafka poison; use `str(uuid.uuid4())` |
| Row-shape misread as divergence | Server nests term/engine and rounds memory to MiB; the comparator normalizes — use it, don't eyeball |
| VM list endpoint 400 "bad recommendation_id" | List route is `/vm` (singular); `/vms/…` is the history route |
| Container list 400 the same way | Container list is the base route or `/container` (singular) |
| Container memory diffs in exact 2^20 ratios | Server served bytes — pin `memory-unit=MiB` on container-family fetches |
| robne hcp stdout not pure JSON | The hcp path leaks log lines before the envelope — strip them (robne bug, tracked) |
| sshuttle dies but the hypervisor answers | SSH `-L` port-forwards through the hypervisor + a kubeconfig pointing at localhost is a viable reconnection; if the hypervisor is unreachable too, stop and ask |
| Payload files vanish from `/tmp` | Keep scratch under `.superpowers/tmp/` |

## Interrupted Runs

Cluster unreachability mid-run (VPN/sshuttle drop, hypervisor gone)
manifests as **silent poll timeouts and hung agents** — recognize it
early (`oc whoami` timing out, TCP to the cluster VIP dead), stop, and
ask for reconnection; do not retry around a network-level block.

What survives an interruption: the payloads on disk, and — for anything
already injected — the server's computed recommendations. The expensive
phases are done; do not redo them.

Resume protocol:

1. **Check connectivity first** (`oc whoami`; TCP to the API VIP).
2. **Verify what landed** per payload cluster: the clock-pinning stamp
   query doubles as the landed-check — a stamp means recs exist.
3. **Never re-generate or re-inject** a cluster that landed; reuse its
   payload dir and cluster id from the scratch records.
4. **Resume from fetch/compare** (stamps → robne → fetch → compare).
5. As you work, **persist the resume state** — cluster ids, stamps,
   injection results — in the scratch dir; it is the hand-off artifact.

A hung agent (wedged in a call through the dead tunnel) will not
recover on its own once connectivity returns; dispatch a fresh agent
with a pointer to the scratch state and this protocol rather than
waiting it out.

## Reporting

Record the run on the tracker issue (#706 pattern): method, per-entity verdicts
(field counts), divergences with payload/cluster/stamp identifiers. The 10/10
green run is the acceptance gate for #705.
