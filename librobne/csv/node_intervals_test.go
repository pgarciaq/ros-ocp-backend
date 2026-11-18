package csv

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fourIntervalHour builds two containers across the four 15-minute intervals of
// one hour. Interval i carries a node total of i*1000 mC of usage and i*1000 mC
// of requests, split evenly across the two containers.
//
// The three candidate reductions are all distinct by construction:
//
//	mean of interval totals = 2500   (correct: the hour's average load)
//	sum of every row        = 10000  (the #889 defect, 4x)
//	row count division      = 1250   (wrong: averages the containers away)
func fourIntervalHour() []Row {
	var rows []Row
	start := time.Date(2026, 5, 1, 10, 0, 1, 0, time.UTC)
	for i := 1; i <= 4; i++ {
		at := start.Add(time.Duration(i-1) * 15 * time.Minute)
		for c := 0; c < 2; c++ {
			rows = append(rows, Row{
				IntervalStart:     at,
				Node:              "node-a",
				Pod:               fmt.Sprintf("pod-%d", c),
				ContainerName:     fmt.Sprintf("container-%d", c),
				CPUUsageMC:        int64(i * 500),
				MemUsageKiB:       int64(i * 5000),
				CPURequestMC:      int64(i * 500),
				MemRequestKiB:     int64(i * 10000),
				NodeCapacityCPUMC: 64000,
			})
		}
	}
	return rows
}

func TestDailyNodeDigests_HourIsMeanOfIntervalTotals(t *testing.T) {
	t.Parallel()

	got := DailyNodeDigests(fourIntervalHour(), 0.93)
	require.Len(t, got, 1)

	assert.Equal(t, int64(2500), got[0].CPUUsageP95MC, "hour must be the mean of its interval totals, not their sum")
	assert.Equal(t, int64(2500), got[0].CPUUsageMaxMC)
	assert.Equal(t, int64(25000), got[0].MemUsageP95KiB)
	assert.Equal(t, int64(25000), got[0].MemUsageMaxKiB)

	// Requests are the peak interval for the node: 4 intervals of 500 per
	// container peak at 4000, not the 10000 a sum would give.
	assert.Equal(t, int64(4000), got[0].MaxCPURequestsMC)
	assert.Equal(t, int64(80000), got[0].MaxMemRequestsKiB)
	assert.Equal(t, int64(2), got[0].MaxPodCount)
	assert.Equal(t, int64(1), got[0].SampleCount, "one hour of samples")
}

func TestNodeIntervalSums_ReducesAcrossIntervalsNotRows(t *testing.T) {
	t.Parallel()

	var sums NodeIntervalSums
	for _, r := range fourIntervalHour() {
		sums.Add(r.IntervalStart, r.CPURequestMC, r.MemRequestKiB, r.CPUUsageMC, r.MemUsageKiB)
	}

	require.True(t, sums.Seen(10))
	assert.Equal(t, 4, sums.HourIntervalCount(10))

	cpuUse, memUse := sums.HourUsage(10)
	assert.Equal(t, int64(2500), cpuUse)
	assert.Equal(t, int64(25000), memUse)

	cpuReq, memReq := sums.HourRequests(10)
	assert.Equal(t, int64(4000), cpuReq)
	assert.Equal(t, int64(80000), memReq)

	assert.False(t, sums.Seen(11), "no rows collected for that hour")
	emptyUse, _ := sums.HourUsage(11)
	assert.Equal(t, int64(0), emptyUse)
}
