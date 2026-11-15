# #792 — Fail-Fast on Invalid Admin Term Env Vars — Implementation Plan

> **For agentic workers:** superpowers:test-driven-development governs every step.
> Owner decisions 2026-11-15: option 1 — **fail loudly at startup**; keep the
> MIN_DATA_DAYS>WINDOW_DAYS **clamp** (a documented resolution rule, not a
> rejection); vars for term-less or unknown plugins are **removed** — the
> startup validator rejects any `ROS_TERMS_*` var that doesn't target a
> registered TermProvider × known term × known field (the functionality is
> gone — #793 removed HCP terms; quota/cluster-quota/snapshot never had
> them — so nobody legitimate can be setting them, and the strict scan
> catches the typo class for free).

**Repo:** ros-ocp-backend, branch `pgarciaq-rosocp-superpowers-phase17`.
Tracker: #857 (serving P3 blitz, item 4).

## The verified mechanism

- `loadEnvTerm` (`internal/engine/term_config.go:293-316`): each of the
  three fields applies only when parseable and in range; otherwise
  `Warnf` + ignore and `anySet` stays false → the term falls back to the
  plugin default.
- `IsTermLocked` (`:337-341`): locked whenever the var is **set at all**,
  valid or not — so a typo'd value yields the default **and** locked
  (the tenant cannot correct it). The terms handler surfaces the flag
  and refuses tenant writes.
- Startup precedent: `runServiceStartup` (cmd/start.go) already fail-fasts
  via `config.ValidateSecurityConfig()` → `startCmdLog.Fatal(err)`.
- Term-bearing plugins (the `plugin.ByTrait[plugin.TermProvider]()` walk,
  same as the terms handler): container, gpu, namespace, node, pvc
  (example excluded from production; hcp gone since #793).
- **robne/librobne does not read these vars** (grep-verified) — server-side
  only, no parity concern.
- **Recon correction (found by the RED run):** an earlier truncated grep
  cut VM from the TermProvider list and produced a false "VM is not a
  term provider / `ROS_TERMS_VM_SHORT_TERM_WINDOW_DAYS` is dead config"
  finding. VM **is** term-bearing (ADR-0346: terms named `short_term` /
  `medium_term` / `long_term`), the docs example in configurability.md:116
  is **correct**, and those vars keep working. The strict-scan decision is
  unaffected (hcp/quota/cluster-quota/snapshot remain term-less; unknown
  names still rejected); the vm test case uses the realistic
  `ROS_TERMS_VM_SHORT_WINDOW_DAYS` typo instead.

## Mechanism (the fix — two layers, one shared rule set)

**Layer 1 — startup fail-fast:** `engine.ValidateTermEnvConfig() error`:

1. **Strict scan:** build the set of valid variable names from the
   registry — every TermProvider plugin × its `DefaultTerms()` term
   names × {`WINDOW_DAYS`, `MIN_DATA_DAYS`, `DECAY_HALFLIFE_HOURS`}.
   Every env var with the `ROS_TERMS_` prefix must be in that set;
   otherwise an error naming the var and the term-bearing plugins
   ("unknown admin term variable"). Set membership, not string
   decomposition — no ambiguity with hyphens or future multi-word names.
2. **Value validation:** every set var must pass the same per-field rules
   `loadEnvTerm` applies — window: integer 1..`PluginMaxWindowDays`;
   min_data_days: integer ≥ 1; decay_halflife_hours: number ≥ 0. Errors
   name plugin, term, field, value, and the valid range.

Wired in `runServiceStartup` after `plugin.Init()` (the registry is
needed), `startCmdLog.Fatal(err)` per the `ValidateSecurityConfig`
precedent.

**Layer 2 — the invariant (issue criterion 3):** `IsTermLocked` reports
locked only when a set var **would be applied** — it consults the same
per-field validation. With Layer 1 this is redundant in production, but
it makes "locked ⇒ applied" structural: true even on paths that bypass
startup validation (tests, embedded use).

**Shared rules:** a `validateTermEnvValue(field, value, maxWin) error`
helper used by the validator, `loadEnvTerm` (warn+ignore retained as
unreachable-in-production defense), and `IsTermLocked`. The three call
sites cannot drift apart again.

**Not changed:** the MIN_DATA_DAYS>WINDOW_DAYS clamp (owner decision —
kept, a documented resolution rule); the `ROS_SETTINGS_LOCKED` global
and its per-plugin kin (different mechanism, `LogSettingsLockedStartup`).

## Tests

1. **The issue's repro as the integration RED (Layer 2):**
   `ROS_TERMS_CONTAINER_LONG_WINDOW_DAYS=120` →
   `GET /settings/terms?recommendation_type=container` →
   `long.locked == false` and the default window (15).
   **Baseline: `locked == true` — RED.**
2. **Startup-refusal unit tests (Layer 1, new function):** the three
   acceptance cases (out-of-range window; unparseable MIN_DATA_DAYS;
   negative DECAY_HALFLIFE_HOURS) each produce an error naming
   plugin/term/field/value/range; valid values → nil; clean env → nil.
3. **Strict-scan tests:** `ROS_TERMS_HCP_SHORT_WINDOW_DAYS`,
   `ROS_TERMS_VM_SHORT_TERM_WINDOW_DAYS` (the dead docs example),
   `ROS_TERMS_CONTAINER_EXTRA_WINDOW_DAYS` (unknown term),
   `ROS_TERMS_CONTAINER_LONG_FOO` (unknown field) → "unknown admin term
   variable" errors.
4. **The locked-implies-applied invariant:** valid env (`45`) →
   `locked == true` AND `window_days == 45` (not the default).
5. **Existing tests hold:** `TestPutTermSettings_LockedTermReturns422`
   (valid env locks) and the term_config env tests pass unchanged.

## Mutations run

- **M1** (IsTermLocked reverted to set-at-all): the integration RED
  fails again — `locked == true` for the rejected value, with the
  mechanism-naming message.
- **M2** (validator WINDOW_DAYS range check dropped, parse kept): the
  out-of-range-window unit test fails; the unparseable-min test
  correctly still passes (its check is intact) — the tests discriminate.
- **M3** (strict scan dropped, unknown vars accepted): the
  unknown-variable tests fail; the valid-values test correctly still
  passes.

## Honest corrections recorded

- **The VM recon error — twice.** An earlier truncated grep (`head -6` cut
  the plugin list) produced the false finding "VM is not a
  TermProvider." The RED run caught that (VM IS term-bearing), and the
  plan's first correction claimed the docs example
  `ROS_TERMS_VM_SHORT_TERM_WINDOW_DAYS` "keeps working." The fresh-context
  review then proved EMPIRICALLY that this too was wrong: the resolver
  reads the fixed ord names (`short`/`medium`/`long`) — the
  DefaultTerms-derived `short_term` spelling has NEVER been read by
  anything. The docs example was dead config all along. Worse, the first
  validator version built its valid set from DefaultTerms names — it
  would have rejected the operative `ROS_TERMS_VM_SHORT_*` vars at
  startup (a regression on working config) while blessing the dead
  spelling (the locked-but-unapplied shape #792 eliminates). **Fixed:**
  the valid set is built from the resolver's name space (`termNames`);
  the docs example corrected to `ROS_TERMS_VM_MEDIUM_WINDOW_DAYS`; the
  residual VM name-space split (generic-route lock flag reading the
  display name, the Name flip-flop) filed as **#866**.
- **The term cache in the RED:** the first RED run's control test
  (valid env) failed spuriously — the shared 60s term cache carried the
  previous test's resolution for the test org. Both tests now invalidate
  the cache entry first.

## Fresh-context review (one P1, one P2 — both fixed; three P3s handled)

- **P1** (validator valid set from DefaultTerms names vs the resolver's
  ord names — VM sat exactly on the seam): fixed as above;
  `AcceptsValidValues` now includes the operative VM spelling and the
  unknown-variable suite rejects the dead one.
- **P2** (validator read raw `os.Environ()` values; the resolver reads
  TrimSpace'd viper — empty and whitespace-padded values would have
  fatal'd startup on config the resolver honours or ignores): fixed —
  the validator trims and treats empty as unset, exactly
  `config.EnvString`'s semantics; pinned by
  `ReadsValuesLikeTheResolver` (empty → unset; `" 45 "` → valid).
- **P3** (stale comment claiming vm "never was a term provider"):
  removed in the validator rewrite.
- **P3** (kruize/exclusivity mode: no TermProvider enabled → empty
  plugin list in the error): the message now says "no term-bearing
  plugins are enabled — remove the variable or enable its plugin."
- **P3** (latent: a future viper-bound file/flag source for these keys
  would bypass the os.Environ scan): documented in the validator's
  comment — the keys are env-only by design today.

The reviewer independently verified: the M1 mutation live (the
integration RED fails with the mechanism-naming message); the shared
helper's byte-for-byte parity with the old resolver rules (including
edge inputs `+7`, `1e5`, `-0`, overflow); the IsTermLocked⇔anySet
equivalence; the clamp coherence; the startup wiring on all four entry
subcommands; that the robne CLI neither calls runServiceStartup nor
reads ROS_TERMS_*; and the term-less docs list (hcp, quota,
cluster-quota, snapshot).

## Deliverables

- `internal/engine/term_env_validation.go` (new — the validator + shared
  rule helper) or additions to `term_config.go`; `IsTermLocked` update;
  `cmd/start.go` wiring.
- Tests (integration + unit).
- `docs-site/features/configurable-thresholds.md` — the fail-fast
  behavior per the acceptance; `docs-site/architecture/configurability.md`
  — the dead `ROS_TERMS_VM_SHORT_TERM_WINDOW_DAYS` example corrected and
  the invalid-value behavior noted.
- CHANGELOG entry; this plan doc; completion report per
  properly-implement-feature.
