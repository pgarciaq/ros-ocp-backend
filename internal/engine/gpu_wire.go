package engine

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/gpu"
)

func init() {
	gpu.ResolveGPUThresholdSettings = resolveGPUThresholdSettingsImpl
	gpu.LoadGPUIdleConfig = loadGPUIdleConfigImpl
}

func resolveGPUThresholdSettingsImpl(ctx context.Context, pool *pgxpool.Pool, orgID string) (gpu.GPUThresholdSettings, error) {
	return ResolveThresholdCached(ctx, pool, orgID, "gpu", resolveGPUThresholdSettingsUncached)
}

func loadGPUIdleConfigImpl(ctx context.Context, pool *pgxpool.Pool, orgID string) gpu.GPUIdleConfig {
	settings, err := resolveIdleDetectionSettings(ctx, pool, orgID)
	if err != nil {
		cfg := config.GetConfig()
		out := gpu.GPUIdleConfig{
			Enabled:            true,
			IdleSMActiveBP:     500,
			IdleDRAMActiveBP:   500,
			ZombieSMActiveBP:   gpu.GPUZombieThresholdBP,
			ZombieDRAMActiveBP: gpu.GPUZombieThresholdBP,
			MinObservationDays: 7,
		}
		if cfg != nil {
			out.Enabled = cfg.IdleDetectionEnabled
			if cfg.IdleGPUSMActiveBP > 0 {
				out.IdleSMActiveBP = cfg.IdleGPUSMActiveBP
			}
			if cfg.IdleGPUDRAMActiveBP > 0 {
				out.IdleDRAMActiveBP = cfg.IdleGPUDRAMActiveBP
			}
		}
		return out
	}
	return gpuIdleConfigFromSettings(settings)
}

// InitGPUEngine copies GPU recommendation thresholds from the central config.
// Call once after config load (e.g. from cmd/start.go or StartAPIServer).
// Note: vm.InitVMRecDefaults must be called separately by the caller to avoid import cycles.
func InitGPUEngine(cfg *config.Config) {
	if cfg == nil {
		return
	}
	InitThresholdDefaults(cfg)
	gpu.SetDefaultGPUThresholdSettings(defaultGPUThresholdSettings)
}
