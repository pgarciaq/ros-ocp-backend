package csv

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// ClusterQuotaRow is one interval from a ClusterResourceQuota ROS CSV.
type ClusterQuotaRow struct {
	IntervalStart           time.Time
	IntervalEnd             time.Time
	ClusterQuotaName        string
	CPURequestHardMC        int64
	CPURequestUsedMC        int64
	CPULimitHardMC          int64
	CPULimitUsedMC          int64
	MemoryRequestHardBytes  int64
	MemoryRequestUsedBytes  int64
	MemoryLimitHardBytes    int64
	MemoryLimitUsedBytes    int64
	StorageRequestHardBytes int64
	StorageRequestUsedBytes int64
	PodsHard                int64
	PodsUsed                int64
	ObjectCountHard         int64
	ObjectCountUsed         int64
	Namespaces              string
}

// MissingClusterQuotaColumnsError lists required cluster-quota headers that were absent.
type MissingClusterQuotaColumnsError struct {
	Columns []string
}

func (e *MissingClusterQuotaColumnsError) Error() string {
	return fmt.Sprintf("not a cluster-quota CSV (missing columns: %s)", strings.Join(e.Columns, ", "))
}

type clusterQuotaColumnIndex struct {
	intervalStart, intervalEnd int
	clusterQuotaName           int
	cpuRequestHard             int
	cpuRequestUsed             int
	cpuLimitHard               int
	cpuLimitUsed               int
	memRequestHard             int
	memRequestUsed             int
	memLimitHard               int
	memLimitUsed               int
	storageRequestHard         int
	storageRequestUsed         int
	podsHard                   int
	podsUsed                   int
	objectCountHard            int
	objectCountUsed            int
	namespaces                 int
}

func newClusterQuotaColumnIndex() clusterQuotaColumnIndex {
	return clusterQuotaColumnIndex{
		intervalStart: -1, intervalEnd: -1, clusterQuotaName: -1,
		cpuRequestHard: -1, cpuRequestUsed: -1,
		cpuLimitHard: -1, cpuLimitUsed: -1,
		memRequestHard: -1, memRequestUsed: -1,
		memLimitHard: -1, memLimitUsed: -1,
		storageRequestHard: -1, storageRequestUsed: -1,
		podsHard: -1, podsUsed: -1,
		objectCountHard: -1, objectCountUsed: -1,
		namespaces: -1,
	}
}

func buildClusterQuotaColumnIndex(header []string) (clusterQuotaColumnIndex, error) {
	idx := newClusterQuotaColumnIndex()
	for i, col := range header {
		switch strings.TrimSpace(strings.ToLower(col)) {
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
		case "storage_request_hard":
			idx.storageRequestHard = i
		case "storage_request_used":
			idx.storageRequestUsed = i
		case "pods_hard":
			idx.podsHard = i
		case "pods_used":
			idx.podsUsed = i
		case "object_count_hard":
			idx.objectCountHard = i
		case "object_count_used":
			idx.objectCountUsed = i
		case "namespaces":
			idx.namespaces = i
		}
	}
	var missing []string
	if idx.intervalStart < 0 {
		missing = append(missing, "interval_start")
	}
	if idx.intervalEnd < 0 {
		missing = append(missing, "interval_end")
	}
	if idx.clusterQuotaName < 0 {
		missing = append(missing, "cluster_quota_name")
	}
	if len(missing) > 0 {
		return idx, &MissingClusterQuotaColumnsError{Columns: missing}
	}
	return idx, nil
}

// ParseClusterQuotaRows reads a ClusterResourceQuota CSV. Empty names and
// unparseable numbers/timestamps are skipped (counted in skipped).
func ParseClusterQuotaRows(r io.Reader) (rows []ClusterQuotaRow, skipped int, err error) {
	reader := csv.NewReader(r)
	reader.ReuseRecord = true
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, 0, nil
		}
		return nil, 0, fmt.Errorf("reading header: %w", err)
	}
	idx, err := buildClusterQuotaColumnIndex(header)
	if err != nil {
		return nil, 0, err
	}
	rows = make([]ClusterQuotaRow, 0, 64)
	lineNum := 1
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, 0, fmt.Errorf("reading line %d: %w", lineNum+1, err)
		}
		lineNum++
		row, parseErr := parseClusterQuotaRecord(record, idx)
		if parseErr != nil {
			skipped++
			continue
		}
		if row.ClusterQuotaName == "" {
			continue
		}
		rows = append(rows, row)
	}
	return rows, skipped, nil
}

func parseClusterQuotaRecord(record []string, idx clusterQuotaColumnIndex) (ClusterQuotaRow, error) {
	var row ClusterQuotaRow
	var err error
	row.IntervalStart, err = parseFlexibleTimestamp(cell(record, idx.intervalStart))
	if err != nil {
		return row, err
	}
	row.IntervalEnd, err = parseFlexibleTimestamp(cell(record, idx.intervalEnd))
	if err != nil {
		return row, err
	}
	row.ClusterQuotaName = cell(record, idx.clusterQuotaName)
	if row.CPURequestHardMC, err = optionalCoreToMCErr(record, idx.cpuRequestHard); err != nil {
		return row, err
	}
	if row.CPURequestUsedMC, err = optionalCoreToMCErr(record, idx.cpuRequestUsed); err != nil {
		return row, err
	}
	if row.CPULimitHardMC, err = optionalCoreToMCErr(record, idx.cpuLimitHard); err != nil {
		return row, err
	}
	if row.CPULimitUsedMC, err = optionalCoreToMCErr(record, idx.cpuLimitUsed); err != nil {
		return row, err
	}
	if row.MemoryRequestHardBytes, err = optionalInt64Err(record, idx.memRequestHard); err != nil {
		return row, err
	}
	if row.MemoryRequestUsedBytes, err = optionalInt64Err(record, idx.memRequestUsed); err != nil {
		return row, err
	}
	if row.MemoryLimitHardBytes, err = optionalInt64Err(record, idx.memLimitHard); err != nil {
		return row, err
	}
	if row.MemoryLimitUsedBytes, err = optionalInt64Err(record, idx.memLimitUsed); err != nil {
		return row, err
	}
	if row.StorageRequestHardBytes, err = optionalInt64Err(record, idx.storageRequestHard); err != nil {
		return row, err
	}
	if row.StorageRequestUsedBytes, err = optionalInt64Err(record, idx.storageRequestUsed); err != nil {
		return row, err
	}
	if row.PodsHard, err = optionalInt64Err(record, idx.podsHard); err != nil {
		return row, err
	}
	if row.PodsUsed, err = optionalInt64Err(record, idx.podsUsed); err != nil {
		return row, err
	}
	if row.ObjectCountHard, err = optionalInt64Err(record, idx.objectCountHard); err != nil {
		return row, err
	}
	if row.ObjectCountUsed, err = optionalInt64Err(record, idx.objectCountUsed); err != nil {
		return row, err
	}
	row.Namespaces = cell(record, idx.namespaces)
	return row, nil
}

func optionalCoreToMCErr(record []string, i int) (int64, error) {
	s := cell(record, i)
	if s == "" {
		return 0, nil
	}
	return coreToMillicores(s)
}

func optionalInt64Err(record []string, i int) (int64, error) {
	s := cell(record, i)
	if s == "" {
		return 0, nil
	}
	return parseInt64Field(s)
}
