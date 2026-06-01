package ingestion

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func vmSampleRow(start time.Time, vm, ns string, cpuUsage float64, memUsage float64, extras func(*VMRow)) VMRow {
	r := VMRow{
		IntervalStart:      start,
		IntervalEnd:        start.Add(15 * time.Minute),
		VMName:             vm,
		Namespace:          ns,
		NodeName:           "node-1",
		GuestOS:            "linux",
		CPUUsageMC:         cpuUsage,
		CPURequestMC:       4000,
		CPULimitMC:         8000,
		MemoryUsageKiB:     memUsage,
		MemoryRequestKiB:   8 * 1024 * 1024,
		DiskAllocatedBytes: 100 * 1024 * 1024 * 1024,
	}
	if extras != nil {
		extras(&r)
	}
	return r
}

func TestVMDigestBuilder_SingleVMSingleDayMultipleSamples(t *testing.T) {
	base := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	rows := []VMRow{
		vmSampleRow(base, "app-vm", "prod", 100, 1000, nil),
		vmSampleRow(base.Add(15*time.Minute), "app-vm", "prod", 200, 2000, nil),
		vmSampleRow(base.Add(30*time.Minute), "app-vm", "prod", 300, 3000, nil),
	}

	digests := BuildDailyVMDigests(rows)
	require.Len(t, digests, 1)

	var d VMDigestResult
	for _, v := range digests {
		d = v
	}
	assert.Equal(t, int64(200), d.CPUUsageP50MC)
	assert.Equal(t, int64(300), d.CPUUsageP95MC)
	assert.Equal(t, int64(300), d.CPUUsageP99MC)
	assert.Equal(t, int64(300), d.CPUUsageMaxMC)
	assert.Equal(t, int32(3), d.SampleCount)
}

func TestVMDigestBuilder_MultipleVMsSingleDay(t *testing.T) {
	base := time.Date(2026, 5, 10, 8, 0, 0, 0, time.UTC)
	rows := []VMRow{
		vmSampleRow(base, "vm-a", "ns1", 50, 500, nil),
		vmSampleRow(base, "vm-b", "ns1", 150, 1500, nil),
	}
	digests := BuildDailyVMDigests(rows)
	require.Len(t, digests, 2)

	keyA := VMDigestKey{VMName: "vm-a", Namespace: "ns1", BucketDate: time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)}
	keyB := VMDigestKey{VMName: "vm-b", Namespace: "ns1", BucketDate: time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)}
	assert.Contains(t, digests, keyA)
	assert.Contains(t, digests, keyB)
	assert.Equal(t, int64(50), digests[keyA].CPUUsageP50MC)
	assert.Equal(t, int64(150), digests[keyB].CPUUsageP50MC)
}

func TestVMDigestBuilder_SingleVMMultipleDays(t *testing.T) {
	day1 := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC)
	rows := []VMRow{
		vmSampleRow(day1, "vm1", "ns", 100, 1000, nil),
		vmSampleRow(day2, "vm1", "ns", 500, 5000, nil),
	}
	digests := BuildDailyVMDigests(rows)
	require.Len(t, digests, 2)
}

func TestVMDigestBuilder_MinAgentSamplesForPercentile(t *testing.T) {
	base := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	rows := make([]VMRow, 0, 19)
	for i := 0; i < 19; i++ {
		mem := 100.0 + float64(i)
		rows = append(rows, vmSampleRow(base.Add(time.Duration(i)*15*time.Minute), "ga-vm", "ns", 20, 2000, func(r *VMRow) {
			r.MemoryAvailableKiB = &mem
		}))
	}
	digests := BuildDailyVMDigests(rows)
	require.Len(t, digests, 1)
	var d VMDigestResult
	for _, v := range digests {
		d = v
	}
	assert.Equal(t, int32(19), d.AgentSampleCount)
	assert.Nil(t, d.MemAvailableP50KiB)
	assert.Nil(t, d.MemAvailableP95KiB)
}

func TestVMDigestBuilder_GuestAgentFieldsPresent(t *testing.T) {
	base := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	rows := make([]VMRow, 0, 20)
	for i := 0; i < 20; i++ {
		mem := 100.0 + float64(i)*10
		rows = append(rows, vmSampleRow(base.Add(time.Duration(i)*15*time.Minute), "ga-vm", "ns", 10+float64(i), 1000, func(r *VMRow) {
			r.MemoryAvailableKiB = &mem
		}))
	}
	digests := BuildDailyVMDigests(rows)
	require.Len(t, digests, 1)
	var d VMDigestResult
	for _, v := range digests {
		d = v
	}
	require.NotNil(t, d.MemAvailableP50KiB)
	require.NotNil(t, d.MemAvailableP95KiB)
	assert.Equal(t, int32(20), d.AgentSampleCount)
	assert.NotNil(t, d.MemAvailableP50KiB)
	assert.NotNil(t, d.MemAvailableP95KiB)
}

func TestVMDigestBuilder_NoGuestAgentFields(t *testing.T) {
	base := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	rows := []VMRow{vmSampleRow(base, "no-ga", "ns", 10, 1000, nil)}
	digests := BuildDailyVMDigests(rows)
	require.Len(t, digests, 1)
	var d VMDigestResult
	for _, v := range digests {
		d = v
	}
	assert.Nil(t, d.MemAvailableP50KiB)
	assert.Nil(t, d.MemAvailableP95KiB)
}

func TestVMDigestBuilder_SingleSamplePercentilesEqual(t *testing.T) {
	base := time.Date(2026, 5, 10, 6, 0, 0, 0, time.UTC)
	rows := []VMRow{vmSampleRow(base, "solo", "ns", 777, 8888, nil)}
	digests := BuildDailyVMDigests(rows)
	require.Len(t, digests, 1)
	var d VMDigestResult
	for _, v := range digests {
		d = v
	}
	assert.Equal(t, int64(777), d.CPUUsageP50MC)
	assert.Equal(t, int64(777), d.CPUUsageP95MC)
	assert.Equal(t, int64(777), d.CPUUsageP99MC)
	assert.Equal(t, int64(777), d.CPUUsageMaxMC)
	assert.Equal(t, int64(8888), d.MemUsageP50KiB)
	assert.Equal(t, int64(8888), d.MemUsageP95KiB)
	assert.Equal(t, int32(1), d.SampleCount)
}

func TestVMDigestBuilder_RestartCountSum(t *testing.T) {
	base := time.Date(2026, 5, 10, 6, 0, 0, 0, time.UTC)
	r1 := int32(1)
	r2 := int32(2)
	rows := []VMRow{
		vmSampleRow(base, "vm", "ns", 100, 1024, nil),
		vmSampleRow(base.Add(15*time.Minute), "vm", "ns", 100, 1024, nil),
	}
	rows[0].RestartCount = &r1
	rows[1].RestartCount = &r2
	digests := BuildDailyVMDigests(rows)
	require.Len(t, digests, 1)
	var d VMDigestResult
	for _, v := range digests {
		d = v
	}
	assert.Equal(t, int32(3), d.RestartCountSum)
}

func vmSampleRowWithGPU(start time.Time, vm, ns, gpuUUID string, utilAvg float64) VMRow {
	gpuCount := int32(1)
	model := "A100"
	return vmSampleRow(start, vm, ns, 100, 1024, func(r *VMRow) {
		r.GPUCount = &gpuCount
		r.GPUUUID = &gpuUUID
		r.GPUModel = &model
		r.GPUUtilizationAvg = &utilAvg
		max := utilAvg + 0.05
		r.GPUUtilizationMax = &max
	})
}

func TestBuildDailyVMDigests_WithGPUDevices(t *testing.T) {
	base := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	rows := []VMRow{
		vmSampleRowWithGPU(base, "gpu-vm", "ml", "GPU-1111", 0.10),
		vmSampleRowWithGPU(base.Add(15*time.Minute), "gpu-vm", "ml", "GPU-2222", 0.20),
	}
	digests := BuildDailyVMDigests(rows)
	require.Len(t, digests, 1)

	var d VMDigestResult
	for _, v := range digests {
		d = v
	}
	require.Len(t, d.GPUDevices, 2)
	uuids := []string{d.GPUDevices[0].UUID, d.GPUDevices[1].UUID}
	assert.ElementsMatch(t, []string{"GPU-1111", "GPU-2222"}, uuids)
}

func TestBuildDailyVMDigests_GPUDeviceAveraging(t *testing.T) {
	base := time.Date(2026, 5, 10, 8, 0, 0, 0, time.UTC)
	rows := []VMRow{
		vmSampleRowWithGPU(base, "avg-vm", "ns", "GPU-AAAA", 0.10),
		vmSampleRowWithGPU(base.Add(15*time.Minute), "avg-vm", "ns", "GPU-AAAA", 0.30),
	}
	digests := BuildDailyVMDigests(rows)
	require.Len(t, digests, 1)

	var d VMDigestResult
	for _, v := range digests {
		d = v
	}
	require.Len(t, d.GPUDevices, 1)
	// avg(0.10, 0.30) = 0.20 → 2000 basis points
	assert.Equal(t, int32(2000), d.GPUDevices[0].UtilAvgBP)
}

func TestBuildDailyVMDigests_NoGPU_EmptyDevices(t *testing.T) {
	base := time.Date(2026, 5, 10, 6, 0, 0, 0, time.UTC)
	rows := []VMRow{vmSampleRow(base, "plain-vm", "ns", 50, 512, nil)}
	digests := BuildDailyVMDigests(rows)
	require.Len(t, digests, 1)

	var d VMDigestResult
	for _, v := range digests {
		d = v
	}
	assert.Empty(t, d.GPUDevices)
	assert.Equal(t, int32(0), d.GPUCount)
}
