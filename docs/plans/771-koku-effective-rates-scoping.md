# #771 Koku Effective-Rates Tenant Scoping — Implementation Plan

> **For agentic workers:** superpowers:test-driven-development governs every step. Owner go-ahead 2026-11-05. Implementation in `~/dev/koku/koku` (fork `pgarciaq`, branch `pgarciaq-rosocp-superpowers-phase17`, at the reviewed baseline `926c76fee` — zero drift); central tracker ros-ocp-backend #771.

**Goal:** the effective_rates endpoint resolves the provider by (validated customer, cluster_id), not a global authentication scan returning the first match.

**Mechanism (verified at HEAD):** `_resolve_provider_uuid(cluster_id)` iterates ALL `ProviderAuthentication` rows and returns the first provider matching the auth row — no customer predicate. Koku explicitly supports the same cluster_id across customers via shared authentication objects (pinned by `api/provider/test/test_serializers.py::test_ocp_cluster_id_isolation_between_customers`). Org B's request returns org A's provider; `_get_configured_rates(schemaB, providerA)` queries the wrong combination; ROS reship trusts the returned UUID.

**Fix (one predicate):** `_resolve_provider_uuid(cluster_id, org_id)` filters `Provider.objects.filter(authentication=auth, customer__org_id=org_id)` (`Provider.customer` is an FK to Customer, `null=True` — unowned providers are excluded, correctly). The view passes its validated `org_id`. Scope gate respected: no security relabeling; cross-customer registration untouched.

**Tests (append to `masu/test/api/test_effective_rates.py`, the established harness):**
- RED: two customers (org1234567 + org2222222), providers A/B sharing ONE authentication row (the "reuses matching authentication objects" case), distinct per-tenant cost models (cpu 2.0 vs 1.0). Org B's request → assert provider_uuid == B's and rates == B's. Baseline: A's provider returned (the filed mechanism).
- Controls: org A's request → A's provider + A's rates (order-reversal); org3333333 (customer exists, no provider for the cluster) → 404, no foreign data; namespace aggregates stay tenant-scoped (seed a summary row for the shared cluster in B's schema only → B's response has it, A's doesn't).
- Cross-customer registration stays supported: the existing serializer test pins it (rerun).

**Constraints:** koku-only; no registration restrictions; no drive-by refactors; commits wait for owner go-ahead. Exclude the unrelated working-tree files (docker-compose.yml, stray CSVs).
