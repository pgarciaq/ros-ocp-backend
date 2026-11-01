# IQE requirement registration for ROS recommendation tests

> **Full guide:** [docs-site/testing/iqe-requirements-registration.md](../../docs-site/testing/iqe-requirements-registration.md)
> — browsable in-repo and on the published site. It is the **single source**:
> the 24-ID requirement registry, the marker convention (the `cost_ros_ocp`
> catch-all default; specific IDs minted only for targetable capabilities),
> and the app-interface registration steps live on that page.

**Why this page is a stub:** this file held a June-2026 snapshot (8-ID
inventory, `iqe-cost-management-plugin`-centric paths) that drifted from the
#735/#736 rebuild of the public page. Rather than re-mirror, the duplication
was removed in the docs dedup pass (2026-11-01). The prior content is preserved
in git history (through commit `440efc95`).

**Rule for agents:** registry and registration fact edits go to the docs-site
page only; do not restore duplicated content here. The pair still appears in
[docs-site-sync.md](../agents/docs-site-sync.md) so `make docs-sync-check`
keeps both files present.

## Internal-only notes

### Container recommendations — IQE ownership split (verified 2026-11-01)

Container list/detail coverage is intentionally split across two IQE plugins to
avoid duplicate maintenance and matrix drift:

| Plugin | Owns |
|--------|------|
| **iqe-cost-management-plugin** (family) | Detail contract, dual-engine (cost + performance), tag filtering, history, quality, idle detection, business hours (`test_ros_container_detail.py`, `test_ros_idle_detection.py`, `test_ros_business_hours.py`, …) |
| **iqe-ros-ocp-plugin** (fork: `~/dev/koku/iqe-ros-ocp-plugin`) | List filters (cluster, project, workload, container, workload_type), sorting, keyset pagination, settings (thresholds, business hours, HCP correlation), CSV export, notification codes catalog |

Each plugin declares the requirement IDs its own tests use in its **own**
`conf/requirements.yaml` (`iqe_cost_management/…` for the family plugin,
`iqe_ros_ocp/…` for the ROS fork). When adding a new container API behavior,
place the IQE test in the owning plugin above — do not mirror the same
assertion in both plugins unless the behavior spans both surfaces (e.g. a list
field that must also appear on detail).
