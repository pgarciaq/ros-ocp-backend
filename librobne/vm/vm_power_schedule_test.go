package vm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func vmDigestIdleDay() Digest {
	const cpuP95 = 10         // all fixtures share one idle CPU shape
	const memP95 = 100 * 1024 // all fixtures share one idle memory shape
	return Digest{CPUUsageP95MC: cpuP95, MemUsageP95KiB: memP95}
}

func vmDigestActiveDay() Digest {
	return Digest{CPUUsageP95MC: 500, MemUsageP95KiB: 1024 * 1024}
}

func TestDetectPowerOffCandidate_MostlyIdleWithActiveDays(t *testing.T) {
	cfg := DefaultVMRecConfig()
	cfg.PowerOffMinIdleDays = 14
	cfg.PowerOffIdleRatioThreshold = 0.7

	digests := make([]Digest, 0, 20)
	for i := 0; i < 16; i++ {
		digests = append(digests, vmDigestIdleDay())
	}
	for i := 0; i < 4; i++ {
		digests = append(digests, vmDigestActiveDay())
	}

	ok, mult := DetectPowerOffCandidate(digests, cfg)
	require.True(t, ok)
	require.NotNil(t, mult)
	assert.InDelta(t, 0.8, *mult, 1e-9)
}

func TestDetectPowerOffCandidate_AllIdleDays_NotCandidate(t *testing.T) {
	cfg := DefaultVMRecConfig()
	cfg.PowerOffMinIdleDays = 14

	digests := make([]Digest, 14)
	for i := range digests {
		digests[i] = vmDigestIdleDay()
	}

	ok, mult := DetectPowerOffCandidate(digests, cfg)
	assert.False(t, ok)
	assert.Nil(t, mult)
}

func TestDetectPowerOffCandidate_BelowIdleRatioThreshold(t *testing.T) {
	cfg := DefaultVMRecConfig()
	cfg.PowerOffMinIdleDays = 14
	cfg.PowerOffIdleRatioThreshold = 0.7

	digests := make([]Digest, 0, 20)
	for i := 0; i < 10; i++ {
		digests = append(digests, vmDigestIdleDay())
	}
	for i := 0; i < 10; i++ {
		digests = append(digests, vmDigestActiveDay())
	}

	ok, mult := DetectPowerOffCandidate(digests, cfg)
	assert.False(t, ok)
	assert.Nil(t, mult)
}

func TestDetectPowerOffCandidate_InsufficientHistory(t *testing.T) {
	cfg := DefaultVMRecConfig()
	cfg.PowerOffMinIdleDays = 14

	digests := make([]Digest, 10)
	for i := range digests {
		digests[i] = vmDigestIdleDay()
	}

	ok, mult := DetectPowerOffCandidate(digests, cfg)
	assert.False(t, ok)
	assert.Nil(t, mult)
}

func TestDetectPowerOffCandidate_FeatureDisabled(t *testing.T) {
	cfg := DefaultVMRecConfig()
	cfg.EnablePowerSchedule = false
	cfg.PowerOffMinIdleDays = 14

	digests := make([]Digest, 0, 20)
	for i := 0; i < 16; i++ {
		digests = append(digests, vmDigestIdleDay())
	}
	for i := 0; i < 4; i++ {
		digests = append(digests, vmDigestActiveDay())
	}

	ok, mult := DetectPowerOffCandidate(digests, cfg)
	assert.False(t, ok)
	assert.Nil(t, mult)
}

func TestPowerOffIdleRatioBasisPoints(t *testing.T) {
	assert.Equal(t, int32(7000), PowerOffIdleRatioBasisPoints(0.7))
	assert.Equal(t, int32(70), PowerOffIdlePercentFromBasisPoints(7000))
}
