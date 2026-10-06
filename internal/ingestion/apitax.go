package ingestion

import (
	"context"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
	libcsv "github.com/redhatinsights/ros-ocp-backend/librobne/csv"
)

// APITaxRow is a parsed webhook snapshot (librobne/csv.APITaxRow).
type APITaxRow = libcsv.APITaxRow

// forEachAPITaxCSVRow parses API tax CSV rows one at a time without retaining
// a full-slice copy. Processor ingest uses this; ParseAPITaxRows collects from
// it for tests.
func forEachAPITaxCSVRow(ctx context.Context, r io.Reader, fn func(APITaxRow) error) (int, error) {
	count := 0
	skipped, err := libcsv.ForEachAPITax(ctx, r, func(row libcsv.APITaxRow) error {
		if err := fn(row); err != nil {
			return err
		}
		count++
		return nil
	})
	if skipped > 0 {
		metrics.IncCSVRowsSkipped("apitax", skipped)
		logging.GetLogger().Warnf("ParseAPITaxRows: skipped %d malformed or invalid rows", skipped)
	}
	return count, err
}

// ParseAPITaxRows parses the API tax CSV into APITaxRow structs. For tests
// and callers that want a slice; processor ingest uses forEachAPITaxCSVRow.
func ParseAPITaxRows(r io.Reader) ([]APITaxRow, error) {
	var rows []APITaxRow
	_, err := forEachAPITaxCSVRow(context.Background(), r, func(row APITaxRow) error {
		rows = append(rows, row)
		return nil
	})
	return rows, err
}

func upsertAPITaxRow(ctx context.Context, pool *pgxpool.Pool, r APITaxRow, orgID, clusterUUID string) error {
	// Hourly bucketing mirrors SLO: window keyed on hour of window_end so
	// per-cycle re-emits upsert idempotently. Cumulative counts stored
	// verbatim; reset-aware deltas and p99 are computed at read (correlator),
	// not here. Totals/rejected ride NULL when the series was absent.
	le := r.Le
	if math.IsInf(le, 1) {
		le = math.Inf(1)
	}
	var total, rejected interface{}
	if r.HasTotal {
		total = r.TotalCount
	}
	if r.HasRejected {
		rejected = r.RejectedCount
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO hosted_api_tax_rollups (
			window_start, window_end, org_id, cluster_uuid, webhook_name,
			le, bucket_count, total_count, rejected_count, collected_at, source
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (org_id, cluster_uuid, webhook_name, window_start, window_end, le, source)
		DO UPDATE SET bucket_count = EXCLUDED.bucket_count,
			total_count = EXCLUDED.total_count,
			rejected_count = EXCLUDED.rejected_count,
			collected_at = EXCLUDED.collected_at`,
		r.WindowStart.UTC(), r.WindowEnd.UTC(), orgID, clusterUUID, r.WebhookName,
		le, r.BucketCount, total, rejected, r.CollectedAt.UTC(), r.Source,
	)
	if err != nil {
		return fmt.Errorf("upserting api tax %s/%s/%v: %w", r.WebhookName, r.Source, r.Le, err)
	}
	return nil
}

// EnsureAPITaxPartitionsForMonth pre-creates the monthly partition for the
// API tax table. Called per ingest window; idempotent.
func EnsureAPITaxPartitionsForMonth(ctx context.Context, pool *pgxpool.Pool, monthStart time.Time) error {
	ms := time.Date(monthStart.Year(), monthStart.Month(), 1, 0, 0, 0, 0, time.UTC)
	me := ms.AddDate(0, 1, 0)
	part := "hosted_api_tax_rollups_" + ms.Format("200601")
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_class WHERE relname = $1)`, part).Scan(&exists); err != nil {
		return fmt.Errorf("checking partition %s: %w", part, err)
	}
	if exists {
		return nil
	}
	q := `CREATE TABLE IF NOT EXISTS "` + part + `" PARTITION OF "hosted_api_tax_rollups` +
		`" FOR VALUES FROM ('` + ms.Format("2006-01-02") + `') TO ('` + me.Format("2006-01-02") + `')`
	if _, err := pool.Exec(ctx, q); err != nil {
		return fmt.Errorf("creating partition %s: %w", part, err)
	}
	return nil
}

// ProcessAPITaxCSV is the top-level entry point for API tax CSV ingestion
// (thin W5, #393). Same load-bearing contract as SLO: never permanently
// fails the manifest on bad data — malformed rows skip with counters, header
// errors abort the file but the caller still marks Done, so a poisoned file
// cannot gate container recommendations via manifest completeness.
func ProcessAPITaxCSV(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) error {
	rows, err := ParseAPITaxRows(r)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		logging.GetLogger().WithField("cluster_uuid", clusterUUID).Info("ProcessAPITaxCSV: no API tax rows found")
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
		if err := EnsureAPITaxPartitionsForMonth(ctx, pool, ms); err != nil {
			logging.GetLogger().Warnf("ProcessAPITaxCSV: partition pre-creation %s: %v", ms.Format("200601"), err)
		}
	}
	for _, row := range rows {
		if err := upsertAPITaxRow(ctx, pool, row, orgID, clusterUUID); err != nil {
			return err
		}
	}
	logging.GetLogger().WithField("cluster_uuid", clusterUUID).Infof("ProcessAPITaxCSV: upserted %d API tax rows", len(rows))
	return nil
}
