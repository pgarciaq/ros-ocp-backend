# ADR-0333: Unused HostedCluster (“zombie”) FinOps — W3

## Status

Accepted (design) — **implementation coding deferred** until explicit W3 greenlight

## Phase

HCP / fleet FinOps (research R4 complete)

## Context (plain English)

A **HostedCluster** always keeps a **control plane running on the management cluster**, even if nobody is using the hosted cluster’s worker nodes.

That is expensive: idle apps on the hosted side do **not** turn off the management-side control plane.

We want recommendations like: “This hosted cluster looks unused for N days, but its control plane is still running — review whether to delete it (or use a platform sleep/destroy workflow).”

Lab: one active HostedCluster `kubevirt-demo` (Available; workers present). Not a zombie example, but enough to list real signals from the HostedCluster object and HyperShift docs.

## Decision

### Go / no-go for W3

| Verdict | Meaning |
|---------|---------|
| **GO (with caveats)** | Build **advisory** “unused hosted cluster still costing control plane” when we can see management HostedCluster inventory + hosted (or management-inferred) idle usage |
| **NO-GO** | Recommending HyperShift `pausedUntil` as a **cost-saving** action — it only pauses **controllers updating the object**, it does **not** shut down the control plane |
| **NO-GO (for now)** | Claiming a universal “hibernate” button for ROSA HCP — **ROSA HCP has no native hibernate**; ROSA classic hibernate is a separate/limited story; some tools “sleep” by **destroying and later recreating** the cluster |

### What “unused” means (signals)

Need **both** sides of the story when possible:

**A — Hosted cluster looks idle** (any of these, over a configurable window e.g. 7–14 days):

| Signal | Source | Notes |
|--------|--------|-------|
| Very low worker / pod CPU & memory | Existing ROS digests on **hosted** source | Primary |
| Few or no non-system workloads | Hosted digests / namespace filters | Avoid counting platform pods as “busy” |
| NodePool scaled to 0 or tiny | Management `NodePool` (optional later) | Workers off ≠ control plane off |

**B — Control plane still “on” and costing** (management):

| Signal | Source | Notes |
|--------|--------|-------|
| HostedCluster exists and is Available | `HostedCluster` status `Available=True`; annotation `hypershift.openshift.io/HasBeenAvailable` | Lab has both |
| Control-plane pods still running / requesting CPU-memory | Digests in that HC’s control-plane namespace (needs operator collect — #405) | Strengthens $ story |
| Age | `metadata.creationTimestamp` | Optional: don’t nag brand-new clusters |

**Join:** same as elsewhere — `HostedCluster.spec.clusterID` ↔ hosted cluster id.

**If we only have management** (RH-operated): can still say “HC exists + CP pods busy/idle” but **hosted idle** is weaker without customer hosted digests — prefer dual evidence for high confidence.

**If we only have hosted** (customer, no management robne): can say “cluster looks idle” but **must not invent** “delete the Red Hat control plane” — soft “review whether you still need this cluster” only (ADR-0330).

### HyperShift / platform actions (important)

| Mechanism | What it actually does | Cost save? | Our product copy |
|-----------|----------------------|------------|------------------|
| `spec.pausedUntil` on HostedCluster / NodePool | Stops HyperShift **reconciling** (ops/debug/backup) | **No** — CP keeps running | Never recommend for FinOps |
| Scale NodePool to 0 / autoscaler down | Stops **worker** machines | Saves **worker** $ only | OK as secondary tip; say CP still runs |
| Delete HostedCluster | Tears down CP + related resources | **Yes** (main real save) | Primary recommendation for true zombies |
| ROSA HCP “sleep” patterns (external tools) | Often **destroy cluster**, keep IAM/DNS, recreate later | Yes, but disruptive | Mention as platform-specific ops, not pretend native hibernate |
| ROSA **classic** hibernate (tech preview) | Not HCP | N/A for HCP path | Out of W3 HCP scope |

Ask HyperShift/ACM (#398) before coding: any future official sleep/hibernate for HCP we should detect?

### Thin algorithm (advisory)

```text
join HC + hosted_cluster on clusterID (when both exist)
idle = hosted usage below threshold for N days
       (and/or NodePools at 0 for N days)
cp_on = HostedCluster Available (and preferably CP pods still present)

if idle and cp_on and cluster older than grace period:
  emit advisory:
    "Hosted cluster looks unused; control plane still running on management.
     Review delete (or your platform’s destroy/recreate sleep workflow).
     Scaling workers to zero does not stop control-plane cost.
     Do not use pausedUntil to save money."
  confidence = high if both planes; medium if one plane only
else:
  no emission
```

Never auto-delete. Never customer CTA to resize RH shared CP.

### Who sees what

| Audience | Message |
|----------|---------|
| Self-managed / RH-internal with management view | Full: name the HC, estimate CP waste if digests exist, suggest delete/review |
| Customer hosted-only | Soft: cluster looks idle — confirm you still need it; no RH CP internals |

## Alternatives Considered

### Treat pausedUntil as hibernate

Rejected — wrong and harmful advice.

### Wait for native HCP hibernate

Rejected as a blocker — delete/review advisory is valuable today.

### Require perfect $ chargeback first

Rejected — W3 can ship as usage/idle advisory; dollar attribution is Family H / later.

## Consequences

- W3 backlog #391 may be promoted after coding greenlight; still no code now.
- Operator may need HostedCluster / NodePool inventory facts (related to W0 topology emission).
- #405 helps dollar/CP presence proof but idle+Available is enough for a first advisory.
- Family J (business-hours sleep) stays separate: no reliable native HCP hibernate yet.

## Related Decisions

- [ADR-0328](0328-hcp-cluster-topology-detection-w0.md) — know management vs hosted
- [ADR-0330](0330-hcp-audience-visibility-rh-vs-customer.md) — copy branching
- [ADR-0332](0332-thin-cross-plane-causality-w2.md) — join key
- Planned feature Family D; research #388; wedge #391; outreach #398

## References

- Lab `mgmt-hc.yaml`: HostedCluster `kubevirt-demo`, `Available`, `HasBeenAvailable=true`, no `pausedUntil`
- HyperShift docs: [Pause reconciliation](https://hypershift-docs.netlify.app/how-to/pause-reconciliation/) — not cost off
- HyperShift API: `pausedUntil` on HostedCluster / NodePool
- ROSA classic hibernate TP (not HCP); ROSA HCP sleep often = destroy/recreate patterns
