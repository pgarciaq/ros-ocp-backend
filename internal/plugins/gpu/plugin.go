// Package gpu implements the GPU recommendation plugin.
//
// This plugin handles NVIDIA GPU utilization analysis, classification, and
// right-sizing recommendations including MIG (Multi-Instance GPU) partitioning
// and time-slicing strategies.
//
// # Ingestion
//
// GPU data piggybacks on container CSV reports: the plugin implements [plugin.IngestHook]
// to extract GPU-specific columns (DCGM profiling metrics, GPU model, MIG profiles)
// after the container ingestor runs. These are upserted into gpu_container_digests.
//
// # Recommendations
//
// The GPU engine ([engine.RecommendGPU]) analyzes utilization patterns to classify
// GPU usage (compute-bound, memory-bound, idle, mixed) and recommends:
//   - Current GPU appropriateness (over/under-provisioned)
//   - MIG partition profiles when applicable
//   - Time-slicing replica counts for shared GPU scenarios
//   - Node-level GPU scheduling recommendations
//
// # Default Terms
//
//   - short: 1-day window, 1 day minimum data, no decay
//   - medium: 7-day window, 3 days minimum, 168h decay half-life
//   - long: 15-day window, 7 days minimum, 360h decay half-life
//
// MaxWindowDays is 90 because GPU workloads (ML training, inference) tend to
// change characteristics with model/framework updates on a similar cadence to
// CPU/memory container workloads.
//
// # Traits Implemented
//
//   - [plugin.IngestHook] — extracts GPU data after "container" CSV processing
//   - [plugin.APIEnricher] — decorates container list/detail responses with GPU info
//   - [plugin.APIProvider] — GPU summary, time-slicing, and MIG endpoints
//   - [plugin.RetentionProvider] — sweeps gpu_container_digests
//   - [plugin.TermProvider] — configurable short/medium/long terms (max 90 days)
package gpu

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

// GPUPlugin handles GPU utilization analysis, classification, and right-sizing
// recommendations (MIG partitioning and time-slicing).
type GPUPlugin struct {
	plugin.BasePlugin
}

func init() {
	plugin.Register(&GPUPlugin{})
}

func (p *GPUPlugin) Name() string { return "gpu" }

func (p *GPUPlugin) Enabled() bool { return plugin.EnabledFor(p.Name()) }

func (p *GPUPlugin) Priority() int { return 20 }

func (p *GPUPlugin) HookAfterCSVTypes() []string {
	return []string{"container"}
}

func (p *GPUPlugin) AfterIngest(ctx context.Context, pool *pgxpool.Pool, rows []ingestion.MetricRow, orgID, clusterUUID string) error {
	return ingestion.UpsertGPUDigests(ctx, pool, rows, orgID, clusterUUID)
}

func (p *GPUPlugin) EnrichResponse(ctx context.Context, resp interface{}) error {
	in, ok := resp.(*rosapi.NativeContainerEnrichmentInput)
	if !ok || in == nil || !plugin.EnabledFor(p.Name()) {
		return nil
	}
	rosapi.EnrichNativeContainerResultsWithGPU(ctx, in.OrgID, in.Results)
	return nil
}

func (p *GPUPlugin) RegisterRoutes(g *echo.Group) {
	if plugin.EnabledFor(plugin.KruizePluginName) {
		return
	}
	g.GET("/recommendations/openshift/gpu", rosapi.GetGPUSummary)
	g.GET("/recommendations/openshift/gpu/timeslicing", rosapi.GetNodeRecommendations)
	g.GET("/recommendations/openshift/gpu/timeslicing/history", rosapi.GetNodeGPUTimeslicingRecommendationHistory)
	g.GET("/recommendations/openshift/gpu/mig", rosapi.GetGPUMIGRecommendations)
}

func (p *GPUPlugin) RetentionTables() []string {
	return []string{
		"gpu_container_digests",
		"node_gpu_timeslicing_recommendations",
		"node_gpu_timeslicing_recommendation_history",
	}
}

func (p *GPUPlugin) SweepRetention(ctx context.Context, pool *pgxpool.Pool, olderThan time.Time) error {
	if err := engine.SweepPartitionedTables(ctx, pool, []string{"gpu_container_digests"}, olderThan.Format("200601")); err != nil {
		return err
	}
	if _, err := pool.Exec(ctx,
		`DELETE FROM node_gpu_timeslicing_recommendations WHERE COALESCE(last_seen_at, updated_at) < $1`,
		olderThan,
	); err != nil {
		return err
	}
	if _, err := pool.Exec(ctx,
		`DELETE FROM gpu_mig_recommendation_sets WHERE last_reported < $1`,
		olderThan,
	); err != nil {
		return err
	}
	return engine.PruneNodeGPUTimeslicingRecommendationHistory(ctx, pool)
}

func (p *GPUPlugin) DefaultTerms() []plugin.TermConfig {
	return []plugin.TermConfig{
		{Name: "short", WindowDays: 1, MinDataDays: 1, DecayHalfLifeHours: 0},
		{Name: "medium", WindowDays: 7, MinDataDays: 3, DecayHalfLifeHours: 168},
		{Name: "long", WindowDays: 15, MinDataDays: 7, DecayHalfLifeHours: 360},
	}
}

func (p *GPUPlugin) MaxWindowDays() int { return 90 }
