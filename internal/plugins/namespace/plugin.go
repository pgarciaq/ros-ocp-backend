// Package namespace implements the namespace-level resource recommendation plugin.
//
// This plugin aggregates container-level metrics at the namespace granularity
// to recommend namespace resource quotas (CPU and memory requests/limits).
// It supports multiple recommendation "engines" (profiles) per term: conservative
// and aggressive, giving cluster admins a range of options.
//
// # Ingestion
//
// Namespace data arrives in "namespace" CSV reports from the koku-metrics-operator,
// containing namespace-level aggregated CPU and memory metrics. These are processed
// into daily_namespace_digests.
//
// # Recommendations
//
// The namespace engine ([engine.RecommendAllNamespaces]) processes digests within
// each term's window using percentile-based sizing per engine profile:
//   - Conservative: higher percentiles → fewer OOM kills, more headroom
//   - Aggressive: lower percentiles → tighter packing, higher utilization
//
// Each (namespace × term × engine) combination produces one recommendation.
//
// # Default Terms
//
//   - short: 1-day window, 1 day minimum data, no decay
//   - medium: 7-day window, 3 days minimum, 168h decay half-life
//   - long: 15-day window, 7 days minimum, 360h decay half-life
//
// MaxWindowDays is 90 because namespace quotas reflect aggregate workload
// behavior which, like individual containers, shifts with release cycles.
//
// # Special Behavior
//
// Unlike other plugins, NamespacePlugin stays enabled even in Kruize mode
// because namespace HTTP routes need to remain accessible regardless of the
// recommendation engine backend.
//
// # Traits Implemented
//
//   - [plugin.CSVIngestor] — parses "namespace" CSV type
//   - [plugin.APIProvider] — namespace recommendation list/detail endpoints
//   - [plugin.RetentionProvider] — sweeps daily_namespace_digests, namespace_usage_samples
//   - [plugin.TermProvider] — configurable short/medium/long terms (max 90 days)
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

// NamespacePlugin handles namespace-level resource quota recommendations
// using aggregated container metrics and multiple engine profiles.
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

func (p *NamespacePlugin) DefaultTerms() []plugin.TermConfig {
	return []plugin.TermConfig{
		{Name: "short", WindowDays: 1, MinDataDays: 1, DecayHalfLifeHours: 0},
		{Name: "medium", WindowDays: 7, MinDataDays: 3, DecayHalfLifeHours: 168},
		{Name: "long", WindowDays: 15, MinDataDays: 7, DecayHalfLifeHours: 360},
	}
}

func (p *NamespacePlugin) MaxWindowDays() int { return 90 }
