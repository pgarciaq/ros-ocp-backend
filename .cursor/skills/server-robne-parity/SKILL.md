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
- robne built (`make robne`); payloads on disk — either the chart E2E temp dirs
  (`/tmp/e2e-*`, one cluster each) or nise-generated.
- For PVC / cluster-quota legs: the E2E flows package only the CSVs they assert
  on — inject those payloads manually (fresh UUID cluster; see Procedure).

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
   Stamp tables: container/node `recommendation_sets`, namespace
   `namespace_recommendation_sets`, gpu-mig `gpu_mig_recommendation_sets`,
   gpu-timeslicing `node_gpu_timeslicing_recommendations`, vm
   `vm_recommendations`, pvc `pvc_recommendation_sets`, cluster-quota
   `cluster_quota_recommendation_sets`, snapshot `snapshot_recommendation_sets`,
   hcp `hcp_capacity_history`. DB access:
   `oc exec -n cost-onprem cost-onprem-database-0 -- env PGPASSWORD="$ROS_PASS" psql -U "$ROS_USER" -d costonprem_ros -t -c "…"`
   with `ROS_USER`/`ROS_PASS` from secret `cost-onprem-db-credentials`
   (keys `ros-user`/`ros-password`).
3. **Fetch server rows** through the gateway (Bearer JWT + identity; the
   cluster cert is self-signed and filters contain brackets, so always
   `curl -skg`). For the container family and PVC, fetch **per
   term** and merge — the default projection serves a subset:
   `filter[term]=short_term|medium_term|long_term`.
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
| Payload files vanish from `/tmp` | Keep scratch under `.superpowers/tmp/` |

## Reporting

Record the run on the tracker issue (#706 pattern): method, per-entity verdicts
(field counts), divergences with payload/cluster/stamp identifiers. The 10/10
green run is the acceptance gate for #705.
