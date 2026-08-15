package node

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

const gibKiB = 1024 * 1024

func TestRecommendedNodeCapacity(t *testing.T) {
	cpu, mem := recommendedNodeCapacity(3500, 8*gibKiB, 0, 0, 0.80)
	assert.Equal(t, int64(5000), cpu)
	assert.Equal(t, int64(10*gibKiB), mem)

	cpuPerf, memPerf := recommendedNodeCapacity(3500, 8*gibKiB, 0, 0, 0.55)
	assert.Greater(t, cpuPerf, cpu)
	assert.Greater(t, memPerf, mem)
}

func TestHasFullSpareNodeHeadroom(t *testing.T) {
	assert.True(t, hasFullSpareNodeHeadroom(16000, 64*gibKiB, 4000, 16*gibKiB, 2.0))
	assert.False(t, hasFullSpareNodeHeadroom(16000, 64*gibKiB, 9000, 32*gibKiB, 2.0))
	assert.False(t, hasFullSpareNodeHeadroom(0, 64*gibKiB, 4000, 16*gibKiB, 2.0))
}
