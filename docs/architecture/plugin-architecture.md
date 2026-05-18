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

```150:184:internal/services/report_processor.go
	var csvType types.PayloadType

	useNativeCSVIngest := !plugin.EnabledFor(plugin.KruizePluginName)

	for _, file := range kafkaMsg.Files {
		csvType = utils.DetermineCSVType(file)

		if useNativeCSVIngest && csvType == types.PayloadTypeContainer {
			if err := processContainerCSVNative(file, kafkaMsg); err != nil {
				reportProcessingFailed = true
				recordKafkaTransient(err)
			}
			continue
		}
		if useNativeCSVIngest && csvType == types.PayloadTypeNamespace {
			if err := processNamespaceCSVNative(file, kafkaMsg); err != nil {
				reportProcessingFailed = true
				recordKafkaTransient(err)
			}
			continue
		}
		if useNativeCSVIngest && csvType == types.PayloadTypeStorage {
			if err := processStorageCSVNative(file, kafkaMsg); err != nil {
				reportProcessingFailed = true
				recordKafkaTransient(err)
			}
			continue
		}
		if useNativeCSVIngest && csvType == types.PayloadTypeSnapshot {
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

**GPU and node digest upserts chained after container digest processing** (`internal/ingestion/pipeline.go`): **`ParseAndDigestCSV`** parses, validates, groups, and upserts container digests, returning **`[]MetricRow`**; **`ProcessCSVToDigests`** wraps it and invokes GPU/node upserts (soon **`IngestHook`** dispatch from **`report_processor.go`**):

```277:297:internal/ingestion/pipeline.go
// ProcessCSVToDigests is the full native engine ingestion pipeline:
// parse CSV -> validate -> group by container+day -> compute digests -> upsert to DB,
// then GPU and node digest upserts (slated to move behind ingest-hook dispatch).
func ProcessCSVToDigests(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) error {
	rows, err := ParseAndDigestCSV(ctx, pool, r, orgID, clusterUUID)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}

	if err := upsertGPUDigests(ctx, pool, rows, orgID, clusterUUID); err != nil {
		return fmt.Errorf("GPU digest upsert: %w", err)
	}

	if err := upsertNodeDigests(ctx, pool, rows, orgID, clusterUUID); err != nil {
		return fmt.Errorf("node digest upsert: %w", err)
	}

	return nil
}
```

**HTTP routes registered imperatively per domain** (`internal/api/server.go`):

```62:140:internal/api/server.go
	nativeRecommendationRoutes := !plugin.EnabledFor(plugin.KruizePluginName)

	// Container recommendations — native engine with Kruize fallback, or legacy-only.
	if nativeRecommendationRoutes {
		// Static /gpu path must register before /:recommendation-id so "gpu" is not captured as an ID.
		v1.GET("/recommendations/openshift/gpu", GetGPUSummary)
		v1.GET("/recommendations/openshift", GetRecommendationSetListWithFallback)
		v1.GET("/recommendations/openshift/:recommendation-id", GetRecommendationSetWithFallback)
	} else {
		// ...
	}

	// Project/Namespace — ...
	if nativeRecommendationRoutes {
		v1.GET("/recommendations/openshift/namespaces", GetNamespaceRecommendationSetListWithFallback)
		// ...
	}
	// ...
	// Node-level GPU time-slicing and MIG-focused listings (native engine only).
	if nativeRecommendationRoutes {
		v1.GET("/recommendations/openshift/gpu/timeslicing", GetNodeRecommendations)
		v1.GET("/recommendations/openshift/gpu/mig", GetGPUMIGRecommendations)
		v1.GET("/recommendations/openshift/nodes", GetNodeUtilizationRecs)
		v1.GET("/recommendations/openshift/nodes/utilization", GetNodeUtilizationRecsLegacyPath)
	}
	// PVC ... Snapshot ...
```

Under Echo, **`static > param > any`** matching means concrete paths such as **`/gpu`** are not consumed by **`/:recommendation-id`** when ordering follows the framework rule in §6.2 (plugin routes first; core catch-alls last). The inline comment reflects the same invariant.

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
| **Testability** | Each plugin can be unit-tested with fake pools and typed stubs; integration tests compose a subset of registered plugins. |

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

**Shared infrastructure:** Trait methods take concrete dependencies in their signatures (for example `*pgxpool.Pool`, `context.Context`, Echo groups). **[`PluginContext`](../../internal/plugin/context.go)** exists for **initialization-time** dependency injection (pool, logger entry, optional typed config snapshot) when core wires plugins at startup.

Plugins may use the same globals as the rest of the codebase — for example **[`config.GetConfig()`](../../internal/config/config.go)** and **[`logging.GetLogger()`](../../internal/logging/logging.go)** — where that matches existing patterns. Prefer explicit parameters on trait methods when the dependency is always needed for that operation.

### 3.1 Configuration boundary

- **Single load:** The root **`Config`** struct is populated **once at process startup** via Viper (existing pattern). This remains the central source of truth for service-wide settings.
- **Plugins:** May read **`config.GetConfig()`** like other packages, or receive snapshots via **`PluginContext`** during wiring; avoid duplicating Viper reads for values already on **`Config`** when practical.
- **Cleanup (directional):** Domain-specific toggles that still use raw **`os.Getenv`** should migrate onto the central **`Config`** over time for consistency.

---

## 4. Plugin interfaces (trait-based)

Not every plugin implements every trait. Core code uses **type assertions** (see **[`plugin.ByTrait`](../../internal/plugin/registry.go)**) to detect capabilities—no “fat” interface forcing empty methods.

The canonical definitions live in **[`internal/plugin/plugin.go`](../../internal/plugin/plugin.go)**. Signatures use concrete dependencies (`*pgxpool.Pool`, `echo.Group`, etc.) rather than threading **`PluginContext`** through every hot-path call.

```go
package plugin

// Plugin is the base interface every plugin implements.
type Plugin interface {
	Name() string
	// Enabled reports whether this plugin should participate in the registry.
	Enabled() bool
}

// CSVIngestor plugins own CSV parsing for one or more logical CSV report types.
type CSVIngestor interface {
	Plugin
	SupportedCSVTypes() []string
	IngestCSV(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) ([]ingestion.MetricRow, error)
}

// IngestHook runs after a CSVIngestor has parsed matching CSV types.
type IngestHook interface {
	Plugin
	HookAfterCSVTypes() []string
	AfterIngest(ctx context.Context, pool *pgxpool.Pool, rows []ingestion.MetricRow, orgID, clusterUUID string) error
}

// APIProvider registers HTTP routes on the authenticated API group.
type APIProvider interface {
	Plugin
	RegisterRoutes(g *echo.Group)
}

// APIEnricher plugins decorate responses produced by other handlers/plugins.
type APIEnricher interface {
	Plugin
	EnrichResponse(ctx context.Context, resp interface{}) error
}

// RetentionProvider contributes retention sweep logic for domain-owned tables.
type RetentionProvider interface {
	Plugin
	RetentionTables() []string
	SweepRetention(ctx context.Context, pool *pgxpool.Pool, olderThan time.Time) error
}

// MigrationProvider documents tables owned by plugin DDL in the central migrations tree.
type MigrationProvider interface {
	Plugin
	OwnedTables() []string
}
```

```5:22:internal/ingestion/models.go
// MetricRow represents a single parsed row from an OCP metrics CSV file,
// with all numeric values already converted to integer types (millicores, KiB).
type MetricRow struct {
	IntervalStart time.Time
	IntervalEnd   time.Time
	Namespace     string
	WorkloadName  string
	WorkloadType  string
	ContainerName string
	Pod           string
	Node          string

	CPURequestMC     int64
	CPULimitMC       int64
```

**IngestHook data contract (confirmed — Option B):** After the container **`CSVIngestor`** parses the CSV, hooks receive **`[]MetricRow`** — the existing struct in [`internal/ingestion/models.go`](../../internal/ingestion/models.go). That type is already the de facto DTO for **`upsertGPUDigests`** and **`upsertNodeDigests`** in the ingestion pipeline. **Option C** (hooks re-query the DB after the container writer persists rows) was rejected: it adds I/O, couples hooks to table shapes, and complicates tests. Passing **`[]MetricRow`** avoids an extra DB round-trip; hooks are trivially unit-testable with in-memory slices; and **`MetricRow`** can grow additively as the CSV schema evolves—hooks that only read fields they need remain compatible.

**Note:** Kafka/message types and routing details stay in **`internal/types`** and **`internal/api`**; traits reference **`ingestion.MetricRow`** where needed rather than duplicating models.

---

## 5. Registry implementation

Implementation: **[`internal/plugin/registry.go`](../../internal/plugin/registry.go)**.

- **`Register(p Plugin)`** — appends to a package-level slice; called from each plugin’s **`init()`**. **`Register(nil)` panics** with a clear message — accidental nil registration is a programmer error.
- **Convention:** Always register **pointer receivers** so embedding/`interface` satisfaction works as intended, e.g. **`plugin.Register(&MyPlugin{})`**.
- **`Enabled()`** — returns plugins that pass **`p.Enabled()`** (implementations usually delegate to **`EnabledFor(Name())`** per plugin rules), then applies **kruize exclusivity**: if any plugin named **`kruize`** is enabled, only kruize plugins are returned and others are skipped (with a one-time warning).

Env semantics (**[`EnabledFor`](../../internal/plugin/registry.go)**):

- If **`ROS_ENABLED_PLUGINS`** is non-empty: **allowlist** only (comma-separated names matching **`Plugin.Name()`**).
- If empty: every plugin is eligible except **`kruize`** (off unless allowlisted), then **`ROS_DISABLED_PLUGINS`** applies as a blocklist.

- **Ordering** — registry preserves registration order (import order in **`main`**). For deterministic hooks, core may **sort** hooks or document that hook order follows registration order after filtering.

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

**Hook orchestration rule:** Ingest-hook dispatch (which hooks run after which CSV ingestor, ordering, non-fatal errors) is orchestrated from **`internal/services/report_processor.go`**, not from **`internal/ingestion/`**. That avoids import cycles (**`ingestion`** imports **`plugin`** for **`CSVIngestor`** / **`[]MetricRow`** plumbing).

**Container CSV seam:** [`ParseAndDigestCSV`](../../internal/ingestion/pipeline.go) parses and validates (via [`ParseCSVRows`](../../internal/ingestion/csvparser.go)), groups, upserts container samples/digests, and returns **`[]MetricRow`**. [`ProcessCSVToDigests`](../../internal/ingestion/pipeline.go) wraps it and today still calls GPU/node digest upserts inline; PR2 moves those behind **`IngestHook`** dispatch from **`report_processor.go`**.

Replace the fixed sequence of `if csvType == …` with:

1. Compute `csvType` (eventually **`CSVIngestor.SupportedCSVTypes()`** may supersede central string matching, or **`DetermineCSVType`** becomes a fallback delegating to plugins).
2. For each enabled plugin implementing **`CSVIngestor`**, if the file matches a supported type, call **`IngestCSV`** (or equivalent wiring).
3. For each enabled **`IngestHook`** whose **`HookAfterCSVTypes()`** overlaps the ingestor’s CSV type keys, call **`AfterIngest`** with the parsed **`[]MetricRow`** and identifiers — same in-memory contract as §4 (Option B).

This preserves semantics while allowing GPU/node code to move out of unconditional tail calls inside **`ProcessCSVToDigests`**.

### 6.1.1 Hook failure semantics (confirmed)

**`IngestHook`** invocations are **non-fatal by default**. If a hook (for example GPU digest upsert) returns an error, core **logs** it, **increments an error metric**, and **continues** processing the remainder of the pipeline (including other hooks and downstream steps that do not depend on the failed hook’s side effects). **Container recommendations are the primary product**: an auxiliary plugin bug must **not** prevent container CSV data and container-native recommendations from landing.

This behavior is a core benefit of the plugin architecture: **isolation** between domains limits blast radius compared to today’s inlined chains where a GPU digest failure can fail the whole container ingest path (see [`internal/ingestion/pipeline.go`](../../internal/ingestion/pipeline.go) GPU/node upserts returning errors from `ProcessCSVToDigests`).

**Future extension:** A **`Critical() bool`** trait (or equivalent) on hooks could mark must-succeed contributors that should abort the batch—**not** part of the initial design.

### 6.2 API (`server.go` / handlers)

After constructing Echo groups and middleware:

```go
for _, p := range plugin.Enabled() {
	if api, ok := p.(plugin.APIProvider); ok {
		api.RegisterRoutes(v1)
	}
}
```

**Route ordering (confirmed):** No **`Priority() int`** on **`APIProvider`** is required. Echo’s router matches **`static > param > any`**, so concrete paths such as **`/recommendations/openshift/gpu`** or **`/recommendations/openshift/namespaces`** naturally win over **`/:recommendation-id`** without explicit priorities. **Rule for core:** register catch-all or parameterized routes (**`/:recommendation-id`** and similar) **after** the generic plugin registrar loop so every plugin has registered its static segments first. Plugins expose **full path strings** in their **`RegisterRoutes`** implementations (as today in [`internal/api/server.go`](../../internal/api/server.go)); contributors must not rely on manual ordering beyond “catch-all last.”

Container list/detail handlers resolve **`APIEnricher`** plugins from the registry instead of calling **`enrichWithGPU`** directly (trait shape: **`EnrichResponse`** in §4).

### 6.3 Retention (`internal/engine/retention.go`)

Split today’s monolithic `retainedTables` slice:

- **Framework-owned** tables stay in core (for example org/cluster/account bookkeeping if applicable).
- Each **`RetentionProvider`** appends domain-specific partition parents or runs targeted `DELETE`s.

Core orchestrates (trait shape today uses a cutoff timestamp — see **`RetentionProvider`** in §4):

```go
for _, p := range plugin.Enabled() {
	if rp, ok := p.(plugin.RetentionProvider); ok {
		if err := rp.SweepRetention(ctx, pool, olderThan); err != nil {
			errs = append(errs, err)
		}
	}
}
```

### 6.4 Migrations

**Central directory only (confirmed):** All golang-migrate SQL stays in the repository’s single **`migrations/`** directory with **sequential numeric prefixes** on filenames. Plugin-contributed DDL is added as new numbered files in that same tree—**not** under **`internal/plugins/<name>/migrations/`** or other per-plugin paths.

**Convention:** Each plugin-authored file begins with a SQL comment header identifying ownership, e.g. **`-- plugin: gpu`**, so operators and reviewers can see domain ownership without splitting the migrate graph.

The golang-migrate driver loads **one** sequential chain (core + plugin contributions in numeric order). Disabled plugins still imply an operational constraint: operators must not enable a domain on an existing cluster until its numbered migrations have been applied—same as today when toggling features.

### 6.5 Legacy Kruize engine (optional plugin — confirmed stance)

The **`WithFallback`** handlers and **legacy dataframe CSV ingestion** paths keep a large **Kruize-facing** surface alive alongside the native Go engine: roughly **2.5k+ lines** across the HTTP client ([`internal/utils/kruize/`](../../internal/utils/kruize/)), payload types ([`internal/types/kruizePayload/`](../../internal/types/kruizePayload/)), the recommendation poller (**`recommendation_poller.go`**), legacy ingestion plumbing, and handler fallback branches.

**External dependencies** include HTTP calls to the Kruize server, consumption/production on the Kafka recommendation topic, and **`KRUIZE_*`** configuration variables.

**Decision:** Treat Kruize integration as an **optional legacy plugin** behind the same registry as native domains. **`ROS_USE_NATIVE_ENGINE`** on **`Config`** is **deprecated** (see §11.1); **`plugin.EnabledFor(plugin.KruizePluginName)`** is the unified runtime signal for whether CSV dispatch and HTTP routing use native vs legacy branches. **Remove Kruize-only codepaths only** when product commits to **native-only** operation **and** all tenants are migrated off Kruize-backed flows—avoid stranding operators mid-cutover.

**Mutual exclusivity with native plugins (confirmed):**

- The **Kruize legacy path is disabled by default** — deployments run native plugins unless **`ROS_ENABLED_PLUGINS`** lists **`kruize`** (or the deprecated compat bridge applies — §11.1).
- **Enabling `kruize` automatically disables all other plugins** (the native engine’s domain plugins). The two engines are **mutually exclusive**: operators run **either** native plugins **or** the Kruize legacy plugin, **never both at once**.
- **Startup enforcement:** When the plugin registry determines that Kruize is active, it **logs a warning** and **skips registration of every other plugin** so native hooks/routes/retention do not double-process the same workloads.
- **Rationale:** Running Kruize alongside native plugins would emit **conflicting or duplicate recommendations** for the same workloads and risks **double-counting** savings or churn in APIs/UI. Mutual exclusivity keeps persisted state and API responses consistent with a single active engine.

---

## 7. Hard coupling points and resolutions

| Coupling | Today | Resolution |
|----------|--------|------------|
| **Shared infrastructure** | Packages import `db.GetPool()` and **`config.GetConfig()`** freely. | Trait methods receive **`pool`** (and similar) explicitly; **`PluginContext`** is optional for startup wiring (§3). Plugins may use **`logging.GetLogger()`** like the rest of the codebase. |
| **Container CSV fan-out** | **`ProcessCSVToDigests`** wraps **`ParseAndDigestCSV`** then always upserts GPU + node digests; **`processContainerCSVNative`** runs node recommendations. | **Container** path exposes **`[]MetricRow`** after **`ParseAndDigestCSV`**. **GPU** / **node** become **`IngestHook`** implementations (same structs **`MetricRow`** already feeds today). Node recommendation batch remains **`node`** plugin responsibility after container ingest completes. |
| **GPU API enrichment** | `handlers.go` calls `enrichWithGPU` unconditionally on native lists. | **GPU** plugin implements **`APIEnricher`** so container handlers delegate enrichment via the registry instead of hard-coded calls. |
| **Retention table lists** | Single `retainedTables` array mixes domains. | Partition parents move under **`RetentionProvider`** per plugin; core retains only cross-cutting tables. |

---

## 8. Plugin directory structure (proposed)

```
internal/plugins/
  registry.go            # init-time wiring helpers (optional PluginContext)
  traits.go              # re-export or thin wrappers if needed

  _example/
    README.md            # trait contract for plugin authors (see §8.1)
    plugin.go            # stub implementations of every trait (compile-time interface check)

  container/
    plugin.go            # init() + composite struct
    ingest.go            # CSVIngestor
    hooks_export.go      # registers nothing — hooks live in gpu/node
    api.go               # APIProvider
    retention.go         # RetentionProvider (samples + daily_container_digests + ...)
    migrations.go        # optional MigrationProvider / OwnedTables metadata — SQL files live only in repo migrations/

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

### 8.1 Sample plugin (`_example`) — confirmed

The repository includes an **`_example`** plugin under **`internal/plugins/_example/`** that implements **all** trait interfaces with **stub/logging bodies** (no production behavior). It serves dual purposes:

- **Authoring template** — copy/adapt when adding a new plugin; shows required method shapes and naming conventions.
- **Compile-time check** — proves the trait set is **coherent and usable** in one package (if `_example` builds, the interfaces remain satisfiable together).

The directory includes its own **`README.md`** explaining the trait contract, registration expectations, and how `_example` differs from enabled domains.

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

*Cells marked “Shared baseline” imply DDL already lives under the central **`migrations/`** directory; **`MigrationProvider`** (if implemented) documents ownership via **`OwnedTables()`** and SQL **`-- plugin:`** headers—not separate migrate trees.*

---

## 10. Adding a new plugin (example: OpenShift Virtualization VMs)

1. **Define plugin name** — `vm` (stable, lowercase, matches env vars).
2. **Create package** `internal/plugins/vm/` with `init() { plugin.Register(&VMPlugin{}) }`.
3. **Implement traits:**
   - **`CSVIngestor`** if a distinct CSV / payload type exists; otherwise **`IngestHook`** if VM metrics piggyback on an existing file.
   - **`APIProvider`** for `/recommendations/openshift/vms` (exact paths follow OpenAPI policy).
   - **`RetentionProvider`** for `daily_vm_digests` and VM recommendation partitions.
   - **`MigrationProvider`** when DDL is plugin-owned.
4. **Add SQL** as the next sequential migration(s) under the central **`migrations/`** directory with a **`-- plugin: vm`** (or matching name) header comment—no **`internal/plugins/vm/migrations/`** subtree.
5. **Blank-import** `_ ".../internal/plugins/vm"` from the main binary.
6. **Update operator / ingest documentation** so the correct files arrive on Kafka when the plugin is enabled.

No edits to `server.go`’s route list should be required beyond the generic registrar loop once Phase 1 lands.

---

## 11. Enable / disable mechanism

| Variable | Semantics |
|----------|-----------|
| **`ROS_ENABLED_PLUGINS`** | Comma-separated allowlist. When set, **only** these plugins run (names match `Plugin.Name()`). |
| **`ROS_DISABLED_PLUGINS`** | Comma-separated blocklist applied when **allowlist is unset**: defaults minus blocked names. |
| **Both unset** | Plugins use **`Plugin.Enabled()`** (typically **`EnabledFor(Name())`**); **`kruize`** stays off unless allowlisted; **`ROS_DISABLED_PLUGINS`** subtracts from that default set (see §5). |

### 11.1 Native vs. legacy Kruize (unified switch)

Legacy vs native dispatch no longer depends on a separate **`cfg.UseNativeEngine`** branch in parallel with the registry:

- **Runtime signal:** [`report_processor.go`](../../internal/services/report_processor.go) and [`server.go`](../../internal/api/server.go) use **`!plugin.EnabledFor(plugin.KruizePluginName)`** (native CSV ingest paths and native HTTP routes when the **`kruize`** plugin is off).
- **Preferred configuration:** `ROS_ENABLED_PLUGINS=kruize` to run legacy Kruize only (native plugins excluded by registry exclusivity).
- **Deprecated:** `ROS_USE_NATIVE_ENGINE` on [`Config`](../../internal/config/config.go) is retained for backward compatibility only.
- **Compatibility bridge:** [`ApplyLegacyUseNativeEngineEnv`](../../internal/plugin/registry.go) runs from [`main`](../../rosocp.go) **before** [`cmd.Execute()`](../../cmd/root.go). When **`UseNativeEngine`** is **`false`** (typically **`ROS_USE_NATIVE_ENGINE=false`**) and **`ROS_ENABLED_PLUGINS`** is unset/whitespace-only, the bridge sets **`ROS_ENABLED_PLUGINS=kruize`** and emits a **deprecation warning** directing operators to migrate to the explicit allowlist.

If **`ROS_ENABLED_PLUGINS`** is already set, the bridge does **not** override it (allowlist wins).

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
| **Phase 1 — Skeleton** | Add **`internal/plugin`** registry, env parsing, unit tests, optional **`PluginContext`**. Refactor **`report_processor.go`**, **`server.go`**, and **`retention.go`** to loop plugins while existing logic lives in **adapter structs** calling today's functions. |
| **Phase 2 — Simple domains** | Migrate **PVC** and/or **snapshot** end-to-end into plugin packages (low cross-talk). |
| **Phase 3 — Coupled domains** | Break **`ProcessCSVToDigests`** GPU/node tail into **`IngestHook`** implementations; move **`runNodeRecommendations`** invocation to **node** plugin; replace **`enrichWithGPU`** with **`APIEnricher`**. |
| **Phase 4 — New domains** | Add **VM** (then JVM/Go) using only plugin-local code + blank import—prove the framework. |

Detailed testing expectations per phase are in [§16](#16-test-strategy).

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
| **Double CSV fetch** | Eliminated for hooks on the container path by passing **`[]MetricRow`** from the primary ingestor (§4). Hooks must not re-download the same URL unless a future use case explicitly requires it. |
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
| **IngestHook loads rows from DB after container write (Option C)** | Extra I/O and tight coupling to table schemas; hooks harder to unit test — **`[]MetricRow`** passed in-process is the confirmed contract (§4). |

---

## 16. Test strategy

**Current state (baseline):** **74** test files totaling roughly **16.7k** lines; tests are **colocated** with production code. Approximately **53** files are **pure unit tests**; approximately **21** require **PostgreSQL** via **testcontainers**.

**Principle:** Existing tests **stay where they are** today—they are the **acceptance criteria** for the refactor. If all **74** files still pass after each extraction phase, that phase is considered behavior-preserving.

**Phased approach:**

| Phase | Testing focus |
|-------|----------------|
| **Phase 1 — Framework** | Add tests for the **registry**, trait dispatch loops, and optional **`PluginContext`** wiring. The **`_example`** plugin proves traits stay callable through registration. **Existing tests unchanged**—they provide the regression safety net. |
| **Phase 2 — Container plugin extraction** | Keep **`recommend_cpu_test.go`**, **`recommend_memory_test.go`**, **`digest_test.go`**, and related container tests **in place** and passing. Add a **small integration test** asserting the container plugin **registers** and the dispatch loop **invokes** it for container CSVs. |
| **Phase 3+ — GPU, node, namespace plugins** | Same pattern: **domain-specific tests remain** where they live today; add **wiring tests** that enabled plugins are registered and hooks/routes/retention hooks fire as expected. |
| **Post-extraction (optional)** | **Cosmetic** re-home tests under each plugin’s directory—organizational only; **no functional requirement**. |

**Known refactor prerequisite:** **`handlers_node_recs_integration_test.go`** (~886 lines) mixes **GPU time-slicing** scenarios with **node utilization** scenarios. It should be **split** when those concerns become separate plugins (aligned with §12 Phase 3 / coupled domains).

---

## 17. Summary

A **trait-based, compile-time plugin model** with **`ROS_ENABLED_PLUGINS` / `ROS_DISABLED_PLUGINS`** gives operators control over recommendation domains **without forking the binary**, while eliminating the worst of today’s scattered `if` chains documented in §1.2. Confirmed mechanics: **`IngestHook`** receives **`[]MetricRow`** (§4), hook failures are **non-fatal by default** (§6.1.1), migrations remain **one central numbered directory** with **`-- plugin:`** headers (§6.4), Echo **static-before-param** routing avoids a **`Priority()`** API when core registers catch-alls **last** (§6.2), **Kruize stays an optional legacy path** until native-only is mandatory (§6.5), **trait methods take concrete deps** (pool, Echo groups) while **`PluginContext`** remains available for optional startup wiring (§3), plugins may use **`config.GetConfig()`** / **`logging.GetLogger()`** like other packages (§3.1), an **`_example`** plugin documents traits at compile time (§8.1), and testing stays anchored on existing coverage plus phased wiring tests ([§16](#16-test-strategy)). The phased migration limits risk: first introduce indirection, then move code, then untangle GPU/node/container coupling, then add VM/JVM/Golang plugins as first-class citizens.
