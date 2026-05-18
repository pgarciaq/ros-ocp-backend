package snapshot

import (
	"context"
	"io"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	rosapi "github.com/redhatinsights/ros-ocp-backend/internal/api"
	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
	"github.com/redhatinsights/ros-ocp-backend/internal/types"
)

// SnapshotPlugin handles snapshot/staleness CSV ingestion.
type SnapshotPlugin struct{}

func init() {
	plugin.Register(&SnapshotPlugin{})
}

func (p *SnapshotPlugin) Name() string { return "snapshot" }

func (p *SnapshotPlugin) Enabled() bool { return plugin.EnabledFor(p.Name()) }

func (p *SnapshotPlugin) SupportedCSVTypes() []string {
	return []string{string(types.PayloadTypeSnapshot)}
}

func (p *SnapshotPlugin) IngestCSV(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) ([]ingestion.MetricRow, error) {
	if err := ingestion.ProcessSnapshotCSV(ctx, pool, r, orgID, clusterUUID); err != nil {
		return nil, err
	}
	return nil, nil
}

func (p *SnapshotPlugin) RegisterRoutes(g *echo.Group) {
	if plugin.EnabledFor(plugin.KruizePluginName) {
		return
	}
	g.GET("/recommendations/openshift/snapshots", rosapi.GetSnapshotRecommendations)
	g.GET("/recommendations/openshift/settings/snapshot", rosapi.GetSnapshotSettings)
	g.PUT("/recommendations/openshift/settings/snapshot", rosapi.PutSnapshotSettings)
}
