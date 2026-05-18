package pvc

import (
	"context"
	"io"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
	"github.com/redhatinsights/ros-ocp-backend/internal/types"
)

// PVCPlugin handles PVC (persistent volume) recommendation CSV ingestion.
type PVCPlugin struct{}

func init() {
	plugin.Register(&PVCPlugin{})
}

func (p *PVCPlugin) Name() string { return "pvc" }

func (p *PVCPlugin) Enabled() bool { return plugin.EnabledFor(p.Name()) }

func (p *PVCPlugin) SupportedCSVTypes() []string {
	return []string{string(types.PayloadTypeStorage)}
}

func (p *PVCPlugin) IngestCSV(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) ([]ingestion.MetricRow, error) {
	if err := ingestion.ProcessStorageCSV(ctx, pool, r, orgID, clusterUUID); err != nil {
		return nil, err
	}
	return nil, nil
}
