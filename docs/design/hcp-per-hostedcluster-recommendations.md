# HCP per-HostedCluster recommendations — design proposal

**Status:** Proposed for review; no implementation is authorized by this document.
**Built since:** #630 (0b86e9b1) and #632 (8ed295cd) landed on the phase branch; #621/#622 closed as answered.
**Date:** 2026-09-28

## 1. Goal

Enable the native ROS engine and customer-facing experience to support
per-hostedcluster recommendations for a self-managed HCP fleet
(MC1 + HC1/HC2/HC3): management control-plane rightsizing, per-hosted
labeling, cross-plane correlation, latency evidence, and API/UI —
tracked across #584 (shipped management-level slice), #585 (honor-closed —
join design superseded by #621/#622 contracts), #621 (CLOSED as answered),
#622 (CLOSED as answered), #623 (CLOSED as answered), #624 (SLO store,
close-drafted), #625 (correlator build,
parent #404), #628 (correlation settings domain, child of #625; thresholds
recorded), #626 (CLOSED as answered), #638 (surface BUILT),
#639 (grouped savings follow-up), #640 (IQE, code pushed/CI pending),
#641 (chart E2E, gated on fixtures), #642 (test-infra fix),
#643 (spec-duplicates question), #630 (plugin BUILT), #631 (routing BUILT),
#632 (persistence BUILT, proven live), #633 (emission BUILT, proven live),
#634 (history BUILT), #635 (read degrade BUILT),
#620 (RH-operated bridge investigation), #627 (future optional distribution,
postponed), and #629 (deferred dedicated-master investigation, not HCP fleet).
#637 (koku-ui HCP tab, child of #626) with backend projection slice #651;
#650 (manifest stall fix) also landed in this window.
The RH-operated exchange (#620) is a separate investigation and is not
required for self-managed value.

The management-cluster identity remains the source/resource identity for W1.
HostedCluster identity is supplemental. This association is not a cost
allocation key and does not change cost distribution.

## 2. Agreed boundaries

- **M1 — self-managed dual-plane (ship first):** management and hosted evidence are in the
  customer org. Full W1/W2 language may be served subject to existing org and
  RBAC scope. No cross-tenancy exchange is used or needed here.
- **M2 — hosted-only:** hosted workload and W0 behavior remain available. No W1
  or W2 causality result is produced without management evidence.
- **M3 — RH-operated management (deferred to #620 investigation):** raw management evidence and full W1/W2 stay
  in RH tenancy. The customer may receive only a sanitized J2 W2 advisory in
  the customer org. Raw management digests and RH management identity are never
  copied to that org. "To Red Hat" means to the Cost Management backend in the
  RH tenancy where management data lives (SaaS console.redhat.com, or the
  RH-operated on-prem equivalent) — not a hostname hardcode.
- **Floors and targets (locked 2026-09-28):** HyperShift floor 4.22.14
  (lab hcp-mgmt/hc01 verified); fleet design target ~100 HCs per management
  (lab 1+1 proves join, not fleet perf); history never overwrites
  (HC3 vs HC5 coexist, §3.3); shared components unattributed by default,
  optional distribution tracked in #627 (postponed).
- **Collection is plane-local.** The management collector does not become a
  guest-cluster collector. Graceful degradation is two-tier: guardrail routing
  keys on the namespace list alone, per-HC association additionally requires
  the HostedControlPlane read — denying the new rule removes labeling only,
  never etcd-safe floors (#622/#621 contracts). M3 requires a separately authorized, minimized
  signal exchange to the RH correlator and trusted routing back to the correct
  customer org.
- Do not add a caller-selectable `M1`/`M2`/`M3` query parameter. These modes
  describe evidence ownership and tenancy, not caller-granted permissions.
- Do not change the meaning of `cluster_uuid`, recommendation IDs, or cost
  allocation/distribution.
- Do not recommend customer action to resize RH-managed shared control planes;
  no automated remediation is in scope.

## 3. Identity and report-scoped association

### 3.1 Resource identity

| Record | `cluster_uuid` meaning | Separate association |
|---|---|---|
| W1 control-plane recommendation | Management cluster where the pod was measured | HostedCluster ID, when proven |
| Hosted workload/node recommendation | Hosted cluster where the workload/node was measured | Its cluster UUID is the HC join key |
| M3 J2 advisory | Customer's hosted cluster | No management cluster or HCP namespace identity |

The HCP namespace remains the `project`/namespace for W1. Recommendation IDs
remain derived from their existing management cluster, namespace, workload,
workload type, and container identity; adding the HC association must not
change established detail links.

### 3.2 Association evidence and validity

The operator report manifest already has report start/end and collection status
fields, but the current backend only parses/persists topology facts and keeps
the latest HCP namespace list on the cluster row. That latest-row model is not
sufficient for historical identity: an API-time join could relabel a
recommendation after an HC is recreated, and current facts do not distinguish
a complete empty result from a failed list.

The report contract must carry an immutable, report-scoped mapping snapshot
with:

- explicit completeness and observation time;
- each HCP namespace and the HostedCluster ID obtained from the corresponding
  live HostedControlPlane, cross-checked against exactly one live HostedCluster
  with the same `spec.clusterID`;
- the relevant Kubernetes object UIDs and creation times needed to establish
  incarnation relative to the recommendation's measurement interval; and
- internal collection diagnostics sufficient to explain an incomplete map.

The mapping is complete only when all required inventory reads succeeded and
the namespace-to-ID mapping is unambiguous and consistent. Do not use namespace
name patterns, HC names, `infraID`, or `infrastructureName` as fallback
identity. The management operator needs read-only HostedControlPlane access in
addition to its existing HostedCluster access (#622).

Live lab proof (2026-09-28, hcp-mgmt + hc01, 4.22.14): HostedCluster
hc01-infra/hc01 spec.clusterID `d5d31999-89ed-4c13-b5e8-c9193f62e630` equals
hosted ClusterVersion ID (same value), while management ClusterVersion is
`0b9c0054-e53a-4544-9d20-6e0a7f26943a` — proving the management CSV clusterID
cannot substitute for the join. HCP namespace is `hc01-infra-hc01` with label
`hypershift.openshift.io/hosted-control-plane=true`. HCP pods carry
`hypershift.openshift.io/control-plane-component` (or `control-plane=true` for
multus/network pods); `csi-snapshot-controller` carries only the namespace
label, so the filter must OR both pod labels. `oc auth can-i get
hostedcontrolplanes` returns yes; no koku-metrics-operator is installed on
management yet. Persistence of this snapshot is tracked in #621.

Persist the snapshot by manifest identity and pass that same snapshot to the
deferred recommendation run. Do not resolve recommendations from the latest
`clusters` row. A recommendation receives an HC ID only when the complete
snapshot proves one unique association valid for the full measurement window.
If a recommendation aggregates data from multiple report manifests, every
contributing interval must have consistent complete identity evidence for the
same HC incarnation; otherwise the recommendation remains unassociated.
If the HC incarnation postdates or overlaps the window, required facts are
missing, IDs conflict, collection is incomplete, or the supported API contract
cannot establish identity stability, leave the W1 result management/namespace
scoped and unassociated. Never guess. The current upstream HyperShift API
defines `spec.clusterID` as uniquely identifying a cluster in space and time,
generates a random value when omitted, and makes it immutable once set. Treat
that as the HC identity contract; if the deployed API/version or observed
inventory violates it (for example, duplicate live IDs), fail closed rather
than falling back to names.

Persist the resolved HC ID with the recommendation when computed. A later
incomplete snapshot must not retain a now-invalid association on a refreshed
recommendation; update it to unassociated. A GET must never retroactively
relabel an existing row from current topology facts.

### 3.3 HostedCluster lifecycle and recommendation history

HyperShift derives an HCP namespace as
`<management namespace>-<HostedCluster name with dots replaced by hyphens>`.
For example, `hosted.cluster` and `hosted-cluster` in management namespace
`clusters` both derive `clusters-hosted-cluster`. Different HostedCluster names
can therefore collide; detect any live collision or ambiguous mapping and fail
closed.

For the concrete HC3→HC5 case: if HC5 has a different, non-colliding
`metadata.namespace/name`, it derives a different HCP namespace. If that exact
namespaced HC identity is recreated after HC3 and its HCP namespace are deleted,
it derives the same HCP namespace name; the recreated Kubernetes Namespace is a
new object, not the surviving HC3 namespace. HyperShift's getting-started guide
requires names to be unique within a base domain, but does not state that names
or derived namespaces are permanently reserved after deletion or document
same-name recreation as a routine workflow. Treat reuse as a defensive
lifecycle case, not an assumed product behavior. HyperShift also supports an
annotation that skips HCP namespace deletion, so a retained namespace must be
treated as a live conflict, not as a clean replacement. In either case, the
report-scoped HC ID—not the HCP namespace or name—determines which incarnation
a recommendation belongs to. HC5 needs fresh, qualifying evidence for its own
windows, not pre-creation telemetry or a namespace-mapping ledger.

When HC association is proven, freeze the HC ID on each W1 recommendation
history snapshot alongside the unchanged management `cluster_uuid`. Historical
reads must return that stored association and must not join to the latest
topology row. Keep the existing public recommendation ID and live
recommendation identity unchanged. History persistence must distinguish
different HC IDs when the management cluster, HCP namespace, workload, and
recording day are otherwise equal, so an HC5 write cannot overwrite HC3's
same-day snapshot (owner-agreed 2026-09-28: never overwrite; extend the
`recommendation_history` PK in `migrations/000029_create_recommendation_quality_and_history.up.sql`
and the `ON CONFLICT` in `internal/engine/container/history.go:44` to include
nullable `hosted_cluster_id`; unassociated rows stay null and never join).
Tracked in #623. An unassociated snapshot must not be inferred to belong to
either HC. Expose the frozen HC ID on history results so HC3 and HC5 entries
remain separately attributable/filterable under existing RBAC. This applies
within the configured recommendation-history retention period; it does not
promise indefinite archival or retroactively assign HC IDs to pre-feature
snapshots whose association cannot be proven. A separate mapping-history
ledger is not required for this retention behavior.

## 4. Recommendation and signal flow

### 4.1 HCP rightsizing (hcp plugin)

- The `hcp` plugin consumes native container digests on management-cluster reports
  scoped to the HCP namespace via `IngestHook` (no duplicate CSV ingest; gpu/node
  precedent). Toggled via `ROS_ENABLED_PLUGINS=hcp`; owns its guardrails, routes,
  and retention. Terms v1: reuse container short/medium/long windows with
  `supports_terms:false`.
- Apply the accepted HCP filters and guardrails in ADR-0331, including the
  conservative CPU/memory floors and suppression of replica recommendations.
- Mark results as the `hcp` recommendation type (API identifier `hcp`; UI display
  may read Hosted Control Plane). Keep management
  `cluster_uuid`; attach the report-proven HC ID separately when available.
- W1 remains independently useful when W2 evidence is absent. Lack of an HC
  association must not erase an otherwise-valid management-scoped W1 result.

### 4.2 Hosted workload and worker pressure

- Reuse existing native hosted workload and node pipelines. The hosted
  ClusterVersion ID is the HC join key, as confirmed by ADR-0332 and lab
  evidence.
- Derive W2's worker-pressure negative control from positive, fresh hosted
  node/pod pressure evidence, with an explicit completeness/coverage state.
- Absence of a node recommendation is not proof that workers are healthy.

### 4.3 W2: precision-first cross-plane correlation

Use aligned per-HC time windows and the accepted ADR-0332 conditions:

| Symbol | Evidence |
|---|---|
| `H` | Hosted API latency is high using correctly filtered histogram evidence |
| `C` | The mapped management control plane is stressed; initially this may reuse
  HCP-namespace container CPU evidence, with optional API/etcd signals later |
| `N` | Hosted workers are under high pressure |

Emit high-confidence “do not add workers first; investigate the control plane”
only for `H && C && !N`. `H && N` does not blame the control plane. `H && !C`
does not blame the control plane. Missing, stale, incomplete, unassociated,
or temporally misaligned evidence emits no causality result and must not be
presented as proof of health.

The new hosted SLO collection should retain bounded histogram bucket rollups
per HC and time window, filtered to exclude `WATCH`, `CONNECT`, and `PROXY` as
required by ADR-0332. Do not ship raw Prometheus series or high-cardinality
labels. Sending only a p99 scalar is insufficient if aggregation/recombination
would make the quantile inaccurate. The M3 exchange also carries the worker
pressure/completeness summary, HC ID, observation window, and freshness/quality
metadata; RH combines it with RH-local management evidence.

Numeric “high” thresholds, baseline policy, observation window, clock-skew
tolerance, and expiration/freshness limits are a pre-production gate owned by
#625 (child of #404). ADR-0332 does not choose them, and the one-HC/two-worker lab is not a fleet calibration
set. Record the selected policy and evidence before enabling W2; do not trade
accuracy for payload size or speed without a quantified decision and approval.
The SLO bucket store feeding this gate is tracked in #624, and the settings
contract (three-tier `ROS_HCP_*` + `/settings/hcp-correlation`, capabilities,
recalc) in #628. Live probes 2026-09-28 force C-v1=CPU: hosted filtered p99
25ms healthy, mgmt HCP CPU 1.7 cores idle, per-namespace apiserver latency query
empty, etcd buckets present. Shared
control-plane components (konnectivity, shared ingress,
control-plane-operator serving N HCs) stay unattributed by default; optional
100pct distribution is future work in #627 (postponed, default off).

## 5. Audience, tenancy, and M3 exchange

### 5.1 M1 and M2

- In M1, correlate the two planes within the same customer org. The HC filter
  is an additional narrowing filter only; all existing `org_id`, management
  cluster, and HCP namespace/project RBAC checks remain in force.
- In M2, the absence of management input means no W1/W2 record. Continue hosted
  workload and topology behavior without manufacturing a cross-plane result.

### 5.2 M3 J1/J2

- RH stores management reports, W1, and full W2 in RH tenancy (J1).
- A trusted authenticated channel supplies only bounded hosted SLO and worker
  pressure rollups to the RH correlator. The source-side authorization and
  approved data-sharing policy must be explicit; user-supplied HC IDs are not
  authorization.
- A trusted platform association must route the result to the correct customer
  org and verify that the HC belongs to that org. HC ID alone does not identify
  or authorize a customer tenant.
- Write only a separate J2 advisory record to the customer org. It contains
  hosted-cluster identity, advice code/copy, confidence, evidence window,
  freshness/expiry, and safe CTA. It contains no RH management cluster UUID or
  alias, HCP namespace, node/IP/name, sibling HC identity, raw metric, or CP
  resize field.
- If signal-sharing authorization or destination routing is missing/ambiguous,
  do not publish J2. Keep raw evidence RH-side and use the ADR-0330 safe fallback
  (hosted W0 only).

This M3 bridge and authoritative HC-to-customer-org routing are platform
dependencies not established by the current ROS API contract. They must be
owned and approved before M3 customer W2 is enabled. RH direct collection from
guest clusters is not the selected approach.

## 6. API and UI shape

- Add a dedicated Control Plane recommendation API/UI surface for W1. Include
  the separate HC association and distinct HC filter plus group-by; keep existing
  `cluster` semantics as the reporting/source cluster and `project` as the HCP
  namespace. Owner-agreed 2026-09-28: unassociated rows show with
  `incomplete:true` (visible management-scoped, excluded from hosted-specific
  joins). Enforcement point to name in #626: org + management-cluster +
  HCP-namespace checks in `internal/model/recommendation_set.go` and
  `internal/rbac/query_builder.go` run before the hosted filter, and the hosted
  tag never grants management access. Tracked in #626; M3 advisory stays a
  structurally separate resource owned by #620. Include the frozen HC association in history results and let
  users distinguish/filter history by HC ID. Preserve existing recommendation
  IDs and detail links.
- Apply org scope and existing cluster/project RBAC before the HC filter. The
  HC association never grants access to the management cluster or HCP project.
- Keep unassociated W1 results management/namespace scoped. They are not
  eligible for HC-specific joins or filters.
- Serve M1 full W2 through the full recommendation/correlation contract and RH
  internal consumers through RH-side access. Serve M3 customers through a
  structurally separate advisory resource, not by redacting full W2 records in
  the UI.
- The customer advisory identifies only the customer's hosted cluster and
  shows evidence window/freshness and confidence. It expires when evidence is
  stale and uses the approved provider-contact/no-worker-scale-first language.
- Update OpenAPI, API contract tests, UI types/list/detail behavior, and public
  docs when implementation changes the customer contract. The API does not
  accept a caller-selected audience mode.

Exact contract (locked 2026-09-28, #626): route
`GET /recommendations/openshift/hcp` with `recommendation_type hcp`;
`cluster_uuid` stays source cluster, `project` stays HCP namespace; new nullable
`hosted_cluster_id` plus `incomplete` flag; hosted filter plus group-by;
history returns frozen hosted ID. The identity and visibility semantics above
plus this contract are the customer contract.

## 7. Delivery sequence and gates

1. **Foundation + association (BUILT 2026-09-29):** `hcp` plugin scaffold first (#630, parent #384,
   build-first; #626 depends on it, not vice versa). Then operator report
   evidence and read permission (#622/#633, proven live on hcp-mgmt); backend
   manifest-scoped persistence (#621/#632, proven live); temporal validation; W1 association storage;
   M1/RH API and Control Plane UI (#626/#638). W1 remains useful without association.
   History PK in #623/#634 (never overwrite); server routing in #631; read degrade in #635.
2. **W2 in one trust domain:** bounded hosted API SLO collection/store (#624), evidence
   storage, window alignment, precision-first correlator and numeric policy gate,
   and M1 full result (#625, child of #404).
   Gate on PromQL correctness, thresholds, freshness, clock skew, and positive
   and negative controls.
   Thin W5 (#393): webhook rollup store + `tune_noisy_webhook` advisories —
   per-cluster admission-webhook p99/rejected-rate gates, verdict-distinguished
   rows in the existing advisories table, empty HC attribution for shared-plane
   evidence. No routes, no UI.
   W3 child-0 (#658): unused-HC idle rule — trailing-14d MAX-daily gate plus
   still-on proof, `review_unused_hosted_cluster` verdicts (high/medium) in
   the same table, idle-T three-tier on the existing settings domain. No
   migration, no UI, no operator changes.
   W3 child-1 (#664, ADR-0339): idle leg redefined to workload-presence +
   CPU primary (API counts non-discriminating per #662 measurements);
   new `z_idle_cpu_floor_mc` setting (default 10m, discouraged), T
   deprecated, compiled narrow platform exclusion (koku sync in #665).
   NodePool inventory (#673, Child A of #660): operator NodePool reads +
   `hosted_nodepool_rollups` store; rule interprets in Child B.
3. **M3 customer advisory (deferred):** authenticated minimized signal exchange, trusted
   tenant routing, J1/J2 storage policy, and separate advisory API/UI (#620
   investigation first; build only after owner + data-sharing approval). Gate on
   platform authorization, data-sharing approval, destination verification,
   and adversarial cross-tenant tests.

Hosted workload/node recommendations and W0 are reused, not rewritten. This
design does not authorize implementation, deployment, issue edits, or changes
to other repositories.

## 8. Acceptance criteria

### Collection and association

- A complete, fresh, one-to-one namespace↔HC snapshot associates a W1 result
  only when valid for the complete recommendation measurement interval.
- Missing HCP/HC, duplicate/conflicting IDs, any required inventory read error
  (record which read failed), distinct HC names colliding after namespace normalization, stale evidence,
  or an unprovable incarnation produces no HC association — rows stay
  management-scoped with guardrails on whenever the namespace list is present.
- W1 remains available under management cluster + HCP namespace identity when
  the association is absent.
- Recreating an HC or changing an association cannot retroactively relabel an
  older recommendation; refreshed invalid rows clear their HC association.
- Recreating HC3's exact management `metadata.namespace/name` as HC5 after
  deleting HC3 and its HCP namespace derives the same HCP namespace name. With
  otherwise identical management cluster, namespace, workload, and recording
  day, history retains separate HC3 and HC5 snapshots with their frozen HC IDs;
  the HC5 write cannot overwrite HC3. Unassociated snapshots remain
  unassociated, and old snapshots are not backfilled without proof.
- Tests use at least two synthetic HCs with distinct IDs and adversarial
  same-name/namespace-like values so accidental name joins and fan-out are
  observable.

### W2 correctness

- `H && C && !N` produces the intended high-confidence result.
- `H && N`, `H && !C`, missing/stale/incomplete evidence, identity ambiguity,
  and out-of-tolerance clocks do not produce CP blame.
- PromQL tests verify `WATCH`/`CONNECT`/`PROXY` exclusion against known-good
  dashboard semantics; histogram rollups retain sufficient bucket information.
- Worker-pressure absence is never interpreted as “not pressured.”

### API, UI, and tenancy

- M1 HC filtering cannot bypass org, management-cluster, or HCP namespace RBAC;
  `cluster_uuid` still identifies the cluster where evidence was collected.
  Unassociated rows show with `incomplete:true` (#626); history PK carries
  nullable `hosted_cluster_id` (#623).
- M2 produces no W1/W2 result without management evidence.
- M3 customer responses cannot contain RH management identity, HCP namespace,
  sibling identity, raw measurements, or CP-resize advice.
- M3 delivery rejects absent, ambiguous, or unauthorized HC-to-customer routing;
  the correct customer org is verified before J2 persistence.
- Expired/incomplete J2 evidence is not rendered as current advice.

Mutation-resistant regression fixtures must fail for the intended mapping,
time-validity, RBAC, or disclosure mechanism—not for incidental ordering or
same-value coincidences.

## 9. Explicit non-goals

- Replacing or changing cost allocation/distribution (optional future #627 only).
- Dedicated/static-pod master rightsizing in this plugin (deferred #629, not HCP fleet).
- A `/settings/hcp` god-route or stuffing correlation policy into
  `/settings/node`/`/settings/container` (narrow `/settings/hcp-correlation` locked in #628).
- Replacing management `cluster_uuid` with HC ID or changing existing ROS IDs.
- Centralizing guest workload collection on the management cluster.
- Full causality beyond ADR-0332's thin MVP, automated remediation, or a
  customer CTA to resize RH-managed control planes.
- Guessing numeric thresholds or latency/clock tolerances from the current lab.
- Exposing raw RH management data or adding a client-controlled audience mode.
- Implementing M3 cross-tenant infrastructure without platform approval.

## 10. Source references and current gaps

- [ADR-0328 — topology classification](../adr/0328-hcp-cluster-topology-detection-w0.md)
- [ADR-0329 — HCP namespace collection](../adr/0329-ros-auto-include-hypershift-hcp-namespaces.md)
- [ADR-0330 — audience and tenancy](../adr/0330-hcp-audience-visibility-rh-vs-customer.md)
- [ADR-0331 — W1 filters and guardrails](../adr/0331-management-cp-rightsizing-filters-and-guardrails.md)
- [ADR-0332 — W2 metrics and algorithm](../adr/0332-thin-cross-plane-causality-w2.md)
- [HCP fleet optimization plan](../plans/hcp-fleet-optimization.md)
- Report topology persistence is in `internal/services/report_processor.go`
  and `librobne/pgrec/cluster.go`, plus report-scoped snapshots in
  `librobne/pgrec/hcp_snapshot.go` keyed by manifest identity (#632).
- Native recommendation identity and writes are in
  `librobne/types/types.go`, `librobne/pgrec/write.go`, and
  `internal/model/types/recommendation_ids.go`. History is daily-upserted by
  management cluster/workload identity in `internal/engine/container/history.go`
  with a frozen hosted sentinel in its key (#634); `migrations/000029_create_recommendation_quality_and_history.up.sql`
  defines the corresponding history primary key.
- API identity/RBAC is based on authenticated org plus reporting
  cluster/project in `internal/model/recommendation_set.go` and
  `internal/rbac/query_builder.go`, with the dedicated HCP surface
  (`GET /recommendations/openshift/hcp`, filter/group-by, `incomplete`) in
  `internal/api/handlers_hcp.go` and the `hcp` plugin (#638).
- The operator topology reader emits the namespace-to-HC-ID snapshot with a
  HostedControlPlane read rule (#633, proven live).
- The current ROS backend has no SLO rollup store (#624) or M3 signal-exchange and
  customer-org routing contract (#620). Correlation settings have no
  `/settings/hcp-correlation` route yet (#628); the `hcp` plugin scaffold
  (`internal/plugins/hcp`, IngestHook, `ROS_ENABLED_PLUGINS=hcp`) is part of #626.
- Lab probes 2026-09-28 (hcp-mgmt + hc01, 4.22.14): join proven, HCP labels proven
  including the `csi-snapshot-controller` OR-label edge, `can-i get hostedcontrolplanes=yes`,
  Prometheus on both planes, hosted filtered p99 25ms, mgmt HCP CPU 1.7 cores,
  per-ns apiserver latency empty, etcd buckets present, no metrics-operator on management yet.
- HyperShift's current upstream contract and namespace derivation are described
  in the [HostedCluster API](https://github.com/openshift/hypershift/blob/main/api/hypershift/v1beta1/hostedcluster_types.go),
  [namespace helper](https://github.com/openshift/hypershift/blob/main/hypershift-operator/controllers/manifests/manifests.go),
  [HCP namespace example](https://hypershift-docs.netlify.app/labs/IPv4/hostedcluster/hostedcluster/),
  and [create/delete guide](https://hypershift-docs.netlify.app/getting-started/).

## 11. Tracker coordination note

#585 is honor-closed (join design superseded by #621/#622 contracts). The work
in this doc is now tracked as: #584 shipped management-level; #630 hcp plugin
scaffold first (parent #384 — BUILT); #621 snapshot persistence (CLOSED
as answered); #622
operator emission (CLOSED as answered); #623 history PK (CLOSED as answered,
never overwrite); #624 SLO store (close-drafted + retention/thresholds/analysis
locked); #625
correlator build (parent #404) plus #628 settings domain (child of #625,
thresholds recorded);
#626 API/UI surface (CLOSED as answered; enforcement decided in #589); #620 M3 bridge investigation (deferred; see timing below);
#627 future optional distribution (postponed); #629 deferred dedicated-master
investigation (explicit non-HCP-fleet non-goal). Builds landed: #632 backend
persistence (proven live) implements #621; #633 operator emission (proven live)
implements #622; #634 history implements #623; #631 routing; #635 read degrade;
#638 surface implements #626; #639 grouped savings (BUILT: per-HC SUM of pinned
short/cost savings cents through the fleet currency pipeline); #640/#641/#642/#643 tracked follow-ups.

#620 timing: run the investigation after self-managed association + SLO store
close their criteria (#621/#624), so the bridge reuses proven rollup shapes
instead of inventing them. Do not start M3 build until #620 names an owner
and cites written authorization + routing verification.
