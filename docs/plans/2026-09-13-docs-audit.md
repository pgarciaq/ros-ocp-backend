# Full Docs Audit Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate outdated statements across `docs-site/` (and mirrored `docs/` facts) via per-area honesty-exercise audits, with every fix second-sourced against code.

**Architecture:** Independent per-area batches, each producing a discrepancy matrix plus minimal merge-first doc edits plus docs-gate verification. Batches are independently reviewable and committable, so a rejection in one never blocks the others.

**Tech Stack:** MkDocs (`docs-site/`), Go source of truth (`internal/`, `cmd/`), docs gates (`make docs-lint docs-drift docs-sync-check`), grep/glob for second-sourcing.

**Spec:** Team request 2026-09-13 (scope "Full audit", prompted by stale `recommendation-poller` guidance in `docs-site/quickstart.md`) + `docs/agents/docs-site-sync.md` (sync rules) + Class-A conventions in `docs-site/contributing.md`.

## Global Constraints

* Merge-first edits: read the full page first, keep all structure that is still true. Line count may drop when content is genuinely obsolete (e.g. a removed feature or implementation detail needs no documentation) — but flag the trim with justification in the batch proposal and get explicit approval BEFORE trimming a page. Unexplained shrinkage is a plan failure.
* Note: this relaxes the honesty-exercise skill's same-or-up line-count rule — user instruction takes precedence.
* Every factual change second-sourced (code grep plus one independent doc location).
* Bump `> **Last verified:**` to the edit date on every touched Class-A page.
* Verify each batch: `make docs-lint docs-drift docs-sync-check` (plus targeted greps).
* No commit/push without explicit go-ahead per batch; push only to fork `pgarciaq/ros-ocp-backend`, never `origin` (upstream, read-only).
* Customer-relevant facts mirrored across `docs/` ↔ `docs-site/` pairs (adapt links, never wholesale copy); `docs/agents/`, ADRs, plans, archives stay internal-only.
* Phase-branch rule: live pointers go to the current phase branch; point-in-time results/archives keep their branch.
* Scope to the demonstrated case: no drive-by rewrites; "verified current, stamp bumped" is a valid audit outcome.

---

### Task 1: Poller/Kruize sweep (known-stale, deterministic)

**Files:**
* Modify: `docs-site/contributing.md:61,156,164` and root `CONTRIBUTING.md:61,156,164` (mirrored pair — same fix both sides)
* Modify: `docs-site/development.md:65,69`
* Modify: `docs-site/quickstart.md:60` (`phase14` checkout pointer → current phase branch; second source: page header plus `git branch --show-current`)

**Interfaces:**
* Consumes: Batch 0 (quickstart poller fix, commit `86b3d38a`) as wording precedent.
* Produces: Kruize-only qualifier wording reused by later batches.

- [ ] **Step 1: Record each stale claim verbatim**

Read `docs-site/contributing.md` around lines 61, 150-170; `docs-site/development.md` around lines 60-75; `docs-site/quickstart.md` around line 60. Note the exact claim in each spot (e.g. contributing.md:61 "Computes recommendations from digests on schedule" — inaccurate even for Kruize: the poller fetches results from Kruize via `kruize.Update_recommendations`, it does not compute from digests).

- [ ] **Step 2: Apply Kruize-only qualifiers**

Mirror the approved quickstart wording: the native processor computes recommendations inline; `recommendation-poller` only consumes `rosocp.kruize.recommendations` when `ROS_ENABLED_PLUGINS=kruize`. Fix `contributing.md:61`'s "computes from digests" description. Apply the identical fact to root `CONTRIBUTING.md` (adapted links only, no prose fork).

- [ ] **Step 3: Fix the quickstart branch pointer**

`docs-site/quickstart.md:60`: `git checkout pgarciaq-rosocp-superpowers-phase14` → current phase branch (verify with `git branch --show-current`; page header already names the current branch).

- [ ] **Step 4: Run docs gates**

Run: `make docs-drift docs-sync-check` and `./scripts/check-docs-lint.sh --soft` filtered to touched files.
Expected: no new findings (the `changelog.md` hard-fail is pre-existing until Task 2).

- [ ] **Step 5: Propose diff and wait**

Post the diff for review. Commit only on explicit go-ahead (one commit for the batch).

### Task 2: Changelog lint failure (current docs-gate red)

**Files:**
* Modify: `docs-site/changelog.md` (3 broken `robne-cli.md` links with doubled `docs-site/` prefix)

- [ ] **Step 1: Locate the real target**

Glob for `**/robne-cli.md` under `docs-site/`. If the page exists elsewhere, fix the link paths. If it does not exist, report it as missing — do not invent content.

- [ ] **Step 2: Fix the links**

Edit only the broken link lines.

- [ ] **Step 3: Run the hard gate**

Run: `make docs-lint`
Expected: PASS.

- [ ] **Step 4: Propose diff and wait**

Post the diff for review. Commit only on explicit go-ahead.

### Task 3: Phase-pointer sweep in validating-native-engine.md

**Files:**
* Modify: `docs-site/testing/validating-native-engine.md` (candidate lines `:16,241,251,268,273,282,690,726,992,2647`)
* Modify (only where the fact is shared): `docs/testing/validating-native-engine.md` per the sync-pair table

- [ ] **Step 1: Classify each `phase12/14` hit**

For every line, decide keep-vs-update: live instruction ("deploy phase12 on all three repos", "phase12 koku-metrics-operator required") → update to the current phase branch; point-in-time baseline ("501 passed on phase12 images, June 2026") → keep, adding a "(point-in-time)" marker only where ambiguous.

- [ ] **Step 2: Cross-check against the page's own rule**

The page requires all repos on the same branch (`:54`); verify the chosen branch against `git branch --show-current` and sibling-repo reality before editing.

- [ ] **Step 3: Run docs gates**

Run: `make docs-lint docs-drift docs-sync-check`
Expected: pass.

- [ ] **Step 4: Propose diff and wait**

Post the diff for review. Commit only on explicit go-ahead.

### Task 4: Setup and config cluster audit

**Files (audit; modify only where drift is demonstrated):**
* `docs-site/configuration.md`, `docs-site/testing.md`, `docs-site/testing/test-data-recipes.md`, `docs-site/monitoring.md`, `docs-site/operations/configuration.md`

- [ ] **Step 1: Discovery** — list every customer-relevant fact per page (env defaults, ports, process lists, file paths).
- [ ] **Step 2: Audit** — second-source each fact against `internal/config/config.go`, `Makefile`, `scripts/docker-compose.yml`, and `cmd/start.go`.
- [ ] **Step 3: Alignment matrix** — one row per fact, marked aligned / stale / missing; report it before editing.
- [ ] **Step 4: Minimal fixes** — merge-first edits for stale rows (trims allowed only per the Global Constraints trim protocol: justify + pre-approve); bump `Last verified` on touched Class-A pages; mirror shared facts across sync pairs.
- [ ] **Step 5: Run docs gates.** Expected: pass.
- [ ] **Step 6: Propose diff and wait.** Commit only on explicit go-ahead.

### Task 5: Architecture cluster audit

**Files (audit; modify only where drift is demonstrated):**
* `docs-site/architecture/plugin-architecture.md`, `recommendation-engines.md`, `native-migration.md`, `cost-integration.md`, `configurability.md`, `kafka-schema.md`, plus math/decay/gpu-catalog pages

- [ ] **Step 1: Discovery** — same as Task 4, with second sources in `internal/plugin/registry.go`, `internal/plugins/plugins.go`, `internal/services/report_processor.go`, `internal/api/server.go`.
- [ ] **Step 2: Audit** — same as Task 4.
- [ ] **Step 3: Alignment matrix** — report before editing.
- [ ] **Step 4: Minimal fixes** — same as Task 4.
- [ ] **Step 5: Run docs gates.** Expected: pass.
- [ ] **Step 6: Propose diff and wait.** Commit only on explicit go-ahead.

### Task 6: Plugins and features cluster audit

**Files (audit; modify only where drift is demonstrated):**
* `docs-site/plugin-reference/*` plus `docs-site/features/*` (one honesty matrix per plugin)

- [ ] **Step 1: Discovery** — per plugin: endpoints, filters, response shape, notification codes, terms, CSV/export coverage.
- [ ] **Step 2: Audit** — second-source against plugin `plugin.go` files, `internal/notifications/catalog.go`, `openapi.json`, handlers, and unit tests.
- [ ] **Step 3: Alignment matrix** — per plugin, reported before editing.
- [ ] **Step 4: Minimal fixes** — same protocol as Task 4.
- [ ] **Step 5: Run docs gates.** Expected: pass.
- [ ] **Step 6: Propose diff and wait.** Commit only on explicit go-ahead (one commit per plugin or per small group — reviewer's call at proposal time).

### Task 7: Operations cluster audit

**Files (audit; modify only where drift is demonstrated):**
* `docs-site/operations/*`, `docs-site/monitoring.md` (re-check), `docs-site/known-issues.md`, `docs-site/security/*`

- [ ] Steps 1–6: identical protocol to Task 4 (Discovery → Audit → matrix → fixes → gates → propose → commit on go-ahead).

### Task 8: Remainder sweep

**Files (audit; modify only where drift is demonstrated):**
* `docs-site/api-reference/*`, `docs-site/contributing.md` (re-check), `docs-site/whats-new.md`, `docs-site/changelog.md` (re-check), any unvisited Class-A page

- [ ] Steps 1–6: identical protocol to Task 4.
- [ ] **Step 7: Final full-gate run** — `make docs-lint docs-drift docs-sync-check` (hard mode) plus `make test-short` if any audit touched code-adjacent claims. Report the all-green result or the deferred-items list with per-item justification.

## Stop conditions

* A page that audits clean is recorded as "verified current, stamp bumped" — never padded with invented changes.
* Any cluster may be deferred by the reviewer at proposal time; deferred items carry their own justification and do not block other clusters.
* If Task 4 finds systemic drift (e.g. renamed env vars), re-propose sizing for Tasks 5–8 before continuing.
