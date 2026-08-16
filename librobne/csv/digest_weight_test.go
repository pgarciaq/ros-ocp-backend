package csv

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDailyDigestsWeighted_SkipsAllOffHoursDays(t *testing.T) {
	t.Parallel()
	csvBody := niseHeader() + "\n" +
		niseRow("app", "api", "2026-01-05 12:00:00 +0000 UTC", "2026-01-05 13:00:00 +0000 UTC", "0.2", "0.1") + "\n" +
		niseRow("app", "api", "2026-01-05 20:00:00 +0000 UTC", "2026-01-05 21:00:00 +0000 UTC", "0.4", "0.2") + "\n" +
		niseRow("app", "api", "2026-01-03 12:00:00 +0000 UTC", "2026-01-03 13:00:00 +0000 UTC", "0.3", "0.15") + "\n"
	rows, skipped, err := ParseRows(strings.NewReader(csvBody))
	require.NoError(t, err)
	require.Zero(t, skipped)

	all, _, err := DailyDigests(rows)
	require.NoError(t, err)
	require.Len(t, all, 2)

	weightFn := func(t time.Time) float64 {
		if t.Weekday() == time.Monday && t.Hour() >= 9 && t.Hour() < 17 {
			return 1
		}
		return 0
	}
	bh, ds, err := DailyDigestsWeighted(rows, weightFn)
	require.NoError(t, err)
	require.Len(t, bh, 1, "Saturday dropped; Monday kept because 12:00 is in-window")
	assert.Equal(t, time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC), bh[0].Row.BucketDate)
	assert.Equal(t, int64(1), bh[0].Row.SampleCount)
	assert.Equal(t, time.Date(2026, 1, 5, 21, 0, 0, 0, time.UTC), ds.MaxEnd)
}

func TestDailyNamespaceDigestsWeighted_SkipsAllOffHoursDays(t *testing.T) {
	t.Parallel()
	csvBody := strings.Join([]string{
		"interval_start,interval_end,namespace,cpu_request_namespace_sum,cpu_usage_namespace_avg,memory_request_namespace_sum,memory_usage_namespace_avg",
		"2026-01-05 12:00:00 +0000 UTC,2026-01-05 13:00:00 +0000 UTC,app,0.200,0.100,1073741824,536870912",
		"2026-01-05 20:00:00 +0000 UTC,2026-01-05 21:00:00 +0000 UTC,app,0.400,0.200,1073741824,536870912",
		"2026-01-03 12:00:00 +0000 UTC,2026-01-03 13:00:00 +0000 UTC,app,0.300,0.150,1073741824,536870912",
	}, "\n")
	rows, skipped, err := ParseNamespaceRows(strings.NewReader(csvBody))
	require.NoError(t, err)
	require.Zero(t, skipped)

	all, _, err := DailyNamespaceDigests(rows)
	require.NoError(t, err)
	require.Len(t, all["app"], 2)

	weightFn := func(t time.Time) float64 {
		if t.Weekday() == time.Monday && t.Hour() >= 9 && t.Hour() < 17 {
			return 1
		}
		return 0
	}
	bh, _, err := DailyNamespaceDigestsWeighted(rows, weightFn)
	require.NoError(t, err)
	require.Len(t, bh["app"], 1)
	assert.Equal(t, int64(1), bh["app"][0].SampleCount)
}
