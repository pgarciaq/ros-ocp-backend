package engine

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

func syntheticDigestRow(day time.Time, cpuMC, memKiB int64) DigestRow {
	return DigestRow{
		BucketDate:        day,
		CPURequestP50MC:   cpuMC,
		CPURequestP60MC:   cpuMC,
		CPURequestP95MC:   cpuMC + 20,
		CPURequestP98MC:   cpuMC + 25,
		CPURequestP99MC:   cpuMC + 30,
		CPUUsageP50MC:     cpuMC / 4,
		CPUUsageP60MC:     cpuMC / 4,
		CPUUsageP95MC:     cpuMC / 3,
		CPUUsageP98MC:     cpuMC / 3,
		CPUUsageP99MC:     cpuMC / 3,
		CPUUsageMaxMC:     cpuMC / 2,
		MemRequestP50KiB:  memKiB,
		MemRequestP60KiB:  memKiB,
		MemRequestP95KiB:  memKiB + 1024,
		MemRequestP98KiB:  memKiB + 1536,
		MemRequestP99KiB:  memKiB + 2048,
		MemUsageP50KiB:    memKiB / 2,
		MemUsageP60KiB:    memKiB / 2,
		MemUsageP95KiB:    memKiB * 2 / 3,
		MemUsageP98KiB:    memKiB * 2 / 3,
		MemUsageP99KiB:    memKiB * 2 / 3,
		MemUsageMaxKiB:    memKiB,
		CPUUsageMeanMC:    cpuMC / 4,
		MemUsageMeanKiB:   memKiB / 2,
		SampleCount:       96,
		PodCountMin:       1,
		PodCountMax:       1,
		PodCountAvg:       1,
		DesiredReplicas:   1,
		AvailableReplicas: 1,
	}
}

func syntheticKeyedDigests(containers, days int) []KeyedDigest {
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	rows := make([]KeyedDigest, 0, containers*days)
	for c := 0; c < containers; c++ {
		key := ContainerKey{
			Namespace:     "ns",
			Workload:      fmt.Sprintf("wl-%d", c),
			WorkloadType:  "deployment",
			ContainerName: "app",
		}
		for d := 0; d < days; d++ {
			rows = append(rows, KeyedDigest{
				Key: key,
				Row: syntheticDigestRow(start.AddDate(0, 0, d), 200, 524288),
			})
		}
	}
	return rows
}

func TestRecommendWorkloads_NoPool(t *testing.T) {
	rows := syntheticKeyedDigests(1, 14)
	now := rows[len(rows)-1].Row.BucketDate
	cfg := DefaultEngineConfig("org-1", "cluster-1", now)
	ctx := context.Background()

	var recs []ContainerRec
	err := RecommendWorkloads(ctx, rows, cfg, func(batch []ContainerRec) error {
		recs = append(recs, batch...)
		return nil
	})
	require.NoError(t, err)
	require.Len(t, recs, 6, "1 container × 3 terms × 2 engines")

	for _, r := range recs {
		assert.Equal(t, "org-1", r.OrgID)
		assert.Equal(t, "cluster-1", r.ClusterUUID)
		assert.Equal(t, "ns", r.Namespace)
		assert.Equal(t, "wl-0", r.Workload)
		assert.Equal(t, "app", r.ContainerName)
		assert.Greater(t, r.RecCPURequestMC, int64(0))
		assert.Greater(t, r.RecMemRequestKiB, int64(0))
		assert.GreaterOrEqual(t, r.RecCPULimitMC, r.RecCPURequestMC)
		assert.GreaterOrEqual(t, r.RecMemLimitKiB, r.RecMemRequestKiB)
		assert.Nil(t, r.EstimatedSavingsCents, "Apply* is a separate call")
	}
}

func TestRecommendWorkloads_EmptyAndNilEmit(t *testing.T) {
	cfg := DefaultEngineConfig("org", "cluster", time.Date(2026, 3, 14, 0, 0, 0, 0, time.UTC))
	ctx := context.Background()

	err := RecommendWorkloads(ctx, nil, cfg, func([]ContainerRec) error {
		t.Fatal("emit must not run on empty rows")
		return nil
	})
	require.NoError(t, err)

	err = RecommendWorkloads(ctx, syntheticKeyedDigests(1, 1), cfg, nil)
	require.NoError(t, err)
}

func TestRecommendWorkloads_TwoContainers(t *testing.T) {
	rows := syntheticKeyedDigests(2, 14)
	now := rows[len(rows)-1].Row.BucketDate
	cfg := DefaultEngineConfig("org", "cluster", now)
	cfg.BatchSize = 1

	var emits int
	var recs []ContainerRec
	err := RecommendWorkloads(context.Background(), rows, cfg, func(batch []ContainerRec) error {
		emits++
		recs = append(recs, batch...)
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 2, emits, "BatchSize 1 must emit once per container")
	assert.Len(t, recs, 12)
}

func TestRecommendWorkloads_DoesNotCallTimeNow(t *testing.T) {
	rows := syntheticKeyedDigests(1, 14)
	last := rows[len(rows)-1].Row.BucketDate
	cfg := DefaultEngineConfig("org", "cluster", last.Add(72*time.Hour))
	cfg.StalenessThreshold = time.Hour
	cfg.ClusterLastReported = time.Time{}

	var recs []ContainerRec
	err := RecommendWorkloads(context.Background(), rows, cfg, func(batch []ContainerRec) error {
		recs = append(recs, batch...)
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, recs)
	for _, r := range recs {
		assert.True(t, r.Stale, "stale uses deposited Now vs digest date, not wall clock")
	}
}

// crashLoopKeyedDigests builds two adversarial fixtures with distinct
// request/limit values so incidental equality cannot green the gate test:
//   - churn: 400mc / 1GiB requests, 5 restarts on the latest day (every term
//     window covers the latest day, so all three terms see sum 5 >= 3).
//   - control: 200mc / 512MiB requests, zero restarts (must downsize normally).
func crashLoopKeyedDigests() []KeyedDigest {
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	const days = 14
	rows := make([]KeyedDigest, 0, 2*days)
	churnKey := ContainerKey{Namespace: "ns", Workload: "churn", WorkloadType: "deployment", ContainerName: "app"}
	controlKey := ContainerKey{Namespace: "ns", Workload: "control", WorkloadType: "deployment", ContainerName: "app"}
	for d := 0; d < days; d++ {
		churnRow := syntheticDigestRow(start.AddDate(0, 0, d), 400, 1048576)
		if d == days-1 {
			churnRow.RestartCountSum = 5
		}
		rows = append(rows, KeyedDigest{Key: churnKey, Row: churnRow})
	}
	for d := 0; d < days; d++ {
		rows = append(rows, KeyedDigest{Key: controlKey, Row: syntheticDigestRow(start.AddDate(0, 0, d), 200, 524288)})
	}
	return rows
}

func TestRecommendWorkloads_CrashLoopWithholdsCostDownsize(t *testing.T) {
	rows := crashLoopKeyedDigests()
	now := rows[len(rows)-1].Row.BucketDate
	cfg := DefaultEngineConfig("org", "cluster", now)

	var recs []ContainerRec
	err := RecommendWorkloads(context.Background(), rows, cfg, func(batch []ContainerRec) error {
		recs = append(recs, batch...)
		return nil
	})
	require.NoError(t, err)
	require.Len(t, recs, 12, "2 containers × 3 terms × 2 engines")

	byKey := make(map[string]ContainerRec, len(recs))
	for _, r := range recs {
		byKey[r.Workload+"/"+r.Term+"/"+r.Engine] = r
	}

	for _, term := range []string{"short", "medium", "long"} {
		churn := byKey["churn/"+term+"/cost"]
		require.Equal(t, "cost", churn.Engine)
		assert.Equal(t, int64(5), churn.RestartCountSum, "term %s window must see the latest-day restarts", term)
		// Gate: cost downsize withheld — rec pinned at current, savings zeroed.
		assert.Equal(t, int64(400), churn.RecCPURequestMC, "term %s cost CPU request held at current", term)
		assert.Equal(t, int64(420), churn.RecCPULimitMC, "term %s cost CPU limit held at current", term)
		assert.Equal(t, int64(1048576), churn.RecMemRequestKiB, "term %s cost mem request held at current", term)
		assert.Equal(t, int64(1049600), churn.RecMemLimitKiB, "term %s cost mem limit held at current", term)
		require.NotNil(t, churn.EstimatedSavingsCents, "term %s savings must be zeroed, not nil", term)
		assert.Equal(t, int64(0), *churn.EstimatedSavingsCents, "term %s", term)
		require.NotNil(t, churn.EstimatedCPUSavingsCents, "term %s", term)
		assert.Equal(t, int64(0), *churn.EstimatedCPUSavingsCents, "term %s", term)
		require.NotNil(t, churn.EstimatedMemSavingsCents, "term %s", term)
		assert.Equal(t, int64(0), *churn.EstimatedMemSavingsCents, "term %s", term)
		assert.Contains(t, churn.NotificationCodes, types.NotifUnstableContainer, "term %s", term)

		// Control downsizes normally on cost with no unstable code.
		control := byKey["control/"+term+"/cost"]
		assert.Less(t, control.RecCPURequestMC, int64(200), "term %s control must downsize", term)
		assert.Less(t, control.RecMemRequestKiB, int64(524288), "term %s control must downsize", term)
		assert.NotContains(t, control.NotificationCodes, types.NotifUnstableContainer, "term %s", term)

		// Performance engine is untouched by the gate: still downsizes, still coded.
		perf := byKey["churn/"+term+"/performance"]
		assert.Less(t, perf.RecCPURequestMC, int64(400), "term %s perf must still downsize", term)
		assert.Contains(t, perf.NotificationCodes, types.NotifUnstableContainer, "term %s", term)
	}
}

// upsizeLoopKeyedDigests builds one adversarial fixture with values distinct
// from the downsize gate fixtures so incidental equality cannot green it:
// tiny requests (100mc / 128MiB) with sustained usage far above request, plus
// 5 restarts on the latest day (every term window covers the latest day, so
// all three terms see sum 5 >= 3). The cost engine signal is an UPSIZE.
func upsizeLoopKeyedDigests() []KeyedDigest {
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	const days = 14
	rows := make([]KeyedDigest, 0, days)
	key := ContainerKey{Namespace: "ns", Workload: "upsize-churn", WorkloadType: "deployment", ContainerName: "app"}
	for d := 0; d < days; d++ {
		row := syntheticDigestRow(start.AddDate(0, 0, d), 100, 131072)
		// Sustained hot usage well above request so the cost engine
		// recommends an upsize, not a downsize.
		row.CPUUsageP50MC = 800
		row.CPUUsageP60MC = 800
		row.CPUUsageP95MC = 900
		row.CPUUsageP98MC = 950
		row.CPUUsageP99MC = 950
		row.CPUUsageMaxMC = 1000
		row.CPUUsageMeanMC = 800
		row.MemUsageP50KiB = 1048576
		row.MemUsageP60KiB = 1048576
		row.MemUsageP95KiB = 1179648
		row.MemUsageP98KiB = 1179648
		row.MemUsageP99KiB = 1179648
		row.MemUsageMaxKiB = 1310720
		row.MemUsageMeanKiB = 1048576
		if d == days-1 {
			row.RestartCountSum = 5
		}
		rows = append(rows, KeyedDigest{Key: key, Row: row})
	}
	return rows
}

func TestRecommendWorkloads_CrashLoopUpsizePassthrough(t *testing.T) {
	rows := upsizeLoopKeyedDigests()
	now := rows[len(rows)-1].Row.BucketDate
	cfg := DefaultEngineConfig("org", "cluster", now)

	var recs []ContainerRec
	err := RecommendWorkloads(context.Background(), rows, cfg, func(batch []ContainerRec) error {
		recs = append(recs, batch...)
		return nil
	})
	require.NoError(t, err)
	require.Len(t, recs, 6, "1 container × 3 terms × 2 engines")

	byKey := make(map[string]ContainerRec, len(recs))
	for _, r := range recs {
		byKey[r.Workload+"/"+r.Term+"/"+r.Engine] = r
	}

	for _, term := range []string{"short", "medium", "long"} {
		cost := byKey["upsize-churn/"+term+"/cost"]
		require.Equal(t, "cost", cost.Engine)
		assert.Equal(t, int64(5), cost.RestartCountSum, "term %s window must see the latest-day restarts", term)
		// Gate must not touch upsizes: cost rec stays above current, and
		// savings stay nil (the gate zeroes them only when it withholds).
		assert.Greater(t, cost.RecCPURequestMC, cost.CurrentCPURequestMC, "term %s cost CPU upsize must pass through", term)
		assert.Greater(t, cost.RecMemRequestKiB, cost.CurrentMemRequestKiB, "term %s cost mem upsize must pass through", term)
		assert.Nil(t, cost.EstimatedSavingsCents, "term %s gate must not zero savings on passthrough", term)
		assert.Contains(t, cost.NotificationCodes, types.NotifUnstableContainer, "term %s", term)
	}
}
