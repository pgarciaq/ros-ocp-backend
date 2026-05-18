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

type GPUPlugin struct{}

func init() {
	plugin.Register(&GPUPlugin{})
}

func (p *GPUPlugin) Name() string { return "gpu" }

func (p *GPUPlugin) Enabled() bool { return plugin.EnabledFor(p.Name()) }

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
	g.GET("/recommendations/openshift/gpu/mig", rosapi.GetGPUMIGRecommendations)
}

func (p *GPUPlugin) RetentionTables() []string {
	return []string{"gpu_container_digests"}
}

func (p *GPUPlugin) SweepRetention(ctx context.Context, pool *pgxpool.Pool, olderThan time.Time) error {
	return engine.SweepPartitionedTables(ctx, pool, p.RetentionTables(), olderThan.Format("200601"))
}
