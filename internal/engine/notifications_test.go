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
	rec := ContainerRec{DataDays: 7, IsIdle: true, ConfidenceLevel: 0.8}
	codes := EvaluateNotifications(rec, 3)
	assert.Contains(t, codes, NotifIdleWorkload)
}

func TestEvaluateNotifications_MemoryTrendingUp(t *testing.T) {
	rec := ContainerRec{DataDays: 7, TrendSlope: 500.0, ConfidenceLevel: 0.8}
	codes := EvaluateNotifications(rec, 3)
	assert.Contains(t, codes, NotifMemoryTrendingUp)
}

func TestEvaluateNotifications_HealthyWorkload_NoCodes(t *testing.T) {
	rec := ContainerRec{
		DataDays:        7,
		OOMCountSum:     0,
		IsIdle:          false,
		TrendSlope:      0,
		ConfidenceLevel: 0.9,
	}
	codes := EvaluateNotifications(rec, 3)
	assert.Empty(t, codes)
}

func TestEvaluateNotifications_MultipleCodes(t *testing.T) {
	rec := ContainerRec{
		DataDays:        1,
		OOMCountSum:     2,
		IsIdle:          true,
		TrendSlope:      0,
		ConfidenceLevel: 0.2,
	}
	codes := EvaluateNotifications(rec, 3)
	assert.Contains(t, codes, NotifLowConfidence)
	assert.Contains(t, codes, NotifOOMDetected)
	assert.Contains(t, codes, NotifIdleWorkload)
	assert.True(t, len(codes) >= 3)
}
