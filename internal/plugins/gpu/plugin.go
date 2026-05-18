package gpu

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

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
