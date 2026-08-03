# ADR-0332: Thin cross-plane causality (W2) — hosted API pain vs management CP

## Status

Accepted (design) — **implementation coding deferred** until explicit W2 greenlight

## Phase

HCP / fleet FinOps (research R3 complete)

## Context

When a hosted OpenShift cluster feels slow, operators often add worker nodes.
That is frequently wrong if the bottleneck is the **shared control plane** running
on a **management** cluster (HyperShift / ROSA HCP / ARO HCP).

Lab + product intent:

- Join identity `HostedCluster.spec.clusterID` ↔ hosted ClusterVersion / ROS
  `cluster_uuid` is confirmed (#401).
- Prometheus runs on **both** management and hosted lab clusters; a naive
  `apiserver_request_duration_seconds` p99 query **returns data** on the hosted
  side (value needs careful PromQL — see below).
- Red Hat does **not** yet collect Cost Management metrics on ROSA/ARO
  management clusters but **will** — this feature family exists to optimize
  that management plane when collection exists.
- Audience rules (ADR-0330): RH-internal full causality; customer gets advisory
  only (never “resize RH etcd”).

R3 scope is **thin**: one join story. Deferred: webhook/API tax, zombie HC,
fleet admission headroom, noisy-neighbor CP moves (still real problems; later).

## Decision

### Go / no-go for W2

| Verdict | Meaning |
|---------|---------|
| **GO (with caveats)** for W2 design | Build thin correlator when management + hosted sources exist for the same hosted cluster id |
| **GO** customer path | Advisory only per ADR-0330 (“don’t add workers first / contact provider”) |
| **NO-GO** | Customer CTA to resize Red Hat–managed control plane; assertive CP-blame without management evidence |
| **Caveats** | Requires new SLO series in metrics operator (not in today’s ROS container CSVs); refined PromQL; W0 topology recommended first so we know the cluster is hosted |

W0+W1 remain independently shippable without W2.

### Join key

**Primary:** `HostedCluster.spec.clusterID` (management) = hosted
`ClusterVersion.spec.clusterID` / ROS `cluster_uuid`.

**Fallback:** `infraID` / `infrastructureName` (weaker; rename/recreate risk).

**Failure modes:** HC recreate changes id; missing management source (customer
hosted-only); clock skew across sources; multi-HC density needs per-HC
namespace scoping on management metrics.

### Minimum metrics (thin MVP)

**Hosted (customer or same-org collector):**

| Signal | Candidate series | Role |
|--------|------------------|------|
| API slow | `apiserver_request_duration_seconds` (histogram) — **exclude WATCH**; prefer mutating verbs or short GETs | Primary pain |
| Workers not saturated | Existing node/pod CPU-memory pressure from ROS digests | Negative control |

**Management (RH or self-managed CP owner):**

| Signal | Candidate series | Role |
|--------|------------------|------|
| Per-HC API stress | `apiserver_request_duration_seconds` and/or CPU usage for pods in that HC’s control-plane namespace (`hosted-control-plane` ns) | CP stress |
| etcd stress (optional v1.1) | `etcd_disk_wal_fsync_duration_seconds`, DB size growth | Strengthens confidence |
| Konnectivity errors (optional) | konnectivity proxy/server error rates | Alt explanation |

**PromQL hygiene (required):** A bare
`histogram_quantile(0.99, sum(rate(apiserver_request_duration_seconds_bucket[5m])) by (le))`
can return huge values (lab saw `60`) because long-lived **WATCH** and mixed
verbs dominate. Implementation must filter (e.g. `verb!~"WATCH|CONNECT|PROXY"`)
and document unit tests against known-good dashboards. Availability of the
metric name matters more for R3 than the raw lab scalar.

**Not required for thin W2:** HyperShift metrics-forwarding into the guest
(optional; helpful for guest-only dashboards, not our dual-collector model).

### Thin algorithm (precision-first)

```text
join management_source and hosted_source on clusterID
inputs over window W (e.g. 1h):
  H = hosted API latency high (refined p99 vs baseline or absolute threshold TBD at impl)
  C = management CP stress high for that HC namespace (latency and/or CPU)
  N = hosted worker / node pressure high (existing ROS signals)

if H and C and not N:
  confidence = high
  emit advisory: do not add worker nodes first; investigate control plane
    (RH-internal: may deep-link to W1 CP rightsizing / runbooks)
    (customer: contact provider / case — no RH CP resize CTA)
  optionally suppress or de-prioritize “add nodes” style advice for this cluster
elif H and N:
  no CP blame — keep normal node/workload recommendations
elif H and not C:
  no CP blame — look at DNS, webhooks, apps (later families)
else:
  no emission
```

Confidence must be explicit. Prefer **suppressing bad worker-scale advice**
over aggressive auto-remediation.

### Where the correlator runs

| Environment | Who collects management metrics | Who collects hosted metrics | Where correlator runs |
|-------------|----------------------------------|----------------------------|------------------------|
| Self-managed dual-plane | Customer | Customer | Same org / same robne deployment that sees both sources |
| ROSA/ARO HCP (future RH collection) | Red Hat on management | Customer on hosted | Red Hat side (or RH-assisted export of sanitized advisory to customer) per ADR-0330 |

### Operator gaps (for later coding, not W1)

- Export hosted API latency SLO samples (or digest rollups) into ROS pipeline.
- Export per-HC management API/etcd stress (or reuse container digests + new SLO CSV).
- Manifest / HC inventory already needed for W0 helps join.

## Alternatives Considered

### Hosted-only CP blame

Rejected: high false-positive rate (ADR research premise).

### Wait for HyperShift metrics forwarding only

Rejected as sole path: RH will collect on management; dual-collector model matches product intent. Forwarding remains optional enhancement.

### Full causality (webhooks, DNS, noisy neighbor) in v1

Rejected for thin MVP; tracked as later research (R4–R6 / wedges).

## Consequences

- W2 backlog #404 may be promoted after coding greenlight; still no code now.
- ADR issue #400 satisfied by this record for thin MVP metrics + join.
- Impl must invest in PromQL correctness tests (WATCH exclusion).
- Product copy templates must branch RH-internal vs customer (ADR-0330).

## Related Decisions

- [ADR-0328](0328-hcp-cluster-topology-detection-w0.md) — topology (know hosted)
- [ADR-0330](0330-hcp-audience-visibility-rh-vs-customer.md) — who sees what
- [ADR-0331](0331-management-cp-rightsizing-filters-and-guardrails.md) — W1 separate from SLO join
- Planned feature + `docs/plans/hcp-fleet-optimization.md`
- Research #387, wedge #404, epic #384

## References

- Lab: Prometheus Available on management + hosted; hosted API duration query returns data
- Product: RH will run Cost Management collection on ROSA/ARO management clusters
- HyperShift control-plane metrics forwarding (optional guest path): hypershift docs / OCPSTRAT-1852
