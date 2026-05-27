// Package container implements the container CPU/memory recommendation plugin.
//
// This is the primary recommendation domain. It ingests "container" CSV reports
// (pod-level CPU and memory metrics from the koku-metrics-operator), computes
// per-container daily digests, and produces short/medium/long term recommendations
// using percentile-based sizing with optional exponential decay weighting.
//
// # Ingestion
//
// CSV rows contain per-container metrics: CPU request/limit/usage, memory
// request/limit/usage, reported hourly. The ingestor aggregates these into
// daily_container_digests (partition-managed, month-granularity).
//
// # Recommendations
//
// The native engine ([engine.RecommendWorkloadsStreaming]) processes digests
// within each term's window and produces recommendations using configurable
// percentile thresholds. Decay half-life causes recent data to have more
// influence on the final recommendation.
//
// # Default Terms
//
//   - short: 1-day window, 1 day minimum data, no decay
//   - medium: 7-day window, 3 days minimum, 168h (7d) decay half-life
//   - long: 15-day window, 7 days minimum, 360h (15d) decay half-life
//
// MaxWindowDays is 90 because container metrics are high-frequency and
// recommendations beyond ~3 months rarely add signal (workload behavior
// typically shifts at that timescale due to releases and config changes).
//
// # Traits Implemented
//
//   - [plugin.CSVIngestor] — parses "container" CSV type
//   - [plugin.RetentionProvider] — sweeps daily_container_digests, container_usage_samples
//   - [plugin.TermProvider] — configurable short/medium/long terms (max 90 days)
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

// ContainerPlugin handles container CPU/memory recommendation CSV ingestion,
// retention, and term configuration.
type ContainerPlugin struct {
	plugin.BasePlugin
}

func init() {
	plugin.Register(&ContainerPlugin{})
}

func (p *ContainerPlugin) Name() string { return "container" }

func (p *ContainerPlugin) Enabled() bool { return plugin.EnabledFor(p.Name()) }

func (p *ContainerPlugin) SupportedCSVTypes() []string {
	return []string{"container"}
}

func (p *ContainerPlugin) IngestCSV(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) ([]ingestion.MetricRow, error) {
	return ingestion.ParseAndDigestCSV(ctx, pool, r, orgID, clusterUUID, ingestion.ParseDigestOptions{
		EnableGPU:  plugin.EnabledFor("gpu"),
		EnableNode: plugin.EnabledFor("node"),
	})
}

func (p *ContainerPlugin) RetentionTables() []string {
	return []string{"daily_container_digests", "container_usage_samples"}
}

func (p *ContainerPlugin) SweepRetention(ctx context.Context, pool *pgxpool.Pool, olderThan time.Time) error {
	return engine.SweepPartitionedTables(ctx, pool, p.RetentionTables(), olderThan.Format("200601"))
}

func (p *ContainerPlugin) DefaultTerms() []plugin.TermConfig {
	return []plugin.TermConfig{
		{Name: "short", WindowDays: 1, MinDataDays: 1, DecayHalfLifeHours: 0},
		{Name: "medium", WindowDays: 7, MinDataDays: 3, DecayHalfLifeHours: 168},
		{Name: "long", WindowDays: 15, MinDataDays: 7, DecayHalfLifeHours: 360},
	}
}

func (p *ContainerPlugin) MaxWindowDays() int { return 90 }
