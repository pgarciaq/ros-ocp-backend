# #769 Savings Callback Cluster ID — Implementation Plan

> **For agentic workers:** superpowers:test-driven-development governs every step. Owner go-ahead 2026-11-06. Two repos: the fix in `~/dev/koku/koku` (fork `pgarciaq`, branch `pgarciaq-rosocp-superpowers-phase17`, at `5a4773588` after #771); the consumer contract guard in `~/dev/koku/ros-ocp-backend` (same branch pattern, at `90e023e2`). Central tracker ros-ocp-backend #769.

**Goal:** the savings callback sends the provider's actual OpenShift cluster ID in `cluster_uuid`; ROS's verbatim use (rate-cache invalidation + recommendation selection) then targets the real cluster.

**Mechanism (verified at HEAD, both sides):** koku's notifier maps `payload["cluster_uuid"] = str(provider_uuid)`; the updater caller has held `self._cluster_id = get_cluster_id_from_provider(...)` since construction; ROS's handler → `RecalculateSavingsForOrg` → `InvalidateCostDataCache(orgID, clusterUUID)` + `listClustersForSavingsRecalc → []string{clusterUUID}` — verbatim, no translation. The existing koku test asserts the wrong mapping (`assertEqual(str(ocp_provider_uuid), payload["cluster_uuid"])`).

**Fix (koku):** the notifier gains `cluster_uuid: str | None = None` used for the payload (None → omitted → ROS's org-wide path, the correct degradation); `provider_uuid` stays for the log line. The caller passes `cluster_uuid=self._cluster_id`. HTTP contract otherwise unchanged.

**Tests:**
- Koku notifier RED: distinct IDs by construction (uuid ≠ "OCP-on-Prem"); assert `payload["cluster_uuid"] == the provider's real cluster ID`. Baseline: TypeError (missing kwarg — the missing capability), then the assertion failure via the mutation (payload still provider_uuid). Topology pair: the same payload assertions under BOTH `ROS_OCP_BACKEND_URL` and `ROS_API_HOST/PORT` wiring.
- Koku updater pin: patch the notifier in the updater module, drive `update_summary_cost_model_costs` (the existing harness), assert the notify call received `cluster_uuid == updater._cluster_id`. RED on baseline (no kwarg).
- ROS consumer guard (contract pin, GREEN-tier): engine-level — `RecalculateSavingsForOrg` with a distinct cluster string; the per-cluster func (via `SetClusterSavingsRecalcFuncForTest`) receives it verbatim (selection), and a REAL cache-invalidation assertion via the costdata httptest pattern (the target cluster's entry refetched; another cluster's stays cached).

**Constraints:** no ROS production change (the consumer is correct); the 202 status is never evidence; commits wait for owner go-ahead.
