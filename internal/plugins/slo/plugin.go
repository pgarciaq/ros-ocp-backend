// Package slo implements the SLO rollup store plugin (#644, implements #624).
//
// Two normalized tables, HC x window keyed: hosted_api_bucket_rollups
// (cumulative histogram buckets per verb_group/le) + hosted_worker_pressure
// (backend-derived from node digests, codes 12/74 + freshness gate — no
// operator CSV per the canonical contract). Retention governed by
// ROS_HISTORY_RETENTION_DAYS (90d), swept with history.
//
// # Traits Implemented
//
//   - [plugin.CSVIngestor] — claims "slo" CSV type (ros-openshift-slo-)
//   - [plugin.RetentionProvider] — sweeps both tables
//
// No APIProvider (no routes), no TermProvider (no terms). The deferred
// recommendation switch needs no case: SLO rows unblock correlator inputs
// (#625) without running a recommendation engine.
package slo

import (
	"context"
	"io"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
	"github.com/redhatinsights/ros-ocp-backend/internal/types"
)

// SLOPlugin handles SLO bucket rollup CSV ingestion and retention.
type SLOPlugin struct {
	plugin.BasePlugin
}

func init() {
	plugin.Register(&SLOPlugin{})
}

func (p *SLOPlugin) Name() string { return "slo" }

func (p *SLOPlugin) Enabled() bool { return plugin.EnabledFor(p.Name()) }

func (p *SLOPlugin) Priority() int { return 50 }

func (p *SLOPlugin) SupportedCSVTypes() []string {
	return []string{string(types.PayloadTypeSLO)}
}

func (p *SLOPlugin) IngestCSV(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) ([]ingestion.MetricRow, error) {
	if err := ingestion.ProcessSLOCSV(ctx, pool, r, orgID, clusterUUID); err != nil {
		return nil, err
	}
	return nil, nil
}

func (p *SLOPlugin) RetentionTables() []string {
	return []string{"hosted_api_bucket_rollups", "hosted_worker_pressure"}
}

func (p *SLOPlugin) SweepRetention(ctx context.Context, pool *pgxpool.Pool, olderThan time.Time) error {
	return engine.SweepPartitionedTables(ctx, pool, p.RetentionTables(), olderThan.Format("200601"))
}
