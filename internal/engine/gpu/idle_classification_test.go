package gpu

import (
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
	"github.com/stretchr/testify/assert"
)

func TestApplyGPUIdleWasteCents_FullGPURate(t *testing.T) {
	result := core.IdleResult{State: core.IdleStateIdle}
	ApplyGPUIdleWasteCents(&result, 600.0)
	assert.Equal(t, int64(60000), result.WasteCents)
}

func TestApplyGPUIdleWasteCents_ActiveNoWaste(t *testing.T) {
	result := core.IdleResult{State: core.IdleStateActive}
	ApplyGPUIdleWasteCents(&result, 600.0)
	assert.Equal(t, int64(0), result.WasteCents)
}
