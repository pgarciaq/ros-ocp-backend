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

## Update (2026-09-17, #583 — new-lab evidence; decision text above immutable)

- Live Agent lab (`hcp-mgmt` + `hc01`): 1,561 management ROS container rows, 100% in `hc01-infra-hc01`; 39 control-plane/operator workloads; zero `virt-launcher`, zero tenants.
- ROS CSVs carry no pod-label columns, so the label-based INCLUDE above is unimplementable on CSV data. Locked revision: namespace-membership as the filter (HCP ns provable via #406) + workload inventory as pinning/tripwire; `virt-launcher-` exclusion dropped as KubeVirt-only.
- 24 blank-owner rows (`cluster-image-registry-operator` / `apiserver-token-minter`, CSV join gap) still classify by namespace.
- Full evidence + locked rules: #583 design-lock comment. No new ADR (findings, not decisions).
- Locked floor values (W1.1 #584): relative 70% of window-median current request (median resists single-bucket redeploy drops; 1-row short window falls back to absolute) + absolute 100m CPU / 128MiB memory, `max` of both on cost and perf, uniform strict set. Replica recs suppressed for HCP groups (statefulsetMinReplicas=1 would let idle-etcd recs destroy quorum). CLI wired from payload manifest (Path 1); server path #590. Internal routing only; no API value yet.
