package ingestion

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

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
	intervalStart      int
	namespace          int
	vmName             int
	gpuUUID            int
	gpuModel           int
	utilizationAvg     int
	utilizationMax     int
	fbUsedAvgMiB       int
	fbUsedMaxMiB       int
	smActiveAvg        int
	tensorActiveAvg    int
	dramActiveAvg      int
	migProfile         int
	maxSlices          int
}

// VMGPUDeviceRow is one 15-minute sample for a single GPU attached to a VM.
type VMGPUDeviceRow struct {
	IntervalStart time.Time
	Namespace     string
	VMName        string
	GPUUUID       string
	GPUModel      string
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
		return idx, fmt.Errorf("VM GPU device CSV missing required columns: %s", strings.Join(missing, ", "))
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

// ParseVMGPUDeviceCSVRows parses ros-openshift-vm-gpu-device CSV content.
func ParseVMGPUDeviceCSVRows(r io.Reader) ([]VMGPUDeviceRow, error) {
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1

	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("reading VM GPU device CSV header: %w", err)
	}
	idx, err := buildVMGPUDeviceColumnIndex(header)
	if err != nil {
		return nil, err
	}

	log := logging.GetLogger()
	var rows []VMGPUDeviceRow
	lineNum := 1
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading VM GPU device CSV row: %w", err)
		}
		lineNum++
		row, parseErr := parseVMGPUDeviceRecord(record, idx)
		if parseErr != nil {
			log.Warnf("ParseVMGPUDeviceCSVRows: skipping line %d: %v", lineNum, parseErr)
			continue
		}
		if row.VMName == "" || row.Namespace == "" || row.GPUUUID == "" {
			log.Warnf("ParseVMGPUDeviceCSVRows: skipping line %d: empty vm_name, namespace, or gpu_uuid", lineNum)
			continue
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func parseVMGPUDeviceRecord(record []string, idx vmGPUDeviceHeaderIdx) (VMGPUDeviceRow, error) {
	var row VMGPUDeviceRow
	var err error

	row.IntervalStart, err = parseFlexibleTimestamp(fieldAt(record, idx.intervalStart))
	if err != nil {
		return row, fmt.Errorf("parse interval_start: %w", err)
	}
	row.Namespace = strings.TrimSpace(fieldAt(record, idx.namespace))
	row.VMName = strings.TrimSpace(fieldAt(record, idx.vmName))
	row.GPUUUID = strings.TrimSpace(fieldAt(record, idx.gpuUUID))
	row.GPUModel = strings.TrimSpace(fieldAt(record, idx.gpuModel))

	row.UtilizationAvg, err = optionalFloatValue(fieldAt(record, idx.utilizationAvg))
	if err != nil {
		return row, err
	}
	row.UtilizationMax, err = optionalFloatValue(fieldAt(record, idx.utilizationMax))
	if err != nil {
		return row, err
	}
	row.FBUsedAvgMiB, err = optionalFloatValue(fieldAt(record, idx.fbUsedAvgMiB))
	if err != nil {
		return row, err
	}
	row.FBUsedMaxMiB, err = optionalFloatValue(fieldAt(record, idx.fbUsedMaxMiB))
	if err != nil {
		return row, err
	}
	row.SMActiveAvg, err = optionalFloatValue(fieldAt(record, idx.smActiveAvg))
	if err != nil {
		return row, err
	}
	row.TensorActiveAvg, err = optionalFloatValue(fieldAt(record, idx.tensorActiveAvg))
	if err != nil {
		return row, err
	}
	row.DRAMActiveAvg, err = optionalFloatValue(fieldAt(record, idx.dramActiveAvg))
	if err != nil {
		return row, err
	}
	row.MIGProfile = strings.TrimSpace(fieldAt(record, idx.migProfile))
	if v := strings.TrimSpace(fieldAt(record, idx.maxSlices)); v != "" {
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil {
			return row, fmt.Errorf("parse max_slices: %w", err)
		}
		row.MaxSlices = int32(n)
	}
	return row, nil
}

// MergeVMGPUDeviceRowsIntoDigests aggregates device CSV samples into digest GPU device lists.
func MergeVMGPUDeviceRowsIntoDigests(digests map[VMDigestKey]VMDigestResult, deviceRows []VMGPUDeviceRow) {
	type deviceKey struct {
		digest VMDigestKey
		uuid   string
	}
	acc := make(map[deviceKey]*vmGPUDeviceAccumulator)

	for _, r := range deviceRows {
		bucket := vmBucketDate(r.IntervalStart)
		dk := VMDigestKey{VMName: r.VMName, Namespace: r.Namespace, BucketDate: bucket}
		uuid := r.GPUUUID
		key := deviceKey{digest: dk, uuid: uuid}
		dev, ok := acc[key]
		if !ok {
			dev = &vmGPUDeviceAccumulator{uuid: uuid, model: r.GPUModel, maxSlices: r.MaxSlices, migProfile: r.MIGProfile}
			acc[key] = dev
		}
		if r.GPUModel != "" {
			dev.model = r.GPUModel
		}
		if r.UtilizationAvg > 0 {
			dev.utilAvg = append(dev.utilAvg, r.UtilizationAvg)
		}
		if r.UtilizationMax > 0 {
			dev.utilMax = append(dev.utilMax, r.UtilizationMax)
		}
		if r.FBUsedAvgMiB > 0 {
			dev.fbAvg = append(dev.fbAvg, r.FBUsedAvgMiB)
		}
		if r.FBUsedMaxMiB > 0 {
			dev.fbMax = append(dev.fbMax, r.FBUsedMaxMiB)
		}
		if r.SMActiveAvg > 0 {
			dev.smAvg = append(dev.smAvg, r.SMActiveAvg)
		}
		if r.TensorActiveAvg > 0 {
			dev.tensorAvg = append(dev.tensorAvg, r.TensorActiveAvg)
		}
		if r.DRAMActiveAvg > 0 {
			dev.dramAvg = append(dev.dramAvg, r.DRAMActiveAvg)
		}
		if r.MIGProfile != "" {
			dev.migProfile = r.MIGProfile
		}
		if r.MaxSlices > dev.maxSlices {
			dev.maxSlices = r.MaxSlices
		}
	}

	for key, dev := range acc {
		d, ok := digests[key.digest]
		if !ok {
			d = VMDigestResult{
				VMName:     key.digest.VMName,
				Namespace:  key.digest.Namespace,
				BucketDate: key.digest.BucketDate,
			}
		}
		d.GPUDevices = appendOrReplaceGPUDevice(d.GPUDevices, dev.toIngestGPUDeviceDigest())
		digests[key.digest] = d
	}
}

func appendOrReplaceGPUDevice(devices []ingestGPUDeviceDigest, dev ingestGPUDeviceDigest) []ingestGPUDeviceDigest {
	for i, existing := range devices {
		if existing.UUID == dev.UUID {
			devices[i] = dev
			return devices
		}
	}
	return append(devices, dev)
}

func (d *vmGPUDeviceAccumulator) toIngestGPUDeviceDigest() ingestGPUDeviceDigest {
	return ingestGPUDeviceDigest{
		UUID:          d.uuid,
		Model:         d.model,
		UtilAvgBP:     ratioToBasisPoints(avgFloatSlice(d.utilAvg)),
		UtilMaxBP:     ratioToBasisPoints(maxFloatSlice(d.utilMax)),
		FBUsedAvgMiB:  avgFloatSlice(d.fbAvg),
		FBUsedMaxMiB:  maxFloatSlice(d.fbMax),
		SMActiveAvgBP: ratioToBasisPoints(avgFloatSlice(d.smAvg)),
		TensorAvgBP:   ratioToBasisPoints(avgFloatSlice(d.tensorAvg)),
		DRAMAvgBP:     ratioToBasisPoints(avgFloatSlice(d.dramAvg)),
		MIGProfile:    d.migProfile,
		MaxSlices:     d.maxSlices,
	}
}
