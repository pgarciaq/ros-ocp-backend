package namespace

import (
	"context"
	"testing"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func syntheticDigestRow(day time.Time, cpuMC, memKiB int64) types.DigestRow {
	return types.DigestRow{
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

func TestRecommendNamespaces_NoPool(t *testing.T) {
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	rows := make([]types.DigestRow, 14)
	for i := range rows {
		rows[i] = syntheticDigestRow(start.AddDate(0, 0, i), 200, 524288)
	}
	grouped := map[NamespaceKey][]types.DigestRow{
		{Namespace: "payments"}: rows,
	}
	now := rows[len(rows)-1].BucketDate
	cfg := DefaultNamespaceEngineConfig("org-1", "cluster-1", now, now)

	recs, err := RecommendNamespaces(context.Background(), grouped, cfg)
	require.NoError(t, err)
	require.Len(t, recs, 6, "1 namespace × 3 terms × 2 engines")
	for _, r := range recs {
		assert.Equal(t, "org-1", r.OrgID)
		assert.Equal(t, "cluster-1", r.ClusterUUID)
		assert.Equal(t, "payments", r.Namespace)
		assert.Equal(t, ScheduleAllHours, r.ScheduleType)
		assert.Greater(t, r.RecCPURequestMC, int64(0))
		assert.Greater(t, r.RecMemRequestKiB, int64(0))
		assert.Nil(t, r.EstimatedSavingsCents, "ApplyNamespaceSavingsEstimates is a separate call")
	}
}

func TestRecommendNamespaces_EmptyAndAllow(t *testing.T) {
	cfg := DefaultNamespaceEngineConfig("org", "cluster", time.Date(2026, 3, 14, 0, 0, 0, 0, time.UTC), time.Date(2026, 3, 14, 0, 0, 0, 0, time.UTC))
	recs, err := RecommendNamespaces(context.Background(), nil, cfg)
	require.NoError(t, err)
	assert.Empty(t, recs)

	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	row := syntheticDigestRow(start, 200, 524288)
	grouped := map[NamespaceKey][]types.DigestRow{
		{Namespace: "skip-me"}: {row},
		{Namespace: "keep-me"}: make([]types.DigestRow, 14),
	}
	for i := range grouped[NamespaceKey{Namespace: "keep-me"}] {
		grouped[NamespaceKey{Namespace: "keep-me"}][i] = syntheticDigestRow(start.AddDate(0, 0, i), 200, 524288)
	}
	cfg.Now = start.AddDate(0, 0, 13)
	cfg.End = cfg.Now
	cfg.NamespaceAllow = func(ns string) bool { return ns == "keep-me" }
	recs, err = RecommendNamespaces(context.Background(), grouped, cfg)
	require.NoError(t, err)
	require.NotEmpty(t, recs)
	for _, r := range recs {
		assert.Equal(t, "keep-me", r.Namespace)
	}
}

func TestRecommendNamespaces_DoesNotCallTimeNow(t *testing.T) {
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	rows := make([]types.DigestRow, 14)
	for i := range rows {
		rows[i] = syntheticDigestRow(start.AddDate(0, 0, i), 200, 524288)
	}
	last := rows[len(rows)-1].BucketDate
	cfg := DefaultNamespaceEngineConfig("org", "cluster", last.Add(72*time.Hour), last)
	cfg.StalenessThreshold = time.Hour
	cfg.ClusterLastReported = time.Time{}
	grouped := map[NamespaceKey][]types.DigestRow{{Namespace: "ns"}: rows}

	recs, err := RecommendNamespaces(context.Background(), grouped, cfg)
	require.NoError(t, err)
	require.NotEmpty(t, recs)
	for _, r := range recs {
		assert.True(t, r.Stale, "stale uses deposited Now vs digest date, not wall clock")
	}
}
