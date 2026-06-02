// Package snapshot implements the VolumeSnapshot staleness detection plugin.
//
// This plugin identifies VolumeSnapshots that have become stale (not refreshed
// within a configurable threshold) and surfaces them as optimization opportunities.
// Stale snapshots consume storage capacity without providing useful point-in-time
// recovery.
//
// # Ingestion
//
// Snapshot data arrives in "snapshot" CSV reports from the koku-metrics-operator,
// containing VolumeSnapshot metadata (name, namespace, creation time, size, source PVC).
//
// # Recommendations
//
// The snapshot engine uses a simple age threshold (configurable per-tenant via the
// settings endpoint) to classify snapshots as stale. Unlike other plugins, snapshot
// recommendations are binary (stale/not-stale) rather than quantitative, so
// configurable terms are not applicable here.
//
// # Traits Implemented
//
//   - [plugin.CSVIngestor] — parses "snapshot" CSV type
//   - [plugin.APIProvider] — snapshot staleness list + per-tenant settings endpoints
//
// Note: This plugin does NOT implement [plugin.TermProvider] because snapshot
// staleness is threshold-based (days since last refresh), not window-based.
// Staleness settings are managed through a separate settings endpoint.
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

// SnapshotPlugin handles VolumeSnapshot staleness detection and reporting.
type SnapshotPlugin struct {
	plugin.BasePlugin
}

func init() {
	plugin.Register(&SnapshotPlugin{})
}

func (p *SnapshotPlugin) Name() string { return "snapshot" }

func (p *SnapshotPlugin) Enabled() bool { return plugin.EnabledFor(p.Name()) }

func (p *SnapshotPlugin) Priority() int { return 40 }

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
	g.GET("/recommendations/openshift/snapshots/summary", rosapi.GetSnapshotSummary)
	g.GET("/recommendations/openshift/snapshots", rosapi.GetSnapshotRecommendations)
	g.GET("/recommendations/openshift/settings/snapshot", rosapi.GetSnapshotSettings)
	g.PUT("/recommendations/openshift/settings/snapshot", rosapi.PutSnapshotSettings)
	g.DELETE("/recommendations/openshift/settings/snapshot", rosapi.DeleteSnapshotSettings)
}
