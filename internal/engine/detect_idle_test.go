package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDetectIdle_AllBelowThreshold_ReturnsTrue(t *testing.T) {
	rows := []DigestRow{
		{BucketDate: time.Now(), CPUUsageMaxMC: 5, MemUsageMaxKiB: 1000},
		{BucketDate: time.Now(), CPUUsageMaxMC: 8, MemUsageMaxKiB: 5000},
		{BucketDate: time.Now(), CPUUsageMaxMC: 3, MemUsageMaxKiB: 2000},
	}
	assert.True(t, DetectIdle(rows, 10, 10240))
}

func TestDetectIdle_CPUAboveThreshold_ReturnsFalse(t *testing.T) {
	rows := []DigestRow{
		{BucketDate: time.Now(), CPUUsageMaxMC: 5, MemUsageMaxKiB: 1000},
		{BucketDate: time.Now(), CPUUsageMaxMC: 15, MemUsageMaxKiB: 1000},
		{BucketDate: time.Now(), CPUUsageMaxMC: 3, MemUsageMaxKiB: 1000},
	}
	assert.False(t, DetectIdle(rows, 10, 10240))
}

func TestDetectIdle_MemAboveThreshold_ReturnsFalse(t *testing.T) {
	rows := []DigestRow{
		{BucketDate: time.Now(), CPUUsageMaxMC: 5, MemUsageMaxKiB: 1000},
		{BucketDate: time.Now(), CPUUsageMaxMC: 3, MemUsageMaxKiB: 20000},
	}
	assert.False(t, DetectIdle(rows, 10, 10240))
}

func TestDetectIdle_ExactlyAtThreshold_ReturnsFalse(t *testing.T) {
	rows := []DigestRow{
		{BucketDate: time.Now(), CPUUsageMaxMC: 10, MemUsageMaxKiB: 1000},
	}
	assert.False(t, DetectIdle(rows, 10, 10240))
}

func TestDetectIdle_MemExactlyAtThreshold_ReturnsFalse(t *testing.T) {
	rows := []DigestRow{
		{BucketDate: time.Now(), CPUUsageMaxMC: 5, MemUsageMaxKiB: 10240},
	}
	assert.False(t, DetectIdle(rows, 10, 10240))
}

func TestDetectIdle_Empty_ReturnsFalse(t *testing.T) {
	assert.False(t, DetectIdle(nil, 10, 10240))
}

func TestDetectIdle_ZeroUsage_ReturnsTrue(t *testing.T) {
	rows := []DigestRow{
		{BucketDate: time.Now(), CPUUsageMaxMC: 0, MemUsageMaxKiB: 0},
		{BucketDate: time.Now(), CPUUsageMaxMC: 0, MemUsageMaxKiB: 0},
	}
	assert.True(t, DetectIdle(rows, 10, 10240))
}

// DetectAbandoned tests

func TestDetectAbandoned_AllZero_ReturnsTrue(t *testing.T) {
	rows := []DigestRow{
		{BucketDate: time.Now(), CPUUsageMaxMC: 0, MemUsageMaxKiB: 0},
		{BucketDate: time.Now(), CPUUsageMaxMC: 0, MemUsageMaxKiB: 0},
	}
	assert.True(t, DetectAbandoned(rows))
}

func TestDetectAbandoned_SomeCPU_ReturnsFalse(t *testing.T) {
	rows := []DigestRow{
		{BucketDate: time.Now(), CPUUsageMaxMC: 1, MemUsageMaxKiB: 0},
		{BucketDate: time.Now(), CPUUsageMaxMC: 0, MemUsageMaxKiB: 0},
	}
	assert.False(t, DetectAbandoned(rows))
}

func TestDetectAbandoned_SomeMem_ReturnsFalse(t *testing.T) {
	rows := []DigestRow{
		{BucketDate: time.Now(), CPUUsageMaxMC: 0, MemUsageMaxKiB: 100},
		{BucketDate: time.Now(), CPUUsageMaxMC: 0, MemUsageMaxKiB: 0},
	}
	assert.False(t, DetectAbandoned(rows))
}

func TestDetectAbandoned_Empty_ReturnsFalse(t *testing.T) {
	assert.False(t, DetectAbandoned(nil))
}
