package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ThresholdValidationError lists field validation failures for threshold settings updates.
type ThresholdValidationError struct {
	Errors []string
}

func (e *ThresholdValidationError) Error() string {
	if len(e.Errors) == 0 {
		return "threshold settings validation failed"
	}
	msg := e.Errors[0]
	for i := 1; i < len(e.Errors); i++ {
		msg += "; " + e.Errors[i]
	}
	return msg
}

type fieldValidator struct {
	errs []string
}

func (v *fieldValidator) addRangeFloat(field string, val float64, min, max float64) {
	if val < min || val > max {
		v.errs = append(v.errs, fmt.Sprintf("%s must be between %g and %g", field, min, max))
	}
}

func (v *fieldValidator) addRangeFloat32(field string, val float32, min, max float64) {
	v.addRangeFloat(field, float64(val), min, max)
}

func (v *fieldValidator) addRangeInt(field string, val int, min, max int) {
	if val < min || val > max {
		v.errs = append(v.errs, fmt.Sprintf("%s must be between %d and %d", field, min, max))
	}
}

func (v *fieldValidator) addRangeInt64(field string, val int64, min, max int64) {
	if val < min || val > max {
		v.errs = append(v.errs, fmt.Sprintf("%s must be between %d and %d", field, min, max))
	}
}

func (v *fieldValidator) addConstraint(field, message string) {
	v.errs = append(v.errs, fmt.Sprintf("%s: %s", field, message))
}

func (v *fieldValidator) result() error {
	if len(v.errs) == 0 {
		return nil
	}
	return &ThresholdValidationError{Errors: v.errs}
}

// ValidateThresholdSettingsUpdate checks incoming PUT fields against allowed ranges
// and cross-field constraints before locked-field enforcement.
func ValidateThresholdSettingsUpdate(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, recType string,
	rawUpdate json.RawMessage,
) error {
	switch recType {
	case "container", "namespace":
		var update SizingThresholdSettingsUpdate
		if err := json.Unmarshal(rawUpdate, &update); err != nil {
			return fmt.Errorf("invalid request body: %w", err)
		}
		current, err := resolveSizingForValidation(ctx, pool, orgID, recType)
		if err != nil {
			return err
		}
		return validateSizingThresholdUpdate(update, current)
	case "node":
		var update NodeThresholdSettingsUpdate
		if err := json.Unmarshal(rawUpdate, &update); err != nil {
			return fmt.Errorf("invalid request body: %w", err)
		}
		return validateNodeThresholdUpdate(update)
	case "gpu":
		var update GPUThresholdSettingsUpdate
		if err := json.Unmarshal(rawUpdate, &update); err != nil {
			return fmt.Errorf("invalid request body: %w", err)
		}
		current, err := ResolveGPUThresholdSettings(ctx, pool, orgID)
		if err != nil {
			return err
		}
		return validateGPUThresholdUpdate(update, current)
	case "pvc":
		var update PVCThresholdSettingsUpdate
		if err := json.Unmarshal(rawUpdate, &update); err != nil {
			return fmt.Errorf("invalid request body: %w", err)
		}
		current, err := ResolvePVCThresholdSettings(ctx, pool, orgID)
		if err != nil {
			return err
		}
		return validatePVCThresholdUpdate(update, current)
	default:
		return fmt.Errorf("unsupported recommendation_type %q", recType)
	}
}

func resolveSizingForValidation(ctx context.Context, pool *pgxpool.Pool, orgID, recType string) (SizingThresholdSettings, error) {
	switch recType {
	case "container":
		return ResolveContainerSizingThresholds(ctx, pool, orgID)
	case "namespace":
		return ResolveNamespaceSizingThresholds(ctx, pool, orgID)
	default:
		return SizingThresholdSettings{}, fmt.Errorf("unsupported recommendation_type %q", recType)
	}
}

func validateSizingThresholdUpdate(update SizingThresholdSettingsUpdate, current SizingThresholdSettings) error {
	v := fieldValidator{}

	if update.CPUCostPercentile != nil {
		v.addRangeFloat("cpu_cost_percentile", *update.CPUCostPercentile, 0.01, 1.0)
	}
	if update.CPUPerfPercentile != nil {
		v.addRangeFloat("cpu_perf_percentile", *update.CPUPerfPercentile, 0.01, 1.0)
	}
	if update.MemCostPercentile != nil {
		v.addRangeFloat("mem_cost_percentile", *update.MemCostPercentile, 0.01, 1.0)
	}
	if update.MemPerfPercentile != nil {
		v.addRangeFloat("mem_perf_percentile", *update.MemPerfPercentile, 0.01, 1.0)
	}
	if update.MinMargin != nil {
		v.addRangeFloat("min_margin", *update.MinMargin, 1.0, 3.0)
	}
	if update.MaxMargin != nil {
		v.addRangeFloat("max_margin", *update.MaxMargin, 1.0, 3.0)
	}
	if update.LimitMultiplier != nil {
		v.addRangeFloat("limit_multiplier", *update.LimitMultiplier, 1.0, 5.0)
	}
	if update.CPUFloorMC != nil {
		v.addRangeInt64("cpu_floor_mc", *update.CPUFloorMC, 1, 1000)
	}
	if update.IdleCPUThresholdMC != nil {
		v.addRangeInt64("idle_cpu_threshold_mc", *update.IdleCPUThresholdMC, 1, 10000)
	}
	if update.IdleMemThresholdKiB != nil {
		v.addRangeInt64("idle_mem_threshold_kib", *update.IdleMemThresholdKiB, 1, 10485760)
	}
	if update.MemTrendSlopeThreshold != nil {
		v.addRangeFloat("mem_trend_slope_threshold", *update.MemTrendSlopeThreshold, 1.0, 1000000.0)
	}
	if update.LowConfidenceThreshold != nil {
		v.addRangeFloat32("low_confidence_threshold", *update.LowConfidenceThreshold, 0.01, 1.0)
	}

	minMargin := current.MinMargin
	if update.MinMargin != nil {
		minMargin = *update.MinMargin
	}
	maxMargin := current.MaxMargin
	if update.MaxMargin != nil {
		maxMargin = *update.MaxMargin
	}
	if minMargin > maxMargin {
		v.addConstraint("min_margin", "must be less than or equal to max_margin")
	}

	return v.result()
}

func validateNodeThresholdUpdate(update NodeThresholdSettingsUpdate) error {
	v := fieldValidator{}

	if update.UnderutilThreshold != nil {
		v.addRangeFloat("underutil_threshold", *update.UnderutilThreshold, 0.01, 0.99)
	}
	if update.OvercommitThreshold != nil {
		v.addRangeFloat("overcommit_threshold", *update.OvercommitThreshold, 1.0, 10.0)
	}
	if update.AllocatableFactor != nil {
		v.addRangeFloat("allocatable_factor", *update.AllocatableFactor, 0.5, 1.0)
	}
	if update.StrandedImbalanceThreshold != nil {
		v.addRangeFloat("stranded_imbalance_threshold", *update.StrandedImbalanceThreshold, 0.1, 1.0)
	}
	if update.EMAAlpha != nil {
		v.addRangeFloat("ema_alpha", *update.EMAAlpha, 0.01, 1.0)
	}
	if update.CostTargetUtilization != nil {
		v.addRangeFloat("cost_target_utilization", *update.CostTargetUtilization, 0.1, 0.99)
	}
	if update.PerfTargetUtilization != nil {
		v.addRangeFloat("perf_target_utilization", *update.PerfTargetUtilization, 0.1, 0.99)
	}
	if update.PerfConsolidationHeadroomMultiplier != nil {
		v.addRangeFloat("perf_consolidation_headroom_multiplier", *update.PerfConsolidationHeadroomMultiplier, 1.0, 10.0)
	}
	if update.TrendMinDays != nil {
		v.addRangeInt("trend_min_days", *update.TrendMinDays, 1, 30)
	}

	return v.result()
}

func validateGPUThresholdUpdate(update GPUThresholdSettingsUpdate, current GPUThresholdSettings) error {
	v := fieldValidator{}

	if update.IdleThreshold != nil {
		v.addRangeFloat("idle_threshold", *update.IdleThreshold, 0.0, 1.0)
	}
	if update.UnderutilizedSMThreshold != nil {
		v.addRangeFloat("underutilized_sm_threshold", *update.UnderutilizedSMThreshold, 0.0, 1.0)
	}
	if update.UnderutilizedTensorThreshold != nil {
		v.addRangeFloat("underutilized_tensor_threshold", *update.UnderutilizedTensorThreshold, 0.0, 1.0)
	}
	if update.MemBoundDRAMThreshold != nil {
		v.addRangeFloat("membound_dram_threshold", *update.MemBoundDRAMThreshold, 0.0, 1.0)
	}
	if update.MemBoundTensorThreshold != nil {
		v.addRangeFloat("membound_tensor_threshold", *update.MemBoundTensorThreshold, 0.0, 1.0)
	}
	if update.FBHeadroomFactor != nil {
		v.addRangeFloat("fb_headroom_factor", *update.FBHeadroomFactor, 0.0, 1.0)
	}
	if update.ComputeBoundDRAMThreshold != nil {
		v.addRangeFloat("compute_bound_dram_threshold", *update.ComputeBoundDRAMThreshold, 0.0, 1.0)
	}
	if update.MIGFBPercentile != nil {
		v.addRangeFloat("mig_fb_percentile", *update.MIGFBPercentile, 0.0, 1.0)
	}
	if update.ConfidenceDaysTier1 != nil {
		v.addRangeInt("confidence_days_tier1", *update.ConfidenceDaysTier1, 1, 365)
	}
	if update.ConfidenceDaysTier2 != nil {
		v.addRangeInt("confidence_days_tier2", *update.ConfidenceDaysTier2, 1, 365)
	}
	if update.ConfidenceDaysTier3 != nil {
		v.addRangeInt("confidence_days_tier3", *update.ConfidenceDaysTier3, 1, 365)
	}
	if update.SpikeRatioThreshold != nil {
		v.addRangeFloat("spike_ratio_threshold", *update.SpikeRatioThreshold, 1.0, 100.0)
	}
	if update.SpikeConfidencePenalty != nil {
		v.addRangeFloat("spike_confidence_penalty", *update.SpikeConfidencePenalty, 0.01, 1.0)
	}
	if update.NoProfilingConfidenceFactor != nil {
		v.addRangeFloat("no_profiling_confidence_factor", *update.NoProfilingConfidenceFactor, 0.01, 1.0)
	}
	if update.TimeslicingMajorityThreshold != nil {
		v.addRangeFloat("timeslicing_majority_threshold", *update.TimeslicingMajorityThreshold, 0.01, 1.0)
	}
	if update.TimeslicingMinReplicas != nil {
		v.addRangeInt("timeslicing_min_replicas", *update.TimeslicingMinReplicas, 1, 16)
	}
	if update.TimeslicingMaxReplicas != nil {
		v.addRangeInt("timeslicing_max_replicas", *update.TimeslicingMaxReplicas, 1, 16)
	}
	if update.TimeslicingBasePenalty != nil {
		v.addRangeFloat("timeslicing_base_penalty", *update.TimeslicingBasePenalty, 0.01, 1.0)
	}
	if update.TimeslicingImpactedWeight != nil {
		v.addRangeFloat("timeslicing_impacted_weight", *update.TimeslicingImpactedWeight, 0.01, 1.0)
	}
	if update.NodeFreshnessDays != nil {
		v.addRangeInt("node_freshness_days", *update.NodeFreshnessDays, 1, 90)
	}

	tier1 := current.ConfidenceDaysTier1
	if update.ConfidenceDaysTier1 != nil {
		tier1 = *update.ConfidenceDaysTier1
	}
	tier2 := current.ConfidenceDaysTier2
	if update.ConfidenceDaysTier2 != nil {
		tier2 = *update.ConfidenceDaysTier2
	}
	tier3 := current.ConfidenceDaysTier3
	if update.ConfidenceDaysTier3 != nil {
		tier3 = *update.ConfidenceDaysTier3
	}
	if tier1 >= tier2 {
		v.addConstraint("confidence_days_tier1", "must be less than confidence_days_tier2")
	}
	if tier2 >= tier3 {
		v.addConstraint("confidence_days_tier2", "must be less than confidence_days_tier3")
	}

	minReplicas := current.TimeslicingMinReplicas
	if update.TimeslicingMinReplicas != nil {
		minReplicas = *update.TimeslicingMinReplicas
	}
	maxReplicas := current.TimeslicingMaxReplicas
	if update.TimeslicingMaxReplicas != nil {
		maxReplicas = *update.TimeslicingMaxReplicas
	}
	if minReplicas > maxReplicas {
		v.addConstraint("timeslicing_min_replicas", "must be less than or equal to timeslicing_max_replicas")
	}

	return v.result()
}

func validatePVCThresholdUpdate(update PVCThresholdSettingsUpdate, current PVCThresholdSettings) error {
	v := fieldValidator{}

	if update.OversizedThreshold != nil {
		v.addRangeFloat("oversized_threshold", *update.OversizedThreshold, 0.01, 0.99)
	}
	if update.NearFullThreshold != nil {
		v.addRangeFloat("near_full_threshold", *update.NearFullThreshold, 0.01, 0.99)
	}
	if update.MinTrendDays != nil {
		v.addRangeInt("min_trend_days", *update.MinTrendDays, 1, 365)
	}
	if update.RecommendedSizeMultiplier != nil {
		v.addRangeInt("recommended_size_multiplier", *update.RecommendedSizeMultiplier, 1, 10)
	}
	if update.MinRecommendedGiB != nil {
		v.addRangeInt("min_recommended_gib", *update.MinRecommendedGiB, 1, 10240)
	}
	if update.DaysToFullAlert != nil {
		v.addRangeInt("days_to_full_alert", *update.DaysToFullAlert, 1, 365)
	}

	oversized := current.OversizedThreshold
	if update.OversizedThreshold != nil {
		oversized = *update.OversizedThreshold
	}
	nearFull := current.NearFullThreshold
	if update.NearFullThreshold != nil {
		nearFull = *update.NearFullThreshold
	}
	if oversized >= nearFull {
		v.addConstraint("oversized_threshold", "must be less than near_full_threshold")
	}

	return v.result()
}
