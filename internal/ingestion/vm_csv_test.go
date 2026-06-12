package ingestion

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func vmCSVHeader() string {
	return strings.Join(vmCSVExpectedColumns, ",")
}

func TestVMParseCSVRows_ValidAllColumns(t *testing.T) {
	csv := vmCSVHeader() + `
2026-05-01T12:00:00Z,2026-05-01T12:15:00Z,web-vm,production,worker-1,linux,1500,2000,4000,1048576,2097152,1572864,107374182400,53687091200,107374182400,120,80,1048576,524288
`
	rows, err := ParseVMCSVRows(strings.NewReader(csv))
	require.NoError(t, err)
	require.Len(t, rows, 1)

	row := rows[0]
	assert.Equal(t, "web-vm", row.VMName)
	assert.Equal(t, "production", row.Namespace)
	assert.Equal(t, "worker-1", row.NodeName)
	assert.Equal(t, "linux", row.GuestOS)
	assert.InDelta(t, 1500, row.CPUUsageMC, 0.001)
	assert.InDelta(t, 2000, row.CPURequestMC, 0.001)
	assert.InDelta(t, 4000, row.CPULimitMC, 0.001)
	assert.InDelta(t, 1048576, row.MemoryUsageKiB, 0.001)
	assert.InDelta(t, 2097152, row.MemoryRequestKiB, 0.001)
	require.NotNil(t, row.MemoryAvailableKiB)
	assert.InDelta(t, 1572864, *row.MemoryAvailableKiB, 0.001)
	assert.InDelta(t, 107374182400, row.DiskAllocatedBytes, 0.001)
	require.NotNil(t, row.FilesystemUsedBytes)
	assert.InDelta(t, 53687091200, *row.FilesystemUsedBytes, 0.001)
	require.NotNil(t, row.FilesystemCapacityBytes)
	assert.InDelta(t, 107374182400, *row.FilesystemCapacityBytes, 0.001)
	require.NotNil(t, row.DiskReadIOPS)
	assert.InDelta(t, 120, *row.DiskReadIOPS, 0.001)
	require.NotNil(t, row.DiskWriteIOPS)
	assert.InDelta(t, 80, *row.DiskWriteIOPS, 0.001)
	require.NotNil(t, row.DiskReadBytesPerSec)
	assert.InDelta(t, 1048576, *row.DiskReadBytesPerSec, 0.001)
	require.NotNil(t, row.DiskWriteBytesPerSec)
	assert.InDelta(t, 524288, *row.DiskWriteBytesPerSec, 0.001)
}

func TestVMParseCSVRows_MissingGuestAgentColumns(t *testing.T) {
	csv := vmCSVHeader() + `
2026-05-01T12:00:00Z,2026-05-01T12:15:00Z,db-vm,apps,node-a,linux,500,1000,2000,524288,1048576,,10737418240,,,,,,
`
	rows, err := ParseVMCSVRows(strings.NewReader(csv))
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Nil(t, rows[0].MemoryAvailableKiB)
	assert.Nil(t, rows[0].FilesystemUsedBytes)
	assert.Nil(t, rows[0].FilesystemCapacityBytes)
}

func TestVMParseCSVRows_EmptyCSV(t *testing.T) {
	csv := vmCSVHeader() + "\n"
	rows, err := ParseVMCSVRows(strings.NewReader(csv))
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestVMParseCSVRows_MalformedRowSkipped(t *testing.T) {
	before := csvRowsSkippedTotal("vm")
	csv := vmCSVHeader() + `
not-a-timestamp,2026-05-01T12:15:00Z,bad-vm,ns,node,linux,100,200,300,1024,2048,,1000,,,,,,
2026-05-01T12:00:00Z,2026-05-01T12:15:00Z,good-vm,ns,node,linux,100,200,300,1024,2048,,1000,,,,,,
`
	rows, err := ParseVMCSVRows(strings.NewReader(csv))
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "good-vm", rows[0].VMName)
	assert.Equal(t, before+1, csvRowsSkippedTotal("vm"))
}

func csvRowsSkippedTotal(reportType string) float64 {
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		return 0
	}
	for _, mf := range mfs {
		if mf.GetName() != "rosocp_csv_rows_skipped_total" {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, lp := range m.GetLabel() {
				if lp.GetName() == "report_type" && lp.GetValue() == reportType {
					return m.GetCounter().GetValue()
				}
			}
		}
	}
	return 0
}

func TestVMParseCSVRows_OldFormatWithoutRestartCount(t *testing.T) {
	// Legacy operator CSV: 18 required columns only (no restart_count or GPU columns).
	csv := vmCSVHeader() + `
2026-05-01T12:00:00Z,2026-05-01T12:15:00Z,legacy-vm,apps,node-a,linux,500,1000,2000,524288,1048576,1572864,10737418240,53687091200,107374182400,120,80,1048576,524288
`
	rows, err := ParseVMCSVRows(strings.NewReader(csv))
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Nil(t, rows[0].RestartCount)
	assert.Equal(t, "legacy-vm", rows[0].VMName)
}

func TestVMParseCSVRows_WrongHeader(t *testing.T) {
	csv := `interval_start,interval_end,vm_name
2026-05-01T12:00:00Z,2026-05-01T12:15:00Z,vm1
`
	_, err := ParseVMCSVRows(strings.NewReader(csv))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required columns")
}

func TestVMParseCSVRows_TimestampFormats(t *testing.T) {
	t.Run("RFC3339", func(t *testing.T) {
		csv := vmCSVHeader() + `
2026-05-01T12:00:00Z,2026-05-01T12:15:00Z,vm-rfc,ns,node,linux,10,20,30,1024,2048,,1000,,,,,,
`
		rows, err := ParseVMCSVRows(strings.NewReader(csv))
		require.NoError(t, err)
		require.Len(t, rows, 1)
		assert.Equal(t, 2026, rows[0].IntervalStart.Year())
		assert.Equal(t, time.May, rows[0].IntervalStart.Month())
	})

	t.Run("operator format", func(t *testing.T) {
		csv := vmCSVHeader() + `
2026-05-01 12:00:00 +0000 UTC,2026-05-01 12:15:00 +0000 UTC,vm-op,ns,node,linux,10,20,30,1024,2048,,1000,,,,,,
`
		rows, err := ParseVMCSVRows(strings.NewReader(csv))
		require.NoError(t, err)
		require.Len(t, rows, 1)
		assert.Equal(t, "vm-op", rows[0].VMName)
		assert.Equal(t, 12, rows[0].IntervalStart.UTC().Hour())
	})
}
