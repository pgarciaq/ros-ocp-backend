# ADR-0342: Explicit opt-in label wins over kube/openshift namespace exclusion

## Status

Accepted — implements [Issue #694](https://github.com/pgarciaq/ros-ocp-backend/issues/694)
(true management-overhead coverage, owner greenlight 2026-10-16).

## Phase

Collection (operator PromQL; cross-repo: `koku-metrics-operator`
`internal/collector/queries.go`).

## Context (plain English)

The operator only collects namespaces carrying an opt-in label
(`cost_management_optimizations`, `insights_...` variant, or the HCP label).
On top of that, every ROS query also excluded `kube-*`/`openshift`/`openshift-*`
names by pattern — including namespaces a human had deliberately labeled.
The exclusion therefore vetoed explicit opt-ins: labeling `openshift-monitoring`
changed nothing, silently. Unlabeled namespaces were never collected either way.

## Decision

An explicit opt-in label wins: drop the `namespace!~` matcher from
`kube_namespace_labels` selectors in ROS container queries. Every remaining
selector joins on a label match, so exactly the labeled namespaces flow —
unlabeled defaults are unchanged, and no unlabeled fleet-wide collection exists.

## Alternatives considered

- **Keep the exclusion (status quo):** rejected — makes the label a lie for
  precisely the platform namespaces management-overhead coverage needs.
- **Drop the label requirement too (collect everything):** rejected — data
  volume explosion plus recommendations on core platform components nobody
  asked to optimize.
- **Finer split (always-exclude `kube-*`, allow labeled `openshift-*`):**
  deferred — revisit if a labeled `kube-*` namespace ever needs coverage.

## Guidance (policy, not mechanism)

Label MCE/HyperShift-related and self-managed monitoring namespaces.
Advised against: the management cluster's own `kube-apiserver`/`etcd`/
`controller-manager` (control-plane-of-the-control-plane; SRE domain).
Quota queries keep their exclusions (separate surface, separate justification).

## Consequences

- Labeling a namespace is now sufficient *and* necessary for collection —
  no second gate hides behind it. Documented in the public workflow page.
- Rollout is behavior-neutral until namespaces are labeled (verified: only
  `ros-demo` carries the label on the lab).
- Reversible per namespace (remove the label); no schema or API change.
