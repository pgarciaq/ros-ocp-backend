package engine

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// retentionFixtureMonth picks a calendar month for history-retention fixtures
// that is provably on the correct side of BOTH cutoffs on any date (#779):
// the digest months cutoff would KEEP a partition of that month
// (YM >= digestCutoffYM), and the history days cutoff would SWEEP it
// (YM < historyCutoffYM). The pre-fix behavior this rules out is the
// "now minus 2 months" choice, whose month can overlap the days cutoff —
// production whole-partition policy then correctly keeps the partition and
// the test fails on valid behavior.
//
// The largest month the days cutoff sweeps is the month BEFORE the cutoff's
// own month (the cutoff's month is kept whole by the ym >= cutoffYM policy
// in SweepPartitionedTables). If even that month falls below the digest
// cutoff, no calendar month qualifies and the window is impossible.
func retentionFixtureMonth(now time.Time, historyDays, retentionMonths int) (time.Time, error) {
	histCutoff := now.AddDate(0, 0, -historyDays)
	digestCutoff := now.AddDate(0, -retentionMonths, 0)
	histYM := histCutoff.Format("200601")
	digestYM := digestCutoff.Format("200601")

	cand := time.Date(histCutoff.Year(), histCutoff.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -1, 0)
	if cand.Format("200601") < digestYM {
		return time.Time{}, fmt.Errorf(
			"retentionFixtureMonth: no calendar month in [digest %s, history %s) for now=%s days=%d months=%d",
			digestYM, histYM, now.Format(time.RFC3339), historyDays, retentionMonths)
	}
	return cand, nil
}

// TestRetentionFixtureMonth pins the #779 fixture invariants across calendar
// edge dates. Each assertion names the pre-fix behavior it rules out:
// the >= months-cutoff assertion rules out a fixture the months cutoff would
// sweep (test would then pass under the WRONG cutoff class); the < days-cutoff
// assertion rules out a fixture month overlapping the retention window
// (whole-partition policy keeps it, test fails on valid behavior).
func TestRetentionFixtureMonth(t *testing.T) {
	check := func(t *testing.T, now time.Time, historyDays, retentionMonths int) {
		t.Helper()
		ms, err := retentionFixtureMonth(now, historyDays, retentionMonths)
		require.NoError(t, err)
		ym := ms.Format("200601")
		digestYM := now.AddDate(0, -retentionMonths, 0).Format("200601")
		histYM := now.AddDate(0, 0, -historyDays).Format("200601")
		assert.GreaterOrEqual(t, ym, digestYM,
			"months cutoff must KEEP the fixture partition (a lower month would let the test pass under the months cutoff — wrong class)")
		assert.Less(t, ym, histYM,
			"days cutoff must SWEEP the fixture partition (an overlapping month is kept by whole-partition policy)")
		assert.Equal(t, 1, ms.Day(), "helper returns a month start")
		_, _, d := ms.UTC().Date()
		assert.Equal(t, 1, d)
	}

	cases := []struct {
		name            string
		now             time.Time
		historyDays     int
		retentionMonths int
	}{
		{"typical november", time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC), 45, 3},
		{"mid-month", time.Date(2026, 11, 15, 23, 59, 0, 0, time.UTC), 45, 3},
		{"month start first hour", time.Date(2026, 12, 1, 0, 30, 0, 0, time.UTC), 45, 3},
		{"year boundary", time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), 45, 3},
		{"leap february", time.Date(2028, 3, 1, 5, 0, 0, 0, time.UTC), 45, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			check(t, tc.now, tc.historyDays, tc.retentionMonths)
		})
	}

	// Impossible window (Review Focus 1): months=1, days=25 gives a ~5-day
	// interval — no calendar month fits. Ruled out: silently returning a
	// wrong-side month instead of erroring.
	_, err := retentionFixtureMonth(time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC), 25, 1)
	require.Error(t, err, "impossible window must error, never return a wrong-side month")
	assert.Contains(t, err.Error(), "no calendar month")
}
