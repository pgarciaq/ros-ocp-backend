// Package example is the `_example` template plugin from docs/architecture/plugin-architecture.md.
//
// Go build tooling ignores directories whose names begin with '_', so this package lives under
// internal/plugins/example while [ExamplePlugin.Name] still reports "_example".
//
// It is disabled by default ([ExamplePlugin.Enabled] is always false).
package example

import (
	"context"
	"io"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
)

func init() {
	plugin.Register(&ExamplePlugin{})
}

// ExamplePlugin is a stub implementation used as a authoring template only.
type ExamplePlugin struct{}

func (p *ExamplePlugin) Name() string {
	return "_example"
}

func (p *ExamplePlugin) Enabled() bool {
	return false
}

func (p *ExamplePlugin) SupportedCSVTypes() []string {
	logging.GetLogger().WithField("plugin", p.Name()).Debug("ExamplePlugin.SupportedCSVTypes")
	return nil
}

func (p *ExamplePlugin) IngestCSV(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) ([]ingestion.MetricRow, error) {
	logging.GetLogger().WithField("plugin", p.Name()).Debug("ExamplePlugin.IngestCSV")
	return nil, nil
}

func (p *ExamplePlugin) HookAfterCSVTypes() []string {
	logging.GetLogger().WithField("plugin", p.Name()).Debug("ExamplePlugin.HookAfterCSVTypes")
	return nil
}

func (p *ExamplePlugin) AfterIngest(ctx context.Context, pool *pgxpool.Pool, rows []ingestion.MetricRow, orgID, clusterUUID string) error {
	logging.GetLogger().WithField("plugin", p.Name()).Debug("ExamplePlugin.AfterIngest")
	return nil
}

func (p *ExamplePlugin) RegisterRoutes(g *echo.Group) {
	logging.GetLogger().WithField("plugin", p.Name()).Debug("ExamplePlugin.RegisterRoutes")
}

func (p *ExamplePlugin) EnrichResponse(ctx context.Context, resp interface{}) error {
	logging.GetLogger().WithField("plugin", p.Name()).Debug("ExamplePlugin.EnrichResponse")
	return nil
}

func (p *ExamplePlugin) RetentionTables() []string {
	logging.GetLogger().WithField("plugin", p.Name()).Debug("ExamplePlugin.RetentionTables")
	return nil
}

func (p *ExamplePlugin) SweepRetention(ctx context.Context, pool *pgxpool.Pool, olderThan time.Time) error {
	logging.GetLogger().WithField("plugin", p.Name()).Debug("ExamplePlugin.SweepRetention")
	return nil
}

func (p *ExamplePlugin) OwnedTables() []string {
	logging.GetLogger().WithField("plugin", p.Name()).Debug("ExamplePlugin.OwnedTables")
	return nil
}
