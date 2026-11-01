# ADR-0351: Topology Collection Reads Cluster-Scoped Facts Uncached, Bounded Per Call

## Status

Accepted — implemented in #729 (`koku-metrics-operator` `d5764197`)

## Phase

Operator reliability — the HCP/fleet facts ADR-0328 classifies on

## Context

ADR-0328 decided **what** the operator emits as cluster topology
(`Infrastructure` status and labels, node role counts, HostedCluster /
HostedControlPlane inventory for HyperShift). It did not decide **how** those
reads execute. The collection function carries a never-fail contract — an
unreadable API degrades to empty facts plus `status.topology.collectionError`
so packaging and upload are never blocked on topology — and #729 found that the
contract was unenforceable in the one case that matters.

The reconciler held the manager's **cached** client. In controller-runtime
v0.24.1 a cached read of a type whose informer is not yet synced enters
`internal.Informers.Get` with `shouldBlock` defaulting to `true` and parks in
`cache.WaitForCacheSync(ctx.Done(), HasSynced)`
(`vendor/sigs.k8s.io/controller-runtime/pkg/cache/internal/informers.go:324-334`).
A LIST or WATCH denied by RBAC never syncs, so `HasSynced()` stays false forever:
the read does not return an error, it does not return at all, and the
`CollectionError` path never runs. The reconcile context is the only escape
hatch and it carries no deadline.

`MaxConcurrentReconciles` is unset, so it defaults to **1**
(`pkg/controller/controller.go:244-245`): one denied cluster-scoped read parks
every MetricsConfig in the process while the pod stays `Running` and the CSV
stays `Succeeded`.

Measured on the SNO lab (`api.sno.karmalabs.corp`, arm64), pre-fix image
`f9c7dff0`: revoke the `infrastructures` grant, restart the pod so the informer
starts fresh, then change `spec.upload.upload_cycle` 60 → 1200. The reconcile
started at 00:33:37 and stayed parked until the grant was restored 2m47s later;
`status.upload.upload_cycle` stayed at 60 for the whole 180s observation window,
zero `topology collection degraded` lines were written, and the only trace was
the klog line `Failed to watch *v1.Infrastructure … is forbidden`. Restoring the
grant resumed the **same** reconcile and it wrote 1200 — the stall was the
topology read. (An RBAC edit alone does not reproduce it: an informer that has
already synced keeps `HasSynced() == true`, so the pod must be restarted.)

## Decision

### 1. Cluster-scoped reads in the reconcile path use the uncached API reader

`collectClusterTopology` and its list helpers take `client.Reader`, and
`setClusterTopology` — now a reconciler method, so the wiring is testable and not
only the collection logic — passes `r.apiReader` (the `manager.GetAPIReader()`
client already used by `prometheus.go`). No informer, no cache-sync wait, no
stale-cache read.

### 2. The deadline is per call, never on the reconcile context

`topologyCollectionTimeout = 5 * time.Second` wraps **only** the topology
collection. The reconcile context also carries the Prometheus queries and the
upload, which must not be truncated by a topology read; a topology-specific
deadline could be added later without touching them, and the reverse is not true.
On expiry the reads error out, the existing `CollectionError` path names the
resource, and the reconcile continues.

5s is a backstop, not a budget: a `GET` and three or four `LIST`s whose normal
latency is well under a second. It is the value in the fix plan reviewed on #729
before implementation, and it is bounded by the reconcile's own 5-minute requeue,
so a degraded cycle costs at most one interval.

### 3. Degradation is recorded, never silent

`hack/verify-install.sh` now prints `status.topology.collectionError`, so the
next occurrence names the resource instead of leaving "topology is empty" to be
traced through logs, and `hack/verify-install.selftest.sh` pins the degraded and
healthy renderings.

## Alternatives Considered

### Keep the cached client and disable the sync wait

`cache.BlockUntilSynced(false)` exists, but it is an `InformerGetOption` on
`cache.Informers.Get`; the reconciler path goes through `client.Reader.Get`,
which calls `getInformerForKind` with a zero `GetOptions`
(`pkg/cache/informer_cache.go:65-84, 172-173`) — the blocking behaviour is not
reachable from reconcile code at all. Even if it were, it would trade a visible
stall for a **silent empty read**: topology would report zero facts with no
`collectionError`, which is precisely the failure mode ADR-0328's `unknown`
class is meant to avoid.

### Give the whole manager an uncached client

Turning the cache off (or `DisableFor`-ing the cluster-scoped types) changes the
cost and the semantics of every read in the operator, including the
high-frequency namespaced ones, to fix five calls per five-minute reconcile.
Rejected as out of proportion.

### Require that the deployment always grant `infrastructures` list/watch

The stall was caused by a **degraded** grant (upgrade skew, a scoped
ClusterRole, a delegated install). A best-effort enrichment's contract cannot
depend on the failure it is supposed to tolerate, and CI cannot prove the
permission on a customer cluster.

## Consequences

- Topology costs a live API round-trip each reconcile: `GET`
  `infrastructures.config.openshift.io/cluster`, `LIST` nodes, `LIST`
  namespaces (HCP label selector), `LIST` hostedclusters, plus
  `LIST` hostedcontrolplanes when labelled namespaces exist. Every five minutes,
  accepted.
- A slow-but-permitted apiserver consumes the 5s bound and degrades topology for
  that cycle; the next reconcile restores it. A denied API degrades with a named
  resource in milliseconds and clears the same way.
- RBAC is unchanged: `config/rbac/role.yaml` already grants more than an uncached
  read needs. The `watch` verbs on the cluster-scoped types are now unnecessary
  but stay, because they are generated from the kubebuilder markers; removing
  them is a separate change with no evidence yet about other consumers.
- The same class of defect remains elsewhere: `configurePVC` reads through the
  cached client under `context.Background()`
  (`costmanagementmetricsconfig_controller.go:641`) and is tracked separately,
  not fixed here.

## Verification

- `TestSetClusterTopology_UsesUncachedReader` fails pre-fix naming the cached
  client; `TestSetClusterTopology_BoundsBlockedRead` fails pre-fix with "still
  running after 15s; the collection deadline (5s) is not applied". Both were
  mutation-checked (fix stashed) and fail for the stated mechanism.
- Lab pass 2 on the fix image (`fix729-202611010043`): with `infrastructures`
  revoked and the pod restarted, `topology collection degraded` fired **322ms**
  after the reconcile started, naming
  `infrastructures.config.openshift.io "cluster" is forbidden … cannot get
  resource "infrastructures"`; spec → status latency was **871ms** (pre-fix:
  never), node counts `1/1` survived, and restoring the grant cleared
  `collectionError` and restored `SingleReplica` within 15s.

## Related Decisions

- [ADR-0328](0328-hcp-cluster-topology-detection-w0.md): what topology emits and
  how the backend classifies on it — this record covers how those facts are read.
- [ADR-0348](0348-w8-nonprod-sleep-advisory.md): consumes the `hcTier` label these
  reads surface.
