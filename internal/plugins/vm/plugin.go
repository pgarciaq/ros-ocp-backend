// Package vm implements OpenShift Virtualization VM recommendation ingestion.
//
// The plugin ingests ros-openshift-vm-usage CSV reports (15-minute samples),
// aggregates them into daily_vm_digests. VM recommendations run after manifest
// ingest completes (see services.runManifestRecommendations), not inline here.
//
// # Traits Implemented
//
//   - [plugin.CSVIngestor] — parses "vm" CSV type
//   - [plugin.RetentionProvider] — sweeps daily_vm_digests and vm_recommendations
//   - [plugin.TermProvider] — short/medium/long term windows for VM sizing
package vm

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	rosapi "github.com/redhatinsights/ros-ocp-backend/internal/api"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
	"github.com/redhatinsights/ros-ocp-backend/internal/types"
)

// VMPlugin handles VM usage digest ingestion for virtualization right-sizing.
type VMPlugin struct {
	plugin.BasePlugin
}

func init() {
	plugin.Register(&VMPlugin{})
}

func (p *VMPlugin) Name() string { return "vm" }

func (p *VMPlugin) Enabled() bool {
	return plugin.EnabledFor(p.Name())
}

func (p *VMPlugin) Priority() int { return 40 }

func (p *VMPlugin) SupportedCSVTypes() []string {
	return []string{
		string(types.PayloadTypeVM),
		string(types.PayloadTypeVMGPU),
	}
}

func (p *VMPlugin) IngestCSV(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) ([]ingestion.MetricRow, error) {
	buf := bufio.NewReader(r)
	header, err := buf.ReadString('\n')
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("reading VM CSV header: %w", err)
	}
	body := io.MultiReader(strings.NewReader(header), buf)
	if isVMGPUDeviceCSVHeader(header) {
		if err := ingestion.IngestVMGPUDeviceCSV(ctx, pool, body, orgID, clusterUUID); err != nil {
			return nil, fmt.Errorf("ingesting VM GPU device CSV: %w", err)
		}
		logging.ForOrg(orgID, clusterUUID).Info("VMPlugin.IngestCSV: upserted VM GPU device rows")
		return nil, nil
	}

	rows, err := ingestion.ParseVMCSVRows(body)
	if err != nil {
		return nil, fmt.Errorf("parsing VM CSV: %w", err)
	}
	if len(rows) == 0 {
		logging.ForOrg(orgID, clusterUUID).Info("VMPlugin.IngestCSV: no VM rows found")
		return nil, nil
	}

	digestMap := ingestion.BuildDailyVMDigests(rows)
	digests := make([]ingestion.VMDigestResult, 0, len(digestMap))
	for _, d := range digestMap {
		digests = append(digests, d)
	}

	if err := upsertDigestResults(ctx, pool, orgID, clusterUUID, digests); err != nil {
		return nil, fmt.Errorf("upserting VM digests: %w", err)
	}

	logging.ForOrg(orgID, clusterUUID).Infof("VMPlugin.IngestCSV: upserted %d VM digests", len(digests))
	return nil, nil
}

func isVMGPUDeviceCSVHeader(header string) bool {
	lower := strings.ToLower(header)
	return strings.Contains(lower, "gpu_uuid") && !strings.Contains(lower, "cpu_usage_mc")
}

func (p *VMPlugin) RegisterRoutes(g *echo.Group) {
	if plugin.EnabledFor(plugin.KruizePluginName) || !plugin.EnabledFor(p.Name()) {
		return
	}
	g.GET("/recommendations/openshift/vm", rosapi.GetVMRecommendations)
	g.GET("/recommendations/openshift/vm/detail", rosapi.GetVMRecommendationDetail)
	g.GET("/recommendations/openshift/vms/:vm_name/history", rosapi.GetVMRecommendationHistory)
	g.GET("/recommendations/openshift/instance-types", rosapi.GetClusterInstanceTypes)
}

func (p *VMPlugin) DefaultTerms() []plugin.TermConfig {
	return []plugin.TermConfig{
		{Name: "short_term", WindowDays: 7, MinDataDays: 3, DecayHalfLifeHours: 0},
		{Name: "medium_term", WindowDays: 15, MinDataDays: 7, DecayHalfLifeHours: 0},
		{Name: "long_term", WindowDays: 30, MinDataDays: 15, DecayHalfLifeHours: 0},
	}
}

func (p *VMPlugin) MaxWindowDays() int { return 90 }

func (p *VMPlugin) RetentionTables() []string {
	return []string{"daily_vm_digests", "vm_recommendations", "vm_recommendation_history"}
}

func (p *VMPlugin) SweepRetention(ctx context.Context, pool *pgxpool.Pool, olderThan time.Time) error {
	cutoff := olderThan.Format("2006-01-02")
	_, err := pool.Exec(ctx, `DELETE FROM daily_vm_digests WHERE bucket_date < $1::date`, cutoff)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `DELETE FROM vm_recommendations WHERE last_recommended_at < $1`, olderThan)
	if err != nil {
		return err
	}
	return engine.PruneVMRecommendationHistory(ctx, pool)
}
