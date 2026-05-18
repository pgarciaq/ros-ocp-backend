package namespace

import (
	"context"
	"io"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	rosapi "github.com/redhatinsights/ros-ocp-backend/internal/api"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
)

type NamespacePlugin struct{}

func init() {
	plugin.Register(&NamespacePlugin{})
}

func (p *NamespacePlugin) Name() string { return "namespace" }

// Enabled controls registry visibility for this plugin. Namespace HTTP routes must stay available
// in Kruize mode; native CSV ingestion still respects mutual exclusivity via [plugin.Enabled].
func (p *NamespacePlugin) Enabled() bool {
	if plugin.EnabledFor(plugin.KruizePluginName) {
		return true
	}
	return plugin.EnabledFor(p.Name())
}

func (p *NamespacePlugin) SupportedCSVTypes() []string {
	return []string{"namespace"}
}

func (p *NamespacePlugin) IngestCSV(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) ([]ingestion.MetricRow, error) {
	if err := ingestion.ProcessNamespaceCSVToDigests(ctx, pool, r, orgID, clusterUUID); err != nil {
		return nil, err
	}
	return nil, nil
}

func (p *NamespacePlugin) RegisterRoutes(g *echo.Group) {
	native := !plugin.EnabledFor(plugin.KruizePluginName)
	if native {
		g.GET("/recommendations/openshift/namespaces", rosapi.GetNamespaceRecommendationSetListWithFallback)
		g.GET("/recommendations/openshift/namespaces/:recommendation-id", rosapi.GetNamespaceRecommendationSetWithFallback)
		g.GET("/openshift/namespace/recommendations", rosapi.GetNamespaceRecommendationSetListWithFallback)
		g.GET("/recommendations/openshift/namespace/:recommendation-id", rosapi.GetNamespaceRecommendationSetWithFallback)
		return
	}
	g.GET("/recommendations/openshift/namespaces", rosapi.GetNamespaceRecommendationSetList)
	g.GET("/recommendations/openshift/namespaces/:recommendation-id", rosapi.GetNamespaceRecommendationSet)
	g.GET("/openshift/namespace/recommendations", rosapi.GetNamespaceRecommendationSetList)
	g.GET("/recommendations/openshift/namespace/:recommendation-id", rosapi.GetNamespaceRecommendationSet)
}

func (p *NamespacePlugin) RetentionTables() []string {
	return []string{"daily_namespace_digests", "namespace_usage_samples"}
}

func (p *NamespacePlugin) SweepRetention(ctx context.Context, pool *pgxpool.Pool, olderThan time.Time) error {
	return engine.SweepPartitionedTables(ctx, pool, p.RetentionTables(), olderThan.Format("200601"))
}
