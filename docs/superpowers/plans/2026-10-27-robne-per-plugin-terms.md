# robne Per-Plugin Term Windows Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** robne resolves term windows per plugin (documented table by default, per-plugin YAML override when configured), matching the server's term resolution; #705's VM and PVC divergences close; the parity comparison reaches 10/10.

**Architecture:** The six documented term tables single-source in librobne (container family and VM already there; PVC added beside its engine); the server's plugin `DefaultTerms()` implementations delegate to those constants (behavior identical, pinned by existing tests); robne gains an effective-terms resolver (`configured-for-plugin ‖ documented-table`, no fall-through) used by all six term-bearing recommend paths, a `plugin_terms:` YAML map, and a read window sized from the maximum effective lookback.

**Tech Stack:** Go 1.26, librobne (CGO-free nested module, vendored into the root module), cobra-style CLI in `cmd/robne`.

**ADR:** [ADR-0346](../../adr/0346-per-plugin-term-windows-shared-contract.md) (normative for the decisions). Issue: #705 (fix) with #706 (validation harness and acceptance gate).

## Global Constraints

- librobne stays CGO-free; `make robne` must keep building with `CGO_ENABLED=0`. Every librobne change requires `go mod vendor` in the root module.
- Server-side behavior must not change: the plugin-`DefaultTerms()` delegation is a refactor of where numbers live, pinned by the existing `internal/engine` term-config tests staying green (run them before and after).
- No numbers are copied by hand: the PVC table's values come from the server plugin's `DefaultTerms()` at implementation time, and the delegation makes the plugin and librobne read one constant thereafter.
- Validation bounds for `plugin_terms:` are mirrored from the server's term-settings enforcement (locate it in Task 1 recon; do not invent bounds).
- Pre-release output change is accepted (ADR-0346): robne VM/PVC recommendations change once the tables are correct; goldens are rewritten deliberately, never hand-edited; the JSON envelope shape does not change.
- `make lint` (golangci-lint; use `bin/golangci-lint` at 2.15.0+) and `make test-short` must pass at every task boundary.
- The spec (`docs/plans/robne-cli-spec.md`) is normative after Task 1 amends it; code and spec move in the same task.

## Review Focus

Input classes this fix must handle correctly; each pinned by the named test in its owning task:

1. **13-day VM window** — long_term (30d lookback, 15d min) must be withheld; medium (15d) clamps to 13; short (7d) uses 7. Pinned by `TestVMTerms_ThirteenDayWindowWithholdsLong` (Task 3; fails pre-fix because robne's long is 15d/min-7).
2. **Window-sensitive PVC** — near-full 9.8 GiB on days 1–2, 6.5 GiB after, 13-day window: short identical, medium uses all 13 days (near_full), long uses all 13. Pinned by `TestPVCTerms_MediumUsesAllAvailableDays` (Task 3; fails pre-fix because robne's medium is the 7-day container window → healthy).
3. **Override isolation (requirement 3)** — `plugin_terms.vm` configured with shifted windows: VM recs change, container recs byte-identical to the no-config run. Pinned by `TestPluginTerms_OverrideIsolation` (Task 5).
4. **Ambiguity** — `terms:` and `plugin_terms.container` both set: hard error naming both keys. Pinned by `TestPluginTerms_AmbiguityError` (Task 5).
5. **Unknown plugin key** — `plugin_terms.snapshot:`: hard error listing the six valid keys. Pinned by `TestPluginTerms_UnknownPluginError` (Task 5).
6. **Path-A read window** — postgres input with 40 days of stored digests: VM long_term (30d) sees 30 days, not 15. Pinned by `TestReadWindow_MaxEffectiveLookback` (Task 4).

## Tasks

### Task 1: Spec amendment + server-bounds recon

- [ ] Recon: locate the server's term-settings validation bounds (the Settings API path that enforces 1–90 day windows and min ≤ window) and the PVC plugin's `DefaultTerms()` values; record both in the spec amendment.
- [ ] Amend `docs/plans/robne-cli-spec.md`: the `plugin_terms:` key (six plugin entries, list shape, name normalization `short`↔`short_term`), `terms:` becomes explicitly container-scoped, the both-keys ambiguity error, unknown-plugin-key error, validation bounds (mirrored), and the read-window rule (max effective lookback). Update the reserved-keys section: `plugin_terms:` is a deliberate partial unlock per ADR-0346.
- [ ] Verify: `make docs-lint docs-drift docs-sync-check`.

### Task 2: librobne single-source consolidation

- [ ] Add the PVC documented term table to `librobne/pvc` (values from the server plugin's `DefaultTerms()` — Task 1 recon; accessor named consistently with `vm.DefaultVMTermWindows()`).
- [ ] Make the server plugins' `DefaultTerms()` implementations delegate to the librobne constants (container/namespace/node/gpu → `librobne/engine.DefaultTerms()`; vm → `librobne/vm`; pvc → the new table). Where a plugin's table already equals the librobne constant, the delegation is a no-op refactor.
- [ ] Run `internal/engine` term-config tests before and after: identical results (characterization).
- [ ] `go mod vendor`; `make test-short`; `make lint`.

### Task 3: Detector tests, RED

- [ ] Write `TestVMTerms_ThirteenDayWindowWithholdsLong` (Review Focus 1): 13-day VM digest fixture through robne's recommend path; assert long_term absent, medium present with 13-day data, short with 7-day data. Verify it FAILS pre-fix for the #705 reason (robne emits long_term — wrong table), reading the failure message to confirm the mechanism, not an incidental property.
- [ ] Write `TestPVCTerms_MediumUsesAllAvailableDays` (Review Focus 2): the window-sensitive fixture (near-full days 1–2, 6.5 GiB after, 13 days); assert medium matches the all-13-days computation, short matches 7-day, long matches 13. Verify RED for the #705 reason (robne medium = 7-day window → healthy classification).
- [ ] Commit the failing tests with the observed failure output recorded in the plan execution ledger.

### Task 4: The fix — effective-terms resolution and call sites

- [ ] Implement the effective-terms resolver in `cmd/robne` (or librobne where both sides could use it): `effectiveTerms(plugin, cfg)` → `plugin_terms[plugin]` ‖ documented table for that plugin (librobne constants from Task 2). No fall-through between plugins.
- [ ] Wire all six term-bearing recommend paths to resolve per plugin: container, namespace, node, gpu (previously `ec.Terms`), vm, pvc. Behavior for the container family is unchanged at defaults (their documented table IS `ec.Terms`' source) — pin with a defaults test asserting all six effective tables equal the documented constants when no `plugin_terms` is set.
- [ ] Read window: `digestWindow` sized from the max lookback across all effective tables (Review Focus 6).
- [ ] Task 3 tests GREEN. New test: `TestReadWindow_MaxEffectiveLookback`.
- [ ] `make test-short`; `make lint`. Regenerate nothing yet — goldens are Task 6.

### Task 5: `plugin_terms:` config surface

- [ ] Parse `plugin_terms:` (map, six valid keys, list shape per entry, name normalization); whole-key-replacement overlay per plugin entry.
- [ ] Validation: bounds mirrored from Task 1 recon; min ≤ window per entry.
- [ ] Errors: `terms:` + `plugin_terms.container` ambiguity (Review Focus 4); unknown plugin keys including term-less plugins (Review Focus 5).
- [ ] Tests: `TestPluginTerms_OverrideIsolation` (Review Focus 3 — VM overridden, container byte-identical), `TestPluginTerms_AmbiguityError`, `TestPluginTerms_UnknownPluginError`, `TestPluginTerms_DefaultsWhenUnset`, plus override-path coverage per plugin.
- [ ] `make test-short`; `make lint`.

### Task 6: Goldens, sample, docs

- [ ] Regenerate goldens (`make robne` + the golden regeneration path); verify the diffs are exactly the VM/PVC term-window changes (spot-check one VM row's data_days against the new table).
- [ ] `cmd/robne/robne.yaml.sample`: commented `plugin_terms:` example. This stays the **single canonical sample file** — no second YAML sample; the copy-the-full-block contract and the whole-key-replacement warnings are built around one file.
- [ ] `cmd/robne/README.md` and `docs-site/features/robne-cli.md`: the three term groups, `plugin_terms:` usage (short inline snippet matching the sample's commented shape), the `terms:` container-scoping clarification, the pre-release output-change note.
- [ ] Cobra `Example` field on the **root command**: a compact `robne.yaml` config example (a `sizing:` fragment plus the commented `plugin_terms:` shape mirroring the sample, the search-path one-liner, and pointers to `robne.yaml.sample` and the spec for the full contract). This is the only vehicle for YAML content in the manpages/markdown — `GenManTree`/`GenMarkdownTree` render it into the EXAMPLES sections of both trees; never hand-edit generated files. Reference-grade depth only: overlay semantics and per-plugin tables stay in the spec and the curated public page.
- [ ] `make man` (the Example field changes generated output — regeneration expected) and `make man-check` green. `make docs-lint docs-drift docs-sync-check`.

### Task 7: Parity re-run — the acceptance gate

- [ ] Run the parity validation per `.cursor/skills/server-robne-parity/SKILL.md` with detector-grade payloads: the window-sensitive PVC fixture and a <15-day VM window (stock templates cannot detect this bug — ADR-0346).
- [ ] 10/10 green, including VM term coverage and PVC medium on the window-sensitive fixture. Any red is a finding, not a flake: root-cause before proceeding.
- [ ] Record the run on #706 (method + verdicts) and the fix completion on #705 (root cause, commits, the two detector tests, the parity gate).
- [ ] The org's term settings on the comparison cluster must be default for a valid comparison (the skill's pre-check); if tuned, either reset or mirror via `plugin_terms` — record which.

## Fixture Provenance

- **VM 13-day fixture:** derived from the 2026-10-27 cold-start parity evidence (cluster `009ee48a`, 13 days, `data_days` arithmetic short=1/medium=7/long=13 under the wrong table — the test asserts the corrected 7/13/withheld shape). Guest-agent-bearing VMs so the memory branch is exercised (the 17-GiB raw from guest actuals, per the #705 explain evidence).
- **Window-sensitive PVC fixture:** the recipe from the #706 run records (near-full PVC 9.8 GiB days 1–2, 6.5 GiB after, 13-day window; expected: short identical, medium divergent pre-fix, long identical). Stock `ocp_report_pvc_rightsizing.yml` is window-insensitive by construction and must not be used as a detector.
- **Server-side expected values:** taken from the live server's output on the same payloads during the 2026-10-27 runs (recorded in `.superpowers/tmp/parity-20261027/results/` and the #706 comments) — the fix's target is matching those.

## Non-goals

- No entity settings schemas beyond terms (the reserved-keys posture stays; the future unlock supersedes `plugin_terms` naturally).
- No server-side term-config changes (three-tier resolution, Settings API, DB caching untouched).
- No change to term-less plugins (quota, cluster-quota, snapshot, hcp-capacity).
- No robne hcp stdout log-leak fix (separate small bug; ride only if the owner explicitly folds it in).
