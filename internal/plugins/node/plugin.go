package node

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	rosapi "github.com/redhatinsights/ros-ocp-backend/internal/api"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
)

type NodePlugin struct{}

func init() {
	plugin.Register(&NodePlugin{})
}

func (p *NodePlugin) Name() string { return "node" }

func (p *NodePlugin) Enabled() bool { return plugin.EnabledFor(p.Name()) }

func (p *NodePlugin) HookAfterCSVTypes() []string {
	return []string{"container"}
}

func (p *NodePlugin) AfterIngest(ctx context.Context, pool *pgxpool.Pool, rows []ingestion.MetricRow, orgID, clusterUUID string) error {
	return ingestion.UpsertNodeDigests(ctx, pool, rows, orgID, clusterUUID)
}

func (p *NodePlugin) RegisterRoutes(g *echo.Group) {
	if plugin.EnabledFor(plugin.KruizePluginName) {
		return
	}
	g.GET("/recommendations/openshift/nodes", rosapi.GetNodeUtilizationRecs)
	g.GET("/recommendations/openshift/nodes/utilization", rosapi.GetNodeUtilizationRecsLegacyPath)
}

func (p *NodePlugin) RetentionTables() []string {
	return []string{"daily_node_digests", "node_recommendations"}
}

func (p *NodePlugin) SweepRetention(ctx context.Context, pool *pgxpool.Pool, olderThan time.Time) error {
	return engine.SweepPartitionedTables(ctx, pool, p.RetentionTables(), olderThan.Format("200601"))
}
