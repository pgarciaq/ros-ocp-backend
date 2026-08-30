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

// ClusterQuotaMetricRow is a parsed ClusterResourceQuota CSV row (librobne/csv.ClusterQuotaRow).
type ClusterQuotaMetricRow = libcsv.ClusterQuotaRow

// forEachClusterQuotaCSVRow parses cluster-quota CSV rows one at a time without
// retaining a full-slice copy. Processor ingest uses this; ParseClusterQuotaCSVRows
// collects from it for tests.
func forEachClusterQuotaCSVRow(ctx context.Context, r io.Reader, fn func(ClusterQuotaMetricRow) error) (int, error) {
	count := 0
	skipped, err := libcsv.ForEachClusterQuota(ctx, r, func(row libcsv.ClusterQuotaRow) error {
		if err := fn(row); err != nil {
			return err
		}
		count++
		return nil
	})
	if skipped > 0 {
		metrics.IncCSVRowsSkipped("cluster-quota", skipped)
		logging.GetLogger().Warnf("ParseClusterQuotaCSVRows: skipped %d malformed or invalid rows", skipped)
	}
	return count, err
}

// ParseClusterQuotaCSVRows parses cluster ResourceQuota CSV rows.
// Processor ingest uses forEachClusterQuotaCSVRow; this collector is for tests
// and callers that still want a slice. Empty names are dropped. Bad
// timestamps/numbers are skipped.
func ParseClusterQuotaCSVRows(r io.Reader) ([]ClusterQuotaMetricRow, error) {
	var rows []ClusterQuotaMetricRow
	_, err := forEachClusterQuotaCSVRow(context.Background(), r, func(row ClusterQuotaMetricRow) error {
		rows = append(rows, row)
		return nil
	})
	return rows, err
}

type clusterQuotaDigestKey struct {
	orgID            string
	clusterUUID      string
	clusterQuotaName string
	reportDate       time.Time
}

type clusterQuotaDigestAgg struct {
	key                clusterQuotaDigestKey
	cpuRequestHard     int64
	cpuRequestUsed     int64
	cpuLimitHard       int64
	cpuLimitUsed       int64
	memoryRequestHard  int64
	memoryRequestUsed  int64
	memoryLimitHard    int64
	memoryLimitUsed    int64
	storageRequestHard int64
	storageRequestUsed int64
	podsHard           int64
	podsUsed           int64
	objectCountHard    int64
	objectCountUsed    int64
	namespaces         string
}

func addClusterQuotaRowToGroups(out map[clusterQuotaDigestKey]*clusterQuotaDigestAgg, row ClusterQuotaMetricRow, orgID, clusterUUID string) {
	reportDate := time.Date(row.IntervalEnd.Year(), row.IntervalEnd.Month(), row.IntervalEnd.Day(), 0, 0, 0, 0, time.UTC)
	key := clusterQuotaDigestKey{
		orgID:            orgID,
		clusterUUID:      clusterUUID,
		clusterQuotaName: row.ClusterQuotaName,
		reportDate:       reportDate,
	}
	agg, ok := out[key]
	if !ok {
		agg = &clusterQuotaDigestAgg{key: key}
		out[key] = agg
	}
	agg.cpuRequestHard = maxInt64(agg.cpuRequestHard, row.CPURequestHardMC)
	agg.cpuRequestUsed = maxInt64(agg.cpuRequestUsed, row.CPURequestUsedMC)
	agg.cpuLimitHard = maxInt64(agg.cpuLimitHard, row.CPULimitHardMC)
	agg.cpuLimitUsed = maxInt64(agg.cpuLimitUsed, row.CPULimitUsedMC)
	agg.memoryRequestHard = maxInt64(agg.memoryRequestHard, row.MemoryRequestHardBytes)
	agg.memoryRequestUsed = maxInt64(agg.memoryRequestUsed, row.MemoryRequestUsedBytes)
	agg.memoryLimitHard = maxInt64(agg.memoryLimitHard, row.MemoryLimitHardBytes)
	agg.memoryLimitUsed = maxInt64(agg.memoryLimitUsed, row.MemoryLimitUsedBytes)
	agg.storageRequestHard = maxInt64(agg.storageRequestHard, row.StorageRequestHardBytes)
	agg.storageRequestUsed = maxInt64(agg.storageRequestUsed, row.StorageRequestUsedBytes)
	agg.podsHard = maxInt64(agg.podsHard, row.PodsHard)
	agg.podsUsed = maxInt64(agg.podsUsed, row.PodsUsed)
	agg.objectCountHard = maxInt64(agg.objectCountHard, row.ObjectCountHard)
	agg.objectCountUsed = maxInt64(agg.objectCountUsed, row.ObjectCountUsed)
	if row.Namespaces != "" {
		agg.namespaces = row.Namespaces
	}
}

func groupClusterQuotaRows(rows []ClusterQuotaMetricRow, orgID, clusterUUID string) map[clusterQuotaDigestKey]*clusterQuotaDigestAgg {
	out := make(map[clusterQuotaDigestKey]*clusterQuotaDigestAgg)
	for _, row := range rows {
		addClusterQuotaRowToGroups(out, row, orgID, clusterUUID)
	}
	return out
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// ProcessClusterQuotaCSV parses cluster-quota CSV and upserts daily_cluster_quota_digests.
// Rows are grouped in the callback (max hard/used per day). This does not use CLI
// DailyClusterQuotaDigests / LatestClusterQuotaSnapshots — processor upserts every
// day with GREATEST merge against existing digest rows.
func ProcessClusterQuotaCSV(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) error {
	groups := make(map[clusterQuotaDigestKey]*clusterQuotaDigestAgg)
	n := 0
	_, err := forEachClusterQuotaCSVRow(ctx, r, func(row ClusterQuotaMetricRow) error {
		addClusterQuotaRowToGroups(groups, row, orgID, clusterUUID)
		n++
		return nil
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	for _, agg := range groups {
		if err := upsertClusterQuotaDigest(ctx, pool, agg); err != nil {
			return err
		}
	}
	return nil
}

func upsertClusterQuotaDigest(ctx context.Context, pool *pgxpool.Pool, agg *clusterQuotaDigestAgg) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO daily_cluster_quota_digests (
			org_id, cluster_uuid, cluster_quota_name, report_date,
			cpu_request_hard, cpu_request_used,
			cpu_limit_hard, cpu_limit_used,
			memory_request_hard, memory_request_used,
			memory_limit_hard, memory_limit_used,
			storage_request_hard, storage_request_used,
			pods_hard, pods_used,
			object_count_hard, object_count_used,
			namespaces
		) VALUES (
			$1, $2::uuid, $3, $4,
			$5, $6, $7, $8, $9, $10, $11, $12,
			$13, $14, $15, $16, $17, $18, $19
		)
		ON CONFLICT (org_id, cluster_uuid, cluster_quota_name, report_date)
		DO UPDATE SET
			cpu_request_hard = GREATEST(COALESCE(daily_cluster_quota_digests.cpu_request_hard, 0), COALESCE(EXCLUDED.cpu_request_hard, 0)),
			cpu_request_used = GREATEST(COALESCE(daily_cluster_quota_digests.cpu_request_used, 0), COALESCE(EXCLUDED.cpu_request_used, 0)),
			cpu_limit_hard = GREATEST(COALESCE(daily_cluster_quota_digests.cpu_limit_hard, 0), COALESCE(EXCLUDED.cpu_limit_hard, 0)),
			cpu_limit_used = GREATEST(COALESCE(daily_cluster_quota_digests.cpu_limit_used, 0), COALESCE(EXCLUDED.cpu_limit_used, 0)),
			memory_request_hard = GREATEST(COALESCE(daily_cluster_quota_digests.memory_request_hard, 0), COALESCE(EXCLUDED.memory_request_hard, 0)),
			memory_request_used = GREATEST(COALESCE(daily_cluster_quota_digests.memory_request_used, 0), COALESCE(EXCLUDED.memory_request_used, 0)),
			memory_limit_hard = GREATEST(COALESCE(daily_cluster_quota_digests.memory_limit_hard, 0), COALESCE(EXCLUDED.memory_limit_hard, 0)),
			memory_limit_used = GREATEST(COALESCE(daily_cluster_quota_digests.memory_limit_used, 0), COALESCE(EXCLUDED.memory_limit_used, 0)),
			storage_request_hard = GREATEST(COALESCE(daily_cluster_quota_digests.storage_request_hard, 0), COALESCE(EXCLUDED.storage_request_hard, 0)),
			storage_request_used = GREATEST(COALESCE(daily_cluster_quota_digests.storage_request_used, 0), COALESCE(EXCLUDED.storage_request_used, 0)),
			pods_hard = GREATEST(COALESCE(daily_cluster_quota_digests.pods_hard, 0), COALESCE(EXCLUDED.pods_hard, 0)),
			pods_used = GREATEST(COALESCE(daily_cluster_quota_digests.pods_used, 0), COALESCE(EXCLUDED.pods_used, 0)),
			object_count_hard = GREATEST(COALESCE(daily_cluster_quota_digests.object_count_hard, 0), COALESCE(EXCLUDED.object_count_hard, 0)),
			object_count_used = GREATEST(COALESCE(daily_cluster_quota_digests.object_count_used, 0), COALESCE(EXCLUDED.object_count_used, 0)),
			namespaces = COALESCE(NULLIF(EXCLUDED.namespaces, ''), daily_cluster_quota_digests.namespaces)`,
		agg.key.orgID, agg.key.clusterUUID, agg.key.clusterQuotaName, agg.key.reportDate,
		nullableInt64Digest(agg.cpuRequestHard), nullableInt64Digest(agg.cpuRequestUsed),
		nullableInt64Digest(agg.cpuLimitHard), nullableInt64Digest(agg.cpuLimitUsed),
		nullableInt64Digest(agg.memoryRequestHard), nullableInt64Digest(agg.memoryRequestUsed),
		nullableInt64Digest(agg.memoryLimitHard), nullableInt64Digest(agg.memoryLimitUsed),
		nullableInt64Digest(agg.storageRequestHard), nullableInt64Digest(agg.storageRequestUsed),
		nullableInt64Digest(agg.podsHard), nullableInt64Digest(agg.podsUsed),
		nullableInt64Digest(agg.objectCountHard), nullableInt64Digest(agg.objectCountUsed),
		nullableStringDigest(agg.namespaces),
	)
	if err != nil {
		return fmt.Errorf("upsert cluster quota digest %s: %w", agg.key.clusterQuotaName, err)
	}
	return nil
}

func nullableInt64Digest(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullableStringDigest(v string) any {
	if v == "" {
		return nil
	}
	return v
}
