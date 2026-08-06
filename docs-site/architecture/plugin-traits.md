# Plugin traits

> **Last verified:** 2026-08-06

Author-facing catalog of **trait interfaces** — optional capabilities a recommendation
plugin can implement beyond the base [`Plugin`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/plugin/plugin.go)
contract. Core dispatch uses type assertions ([`plugin.ByTrait`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/plugin/registry.go));
there is no fat interface forcing empty methods.

| Looking for… | Go here |
|--------------|---------|
| **What each trait means** (this page) | Catalog + per-trait notes below |
| **Who implements what** | [Trait matrix](../plugin-reference/index.md#trait-matrix) on the plugin-reference overview |
| **Go signatures & design history** | [Plugin Architecture §4](plugin-architecture.md#4-plugin-interfaces-trait-based) |
| **Term window defaults** | [§9.1 TermProvider defaults](plugin-architecture.md#91-termprovider-per-plugin-default-terms) |
| **Scaffold a plugin** | [`make new-plugin`](../development.md#adding-a-plugin) / [`cmd/newplugin`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/cmd/newplugin) |

Canonical definitions: [`internal/plugin/plugin.go`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/plugin/plugin.go).

## Catalog

| Trait | Role | When you need it | Who uses it today |
|-------|------|------------------|-------------------|
| **`Plugin` (base)** | Identity, enablement, phase, priority | Always (required) | All production plugins + `_example` |
| **`CSVIngestor`** | Owns parsing one or more CSV / payload types; writes digests; may return `MetricRow`s for hooks | New report type or columns this domain owns | `container`, `namespace`, `pvc`, `vm`, `snapshot`, `cluster-quota` |
| **`IngestHook`** | Runs after another plugin’s CSV ingest; piggybacks without owning the file | Derive secondary digests from someone else’s CSV | `gpu`, `node` (after `container`) |
| **`APIProvider`** | Registers authenticated HTTP routes | List / detail / settings for this domain | Almost all; **not** `container` (legacy core handlers) or `kruize` (marker only) |
| **`APIEnricher`** | Decorates another handler’s response (type-assert; no-op if wrong type) | Cross-domain fields on an existing API | Mainly **`gpu`** → container payloads |
| **`RetentionProvider`** | Declares tables + `SweepRetention` for the housekeeper | You own digest / recommendation tables that must age out | Most plugins with tables; **not** `kruize` (no tables here) or `snapshot` (inventory purge stays in core retention) |
| **`TermProvider`** | Short / medium / long windows; settings capabilities + tenant/admin overrides | Multi-term sizing / trend engines | `container`, `namespace`, `gpu`, `node`, `pvc`, `vm`; **not** `snapshot` (threshold), `quota` / `cluster-quota` (derived), `kruize` (own windows) |
| **`MigrationProvider`** | `OwnedTables()` only — **reserved** | Not for runtime today | **`_example` only**; DDL stays in root `migrations/` with `-- plugin:` headers |

Phases (`produce` / `enrich` / `optimize`) and `Priority` are part of **base `Plugin`**, not separate traits. Defaults via [`BasePlugin`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/plugin/phases.go): produce + priority 50. Order is **phase → priority (lower first) → name**. See [Plugin Execution Phases](plugin-phases.md).

## Scaffolder defaults

`make new-plugin` / `go run ./cmd/newplugin` generates:

| Live | Commented (uncomment or pass `TRAITS=…`) |
|------|------------------------------------------|
| `Plugin`, `APIProvider`, `RetentionProvider` | `CSVIngestor`, `IngestHook`, `APIEnricher`, `TermProvider` |
| | `MigrationProvider` — commented and labeled **RESERVED** |

See [#410](https://github.com/pgarciaq/ros-ocp-backend/issues/410).

---

## Plugin (base)

**Required.** Every plugin implements `Name()`, `Enabled()`, `Phase()`, and `Priority()`.

- **`Name()`** — stable id used in env vars (`ROS_ENABLED_PLUGINS`), logs, OpenAPI `x-plugin-required`, and settings.
- **`Enabled()`** — almost always `plugin.EnabledFor(p.Name())`. Do **not** copy the example’s always-`false` stub.
- **`Phase()` / `Priority()`** — embed `BasePlugin` for produce/50, or override for enrich/optimize or earlier/later within a phase.

Registration is a blank import in [`internal/plugins/plugins.go`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/plugins/plugins.go) whose `init()` calls `plugin.Register` — **not** editing `internal/plugin/registry.go`.

## CSVIngestor

Owns CSV parsing for one or more logical types (`SupportedCSVTypes` + `IngestCSV`).

Use when the operator / payload introduces a file this domain is responsible for. Return `[]ingestion.MetricRow` when downstream **`IngestHook`** plugins need the in-memory rows; otherwise `nil` is fine.

**Do not use** when you only need to react to another plugin’s CSV — use **`IngestHook`** instead.

## IngestHook

Runs after a matching `CSVIngestor` finishes (`HookAfterCSVTypes` + `AfterIngest`).

Classic pattern: `gpu` and `node` hook after `container` CSV to upsert domain digests without claiming the container file.

Hooks are non-fatal by default (errors increment Prometheus counters). See [Plugin Architecture §6.1](plugin-architecture.md#61-ingestion-report_processordo).

## APIProvider

Registers routes on the authenticated Echo group via `RegisterRoutes`.

New recommendation domains almost always need list/detail (and often settings) under `/recommendations/openshift/...`. Document paths in `openapi.json` with `x-plugin-required` so disabled plugins disappear from `/openapi.json`.

**Exceptions today:** `container` routes still live in core `server.go`; `kruize` is a registry marker without `APIProvider`.

## APIEnricher

Decorates another plugin’s or handler’s response (`EnrichResponse`). Type-assert `resp` to the expected input and no-op if it does not match.

Use sparingly for true cross-domain enrichment (e.g. GPU fields on container list/detail). Prefer owning your own **`APIProvider`** routes when you control the full response shape.

## RetentionProvider

Declares `RetentionTables()` and implements `SweepRetention` for housekeeper sweeps.

If your plugin writes digests or recommendation sets, implement this so data expires with `ROS_RETENTION_*` policy. Empty stubs from the scaffolder are intentional until tables exist.

**Exceptions:** `kruize` owns no tables in-process; `snapshot` inventory purge remains in core [`retention.go`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/engine/retention.go).

## TermProvider

Declares `DefaultTerms()` (short / medium / long) and `MaxWindowDays()`.

Enables:

- `supports_terms: true` on `GET /settings/capabilities`
- Tenant overrides via `PUT /settings/terms?recommendation_type=<name>`
- Admin locks via `ROS_TERMS_<PLUGIN>_<TERM>_<FIELD>`

**Do not use** for binary / threshold recommendations (`snapshot`) or domains that do not expose multi-window engines (`quota`, `cluster-quota` today). Per-plugin default windows: [§9.1](plugin-architecture.md#91-termprovider-per-plugin-default-terms).

## MigrationProvider (reserved)

`OwnedTables()` only. **Nothing in the dispatch pipeline consumes this trait.**

Ship DDL as numbered files under repo-root `migrations/` with a `-- plugin: <name>` header. The scaffolder leaves this trait commented and labeled RESERVED so authors do not expect auto-migrations from uncommenting it.
