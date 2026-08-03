# ADR-0329: Auto-include HyperShift HCP namespaces in ROS queries

## Status

Accepted

## Phase

HCP / fleet FinOps (pre-implementation) — **koku-metrics-operator** change

## Context

W1 (management CP rightsizing) needs ROS container digests for pods in Hosted
Control Plane namespaces (e.g. `clusters-<hc-name>`).

Today ROS PromQL requires namespace label `cost_management_optimizations=true`
(and historically excludes `kube-*` / `openshift*`). Lab default HCP namespaces
carry `hypershift.openshift.io/hosted-control-plane=true` but **not** the
optimizations label unless manually applied.

Management Prometheus already exposes CP container request metrics (etcd,
kube-apiserver, …). Without collection coverage, W1 cannot ingest.

Manual labeling every HCP namespace does not scale for fleets.

## Decision

**Option B:** koku-metrics-operator ROS queries **auto-include** namespaces with:

`hypershift.openshift.io/hosted-control-plane=true`

in addition to (not instead of) the existing optimizations-label path.

Optional later: opt-out label if a customer must exclude a specific HCP ns.

Do **not** rely on manual `cost_management_optimizations` alone for HCP ns
(though labeling remains valid and compatible).

## Alternatives Considered

### A — Runbook only (manual label each HCP ns)

Works for labs; fails at fleet scale; easy to forget on new HostedClusters.

### C — Collect all non-openshift namespaces on management

Too broad; pulls unrelated management workloads into ROS without intent.

### Change only robne filters, assume cost CSVs

Rejected: robne W1 path is ROS digests, not cost `ocp_pod_usage` alone.

## Consequences

- Operator query changes in `internal/collector/queries.go` (and tests/docs).
- Document in operator CSV / ROS docs that HCP ns are in-scope on management.
- W1 greenlight still needs an environment with operator installed on management
  after this ships (lab currently has no operator — research closed via Prom proof).
- Tracking: #405

## Related Decisions

- [ADR-0328](0328-hcp-cluster-topology-detection-w0.md): Topology (W0).
- [ADR-0331](0331-management-cp-rightsizing-filters-and-guardrails.md): W1 filters/guardrails.
- Epic #384, R2 #386, lab gate #401 (closed)

## References

- Lab: Prom count-by-container in `clusters-kubevirt-demo` (CP components present)
- HyperShift ns label: `hypershift.openshift.io/hosted-control-plane=true`
