# ADR-0344: Management-overhead namespace label fleet list (waves 1–2)

## Status

Accepted — owner-approved full list 2026-10-18; implements the collection
half of [Issue #694](https://github.com/pgarciaq/ros-ocp-backend/issues/694)
(wave-2 approval + documentation requirement; the list must live in public
docs and ADRs, never only in a lab notes file).

## Phase

Collection (cross-repo: `koku-metrics-operator` label-wins filter, per
[ADR-0342](0342-label-wins-over-namespace-exclusion.md)).

## Context (plain English)

True management-overhead coverage needs the management cluster's own
platform namespaces in the digests, but nothing collects unlabeled
namespaces and no default include list exists in the operator. The opt-in
label (`cost_management_optimizations`) must therefore be **applied per
namespace, per cluster**; documenting a list anywhere — including these
docs — changes nothing. This ADR records *which* namespaces were chosen
(and which were deliberately not), because that choice is a product
decision, not a lab artifact.

## Decision

Wave 1 (applied 2026-10-17/18) — HyperShift/MCE platform namespaces on the
management cluster:

- `multicluster-engine`
- `hive`
- `hypershift`
- `koku-metrics-operator` (operator release namespace)
- `ros-demo` (test workload)

Wave 2 (owner-approved 2026-10-18, applied the same day):

- `openshift-monitoring` — platform Prometheus/Thanos/Alertmanager
  ("self-managed monitoring" per the ADR-0342 guidance; the first
  `openshift-*` namespace to cross the exclusion **by label** — by design)
- `openshift-user-workload-monitoring` — carries per-hosted-cluster metering load
- `klusterlet-hc01`, `klusterlet-hc02`, `klusterlet-hc03` — per-hosted-cluster
  agent namespaces on the management cluster (klusterlet-hc03 expires with the
  hc03 zombie deletion; an inert label on a gone namespace is harmless)
- `open-cluster-management-agent`, `open-cluster-management-agent-addon`,
  `open-cluster-management-hub` — hosted-cluster support plumbing (owner addition)
- `local-cluster` — MCE's own hub-import namespace (owner addition)

**Excluded, deliberately (owner-approved):**

- `openshift-etcd`, `openshift-kube-apiserver` — the management cluster's own
  control plane (locked #694 decision; SRE domain, not resizer's advice).
- Remaining `openshift-*` platform/operator namespaces (~40) — no unlabeled
  fleet-wide collection; labeling them all would dilute the overhead view
  into a full platform view.
- Lab tooling namespaces (`kcli-infra`, `local-path-storage`).
- `hc0X-infra*` namespaces require **no label**: they are HCP-evidenced and
  admitted via the snapshot machinery (#621/#632).

## Rules

- Label-wins is the mechanism (ADR-0342); this ADR fixes the *policy* — the
  concrete list above is the promised content of that guidance.
- The list is **per-cluster**: a new management cluster must re-apply the same
  list; the docs intention does not propagate.
- Reversible per namespace: `oc label namespace <ns> cost_management_optimizations-`.
- The quota surface keeps its exclusions (untouched, per ADR-0342).

## Alternatives considered

- **Bake the list into the operator as defaults:** rejected — the whole point
  of #694 (and its owner decisions) is that collection stays opt-in per
  namespace; a compiled-in default is unlabeled fleet-wide collection in
  disguise.
- **Extend ADR-0342's guidance section with the concrete list:** rejected —
  0342 is the mechanism decision; the fleet list is a separate, revisable
  policy likely to change on lab/fleet evolution without touching the
  label-wins mechanism.
- **Label the entire platform (`openshift-*` wholesale):** rejected — turns
  the management-overhead view into the platform view; noise for consumers.

## Consequences

- The public configuration reference enumerates the same list (single source
  of truth duplicated by design: ADR = decision + rationale; config pages =
  instructions).
- New namespaces enter this list only by owner decision; edits update this
  ADR and the config page together.
- Verification that the labeled namespaces flow is a per-cycle lab check
  (overhead sub-tab rows per namespace), not a product guarantee until data
  confirms it.
