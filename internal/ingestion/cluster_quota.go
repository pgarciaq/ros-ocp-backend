package ingestion

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

// ClusterQuotaMetricRow is one interval row from a cluster-quota ROS CSV.
type ClusterQuotaMetricRow struct {
	IntervalStart      time.Time
	IntervalEnd        time.Time
	ClusterQuotaName   string
	CPURequestHardMC   int64
	CPURequestUsedMC   int64
	CPULimitHardMC     int64
	CPULimitUsedMC     int64
	MemoryRequestHard  int64
	MemoryRequestUsed  int64
	MemoryLimitHard    int64
	MemoryLimitUsed    int64
}

type crqColumnIndex struct {
	intervalStart    int
	intervalEnd      int
	clusterQuotaName int
	cpuRequestHard   int
	cpuRequestUsed   int
	cpuLimitHard     int
	cpuLimitUsed     int
	memRequestHard   int
	memRequestUsed   int
	memLimitHard     int
	memLimitUsed     int
}

func buildCRQColumnIndex(header []string) (crqColumnIndex, error) {
	idx := crqColumnIndex{
		intervalStart: -1, intervalEnd: -1, clusterQuotaName: -1,
		cpuRequestHard: -1, cpuRequestUsed: -1,
		cpuLimitHard: -1, cpuLimitUsed: -1,
		memRequestHard: -1, memRequestUsed: -1,
		memLimitHard: -1, memLimitUsed: -1,
	}
	for i, col := range header {
		switch col {
		case "interval_start":
			idx.intervalStart = i
		case "interval_end":
			idx.intervalEnd = i
		case "cluster_quota_name", "cluster_resource_quota":
			idx.clusterQuotaName = i
		case "cpu_request_hard", "cpu_request_cluster_sum":
			idx.cpuRequestHard = i
		case "cpu_request_used", "cpu_request_cluster_used":
			idx.cpuRequestUsed = i
		case "cpu_limit_hard", "cpu_limit_cluster_sum":
			idx.cpuLimitHard = i
		case "cpu_limit_used", "cpu_limit_cluster_used":
			idx.cpuLimitUsed = i
		case "memory_request_hard", "memory_request_cluster_sum":
			idx.memRequestHard = i
		case "memory_request_used", "memory_request_cluster_used":
			idx.memRequestUsed = i
		case "memory_limit_hard", "memory_limit_cluster_sum":
			idx.memLimitHard = i
		case "memory_limit_used", "memory_limit_cluster_used":
			idx.memLimitUsed = i
		}
	}
	if idx.intervalStart < 0 {
		return idx, fmt.Errorf("ParseClusterQuotaCSVRows: missing required column %q", "interval_start")
	}
	if idx.intervalEnd < 0 {
		return idx, fmt.Errorf("ParseClusterQuotaCSVRows: missing required column %q", "interval_end")
	}
	if idx.clusterQuotaName < 0 {
		return idx, fmt.Errorf("ParseClusterQuotaCSVRows: missing required column cluster_quota_name or cluster_resource_quota")
	}
	return idx, nil
}

// ParseClusterQuotaCSVRows parses cluster ResourceQuota CSV rows (header-based columns).
func ParseClusterQuotaCSVRows(r io.Reader) ([]ClusterQuotaMetricRow, error) {
	reader := csv.NewReader(r)
	header, err := reader.Read()
	if err != nil {
		if err == io.EOF {
			return nil, nil
		}
		return nil, fmt.Errorf("ParseClusterQuotaCSVRows: reading header: %w", err)
	}

	idx, err := buildCRQColumnIndex(header)
	if err != nil {
		return nil, err
	}

	var rows []ClusterQuotaMetricRow
	lineNum := 1
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("ParseClusterQuotaCSVRows: reading line %d: %w", lineNum+1, err)
		}
		lineNum++

		row, parseErr := parseCRQRecord(record, idx)
		if parseErr != nil {
			logging.GetLogger().Debugf("ParseClusterQuotaCSVRows: skipping line %d: %v", lineNum, parseErr)
			continue
		}
		if row.ClusterQuotaName == "" {
			continue
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func parseCRQRecord(record []string, idx crqColumnIndex) (ClusterQuotaMetricRow, error) {
	var row ClusterQuotaMetricRow
	var err error

	row.IntervalStart, err = parseFlexibleTimestamp(strings.TrimSpace(record[idx.intervalStart]))
	if err != nil {
		return row, fmt.Errorf("interval_start: %w", err)
	}
	row.IntervalEnd, err = parseFlexibleTimestamp(strings.TrimSpace(record[idx.intervalEnd]))
	if err != nil {
		return row, fmt.Errorf("interval_end: %w", err)
	}
	row.ClusterQuotaName = strings.TrimSpace(record[idx.clusterQuotaName])

	if idx.cpuRequestHard >= 0 && idx.cpuRequestHard < len(record) && record[idx.cpuRequestHard] != "" {
		row.CPURequestHardMC, err = CoreToMillicores(record[idx.cpuRequestHard])
		if err != nil {
			return row, err
		}
	}
	if idx.cpuRequestUsed >= 0 && idx.cpuRequestUsed < len(record) && record[idx.cpuRequestUsed] != "" {
		row.CPURequestUsedMC, err = CoreToMillicores(record[idx.cpuRequestUsed])
		if err != nil {
			return row, err
		}
	}
	if idx.cpuLimitHard >= 0 && idx.cpuLimitHard < len(record) && record[idx.cpuLimitHard] != "" {
		row.CPULimitHardMC, err = CoreToMillicores(record[idx.cpuLimitHard])
		if err != nil {
			return row, err
		}
	}
	if idx.cpuLimitUsed >= 0 && idx.cpuLimitUsed < len(record) && record[idx.cpuLimitUsed] != "" {
		row.CPULimitUsedMC, err = CoreToMillicores(record[idx.cpuLimitUsed])
		if err != nil {
			return row, err
		}
	}
	if idx.memRequestHard >= 0 && idx.memRequestHard < len(record) && record[idx.memRequestHard] != "" {
		row.MemoryRequestHard, err = parseInt64Field(record[idx.memRequestHard])
		if err != nil {
			return row, err
		}
	}
	if idx.memRequestUsed >= 0 && idx.memRequestUsed < len(record) && record[idx.memRequestUsed] != "" {
		row.MemoryRequestUsed, err = parseInt64Field(record[idx.memRequestUsed])
		if err != nil {
			return row, err
		}
	}
	if idx.memLimitHard >= 0 && idx.memLimitHard < len(record) && record[idx.memLimitHard] != "" {
		row.MemoryLimitHard, err = parseInt64Field(record[idx.memLimitHard])
		if err != nil {
			return row, err
		}
	}
	if idx.memLimitUsed >= 0 && idx.memLimitUsed < len(record) && record[idx.memLimitUsed] != "" {
		row.MemoryLimitUsed, err = parseInt64Field(record[idx.memLimitUsed])
		if err != nil {
			return row, err
		}
	}
	return row, nil
}

func parseInt64Field(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid number %q: %w", s, err)
	}
	if f < 0 {
		return 0, fmt.Errorf("negative value %q", s)
	}
	return int64(f), nil
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
}

func groupClusterQuotaRows(rows []ClusterQuotaMetricRow, orgID, clusterUUID string) map[clusterQuotaDigestKey]*clusterQuotaDigestAgg {
	out := make(map[clusterQuotaDigestKey]*clusterQuotaDigestAgg)
	for _, row := range rows {
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
		agg.memoryRequestHard = maxInt64(agg.memoryRequestHard, row.MemoryRequestHard)
		agg.memoryRequestUsed = maxInt64(agg.memoryRequestUsed, row.MemoryRequestUsed)
		agg.memoryLimitHard = maxInt64(agg.memoryLimitHard, row.MemoryLimitHard)
		agg.memoryLimitUsed = maxInt64(agg.memoryLimitUsed, row.MemoryLimitUsed)
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
func ProcessClusterQuotaCSV(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) error {
	rows, err := ParseClusterQuotaCSVRows(r)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	groups := groupClusterQuotaRows(rows, orgID, clusterUUID)
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
			memory_limit_hard, memory_limit_used
		) VALUES (
			$1, $2::uuid, $3, $4,
			$5, $6, $7, $8, $9, $10, $11, $12
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
			memory_limit_used = GREATEST(COALESCE(daily_cluster_quota_digests.memory_limit_used, 0), COALESCE(EXCLUDED.memory_limit_used, 0))`,
		agg.key.orgID, agg.key.clusterUUID, agg.key.clusterQuotaName, agg.key.reportDate,
		nullableInt64Digest(agg.cpuRequestHard), nullableInt64Digest(agg.cpuRequestUsed),
		nullableInt64Digest(agg.cpuLimitHard), nullableInt64Digest(agg.cpuLimitUsed),
		nullableInt64Digest(agg.memoryRequestHard), nullableInt64Digest(agg.memoryRequestUsed),
		nullableInt64Digest(agg.memoryLimitHard), nullableInt64Digest(agg.memoryLimitUsed),
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
