package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestClassifyIdleState_EarlyZombie_AllZeroUsage(t *testing.T) {
	cfg := DefaultIdleConfig()
	rows := []DigestRow{
		{BucketDate: time.Now().AddDate(0, 0, -2), CPUUsageMaxMC: 0, MemUsageMaxKiB: 0},
		{BucketDate: time.Now().AddDate(0, 0, -1), CPUUsageMaxMC: 0, MemUsageMaxKiB: 0},
	}
	result := ClassifyIdleState(rows, 100, 1024, "Deployment", "myapp", cfg)
	assert.Equal(t, IdleStateZombie, result.State)
	assert.NotNil(t, result.IdleSince)
}

func TestClassifyIdleState_EarlyZombie_SingleDay(t *testing.T) {
	cfg := DefaultIdleConfig()
	rows := []DigestRow{
		{BucketDate: time.Now(), CPUUsageMaxMC: 0, MemUsageMaxKiB: 0},
	}
	result := ClassifyIdleState(rows, 100, 1024, "Deployment", "myapp", cfg)
	assert.Equal(t, IdleStateZombie, result.State, "single day zero usage should be zombie")
}

func TestClassifyIdleState_EarlyZombie_NotTriggeredWithCPU(t *testing.T) {
	cfg := DefaultIdleConfig()
	rows := []DigestRow{
		{BucketDate: time.Now().AddDate(0, 0, -1), CPUUsageMaxMC: 1, MemUsageMaxKiB: 0},
		{BucketDate: time.Now(), CPUUsageMaxMC: 0, MemUsageMaxKiB: 0},
	}
	result := ClassifyIdleState(rows, 100, 1024, "Deployment", "myapp", cfg)
	assert.Equal(t, IdleStateActive, result.State, "non-zero CPU means not early zombie")
}

func TestClassifyIdleState_EarlyZombie_NotTriggeredWithMem(t *testing.T) {
	cfg := DefaultIdleConfig()
	rows := []DigestRow{
		{BucketDate: time.Now().AddDate(0, 0, -1), CPUUsageMaxMC: 0, MemUsageMaxKiB: 100},
		{BucketDate: time.Now(), CPUUsageMaxMC: 0, MemUsageMaxKiB: 0},
	}
	result := ClassifyIdleState(rows, 100, 1024, "Deployment", "myapp", cfg)
	assert.Equal(t, IdleStateActive, result.State, "non-zero mem means not early zombie")
}

func TestClassifyIdleState_EarlyZombie_ExcludedNamespace(t *testing.T) {
	cfg := DefaultIdleConfig()
	rows := []DigestRow{
		{BucketDate: time.Now(), CPUUsageMaxMC: 0, MemUsageMaxKiB: 0},
	}
	result := ClassifyIdleState(rows, 100, 1024, "Deployment", "kube-system", cfg)
	assert.Equal(t, IdleStateActive, result.State, "excluded namespace skips early zombie")
}

func TestIdleClassificationAuthoritative_EarlyZombie(t *testing.T) {
	cfg := DefaultIdleConfig()
	rows := []DigestRow{
		{BucketDate: time.Now(), CPUUsageMaxMC: 0, MemUsageMaxKiB: 0},
	}
	assert.True(t, idleClassificationAuthoritative(cfg, "Deployment", "myapp", rows),
		"all-zero usage should be authoritative regardless of observation days")
}

func TestIdleClassificationAuthoritative_InsufficientDataNonZero(t *testing.T) {
	cfg := DefaultIdleConfig()
	rows := []DigestRow{
		{BucketDate: time.Now(), CPUUsageMaxMC: 5, MemUsageMaxKiB: 100},
	}
	assert.False(t, idleClassificationAuthoritative(cfg, "Deployment", "myapp", rows),
		"non-zero usage with insufficient data should not be authoritative")
}

func TestAllZeroUsage(t *testing.T) {
	assert.True(t, allZeroUsage([]DigestRow{
		{CPUUsageMaxMC: 0, MemUsageMaxKiB: 0},
		{CPUUsageMaxMC: 0, MemUsageMaxKiB: 0},
	}))
	assert.False(t, allZeroUsage([]DigestRow{
		{CPUUsageMaxMC: 1, MemUsageMaxKiB: 0},
	}))
	assert.False(t, allZeroUsage([]DigestRow{
		{CPUUsageMaxMC: 0, MemUsageMaxKiB: 1},
	}))
	assert.False(t, allZeroUsage(nil))
}
