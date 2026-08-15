package node

import (
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
	"github.com/stretchr/testify/assert"
)

func defaultNodeIdleSettings() ThresholdSettings {
	s := DefaultThresholdSettings()
	s.ZombieCPUP95MC = 200
	s.ZombieMaxPods = 5
	s.IdleCPUUtilPct = 10
	s.IdleMemUtilPct = 10
	s.IdleMaxPods = 10
	return s
}

func TestClassifyNodeIdleState_ZombieLowCPUFewPods(t *testing.T) {
	class := nodeClassification{
		validDays:        3,
		maxCPUUsageP95MC: 100,
		PodCount:         2,
	}
	assert.Equal(t, types.IdleStateZombie, ClassifyNodeIdleState(class, defaultNodeIdleSettings()))
}

func TestClassifyNodeIdleState_IdleLowUtilModeratePods(t *testing.T) {
	class := nodeClassification{
		validDays:        3,
		maxCPUUsageP95MC: 800,
		CPUUtilP95:       0.08,
		MemUtilP95:       0.07,
		PodCount:         8,
	}
	assert.Equal(t, types.IdleStateIdle, ClassifyNodeIdleState(class, defaultNodeIdleSettings()))
}

func TestClassifyNodeIdleState_ActiveUnderutilizedNotIdle(t *testing.T) {
	class := nodeClassification{
		validDays:        3,
		maxCPUUsageP95MC: 4000,
		CPUUtilP95:       0.25,
		MemUtilP95:       0.20,
		PodCount:         8,
	}
	assert.Equal(t, types.IdleStateActive, ClassifyNodeIdleState(class, defaultNodeIdleSettings()))
}

func TestClassifyNodeIdleState_ActiveLowUtilTooManyPods(t *testing.T) {
	class := nodeClassification{
		validDays:        3,
		maxCPUUsageP95MC: 500,
		CPUUtilP95:       0.05,
		MemUtilP95:       0.04,
		PodCount:         50,
	}
	assert.Equal(t, types.IdleStateActive, ClassifyNodeIdleState(class, defaultNodeIdleSettings()))
}

func TestClassifyNodeIdleState_ZombieAlsoUnderutilized(t *testing.T) {
	class := nodeClassification{
		validDays:        3,
		maxCPUUsageP95MC: 100,
		CPUUtilP95:       0.01,
		MemUtilP95:       0.01,
		PodCount:         2,
		IsUnderutilized:  true,
	}
	state := ClassifyNodeIdleState(class, defaultNodeIdleSettings())
	assert.Equal(t, types.IdleStateZombie, state)
}
