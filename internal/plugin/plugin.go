// Package plugin defines the trait interfaces and registry for the
// ros-ocp-backend plugin architecture.
package plugin

import (
	"context"
	"io"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
)

// Plugin is the base interface every plugin implements.
type Plugin interface {
	Name() string
	// Enabled reports whether this plugin should participate in the registry.
	// Implementations typically delegate to [EnabledFor] with plugin-specific
	// exceptions (for example a template plugin that is always off).
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

// MigrationProvider is reserved for future use when plugins can declare DDL ownership in docs and tooling.
// Nothing in the dispatch pipeline consumes this trait yet.
type MigrationProvider interface {
	Plugin
	OwnedTables() []string
}
