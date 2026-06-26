package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func digestDay(day int, cpuP95, cpuMax, memP95, memMax int64) DigestRow {
	return DigestRow{
		BucketDate:    time.Date(2026, 1, day, 0, 0, 0, 0, time.UTC),
		CPUUsageP95MC: cpuP95,
		CPUUsageMaxMC: cpuMax,
		MemUsageP95KiB: memP95,
		MemUsageMaxKiB: memMax,
	}
}

func observationRows(n int, cpuP95, cpuMax, memP95, memMax int64) []DigestRow {
	rows := make([]DigestRow, n)
	for i := range rows {
		rows[i] = digestDay(i+1, cpuP95, cpuMax, memP95, memMax)
	}
	return rows
}

func TestClassifyIdleState_ActiveNormalUtilization(t *testing.T) {
	cfg := DefaultIdleConfig()
	rows := observationRows(14, 500, 600, 2048, 4096)
	result := ClassifyIdleState(rows, 1000, 8192, "Deployment", "app", cfg)
	assert.Equal(t, IdleStateActive, result.State)
	assert.Nil(t, result.IdleSince)
}

func TestClassifyIdleState_ZombieNearZeroCPU(t *testing.T) {
	cfg := DefaultIdleConfig()
	rows := observationRows(14, 0, 5, 0, 0)
	result := ClassifyIdleState(rows, 1000, 8192, "Deployment", "app", cfg)
	assert.Equal(t, IdleStateZombie, result.State)
	assert.NotNil(t, result.IdleSince)
}

func TestClassifyIdleState_IdleLowUtilizationRelativeToRequest(t *testing.T) {
	cfg := DefaultIdleConfig()
	rows := observationRows(14, 10, 15, 40, 50)
	result := ClassifyIdleState(rows, 1000, 8192, "Deployment", "app", cfg)
	assert.Equal(t, IdleStateIdle, result.State)
}

func TestClassifyIdleState_BurstyWorkloadActive(t *testing.T) {
	cfg := DefaultIdleConfig()
	rows := observationRows(14, 10, 200, 40, 50)
	result := ClassifyIdleState(rows, 1000, 8192, "Deployment", "app", cfg)
	assert.Equal(t, IdleStateActive, result.State)
}

func TestClassifyIdleState_DaemonSetExcluded(t *testing.T) {
	cfg := DefaultIdleConfig()
	rows := observationRows(14, 0, 0, 0, 0)
	result := ClassifyIdleState(rows, 1000, 8192, "daemonset", "app", cfg)
	assert.Equal(t, IdleStateActive, result.State)

	// Case-insensitive: PascalCase should also be excluded (e.g. user-configured exclusion).
	result = ClassifyIdleState(rows, 1000, 8192, "DaemonSet", "app", cfg)
	assert.Equal(t, IdleStateActive, result.State)
}

func TestClassifyIdleState_ExcludedNamespace(t *testing.T) {
	cfg := DefaultIdleConfig()
	rows := observationRows(14, 0, 0, 0, 0)
	result := ClassifyIdleState(rows, 1000, 8192, "Deployment", "kube-system", cfg)
	assert.Equal(t, IdleStateActive, result.State)

	result = ClassifyIdleState(rows, 1000, 8192, "Deployment", "openshift-monitoring", cfg)
	assert.Equal(t, IdleStateActive, result.State)
}

func TestClassifyIdleState_InsufficientObservationDays(t *testing.T) {
	cfg := DefaultIdleConfig()
	rows := observationRows(5, 0, 0, 0, 0)
	result := ClassifyIdleState(rows, 1000, 8192, "Deployment", "app", cfg)
	assert.Equal(t, IdleStateActive, result.State)
}

func TestClassifyIdleState_EmptyRows(t *testing.T) {
	cfg := DefaultIdleConfig()
	result := ClassifyIdleState(nil, 1000, 8192, "Deployment", "app", cfg)
	assert.Equal(t, IdleStateActive, result.State)
}

func TestClassifyIdleState_ZeroRequests(t *testing.T) {
	cfg := DefaultIdleConfig()
	rows := observationRows(14, 10, 15, 40, 50)
	result := ClassifyIdleState(rows, 0, 0, "Deployment", "app", cfg)
	assert.Equal(t, IdleStateActive, result.State)
}

func TestClassifyIdleState_Disabled(t *testing.T) {
	cfg := DefaultIdleConfig()
	cfg.Enabled = false
	rows := observationRows(14, 0, 0, 0, 0)
	result := ClassifyIdleState(rows, 1000, 8192, "Deployment", "app", cfg)
	assert.Equal(t, IdleStateActive, result.State)
}

func TestIsExcludedNamespace_Glob(t *testing.T) {
	assert.True(t, isExcludedNamespace("openshift-monitoring", []string{"openshift-*"}))
	assert.False(t, isExcludedNamespace("app-prod", []string{"openshift-*"}))
}

func TestComputeIdleDuration(t *testing.T) {
	since := time.Now().UTC().AddDate(0, 0, -10)
	assert.Equal(t, 10, computeIdleDuration(&since))
	assert.Equal(t, 0, computeIdleDuration(nil))
}

func TestMaxDailyCPUUsageP95_ReturnsMaxNotWindowP95(t *testing.T) {
	rows := make([]DigestRow, 20)
	for i := range rows {
		rows[i] = digestDay(i+1, 50, 60, 2048, 4096)
	}
	rows[0] = digestDay(1, 800, 900, 2048, 4096)
	assert.Equal(t, int64(800), maxDailyCPUUsageP95(rows))
}

func TestMaxDailyMemUsageP95_ReturnsMaxAcrossDays(t *testing.T) {
	rows := []DigestRow{
		digestDay(1, 50, 60, 100, 120),
		digestDay(2, 50, 60, 500, 520),
	}
	assert.Equal(t, int64(500), maxDailyMemUsageP95(rows))
}

func TestMaxDailyCPUUsageP95_ConservativeUpperBoundForIdle(t *testing.T) {
	cfg := DefaultIdleConfig()
	rows := make([]DigestRow, 20)
	for i := range rows {
		rows[i] = digestDay(i+1, 0, 5, 0, 0)
	}
	rows[0] = digestDay(1, 500, 600, 2048, 4096)
	result := ClassifyIdleState(rows, 1000, 8192, "Deployment", "app", cfg)
	assert.Equal(t, IdleStateActive, result.State, "early spike raises max daily P95 above zombie threshold")
}

func TestFindIdleSince_ConsecutiveTail(t *testing.T) {
	rows := observationRows(5, 0, 0, 0, 0)
	rows[0] = digestDay(1, 500, 600, 2048, 4096)
	since := findIdleSince(rows, func(r DigestRow) bool {
		return r.CPUUsageMaxMC < 10
	})
	assert.NotNil(t, since)
	assert.Equal(t, 2, since.Day())
}
