# ADR-0346: Per-plugin term windows as a shared librobne contract

## Status

Accepted (2026-10-27; owner decision in the #705 implementation discussion)

## Phase

robne CLI / engine term-window fix (#705); server term-config delegation

## Context

The #705 parity divergence (robne CLI vs server producing different VM and
PVC recommendations from identical input) was pinned to a single root cause:
`cmd/robne/recommend.go` feeds the **container-family term table**
(`librobne/engine.DefaultTerms()`: short 1d / medium 7d / long 15d) into
*every* entity's engine. The documented tables differ for two entities —
VM (7/15/30, min 3/7/15) and PVC (7/30/90) — and exactly those two diverged;
container, namespace, node, and gpu (all 1/7/15) were at parity. The server
is unaffected: `internal/engine/term_config.go` resolves per-plugin terms via
`LoadTermConfigCached(ctx, pool, orgID, "<plugin>")`, defaulting through
`DefaultTermsForPlugin` (the plugin registry's TermProvider trait).

A second facet: robne's Path-A DB read window (`digestWindow(ec.Terms)`,
max 15d from the container table) would truncate VM (30d) and PVC (90d)
long-term data even after the recommend-side fix.

Owner requirements for the fix:

1. robne CLI as configurable as the server — per-plugin terms when wanted.
2. Documented (server) defaults when no terms configuration is passed.
3. Overriding one plugin's terms affects only that plugin.

## Decision

**Single source.** The six documented per-plugin term tables live in
librobne exactly once each: the container-family table stays
`librobne/engine.DefaultTerms()`, the VM table stays
`librobne/vm.DefaultVMTermWindows()`, and a PVC table is added beside the
PVC engine with its numbers taken from the server plugin's `DefaultTerms()`
(not copied by hand). The server's per-plugin `DefaultTerms()`
implementations delegate to these librobne constants so both consumers read
one number per plugin. Server behavior is unchanged and pinned by the
existing term-config tests.

**Effective-terms resolution (robne).** Each term-bearing plugin resolves
its terms independently: `user configuration for that plugin`, else `the
plugin's documented table`. Nothing falls through between plugins — the
current fall-through is the bug being fixed. This mirrors the server's
resolution (tenant override ‖ plugin default) minus the tiers robne does not
have (DB-backed Settings API, admin env locks).

**robne YAML surface.** A new `plugin_terms:` map with the six
term-bearing plugin keys (`container`, `namespace`, `node`, `gpu`, `vm`,
`pvc`), each a terms list in the existing shape. Unset plugins use their
documented defaults. The existing top-level `terms:` list remains valid and
becomes explicitly container-scoped (back-compat: existing files keep
working; anyone who relied on `terms:` moving VM/PVC windows was relying on
the bug — pre-release window). Setting both `terms:` and
`plugin_terms.container` is a hard error (ambiguous). Unknown plugin keys —
including the term-less plugins (quota, cluster-quota, snapshot,
hcp-capacity have no terms by design) — are errors. Validation bounds mirror
the server's (locate and reuse its enforcement, do not invent new bounds).
Overlay semantics stay whole-key replacement per plugin entry.

**Read window.** robne's Path-A digest SELECT window becomes the maximum
lookback across all *effective* tables (configured or documented), not the
container table's maximum.

**Term-less plugins.** quota and cluster-quota (threshold-based), snapshot
(age-threshold), and the HCP fleet-capacity advisory (snapshot-series model)
have no term windows in either implementation; `plugin_terms` entries for
them are invalid.

## Alternatives considered

- **Minimal (per-entity hardcoded defaults, no new config):** fixes the
  bug but cannot mirror a server whose tenant has tuned terms — rejected by
  requirement 1.
- **Grouped config (container-family / VM / PVC keys):** smaller surface,
  but the server's granularity is per-plugin and grouped config cannot
  mirror a server where e.g. namespace is tuned independently of container —
  rejected by requirement 1.
- **robne imports `internal/engine.DefaultTermsForPlugin`:** fastest, but
  couples the CLI to the server's plugin-registry wiring — rejected in
  favor of the librobne single source, which makes this class of drift
  structurally impossible.

## Consequences

- robne's VM and PVC recommendation output changes once the tables are
  correct — accepted in the pre-release window; goldens regenerate; the
  spec amendment and public docs ride with the fix.
- The `robne-cli-spec.md` reserved-keys posture gains a deliberate partial
  unlock: `plugin_terms:` exposes terms per plugin ahead of the full entity
  settings schemas. The future entity-settings unlock supersedes and
  extends this key naturally.
- Acceptance is detector-grade: the parity comparison must reach 10/10
  green on fixtures that can actually detect the bug (a window-sensitive
  PVC payload and a <15-day VM window; stock templates are window-insensitive
  or mask the facets — see #705/#706 run records).
