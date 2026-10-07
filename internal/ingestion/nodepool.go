package ingestion

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
	libcsv "github.com/redhatinsights/ros-ocp-backend/librobne/csv"
)

// NodepoolRow is a parsed NodePool snapshot (librobne/csv.NodepoolRow).
type NodepoolRow = libcsv.NodepoolRow

// forEachNodepoolCSVRow parses NodePool CSV rows one at a time without
// retaining a full-slice copy. Processor ingest uses this;
// ParseNodepoolRows collects from it for tests.
func forEachNodepoolCSVRow(ctx context.Context, r io.Reader, fn func(NodepoolRow) error) (int, error) {
	count := 0
	skipped, err := libcsv.ForEachNodepool(ctx, r, func(row libcsv.NodepoolRow) error {
		if err := fn(row); err != nil {
			return err
		}
		count++
		return nil
	})
	if skipped > 0 {
		metrics.IncCSVRowsSkipped("nodepool", skipped)
		logging.GetLogger().Warnf("ParseNodepoolRows: skipped %d malformed or invalid rows", skipped)
	}
	return count, err
}

// ParseNodepoolRows parses the NodePool CSV into NodepoolRow structs. For
// tests and callers that want a slice; processor ingest uses
// forEachNodepoolCSVRow.
func ParseNodepoolRows(r io.Reader) ([]NodepoolRow, error) {
	var rows []NodepoolRow
	_, err := forEachNodepoolCSVRow(context.Background(), r, func(row NodepoolRow) error {
		rows = append(rows, row)
		return nil
	})
	return rows, err
}

func upsertNodepoolRow(ctx context.Context, pool *pgxpool.Pool, r NodepoolRow, orgID, clusterUUID string) error {
	// Hourly windows keyed verbatim; per-cycle re-emits upsert
	// idempotently on the full PK. Counts stored verbatim — scaled-to-0
	// (spec AND status zero) is interpreted at read (Child B), never
	// pre-filtered here.
	_, err := pool.Exec(ctx, `
		INSERT INTO hosted_nodepool_rollups (
			window_start, window_end, org_id, cluster_uuid, hc_cluster_id,
			pool_name, spec_replicas, status_replicas, autoscaling,
			collected_at, source
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (org_id, cluster_uuid, hc_cluster_id, pool_name, window_start, window_end, source)
		DO UPDATE SET spec_replicas = EXCLUDED.spec_replicas,
			status_replicas = EXCLUDED.status_replicas,
			autoscaling = EXCLUDED.autoscaling,
			collected_at = EXCLUDED.collected_at`,
		r.WindowStart.UTC(), r.WindowEnd.UTC(), orgID, clusterUUID, r.HCClusterID,
		r.PoolName, r.SpecReplicas, r.StatusReplicas, r.Autoscaling,
		r.CollectedAt.UTC(), r.Source,
	)
	if err != nil {
		return fmt.Errorf("upserting nodepool %s/%s: %w", r.HCClusterID, r.PoolName, err)
	}
	return nil
}

// EnsureNodepoolPartitionsForMonth pre-creates the monthly partition for
// the NodePool table. Called per ingest window; idempotent.
func EnsureNodepoolPartitionsForMonth(ctx context.Context, pool *pgxpool.Pool, monthStart time.Time) error {
	ms := time.Date(monthStart.Year(), monthStart.Month(), 1, 0, 0, 0, 0, time.UTC)
	me := ms.AddDate(0, 1, 0)
	part := "hosted_nodepool_rollups_" + ms.Format("200601")
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_class WHERE relname = $1)`, part).Scan(&exists); err != nil {
		return fmt.Errorf("checking partition %s: %w", part, err)
	}
	if exists {
		return nil
	}
	q := `CREATE TABLE IF NOT EXISTS "` + part + `" PARTITION OF "hosted_nodepool_rollups` +
		`" FOR VALUES FROM ('` + ms.Format("2006-01-02") + `') TO ('` + me.Format("2006-01-02") + `')`
	if _, err := pool.Exec(ctx, q); err != nil {
		return fmt.Errorf("creating partition %s: %w", part, err)
	}
	return nil
}

// ProcessNodepoolCSV is the top-level entry point for NodePool CSV
// ingestion (#673, Child A of #660). Same load-bearing contract as
// SLO/apitax: never permanently fails the manifest on bad data.
func ProcessNodepoolCSV(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) error {
	rows, err := ParseNodepoolRows(r)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		logging.GetLogger().WithField("cluster_uuid", clusterUUID).Info("ProcessNodepoolCSV: no NodePool rows found")
		return nil
	}
	// Pre-create partitions for all observed months before upserting;
	// ingest never fails on partition DDL (same contract as SLO).
	months := map[string]time.Time{}
	for _, row := range rows {
		ms := time.Date(row.WindowStart.Year(), row.WindowStart.Month(), 1, 0, 0, 0, 0, time.UTC)
		months[ms.Format("200601")] = ms
	}
	for _, ms := range months {
		if err := EnsureNodepoolPartitionsForMonth(ctx, pool, ms); err != nil {
			logging.GetLogger().Warnf("ProcessNodepoolCSV: partition pre-creation %s: %v", ms.Format("200601"), err)
		}
	}
	for _, row := range rows {
		if err := upsertNodepoolRow(ctx, pool, row, orgID, clusterUUID); err != nil {
			return err
		}
	}
	logging.GetLogger().WithField("cluster_uuid", clusterUUID).Infof("ProcessNodepoolCSV: upserted %d NodePool rows", len(rows))
	return nil
}
