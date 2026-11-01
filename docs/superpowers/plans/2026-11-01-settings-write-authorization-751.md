# Settings Write Authorization (#751) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans for native execution or superpowers:subagent-driven-development if the owner chooses delegation. Execute task-by-task and tick completed steps. Do not commit, push, deploy, or close issues without the explicit owner instruction required by AGENTS.md.

**Goal:** A settings permission denial must terminate request processing before persistence, pruning, recalculation, reship, or cache mutation, while preserving legitimate access and the current 403 response.

**Architecture:** Keep the existing `requireSettingsWrite(c echo.Context) error` interface and all caller guards. On denied access, send the existing JSON; propagate a response-write error if present, otherwise return `echo.ErrForbidden`. Production permission aggregation, inheritance, route registration, request schemas and settings-lock policy stay unchanged. Add broad stateful regression coverage and evaluate the associated consumer/spec/documentation deliverables.

**Tech Stack:** Go/Echo, PostgreSQL testcontainers, existing recalculation/reship test seams, OpenAPI JSON, Python IQE/pytest, AsciiDoc/Bruno.

**Spec:** [#751](https://github.com/pgarciaq/ros-ocp-backend/issues/751) and its handoff comment; coordination parent [#780](https://github.com/pgarciaq/ros-ocp-backend/issues/780). This plan implements the narrow scope discussed with the owner; it does not implement #752–#754 or deferred #738/#742.

## Steps

- [x] Implement and mutation-check the ROS guard fix with unit, routed persistence, side-effect and OpenAPI contracts.
- [x] Add IQE API-only state-preservation coverage and document its isolated-identity execution prerequisites.
- [x] Update the API cheat sheet/Bruno references and generated deliverables.
- [x] Obtain independent pre-commit security review, run verification, and post the evidence-based completion report for owner review.

## Global Constraints

- Branch: `pgarciaq-rosocp-superpowers-phase17` in every touched repository. Preflight status before edits; preserve unrelated work. Do not create/switch worktrees or branches without owner approval.
- ROS baseline: `3061a76f64c4db30756f4e7a1d48f6814276618e`. IQE baseline: `17676074aed88a396cf59a0b87368982ce8f21c2`. Cheat-sheet baseline: `facd875e9ff1984cace7c409b6acb99fbe9acb8f`.
- The uncommitted v15 audit report is separate existing work; never stage or discard it as part of this fix without explicit instruction.
- Exact denial: HTTP 403, one JSON object with `status: "error"`, `message: "User does not have permission to modify settings"`. Do not append an additional error body.
- Preserve `settings.write=["*"]`, the global `"*"` permission key, intentional RBAC-disabled behavior, GET behavior, per-field/global locks, deprecated alias headers and valid business-hours URL validation.
- Twenty helper callers cover 34 documented PUT/DELETE operations; parameterized alias/type variants and three business-hours scopes must not disappear from coverage.
- Use legitimate `x-rh-identity` and deterministic authoritative RBAC responses in integration tests. No Keycloak client-secret debugging or fabricated cross-tenant permission bypass.
- Test with disposable local databases; IQE never queries databases directly. No live settings writes on the shared lab tenant. Live testing requires an explicitly approved isolated target, suitable identities and current rebuilt/deployed images.
- No new migration, production dependency, authorization-policy redesign or feature flag. A new ADR is not expected for this routine control-flow repair; reconsider only if the implementation proposes a substantive design change.
- Issue bodies remain unchanged. Claim/plan/implementation/verification updates are comments; completion tables distinguish verified from asserted results.

## Review Focus

1. A successful JSON response write returns nil; a denial still must stop every caller. Test the production guard and representative real mutations, not Echo routing in isolation.
2. An invalid/incomplete PUT, empty DELETE target, or enabled lock can mask the bug. Use valid unlocked requests and seeded existing rows, with independently checked expected state.
3. Async work may outlive the HTTP response. Drain existing jobs before baseline snapshots and wait for completion before asserting absence of recalculation/reship.
4. A response writer can fail. Preserve the actual error and still prevent continuation; preserve the single committed denial body on successful writes.
5. Alias/type variants and business-hours org/cluster/namespace wrappers share helpers. Inventory actual registrations and exercise each distinct mutation/side-effect domain without testing only 20 source call sites.

## Task 1: ROS control-flow fix, regression matrix and public contract

**Files:**
- Modify `internal/api/settings_rbac.go`.
- Extend `internal/api/settings_rbac_test.go` for production-guard unit contracts and strengthen existing denial assertions.
- Create `internal/api/settings_rbac_persistence_test.go` for the routed, stateful matrix (external `api_test` package; production route registration and middleware).
- Extend `internal/api/openapi_contract_test.go` for served/documented settings-denial coverage.
- Modify `openapi.json`, `CHANGELOG.md`, `docs-site/features/configurable-thresholds.md`, and `docs-site/testing.md` where denial/state-preservation behavior is described.
- Update the v15 audit finding's **remediation status only after verification**; preserve its historical evidence. Do not restore full internal stub copies.

**Interfaces:**
- Consumes `hasSettingsWritePermission`, `api.RegisterV1RoutesForTest(v1 *echo.Group, bhTrigger reship.Triggerer)`, `testutil.SetupTestDB`, `engine.SetThresholdRecalcHookForTest` / `ClearThresholdRecalcHookForTest`, and existing async-job/test cleanup primitives.
- Produces unchanged `requireSettingsWrite(c echo.Context) error`: nil only for an allowed write; actual writer error on serialization/write failure; non-nil `echo.ErrForbidden` after a successfully emitted denial.

- [x] Write `TestRequireSettingsWrite_DeniedStopsCaller`: read-only/absent/non-wildcard write grants produce the exact 403 JSON, a non-nil error and zero continuation-side effects. Add allowed writer/global wildcard/RBAC-disabled controls and a failing-response-writer case. Register cleanup before environment changes so config resets after restored environment. Do not use `t.Parallel` with global config/pool/hooks.
- [x] Run `go test -mod=readonly -short ./internal/api -run '^TestRequireSettingsWrite' -count=1 -v`. Confirm RED specifically because denied successful-response writes return nil/reach the protected continuation; record the exact output. Writer-failure and permitted controls should already characterize valid behavior.
- [x] Write the persistence matrix with actual registered routes, genuine non-admin identity and only an external RBAC HTTP double. Cover typed thresholds `container/namespace/node/gpu/pvc`, deprecated aliases with valid types, generic terms with actual supported type variants, VM settings/terms, idle, quota, cluster quota, snapshot, HCP correlation and BH org/cluster/namespace PUT/DELETE. Force intended features enabled and locks off for permission-negative cases.
- [x] For each domain, seed a real non-default override, prove it exists, use a different valid PUT body or DELETE it as the denied caller, and compare actual database rows (not merely GET/cache or status). Assert no recalculation, BH digest pruning, reship call/pending stamp or cache side effect where the actual handler provides one. Positive writer controls prove the same fixture really can mutate and trigger its intended side effects. Retain distinct lock-negative controls.
- [x] Run `go test -mod=readonly ./internal/api -run 'TestSettingsRBAC|TestRequireSettingsWrite' -count=1 -v` on the pre-fix guard. Confirm RED names persistence/side-effect changes, not invalid fixtures, missing partitions or incidental response formatting.
- [x] Apply only the guard change described above. Run the same focused short/integration commands to GREEN. Do not rewrite 20 caller interfaces or permission inheritance.
- [x] Mutation-check the new tests using a temporary Go overlay of the original guard rather than stashing/resetting the shared checkout. Observe the same named failure mechanism; remove only audit-owned temporary artifacts. Restore the real fixed source selection and rerun GREEN.
- [x] Add authorization-denied 403 documentation to all 34 existing settings mutation operations, preserving any lock-specific descriptions and success/error schemas. Add `TestOpenAPISettingsMutations_DocumentPermissionDenied403` using the existing `openAPIPathItem.Put/Delete.Responses` structures plus actual routed denial bodies; the existing `getResponseSchema` helper is GET/200-only and must not be assumed to inspect PUT/DELETE/403. Test the consumed/served contract rather than a prose grep. Update CHANGELOG and the public RBAC/testing guidance; no UI runtime or API body change is intended.
- [x] Run `make test-short`, the full targeted settings integration/lock suites, `make test`, and `make docs-lint docs-drift docs-sync-check`. Record every failing test by name. Existing #779/compatibility failures are not authorized drive-by fixes and do not become a claimed green suite.

## Task 2: IQE state-preservation regression

**Files:**
- Create `iqe-ros-ocp-plugin/iqe_ros_ocp/tests/rest/test_settings_rbac.py`.
- Extend `iqe-ros-ocp-plugin/iqe_ros_ocp/fixtures/rbac_fixtures.py` with a settings-specific context; do not depend on namespace recommendation ingestion just to test settings permission.
- Update `iqe-ros-ocp-plugin/README.md` with the explicit isolated-identity invocation and prerequisites.

**Interfaces:**
- Consumes the existing `application.copy_using(user=UserSpec)` context manager and raw Cost Management REST client call pattern; no DB accessor.
- Produces a function-scoped `settings_rbac_context` fixture yielding the administrator and read-only application contexts for the **same disposable test organization**, plus cleanup ownership of its temporary snapshot override. A resolved test-user credential mapping is supplied by external IQE config as `ros_ocp.settings_rbac_readonly_user`; never commit credentials or auto-provision platform/Keycloak users.
- This credential/config mechanism is an implementation proposal for owner review, not a pre-existing configured persona. No suitable preconfigured read-only user was found in the inspected ROS/Cost Management profiles.

- [x] Add `test_settings_rbac_readonly_put_preserves_snapshot_override` and `test_settings_rbac_readonly_delete_preserves_snapshot_override`, tagged `requirements: cost_ros_ocp`. Use a caller-supplied isolated org/read-only profile and the existing current-env/auth transport, so the mechanism supports both SaaS and on-prem rather than hardcoding a platform RBAC provisioner.
- [x] Require an explicitly selected manual/isolated test context before mutation. Seed `stale_days=120` through the authorized API; denied PUT attempts `stale_days=121`; both denied methods must return 403 and an authorized fresh GET must still return 120. Snapshot GET resolves from DB (verified callee), so no stale settings cache is used as the evidence. Clean up only the temporary override in that disposable org, in `finally`, including on failure.
- [x] Add writer/read preconditions and an authorized positive mutation control. Wrong credentials, wrong org, unexpected API errors and a configured-but-writable supposed read-only persona must fail visibly, not become skip/pass. No broad exception swallowing.
- [x] Run collection, `python3 scripts/check_requirements.py`, and `python3 -m unittest discover -s tests -v` in the owning IQE environment. Run the selected deployed tests only after target/identity/deployment approval: `iqe tests plugin ros_ocp -m cost_ocp_on_prem -k settings_rbac` (or the matching SaaS selection).
- [x] Prove RED against an isolated pre-fix target and GREEN against the fixed target when available. If runtime identity/target provisioning is unavailable, report this E2E execution as **not verified**, name the prerequisite, and obtain an explicit owner-approved tracked follow-up before representing #751 as fully complete. Missing live evidence is not permission to weaken assertions or mutate a shared tenant.

## Task 3: Operator reference examples and final handoff

**Files:**
- Modify `costmgmt-api-cheatsheet/costmgmt-api-cheatsheet.adoc`: add a shared settings-write RBAC section adjacent to the platform-lock section, with the exact existing denial JSON and no-mutation guarantee.
- Update documentation blocks in `bruno/Optimizations/PUT Settings Snapshot.bru` and `bruno/Optimizations/DELETE Settings Business Hours.bru` with the permission prerequisite and safe state-verification guidance; preserve request URL/body/auth fields.
- Regenerate `costmgmt-api-cheatsheet.html` and `costmgmt-api-cheatsheet.pdf` using that repo's existing Makefile, loading applicable artifact guidance before generation. No new Containerfile/build tooling.

**Interfaces:** unchanged request/response wire contract from Task 1; this task documents the same settings role versus environment-lock distinction without introducing a new endpoint or credential format.

- [x] Apply the reference/example updates and run `make all` in the cheat-sheet repository. Inspect rendered HTML/PDF and record generator availability/results; do not claim a rebuilt artifact from only editing its source.
- [x] Obtain a fresh independent security review before any commit: reviewer must answer **“How could these tests pass while the permission bypass lives?”** Check malformed-body, empty-row, lock and async masking, all callers, positive access and response-write-error paths.
- [x] Recheck every touched repo's branch/diff/status and relevant suites. Verify spec JSON, documentation gates and IQE metadata. Preserve unrelated audit report/history and any user changes.
- [x] Post implementation/evidence comments on #751 and update parent #780 with per-repo SHAs only when such commits actually exist. Produce the full properly-implement-feature completion table: implementation/tests/spec/IQE/reference docs/Bruno/CHANGELOG done or specifically justified/tracked; UI placement/state transitions dismissed because no UI element/list flow changes; ADR/migration dismissed only while the narrow repair remains unchanged; performance audit dismissed because no optimization phase was requested.
- [x] Present the diff, executed versus skipped/failed verification, outstanding live-E2E prerequisites, and reviewer verdict to the owner. **Stop for explicit commit/push/close instructions.** When a close is authorized, post the completion table with it; do not close with a bare commit reference.

## Execution choice and self-review

Recommended: **native execution** for this tightly coupled guard/test-matrix change, with an independent pre-commit security reviewer. Subagent-driven execution remains available if the owner prefers task-by-task delegation. No automatic frequent commits override the repository approval gate.

Self-review: all #751 acceptance items map to the three tasks; the 20 callers/34 documented operations, denied persistence and async effects, valid-role/lock/transport controls and both deployment identities are covered. No new production interfaces, migration or authorization policy is invented. The only pending execution prerequisite is an approved isolated live target/read-only profile for actual IQE RED→GREEN evidence; collection is explicitly not an E2E pass.
