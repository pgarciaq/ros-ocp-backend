package ingestion

import (
	"context"
	"io"

	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
	libcsv "github.com/redhatinsights/ros-ocp-backend/librobne/csv"
)

// VMGPUDeviceRow is a parsed ROS VM GPU-device companion CSV row (librobne/csv.VMGPURow).
type VMGPUDeviceRow = libcsv.VMGPURow

// CanonicalVMGPUDeviceCSVHeader returns the comma-separated required column
// header for ros-openshift-vm-gpu-device CSVs.
func CanonicalVMGPUDeviceCSVHeader() string {
	return libcsv.CanonicalVMGPUDeviceCSVHeader()
}

// forEachVMGPUDeviceCSVRow parses VM GPU-device companion CSV rows one at a
// time without retaining a full-slice copy.
func forEachVMGPUDeviceCSVRow(ctx context.Context, r io.Reader, fn func(VMGPUDeviceRow) error) (int, error) {
	count := 0
	skipped, err := libcsv.ForEachVMGPU(ctx, r, func(row libcsv.VMGPURow) error {
		if err := fn(row); err != nil {
			return err
		}
		count++
		return nil
	})
	if skipped > 0 {
		metrics.IncCSVRowsSkipped("vm-gpu-device", skipped)
		logging.GetLogger().Warnf("ParseVMGPUDeviceCSVRows: skipped %d malformed or invalid rows", skipped)
	}
	return count, err
}

// ParseVMGPUDeviceCSVRows parses ros-openshift-vm-gpu-device CSV content into
// VMGPUDeviceRow values. Processor ingest uses forEachVMGPUDeviceCSVRow; this
// collector is for tests and callers that still want a slice.
func ParseVMGPUDeviceCSVRows(r io.Reader) ([]VMGPUDeviceRow, error) {
	var rows []VMGPUDeviceRow
	_, err := forEachVMGPUDeviceCSVRow(context.Background(), r, func(row VMGPUDeviceRow) error {
		rows = append(rows, row)
		return nil
	})
	return rows, err
}

type vmGPUDeviceAccKey struct {
	digest VMDigestKey
	uuid   string
}

func addVMGPUDeviceRow(acc map[vmGPUDeviceAccKey]*vmGPUDeviceAccumulator, r VMGPUDeviceRow, weightFn func(VMGPUDeviceRow) float64) {
	if weightFn != nil && weightFn(r) <= 0 {
		return
	}
	bucket := vmBucketDate(r.IntervalStart)
	dk := VMDigestKey{VMName: r.VMName, Namespace: r.Namespace, BucketDate: bucket}
	key := vmGPUDeviceAccKey{digest: dk, uuid: r.GPUUUID}
	dev, ok := acc[key]
	if !ok {
		dev = &vmGPUDeviceAccumulator{uuid: r.GPUUUID, model: r.GPUModel, maxSlices: r.MaxSlices, migProfile: r.MIGProfile}
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

func applyVMGPUDeviceAcc(acc map[vmGPUDeviceAccKey]*vmGPUDeviceAccumulator, digests map[VMDigestKey]VMDigestResult) {
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

// MergeVMGPUDeviceRowsIntoDigests aggregates device CSV samples into digest GPU device lists.
func MergeVMGPUDeviceRowsIntoDigests(digests map[VMDigestKey]VMDigestResult, deviceRows []VMGPUDeviceRow) {
	mergeVMGPUDeviceRows(digests, deviceRows, nil)
}

// MergeVMGPUDeviceRowsIntoDigestsIfWeight is drop-or-full: weight <= 0 drops the sample.
func MergeVMGPUDeviceRowsIntoDigestsIfWeight(digests map[VMDigestKey]VMDigestResult, deviceRows []VMGPUDeviceRow, weightFn func(VMGPUDeviceRow) float64) {
	mergeVMGPUDeviceRows(digests, deviceRows, weightFn)
}

func mergeVMGPUDeviceRows(digests map[VMDigestKey]VMDigestResult, deviceRows []VMGPUDeviceRow, weightFn func(VMGPUDeviceRow) float64) {
	acc := make(map[vmGPUDeviceAccKey]*vmGPUDeviceAccumulator)
	for _, r := range deviceRows {
		addVMGPUDeviceRow(acc, r, weightFn)
	}
	applyVMGPUDeviceAcc(acc, digests)
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
