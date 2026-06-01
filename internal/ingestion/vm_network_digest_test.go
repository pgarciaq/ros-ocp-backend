package ingestion

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildDailyVMDigests_NetworkAggregation(t *testing.T) {
	base := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	rx := 30_000_000.0
	tx := 32_500_000.0
	rxPps := 50_000.0
	txPps := 50_000.0
	rxDrop := 100.0
	txDrop := 50.0

	rows := []VMRow{
		vmNetworkSampleRow(base, "net-vm", "edge", rx, tx, rxPps, txPps, rxDrop, txDrop),
		vmNetworkSampleRow(base.Add(15*time.Minute), "net-vm", "edge", rx*0.9, tx*0.9, rxPps, txPps, rxDrop, txDrop),
	}

	digests := BuildDailyVMDigests(rows)
	key := VMDigestKey{VMName: "net-vm", Namespace: "edge", BucketDate: time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)}
	d, ok := digests[key]
	require.True(t, ok)
	assert.Equal(t, int64(62_500_000), d.NetThroughputP95BPS)
	assert.Equal(t, int64(100_000), d.NetPPSP95)
	// (100+50)/(50000+50000)*10000 = 15 bp
	assert.Equal(t, int32(15), d.NetDropRatioMaxBP)
}

func TestParseVMCSVRows_BackwardCompatNoNetworkColumns(t *testing.T) {
	csv := CanonicalVMUsageCSVHeader() + `
2026-05-01T12:00:00Z,2026-05-01T12:15:00Z,vm1,ns1,node1,linux,1000,2000,2000,4096,8192,,1073741824,,,,100,50,1024,512,0
`
	rows, err := ParseVMCSVRows(strings.NewReader(csv))
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Nil(t, rows[0].NetRxBytesPerSec)
	assert.Nil(t, rows[0].NetTxBytesPerSec)
}

func vmNetworkSampleRow(start time.Time, vm, ns string, rxBps, txBps, rxPps, txPps, rxDrop, txDrop float64) VMRow {
	return VMRow{
		IntervalStart:      start,
		IntervalEnd:        start.Add(15 * time.Minute),
		VMName:             vm,
		Namespace:          ns,
		NodeName:           "node1",
		GuestOS:            "linux",
		CPUUsageMC:         500,
		CPURequestMC:       4000,
		CPULimitMC:         4000,
		MemoryUsageKiB:     2 * 1024 * 1024,
		MemoryRequestKiB:   8 * 1024 * 1024,
		DiskAllocatedBytes: 100 * 1024 * 1024 * 1024,
		NetRxBytesPerSec:   &rxBps,
		NetTxBytesPerSec:   &txBps,
		NetRxPacketsPerSec: &rxPps,
		NetTxPacketsPerSec: &txPps,
		NetRxDropsPerSec:   &rxDrop,
		NetTxDropsPerSec:   &txDrop,
	}
}
