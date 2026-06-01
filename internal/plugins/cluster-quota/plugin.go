// Package clusterquota implements OpenShift ClusterResourceQuota recommendations.
package clusterquota

import (
	"context"
	"io"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	rosapi "github.com/redhatinsights/ros-ocp-backend/internal/api"
	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
	"github.com/redhatinsights/ros-ocp-backend/internal/types"
)

// ClusterQuotaPlugin produces cluster-level ResourceQuota recommendations.
type ClusterQuotaPlugin struct {
	plugin.BasePlugin
}

func init() {
	plugin.Register(&ClusterQuotaPlugin{})
}

func (p *ClusterQuotaPlugin) Name() string { return "cluster-quota" }

func (p *ClusterQuotaPlugin) Enabled() bool { return plugin.EnabledFor(p.Name()) }

func (p *ClusterQuotaPlugin) Priority() int { return 36 }

func (p *ClusterQuotaPlugin) SupportedCSVTypes() []string {
	return []string{string(types.PayloadTypeClusterQuota)}
}

func (p *ClusterQuotaPlugin) IngestCSV(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) ([]ingestion.MetricRow, error) {
	if err := ingestion.ProcessClusterQuotaCSV(ctx, pool, r, orgID, clusterUUID); err != nil {
		return nil, err
	}
	return nil, nil
}

func (p *ClusterQuotaPlugin) RegisterRoutes(g *echo.Group) {
	if plugin.EnabledFor(plugin.KruizePluginName) {
		return
	}
	g.GET("/recommendations/openshift/cluster-quota", rosapi.GetClusterQuotaRecommendations)
	g.GET("/recommendations/openshift/cluster-quota/detail", rosapi.GetClusterQuotaRecommendationDetail)
}

func (p *ClusterQuotaPlugin) RetentionTables() []string {
	return []string{"cluster_quota_recommendation_sets", "daily_cluster_quota_digests"}
}

func (p *ClusterQuotaPlugin) SweepRetention(ctx context.Context, pool *pgxpool.Pool, olderThan time.Time) error {
	_, err := pool.Exec(ctx,
		`DELETE FROM cluster_quota_recommendation_sets WHERE updated_at < $1`,
		olderThan,
	)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx,
		`DELETE FROM daily_cluster_quota_digests WHERE created_at < $1`,
		olderThan,
	)
	return err
}
