package node

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

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
