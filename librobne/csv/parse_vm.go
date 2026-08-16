package csv

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// VMRow is one interval from a ROS VM usage CSV (ros-openshift-vm-usage / ocp_ros_vm_usage).
type VMRow struct {
	IntervalStart time.Time
	IntervalEnd   time.Time
	VMName        string
	Namespace     string
	NodeName      string
	GuestOS       string

	CPUUsageMC   float64
	CPURequestMC float64
	CPULimitMC   float64

	MemoryUsageKiB     float64
	MemoryRequestKiB   float64
	MemoryAvailableKiB *float64

	DiskAllocatedBytes float64

	FilesystemUsedBytes     *float64
	FilesystemCapacityBytes *float64

	DiskReadIOPS         *float64
	DiskWriteIOPS        *float64
	DiskReadBytesPerSec  *float64
	DiskWriteBytesPerSec *float64

	RestartCount *int32

	GPUUUID            *string
	GPUCount           *int32
	GPUModel           *string
	GPUUtilizationAvg  *float64
	GPUUtilizationMax  *float64
	GPUFBUsedAvgMiB    *float64
	GPUFBUsedMaxMiB    *float64
	GPUSMActiveAvg     *float64
	GPUTensorActiveAvg *float64
	GPUDRAMActiveAvg   *float64
	GPUMIGProfile      *string
	GPUMaxSlices       *int32

	NetRxBytesPerSec   *float64
	NetTxBytesPerSec   *float64
	NetRxPacketsPerSec *float64
	NetTxPacketsPerSec *float64
	NetRxDropsPerSec   *float64
	NetTxDropsPerSec   *float64
}

// MissingVMColumnsError lists required VM usage headers that were absent.
type MissingVMColumnsError struct {
	Columns []string
}

func (e *MissingVMColumnsError) Error() string {
	return fmt.Sprintf("not a VM usage CSV (missing columns: %s)", strings.Join(e.Columns, ", "))
}

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
	intervalStart, intervalEnd int
	vmName, namespace          int
	nodeName, guestOS          int
	cpuUsageMC, cpuRequestMC   int
	cpuLimitMC                 int
	memoryUsageKiB             int
	memoryRequestKiB           int
	memoryAvailableKiB         int
	diskAllocatedBytes         int
	filesystemUsedBytes        int
	filesystemCapacityBytes    int
	diskReadIOPS               int
	diskWriteIOPS              int
	diskReadBytesPerSec        int
	diskWriteBytesPerSec       int
	restartCount               int
	gpuUUID, gpuCount          int
	gpuModel                   int
	gpuUtilizationAvg          int
	gpuUtilizationMax          int
	gpuFBUsedAvgMiB            int
	gpuFBUsedMaxMiB            int
	gpuSMActiveAvg             int
	gpuTensorActiveAvg         int
	gpuDRAMActiveAvg           int
	gpuMIGProfile              int
	gpuMaxSlices               int
	netRxBytesPerSec           int
	netTxBytesPerSec           int
	netRxPacketsPerSec         int
	netTxPacketsPerSec         int
	netRxDropsPerSec           int
	netTxDropsPerSec           int
}

func newVMHeaderIdx() vmHeaderIdx {
	return vmHeaderIdx{
		intervalStart: -1, intervalEnd: -1, vmName: -1, namespace: -1,
		nodeName: -1, guestOS: -1, cpuUsageMC: -1, cpuRequestMC: -1, cpuLimitMC: -1,
		memoryUsageKiB: -1, memoryRequestKiB: -1, memoryAvailableKiB: -1,
		diskAllocatedBytes: -1, filesystemUsedBytes: -1, filesystemCapacityBytes: -1,
		diskReadIOPS: -1, diskWriteIOPS: -1, diskReadBytesPerSec: -1, diskWriteBytesPerSec: -1,
		restartCount: -1, gpuUUID: -1, gpuCount: -1, gpuModel: -1,
		gpuUtilizationAvg: -1, gpuUtilizationMax: -1, gpuFBUsedAvgMiB: -1, gpuFBUsedMaxMiB: -1,
		gpuSMActiveAvg: -1, gpuTensorActiveAvg: -1, gpuDRAMActiveAvg: -1,
		gpuMIGProfile: -1, gpuMaxSlices: -1,
		netRxBytesPerSec: -1, netTxBytesPerSec: -1, netRxPacketsPerSec: -1,
		netTxPacketsPerSec: -1, netRxDropsPerSec: -1, netTxDropsPerSec: -1,
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
		case "restart_count":
			idx.restartCount = i
		case "gpu_uuid":
			idx.gpuUUID = i
		case "gpu_count":
			idx.gpuCount = i
		case "gpu_model":
			idx.gpuModel = i
		case "gpu_utilization_avg":
			idx.gpuUtilizationAvg = i
		case "gpu_utilization_max":
			idx.gpuUtilizationMax = i
		case "gpu_fb_used_avg_mib":
			idx.gpuFBUsedAvgMiB = i
		case "gpu_fb_used_max_mib":
			idx.gpuFBUsedMaxMiB = i
		case "gpu_sm_active_avg":
			idx.gpuSMActiveAvg = i
		case "gpu_tensor_active_avg":
			idx.gpuTensorActiveAvg = i
		case "gpu_dram_active_avg":
			idx.gpuDRAMActiveAvg = i
		case "gpu_mig_profile":
			idx.gpuMIGProfile = i
		case "gpu_max_slices":
			idx.gpuMaxSlices = i
		case "net_rx_bytes_per_sec":
			idx.netRxBytesPerSec = i
		case "net_tx_bytes_per_sec":
			idx.netTxBytesPerSec = i
		case "net_rx_packets_per_sec":
			idx.netRxPacketsPerSec = i
		case "net_tx_packets_per_sec":
			idx.netTxPacketsPerSec = i
		case "net_rx_drops_per_sec":
			idx.netRxDropsPerSec = i
		case "net_tx_drops_per_sec":
			idx.netTxDropsPerSec = i
		}
	}
	var missing []string
	for _, col := range vmCSVExpectedColumns {
		if !vmColumnPresent(idx, col) {
			missing = append(missing, col)
		}
	}
	if len(missing) > 0 {
		return idx, &MissingVMColumnsError{Columns: missing}
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

// ParseVMRows reads a ROS VM usage CSV. Empty vm_name or namespace rows and
// bad timestamps/numbers are skipped (counted in skipped). Structural CSV
// errors still fail.
func ParseVMRows(r io.Reader) (rows []VMRow, skipped int, err error) {
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
	idx, err := buildVMColumnIndex(header)
	if err != nil {
		return nil, 0, err
	}
	rows = make([]VMRow, 0, 256)
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
		row, parseErr := parseVMRecord(record, idx)
		if parseErr != nil {
			skipped++
			continue
		}
		if row.VMName == "" || row.Namespace == "" {
			skipped++
			continue
		}
		rows = append(rows, row)
	}
	return rows, skipped, nil
}

func parseVMRecord(record []string, idx vmHeaderIdx) (VMRow, error) {
	var row VMRow
	var err error
	row.IntervalStart, err = parseFlexibleTimestamp(cell(record, idx.intervalStart))
	if err != nil {
		return row, err
	}
	row.IntervalEnd, err = parseFlexibleTimestamp(cell(record, idx.intervalEnd))
	if err != nil {
		return row, err
	}
	row.VMName = cell(record, idx.vmName)
	row.Namespace = cell(record, idx.namespace)
	row.NodeName = cell(record, idx.nodeName)
	row.GuestOS = cell(record, idx.guestOS)

	row.CPUUsageMC, err = parseRequiredFloat(cell(record, idx.cpuUsageMC), "cpu_usage_mc")
	if err != nil {
		return row, err
	}
	row.CPURequestMC, err = parseRequiredFloat(cell(record, idx.cpuRequestMC), "cpu_request_mc")
	if err != nil {
		return row, err
	}
	row.CPULimitMC, err = parseRequiredFloat(cell(record, idx.cpuLimitMC), "cpu_limit_mc")
	if err != nil {
		return row, err
	}
	row.MemoryUsageKiB, err = parseRequiredFloat(cell(record, idx.memoryUsageKiB), "memory_usage_kib")
	if err != nil {
		return row, err
	}
	row.MemoryRequestKiB, err = parseRequiredFloat(cell(record, idx.memoryRequestKiB), "memory_request_kib")
	if err != nil {
		return row, err
	}
	row.DiskAllocatedBytes, err = parseRequiredFloat(cell(record, idx.diskAllocatedBytes), "disk_allocated_bytes")
	if err != nil {
		return row, err
	}

	row.MemoryAvailableKiB, err = parseOptionalFloatPtr(cell(record, idx.memoryAvailableKiB))
	if err != nil {
		return row, err
	}
	row.FilesystemUsedBytes, err = parseOptionalFloatPtr(cell(record, idx.filesystemUsedBytes))
	if err != nil {
		return row, err
	}
	row.FilesystemCapacityBytes, err = parseOptionalFloatPtr(cell(record, idx.filesystemCapacityBytes))
	if err != nil {
		return row, err
	}
	row.DiskReadIOPS, err = parseOptionalFloatPtr(cell(record, idx.diskReadIOPS))
	if err != nil {
		return row, err
	}
	row.DiskWriteIOPS, err = parseOptionalFloatPtr(cell(record, idx.diskWriteIOPS))
	if err != nil {
		return row, err
	}
	row.DiskReadBytesPerSec, err = parseOptionalFloatPtr(cell(record, idx.diskReadBytesPerSec))
	if err != nil {
		return row, err
	}
	row.DiskWriteBytesPerSec, err = parseOptionalFloatPtr(cell(record, idx.diskWriteBytesPerSec))
	if err != nil {
		return row, err
	}
	row.RestartCount, err = parseOptionalInt32(cell(record, idx.restartCount))
	if err != nil {
		return row, err
	}

	if s := cell(record, idx.gpuUUID); s != "" {
		row.GPUUUID = &s
	}
	row.GPUCount, err = parseOptionalInt32(cell(record, idx.gpuCount))
	if err != nil {
		return row, err
	}
	if s := cell(record, idx.gpuModel); s != "" {
		row.GPUModel = &s
	}
	row.GPUUtilizationAvg, err = parseOptionalFloatPtr(cell(record, idx.gpuUtilizationAvg))
	if err != nil {
		return row, err
	}
	row.GPUUtilizationMax, err = parseOptionalFloatPtr(cell(record, idx.gpuUtilizationMax))
	if err != nil {
		return row, err
	}
	row.GPUFBUsedAvgMiB, err = parseOptionalFloatPtr(cell(record, idx.gpuFBUsedAvgMiB))
	if err != nil {
		return row, err
	}
	row.GPUFBUsedMaxMiB, err = parseOptionalFloatPtr(cell(record, idx.gpuFBUsedMaxMiB))
	if err != nil {
		return row, err
	}
	row.GPUSMActiveAvg, err = parseOptionalFloatPtr(cell(record, idx.gpuSMActiveAvg))
	if err != nil {
		return row, err
	}
	row.GPUTensorActiveAvg, err = parseOptionalFloatPtr(cell(record, idx.gpuTensorActiveAvg))
	if err != nil {
		return row, err
	}
	row.GPUDRAMActiveAvg, err = parseOptionalFloatPtr(cell(record, idx.gpuDRAMActiveAvg))
	if err != nil {
		return row, err
	}
	if s := cell(record, idx.gpuMIGProfile); s != "" {
		row.GPUMIGProfile = &s
	}
	row.GPUMaxSlices, err = parseOptionalInt32(cell(record, idx.gpuMaxSlices))
	if err != nil {
		return row, err
	}

	row.NetRxBytesPerSec, err = parseOptionalFloatPtr(cell(record, idx.netRxBytesPerSec))
	if err != nil {
		return row, err
	}
	row.NetTxBytesPerSec, err = parseOptionalFloatPtr(cell(record, idx.netTxBytesPerSec))
	if err != nil {
		return row, err
	}
	row.NetRxPacketsPerSec, err = parseOptionalFloatPtr(cell(record, idx.netRxPacketsPerSec))
	if err != nil {
		return row, err
	}
	row.NetTxPacketsPerSec, err = parseOptionalFloatPtr(cell(record, idx.netTxPacketsPerSec))
	if err != nil {
		return row, err
	}
	row.NetRxDropsPerSec, err = parseOptionalFloatPtr(cell(record, idx.netRxDropsPerSec))
	if err != nil {
		return row, err
	}
	row.NetTxDropsPerSec, err = parseOptionalFloatPtr(cell(record, idx.netTxDropsPerSec))
	if err != nil {
		return row, err
	}
	return row, nil
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

func parseOptionalFloatPtr(s string) (*float64, error) {
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

func parseOptionalFloatValue(s string) (float64, error) {
	v, err := parseOptionalFloatPtr(s)
	if err != nil {
		return 0, err
	}
	if v == nil {
		return 0, nil
	}
	return *v, nil
}

func parseOptionalInt32(s string) (*int32, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	v, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		return nil, err
	}
	if v < 0 {
		return nil, fmt.Errorf("negative value %d", v)
	}
	out := int32(v)
	return &out, nil
}
