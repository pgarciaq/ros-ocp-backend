package vm

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

func TestClassifyIOPattern_LowIO(t *testing.T) {
	iops := int64(40)
	bps := int64(40 * 65536)
	digests := vmIODigestDays(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), 2, iops, bps, 0, 0)
	assert.Equal(t, VMIOPatternLowIO, ClassifyIOPattern(digests, DefaultVMRecConfig()))
}

func TestClassifyIOPattern_Sequential(t *testing.T) {
	// 128 KiB per operation: high throughput relative to IOPS
	readIOPS := int64(500)
	readBPS := int64(500 * 131072)
	digests := vmIODigestDays(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), 2, readIOPS, readBPS, 0, 0)
	assert.Equal(t, VMIOPatternSequential, ClassifyIOPattern(digests, DefaultVMRecConfig()))
}

func TestClassifyIOPattern_Random(t *testing.T) {
	// 4 KiB per operation: high IOPS relative to throughput
	readIOPS := int64(8000)
	readBPS := int64(8000 * 4096)
	digests := vmIODigestDays(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), 2, readIOPS, readBPS, 0, 0)
	assert.Equal(t, VMIOPatternRandom, ClassifyIOPattern(digests, DefaultVMRecConfig()))
}

func TestClassifyIOPattern_Mixed(t *testing.T) {
	readIOPS := int64(2000)
	readBPS := int64(2000 * 32768) // 32 KiB average
	digests := vmIODigestDays(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), 2, readIOPS, readBPS, 0, 0)
	assert.Equal(t, VMIOPatternMixed, ClassifyIOPattern(digests, DefaultVMRecConfig()))
}

func TestClassifyIOPattern_NoMetrics(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 2, func(d *model.DailyVMDigest) {
		d.CPUUsageP95MC = 1000
	})
	assert.Equal(t, "", ClassifyIOPattern(digests, DefaultVMRecConfig()))
}

func TestClassifyIOPattern_CustomThresholds(t *testing.T) {
	cfg := DefaultVMRecConfig()
	cfg.IOSequentialThresholdBytes = 100000
	cfg.IORandomThresholdBytes = 50000
	cfg.IOMinIOPSForClassification = 50

	readIOPS := int64(200)
	readBPS := int64(200 * 120000)
	digests := vmIODigestDays(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), 1, readIOPS, readBPS, 0, 0)
	assert.Equal(t, VMIOPatternSequential, ClassifyIOPattern(digests, cfg))
}

func TestVMRecommend_HighIOStillFiresWithSequentialPattern(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	readIOPS := int64(6000)
	readBPS := int64(6000 * 131072) // sequential + above default high IOPS threshold
	digests := vmIODigestDays(base, 3, readIOPS, readBPS, 0, 0)
	for i := range digests {
		digests[i].CPUUsageP95MC = 3000
	}

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, VMIOPatternSequential, rec.IOPattern)
	require.NotNil(t, rec.IOHint)
	assert.Equal(t, vmIOHintHigh, *rec.IOHint)

	notifs := vmUnmarshalNotifications(t, rec.Notifications)
	require.NotNil(t, vmHasNotificationCode(notifs, NotifVMHighIO))
	require.NotNil(t, vmHasNotificationCode(notifs, NotifVMIOSequential))
	assert.Nil(t, vmHasNotificationCode(notifs, NotifVMIORandom))
}

func TestVMRecommend_SequentialAndRandomNotifications(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	seqIOPS := int64(2000)
	seqBPS := int64(2000 * 65536)
	seqDigests := vmIODigestDays(base, 3, seqIOPS, seqBPS, 0, 0)
	for i := range seqDigests {
		seqDigests[i].CPUUsageP95MC = 3000
	}
	seqRec, err := RecommendVM(seqDigests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, seqRec)
	assert.Equal(t, VMIOPatternSequential, seqRec.IOPattern)
	seqNotifs := vmUnmarshalNotifications(t, seqRec.Notifications)
	require.NotNil(t, vmHasNotificationCode(seqNotifs, NotifVMIOSequential))

	randIOPS := int64(5000)
	randBPS := int64(5000 * 4096)
	randDigests := vmIODigestDays(base, 3, randIOPS, randBPS, 0, 0)
	for i := range randDigests {
		randDigests[i].CPUUsageP95MC = 3000
	}
	randRec, err := RecommendVM(randDigests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, randRec)
	assert.Equal(t, VMIOPatternRandom, randRec.IOPattern)
	randNotifs := vmUnmarshalNotifications(t, randRec.Notifications)
	require.NotNil(t, vmHasNotificationCode(randNotifs, NotifVMIORandom))
}

func vmIODigestDays(base time.Time, n int, readIOPS, readBPS, writeIOPS, writeBPS int64) []model.DailyVMDigest {
	return vmDigestDays(base, n, func(d *model.DailyVMDigest) {
		if readIOPS > 0 {
			v := readIOPS
			d.DiskReadIOPSP95 = &v
		}
		if readBPS > 0 {
			v := readBPS
			d.DiskReadBPS95 = &v
		}
		if writeIOPS > 0 {
			v := writeIOPS
			d.DiskWriteIOPSP95 = &v
		}
		if writeBPS > 0 {
			v := writeBPS
			d.DiskWriteBPS95 = &v
		}
	})
}
