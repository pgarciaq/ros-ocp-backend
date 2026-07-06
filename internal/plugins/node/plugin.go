// Package node implements the node utilization recommendation plugin.
//
// This plugin analyzes node-level CPU and memory utilization to recommend
// optimal node sizing, identify over/under-provisioned nodes, and surface
// consolidation opportunities.
//
// # Ingestion
//
// Node data piggybacks on container CSV reports: the plugin implements
// [plugin.IngestHook] to extract node capacity/allocatable metrics after
// the container ingestor runs. These are upserted into daily_node_digests.
//
// # Recommendations
//
// The node engine ([engine.RecommendNodes]) aggregates pod-level resource
// consumption per node and compares against node capacity to produce
// utilization-based recommendations. Each term produces one recommendation
// per node, considering only the data within that term's window.
//
// # Default Terms
//
//   - short: 1-day window, 1 day minimum data, no decay
//   - medium: 7-day window, 3 days minimum, 168h decay half-life
//   - long: 15-day window, 7 days minimum, 360h decay half-life
//
// MaxWindowDays is 90 because node utilization patterns are primarily driven
// by the workloads running on them (which change on release cycles) and by
// cluster autoscaler behavior, both operating on timescales under 3 months.
//
// # Traits Implemented
//
//   - [plugin.IngestHook] — extracts node data after "container" CSV processing
//   - [plugin.APIProvider] — node utilization recommendation endpoints
//   - [plugin.RetentionProvider] — sweeps daily_node_digests (node_recommendations uses date-based DELETE in engine retention)
//   - [plugin.TermProvider] — configurable short/medium/long terms (max 90 days)
package node

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	rosapi "github.com/redhatinsights/ros-ocp-backend/internal/api"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
)

// NodePlugin handles node-level utilization analysis and right-sizing
// recommendations based on aggregated pod resource consumption.
type NodePlugin struct {
	plugin.BasePlugin
}

func init() {
	plugin.Register(&NodePlugin{})
}

func (p *NodePlugin) Name() string { return "node" }

func (p *NodePlugin) Enabled() bool { return plugin.EnabledFor(p.Name()) }

func (p *NodePlugin) Priority() int { return 30 }

func (p *NodePlugin) HookAfterCSVTypes() []string {
	return []string{"container"}
}

func (p *NodePlugin) AfterIngest(ctx context.Context, pool *pgxpool.Pool, rows []ingestion.MetricRow, orgID, clusterUUID string) error {
	if err := ingestion.UpsertNodeDigests(ctx, pool, rows, orgID, clusterUUID); err != nil {
		return err
	}

	if config.HourlyNodeDigestsEnabled() {
		accumulators := ingestion.AggregateNodeDigests(rows)
		hourlyMap := ingestion.BuildHourlyNodeDigests(accumulators)
		if err := ingestion.UpsertHourlyNodeDigests(ctx, pool, orgID, clusterUUID, hourlyMap); err != nil {
			return fmt.Errorf("upserting hourly node digests: %w", err)
		}
		logging.ForOrg(orgID, clusterUUID).Infof("NodePlugin.AfterIngest: upserted %d hourly node digests", len(hourlyMap))
	}

	return nil
}

func (p *NodePlugin) RegisterRoutes(g *echo.Group) {
	if plugin.EnabledFor(plugin.KruizePluginName) {
		return
	}
	g.GET("/recommendations/openshift/nodes", rosapi.GetNodeUtilizationRecs)
	g.GET("/recommendations/openshift/nodes/utilization", rosapi.GetNodeUtilizationRecsLegacyPath)
	g.GET("/recommendations/openshift/nodes/:node", rosapi.GetNodeUtilizationDetail)
	g.GET("/recommendations/openshift/machinesets", rosapi.GetMachineSetRecommendations)

	if config.VisualInsightsEnabled() && config.HourlyNodeDigestsEnabled() {
		g.GET("/recommendations/openshift/node/:id/hourly-utilization", rosapi.GetNodeHourlyUtilization)
	}
}

func (p *NodePlugin) RetentionTables() []string {
	return []string{"daily_node_digests", "hourly_node_digests"}
}

func (p *NodePlugin) SweepRetention(ctx context.Context, pool *pgxpool.Pool, olderThan time.Time) error {
	if err := engine.SweepPartitionedTables(ctx, pool, []string{"daily_node_digests"}, olderThan.Format("200601")); err != nil {
		return err
	}

	hourlyCutoffYM := time.Now().UTC().AddDate(0, 0, -config.HourlyNodeDigestsRetentionDays()).Format("200601")
	if err := engine.SweepPartitionedTables(ctx, pool, []string{"hourly_node_digests"}, hourlyCutoffYM); err != nil {
		logging.ForOrg("", "").Warnf("SweepRetention: hourly_node_digests partitions: %v", err)
	}

	return nil
}

func (p *NodePlugin) DefaultTerms() []plugin.TermConfig {
	return []plugin.TermConfig{
		{Name: "short", WindowDays: 1, MinDataDays: 1, DecayHalfLifeHours: 0},
		{Name: "medium", WindowDays: 7, MinDataDays: 3, DecayHalfLifeHours: 168},
		{Name: "long", WindowDays: 15, MinDataDays: 7, DecayHalfLifeHours: 360},
	}
}

func (p *NodePlugin) MaxWindowDays() int { return 90 }
