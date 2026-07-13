package vm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

func vmDigestWithDiskIO(readIOPS, writeIOPS, readBPS, writeBPS int64) model.DailyVMDigest {
	return model.DailyVMDigest{
		DiskReadIOPSP95:  &readIOPS,
		DiskWriteIOPSP95: &writeIOPS,
		DiskReadBPS95:    &readBPS,
		DiskWriteBPS95:   &writeBPS,
	}
}

func vmLowIODigest() model.DailyVMDigest {
	return vmDigestWithDiskIO(10, 10, 1000, 1000)
}

func vmRandomHighIOPSDigest() model.DailyVMDigest {
	return vmDigestWithDiskIO(2600, 2600, 500, 500)
}

func vmSequentialHighThroughputDigest() model.DailyVMDigest {
	return vmDigestWithDiskIO(500, 500, 60_000_000, 60_000_000)
}

func TestEvaluateStorageTiering_ColdStorage(t *testing.T) {
	cfg := DefaultVMRecConfig()
	digests := make([]model.DailyVMDigest, 14)
	for i := range digests {
		digests[i] = vmLowIODigest()
	}
	notifs := EvaluateStorageTiering(digests, cfg)
	require.Len(t, notifs, 1)
	assert.Equal(t, NotifVMStorageTierCold, notifs[0].Code)
}

func TestEvaluateStorageTiering_IOPSOptimized(t *testing.T) {
	cfg := DefaultVMRecConfig()
	digests := make([]model.DailyVMDigest, 7)
	for i := range digests {
		digests[i] = vmRandomHighIOPSDigest()
	}
	notifs := EvaluateStorageTiering(digests, cfg)
	require.Len(t, notifs, 1)
	assert.Equal(t, NotifVMStorageTierIOPS, notifs[0].Code)
}

func TestEvaluateStorageTiering_ThroughputOptimized(t *testing.T) {
	cfg := DefaultVMRecConfig()
	digests := make([]model.DailyVMDigest, 7)
	for i := range digests {
		digests[i] = vmSequentialHighThroughputDigest()
	}
	notifs := EvaluateStorageTiering(digests, cfg)
	require.Len(t, notifs, 1)
	assert.Equal(t, NotifVMStorageTierThroughput, notifs[0].Code)
}

func TestEvaluateStorageTiering_MixedPatterns_NoNotifications(t *testing.T) {
	cfg := DefaultVMRecConfig()
	digests := []model.DailyVMDigest{
		vmLowIODigest(),
		vmLowIODigest(),
		vmLowIODigest(),
		vmRandomHighIOPSDigest(),
		vmRandomHighIOPSDigest(),
		vmRandomHighIOPSDigest(),
		vmSequentialHighThroughputDigest(),
		vmSequentialHighThroughputDigest(),
		vmSequentialHighThroughputDigest(),
	}
	notifs := EvaluateStorageTiering(digests, cfg)
	assert.Empty(t, notifs)
}

func TestEvaluateStorageTiering_InsufficientHistory(t *testing.T) {
	cfg := DefaultVMRecConfig()
	digests := make([]model.DailyVMDigest, 5)
	for i := range digests {
		digests[i] = vmLowIODigest()
	}
	notifs := EvaluateStorageTiering(digests, cfg)
	assert.Empty(t, notifs)
}

func TestEvaluateStorageTiering_Disabled(t *testing.T) {
	cfg := DefaultVMRecConfig()
	cfg.StorageTieringEnabled = false
	digests := make([]model.DailyVMDigest, 14)
	for i := range digests {
		digests[i] = vmLowIODigest()
	}
	notifs := EvaluateStorageTiering(digests, cfg)
	assert.Empty(t, notifs)
}
