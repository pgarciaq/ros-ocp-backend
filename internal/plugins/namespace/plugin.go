package namespace

import (
	"context"
	"io"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
)

type NamespacePlugin struct{}

func init() {
	plugin.Register(&NamespacePlugin{})
}

func (p *NamespacePlugin) Name() string { return "namespace" }

func (p *NamespacePlugin) Enabled() bool { return plugin.EnabledFor(p.Name()) }

func (p *NamespacePlugin) SupportedCSVTypes() []string {
	return []string{"namespace"}
}

func (p *NamespacePlugin) IngestCSV(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) ([]ingestion.MetricRow, error) {
	if err := ingestion.ProcessNamespaceCSVToDigests(ctx, pool, r, orgID, clusterUUID); err != nil {
		return nil, err
	}
	return nil, nil
}
