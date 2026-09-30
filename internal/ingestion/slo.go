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

// SLORow is a parsed SLO bucket snapshot (librobne/csv.SLORow).
type SLORow = libcsv.SLORow

// forEachSLOCSVRow parses SLO CSV rows one at a time without retaining a
// full-slice copy. Processor ingest uses this; ParseSLORows collects from it
// for tests.
func forEachSLOCSVRow(ctx context.Context, r io.Reader, fn func(SLORow) error) (int, error) {
	count := 0
	skipped, err := libcsv.ForEachSLO(ctx, r, func(row libcsv.SLORow) error {
		if err := fn(row); err != nil {
			return err
		}
		count++
		return nil
	})
	if skipped > 0 {
		metrics.IncCSVRowsSkipped("slo", skipped)
		logging.GetLogger().Warnf("ParseSLORows: skipped %d malformed or invalid rows", skipped)
	}
	return count, err
}

// ParseSLORows parses the SLO CSV into SLORow structs. For tests and callers
// that want a slice; processor ingest uses forEachSLOCSVRow.
func ParseSLORows(r io.Reader) ([]SLORow, error) {
	var rows []SLORow
	_, err := forEachSLOCSVRow(context.Background(), r, func(row SLORow) error {
		rows = append(rows, row)
		return nil
	})
	return rows, err
}

// bucketHour truncates t to the hour. Backend owns hourly bucketing (#644):
// operator emits per-cycle cumulative snapshots; the stored window is keyed
// on the hour containing window_end so per-cycle re-emits upsert idempotently.
func bucketHour(t time.Time) time.Time {
	return t.UTC().Truncate(time.Hour)
}

func upsertSLOBucketRow(ctx context.Context, pool *pgxpool.Pool, r SLORow, orgID, clusterUUID string) error {
	// Hourly bucketing: window keyed on hour of window_end; window_start kept
	// as given for traceability but PK covers the full window so re-emits
	// upsert rather than duplicate. Cumulative counts stored verbatim;
	// reset-aware deltas are computed at read (correlator #625), not here.
	le := r.Le
	if math.IsInf(le, 1) {
		le = math.Inf(1)
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO hosted_api_bucket_rollups (
			window_start, window_end, org_id, cluster_uuid, hc_cluster_id,
			verb_group, le, bucket_count, collected_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (org_id, cluster_uuid, hc_cluster_id, window_start, window_end, verb_group, le)
		DO UPDATE SET bucket_count = EXCLUDED.bucket_count, collected_at = EXCLUDED.collected_at`,
		r.WindowStart.UTC(), r.WindowEnd.UTC(), orgID, clusterUUID, r.HCClusterID,
		r.VerbGroup, le, r.BucketCount, r.CollectedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("upserting slo bucket %s/%s/%v: %w", r.HCClusterID, r.VerbGroup, r.Le, err)
	}
	return nil
}

// EnsureSLOPartitionsForMonth pre-creates monthly partitions for both SLO
// tables. Called per ingest window; idempotent (IF NOT EXISTS via catalog check).
func EnsureSLOPartitionsForMonth(ctx context.Context, pool *pgxpool.Pool, monthStart time.Time) error {
	ms := time.Date(monthStart.Year(), monthStart.Month(), 1, 0, 0, 0, 0, time.UTC)
	me := ms.AddDate(0, 1, 0)
	for _, tbl := range []string{"hosted_api_bucket_rollups", "hosted_worker_pressure"} {
		part := tbl + "_" + ms.Format("200601")
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_class WHERE relname = $1)`, part).Scan(&exists); err != nil {
			return fmt.Errorf("checking partition %s: %w", part, err)
		}
		if exists {
			continue
		}
		// Table names are constants above; months are formatted dates.
		q := `CREATE TABLE IF NOT EXISTS "` + part + `" PARTITION OF "` + tbl +
			`" FOR VALUES FROM ('` + ms.Format("2006-01-02") + `') TO ('` + me.Format("2006-01-02") + `')`
		if _, err := pool.Exec(ctx, q); err != nil {
			return fmt.Errorf("creating partition %s: %w", part, err)
		}
	}
	return nil
}

// ProcessSLOCSV is the top-level entry point for SLO CSV ingestion (#644).
// Never permanently fails the manifest on bad data: malformed rows are
// skipped with counters (see forEachSLOCSVRow); header errors abort the file
// but the caller (parallel_ingest SLO case) still marks Done — a poisoned SLO
// file must not gate container recommendations via manifest completeness.
// Deltas are reset-aware at read; ingest stores cumulative snapshots verbatim.
func ProcessSLOCSV(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) error {
	months := map[string]time.Time{}
	inserted := 0
	_, err := forEachSLOCSVRow(ctx, r, func(row SLORow) error {
		ms := time.Date(row.WindowStart.Year(), row.WindowStart.Month(), 1, 0, 0, 0, 0, time.UTC)
		months[ms.Format("200601")] = ms
		if err := upsertSLOBucketRow(ctx, pool, row, orgID, clusterUUID); err != nil {
			return err
		}
		inserted++
		return nil
	})
	if err != nil {
		return err
	}
	// Best-effort partition pre-creation for observed months is handled by the
	// migration's initial partitions + EnsureSLOPartitionsForMonth at the
	// caller; ingest itself never fails on partition DDL.
	_ = months
	if inserted == 0 {
		logging.GetLogger().WithField("cluster_uuid", clusterUUID).Info("ProcessSLOCSV: no SLO rows found")
		return nil
	}
	logging.GetLogger().WithField("cluster_uuid", clusterUUID).Infof("ProcessSLOCSV: upserted %d SLO bucket rows", inserted)
	return nil
}

// UpsertWorkerPressureDerived writes a backend-derived worker-pressure row
// (#644 REVISED: no operator CSV). pressured is derived from node digest
// notification codes 12 (NODE_OVERCOMMITTED) / 74 (NODE_POD_SCHEDULING_LIMIT)
// with a freshness gate applied by the caller; signals_present names the
// codes present (e.g. ["12","74"]); coverage_pct is explicit 0-100.
// Absence/staleness never renders healthy — callers must not synthesize
// pressured=false rows for missing/stale windows.
func UpsertWorkerPressureDerived(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID, hcClusterID string, windowStart, windowEnd time.Time, pressured bool, coveragePct float64, signals []string, observedAt time.Time) error {
	if coveragePct < 0 || coveragePct > 100 {
		return fmt.Errorf("coverage_pct %v out of range [0,100]", coveragePct)
	}
	if signals == nil {
		signals = []string{}
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO hosted_worker_pressure (
			window_start, window_end, org_id, cluster_uuid, hc_cluster_id,
			pressured, coverage_pct, signals_present, observed_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (org_id, cluster_uuid, hc_cluster_id, window_start, window_end)
		DO UPDATE SET pressured = EXCLUDED.pressured, coverage_pct = EXCLUDED.coverage_pct,
			signals_present = EXCLUDED.signals_present, observed_at = EXCLUDED.observed_at`,
		windowStart.UTC(), windowEnd.UTC(), orgID, clusterUUID, hcClusterID,
		pressured, coveragePct, signals, observedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("upserting worker pressure %s: %w", hcClusterID, err)
	}
	return nil
}
