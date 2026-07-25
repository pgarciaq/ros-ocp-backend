package ingestion

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseVMPVCCSVRows_Valid(t *testing.T) {
	csv := `interval_start,interval_end,vm_name,namespace,node_name,pvc_name,disk_capacity_bytes,volume_mode
2026-05-01T12:00:00Z,2026-05-01T12:15:00Z,db-vm-01,vm-ns,worker-1,data-pvc-shared,107374182400,Filesystem
2026-05-01T12:00:00Z,2026-05-01T12:15:00Z,db-vm-01,vm-ns,worker-1,logs-pvc,53687091200,Filesystem
2026-05-01T12:00:00Z,2026-05-01T12:15:00Z,db-vm-02,vm-ns,worker-2,data-pvc-shared,107374182400,Block
`
	rows, err := ParseVMPVCCSVRows(context.Background(), strings.NewReader(csv))
	require.NoError(t, err)
	require.Len(t, rows, 3)

	assert.Equal(t, "db-vm-01", rows[0].VMName)
	assert.Equal(t, "vm-ns", rows[0].Namespace)
	assert.Equal(t, "worker-1", rows[0].NodeName)
	assert.Equal(t, "data-pvc-shared", rows[0].PVCName)
	assert.Equal(t, int64(107374182400), rows[0].DiskCapacityBytes)
	assert.Equal(t, "Filesystem", rows[0].VolumeMode)

	assert.Equal(t, "logs-pvc", rows[1].PVCName)
	assert.Equal(t, "Block", rows[2].VolumeMode)
}

func TestParseVMPVCCSVRows_EmptyFile(t *testing.T) {
	rows, err := ParseVMPVCCSVRows(context.Background(), strings.NewReader(""))
	require.NoError(t, err)
	assert.Nil(t, rows)
}

func TestParseVMPVCCSVRows_HeaderOnly(t *testing.T) {
	csv := "interval_start,vm_name,namespace,pvc_name,disk_capacity_bytes,volume_mode\n"
	rows, err := ParseVMPVCCSVRows(context.Background(), strings.NewReader(csv))
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestParseVMPVCCSVRows_MissingColumn(t *testing.T) {
	csv := "interval_start,vm_name,namespace\n2026-05-01T12:00:00Z,vm,ns\n"
	_, err := ParseVMPVCCSVRows(context.Background(), strings.NewReader(csv))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required columns")
}

func TestParseVMPVCCSVRows_SkipEmptyPVCName(t *testing.T) {
	csv := `interval_start,vm_name,namespace,pvc_name,disk_capacity_bytes,volume_mode
2026-05-01T12:00:00Z,vm,,pvc-1,100,Filesystem
2026-05-01T12:00:00Z,vm,ns,,100,Filesystem
`
	rows, err := ParseVMPVCCSVRows(context.Background(), strings.NewReader(csv))
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestParseVMPVCCSVRows_DefaultVolumeMode(t *testing.T) {
	csv := `interval_start,vm_name,namespace,pvc_name,disk_capacity_bytes,volume_mode
2026-05-01T12:00:00Z,vm-1,ns,pvc-1,100,
`
	rows, err := ParseVMPVCCSVRows(context.Background(), strings.NewReader(csv))
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "Filesystem", rows[0].VolumeMode)
}

func TestMergeVMPVCRowsIntoDigests(t *testing.T) {
	rows := []VMPVCRow{
		{
			IntervalStart:     time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
			VMName:            "db-vm-01",
			Namespace:         "vm-ns",
			PVCName:           "data-pvc",
			DiskCapacityBytes: 100,
			VolumeMode:        "Filesystem",
		},
		{
			IntervalStart:     time.Date(2026, 5, 1, 12, 15, 0, 0, time.UTC),
			VMName:            "db-vm-01",
			Namespace:         "vm-ns",
			PVCName:           "data-pvc",
			DiskCapacityBytes: 200,
			VolumeMode:        "Filesystem",
		},
		{
			IntervalStart:     time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
			VMName:            "db-vm-01",
			Namespace:         "vm-ns",
			PVCName:           "logs-pvc",
			DiskCapacityBytes: 50,
			VolumeMode:        "Block",
		},
	}

	result := MergeVMPVCRowsIntoDigests(rows)
	require.Len(t, result, 1)

	for _, pvcs := range result {
		require.Len(t, pvcs, 2)
		found := make(map[string]IngestPVCDigest)
		for _, pvc := range pvcs {
			found[pvc.PVCName] = pvc
		}
		assert.Equal(t, int64(200), found["data-pvc"].DiskCapacityBytes, "should keep max capacity")
		assert.Equal(t, int64(50), found["logs-pvc"].DiskCapacityBytes)
		assert.Equal(t, "Block", found["logs-pvc"].VolumeMode)
	}
}
