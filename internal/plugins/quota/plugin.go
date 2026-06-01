// Package quota implements ResourceQuota right-sizing recommendations.
//
// The plugin compares configured quota hard limits and used consumption (from
// namespace CSV metrics) against aggregated container recommendation totals,
// then advises operators to tighten or raise namespace quotas.
package quota

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	rosapi "github.com/redhatinsights/ros-ocp-backend/internal/api"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
)

// QuotaPlugin produces namespace-level quota recommendations.
type QuotaPlugin struct {
	plugin.BasePlugin
}

func init() {
	plugin.Register(&QuotaPlugin{})
}

func (p *QuotaPlugin) Name() string { return "quota" }

func (p *QuotaPlugin) Enabled() bool { return plugin.EnabledFor(p.Name()) }

func (p *QuotaPlugin) Priority() int { return 35 }

// Quota recommendations run from report_processor after container recommendations
// are written (processContainerCSVNative). They are not registered as an IngestHook
// because namespace CSV ingest hooks fire before container recommendation_sets exist.

func (p *QuotaPlugin) RegisterRoutes(g *echo.Group) {
	if plugin.EnabledFor(plugin.KruizePluginName) {
		return
	}
	g.GET("/recommendations/openshift/quota", rosapi.GetQuotaRecommendations)
	g.GET("/recommendations/openshift/quota/detail", rosapi.GetQuotaRecommendationDetail)
}

func (p *QuotaPlugin) RetentionTables() []string {
	return []string{"quota_recommendation_sets", "quota_recommendation_history"}
}

func (p *QuotaPlugin) SweepRetention(ctx context.Context, pool *pgxpool.Pool, olderThan time.Time) error {
	if _, err := pool.Exec(ctx,
		`DELETE FROM quota_recommendation_sets WHERE last_observed_at < $1`,
		olderThan,
	); err != nil {
		return err
	}
	_, err := pool.Exec(ctx,
		`DELETE FROM quota_recommendation_history WHERE recorded_at < $1`,
		olderThan,
	)
	return err
}
