# History-Class Retention Declarations Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** History-class tables become plugin-declarable with the days-based cutoff by construction — the RetentionProvider month-cutoff trap (#701) cannot be wired wrong — with zero user-visible change (6-month digests / 90-day history stay) and the unconditional sweep semantics preserved.

**Architecture:** A separate optional trait `HistoryRetentionProvider` (`HistoryRetentionTables() []string`) — only history-owning plugins implement it; the seven digest implementors are untouched. The core history sweep in `RunRetentionSweep` collects plugin declarations, merges them with the core fallback list (which keeps **every current entry** — core-owned tables, the two orphan rollups, and plugin-owned tables as the plugin-less binary fallback, mirroring the documented digest-class overlap), and sweeps the union once with the `ROS_HISTORY_RETENTION_DAYS` cutoff via `SweepPartitionedTables`. `RetentionProvider.SweepRetention` is unchanged for digest-class custom logic (the gpu-timeslicing row-delete precedent stays out of scope).

**Tech Stack:** Go 1.26, internal/plugin (trait), internal/engine/retention.go (sweep), internal/plugins (declarations).

**Issue:** #701 (design validated in its readiness discussion: split-declaration chosen over both-cutoffs — the latter leaves the trap open). ADR-0107 amendment note (the history class adopts its fallback-slice pattern).

## Global Constraints

- **Zero user-visible change.** Retention periods stay 6 months (digests) / 90 days (history); no table's effective retention changes; no migration.
- **Unconditional sweep semantics preserved.** The history sweep runs even when no plugins are registered (tests/tools rely on this): the core fallback list keeps every current entry; plugin declarations ADD to it, overlap harmless (`DROP IF EXISTS` — the documented digest-class pattern).
- **The trap dies by construction.** A table declared via `HistoryRetentionTables` is swept with the history cutoff by the core sweep — there is no code path where an implementer applies the month cutoff to a history table.
- The two orphan rollup tables (`hosted_api_tax_rollups`, `hosted_nodepool_rollups`) stay core-listed — finding them plugin homes is out of scope.
- `make lint` (golangci-lint ≥2.15.0, `bin/golangci-lint`) and `make test-short` green at every task boundary.

## Review Focus

Input classes the fix must handle; each pinned by the named test in its owning task:

1. **Plugin-declared history table gets the days cutoff** — a registered `HistoryRetentionProvider`'s table is swept at the 90-day cutoff, never the 6-month. Pinned by `TestHistoryRetention_PluginDeclarationUsesDaysCutoff` (Task 3; fails pre-fix — the trait doesn't exist).
2. **Zero-plugin fallback intact** — with no plugins registered, every current `historyRetainedTables` entry still sweeps at the history cutoff. Pinned by `TestHistoryRetention_ZeroPluginFallbackSweepsAllCoreTables` (Task 3).
3. **No retention-period drift** — the digest cutoff is exactly `retentionMonths` and the history cutoff exactly `HistoryRetentionDays` for both paths; no table appears in both sweep classes. Pinned by `TestHistoryRetention_NoCrossClassDrift` (Task 4).
4. **The W4 trap fixture** — a plugin implementing the trait for `hcp_capacity_history`-shaped data receives 90 days, not 6 months (the exact confusion caught in W4 planning). Pinned by `TestHistoryRetention_HcpCapacityHistoryGetsNinetyDays` (Task 4).

## Tasks

### Task 1: The trait + the collection loop

- [ ] `internal/plugin/plugin.go`: the `HistoryRetentionProvider` interface — `Plugin` + `HistoryRetentionTables() []string` — with the doc comment stating the contract: declared tables are swept by the core history sweep at `ROS_HISTORY_RETENTION_DAYS` (default 90), never the digest month cutoff; the core fallback list remains the plugin-less binary path (#701; ADR-0107's fallback-slice pattern extended to the history class).
- [ ] `internal/engine/retention.go`: the history sweep collects `plugin.ByTrait[plugin.HistoryRetentionProvider]()` declarations, dedups against the core list, and sweeps the union with the existing history cutoff. Collection failure semantics match the digest path (a provider error is logged and collected, never skips the core fallback sweep).
- [ ] `make test-short`; `make lint`.

### Task 2: Plugin declarations (one line each, self-contained)

- [ ] `pvc`: `pvc_recommendation_quality`. `vm`: `vm_recommendation_quality`. `gpu`: `gpu_mig_recommendation_quality`. `snapshot`: `snapshot_recommendation_quality`. `hcp`: `hcp_capacity_history` (replacing the plugin's "no RetentionProvider" comment with the new trait's pointer). `slo`: `hosted_api_bucket_rollups`, `hosted_worker_pressure`.
- [ ] Each plugin's doc-header trait list gains the trait; the plugin-reference rows follow in Task 5.
- [ ] The core `historyRetainedTables` list keeps every entry, each plugin-owned row gaining a comment: "plugin-declared via HistoryRetentionProvider; kept here as the plugin-less fallback (overlap harmless)."
- [ ] `make test-short`; `make lint`.

### Task 3: Detector tests, RED→GREEN

- [ ] `TestHistoryRetention_PluginDeclarationUsesDaysCutoff` (Review Focus 1): a test plugin implementing the trait for a scratch table; assert the sweep's effective cutoff for that table is the history cutoff. Verify RED first (trait missing → compile failure is the red).
- [ ] `TestHistoryRetention_ZeroPluginFallbackSweepsAllCoreTables` (Review Focus 2): the existing no-plugin path sweeps the full current list — pin the list's contents so a future removal can't silently drop a fallback.
- [ ] Both green after Tasks 1–2.

### Task 4: Cross-class and trap fixtures

- [ ] `TestHistoryRetention_NoCrossClassDrift` (Review Focus 3): digest tables and history tables are disjoint sets in both paths; the two cutoffs derive from their respective configs.
- [ ] `TestHistoryRetention_HcpCapacityHistoryGetsNinetyDays` (Review Focus 4): the hcp declaration sweeps at 90 days — the W4 near-miss, now impossible by construction.
- [ ] `make test-short`; `make lint`.

### Task 5: Docs

- [ ] ADR-0107 amendment note (comment on the issue, not a body rewrite): the history class adopts the fallback-slice pattern via `HistoryRetentionProvider` (#701).
- [ ] `docs-site/architecture/plugin-traits.md` (and the internal pair if one exists): the new trait, one paragraph — when to implement it, the by-construction cutoff guarantee, the core-fallback overlap.
- [ ] Plugin-reference rows for the six declaring plugins: the trait moves from "No — core history sweep owns …" to the declaration.
- [ ] `make docs-lint docs-drift docs-sync-check`.

## Fixture Provenance

- The W4 trap fixture derives from the 2026-10-25 planning catch (recorded in #701 and the hcp plugin comment): `hcp_capacity_history` would have shipped with 6-month retention via the month-cutoff trait.
- The core-list contents (11 tables) are pinned as-is from `internal/engine/retention.go:54-69` — the test asserts the exact set so neither fallback removal nor silent addition can drift.

## Non-goals

- No retention-period changes; no new tables; no migrations.
- No homes for the two orphan rollup tables.
- No change to `RetentionProvider.SweepRetention` or its digest-class semantics (the gpu-timeslicing row-delete precedent stays as-is).
- No bundling with any feature work — this lands alone.
