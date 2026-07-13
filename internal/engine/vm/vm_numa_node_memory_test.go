package vm

import (
	"testing"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveNUMANodeMemoryGiB_FromNodeDigest(t *testing.T) {
	cfg := DefaultVMRecConfig()
	cfg.NUMAAssumedSockets = 2
	nodeMem := map[string]float64{
		"worker-1": 512,
	}
	assert.InDelta(t, 256, resolveNUMANodeMemoryGiB("worker-1", nodeMem, cfg), 1e-9)
	assert.NotNil(t, CheckNUMAFit(300, resolveNUMANodeMemoryGiB("worker-1", nodeMem, cfg)))
	assert.Nil(t, CheckNUMAFit(200, resolveNUMANodeMemoryGiB("worker-1", nodeMem, cfg)))
}

func TestResolveNUMANodeMemoryGiB_FallbackNoNodeData(t *testing.T) {
	cfg := DefaultVMRecConfig()
	cfg.NUMANodeMemoryGiB = 64
	assert.InDelta(t, 64, resolveNUMANodeMemoryGiB("unknown-node", nil, cfg), 1e-9)
	assert.NotNil(t, CheckNUMAFit(128, resolveNUMANodeMemoryGiB("unknown-node", nil, cfg)))
}

func TestResolveNUMANodeMemoryGiB_CustomSocketCount(t *testing.T) {
	cfg := DefaultVMRecConfig()
	cfg.NUMAAssumedSockets = 4
	nodeMem := map[string]float64{"n1": 512}
	assert.InDelta(t, 128, resolveNUMANodeMemoryGiB("n1", nodeMem, cfg), 1e-9)
}

func TestBuildNodeMemoryGiBMap_LatestDigest(t *testing.T) {
	older := int64(100 * kibPerGiB)
	newer := int64(512 * kibPerGiB)
	rows := []engine.NodeDigestRow{
		{BucketDate: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), Node: "n1", MaxMemAllocKiB: &older},
		{BucketDate: time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC), Node: "n1", MaxMemAllocKiB: &newer},
	}
	m := buildNodeMemoryGiBMap(rows)
	require.Len(t, m, 1)
	assert.InDelta(t, 512, m["n1"], 1e-9)
}
