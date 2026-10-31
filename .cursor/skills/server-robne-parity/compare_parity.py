#!/usr/bin/env python3
"""Field-mapped parity diff: robne CLI JSON vs ros-ocp-backend API responses.

Part of the server-robne-parity skill (#706). Compares one entity type per
invocation and exits 1 on any real divergence (shape-only differences are
normalized, display-rounding tolerances applied).

Usage:
    compare_parity.py --entity vm --robne mig.json --server server-vm.json

Inputs:
    --robne   robne `recommend --format json` output (envelope with sibling
              arrays, e.g. vm_recommendations).
    --server  server API list response ({"meta": ..., "data": [...]}), fetched
              with filter[cluster_uuid] for the same payload's cluster.

Row-shape contract encoded here (learned the hard way — see SKILL.md):
    - The server nests per-term/per-engine variants and projects one row per
      entity; robne flattens to one row per (entity, term, engine).
    - Server terms are short_term/medium_term/long_term; robne uses short/
      medium/long (container family) or short_term/... (VM) — normalized.
    - Units: server CPU cores -> robne millicores (x1000); server memory
      MiB -> robne KiB (x1024) for the container family, GiB -> KiB (x1048576)
      for nodes/VMs; memory display rounding to whole MiB tolerated (+/- 1 MiB).
    - cluster-quota namespaces: server array vs robne comma-joined string.
"""

import argparse
import json
import sys

# robne term -> server term
TERM = {"short": "short_term", "medium": "medium_term", "long": "long_term"}
MEM_ROUNDING_TOL = 1024 + 2  # server rounds container-family memory to whole MiB


def load(path):
    with open(path, encoding="utf-8") as h:
        return json.load(h)


def load_server(paths):
    """Load one or more server response files, merging their data arrays.

    Use this to combine per-term fetches (filter[term]=...) into one comparison
    input when the list API's default projection serves only a subset of terms.
    """
    docs = [load(p) for p in paths]
    if len(docs) == 1:
        return docs[0]
    merged = {"meta": docs[0].get("meta", {}), "data": []}
    for d in docs:
        merged["data"].extend(d.get("data", []))
    return merged


def server_rows(doc):
    return doc["data"] if isinstance(doc, dict) and "data" in doc else doc


def robne_rows(doc, key):
    if not isinstance(doc, dict) or key not in doc or not isinstance(doc[key], list):
        sys.exit(f"robne input has no {key!r} sibling array (envelope keys: {list(doc) if isinstance(doc, dict) else type(doc)})")
    return doc[key]


def norm_term(t):
    return TERM.get(t, t)


def money_ok(a, b, tol):
    return a is not None and b is not None and abs(a - b) <= tol


class Report:
    def __init__(self, entity):
        self.entity = entity
        self.ok = 0
        self.diffs = []
        self.only_server = []
        self.only_robne = []
        self.missing_variants = []

    def good(self):
        self.ok += 1

    def diff(self, key, field, a, b):
        self.diffs.append(f"  DIFF {key} {field}: server={a!r} robne={b!r}")

    def verdict(self):
        lines = [f"{self.entity}: {self.ok} fields identical, {len(self.diffs)} diffs,"
                 f" {len(self.only_server)} server-only / {len(self.only_robne)} robne-only keys,"
                 f" {len(self.missing_variants)} unserved variants (see below, non-fatal)"]
        lines += self.diffs + self.missing_variants
        for k in self.only_server:
            lines.append(f"  SERVER-ONLY {k}")
        for k in self.only_robne:
            lines.append(f"  ROBNE-ONLY {k}")
        print("\n".join(lines))
        return 1 if (self.diffs or self.only_server or self.only_robne) else 0


# ---------------------------------------------------------------- container family

def variant_config(row, term, engine):
    """Server container/namespace nested variant, or None."""
    try:
        return row["recommendations"]["recommendation_terms"][norm_term(term)]["recommendation_engines"][engine]["config"]
    except (KeyError, TypeError):
        return None


def cmp_container_family(rep, entity, robne, server, skey, ckey):
    # Multiple server rows per entity (per-term fetches merge one row per
    # term): aggregate, then search all of them for each requested variant.
    S = {}
    for r in server:
        S.setdefault(skey(r), []).append(r)
    C = {}
    for r in robne:
        C.setdefault(ckey(r), []).append(r)
    rep.only_server = [k for k in S if k not in C]
    rep.only_robne = [k for k in C if k not in S]
    for k in sorted(set(S) & set(C), key=str):
        for cr in C[k]:
            v = None
            for srow in S[k]:
                v = variant_config(srow, cr["term"], cr["engine"])
                if v is not None:
                    break
            tag = f"{cr['term']}/{cr['engine']}"
            if v is None:
                # Not fatal: the container list API serves a default projection;
                # a genuine term-coverage gap is a #705-family finding the
                # operator confirms via explicit term filters or the DB.
                rep.missing_variants.append(f"  MISSING server variant {k} {tag}")
                continue
            pairs = [
                ("cpu_req", cr.get("rec_cpu_request_mc"), v["requests"]["cpu"]["amount"] * 1000, 1.5),
                ("mem_req", cr.get("rec_mem_request_kib"), v["requests"]["memory"]["amount"] * 1024, MEM_ROUNDING_TOL),
                ("cpu_lim", cr.get("rec_cpu_limit_mc"), v["limits"]["cpu"]["amount"] * 1000, 1.5),
                ("mem_lim", cr.get("rec_mem_limit_kib"), v["limits"]["memory"]["amount"] * 1024, MEM_ROUNDING_TOL),
            ]
            for name, a, b, tol in pairs:
                if a is None:
                    continue
                if money_ok(b, a, tol):
                    rep.good()
                else:
                    rep.diff(k, f"{tag} {name}", b, a)


# ---------------------------------------------------------------- flat-row entities

def cmp_flat(rep, robne, server, skey, ckey, fields, tol=0):
    """Both sides flat rows; compare named fields with exact-or-tolerance match."""
    S = {skey(r): r for r in server}
    C = {ckey(r): r for r in robne}
    rep.only_server = [k for k in S if k not in C]
    rep.only_robne = [k for k in C if k not in S]
    for k in sorted(set(S) & set(C), key=str):
        for field, sname, cname in fields:
            a, b = S[k].get(sname), C[k].get(cname)
            if a == b or (isinstance(a, (int, float)) and isinstance(b, (int, float)) and money_ok(a, b, tol)):
                rep.good()
            else:
                rep.diff(k, field, a, b)


ENTITY = {
    "container": dict(
        robne_key="container_recommendations",
        skey=lambda r: (r["project"], r["workload"], r["container"]),
        ckey=lambda r: (r["namespace"], r["workload"], r["container_name"]),
        kind="nested",
    ),
    "namespace": dict(
        robne_key="namespace_recommendations",
        skey=lambda r: r["project"],
        ckey=lambda r: r["namespace"],
        kind="nested",
    ),
    "node": dict(
        robne_key="node_recommendations",
        kind="node",
    ),
    "gpu-mig": dict(
        robne_key="gpu_recommendations",
        kind="flat",
        skey=lambda r: (r["namespace"], r["workload"], r["container"], r["term"]),
        ckey=lambda r: (r["namespace"], r["workload"], r["container_name"], r["term"]),
        fields=[("classification", "gpu_classification", "classification"),
                ("recommended_profile", "recommended_gpu_profile", "recommended_gpu_profile"),
                ("current_profile", "current_gpu_profile", "current_gpu_profile"),
                ("gpu_model_name", "gpu_model", "gpu_model_name")],
    ),
    "gpu-timeslicing": dict(
        robne_key="gpu_timeslicing_recommendations",
        kind="flat",
        skey=lambda r: (r["node_name"], r["gpu_model"], r["term"]),
        ckey=lambda r: (r["node"], r["gpu_model"], r["term"]),
        fields=[("recommended_replicas", "recommended_replicas", "recommended_replicas")],
    ),
    "vm": dict(
        robne_key="vm_recommendations",
        kind="vm",
    ),
    "pvc": dict(
        robne_key="pvc_recommendations",
        kind="flat",
        skey=lambda r: (r["namespace"], r["persistentvolumeclaim"], r["term"]),
        ckey=lambda r: (r["namespace"], r["pvc"], r["term"]),
        fields=[("recommendation_type", "recommendation_type", "recommendation_type"),
                ("recommended_bytes", "recommended_bytes", "recommended_bytes"),
                ("usage_bytes_max", "usage_bytes_max", "usage_bytes_max")],
    ),
    "cluster-quota": dict(
        robne_key="cluster_quota_recommendations",
        kind="crq",
    ),
    "snapshot": dict(
        robne_key="snapshot_recommendations",
        kind="flat",
        skey=lambda r: r["snapshot_name"],
        ckey=lambda r: r["snapshot_name"],
        fields=[("recommendation_type", "recommendation_type", "recommendation_type"),
                ("age_days", "age_days", "age_days"),
                ("restore_size_bytes", "restore_size_bytes", "restore_size_bytes"),
                ("source_pvc_exists", "source_pvc_exists", "source_pvc_exists"),
                ("restored_pvc_count", "restored_pvc_count", "restored_pvc_count")],
    ),
    "hcp": dict(
        robne_key="hcp_recommendations",
        kind="hcp",
    ),
    "hcp-advisory": dict(
        robne_key="hcp_advisories",
        kind="hcp_advisory",
    ),
}

CRQ_MAP = [
    ("cpu_request_millicores", "cpu_request_hard_mc", "cpu_request_recommended_mc"),
    ("cpu_limit_millicores", "cpu_limit_hard_mc", "cpu_limit_recommended_mc"),
    ("memory_request_bytes", "memory_request_hard_bytes", "memory_request_recommended_bytes"),
    ("memory_limit_bytes", "memory_limit_hard_bytes", "memory_limit_recommended_bytes"),
    ("storage_request_bytes", "storage_request_hard_bytes", "storage_request_recommended_bytes"),
    ("pods", "pods_hard", "pods_recommended"),
]


def cmp_hcp_advisory(rep, robne, server):
    """Compare HCP advisories (one per hosted cluster per window). The
    robne hcp_advisories sibling and the server GET .../hcp/advisories
    response share the #717 contract: verdict, confidence, window, typed
    details, evidence. Compared per hosted_cluster_id."""
    srows = {r["hosted_cluster_id"]: r for r in server}
    for c in robne:
        k = c.get("hosted_cluster_id", "?")
        s = srows.pop(k, None)
        if s is None:
            rep.diff(k, "server", "MISSING", "present in robne only")
            continue
        if c.get("verdict") != s.get("verdict"):
            rep.diff(k, "verdict", c.get("verdict"), s.get("verdict"))
        if c.get("confidence") != s.get("confidence"):
            rep.diff(k, "confidence", c.get("confidence"), s.get("confidence"))
        cd = c.get("details") or {}
        sd = s.get("details") or {}
        # Key-union comparison: per-verdict detail fields differ across the
        # advisory family (sleep #721, zombie #722, headroom #723, api-tax
        # #724) — compare every key either side carries, so a missing or
        # extra field on either side is a diff, not a silent pass.
        fields = sorted(set(cd) | set(sd))
        for f in fields:
            rep.diff(k, f"details.{f}", cd.get(f), sd.get(f))
    for k in srows:
        rep.diff(k, "robne", "MISSING", "present in server only")


def compare(entity, robne_doc, server_doc):
    rep = Report(entity)
    cfg = ENTITY[entity]
    robne = robne_rows(robne_doc, cfg["robne_key"])
    server = server_rows(server_doc)
    kind = cfg["kind"]

    if kind == "nested":
        cmp_container_family(rep, entity, robne, server, cfg["skey"], cfg["ckey"])
    elif kind == "flat":
        cmp_flat(rep, robne, server, cfg["skey"], cfg["ckey"], cfg["fields"])
    elif kind == "node":
        # Aggregate server rows per node: per-term fetches produce one row per
        # (node, term), so search all rows for the requested term's block.
        S = {}
        for r in server:
            S.setdefault(r["node"], []).append(r)
        C = {}
        for r in robne:
            C.setdefault(r["node"], []).append(r)
        rep.only_server = [k for k in S if k not in C]
        rep.only_robne = [k for k in C if k not in S]
        for k in sorted(set(S) & set(C), key=str):
            for cr in C[k]:
                block = None
                for srow in S[k]:
                    block = srow["recommendation_terms"].get(norm_term(cr["term"]))
                    if block is not None:
                        break
                if block is None:
                    rep.diff(k, f"{cr['term']}/{cr['engine']}", "variant present", "MISSING server-side")
                    continue
                eng = block["recommendation_engines"][cr["engine"]]
                tag = f"{cr['term']}/{cr['engine']}"
                for name, a, b in [("cpu", eng["recommended_cpu_cores"] * 1000, cr["recommended_cpu_mc"]),
                                   ("mem", eng["recommended_memory_gib"] * 1048576, cr["recommended_mem_kib"])]:
                    if money_ok(a, b, 1.5):
                        rep.good()
                    else:
                        rep.diff(k, f"{tag} {name}", a, b)
                if eng.get("node_count_reduction") == cr.get("node_count_reduction"):
                    rep.good()
                else:
                    rep.diff(k, f"{tag} node_count_reduction", eng.get("node_count_reduction"), cr.get("node_count_reduction"))
    elif kind == "vm":
        S = {}
        for r in server:
            S[(r["vm_name"], r["metadata"]["term"], r["metadata"]["engine"])] = r
        C = {(r["vm_name"], norm_term(r["term"]), r["engine"]): r for r in robne}
        rep.only_server = [k for k in S if k not in C]
        rep.only_robne = [k for k in C if k not in S]
        for k in sorted(set(S) & set(C), key=str):
            s, c = S[k], C[k]
            for name, a, b in [("category", s["category"], c["category"]),
                               ("vcpu", s["recommended"]["vcpu"], c["recommended_vcpu"]),
                               ("memory_gib", s["recommended"]["memory_gib"], c["recommended_memory_gib"])]:
                if a == b:
                    rep.good()
                else:
                    rep.diff(k, name, a, b)
    elif kind == "crq":
        S = {r["cluster_quota_name"]: r for r in server}
        C = {r["cluster_quota_name"]: r for r in robne}
        rep.only_server = [k for k in S if k not in C]
        rep.only_robne = [k for k in C if k not in S]
        for k in sorted(set(S) & set(C), key=str):
            s, c = S[k], C[k]
            for f in ("recommendation_type", "risk_level"):
                if s[f] == c[f]:
                    rep.good()
                else:
                    rep.diff(k, f, s[f], c[f])
            for skeyname, hkey, rkey in CRQ_MAP:
                for side, ckeyname in (("quota_hard", hkey), ("quota_recommended", rkey)):
                    a, b = s[side].get(skeyname), c.get(ckeyname)
                    if a == b:
                        rep.good()
                    else:
                        rep.diff(k, f"{side}.{skeyname}", a, b)
            ns_s = {n["namespace"] if isinstance(n, dict) else n for n in s.get("namespaces", [])}
            ns_c = set(filter(None, (c.get("namespaces") or "").split(",")))
            if ns_s == ns_c:
                rep.good()
            else:
                rep.diff(k, "namespaces", sorted(ns_s), sorted(ns_c))
    elif kind == "hcp_advisory":
        cmp_hcp_advisory(rep, robne, server)
    elif kind == "hcp":
        # Server-only observations are benign by construction: the live cluster
        # keeps accumulating history; only the payload's observations compare.
        S = {r["observed_at"]: r for r in server}
        C = {r["observed_at"]: r for r in robne}
        rep.only_server = []
        rep.only_robne = [k for k in C if k not in S]
        for k in sorted(set(S) & set(C), key=str):
            s, c = S[k], C[k]
            for f in ("current_hc_count", "ready_hcp_count", "inventory_complete",
                      "worker_placement_known", "native_model_version"):
                if s.get(f) == c.get(f):
                    rep.good()
                else:
                    rep.diff(k, f, s.get(f), c.get(f))
            for f in ("cpu_allocatable_millicores", "memory_allocatable_bytes"):
                if money_ok(s["eligible_workers"][f], c["eligible_workers"][f], 0.5):
                    rep.good()
                else:
                    rep.diff(k, f"eligible_workers.{f}", s["eligible_workers"][f], c["eligible_workers"][f])
            ss = {x["name"]: x for x in s["scenarios"]}
            for sc in c["scenarios"]:
                e = ss.get(sc["name"])
                if not e:
                    rep.diff(k, f"scenario {sc['name']}", "present", "MISSING server-side")
                    continue
                for f in ("max_hcs", "limiting_resource", "headroom", "status"):
                    if sc.get(f) == e.get(f):
                        rep.good()
                    else:
                        rep.diff(k, f"{sc['name']}.{f}", e.get(f), sc.get(f))
    return rep


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--entity", required=True, choices=sorted(ENTITY))
    ap.add_argument("--robne", required=True, help="robne recommend --format json output file")
    ap.add_argument("--server", required=True, nargs="+",
                    help="server API list-response JSON file(s); multiple files are merged (per-term fetches)")
    args = ap.parse_args()
    rep = compare(args.entity, load(args.robne), load_server(args.server))
    sys.exit(rep.verdict())


if __name__ == "__main__":
    main()
