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

// VMGPURow is one interval from a ROS VM GPU-device companion CSV.
type VMGPURow struct {
	IntervalStart   time.Time
	Namespace       string
	VMName          string
	GPUUUID         string
	GPUModel        string
	UtilizationAvg  float64
	UtilizationMax  float64
	FBUsedAvgMiB    float64
	FBUsedMaxMiB    float64
	SMActiveAvg     float64
	TensorActiveAvg float64
	DRAMActiveAvg   float64
	MIGProfile      string
	MaxSlices       int32
}

// MissingVMGPUColumnsError lists required VM GPU companion headers that were absent.
type MissingVMGPUColumnsError struct {
	Columns []string
}

func (e *MissingVMGPUColumnsError) Error() string {
	return fmt.Sprintf("not a VM GPU device CSV (missing columns: %s)", strings.Join(e.Columns, ", "))
}

var vmGPUDeviceCSVExpectedColumns = []string{
	"interval_start",
	"namespace",
	"vm_name",
	"gpu_uuid",
	"gpu_model",
	"utilization_avg",
	"utilization_max",
	"fb_used_avg_mib",
	"fb_used_max_mib",
	"sm_active_avg",
	"tensor_active_avg",
	"dram_active_avg",
	"mig_profile",
	"max_slices",
}

type vmGPUDeviceHeaderIdx struct {
	intervalStart, namespace, vmName int
	gpuUUID, gpuModel                int
	utilizationAvg, utilizationMax   int
	fbUsedAvgMiB, fbUsedMaxMiB       int
	smActiveAvg, tensorActiveAvg     int
	dramActiveAvg, migProfile        int
	maxSlices                        int
}

func buildVMGPUDeviceColumnIndex(header []string) (vmGPUDeviceHeaderIdx, error) {
	idx := vmGPUDeviceHeaderIdx{
		intervalStart: -1, namespace: -1, vmName: -1, gpuUUID: -1, gpuModel: -1,
		utilizationAvg: -1, utilizationMax: -1, fbUsedAvgMiB: -1, fbUsedMaxMiB: -1,
		smActiveAvg: -1, tensorActiveAvg: -1, dramActiveAvg: -1, migProfile: -1, maxSlices: -1,
	}
	for i, col := range header {
		switch strings.TrimSpace(strings.ToLower(col)) {
		case "interval_start":
			idx.intervalStart = i
		case "namespace":
			idx.namespace = i
		case "vm_name":
			idx.vmName = i
		case "gpu_uuid":
			idx.gpuUUID = i
		case "gpu_model":
			idx.gpuModel = i
		case "utilization_avg":
			idx.utilizationAvg = i
		case "utilization_max":
			idx.utilizationMax = i
		case "fb_used_avg_mib":
			idx.fbUsedAvgMiB = i
		case "fb_used_max_mib":
			idx.fbUsedMaxMiB = i
		case "sm_active_avg":
			idx.smActiveAvg = i
		case "tensor_active_avg":
			idx.tensorActiveAvg = i
		case "dram_active_avg":
			idx.dramActiveAvg = i
		case "mig_profile":
			idx.migProfile = i
		case "max_slices":
			idx.maxSlices = i
		}
	}
	var missing []string
	for _, col := range vmGPUDeviceCSVExpectedColumns {
		if !vmGPUDeviceColumnPresent(idx, col) {
			missing = append(missing, col)
		}
	}
	if len(missing) > 0 {
		return idx, &MissingVMGPUColumnsError{Columns: missing}
	}
	return idx, nil
}

func vmGPUDeviceColumnPresent(idx vmGPUDeviceHeaderIdx, col string) bool {
	switch col {
	case "interval_start":
		return idx.intervalStart >= 0
	case "namespace":
		return idx.namespace >= 0
	case "vm_name":
		return idx.vmName >= 0
	case "gpu_uuid":
		return idx.gpuUUID >= 0
	case "gpu_model":
		return idx.gpuModel >= 0
	case "utilization_avg":
		return idx.utilizationAvg >= 0
	case "utilization_max":
		return idx.utilizationMax >= 0
	case "fb_used_avg_mib":
		return idx.fbUsedAvgMiB >= 0
	case "fb_used_max_mib":
		return idx.fbUsedMaxMiB >= 0
	case "sm_active_avg":
		return idx.smActiveAvg >= 0
	case "tensor_active_avg":
		return idx.tensorActiveAvg >= 0
	case "dram_active_avg":
		return idx.dramActiveAvg >= 0
	case "mig_profile":
		return idx.migProfile >= 0
	case "max_slices":
		return idx.maxSlices >= 0
	default:
		return false
	}
}

// ParseVMGPURows reads a ROS VM GPU-device companion CSV. Empty identity rows
// and bad timestamps are skipped. Structural CSV errors still fail.
func ParseVMGPURows(r io.Reader) (rows []VMGPURow, skipped int, err error) {
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
	idx, err := buildVMGPUDeviceColumnIndex(header)
	if err != nil {
		return nil, 0, err
	}
	rows = make([]VMGPURow, 0, 64)
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
		row, parseErr := parseVMGPUDeviceRecord(record, idx)
		if parseErr != nil {
			skipped++
			continue
		}
		if row.VMName == "" || row.Namespace == "" || row.GPUUUID == "" {
			skipped++
			continue
		}
		rows = append(rows, row)
	}
	return rows, skipped, nil
}

func parseVMGPUDeviceRecord(record []string, idx vmGPUDeviceHeaderIdx) (VMGPURow, error) {
	var row VMGPURow
	var err error
	row.IntervalStart, err = parseFlexibleTimestamp(cell(record, idx.intervalStart))
	if err != nil {
		return row, err
	}
	row.Namespace = cell(record, idx.namespace)
	row.VMName = cell(record, idx.vmName)
	row.GPUUUID = cell(record, idx.gpuUUID)
	row.GPUModel = cell(record, idx.gpuModel)
	row.UtilizationAvg, err = parseOptionalFloatValue(cell(record, idx.utilizationAvg))
	if err != nil {
		return row, err
	}
	row.UtilizationMax, err = parseOptionalFloatValue(cell(record, idx.utilizationMax))
	if err != nil {
		return row, err
	}
	row.FBUsedAvgMiB, err = parseOptionalFloatValue(cell(record, idx.fbUsedAvgMiB))
	if err != nil {
		return row, err
	}
	row.FBUsedMaxMiB, err = parseOptionalFloatValue(cell(record, idx.fbUsedMaxMiB))
	if err != nil {
		return row, err
	}
	row.SMActiveAvg, err = parseOptionalFloatValue(cell(record, idx.smActiveAvg))
	if err != nil {
		return row, err
	}
	row.TensorActiveAvg, err = parseOptionalFloatValue(cell(record, idx.tensorActiveAvg))
	if err != nil {
		return row, err
	}
	row.DRAMActiveAvg, err = parseOptionalFloatValue(cell(record, idx.dramActiveAvg))
	if err != nil {
		return row, err
	}
	row.MIGProfile = cell(record, idx.migProfile)
	if v := cell(record, idx.maxSlices); v != "" {
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil {
			return row, err
		}
		row.MaxSlices = int32(n)
	}
	return row, nil
}
