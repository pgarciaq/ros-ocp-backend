package gpu

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ResolveGPUThresholdSettings resolves org-specific GPU threshold settings.
// This is a function variable wired by the engine package to break the circular
// import between engine and gpu (the implementation uses shared threshold
// resolution infrastructure in the engine package).
var ResolveGPUThresholdSettings func(ctx context.Context, pool *pgxpool.Pool, orgID string) (GPUThresholdSettings, error)

func resolveGPUThresholdSettingsDefault(_ context.Context, _ *pgxpool.Pool, _ string) (GPUThresholdSettings, error) {
	return DefaultGPUThresholdSettings(), nil
}

func init() {
	if ResolveGPUThresholdSettings == nil {
		ResolveGPUThresholdSettings = resolveGPUThresholdSettingsDefault
	}
}

// LoadGPUIdleConfig resolves GPU idle thresholds using the shared 3-tier model.
// Wired by the engine package to break circular imports.
var LoadGPUIdleConfig func(ctx context.Context, pool *pgxpool.Pool, orgID string) GPUIdleConfig

func loadGPUIdleConfigDefault(_ context.Context, _ *pgxpool.Pool, _ string) GPUIdleConfig {
	return DefaultGPUIdleConfig()
}

func init() {
	if LoadGPUIdleConfig == nil {
		LoadGPUIdleConfig = loadGPUIdleConfigDefault
	}
}
