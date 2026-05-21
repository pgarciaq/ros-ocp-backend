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

// --- TermProvider trait (optional) ---
// Implement TermProvider to declare that this plugin's recommendations are
// parameterized by configurable time-window terms (short, medium, long).
// Plugins implementing this trait:
//   - Appear in GET /settings/capabilities with supports_terms: true
//   - Allow per-tenant term customization via PUT /settings/terms?recommendation_type=<name>
//   - Can have terms locked by admin env vars (ROS_TERMS_<PLUGIN>_<TERM>_<FIELD>)
//
// DefaultTerms returns the plugin-specific default term configurations.
// These are used when no admin or tenant override is set.
// Choose values appropriate for your domain:
//   - Fast-moving metrics (CPU, memory): short windows (1/7/15 days)
//   - Slow-moving metrics (PVC storage): longer windows (7/30/90 days)
func (p *ExamplePlugin) DefaultTerms() []plugin.TermConfig {
	return []plugin.TermConfig{
		{Name: "short", WindowDays: 1, MinDataDays: 1, DecayHalfLifeHours: 0},
		{Name: "medium", WindowDays: 7, MinDataDays: 3, DecayHalfLifeHours: 168},
		{Name: "long", WindowDays: 15, MinDataDays: 7, DecayHalfLifeHours: 360},
	}
}

// MaxWindowDays returns the maximum window_days allowed for this plugin.
// Enforced in both admin env var validation and tenant API PUT requests.
// Choose based on how far back data is meaningful for recommendations.
func (p *ExamplePlugin) MaxWindowDays() int { return 90 }
