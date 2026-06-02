package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

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
