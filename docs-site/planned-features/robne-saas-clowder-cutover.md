# robne SaaS Clowder cutover

## Status

Planned. This page tracks the intended SaaS deployment transition from
`ros-ocp-backend + kruize` to `ros-ocp-backend` running robne directly.

## Why this exists

The recommendation engine has evolved, but SaaS deployment still includes a
separate Kruize service path. The cutover simplifies operations by reducing
moving parts while preserving existing ROS-OCP API and tenant behavior.

## Scope

This page covers:

- Clowder template updates in `ros-ocp-backend` (`clowdapp.yaml`)
- app-interface rollout updates for `insights/ros/deploy-clowder.yml`
- staged rollout, validation, and rollback expectations

This page does **not** decommission unrelated ROS services.

## Current deployment shape (high-level)

The ROS umbrella deployment currently includes multiple templates, including:

- `ros-backend` (RHEL ROS service line)
- `ros-ocp-backend` (this service line)
- `kruize` (separate service path targeted for removal in this cutover)

## Target deployment shape

- Keep `ros-ocp-backend` as the service entry point
- Run recommendations via robne in `ros-ocp-backend`
- Remove/disable separate `kruize` deployment once validation gates pass
- Remove Kruize-specific poller/dependency paths no longer needed

## Rollout model

1. Stage deployment updates first
2. Validate ingestion, recommendation quality, API behavior, and dashboards
3. Promote to prod with a documented rollback path

## Key workstreams

- **Clowder template work** (`ros-ocp-backend` repo)
  - robne-only env/config cleanup
  - probe and parameter consistency
  - removal of hard Kruize dependencies

- **app-interface work** (`service/app-interface`)
  - stage/prod ref and parameter updates
  - disable/remove `kruize` resource template at the right point
  - capacity tuning after poller/Kruize retirement

- **Validation and rollback**
  - stage soak criteria
  - production go/no-go checks
  - rollback checklist

## Tracking

- Epic: [#450](https://github.com/pgarciaq/ros-ocp-backend/issues/450)
- ClowdApp changes: [#451](https://github.com/pgarciaq/ros-ocp-backend/issues/451)
- app-interface changes: [#449](https://github.com/pgarciaq/ros-ocp-backend/issues/449)
- Validation and rollback: [#448](https://github.com/pgarciaq/ros-ocp-backend/issues/448)
- Cost-integration follow-up: [#452](https://github.com/pgarciaq/ros-ocp-backend/issues/452)

## Internal detail

For implementation-level details, use:

- `docs/plans/robne-saas-clowder-gap.md`

