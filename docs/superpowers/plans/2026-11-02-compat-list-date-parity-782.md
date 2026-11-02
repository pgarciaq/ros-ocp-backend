# Compatibility List Date Parity (#782) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans for native execution or superpowers:subagent-driven-development if the owner chooses delegation. Execute task-by-task and tick completed steps. Do not commit, push, deploy, or close issues without explicit owner authorization.

**Goal:** In native mode, make the legacy `/recommendations/openshift/container` list use the documented current-month `updated_at` date window while preserving its compatibility response shape and UUID fallback; retain Kruize-mode date behavior.

**Architecture:** Keep the compatibility list query/model/output path and give it a separate native-mode date mapper. Factor only the existing compatibility parser into a same-file helper parameterized by a static date column/default-end policy; do not refactor the native or namespace mappers. Select the new handler for the `/container` list alias only when native recommendations are active; Kruize mode keeps the current legacy handler. Do not route the compatibility alias wholesale through the native DTO handler: it inner-joins `clusters`, adds `source_id`, and omits rows during missing cluster metadata.

**Tech Stack:** Go/Echo, PostgreSQL testcontainers, IQE pytest, OpenAPI JSON, MkDocs, AsciiDoc.

**Spec:** [#782](https://github.com/pgarciaq/ros-ocp-backend/issues/782); the owner-selected scope is in [the decision comment](https://github.com/pgarciaq/ros-ocp-backend/issues/782#issuecomment-6579353155), with route/consumer evidence in [the follow-up](https://github.com/pgarciaq/ros-ocp-backend/issues/782#issuecomment-6579553341).

## Global Constraints

- Repository: `ros-ocp-backend`, branch `pgarciaq-rosocp-superpowers-phase17`, working base `86131864312ab02533cdf03b8e77ac8f1cb1ae00` (includes #779 fixture-test commits).
- Native-mode default window: `first day of current UTC month <= recommendation_sets.updated_at < now UTC + 1s`; explicit start is inclusive, explicit end is inclusive via `+24h`.
- Preserve Kruize-mode `monitoring_end_time` filtering.
- Preserve compatibility serialization, `source_id: ""`, scalar-subquery alias lookup and UUID fallback when the requesting org has no matching `clusters` row.
- Scope changes to the `/container` **list** alias only. Do not change detail aliases or the canonical `/openshift` handler.
- Keep cluster alias lookup org-scoped. Test identical cluster UUIDs with distinct aliases across orgs; do not rely on insertion order or lexical ordering.
- Do not write shared or production data. Use the repository's disposable testcontainers fixtures.
- Preserve unrelated work in `docs-site/testing/validating-native-engine.md` and `docs/audit/adversarial-review-v15-2026-11-01.md`.
- Public contracts live in `docs-site/`; bump Class A `Last verified` dates. If the cheat-sheet AsciiDoc changes, regenerate HTML/PDF with its existing Makefile.
- ADR-0065 (Kruize-compatible response shape) and ADR-0105 (container handlers remain in core) are constraints. No new ADR unless implementation departs from those decisions.
- No commit, push, deployment, or issue closure without separate explicit authorization.

## Review Focus

1. Month boundary: prior-month `monitoring_end_time` plus current-month `updated_at` is included on the native-mode alias; current-month monitoring time plus prior-month `updated_at` is excluded.
2. Missing cluster metadata is not a recommendation for a container without a cluster: keep the row and render UUID fallback.
3. Same cluster UUID in two orgs must resolve only the requesting org's friendly alias.
4. Kruize mode must continue using `monitoring_end_time` and the legacy handler.
5. Date parity must not change the compatibility response fields or replace its query with the native inner-join DTO.

---

### Task 1: Native-mode compatibility date mapper and route

**Files:**
- Modify `internal/api/utils.go` with a shared container-query parser and separate legacy/native-compat date-column policies.
- Modify `internal/api/handlers.go` to share the compatibility list response path between the existing and native-compat mapper entry points.
- Modify `internal/api/server.go` to override only the `/recommendations/openshift/container` list registration in native mode.
- Modify `internal/api/register_test_routes_test.go` to mirror production route selection.
- Modify `internal/api/api_test.go` to pin both mapper contracts.
- Create `internal/api/compat_list_date_parity_integration_test.go` with HTTP/date-boundary and alias-scoping regressions.
- Correct the stale fallback comment in `internal/api/contract_test.go`; do not rework the #779 alias/fallback test already committed as `c437a057`.

**Interfaces:**
- Keep `MapQueryParameters(echo.Context)` and its `recommendation_sets.monitoring_end_time` behavior for Kruize/legacy requests.
- Add `MapNativeCompatQueryParameters(echo.Context) (map[string]interface{}, error)` for `recommendation_sets.updated_at`, with the native `now+1s` upper-bound default.
- Keep `GetRecommendationSetList(echo.Context)` as the legacy wrapper; add `GetNativeCompatRecommendationSetList(echo.Context) error` as the native-mode wrapper. Both call a shared private list handler with the query-mapper function so response serialization/model behavior stays identical.

- [x] **Step 1: Write the RED month-boundary route test.** In the new integration test file, start from `setupContractTestApp`; set `monitoring_end_time` to the prior month while leaving recommendation `updated_at` in the current month. Request both `/recommendations/openshift/container` and `/recommendations/openshift` without dates. Assert the alias returns rows and the same container IDs as the canonical route, while keeping compatibility-only fields (including empty `source_id`).
- [x] **Step 2: Run RED against current source.** Expected: canonical native/fallback path returns the row; `/container` returns HTTP 200 empty because it filters prior-month `monitoring_end_time`.
- [x] **Step 3: Add the reverse-boundary control.** Set `updated_at` to the prior month and `monitoring_end_time` to the current month; both native-mode paths must exclude the row by the updated-at filter.
- [x] **Step 4: Pin mapper separation.** Add unit coverage proving native-compat emits `recommendation_sets.updated_at` bounds and existing `MapQueryParameters` still emits `recommendation_sets.monitoring_end_time` bounds; preserve inclusive `end_date` behavior.
- [x] **Step 5: Implement the shared compatibility handler and native-only alias override.** Reuse compatibility serialization and query model; only the date mapper differs. Kruize mode and detail routes remain on their existing handlers.
- [x] **Step 6: Correct the stale direct-list comment.** The clusters row is present in `setupContractTestApp`; the actual missing-row UUID fallback is now pinned by the orphan branch added in #779 commit `c437a057`.
- [x] **Step 7: Pin org-scoped alias lookup.** In the new integration test, seed the same cluster UUID with a different alias in a second org; assert only the requesting org's alias is returned regardless of insertion order. Follow `internal/api/handlers_alias_scoping_test.go`'s adversarial pattern.
- [x] **Step 8: Run focused tests and mutation-check.** Run mapper tests, the new route/tenant controls and all seven #782 contract tests. Select the original alias mapping with an overlay and confirm the boundary test fails for the named date-column mismatch; restore fixed selection and rerun GREEN.
- [x] **Step 9: Run the full repository suite once.** Fresh `env GOMAXPROCS=4 GOFLAGS=-p=2 make test` completed successfully on the revised working tree; log `.superpowers/sdd/2026-11-02-compat-list-date-parity-782/task1-full-suite-final.log` contains 98 package result lines and no `FAIL` or `--- FAIL:` lines.

### Task 2: IQE fixture and external contract handoff

**Files:**
- Modify `openapi.json` alias description to document native-mode date parity and compatibility response limits; no endpoint, parameter, schema, or status changes.
- Modify `docs-site/ui-integration-guide.md` and `docs-site/features/container-recommendations.md` with the compatibility list's native/Kruize date behavior and legacy payload differences; bump Class A verification dates.
- Modify the parallel internal contract page `docs/features/container-recommendations.md` with the same API behavior.
- Modify `CHANGELOG.md` for this user-visible compatibility-list correction.
- Modify `costmgmt-api-cheatsheet/costmgmt-api-cheatsheet.adoc` to replace the overbroad “Legacy aliases (identical behavior)” statement, then regenerate HTML/PDF.
- No Bruno change unless an existing request uses `/recommendations/openshift/container`; the current container-list request uses the canonical route.
- No koku-ui code change: current container table uses the canonical route and sends no date parameters.

- [x] Cross-repo IQE check: no test code change is expected. `test_new_container_endpoint.py` uses a fixture that creates `n_days` plus today's data, and `librobne/pgrec/write.go` refreshes `updated_at` with `now()`. Record this evidence-backed dismissal; do not run source-creating IQE tests against a shared target.
- [x] Update OpenAPI/public docs to state native-mode compat-list dates filter `updated_at`, Kruize mode retains `monitoring_end_time`, and the response remains compatibility-shaped rather than byte-identical to the native DTO.
- [x] Mirror the date/filter and compatibility-alias facts into the internal container-recommendations page.
- [x] Update CHANGELOG and the API cheat-sheet source, then regenerate its HTML/PDF. No Bruno alias request exists; the current container-list request uses the canonical route.
- [x] Run `make docs-lint docs-drift docs-sync-check`; run cheat-sheet `make all` and inspect generated outputs if AsciiDoc changes.
- [x] Recheck each touched repo: ROS and cheat-sheet diffs pass `git diff --check`; ROS full suite passes; docs checks and generated HTML/PDF were verified. No IQE code change was warranted.
- [x] Post verified implementation/evidence on #782 in [the implementation and verification comment](https://github.com/pgarciaq/ros-ocp-backend/issues/782#issuecomment-6584850946). No commit, push, deployment, or issue closure is included in this plan.

## Explicit Non-goals

- Do not route `/container` through the native DTO handler or inner-join away compatibility rows.
- Do not change Kruize-mode date semantics, compatibility fields/UUID fallback, detail aliases, or canonical `/openshift` behavior.
- Do not change unrelated #779 retention fixtures or claim the full suite is green.
- Do not infer customer impact for `/container` clients beyond the documented API and observed IQE smoke client; other external clients remain unverified.
