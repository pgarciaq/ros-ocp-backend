package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDetectIdle_AllBelowThreshold_ReturnsTrue(t *testing.T) {
	rows := []DigestRow{
		{BucketDate: time.Now(), CPUUsageMaxMC: 5},
		{BucketDate: time.Now(), CPUUsageMaxMC: 8},
		{BucketDate: time.Now(), CPUUsageMaxMC: 3},
	}
	assert.True(t, DetectIdle(rows, 10))
}

func TestDetectIdle_OneAboveThreshold_ReturnsFalse(t *testing.T) {
	rows := []DigestRow{
		{BucketDate: time.Now(), CPUUsageMaxMC: 5},
		{BucketDate: time.Now(), CPUUsageMaxMC: 15},
		{BucketDate: time.Now(), CPUUsageMaxMC: 3},
	}
	assert.False(t, DetectIdle(rows, 10))
}

func TestDetectIdle_ExactlyAtThreshold_ReturnsFalse(t *testing.T) {
	rows := []DigestRow{
		{BucketDate: time.Now(), CPUUsageMaxMC: 10},
	}
	assert.False(t, DetectIdle(rows, 10))
}

func TestDetectIdle_Empty_ReturnsFalse(t *testing.T) {
	assert.False(t, DetectIdle(nil, 10))
}

func TestDetectIdle_ZeroUsage_ReturnsTrue(t *testing.T) {
	rows := []DigestRow{
		{BucketDate: time.Now(), CPUUsageMaxMC: 0},
		{BucketDate: time.Now(), CPUUsageMaxMC: 0},
	}
	assert.True(t, DetectIdle(rows, 10))
}
