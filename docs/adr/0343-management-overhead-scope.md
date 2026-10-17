# ADR-0343: Management-overhead scope — topology class + not-HCP namespace

## Status

Accepted — implements [Issue #694](https://github.com/pgarciaq/ros-ocp-backend/issues/694)
(owner plan approval 2026-10-17). Serves the namespace side of
[ADR-0342](0342-label-wins-over-namespace-exclusion.md): what the operator
(collects labeled namespaces on the management cluster) flows, this scope
makes servable.

## Phase

Serving (`internal/model/recommendation_hcp.go`, cross-repo: `koku-ui`
overhead sub-tab query assembly).

## Context (plain English)

A management cluster runs two kinds of collected workloads: the control
planes of its hosted clusters (etcd, kube-apiserver, … — served by the
existing HCP surface, `hcp_namespaces` evidence) and its own management
overhead (multicluster-engine, hive, the operator itself, … — collected since
ADR-0342 but dropped by every serving surface, because the HCP scope
`hcpScopeFragment` requires the namespace to be HCP-evidenced). Neither
surface answers "what is my management cluster costing me overhead?".

## Decision

`filter[scope]=management` on the three HCP endpoints (list, grouped,
detail). A row is in management scope when its cluster's persisted W0
topology class is `management` **and** the namespace is absent from that
cluster's `hcp_namespaces` list (NULL list reads as empty). Default requests
(absent, invalid, or any other value behavior: absent → default; unknown
values → 400) are byte-identical to today's — the param is strictly additive,
with no migration (`clusters.cluster_topology` exists since W0, migration
000196).

## Alternatives considered

- **Dedicated management routes:** rejected — duplicates three endpoints'
  plumbing (pagination, RBAC, projection, list options) for one query
  fragment's difference; the surface is a scope, not a new dataset.
- **Label-derived scope (re-read the opt-in label at serve time):** rejected —
  labels are not persisted in the backend database; the collected
  recommendations are the evidence that a namespace was labeled, and topology
  class is the persisted evidence that the cluster is a management cluster.
- **Ship only an operator change (collapse ADR-0342 into collection):**
  rejected by the backend experiment — collected digests flow through the
  standard container endpoint but are dropped by the HCP surface
  (`hcpScopeFragment` fails for `multicluster-engine`), so overhead stayed
  invisible on every tab.

## Consequences

- The overhead sub-tab now serves what it collects; hosted-namespace rows
  never cross between scopes (the fragments are disjoint, proven by tests in
  both directions).
- Quota, node, PVC, and VM surfaces are untouched — container scoped only
  (demonstrated need; the others are not collected on management clusters).
- `hcp_namespaces` stays the only boundary between the two scopes; if a
  hosted namespace were mislabeled in, it would simply exit the overhead view.
