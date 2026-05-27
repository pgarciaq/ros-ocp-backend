// Package plugin defines the trait interfaces and registry for the
// ros-ocp-backend plugin architecture.
//
// Plugins are compile-time, in-process components that own one recommendation
// domain (container CPU/memory, PVC storage, GPU, node utilization, namespace
// quotas, or VolumeSnapshot staleness). Each plugin self-registers via init()
// and implements one or more trait interfaces beyond the base [Plugin] interface.
//
// # Trait Interfaces
//
//   - [Plugin] — base: name, execution phase, and enabled state
//   - [CSVIngestor] — owns CSV parsing for one or more report types
//   - [IngestHook] — runs after a CSVIngestor processes matching CSV types
//   - [APIProvider] — registers HTTP routes on the authenticated API group
//   - [APIEnricher] — decorates responses produced by other handlers
//   - [RetentionProvider] — declares tables and sweep logic for data retention
//   - [TermProvider] — declares configurable recommendation time-window terms
//   - [MigrationProvider] — reserved for future DDL ownership declaration
//
// # Registration and Lifecycle
//
// Plugins call [Register] in their init() function. At startup, cmd/start.go
// imports the "internal/plugins" aggregator package (which blank-imports all
// production plugins) and calls [Init] to apply ROS_ENABLED_PLUGINS /
// ROS_DISABLED_PLUGINS filtering.
//
// # Adding a New Plugin
//
// See internal/plugins/example/ for a complete authoring template. Key steps:
//  1. Create a package under internal/plugins/<name>/
//  2. Define a struct implementing [Plugin] + desired trait interfaces (embed [BasePlugin] for Phase 1)
//  3. Call [Register] in init()
//  4. Add your blank import to internal/plugins/plugins.go
//  5. If implementing [TermProvider], choose appropriate default terms and MaxWindowDays
//
// # Runtime Configuration
//
// Plugins are toggled via environment variables:
//   - ROS_ENABLED_PLUGINS: comma-separated allowlist (empty = all registered)
//   - ROS_DISABLED_PLUGINS: comma-separated denylist (applied after allowlist)
//
// The Kruize legacy plugin is mutually exclusive with all native plugins.
package plugin

import (
	"context"
	"io"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
)

// Plugin is the base interface every plugin implements. It declares the plugin's
// identity, execution phase, and whether it is active in the current configuration.
type Plugin interface {
	// Name returns the unique identifier for this plugin (e.g., "container", "gpu", "pvc").
	// This name is used for registry lookups, env var configuration, API responses,
	// and log fields. It must be stable across versions.
	Name() string
	// Enabled reports whether this plugin should participate in the registry.
	// Implementations typically delegate to [EnabledFor] with plugin-specific
	// exceptions (for example a template plugin that is always off, or the namespace
	// plugin that stays enabled in Kruize mode for route compatibility).
	Enabled() bool
	// Phase returns the execution phase (1=Produce, 2=Enrich, 3=Optimize). The registry
	// runs all plugins in phase N before any plugin in phase N+1. Embed [BasePlugin]
	// for the default Phase 1 (Produce).
	Phase() int
}

// CSVIngestor plugins own CSV parsing for one or more logical CSV report types.
// When a Kafka message arrives with a CSV matching one of the declared types,
// the report processor routes the reader to this plugin's IngestCSV method.
type CSVIngestor interface {
	Plugin
	// SupportedCSVTypes returns the CSV type identifiers this plugin handles
	// (e.g., "container", "storage", "snapshot", "namespace").
	SupportedCSVTypes() []string
	// IngestCSV parses the CSV content from r, processes it into domain-specific
	// digest tables, and optionally returns MetricRow slices for downstream
	// IngestHook plugins to consume. Return nil rows if no hooks need the data.
	IngestCSV(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) ([]ingestion.MetricRow, error)
}

// IngestHook runs after a CSVIngestor has parsed matching CSV types. This allows
// plugins to piggyback on another plugin's ingestion without owning the CSV
// themselves (e.g., GPU and node plugins extract their data from container CSVs).
type IngestHook interface {
	Plugin
	// HookAfterCSVTypes returns which CSV types trigger this hook (e.g., "container").
	HookAfterCSVTypes() []string
	// AfterIngest receives the MetricRow output from the primary CSVIngestor and
	// extracts domain-specific data (e.g., GPU metrics, node capacity metrics).
	AfterIngest(ctx context.Context, pool *pgxpool.Pool, rows []ingestion.MetricRow, orgID, clusterUUID string) error
}

// APIProvider registers HTTP routes on the authenticated API group. Each plugin
// owns its own URL namespace and handler implementations.
type APIProvider interface {
	Plugin
	// RegisterRoutes adds this plugin's HTTP endpoints to the given Echo group.
	// The group is pre-configured with authentication middleware.
	RegisterRoutes(g *echo.Group)
}

// APIEnricher plugins decorate responses produced by other handlers/plugins.
// This allows cross-domain enrichment (e.g., adding GPU classification to
// container recommendation responses) without tight coupling between plugins.
type APIEnricher interface {
	Plugin
	// EnrichResponse decorates the given response object with this plugin's
	// domain data. Implementations should type-assert resp to the expected
	// enrichment input type and no-op if the assertion fails.
	EnrichResponse(ctx context.Context, resp interface{}) error
}

// RetentionProvider contributes retention sweep logic for domain-owned tables.
// The housekeeper service periodically calls SweepRetention on all enabled
// RetentionProvider plugins to remove data older than the configured threshold.
type RetentionProvider interface {
	Plugin
	// RetentionTables returns the table names this plugin owns for retention purposes.
	RetentionTables() []string
	// SweepRetention removes data older than olderThan from this plugin's tables.
	// Implementations should use partition-based deletion where possible.
	SweepRetention(ctx context.Context, pool *pgxpool.Pool, olderThan time.Time) error
}

// MigrationProvider is reserved for future use when plugins can declare DDL ownership
// in docs and tooling. Nothing in the dispatch pipeline consumes this trait yet.
// When implemented, it will allow plugins to declare which database tables they own,
// enabling automated migration generation and schema documentation.
type MigrationProvider interface {
	Plugin
	// OwnedTables returns the table names owned by this plugin's domain.
	OwnedTables() []string
}

// TermConfig defines the parameters for a single recommendation term.
// It mirrors engine.TermConfig to avoid import cycles between the plugin
// and engine packages. The engine package defines the canonical type used
// internally; this copy is used only in plugin trait declarations.
type TermConfig struct {
	// Name identifies the term (typically "short", "medium", or "long").
	Name string
	// WindowDays is the number of days of historical data the recommendation
	// engine considers when computing this term's recommendation.
	WindowDays int
	// MinDataDays is the minimum number of days of data required before the
	// engine will produce a recommendation for this term. If fewer days are
	// available, the term is skipped (no recommendation rather than a bad one).
	MinDataDays int
	// DecayHalfLifeHours controls exponential decay weighting. When > 0, data
	// points are weighted by exp(-ln(2) * age_hours / DecayHalfLifeHours),
	// causing recent data to have more influence. Set to 0 for uniform weighting.
	// Note: PVC terms typically use 0 because the WLS engine applies its own
	// internal decay weighting during slope computation.
	DecayHalfLifeHours float64
}

// TermProvider plugins declare that their recommendations are parameterized by
// configurable time-window terms (short, medium, long). Plugins that implement
// this trait:
//   - Appear in GET /settings/capabilities with supports_terms: true
//   - Allow per-tenant term customization via PUT /settings/terms?recommendation_type=<name>
//   - Can have terms admin-locked via environment variables (ROS_TERMS_<PLUGIN>_<TERM>_<FIELD>)
//
// Term resolution precedence (highest wins):
//  1. Admin environment variables (locked, tenants cannot override)
//  2. Per-tenant database overrides (set via API)
//  3. Plugin defaults (returned by DefaultTerms)
type TermProvider interface {
	Plugin
	// DefaultTerms returns the plugin-specific default term configurations.
	// These values are used when no admin or tenant override exists. The slice
	// should contain exactly three entries named "short", "medium", and "long"
	// with progressively larger WindowDays values.
	DefaultTerms() []TermConfig
	// MaxWindowDays returns the maximum allowed window_days for this plugin.
	// Used to validate both admin env var overrides and tenant API PUT requests.
	// Values exceeding this maximum are clamped (env vars) or rejected (API).
	// Choose based on how far back data remains meaningful for your domain:
	//   - Fast-moving metrics (CPU, memory, GPU): 90 days
	//   - Slow-moving metrics (storage growth): 365 days
	MaxWindowDays() int
}
