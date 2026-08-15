package gpu

import (
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
	libgpu "github.com/redhatinsights/ros-ocp-backend/librobne/gpu"
)

// GPUThresholdsFromConfig constructs GPUThresholds from the application Config.
func GPUThresholdsFromConfig(cfg *config.Config) GPUThresholds {
	if cfg == nil {
		return DefaultGPUThresholds()
	}
	th := GPUThresholds{
		IdleThreshold:       cfg.GPUIdleThreshold,
		UnderutilizedSM:     cfg.GPUUnderutilizedSMThreshold,
		UnderutilizedTensor: cfg.GPUUnderutilizedTensorThreshold,
		MemBoundDRAM:        cfg.GPUMemBoundDRAMThreshold,
		MemBoundTensor:      cfg.GPUMemBoundTensorThreshold,
		FBHeadroomFactor:    cfg.GPUFBHeadroomFactor,
	}
	NormalizeGPUThresholds(&th)
	return th
}

// RecommendGPU produces a GPU recommendation for a container given its daily GPU digests.
// Returns nil if no GPU data is present.
func RecommendGPU(digests []GPUDigestRow) *GPURec {
	if len(digests) > 0 {
		_ = MatchGPUModel(digests[0].GPUModelName)
	}
	return libgpu.RecommendGPU(digests)
}

// RecommendGPUWithSettings produces a GPU recommendation using explicit threshold settings.
func RecommendGPUWithSettings(digests []GPUDigestRow, settings GPUThresholdSettings, idleCfg ...GPUIdleConfig) *GPURec {
	if len(digests) > 0 {
		_ = MatchGPUModel(digests[0].GPUModelName)
	}
	return libgpu.RecommendGPUWithSettings(digests, settings, idleCfg...)
}

// ComputeNodeTimeslicingRecWithSettings produces a time-slicing recommendation using explicit settings.
func ComputeNodeTimeslicingRecWithSettings(group NodeGPUGroup, gpuRate *float32, now time.Time, settings GPUThresholdSettings) *TimeslicingRec {
	_ = MatchGPUModel(group.GPUModel)
	return libgpu.ComputeNodeTimeslicingRecWithSettings(group, gpuRate, now, settings)
}

// ApplyGPUSavings computes the GPU savings estimate using the gpu_cost_per_month
// rate from the cost model. Modifies rec in-place.
//
// Savings logic:
//   - idle: full GPU rate (could remove the GPU entirely)
//   - MIG right-sized: (1 - recommended_slices/total_slices) * rate
//   - well_utilized / no recommendation: $0
//   - no cost data: nil (no estimate available)
func ApplyGPUSavings(rec *GPURec, costData *costdata.ClusterCostData) {
	if rec == nil {
		return
	}
	if costData == nil {
		return
	}

	gpuRateMicroCents := core.RateMicroCentsPerDollarMonth(GPUMonthlyRate(costData))
	if gpuRateMicroCents == 0 {
		return
	}

	var savingsMicroCents int64

	switch rec.Classification {
	case GPUClassIdle:
		savingsMicroCents = gpuRateMicroCents
	case GPUClassUnderutilized, GPUClassComputeBoundUnderutil, GPUClassMemoryBound:
		if rec.RecommendedGPUProfile != "" && rec.RecommendedGPUProfile != "full_gpu" {
			spec := MatchGPUModel(rec.GPUModelName)
			if spec != nil {
				totalSlices := int64(MigTotalSlices(spec))
				recSlices := int64(MigProfileSlices(spec, rec.RecommendedGPUProfile))
				savingsMicroCents = core.MIGFractionSavingsMicroCents(gpuRateMicroCents, totalSlices, recSlices)
			}
		}
	}

	cents := core.MicroCentsToCents(savingsMicroCents)
	rec.EstimatedGPUSavingsCents = &cents
}

// ComputeGPUSavingsCents returns monthly GPU savings in cents, or nil when cost data is unavailable.
func ComputeGPUSavingsCents(rec *GPURec, costData *costdata.ClusterCostData) *int64 {
	if rec == nil {
		return nil
	}
	clone := *rec
	clone.EstimatedGPUSavingsCents = nil
	ApplyGPUSavings(&clone, costData)
	return clone.EstimatedGPUSavingsCents
}

// GPUMonthlyRate extracts the GPU monthly cost rate (infrastructure +
// supplementary) from Koku cost data. Returns 0 if unavailable.
func GPUMonthlyRate(costData *costdata.ClusterCostData) float64 {
	if costData == nil || costData.ConfiguredRates == nil {
		return 0
	}
	rp, ok := costData.ConfiguredRates["gpu_cost_per_month"]
	if !ok {
		return 0
	}
	return rp.Infrastructure + rp.Supplementary
}
