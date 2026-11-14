# Cross-Repo Contributions — for Cost Management (Koku) engineers

> Every fork-branch contribution to the sibling repos, as of
> 2026-11-14. Counts are `git rev-list --count <upstream>..HEAD` on
> each repo's `pgarciaq-rosocp-superpowers-phase17` branch. The
> characterizations are theme-level; the commit-level mapping is each
> repo's branch log (`git log <upstream>..HEAD`). The
> contribution policy (owner-set): ros-ocp-backend and
> iqe-ros-ocp-plugin changes are reviewed on the fork directly (no
> upstream PRs); koku, nise, iqe-cost-management-plugin, and the
> cheatsheet follow those repos' normal downstream processes.

## nise — 76 commits ahead of `origin/main`

The fake-data generator that drives ROS testing end-to-end (operator
CSV shapes, OCP-on-cloud payloads, GPU/VM/namespace fixtures).

- **Pod identity qualification** (ADR-0355, ADR-0356): pod identity is
  namespace-qualified and namespace usage sums are
  interval-container-summed — matching what the operator actually
  emits, so Nise fixtures validate the real parsing path.
- **GPU fixtures**: full DCGM-style GPU model strings; raised random-I/O
  peaks so idle/active classification has signal.
- **TLS hardening** and the **HCP payload generators** (the fleet
  fixtures the HCP capacity path consumes).
- **VM volume synthesis** and the PVC fixture family (the
  namespace-quota fixtures; see the known-issues register for the open
  Nise data-quality items #821/#824/#825/#837/#838).
- **COST-5691** commits track the internal ticket for the
  fixture-family work.
- Status: all cycles closed on the branch; Nise CI is PR/main-only (no
  branch CI) — validated through the ROS-side suites that consume the
  fixtures.

## koku — 53 commits ahead of `origin/main`

The integration points that let Cost Management and ROS cooperate in
one deployment.

- **ROS tag sync**: the tag-push read path (ADR-0353 — per-period label
  summaries), the dual-payload sync documentation rewrite, and the
  `ROS_TAGS_SYNC` skip flag.
- **CSV routing**: storage-usage and snapshot-inventory CSVs route to
  the ROS Kafka topics (Cost Management no longer sees ROS-owned
  payloads).
- **Savings recalculation**: koku notifies the ROS backend to
  recalculate when cost models change (the #769 callback work; its
  residual coalescing guard issue #812 is in the register).
- Also on the branch: the reship integration, currency resolution,
  and cost-group integration work (see the branch log for the
  commit-level mapping).
- **Effective rates**: the passthrough is **tracked, not landed** —
  koku-side issue #467 (stop flattening effective_rates; pass through
  cost-model rates including tag_rates) is open on this fork's board.
- **Param validation** on the report endpoints ROS consumes.
- Status: on the fork branch awaiting the downstream PR flow per the
  policy above.

## koku-metrics-operator — 98 commits ahead of `origin/main`

The operator that queries Prometheus and uploads CSVs — now also the
HCP fleet data source.

- **Phase17 branch merges**: the parked branches #764/#765 (merged via
  #810) and #768 landed on the KMO phase17 branch — full KMO suite +
  CI green.
- **Restart/pressure queries** (the HCP capacity evidence), the
  HCP topology emission, SLO and NodePool rollups, and the packaging
  recovery work. The opt-in-independent hook issue **#811 is open**
  (in the register); #768 fixed a different no-opt-in trigger. The
  unbounded-inventory-read sibling findings #767 (fixed) / #809
  (open, in the register).
- **The soft-split future** (owner-set): KMO soft-splits into
  `costmanagement-metrics-operator` (koku queries) and
  `resourceoptimization-operator` / `robne-operator` (ROS queries),
  generated from one repo via build parameter — our restart/pressure
  queries belong to the robne side.
- Status: phase17 branch, suite + CI green.

## iqe-ros-ocp-plugin — 107 commits ahead of `origin/master`

The QE suite for the ROS API — reviewed on the fork per the policy.

- The **namespace recommendation tests** (list/filter/ordering/history/
  RBAC/units), the **HCP management-scope tests**, and the container
  family's filter/sort/setting tests — including the tests that
  validate the behaviors the phase17 remediation fixed
  (`test_container_filter_engine`,
  `test_namespace_filter_engine`, the variation-ordering contract
  tests).
- **Requirement metadata** (`conf/requirements.yaml`) registering the
  ROS-OCP requirement IDs for CI filtering.
- Validated 2026-11-14 against a locally-built API (the runbook's
  local-target section).

## iqe-cost-management-plugin — 78 commits ahead of `origin/master`

The shared Cost Management QE plugin changes the ROS tests build on:
the unified category/idle-state constants, the MoneyAmount savings
assertions, SPARSE_DATA constants, and the native-engine un-skips.
Follows the downstream process.

## koku-service-operator (chart successor) & costmgmt-api-cheatsheet

- `cost-onprem-chart` is deprecated upstream, replaced by
  `koku-service-operator` — chart fixes are ported there (issue #682
  tracks the remaining ports; on `main`).
- The API cheatsheet receives endpoint updates alongside OpenAPI
  changes (the downstream repo's normal flow).

## The data flow these contributions serve

```
OpenShift cluster → koku-metrics-operator (Prometheus queries, CSV)
  → Kafka → rosocp (this fork: digests → native engine → serving)
  → koku-ui (ROS Optimizations) via the koku API gateway
Koku (Django) ⇄ ROS: tag sync, cost-model change notifications,
  effective-rate passthrough
Nise → the same CSV shapes for testing; IQE → the API contracts
```
