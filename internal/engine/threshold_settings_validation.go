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

// FieldValidator accumulates field validation errors for settings update requests.
type FieldValidator struct {
	errs []string
}

func (v *FieldValidator) AddRangeFloat(field string, val float64, min, max float64) {
	if val < min || val > max {
		v.errs = append(v.errs, fmt.Sprintf("%s must be between %g and %g", field, min, max))
	}
}

func (v *FieldValidator) AddRangeFloat32(field string, val float32, min, max float64) {
	v.AddRangeFloat(field, float64(val), min, max)
}

func (v *FieldValidator) AddRangeInt(field string, val int, min, max int) {
	if val < min || val > max {
		v.errs = append(v.errs, fmt.Sprintf("%s must be between %d and %d", field, min, max))
	}
}

func (v *FieldValidator) AddRangeInt64(field string, val int64, min, max int64) {
	if val < min || val > max {
		v.errs = append(v.errs, fmt.Sprintf("%s must be between %d and %d", field, min, max))
	}
}

func (v *FieldValidator) AddConstraint(field, message string) {
	v.errs = append(v.errs, fmt.Sprintf("%s: %s", field, message))
}

func (v *FieldValidator) Result() error {
	if len(v.errs) == 0 {
		return nil
	}
	return &ThresholdValidationError{Errors: v.errs}
}

var thresholdAllowedFields = map[string]map[string]struct{}{
	"container": sizingThresholdAllowedFields(),
	"namespace": sizingThresholdAllowedFields(),
	"node":      nodeThresholdAllowedFields(),
	"gpu":       gpuThresholdAllowedFields(),
	"pvc":       pvcThresholdAllowedFields(),
}

func sizingThresholdAllowedFields() map[string]struct{} {
	return map[string]struct{}{
		"cpu_cost_percentile":       {},
		"cpu_perf_percentile":       {},
		"mem_cost_percentile":       {},
		"mem_perf_percentile":       {},
		"min_margin":                {},
		"max_margin":                {},
		"limit_multiplier":          {},
		"cpu_floor_mc":              {},
		"idle_cpu_threshold_mc":     {},
		"idle_mem_threshold_kib":    {},
		"mem_trend_slope_threshold": {},
		"low_confidence_threshold":  {},
		"sparse_data_threshold":     {},
	}
}

func nodeThresholdAllowedFields() map[string]struct{} {
	return map[string]struct{}{
		"underutil_threshold":                    {},
		"overcommit_threshold":                   {},
		"allocatable_factor":                     {},
		"stranded_imbalance_threshold":           {},
		"ema_alpha":                              {},
		"cost_target_utilization":                {},
		"perf_target_utilization":                {},
		"perf_consolidation_headroom_multiplier": {},
		"trend_min_days":                         {},
		"zombie_cpu_p95_mc":                      {},
		"zombie_max_pods":                        {},
		"idle_cpu_util_pct":                      {},
		"idle_mem_util_pct":                      {},
		"idle_max_pods":                          {},
		"pod_headroom_consolidation_gate":        {},
		"pod_headroom_notification_threshold":    {},
	}
}

func gpuThresholdAllowedFields() map[string]struct{} {
	return map[string]struct{}{
		"idle_threshold":                   {},
		"underutilized_sm_threshold":       {},
		"underutilized_tensor_threshold":   {},
		"membound_dram_threshold":          {},
		"membound_tensor_threshold":          {},
		"fb_headroom_factor":               {},
		"compute_bound_dram_threshold":     {},
		"mig_fb_percentile":                {},
		"confidence_days_tier1":            {},
		"confidence_days_tier2":            {},
		"confidence_days_tier3":            {},
		"spike_ratio_threshold":            {},
		"spike_confidence_penalty":         {},
		"no_profiling_confidence_factor":   {},
		"timeslicing_majority_threshold":   {},
		"timeslicing_min_replicas":         {},
		"timeslicing_max_replicas":         {},
		"timeslicing_base_penalty":         {},
		"timeslicing_impacted_weight":      {},
		"node_freshness_days":              {},
	}
}

func pvcThresholdAllowedFields() map[string]struct{} {
	return map[string]struct{}{
		"oversized_threshold":         {},
		"near_full_threshold":         {},
		"min_trend_days":              {},
		"recommended_size_multiplier": {},
		"min_recommended_gib":         {},
		"days_to_full_alert":          {},
	}
}

func validateThresholdUpdateKeys(recType string, rawUpdate json.RawMessage) error {
	allowed, ok := thresholdAllowedFields[recType]
	if !ok {
		return fmt.Errorf("unsupported recommendation_type %q", recType)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(rawUpdate, &keys); err != nil {
		return fmt.Errorf("invalid request body: %w", err)
	}
	v := FieldValidator{}
	for key := range keys {
		if _, ok := allowed[key]; !ok {
			v.errs = append(v.errs, fmt.Sprintf("unknown field %q", key))
		}
	}
	return v.Result()
}

// ValidateThresholdSettingsUpdate checks incoming PUT fields against allowed ranges
// and cross-field constraints before locked-field enforcement.
func ValidateThresholdSettingsUpdate(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, recType string,
	rawUpdate json.RawMessage,
) error {
	if err := validateThresholdUpdateKeys(recType, rawUpdate); err != nil {
		return err
	}
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
		current, err := ResolveNodeThresholdSettings(ctx, pool, orgID)
		if err != nil {
			return err
		}
		return validateNodeThresholdUpdate(update, current)
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
	v := FieldValidator{}

	if update.CPUCostPercentile != nil {
		v.AddRangeFloat("cpu_cost_percentile", *update.CPUCostPercentile, 0.01, 1.0)
	}
	if update.CPUPerfPercentile != nil {
		v.AddRangeFloat("cpu_perf_percentile", *update.CPUPerfPercentile, 0.01, 1.0)
	}
	if update.MemCostPercentile != nil {
		v.AddRangeFloat("mem_cost_percentile", *update.MemCostPercentile, 0.01, 1.0)
	}
	if update.MemPerfPercentile != nil {
		v.AddRangeFloat("mem_perf_percentile", *update.MemPerfPercentile, 0.01, 1.0)
	}
	if update.MinMargin != nil {
		v.AddRangeFloat("min_margin", *update.MinMargin, 1.0, 3.0)
	}
	if update.MaxMargin != nil {
		v.AddRangeFloat("max_margin", *update.MaxMargin, 1.0, 3.0)
	}
	if update.LimitMultiplier != nil {
		v.AddRangeFloat("limit_multiplier", *update.LimitMultiplier, 1.0, 5.0)
	}
	if update.CPUFloorMC != nil {
		v.AddRangeInt64("cpu_floor_mc", *update.CPUFloorMC, 1, 1000)
	}
	if update.IdleCPUThresholdMC != nil {
		v.AddRangeInt64("idle_cpu_threshold_mc", *update.IdleCPUThresholdMC, 1, 10000)
	}
	if update.IdleMemThresholdKiB != nil {
		v.AddRangeInt64("idle_mem_threshold_kib", *update.IdleMemThresholdKiB, 1, 10485760)
	}
	if update.MemTrendSlopeThreshold != nil {
		v.AddRangeFloat("mem_trend_slope_threshold", *update.MemTrendSlopeThreshold, 1.0, 1000000.0)
	}
	if update.LowConfidenceThreshold != nil {
		v.AddRangeFloat32("low_confidence_threshold", *update.LowConfidenceThreshold, 0.01, 1.0)
	}
	if update.SparseDataThreshold != nil {
		v.AddRangeInt("sparse_data_threshold", *update.SparseDataThreshold, 1, 30)
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
		v.AddConstraint("min_margin", "must be less than or equal to max_margin")
	}

	return v.Result()
}

func validateNodeThresholdUpdate(update NodeThresholdSettingsUpdate, current NodeThresholdSettings) error {
	v := FieldValidator{}

	if update.UnderutilThreshold != nil {
		v.AddRangeFloat("underutil_threshold", *update.UnderutilThreshold, 0.01, 0.99)
	}
	if update.OvercommitThreshold != nil {
		v.AddRangeFloat("overcommit_threshold", *update.OvercommitThreshold, 1.0, 10.0)
	}
	if update.AllocatableFactor != nil {
		v.AddRangeFloat("allocatable_factor", *update.AllocatableFactor, 0.5, 1.0)
	}
	if update.StrandedImbalanceThreshold != nil {
		v.AddRangeFloat("stranded_imbalance_threshold", *update.StrandedImbalanceThreshold, 0.1, 1.0)
	}
	if update.EMAAlpha != nil {
		v.AddRangeFloat("ema_alpha", *update.EMAAlpha, 0.01, 1.0)
	}
	if update.CostTargetUtilization != nil {
		v.AddRangeFloat("cost_target_utilization", *update.CostTargetUtilization, 0.1, 0.99)
	}
	if update.PerfTargetUtilization != nil {
		v.AddRangeFloat("perf_target_utilization", *update.PerfTargetUtilization, 0.1, 0.99)
	}
	if update.PerfConsolidationHeadroomMultiplier != nil {
		v.AddRangeFloat("perf_consolidation_headroom_multiplier", *update.PerfConsolidationHeadroomMultiplier, 1.0, 10.0)
	}
	if update.TrendMinDays != nil {
		v.AddRangeInt("trend_min_days", *update.TrendMinDays, 1, 30)
	}
	if update.ZombieCPUP95MC != nil {
		v.AddRangeInt64("zombie_cpu_p95_mc", *update.ZombieCPUP95MC, 1, 100000)
	}
	if update.ZombieMaxPods != nil {
		v.AddRangeInt64("zombie_max_pods", *update.ZombieMaxPods, 1, 10000)
	}
	if update.IdleCPUUtilPct != nil {
		v.AddRangeInt64("idle_cpu_util_pct", *update.IdleCPUUtilPct, 0, 100)
	}
	if update.IdleMemUtilPct != nil {
		v.AddRangeInt64("idle_mem_util_pct", *update.IdleMemUtilPct, 0, 100)
	}
	if update.IdleMaxPods != nil {
		v.AddRangeInt64("idle_max_pods", *update.IdleMaxPods, 1, 10000)
	}
	if update.PodHeadroomConsolidationGate != nil {
		v.AddRangeFloat("pod_headroom_consolidation_gate", *update.PodHeadroomConsolidationGate, 0.0, 1.0)
	}
	if update.PodHeadroomNotificationThreshold != nil {
		v.AddRangeFloat("pod_headroom_notification_threshold", *update.PodHeadroomNotificationThreshold, 0.0, 1.0)
	}

	consolidationGate := current.PodHeadroomConsolidationGate
	if update.PodHeadroomConsolidationGate != nil {
		consolidationGate = *update.PodHeadroomConsolidationGate
	}
	notificationThreshold := current.PodHeadroomNotificationThreshold
	if update.PodHeadroomNotificationThreshold != nil {
		notificationThreshold = *update.PodHeadroomNotificationThreshold
	}
	if consolidationGate < notificationThreshold {
		v.AddConstraint(
			"pod_headroom_consolidation_gate",
			"must be greater than or equal to pod_headroom_notification_threshold",
		)
	}

	return v.Result()
}

func validateGPUThresholdUpdate(update GPUThresholdSettingsUpdate, current GPUThresholdSettings) error {
	v := FieldValidator{}

	if update.IdleThreshold != nil {
		v.AddRangeFloat("idle_threshold", *update.IdleThreshold, 0.0, 1.0)
	}
	if update.UnderutilizedSMThreshold != nil {
		v.AddRangeFloat("underutilized_sm_threshold", *update.UnderutilizedSMThreshold, 0.0, 1.0)
	}
	if update.UnderutilizedTensorThreshold != nil {
		v.AddRangeFloat("underutilized_tensor_threshold", *update.UnderutilizedTensorThreshold, 0.0, 1.0)
	}
	if update.MemBoundDRAMThreshold != nil {
		v.AddRangeFloat("membound_dram_threshold", *update.MemBoundDRAMThreshold, 0.0, 1.0)
	}
	if update.MemBoundTensorThreshold != nil {
		v.AddRangeFloat("membound_tensor_threshold", *update.MemBoundTensorThreshold, 0.0, 1.0)
	}
	if update.FBHeadroomFactor != nil {
		v.AddRangeFloat("fb_headroom_factor", *update.FBHeadroomFactor, 0.0, 1.0)
	}
	if update.ComputeBoundDRAMThreshold != nil {
		v.AddRangeFloat("compute_bound_dram_threshold", *update.ComputeBoundDRAMThreshold, 0.0, 1.0)
	}
	if update.MIGFBPercentile != nil {
		v.AddRangeFloat("mig_fb_percentile", *update.MIGFBPercentile, 0.0, 1.0)
	}
	if update.ConfidenceDaysTier1 != nil {
		v.AddRangeInt("confidence_days_tier1", *update.ConfidenceDaysTier1, 1, 365)
	}
	if update.ConfidenceDaysTier2 != nil {
		v.AddRangeInt("confidence_days_tier2", *update.ConfidenceDaysTier2, 1, 365)
	}
	if update.ConfidenceDaysTier3 != nil {
		v.AddRangeInt("confidence_days_tier3", *update.ConfidenceDaysTier3, 1, 365)
	}
	if update.SpikeRatioThreshold != nil {
		v.AddRangeFloat("spike_ratio_threshold", *update.SpikeRatioThreshold, 1.0, 100.0)
	}
	if update.SpikeConfidencePenalty != nil {
		v.AddRangeFloat("spike_confidence_penalty", *update.SpikeConfidencePenalty, 0.01, 1.0)
	}
	if update.NoProfilingConfidenceFactor != nil {
		v.AddRangeFloat("no_profiling_confidence_factor", *update.NoProfilingConfidenceFactor, 0.01, 1.0)
	}
	if update.TimeslicingMajorityThreshold != nil {
		v.AddRangeFloat("timeslicing_majority_threshold", *update.TimeslicingMajorityThreshold, 0.01, 1.0)
	}
	if update.TimeslicingMinReplicas != nil {
		v.AddRangeInt("timeslicing_min_replicas", *update.TimeslicingMinReplicas, 1, 16)
	}
	if update.TimeslicingMaxReplicas != nil {
		v.AddRangeInt("timeslicing_max_replicas", *update.TimeslicingMaxReplicas, 1, 16)
	}
	if update.TimeslicingBasePenalty != nil {
		v.AddRangeFloat("timeslicing_base_penalty", *update.TimeslicingBasePenalty, 0.01, 1.0)
	}
	if update.TimeslicingImpactedWeight != nil {
		v.AddRangeFloat("timeslicing_impacted_weight", *update.TimeslicingImpactedWeight, 0.01, 1.0)
	}
	if update.NodeFreshnessDays != nil {
		v.AddRangeInt("node_freshness_days", *update.NodeFreshnessDays, 1, 90)
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
		v.AddConstraint("confidence_days_tier1", "must be less than confidence_days_tier2")
	}
	if tier2 >= tier3 {
		v.AddConstraint("confidence_days_tier2", "must be less than confidence_days_tier3")
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
		v.AddConstraint("timeslicing_min_replicas", "must be less than or equal to timeslicing_max_replicas")
	}

	return v.Result()
}

func validatePVCThresholdUpdate(update PVCThresholdSettingsUpdate, current PVCThresholdSettings) error {
	v := FieldValidator{}

	if update.OversizedThreshold != nil {
		v.AddRangeFloat("oversized_threshold", *update.OversizedThreshold, 0.01, 0.99)
	}
	if update.NearFullThreshold != nil {
		v.AddRangeFloat("near_full_threshold", *update.NearFullThreshold, 0.01, 0.99)
	}
	if update.MinTrendDays != nil {
		v.AddRangeInt("min_trend_days", *update.MinTrendDays, 1, 365)
	}
	if update.RecommendedSizeMultiplier != nil {
		v.AddRangeInt("recommended_size_multiplier", *update.RecommendedSizeMultiplier, 1, 10)
	}
	if update.MinRecommendedGiB != nil {
		v.AddRangeInt("min_recommended_gib", *update.MinRecommendedGiB, 1, 10240)
	}
	if update.DaysToFullAlert != nil {
		v.AddRangeInt("days_to_full_alert", *update.DaysToFullAlert, 1, 365)
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
		v.AddConstraint("oversized_threshold", "must be less than near_full_threshold")
	}

	return v.Result()
}
