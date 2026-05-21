// Package pvc implements the persistent volume claim (PVC) recommendation plugin.
//
// This plugin analyzes PVC capacity usage over time to recommend right-sized
// volume requests. Unlike CPU/memory which fluctuates rapidly, storage usage
// tends to grow monotonically, so the PVC engine uses exponential-weighted
// least squares (WLS) to project future growth and recommend capacity headroom.
//
// # Ingestion
//
// PVC data arrives in "storage" CSV reports from the koku-metrics-operator,
// containing per-PVC capacity, request, and usage bytes. These are processed
// into daily_pvc_digests.
//
// # Recommendations
//
// The PVC engine ([engine.RecommendPVCs]) fits a weighted linear trend to
// usage data within each term's window using exponential decay weighting
// (recent points matter more). It projects forward to estimate when the PVC
// will reach capacity and recommends an appropriate new size.
//
// # Default Terms
//
//   - short: 7-day window, 3 days minimum, no decay (recent snapshot)
//   - medium: 30-day window, 14 days minimum, no decay (monthly trend)
//   - long: 90-day window, 30 days minimum, no decay (quarterly projection)
//
// MaxWindowDays is 365 because storage growth patterns are slow-moving and
// seasonal (e.g., log accumulation, database growth). A full year of data
// can reveal quarterly or annual cycles invisible in shorter windows.
// This is significantly higher than the 90-day max for CPU/memory plugins.
//
// Note: PVC terms default to zero decay half-life because the WLS engine
// already applies exponential weighting internally during slope computation.
// Setting DecayHalfLifeHours > 0 would double-weight recent data.
//
// # Traits Implemented
//
//   - [plugin.CSVIngestor] — parses "storage" CSV type
//   - [plugin.APIProvider] — PVC recommendation endpoint
//   - [plugin.RetentionProvider] — sweeps daily_pvc_digests
//   - [plugin.TermProvider] — configurable short/medium/long terms (max 365 days)
package pvc

import (
	"context"
	"io"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	rosapi "github.com/redhatinsights/ros-ocp-backend/internal/api"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
	"github.com/redhatinsights/ros-ocp-backend/internal/types"
)

// PVCPlugin handles PVC storage growth analysis and capacity right-sizing
// recommendations using weighted least squares trend projection.
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

func (p *PVCPlugin) RegisterRoutes(g *echo.Group) {
	if plugin.EnabledFor(plugin.KruizePluginName) {
		return
	}
	g.GET("/recommendations/openshift/pvcs", rosapi.GetPVCRecommendations)
}

func (p *PVCPlugin) RetentionTables() []string {
	return []string{"daily_pvc_digests"}
}

func (p *PVCPlugin) SweepRetention(ctx context.Context, pool *pgxpool.Pool, olderThan time.Time) error {
	return engine.SweepPartitionedTables(ctx, pool, p.RetentionTables(), olderThan.Format("200601"))
}

func (p *PVCPlugin) DefaultTerms() []plugin.TermConfig {
	return []plugin.TermConfig{
		{Name: "short", WindowDays: 7, MinDataDays: 3, DecayHalfLifeHours: 0},
		{Name: "medium", WindowDays: 30, MinDataDays: 14, DecayHalfLifeHours: 0},
		{Name: "long", WindowDays: 90, MinDataDays: 30, DecayHalfLifeHours: 0},
	}
}

func (p *PVCPlugin) MaxWindowDays() int { return 365 }
