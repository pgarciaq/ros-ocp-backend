package vm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func vmDigestWithDiskIO(readIOPS, writeIOPS, readBPS, writeBPS int64) Digest {
	return Digest{
		DiskReadIOPSP95:  &readIOPS,
		DiskWriteIOPSP95: &writeIOPS,
		DiskReadBPS95:    &readBPS,
		DiskWriteBPS95:   &writeBPS,
	}
}

func vmLowIODigest() Digest {
	return vmDigestWithDiskIO(10, 10, 1000, 1000)
}

func vmRandomHighIOPSDigest() Digest {
	return vmDigestWithDiskIO(2600, 2600, 500, 500)
}

func vmSequentialHighThroughputDigest() Digest {
	return vmDigestWithDiskIO(500, 500, 60_000_000, 60_000_000)
}

func TestEvaluateStorageTiering_ColdStorage(t *testing.T) {
	cfg := DefaultVMRecConfig()
	digests := make([]Digest, 14)
	for i := range digests {
		digests[i] = vmLowIODigest()
	}
	notifs := EvaluateStorageTiering(digests, cfg)
	require.Len(t, notifs, 1)
	assert.Equal(t, NotifVMStorageTierCold, notifs[0].Code)
}

func TestEvaluateStorageTiering_IOPSOptimized(t *testing.T) {
	cfg := DefaultVMRecConfig()
	digests := make([]Digest, 7)
	for i := range digests {
		digests[i] = vmRandomHighIOPSDigest()
	}
	notifs := EvaluateStorageTiering(digests, cfg)
	require.Len(t, notifs, 1)
	assert.Equal(t, NotifVMStorageTierIOPS, notifs[0].Code)
}

func TestEvaluateStorageTiering_ThroughputOptimized(t *testing.T) {
	cfg := DefaultVMRecConfig()
	digests := make([]Digest, 7)
	for i := range digests {
		digests[i] = vmSequentialHighThroughputDigest()
	}
	notifs := EvaluateStorageTiering(digests, cfg)
	require.Len(t, notifs, 1)
	assert.Equal(t, NotifVMStorageTierThroughput, notifs[0].Code)
}

func TestEvaluateStorageTiering_MixedPatterns_NoNotifications(t *testing.T) {
	cfg := DefaultVMRecConfig()
	digests := []Digest{
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
	digests := make([]Digest, 5)
	for i := range digests {
		digests[i] = vmLowIODigest()
	}
	notifs := EvaluateStorageTiering(digests, cfg)
	assert.Empty(t, notifs)
}

func TestEvaluateStorageTiering_Disabled(t *testing.T) {
	cfg := DefaultVMRecConfig()
	cfg.StorageTieringEnabled = false
	digests := make([]Digest, 14)
	for i := range digests {
		digests[i] = vmLowIODigest()
	}
	notifs := EvaluateStorageTiering(digests, cfg)
	assert.Empty(t, notifs)
}
