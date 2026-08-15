package gpu

import (
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
)

// ApplyGPUIdleWasteCents sets EstimatedWasteCents on the idle result when the GPU
// is idle or zombie and a monthly GPU rate is available (full gpu_cost_per_month).
func ApplyGPUIdleWasteCents(result *core.IdleResult, gpuMonthlyRateUSD float64) {
	if result == nil || result.State == core.IdleStateActive || gpuMonthlyRateUSD <= 0 {
		return
	}
	result.WasteCents = money.USDToCents(gpuMonthlyRateUSD)
}
