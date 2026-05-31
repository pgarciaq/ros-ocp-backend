// Package vm implements OpenShift Virtualization VM recommendation ingestion.
//
// The plugin ingests ros-openshift-vm-usage CSV reports (15-minute samples),
// aggregates them into daily_vm_digests, and will later drive recommendVM() and
// HTTP APIs behind ROS_ENABLE_VM_RECS.
//
// # Traits Implemented
//
//   - [plugin.CSVIngestor] — parses "vm" CSV type
//   - [plugin.RetentionProvider] — sweeps daily_vm_digests and vm_recommendations
package vm

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
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
	return config.GetConfig().EnableVMRecs && plugin.EnabledFor(p.Name())
}

func (p *VMPlugin) Priority() int { return 40 }

func (p *VMPlugin) SupportedCSVTypes() []string {
	return []string{string(types.PayloadTypeVM)}
}

func (p *VMPlugin) IngestCSV(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) ([]ingestion.MetricRow, error) {
	rows, err := ingestion.ParseVMCSVRows(r)
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

func (p *VMPlugin) RetentionTables() []string {
	return []string{"daily_vm_digests", "vm_recommendations"}
}

func (p *VMPlugin) SweepRetention(ctx context.Context, pool *pgxpool.Pool, olderThan time.Time) error {
	cutoff := olderThan.Format("2006-01-02")
	_, err := pool.Exec(ctx, `DELETE FROM daily_vm_digests WHERE bucket_date < $1::date`, cutoff)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `DELETE FROM vm_recommendations WHERE last_recommended_at < $1`, olderThan)
	return err
}
