package gpu

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// GPUThresholdSettings holds GPU classification, confidence, and time-slicing parameters.
type GPUThresholdSettings struct {
	GPUThresholds
	ComputeBoundDRAMThreshold    float64 `json:"compute_bound_dram_threshold"`
	ComputeBoundDRAMThresholdBP  int32   `json:"-"`
	MIGFBPercentile              float64 `json:"mig_fb_percentile"`
	ConfidenceDaysTier1          int     `json:"confidence_days_tier1"`
	ConfidenceDaysTier2          int     `json:"confidence_days_tier2"`
	ConfidenceDaysTier3          int     `json:"confidence_days_tier3"`
	SpikeRatioThreshold          float64 `json:"spike_ratio_threshold"`
	SpikeConfidencePenalty       float64 `json:"spike_confidence_penalty"`
	NoProfilingConfidenceFactor  float64 `json:"no_profiling_confidence_factor"`
	TimeslicingMajorityThreshold float64 `json:"timeslicing_majority_threshold"`
	TimeslicingMinReplicas       int     `json:"timeslicing_min_replicas"`
	TimeslicingMaxReplicas       int     `json:"timeslicing_max_replicas"`
	TimeslicingBasePenalty       float64 `json:"timeslicing_base_penalty"`
	TimeslicingImpactedWeight    float64 `json:"timeslicing_impacted_weight"`
	NodeFreshnessDays            int     `json:"node_freshness_days"`
}

// DefaultGPUThresholdSettings returns compiled defaults for GPU recommendations.
func DefaultGPUThresholdSettings() GPUThresholdSettings {
	s := GPUThresholdSettings{
		GPUThresholds:                DefaultGPUThresholds(),
		ComputeBoundDRAMThreshold:    0.30,
		MIGFBPercentile:              0.98,
		ConfidenceDaysTier1:          3,
		ConfidenceDaysTier2:          7,
		ConfidenceDaysTier3:          14,
		SpikeRatioThreshold:          5.0,
		SpikeConfidencePenalty:       0.70,
		NoProfilingConfidenceFactor:  0.50,
		TimeslicingMajorityThreshold: 0.50,
		TimeslicingMinReplicas:       2,
		TimeslicingMaxReplicas:       8,
		TimeslicingBasePenalty:       0.70,
		TimeslicingImpactedWeight:    0.30,
		NodeFreshnessDays:            7,
	}
	NormalizeGPUThresholdSettings(&s)
	return s
}

// NormalizeGPUThresholdSettings precomputes basis-point fields for GPU classification.
func NormalizeGPUThresholdSettings(s *GPUThresholdSettings) {
	if s == nil {
		return
	}
	normalizeGPUThresholds(&s.GPUThresholds)
	s.ComputeBoundDRAMThresholdBP = ThresholdToBasisPoints(s.ComputeBoundDRAMThreshold)
}

// defaultGPUThresholdSettings is the process-wide default GPU threshold settings.
var defaultGPUThresholdSettings = DefaultGPUThresholdSettings()

// SetDefaultGPUThresholdSettings updates the process-wide GPU threshold settings
// and refreshes derived state. Called by the engine package after applying env locks.
func SetDefaultGPUThresholdSettings(s GPUThresholdSettings) {
	defaultGPUThresholdSettings = s
	defaultThresholds = s.GPUThresholds
}

// ResolveGPUThresholdSettings resolves org-specific GPU threshold settings.
// This is a function variable wired by the engine package to break the circular
// import between engine and gpu (the implementation uses shared threshold
// resolution infrastructure in the engine package).
var ResolveGPUThresholdSettings func(ctx context.Context, pool *pgxpool.Pool, orgID string) (GPUThresholdSettings, error)

// resolveGPUThresholdSettingsDefault is the fallback used when the function
// variable ResolveGPUThresholdSettings has not been wired by the engine package
// (e.g. in unit tests that import gpu directly without importing root engine).
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
