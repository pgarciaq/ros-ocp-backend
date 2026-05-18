# Recommendation Plugin Architecture — Design Proposal

> **Date:** 2026-05-18  
> **Status:** Proposal  
> **Scope:** `ros-ocp-backend` (Go service that ingests OpenShift metrics and serves resource optimization recommendations via HTTP)

ros-ocp-backend today implements multiple recommendation domains—container CPU/memory, namespace aggregates, GPU (MIG and time-slicing), node utilization, PVC storage, and VolumeSnapshot staleness—with additional domains planned (OpenShift Virtualization VMs, Java/JVM, Golang). Each domain spans ingestion, persistence, recommendation logic, HTTP handlers, and retention. **There is no shared plugin abstraction**: behavior is wired through sequential conditionals and tightly coupled call chains across roughly eight touchpoints per feature.

This document proposes **compile-time, in-process plugins** behind small Go interfaces, toggled at runtime via environment variables (`ROS_ENABLED_PLUGINS` / `ROS_DISABLED_PLUGINS`). Plugins ship in the same binary (blank imports + `init()` registration)—no dynamic `.so` loading, no gRPC, no Wasm—preserving **zero interface dispatch overhead** on the hot path beyond ordinary Go polymorphism.

---

## 1. Problem statement

### 1.1 Symptoms

- Adding or substantially changing a recommendation type requires edits across Kafka ingestion, CSV classification, digest pipelines, recommendation engines, HTTP routing, handlers, and often SQL migrations—without a single place that declares “this type exists.”
- Operators cannot disable a domain (for example GPU or snapshots) without feature-specific env vars scattered through `config`, or without removing code paths.
- Cross-cutting behavior (GPU digest upserts during container CSV processing; GPU enrichment of container list/detail APIs; node recommendations invoked from the container native path) is **implicitly sequenced** inside large functions, which obscures ownership and complicates testing.

### 1.2 Concrete coupling in the codebase

**Kafka report dispatch — sequential `if` branches per CSV type** (`internal/services/report_processor.go`):

```104:136:internal/services/report_processor.go
	var csvType types.PayloadType

	for _, file := range kafkaMsg.Files {
		csvType = utils.DetermineCSVType(file)

		if cfg.UseNativeEngine && csvType == types.PayloadTypeContainer {
			if err := processContainerCSVNative(file, kafkaMsg); err != nil {
				reportProcessingFailed = true
				recordKafkaTransient(err)
			}
			continue
		}
		if cfg.UseNativeEngine && csvType == types.PayloadTypeNamespace {
			if err := processNamespaceCSVNative(file, kafkaMsg); err != nil {
				reportProcessingFailed = true
				recordKafkaTransient(err)
			}
			continue
		}
		if cfg.UseNativeEngine && csvType == types.PayloadTypeStorage {
			if err := processStorageCSVNative(file, kafkaMsg); err != nil {
				reportProcessingFailed = true
				recordKafkaTransient(err)
			}
			continue
		}
		if cfg.UseNativeEngine && csvType == types.PayloadTypeSnapshot {
			if err := processSnapshotCSVNative(file, kafkaMsg); err != nil {
				reportProcessingFailed = true
				recordKafkaTransient(err)
			}
			continue
		}
```

**CSV type discrimination by filename substring** (`internal/utils/utils.go`):

```384:395:internal/utils/utils.go
func DetermineCSVType(fileName string) types.PayloadType {
	if strings.Contains(fileName, "namespace") {
		return types.PayloadTypeNamespace
	}
	if strings.Contains(fileName, "snapshot") {
		return types.PayloadTypeSnapshot
	}
	if strings.Contains(fileName, "storage") {
		return types.PayloadTypeStorage
	}
	return types.PayloadTypeContainer
}
```

**Payload type constants** (shared contract; every new file pattern requires updates here and in dispatch) (`internal/types/kafkaMsg.go`):

```5:12:internal/types/kafkaMsg.go
type PayloadType string

const (
	PayloadTypeContainer PayloadType = "container"
	PayloadTypeNamespace PayloadType = "namespace"
	PayloadTypeStorage   PayloadType = "storage"
	PayloadTypeSnapshot  PayloadType = "snapshot"
)
```

**Container CSV native path fans into container recommendations and node recommendations** (`internal/services/report_processor.go`):

```440:545:internal/services/report_processor.go
// processContainerCSVNative handles container CSV files through the native Go
// recommendation engine instead of the Kruize pipeline. It downloads the CSV,
// computes daily digests, upserts them, and runs the recommendation engine.
func processContainerCSVNative(fileURL string, kafkaMsg types.KafkaMsg) error {
	// ...
	if err := ingestion.ProcessCSVToDigests(ctx, pool, body, orgID, clusterUUID); err != nil {
		// ...
	}
	// ... RecommendAllWorkloads, WriteRecommendations, history, quality ...
	if err := runNodeRecommendations(ctx, pool, orgID, clusterUUID, start, now, appCfg); err != nil {
		log.Warnf("native engine: node recommendations incomplete for org=%s cluster=%s: %v", orgID, clusterUUID, err)
		return fmt.Errorf("node recommendations: %w", err)
	}
	return nil
}
```

**GPU and node digest upserts chained inside container digest processing** (`internal/ingestion/pipeline.go`):

```267:276:internal/ingestion/pipeline.go
	log.Infof("ProcessCSVToDigests: upserted %d digests for org=%s cluster=%s",
		len(grouped), orgID, clusterUUID)

	if err := upsertGPUDigests(ctx, pool, rows, orgID, clusterUUID); err != nil {
		return fmt.Errorf("GPU digest upsert: %w", err)
	}

	if err := upsertNodeDigests(ctx, pool, rows, orgID, clusterUUID); err != nil {
		return fmt.Errorf("node digest upsert: %w", err)
	}
```

**HTTP routes registered imperatively per domain** (`internal/api/server.go`):

```61:127:internal/api/server.go
	// Container recommendations — native engine with Kruize fallback, or legacy-only.
	if cfg.UseNativeEngine {
		// Static /gpu path must register before /:recommendation-id so "gpu" is not captured as an ID.
		v1.GET("/recommendations/openshift/gpu", GetGPUSummary)
		v1.GET("/recommendations/openshift", GetRecommendationSetListWithFallback)
		v1.GET("/recommendations/openshift/:recommendation-id", GetRecommendationSetWithFallback)
	} else {
		// ...
	}

	// Project/Namespace — ...
	if cfg.UseNativeEngine {
		v1.GET("/recommendations/openshift/namespaces", GetNamespaceRecommendationSetListWithFallback)
		// ...
	}
	// ...
	// Node-level GPU time-slicing and MIG-focused listings (native engine only).
	if cfg.UseNativeEngine {
		v1.GET("/recommendations/openshift/gpu/timeslicing", GetNodeRecommendations)
		v1.GET("/recommendations/openshift/gpu/mig", GetGPUMIGRecommendations)
		v1.GET("/recommendations/openshift/nodes", GetNodeUtilizationRecs)
		v1.GET("/recommendations/openshift/nodes/utilization", GetNodeUtilizationRecsLegacyPath)
	}
	// PVC ... Snapshot ...
```

**GPU enrichment hard-coded into container list handlers** (`internal/api/handlers.go`):

```372:385:internal/api/handlers.go
	results, count, queryErr := model.GetNativeRecommendations(OrgID, apiListOptions, queryParams, userPerms)
	if queryErr != nil {
		// ...
	}

	enrichWithGPU(c.Request().Context(), results, OrgID)

	hasGPU, gpuModels, gpuClassifications := parseGPUFilters(c)
	results, count = filterGPUResults(results, hasGPU, gpuModels, gpuClassifications)
```

**Retention sweeps use a fixed table list** (`internal/engine/retention.go`):

```23:30:internal/engine/retention.go
// Tables retained by the general ROS_RETENTION_MONTHS setting.
var retainedTables = []string{
	"container_usage_samples",
	"daily_container_digests",
	"daily_namespace_digests",
	"gpu_container_digests",
	"namespace_usage_samples",
}
```

Together, these fragments show the same pattern repeated: **dispatch by enum + imperative wiring**, rather than a registry of named capabilities.

---

## 2. Design goals

| Goal | Intent |
|------|--------|
| **Zero hot-path IPC overhead** | Use Go interfaces and static calls only—no gRPC, no subprocesses, no serialization between core and plugins. |
| **Runtime enable/disable without recompilation** | Operators choose active domains via env vars; compiled plugins not in the allowlist (or explicitly blocklisted) do not run **their** ingest hooks, routes, retention, or migrations **contributions**. The binary remains a single artefact with all plugins linked. |
| **Incremental adoption** | Introduce registry and interfaces first; migrate one recommendation type at a time behind the same API. |
| **Explicit cross-plugin relationships** | Secondary behavior (GPU/node piggybacking on container CSV; GPU enrichment of container HTTP responses) becomes **declarative** (`IngestHook`, `APIEnricher`) instead of buried ordering inside monolithic functions. |
| **Testability** | Each plugin can be unit-tested with fake `PluginContext`; integration tests compose a subset of registered plugins. |

Non-goals are listed in [§13](#13-what-this-design-does-not-do).

---

## 3. Architecture overview

Plugins are **compiled in** and **self-register** via `init()`. A central **registry** filters registrations according to env-configured enabled sets. Core subsystems (**ingestion dispatcher**, **HTTP registrar**, **retention runner**, **migration contributor list**) iterate **only enabled** plugins and invoke optional capabilities via type assertions (trait pattern).

```mermaid
flowchart TB
    subgraph registry [Plugin registry]
        Register["Register(plugin)"]
        Enabled["Enabled() -> []Plugin"]
    end

    subgraph plugins [Compiled plugins]
        Container[container]
        Namespace[namespace]
        GPU[gpu]
        Node[node]
        PVC[pvc]
        Snapshot[snapshot]
        VM["vm future"]
    end

    subgraph core [Core framework]
        Ingest[Ingestion dispatcher]
        APIReg[Route registrar]
        Retain[Retention sweeper]
        Migrate[Migration contributors]
    end

    plugins -->|"init()"| Register
    Enabled --> Ingest
    Enabled --> APIReg
    Enabled --> Retain
    Enabled --> Migrate
```

**Shared infrastructure** (PostgreSQL pool, service config, term-config resolver, Prometheus metrics, structured logging) is passed through a **`PluginContext`** constructed once at startup and injected into plugins that need it (either via setter called from `main`, or passed into `ProcessCSV` / `RunRetention` — exact wiring is an implementation detail left to Phase 1).

---

## 4. Plugin interfaces (trait-based)

Not every plugin implements every trait. Core code uses **type assertions** to detect capabilities—no “fat” interface forcing empty methods.

```go
package plugin

// Plugin is the base identity every recommendation domain must implement.
type Plugin interface {
	Name() string           // stable identifier: "container", "gpu", "pvc", ...
	DefaultEnabled() bool   // when ROS_ENABLED_PLUGINS is unset, include if true
}

// CSVIngestor handles CSV files arriving via Kafka (after URL fetch / typing).
type CSVIngestor interface {
	Plugin
	PayloadTypes() []PayloadType // types.DetermineCSVType or successor maps files here
	ProcessCSV(ctx context.Context, pctx *PluginContext, fileURL string, msg KafkaMsg) error
}

// IngestHook runs after another plugin finishes primary CSV ingestion for the same file.
// Example: GPU and node digest writers logically follow container CSV parsing.
type IngestHook interface {
	Plugin
	HookAfter() string // plugin Name(), e.g. "container"
	OnAfterCSVIngest(ctx context.Context, pctx *PluginContext, primary Plugin, fileURL string, msg KafkaMsg) error
}

// APIProvider registers HTTP routes under the authenticated v1 group.
type APIProvider interface {
	Plugin
	RegisterRoutes(e *echo.Group, pctx *PluginContext)
}

// APIEnricher decorates responses owned by another plugin.
// Example: GPU metadata on container list/detail payloads.
type APIEnricher interface {
	Plugin
	EnrichTarget() string // plugin Name(), e.g. "container"
	EnrichList(ctx context.Context, pctx *PluginContext, orgID string, results *[]model.NativeContainerResult) error
	EnrichDetail(ctx context.Context, pctx *PluginContext, orgID string, detail interface{}) error
}

// RetentionProvider contributes partition drops / DELETE sweeps for domain-owned tables.
type RetentionProvider interface {
	Plugin
	RunRetention(ctx context.Context, pctx *PluginContext, retentionMonths int, historyRetentionDays int) error
}

// MigrationProvider contributes golang-migrate sources or version prerequisites.
// Core migration runner invokes enabled plugins so DDL for disabled domains can be skipped or no-op.
type MigrationProvider interface {
	Plugin
	MigrationsFS() fs.FS        // embed FS, or empty if plugin uses central migrations only
	MigrationsPrefix() string // e.g. "vm" -> files under vm/migrations
}
```

**Note:** `PayloadType`, `KafkaMsg`, and Echo types above are illustrative signatures; the implementation should import the existing `internal/types` and `internal/api` packages rather than duplicating models.

---

## 5. Registry implementation

- **`Register(p Plugin)`** — appends to a package-level slice; called from each plugin’s `init()`.
- **`Enabled()`** — returns plugins filtered by env:

  - If **`ROS_ENABLED_PLUGINS`** is non-empty: treat as **allowlist** (comma-separated names). Only registered plugins whose `Name()` appears are enabled (regardless of `DefaultEnabled()`, unless we explicitly document hybrid behavior—recommended: allowlist wins).
  - If **`ROS_ENABLED_PLUGINS`** is empty: enable all registered plugins such that **`DefaultEnabled() == true`**, then apply **`ROS_DISABLED_PLUGINS`** as a **blocklist** (comma-separated names).

- **Ordering** — registry preserves registration order (import order in `main`). For deterministic hooks, core may **sort** hooks by `(HookAfter(), Name())` or document that hook order follows registration order after filtering.

Illustrative logic:

```go
func Enabled() []Plugin {
	allow := parsePluginSet(os.Getenv("ROS_ENABLED_PLUGINS"))
	deny := parsePluginSet(os.Getenv("ROS_DISABLED_PLUGINS"))
	var out []Plugin
	for _, p := range registry {
		name := p.Name()
		if len(allow) > 0 {
			if allow[name] {
				out = append(out, p)
			}
			continue
		}
		if deny[name] {
			continue
		}
		if p.DefaultEnabled() {
			out = append(out, p)
		}
	}
	return out
}
```

**Blank imports** in `cmd/*/main.go` (or a single `internal/plugins/import_plugins.go`) pull in plugins:

```go
import (
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins/container"
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins/namespace"
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins/gpu"
	// ...
)
```

---

## 6. Core dispatch changes

### 6.1 Ingestion (`report_processor.go`)

Replace the fixed sequence of `if csvType == …` with:

1. Compute `csvType` (eventually **`CSVIngestor.PayloadTypes()`** may supersede central string matching, or `DetermineCSVType` becomes a fallback delegating to plugins).
2. For each enabled plugin implementing **`CSVIngestor`**, if `csvType` ∈ `PayloadTypes()`, call `ProcessCSV`.
3. For each enabled **`IngestHook`** where `HookAfter()` equals the **primary** plugin `Name()` that just ran, call `OnAfterCSVIngest`.

This preserves semantics while allowing GPU/node code to move out of `internal/ingestion/pipeline.go`’s unconditional tail calls.

### 6.2 API (`server.go` / handlers)

After constructing Echo groups and middleware:

```go
for _, p := range plugin.Enabled() {
	if api, ok := p.(plugin.APIProvider); ok {
		api.RegisterRoutes(v1, pctx)
	}
}
```

Static path ordering constraints (for example registering `/recommendations/openshift/gpu` before `/:recommendation-id`) become each plugin’s responsibility, documented in contributor guidelines.

Container list/detail handlers resolve **`APIEnricher`** targets named `"container"` instead of calling `enrichWithGPU` directly.

### 6.3 Retention (`internal/engine/retention.go`)

Split today’s monolithic `retainedTables` slice:

- **Framework-owned** tables stay in core (for example org/cluster/account bookkeeping if applicable).
- Each **`RetentionProvider`** appends domain-specific partition parents or runs targeted `DELETE`s.

Core orchestrates:

```go
for _, p := range plugin.Enabled() {
	if rp, ok := p.(plugin.RetentionProvider); ok {
		if err := rp.RunRetention(ctx, pctx, months, historyDays); err != nil {
			errs = append(errs, err)
		}
	}
}
```

### 6.4 Migrations

The golang-migrate driver loads **core** SQL plus **`MigrationProvider`** entries from enabled plugins only. Disabled plugins skip DDL—operators must not flip a domain from disabled → enabled on an existing cluster **without** running migrations (documented operational constraint).

---

## 7. Hard coupling points and resolutions

| Coupling | Today | Resolution |
|----------|--------|------------|
| **Container CSV fan-out** | `ProcessCSVToDigests` always upserts GPU + node digests; `processContainerCSVNative` always runs node recommendations. | **Container** plugin owns digest + container engine only. **GPU** / **node** implement **`IngestHook`** on `"container"` (receive same `fileURL` / shared parsed artifact via context or staged temp file pointer). Optional: pass a parsed **`[]MetricRow`** through `PluginContext` keyed by request ID to avoid double download. |
| **GPU API enrichment** | `handlers.go` calls `enrichWithGPU` unconditionally on native lists. | **GPU** plugin implements **`APIEnricher`** with `EnrichTarget() == "container"`. Container handlers query enrichers from registry. |
| **Shared infrastructure** | Packages import `db.GetPool()` and `config.GetConfig()` freely. | **`PluginContext`** carries pool, config snapshot, metrics, and collaborators (cost provider, RBAC hooks). Reduces init-time globals for tests. |
| **Retention table lists** | Single `retainedTables` array mixes domains. | Partition parents move under **`RetentionProvider`** per plugin; core retains only cross-cutting tables. |

---

## 8. Plugin directory structure (proposed)

```
internal/plugins/
  registry.go            # Register, Enabled, PluginContext
  traits.go              # interface definitions (+ PayloadType aliases)

  container/
    plugin.go            # init() + composite struct
    ingest.go            # CSVIngestor
    hooks_export.go      # registers nothing — hooks live in gpu/node
    api.go               # APIProvider
    retention.go         # RetentionProvider (samples + daily_container_digests + ...)
    migrations.go        # optional MigrationProvider

  namespace/
    ...

  gpu/
    plugin.go
    ingest_hook.go       # IngestHook(container)
    api.go               # routes + APIEnricher(container)
    retention.go

  node/
    plugin.go
    ingest_hook.go
    recommend_hook.go    # post-container recommendation pass (or fold into ingest hook)
    api.go
    retention.go

  pvc/
    ...

  snapshot/
    ...
```

Engine math under `internal/engine/` can remain shared libraries imported by plugins until a later refactor moves code physically.

---

## 9. Trait matrix (current recommendation types)

| Domain | Plugin name | CSVIngestor | IngestHook | APIProvider | APIEnricher | RetentionProvider | MigrationProvider |
|--------|-------------|:-----------:|:----------:|:-----------:|:-----------:|:-----------------:|:-----------------:|
| Container CPU/memory | `container` | ✅ Primary ros CSV | — | ✅ List/detail/history/quality/terms | — | ✅ Container samples & digests | ✅ Shared baseline |
| Namespace | `namespace` | ✅ | — | ✅ (+ legacy paths) | — | ✅ Namespace samples & digests | ✅ |
| GPU (MIG / time-slicing) | `gpu` | — | ✅ After `container` | ✅ Summary + subroutes | ✅ Targets `container` | ✅ `gpu_container_digests` | ✅ |
| Node utilization | `node` | — | ✅ After `container` (digests); recommendation pass may be same hook or secondary hook | ✅ Nodes routes | — | ✅ `daily_node_digests` & related (when split from core list) | ✅ |
| PVC | `pvc` | ✅ Storage CSV | — | ✅ | — | ✅ PVC digest tables | ✅ |
| Snapshot | `snapshot` | ✅ Snapshot CSV | — | ✅ + settings | — | ✅ `snapshot_inventory` purge logic | ✅ |

*Cells marked “Shared baseline” imply migrations may remain centralized initially; **`MigrationProvider`** can start as a no-op for types whose SQL already lives under `migrations/` until Phase 2 splits files.*

---

## 10. Adding a new plugin (example: OpenShift Virtualization VMs)

1. **Define plugin name** — `vm` (stable, lowercase, matches env vars).
2. **Create package** `internal/plugins/vm/` with `init() { plugin.Register(&VMPlugin{}) }`.
3. **Implement traits:**
   - **`CSVIngestor`** if a distinct CSV / payload type exists; otherwise **`IngestHook`** if VM metrics piggyback on an existing file.
   - **`APIProvider`** for `/recommendations/openshift/vms` (exact paths follow OpenAPI policy).
   - **`RetentionProvider`** for `daily_vm_digests` and VM recommendation partitions.
   - **`MigrationProvider`** when DDL is plugin-owned.
4. **Add SQL** under `migrations/` or embedded `vm/migrations/` per repo convention.
5. **Blank-import** `_ ".../internal/plugins/vm"` from the main binary.
6. **Update operator / ingest documentation** so the correct files arrive on Kafka when the plugin is enabled.

No edits to `server.go`’s route list should be required beyond the generic registrar loop once Phase 1 lands.

---

## 11. Enable / disable mechanism

| Variable | Semantics |
|----------|-----------|
| **`ROS_ENABLED_PLUGINS`** | Comma-separated allowlist. When set, **only** these plugins run (names match `Plugin.Name()`). |
| **`ROS_DISABLED_PLUGINS`** | Comma-separated blocklist applied when **allowlist is unset**: defaults minus blocked names. |
| **Both unset** | All plugins with `DefaultEnabled() == true`. |

Examples:

```bash
# Default bundle (all default-enabled plugins)
ROS_ENABLED_PLUGINS=
ROS_DISABLED_PLUGINS=

# Strict subset for lightweight deployments
ROS_ENABLED_PLUGINS=container,namespace,pvc

# Disable GPU and snapshot domains only
ROS_DISABLED_PLUGINS=gpu,snapshot
```

**Operational note:** Disabling a plugin stops **new** processing and API surfaces for that domain; existing DB rows remain until retention policies drop them (may require running disabled plugin retention once for cleanup, or a documented manual purge).

---

## 12. Migration path (four phases)

| Phase | Deliverable |
|-------|-------------|
| **Phase 1 — Skeleton** | Add `internal/plugins` registry, `PluginContext`, env parsing, unit tests. Refactor `report_processor.go`, `server.go`, and `retention.go` to loop plugins while existing logic lives in **adapter structs** calling today's functions. |
| **Phase 2 — Simple domains** | Migrate **PVC** and/or **snapshot** end-to-end into plugin packages (low cross-talk). |
| **Phase 3 — Coupled domains** | Break **`ProcessCSVToDigests`** GPU/node tail into **`IngestHook`** implementations; move **`runNodeRecommendations`** invocation to **node** plugin; replace **`enrichWithGPU`** with **`APIEnricher`**. |
| **Phase 4 — New domains** | Add **VM** (then JVM/Go) using only plugin-local code + blank import—prove the framework. |

---

## 13. What this design does not do

- **No dynamic `.so` plugins** — avoids Go plugin package instability across platforms and build modes.
- **No gRPC / sidecar plugins** — avoids latency and operational complexity on every request and CSV row batch.
- **No polyglot plugins** — JVM or Python logic would require a **different** integration model (out of scope here).
- **No runtime plugin download** — supply chain and reproducibility stay under Git + container image versioning.

---

## 14. Risks and mitigations

| Risk | Mitigation |
|------|------------|
| **Over-abstraction** | Keep trait count small; forbid “misc” traits—justify each with ≥2 plugins. |
| **Hook ordering bugs** | Integration tests with partial enable sets; document hook contracts; optional deterministic sort. |
| **Double CSV fetch** | Pass parsed rows or disk-cached path via `PluginContext` keyed by `(org, cluster, file URL)` for one ingest cycle. |
| **Migration skew when toggling plugins** | Document: enabling a plugin on an old DB requires migrations; CI runs full migration suite with all plugins registered. |
| **Shared table ownership** | **Core** owns org/cluster/account globals; plugins own domain digest and recommendation tables. |
| **RBAC / OpenAPI drift** | Gate **APIProvider** registration behind existing RBAC middleware; OpenAPI generation must include enabled routes only or document full surface as “implementation toggled.” |

---

## 15. Alternatives considered

| Approach | Why not chosen |
|----------|----------------|
| **`plugin` package (Go runtime plugins)** | Poor portability (Linux-focused), painful linking constraints, difficult debugging—unsuitable for enterprise OpenShift builds (often FIPS / static-ish binaries). |
| **gRPC microservices per domain** | Operational overhead, network latency, duplicated auth and paging semantics; contradicts “single binary” deployment model. |
| **HashiCorp go-plugin** | Same IPC/process isolation benefits/drawbacks as gRPC for this scale; unnecessary when compile-time registration suffices. |
| **Wasm extensions** | Embedding a Wasm runtime adds security surface and latency; team expertise and toolchain cost outweigh benefits for internal recommendation types. |

---

## 16. Summary

A **trait-based, compile-time plugin model** with **`ROS_ENABLED_PLUGINS` / `ROS_DISABLED_PLUGINS`** gives operators control over recommendation domains **without forking the binary**, while eliminating the worst of today’s scattered `if` chains documented in §1.2. The phased migration limits risk: first introduce indirection, then move code, then untangle GPU/node/container coupling, then add VM/JVM/Golang plugins as first-class citizens.
