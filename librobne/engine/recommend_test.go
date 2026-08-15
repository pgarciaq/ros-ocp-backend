package engine

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
