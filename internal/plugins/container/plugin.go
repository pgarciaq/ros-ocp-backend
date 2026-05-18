package container

import (
	"context"
	"io"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
)

type ContainerPlugin struct{}

func init() {
	plugin.Register(&ContainerPlugin{})
}

func (p *ContainerPlugin) Name() string { return "container" }

func (p *ContainerPlugin) Enabled() bool { return plugin.EnabledFor(p.Name()) }

func (p *ContainerPlugin) SupportedCSVTypes() []string {
	return []string{"container"}
}

func (p *ContainerPlugin) IngestCSV(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) ([]ingestion.MetricRow, error) {
	return ingestion.ParseAndDigestCSV(ctx, pool, r, orgID, clusterUUID)
}

func (p *ContainerPlugin) RetentionTables() []string {
	return []string{"daily_container_digests", "container_usage_samples"}
}

func (p *ContainerPlugin) SweepRetention(ctx context.Context, pool *pgxpool.Pool, olderThan time.Time) error {
	return engine.SweepPartitionedTables(ctx, pool, p.RetentionTables(), olderThan.Format("200601"))
}
