# ADR-0331: Management CP rightsizing — filters and guardrails (W1)

## Status

Accepted (design) — implementation deferred until operator collection (#405 / ADR-0329) is available in target environments

## Phase

HCP / fleet FinOps (pre-implementation)

## Context

On a management cluster, HyperShift control-plane components are ordinary pods in
per-HC namespaces. Existing container engines can rightsizing them **if** digests
exist and filters avoid noise / unsafe downsizes.

Lab (R2): stable labels `hypershift.openshift.io/control-plane-component` and
namespace `hypershift.openshift.io/hosted-control-plane=true`. Noise includes
KubeVirt `virt-launcher-*` in the HCP ns, false `etcd` name matches outside HCP
ns, and kube-system helpers.

## Decision

### Ingest / filter

```text
INCLUDE IF
  namespace has hypershift.openshift.io/hosted-control-plane=true
  AND (
    pod has hypershift.openshift.io/control-plane-component
    OR pod has hypershift.openshift.io/control-plane=true
  )
EXCLUDE IF
  name prefix virt-launcher-
  OR known non-CP noise (document denylist as discovered)
```

Prefer **labels over** `{hc.namespace}-{hc.name}` regex (regex is fallback only).

Attribute recommendations to HC via `HostedCluster.spec.clusterID` / HCP namespace.

### Guardrails (posture)

- **Advisory-first**; stricter floors than generic app containers for:
  `etcd`, `kube-apiserver`, `kube-controller-manager`, `kube-scheduler`,
  `openshift-apiserver`, `openshift-oauth-apiserver`, `oauth-openshift`,
  konnectivity server/agent, `control-plane-operator`, `openshift-controller-manager`.
- Prefer gross over-request callouts; never imply aggressive etcd/kas downsize.
- At ship time, tag `recommendation_type` / plugin path as **controlplane**
  (detect via labels first; dedicated plugin label is product hygiene).

### Scope by audience

- **M1 / RH-internal M3:** W1 allowed with guardrails.
- **Customer ROSA (M2/M3 customer UI):** W1 on RH-managed CP **not** shown (ADR-0330).

### Metrics

W1 uses **existing ROS container digests** once ADR-0329 collection is in place.
New SLO series (API latency, etcd fsync) are **R3/W2**, not W1.

## Alternatives Considered

### Same thresholds as app containers

Rejected: unsafe for quorum/CP components.

### Separate PromQL-only CP plugin before digests

Unnecessary for W1 if ROS digests cover HCP ns; adds pipeline complexity.

### Block W1 until W2

Rejected: W0+W1 ship without W2 (MVP ladder).

## Consequences

- Depends on ADR-0329 operator work before production W1 value on management.
- Implementation issues (#403) stay design-ready; coding gated explicitly.
- Tracking: R2 #386, W1 #403, #405

## Related Decisions

- [ADR-0328](0328-hcp-cluster-topology-detection-w0.md)
- [ADR-0329](0329-ros-auto-include-hypershift-hcp-namespaces.md)
- [ADR-0330](0330-hcp-audience-visibility-rh-vs-customer.md)

## References

- Planned feature § R2 research findings
- Lab pod label inventory (`clusters-kubevirt-demo`)
