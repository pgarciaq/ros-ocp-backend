# W4 Fleet Capacity Advisory — Design Proposal

> **Status:** Approved by owner (2026-10-24). W4 is greenlit; product-code work waits for implementation-plan review.
> **Parent:** [#384](https://github.com/pgarciaq/ros-ocp-backend/issues/384)
> **Tracker:** [#392](https://github.com/pgarciaq/ros-ocp-backend/issues/392)
> **Accepted architecture:** [ADR-0334](../../adr/0334-fleet-admission-headroom-w4.md)

## Intent

Provide a cautious, management-cluster-scoped ROS-native capacity advisory for
the organization that operates the HCP management cluster. It should show
fleet-wide scenario headroom and, separately, per-HC evidence/recommendations
where raw inputs support them. Use explicit published sizing assumptions and
compare against MCE's capacity outputs when available; do not present a
universal, lab-calibrated “N more clusters” guarantee.

The owner has explicitly greenlit W4 as a wedge and approved this revised
design. No product code has started.

## Goals

- Compute scenario-labeled capacity with a ROS-native engine from raw
  management-cluster inputs and published sizing assumptions; compare with the
  five MCE request/low/average/medium/high-QPS gauges when present.
- Show fleet-wide headroom as `scenario_max_hcs - current_hc_occupancy`, and
  per-HC evidence/recommendations separately where their inputs support them.
- Scope each result to one management cluster, preserving existing organization
  and cluster RBAC.
- Support the management-operator organization, including self-managed
  customers; do not require cross-tenant M3 routing for this surface.
- Degrade honestly when inputs are missing, stale, incomplete, or pressured.

## Non-goals

- A lab-calibrated or unlabeled capacity formula that claims to reproduce
  MCE's output without matching its assumptions.
- A universal capacity number or calibration from the current lab.
- Per-HC cost attribution, W6 cost distribution, or shared-CP chargeback.
- Cross-tenant publication of RH-operated management-cluster data into a hosted
  customer tenant; this is a separate routing/authorization scope, not a blanket
  restriction on customer-visible self-managed recommendations.
- `robne` CLI-specific file ingestion/output and JSON-envelope changes in W4
  core. The pure `librobne/hcp` calculation remains reusable; CLI exposure is a
  separate, non-blocking follow-up tracked as [#700](https://github.com/pgarciaq/ros-ocp-backend/issues/700) under W4 #392 and related to the closed robne CLI umbrella (#99).
- Automatic scaling or remediation.
- High-frequency/raw Prometheus time-series export.

## Owner decisions already recorded

- **Native engine:** W4 is a ROS-native capacity calculation from raw inputs and
  published sizing assumptions. MCE's capacity gauges are a comparison/reference
  source, not a prohibition on implementing the native engine.
- **Transport:** a bounded `ros-openshift` CSV through existing operator ROS-file
  packaging and ROS typed CSV ingestion. Do not extend manifest `cr_status` or
  Koku Kafka metadata for W4.
- **Output/surface:** expose the fleet-wide scenarios separately, and show
  per-HC evidence/recommendations in the HC list/detail. Do not repeat one
  fleet-wide `+N` value on every HC row. This is neither the org-wide container
  `fleet-summary` nor a replacement for per-HC recommendations.
- **HC occupancy:** count Available, Progressing, Degraded, Paused/`pausedUntil`,
  NodePool-zero, and Deleting-until-removed HostedClusters; exclude fully
  removed HCs. This corresponds to counting every visible HostedCluster API
  object, including terminating objects, without phase or NodePool filters.
  This is an explicit conservative occupancy policy: MCE's average-QPS
  calculation uses Ready HostedControlPlanes only, so label that scenario's
  traffic basis rather than blocking the other scenarios on exact lifecycle
  parity.
- **Missing/stale/pressure:** never zero-fill. A snapshot is stale when its age
  exceeds 2× the effective cadence, effective cadence = max(W4 capture interval,
  upload cycle) (amended 2026-10-25 review: two bare collection intervals left
  `latest` stale most of the time at the default 360-minute upload cycle). The
  snapshot carries both interval fields so serving never guesses. Affirmative
  management pressure suppresses precise added-headroom advice; unknown
  pressure is shown as “not assessed” with a warning, not as proof of safety
  or a blanket feature blocker.
- **Versions:** the source/documented floor found is OCP management 4.17 and MCE
  2.6, with only OCP/MCE pairs allowed by the official management-cluster
  support matrix. The metric names are capability-detected; current/current
  supported pairs are covered by the matrix rather than an arbitrary cross
  product of versions.
- **Persistence:** retain historical recommendation snapshots, following ROS's
  existing history patterns; define retention and query behavior in the W4 plan.
- **Source separation:** do not cross-model blend. Keep native-model results and
  MCE gauge observations labeled as distinct sources when both are shown.
- **Audience:** customer-visible self-managed management-cluster advice is in
  scope. Cross-tenant M3 delivery is not required for W4 core; #620 remains a
  separate investigation for its W2/J2 bridge scope.
- **Modes:** deliver through the present KMO ROS collection path first. Local
  Mode (#138) has no upload-cycle dependency and can add a local adapter later;
  it is not a prerequisite for Remote-mode W4.
- **Validation:** unit/contract tests plus the existing off-matrix plumbing
  smoke; version support follows the documented compatibility matrix.

## Evidence and current implementation gap

- ADR-0334 describes five MCE gauges as estimated maximum HCP capacities under
  request and QPS assumptions. These are aggregate maxima, not per-HC advice.
- Read-only live verification on hcp-mgmt (OCP 4.22.17 / MCE 2.17.3): the
  management KMO service account queried all five gauges through Thanos (HTTP
  200, one series per gauge). Samples were 8/8/8/4/2. The current cluster has
  three visible HostedClusters and three Ready HostedControlPlanes, with no
  paused/deleting objects. This proves the ordinary live path only; the lab is
  off-matrix and must not calibrate user-facing capacity.
- Source inspection of MCE backplane-2.6 and backplane-2.17 finds the same five
  metric names. The gauge implementations are scalar Prometheus gauges; scrape
  labels are added by the monitoring stack. The calculation sums worker-node
  `status.capacity` CPU, memory, and pods and applies the published baselines.
  The average-QPS scenario queries a two-minute `apiserver_request_total` rate
  only for Ready HostedControlPlanes. Therefore the owner's all-visible-HC
  occupancy rule is intentionally more conservative than the Ready-only set
  used to derive average QPS; it is not an exact lifecycle-set equivalence.
- MCE source also defines `mce_hs_addon_worker_node_resource_capacities_gauge`
  (node/cpu/memory/maxPods), `mce_hs_addon_qps_gauge` (rate), and
  `mce_hs_addon_qps_based_hcp_capacity_gauge` (qps_rate). These are useful
  diagnostic/reference inputs; verify their stability before depending on them
  as public contracts.
- Public OCP HCP sizing documentation includes the five capacity metrics in
  OCP 4.17 and later checked versions; OCP 4.17's management support matrix
  includes MCE 2.6/2.7. The MCE 2.6 and 2.17 source branches both contain the
  metric code. Use OCP management 4.17+ / MCE 2.6+ as the initial floor, subject
  to the official compatible-pair matrix (not every cross-product pair).
  References: [OCP 4.17 sizing/support matrix](https://docs.redhat.com/en/documentation/openshift_container_platform/4.17/html/hosted_control_planes/preparing-to-deploy-hosted-control-planes),
  [OCP 4.22 sizing/support matrix](https://docs.redhat.com/en/documentation/openshift_container_platform/4.22/html/hosted_control_planes/preparing-to-deploy-hosted-control-planes),
  [MCE 2.6 calculation source](https://github.com/stolostron/hypershift-addon-operator/blob/backplane-2.6/pkg/agent/hcp_capacity_calculation.go),
  [MCE 2.17 metric definitions](https://github.com/stolostron/hypershift-addon-operator/blob/backplane-2.17/pkg/metrics/hcp-size-calculation-metrics.go).
- Repository inspection found no W4 capacity integration in ROS or KMO. KMO
  already has a generic Prometheus instant-query client, Kubernetes API access,
  a ROS collection gate, and ROS-file CSV packaging. ROS has typed CSV ingest
  plugins and dedicated HCP API routes, but no W4 capacity store/engine yet.
- KMO's existing topology collector lists all visible HostedCluster objects
  without phase filtering and records a `CollectionError` for failed reads.
  However, a missing HostedCluster API currently degrades to an empty list with
  no error; W4 must explicitly distinguish unavailable inventory from a
  complete count of zero. The existing `HostedClusterCount` is therefore not
  sufficient as the sole completeness signal.
- KMO's collection/reporting and upload schedules are separate. It collects
  Prometheus time windows (including 15-minute ROS reports and hourly snapshots)
  and packages/uploads on `upload_cycle`, which defaults to 360 minutes and is
  clamped to at least 60 minutes. The current lab CR uses 60 minutes. Upload
  cadence adds transport latency and feeds the effective staleness window; it
  does not define the snapshot timestamp (`observed_at` is the report-window
  end).
- `disable_metrics_collection_resource_optimization` is the existing KMO ROS
  collection gate. Local Mode in #138 uses its own on-cluster path; the planned
  15-second collection does not imply 15-second freshness for an MCE gauge whose
  scrape/recalculation timestamp has not advanced.
- ROS node-utilization is workload usage, and the existing SLO pressure CSV is
  hosted-worker pressure; neither is direct management-plane pressure. Unknown
  pressure should be surfaced as unknown, not silently treated as healthy.
- The current management HCP lab is ARM64 / Agent / SingleReplica and is
  explicitly off-matrix. Its values must not calibrate or validate user-facing
  capacity estimates.
- #398 is useful for continued platform alignment but is not a W4 implementation
  blocker given the source/version evidence above. #620 concerns a separate
  cross-tenant W2/J2 bridge and does not block same-tenant W4 delivery.

## Proposed data flow

1. **Remote mode now:** add a ROS-specific collector to the present
   koku-metrics-operator path, guarded by the existing
   `disable_metrics_collection_resource_optimization` setting. Collect a
   bounded raw-input snapshot from the Kubernetes API — eligible worker
   CPU/memory/pod capacity, HC/HCP inventory and completeness, pressure —
   and from Prometheus — per-HCP QPS summaries and MCE's five output gauges
   when present as comparison data, not as the only calculation.
2. Send the bounded snapshot as a `ros-openshift` CSV through the existing
   `resource_optimization_files` path. KMO packaging recognizes
   `ros-openshift*.csv`; Koku forwards manifest-listed ROS files; ROS adds a
   typed CSV ingestor. Keep management `cluster_uuid` authoritative from trusted
   report metadata, not a CSV field. Do not extend manifest `cr_status` or Kafka
   metadata for W4.
3. W4 captures hourly, beside the existing hourly snapshot collectors;
   capture cadence stays separate from packaging/upload cadence. The upload
   cycle feeds the effective staleness window (2× the max of capture and
   upload cycle) but does not define the snapshot timestamp — `observed_at`
   is the report-window end, and `mce_sampled_at` preserves the gauge scrape
   time. Local Mode (#138) can
   later use a local adapter and its own cadence; its planned 15-second loop
   does not imply MCE gauges themselves update every 15 seconds. Do not block
   current Remote delivery on that future adapter.
4. Apply the ROS-native published-sizing calculation to complete raw inputs,
   preferably as pure shared `librobne` math. Store time-stamped history for
   fleet scenarios and per-HC recommendation evidence; serve the newest
   history row as current state — no materialized latest table. Do not
   duplicate the same fleet `+N` result on each HC.
5. Serve one management-cluster fleet view and its per-HC recommendations under
   organization/cluster RBAC. Existing `fleet-summary` is org-wide
   container/savings data and `fleet-heatmap` is cluster-by-node utilization;
   neither defines W4. The ROS UI presents both scopes. Self-managed customer
   visibility is in scope; cross-tenant M3 publication is independent and does
   not block W4 core.

## Proposed accuracy and failure behavior

- Calculate the native model only from complete, fresh inputs. Keep each
  request/QPS scenario separate, use documented sizing inputs, and record the
  limiting resource/assumption. Use MCE gauge outputs as a separate reference;
  never zero-fill, substitute one scenario for another, or silently blend
  native and MCE values.
- Count every visible HostedCluster object for occupancy. A failed or
  unavailable HC API means inventory is incomplete, not zero. Subtracting all
  live HCs is the declared conservative occupancy policy. For the MCE
  average-QPS reference, disclose that its observed-QPS input uses Ready HCPs;
  this does not block other scenarios or require an exact lifecycle-set match.
- Mark a snapshot stale when its age exceeds 2× the effective cadence
  (max of the W4 capture interval and the upload cycle, both carried in the
  snapshot). Use the window-end sample timestamp, not packaging time, so
  delayed or replayed reports cannot appear fresh; the effective-cadence clock
  keeps an hourly row fresh through a 6-hour default upload cycle.
- A missing MCE gauge is unavailable, never zero. The native estimate remains
  available when its raw inputs are complete. If native inputs are incomplete
  but a complete MCE scenario set is fresh, show that result explicitly labeled
  as MCE-derived; do not fill individual missing native scenarios from MCE.
- If affirmative management pressure is present, suppress precise added-HC
  headroom and advise growing capacity first. If pressure data is absent or
  stale, show “pressure not assessed” and a caution; do not represent unknown as
  healthy or make the whole W4 feature unavailable. Existing workload
  node-utilization and hosted-worker pressure are not management-pressure
  signals.
- Preserve signed headroom internally. When
  `scenario_max_hcs - current_hc_occupancy <= 0`, show zero additional HCs and
  an at/over-capacity warning.

## Implementation work (not external blockers)

1. Validate known MCE metric names, runtime cardinality, values, and sample age;
   keep metrics capability-detected. Use the documented OCP 4.17+/MCE 2.6+
   floor subject to the official compatible-pair matrix.
2. Add unit/contract fixtures for all HC lifecycle states and for Ready-only
   HCP average-QPS inputs. Verify that incomplete list reads fail closed and
   that the all-visible-HC occupancy rule remains explicit.
3. Implement a management-pressure collector in parallel. Until signals are
   available, report pressure as unknown; suppress precise headroom only when
   affirmative pressure is observed.
4. Implement first in KMO's existing ROS-enabled collection path. Keep
   `disable_metrics_collection_resource_optimization` behavior; design the
   snapshot/engine boundary so #138 can add Local Mode without changing the W4
   recommendation contract.
5. Persist history and expose it through W4's management-scoped API/UI; use
   existing ROS history/retention patterns, with exact retention chosen in the
   implementation plan.

## Approval gate

The owner approved this design on 2026-10-24. #620, #398, and #138 are not
blockers for W4 core: #620 remains a separate cross-tenant W2/J2 investigation,
#398 can continue platform alignment in parallel, and #138 can add Local Mode
later. The implementation plan covers KMO, ROS backend, and ROS UI. Do not
commit or push without explicit approval.
