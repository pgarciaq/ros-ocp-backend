package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEvaluateNotifications_LowConfidence(t *testing.T) {
	rec := ContainerRec{DataDays: 2, ConfidenceLevel: 0.3}
	codes := EvaluateNotifications(rec, 4)
	assert.Contains(t, codes, NotifLowConfidence)
}

func TestEvaluateNotifications_NewWorkload(t *testing.T) {
	rec := ContainerRec{DataDays: 0}
	codes := EvaluateNotifications(rec, 3)
	assert.Contains(t, codes, NotifNewWorkload)
}

func TestEvaluateNotifications_OOMDetected(t *testing.T) {
	rec := ContainerRec{DataDays: 7, OOMCountSum: 3, ConfidenceLevel: 0.8}
	codes := EvaluateNotifications(rec, 3)
	assert.Contains(t, codes, NotifOOMDetected)
}

func TestEvaluateNotifications_IdleWorkload(t *testing.T) {
	rec := ContainerRec{DataDays: 7, IdleState: IdleStateIdle, ConfidenceLevel: 0.8}
	codes := EvaluateNotifications(rec, 3)
	assert.Contains(t, codes, NotifIdleWorkload)
}

func TestEvaluateNotifications_MemoryTrendingUp(t *testing.T) {
	rec := ContainerRec{DataDays: 7, MemTrendSlope: 500.0, ConfidenceLevel: 0.8}
	codes := EvaluateNotifications(rec, 3)
	assert.Contains(t, codes, NotifMemoryTrendingUp)
}

func TestEvaluateNotifications_CPUTrendDoesNotTriggerMemoryNotif(t *testing.T) {
	rec := ContainerRec{DataDays: 7, CPUTrendSlope: 500.0, MemTrendSlope: 0, ConfidenceLevel: 0.8}
	codes := EvaluateNotifications(rec, 3)
	assert.NotContains(t, codes, NotifMemoryTrendingUp)
}

func TestEvaluateNotifications_HealthyWorkload_NoCodes(t *testing.T) {
	rec := ContainerRec{
		DataDays:        7,
		OOMCountSum:     0,
		IdleState:       IdleStateActive,
		CPUTrendSlope:   0,
		MemTrendSlope:   0,
		ConfidenceLevel: 0.9,
	}
	codes := EvaluateNotifications(rec, 3)
	assert.Empty(t, codes)
}

func TestEvaluateNotifications_ZeroCPULimit_NoExtraCodes(t *testing.T) {
	rec := ContainerRec{
		DataDays:        7,
		OOMCountSum:     0,
		IdleState:       IdleStateActive,
		CPUTrendSlope:   0,
		MemTrendSlope:   0,
		ConfidenceLevel: 0.9,
	}
	codes := EvaluateNotifications(rec, 3)
	assert.Empty(t, codes, "healthy workload with zero CPU limit should produce no notification codes")
}

func TestEvaluateNotifications_ZombieGetIdleCode(t *testing.T) {
	rec := ContainerRec{DataDays: 7, IdleState: IdleStateZombie, ConfidenceLevel: 0.8}
	codes := EvaluateNotifications(rec, 3)
	assert.Contains(t, codes, NotifIdleWorkload, "zombie containers should get idle notification code")
}

func TestEvaluateNotifications_IdleNotZombie(t *testing.T) {
	rec := ContainerRec{DataDays: 7, IdleState: IdleStateIdle, ConfidenceLevel: 0.8}
	codes := EvaluateNotifications(rec, 3)
	assert.Contains(t, codes, NotifIdleWorkload)
}

func TestEvaluateNotifications_StaleData(t *testing.T) {
	rec := ContainerRec{DataDays: 7, Stale: true, ConfidenceLevel: 0.8}
	codes := EvaluateNotifications(rec, 3)
	assert.Contains(t, codes, NotifStaleData)
}

func TestEvaluateNotifications_NotStale_NoCode(t *testing.T) {
	rec := ContainerRec{DataDays: 7, Stale: false, ConfidenceLevel: 0.8}
	codes := EvaluateNotifications(rec, 3)
	assert.NotContains(t, codes, NotifStaleData)
}

func TestEvaluateNotifications_MultipleCodes(t *testing.T) {
	rec := ContainerRec{
		DataDays:        1,
		OOMCountSum:     2,
		IdleState:       IdleStateIdle,
		Stale:           true,
		CPUTrendSlope:   0,
		MemTrendSlope:   0,
		ConfidenceLevel: 0.2,
	}
	codes := EvaluateNotifications(rec, 3)
	assert.Contains(t, codes, NotifLowConfidence)
	assert.Contains(t, codes, NotifOOMDetected)
	assert.Contains(t, codes, NotifIdleWorkload)
	assert.Contains(t, codes, NotifStaleData)
	assert.Contains(t, codes, NotifSparseData)
	assert.True(t, len(codes) >= 5)
}

func TestEvaluateNotifications_SparseData(t *testing.T) {
	rec := ContainerRec{DataDays: 1, ConfidenceLevel: 1.0}
	codes := EvaluateNotifications(rec, 1)
	assert.Contains(t, codes, NotifSparseData)
}

func TestEvaluateNotifications_SparseData_ExactThreshold(t *testing.T) {
	rec := ContainerRec{DataDays: 2, ConfidenceLevel: 1.0}
	codes := EvaluateNotifications(rec, 1)
	assert.Contains(t, codes, NotifSparseData, "data_days == threshold should fire")
}

func TestEvaluateNotifications_SparseData_AboveThreshold(t *testing.T) {
	rec := ContainerRec{DataDays: 3, ConfidenceLevel: 1.0}
	codes := EvaluateNotifications(rec, 1)
	assert.NotContains(t, codes, NotifSparseData, "data_days above threshold should not fire")
}

func TestEvaluateNotifications_SparseData_ZeroDays(t *testing.T) {
	rec := ContainerRec{DataDays: 0, ConfidenceLevel: 0}
	codes := EvaluateNotifications(rec, 1)
	assert.NotContains(t, codes, NotifSparseData, "zero data days should not fire SPARSE_DATA (NEW_WORKLOAD fires instead)")
}

func TestEvaluateNotifications_SparseData_OrthogonalToLowConfidence(t *testing.T) {
	// 1 day in a 1-day window: confidence=1.0, so LOW_CONFIDENCE doesn't fire
	// but SPARSE_DATA should fire because absolute data is low
	rec := ContainerRec{DataDays: 1, ConfidenceLevel: 1.0}
	codes := EvaluateNotifications(rec, 1)
	assert.Contains(t, codes, NotifSparseData, "sparse data should fire even with high confidence")
	assert.NotContains(t, codes, NotifLowConfidence, "low confidence should NOT fire with confidence=1.0")
}
