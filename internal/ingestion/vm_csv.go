package ingestion

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

// vmCSVExpectedColumns is the canonical header for ros-openshift-vm-usage CSV rows.
var vmCSVExpectedColumns = []string{
	"interval_start",
	"interval_end",
	"vm_name",
	"namespace",
	"node_name",
	"guest_os",
	"cpu_usage_mc",
	"cpu_request_mc",
	"cpu_limit_mc",
	"memory_usage_kib",
	"memory_request_kib",
	"memory_available_kib",
	"disk_allocated_bytes",
	"filesystem_used_bytes",
	"filesystem_capacity_bytes",
	"disk_read_iops",
	"disk_write_iops",
	"disk_read_bytes_per_sec",
	"disk_write_bytes_per_sec",
}

type vmHeaderIdx struct {
	intervalStart           int
	intervalEnd             int
	vmName                  int
	namespace               int
	nodeName                int
	guestOS                 int
	cpuUsageMC              int
	cpuRequestMC            int
	cpuLimitMC              int
	memoryUsageKiB          int
	memoryRequestKiB        int
	memoryAvailableKiB      int
	diskAllocatedBytes      int
	filesystemUsedBytes     int
	filesystemCapacityBytes int
	diskReadIOPS            int
	diskWriteIOPS           int
	diskReadBytesPerSec     int
	diskWriteBytesPerSec    int
}

func newVMHeaderIdx() vmHeaderIdx {
	return vmHeaderIdx{
		intervalStart:           -1,
		intervalEnd:             -1,
		vmName:                  -1,
		namespace:               -1,
		nodeName:                -1,
		guestOS:                 -1,
		cpuUsageMC:              -1,
		cpuRequestMC:            -1,
		cpuLimitMC:              -1,
		memoryUsageKiB:          -1,
		memoryRequestKiB:        -1,
		memoryAvailableKiB:      -1,
		diskAllocatedBytes:      -1,
		filesystemUsedBytes:     -1,
		filesystemCapacityBytes: -1,
		diskReadIOPS:            -1,
		diskWriteIOPS:           -1,
		diskReadBytesPerSec:     -1,
		diskWriteBytesPerSec:    -1,
	}
}

func buildVMColumnIndex(header []string) (vmHeaderIdx, error) {
	idx := newVMHeaderIdx()
	for i, col := range header {
		switch strings.TrimSpace(strings.ToLower(col)) {
		case "interval_start":
			idx.intervalStart = i
		case "interval_end":
			idx.intervalEnd = i
		case "vm_name":
			idx.vmName = i
		case "namespace":
			idx.namespace = i
		case "node_name":
			idx.nodeName = i
		case "guest_os":
			idx.guestOS = i
		case "cpu_usage_mc":
			idx.cpuUsageMC = i
		case "cpu_request_mc":
			idx.cpuRequestMC = i
		case "cpu_limit_mc":
			idx.cpuLimitMC = i
		case "memory_usage_kib":
			idx.memoryUsageKiB = i
		case "memory_request_kib":
			idx.memoryRequestKiB = i
		case "memory_available_kib":
			idx.memoryAvailableKiB = i
		case "disk_allocated_bytes":
			idx.diskAllocatedBytes = i
		case "filesystem_used_bytes":
			idx.filesystemUsedBytes = i
		case "filesystem_capacity_bytes":
			idx.filesystemCapacityBytes = i
		case "disk_read_iops":
			idx.diskReadIOPS = i
		case "disk_write_iops":
			idx.diskWriteIOPS = i
		case "disk_read_bytes_per_sec":
			idx.diskReadBytesPerSec = i
		case "disk_write_bytes_per_sec":
			idx.diskWriteBytesPerSec = i
		}
	}

	var missing []string
	for _, col := range vmCSVExpectedColumns {
		if !vmColumnPresent(idx, col) {
			missing = append(missing, col)
		}
	}
	if len(missing) > 0 {
		return idx, fmt.Errorf("VM CSV missing required columns: %s", strings.Join(missing, ", "))
	}
	return idx, nil
}

func vmColumnPresent(idx vmHeaderIdx, col string) bool {
	switch col {
	case "interval_start":
		return idx.intervalStart >= 0
	case "interval_end":
		return idx.intervalEnd >= 0
	case "vm_name":
		return idx.vmName >= 0
	case "namespace":
		return idx.namespace >= 0
	case "node_name":
		return idx.nodeName >= 0
	case "guest_os":
		return idx.guestOS >= 0
	case "cpu_usage_mc":
		return idx.cpuUsageMC >= 0
	case "cpu_request_mc":
		return idx.cpuRequestMC >= 0
	case "cpu_limit_mc":
		return idx.cpuLimitMC >= 0
	case "memory_usage_kib":
		return idx.memoryUsageKiB >= 0
	case "memory_request_kib":
		return idx.memoryRequestKiB >= 0
	case "memory_available_kib":
		return idx.memoryAvailableKiB >= 0
	case "disk_allocated_bytes":
		return idx.diskAllocatedBytes >= 0
	case "filesystem_used_bytes":
		return idx.filesystemUsedBytes >= 0
	case "filesystem_capacity_bytes":
		return idx.filesystemCapacityBytes >= 0
	case "disk_read_iops":
		return idx.diskReadIOPS >= 0
	case "disk_write_iops":
		return idx.diskWriteIOPS >= 0
	case "disk_read_bytes_per_sec":
		return idx.diskReadBytesPerSec >= 0
	case "disk_write_bytes_per_sec":
		return idx.diskWriteBytesPerSec >= 0
	default:
		return false
	}
}

// ParseVMCSVRows parses ros-openshift-vm-usage CSV content into VMRow values.
func ParseVMCSVRows(r io.Reader) ([]VMRow, error) {
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1

	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("reading VM CSV header: %w", err)
	}

	idx, err := buildVMColumnIndex(header)
	if err != nil {
		return nil, err
	}

	log := logging.GetLogger()
	var rows []VMRow
	lineNum := 1
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading VM CSV row: %w", err)
		}
		lineNum++

		row, parseErr := parseVMRecord(record, idx)
		if parseErr != nil {
			log.Warnf("ParseVMCSVRows: skipping line %d: %v", lineNum, parseErr)
			continue
		}
		if row.VMName == "" || row.Namespace == "" {
			log.Warnf("ParseVMCSVRows: skipping line %d: empty vm_name or namespace", lineNum)
			continue
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func parseVMRecord(record []string, idx vmHeaderIdx) (VMRow, error) {
	var row VMRow
	var err error

	row.IntervalStart, err = parseFlexibleTimestamp(fieldAt(record, idx.intervalStart))
	if err != nil {
		return row, fmt.Errorf("parse interval_start: %w", err)
	}
	row.IntervalEnd, err = parseFlexibleTimestamp(fieldAt(record, idx.intervalEnd))
	if err != nil {
		return row, fmt.Errorf("parse interval_end: %w", err)
	}

	row.VMName = strings.TrimSpace(fieldAt(record, idx.vmName))
	row.Namespace = strings.TrimSpace(fieldAt(record, idx.namespace))
	row.NodeName = strings.TrimSpace(fieldAt(record, idx.nodeName))
	row.GuestOS = strings.TrimSpace(fieldAt(record, idx.guestOS))

	row.CPUUsageMC, err = parseRequiredFloat(fieldAt(record, idx.cpuUsageMC), "cpu_usage_mc")
	if err != nil {
		return row, err
	}
	row.CPURequestMC, err = parseRequiredFloat(fieldAt(record, idx.cpuRequestMC), "cpu_request_mc")
	if err != nil {
		return row, err
	}
	row.CPULimitMC, err = parseRequiredFloat(fieldAt(record, idx.cpuLimitMC), "cpu_limit_mc")
	if err != nil {
		return row, err
	}
	row.MemoryUsageKiB, err = parseRequiredFloat(fieldAt(record, idx.memoryUsageKiB), "memory_usage_kib")
	if err != nil {
		return row, err
	}
	row.MemoryRequestKiB, err = parseRequiredFloat(fieldAt(record, idx.memoryRequestKiB), "memory_request_kib")
	if err != nil {
		return row, err
	}
	row.DiskAllocatedBytes, err = parseRequiredFloat(fieldAt(record, idx.diskAllocatedBytes), "disk_allocated_bytes")
	if err != nil {
		return row, err
	}

	row.MemoryAvailableKiB, err = parseOptionalFloat(fieldAt(record, idx.memoryAvailableKiB))
	if err != nil {
		return row, fmt.Errorf("parse memory_available_kib: %w", err)
	}
	row.FilesystemUsedBytes, err = parseOptionalFloat(fieldAt(record, idx.filesystemUsedBytes))
	if err != nil {
		return row, fmt.Errorf("parse filesystem_used_bytes: %w", err)
	}
	row.FilesystemCapacityBytes, err = parseOptionalFloat(fieldAt(record, idx.filesystemCapacityBytes))
	if err != nil {
		return row, fmt.Errorf("parse filesystem_capacity_bytes: %w", err)
	}
	row.DiskReadIOPS, err = parseOptionalFloat(fieldAt(record, idx.diskReadIOPS))
	if err != nil {
		return row, fmt.Errorf("parse disk_read_iops: %w", err)
	}
	row.DiskWriteIOPS, err = parseOptionalFloat(fieldAt(record, idx.diskWriteIOPS))
	if err != nil {
		return row, fmt.Errorf("parse disk_write_iops: %w", err)
	}
	row.DiskReadBytesPerSec, err = parseOptionalFloat(fieldAt(record, idx.diskReadBytesPerSec))
	if err != nil {
		return row, fmt.Errorf("parse disk_read_bytes_per_sec: %w", err)
	}
	row.DiskWriteBytesPerSec, err = parseOptionalFloat(fieldAt(record, idx.diskWriteBytesPerSec))
	if err != nil {
		return row, fmt.Errorf("parse disk_write_bytes_per_sec: %w", err)
	}

	return row, nil
}

func fieldAt(record []string, col int) string {
	if col < 0 || col >= len(record) {
		return ""
	}
	return record[col]
}

func parseRequiredFloat(s, field string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("%s is empty", field)
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", field, err)
	}
	if f < 0 {
		return 0, fmt.Errorf("%s is negative", field)
	}
	return f, nil
}

func parseOptionalFloat(s string) (*float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil, err
	}
	if f < 0 {
		return nil, fmt.Errorf("negative value %v", f)
	}
	return &f, nil
}
