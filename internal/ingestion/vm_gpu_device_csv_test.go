package ingestion

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func vmGPUDeviceCSVHeader() string {
	return CanonicalVMGPUDeviceCSVHeader()
}

func TestParseVMGPUDeviceCSV_ValidRows(t *testing.T) {
	csv := vmGPUDeviceCSVHeader() + `
2026-05-01T12:00:00Z,ml-ns,train-vm,gpu-aaa,A100,0.10,0.25,1024,2048,0.05,0.02,0.03,1g.5gb,7
`
	rows, err := ParseVMGPUDeviceCSVRows(strings.NewReader(csv))
	require.NoError(t, err)
	require.Len(t, rows, 1)

	row := rows[0]
	assert.Equal(t, "train-vm", row.VMName)
	assert.Equal(t, "ml-ns", row.Namespace)
	assert.Equal(t, "gpu-aaa", row.GPUUUID)
	assert.Equal(t, "A100", row.GPUModel)
	assert.InDelta(t, 0.10, row.UtilizationAvg, 0.001)
	assert.InDelta(t, 0.25, row.UtilizationMax, 0.001)
	assert.InDelta(t, 1024, row.FBUsedAvgMiB, 0.001)
	assert.Equal(t, "1g.5gb", row.MIGProfile)
	assert.Equal(t, int32(7), row.MaxSlices)
	assert.Equal(t, 2026, row.IntervalStart.Year())
}

func TestParseVMGPUDeviceCSV_EmptyFile(t *testing.T) {
	rows, err := ParseVMGPUDeviceCSVRows(strings.NewReader(""))
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestParseVMGPUDeviceCSV_HeaderOnly(t *testing.T) {
	csv := vmGPUDeviceCSVHeader() + "\n"
	rows, err := ParseVMGPUDeviceCSVRows(strings.NewReader(csv))
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestParseVMGPUDeviceCSV_MalformedRow(t *testing.T) {
	csv := vmGPUDeviceCSVHeader() + `
not-a-timestamp,ns,bad-vm,gpu-1,,bad-util,,,,,,,,
2026-05-01T12:00:00Z,prod,good-vm,gpu-2,H100,0.05,0.10,512,1024,,,,,
`
	rows, err := ParseVMGPUDeviceCSVRows(strings.NewReader(csv))
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "good-vm", rows[0].VMName)
}

func TestParseVMGPUDeviceCSV_MissingColumns(t *testing.T) {
	csv := `interval_start,namespace,vm_name,gpu_uuid
2026-05-01T12:00:00Z,ns,vm1,gpu-1
`
	_, err := ParseVMGPUDeviceCSVRows(strings.NewReader(csv))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing columns")
}

func TestForEachVMGPUDeviceCSVRow_DoesNotMaterializeFullSlice(t *testing.T) {
	csv := vmGPUDeviceCSVHeader() + `
2026-05-01T12:00:00Z,ns,vm-a,gpu-1,A100,0.10,0.25,1024,2048,0.05,0.02,0.03,1g.5gb,7
2026-05-01T12:15:00Z,ns,vm-a,gpu-1,A100,0.11,0.26,1024,2048,0.05,0.02,0.03,1g.5gb,7
`
	seen := 0
	count, err := forEachVMGPUDeviceCSVRow(context.Background(), strings.NewReader(csv), func(row VMGPUDeviceRow) error {
		seen++
		assert.Equal(t, "vm-a", row.VMName)
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 2, count)
	assert.Equal(t, 2, seen)
}
