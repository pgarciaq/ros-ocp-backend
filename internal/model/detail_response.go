package model

import (
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/money"
	"github.com/redhatinsights/ros-ocp-backend/internal/notifications"
)

// GPURecommendation holds GPU-specific recommendation data.
type GPURecommendation struct {
	CurrentGPUModel                       string   `json:"current_gpu_model"`
	CurrentGPUProfile                     *string  `json:"current_gpu_profile"`
	GPUClassification                     string   `json:"gpu_classification,omitempty"`
	RecommendedGPUProfile                 *string  `json:"recommended_gpu_profile,omitempty"`
	MemoryBoundDetected                   bool     `json:"memory_bound_detected"`
	GPUConfidence                         float32  `json:"gpu_confidence"`
	TensorPipeActiveAvg                   float32  `json:"tensor_pipe_active_avg"`
	DRAMActiveAvg                         float32  `json:"dram_active_avg"`
	SMActiveAvg                           float32  `json:"sm_active_avg"`
	FBUsageMaxMiB                         float32  `json:"fb_usage_max_mib"`
	EstimatedMonthlyGPUSavings         *money.MoneyAmount `json:"estimated_monthly_gpu_savings,omitempty"`
	EstimatedMonthlyTimeslicingSavings *money.MoneyAmount `json:"estimated_monthly_timeslicing_savings,omitempty"`
	GPUIdleState                          string   `json:"gpu_idle_state,omitempty"`
	GPUIdleSince                          *string  `json:"gpu_idle_since,omitempty"`
	GPUIdleDurationDays                   *int     `json:"gpu_idle_duration_days,omitempty"`
	EstimatedMonthlyGPUWaste              *money.MoneyAmount `json:"estimated_monthly_gpu_waste,omitempty"`
	Currency                              string   `json:"currency,omitempty"`
	Notifications                         []int16  `json:"notifications,omitempty"`
	TimeSlicingNode                       *string  `json:"time_slicing_node,omitempty"`
	TimeSlicingReplicas                   *int     `json:"time_slicing_replicas,omitempty"`
	Explanation                           *GPUExplanationAPI `json:"explanation,omitempty"`
}

// DetailResponse is the strongly-typed Kruize-compatible response for the
// native detail endpoint. The JSON shape matches what koku-ui expects:
//
//	recommendations.recommendation_terms.<term>.plots.plots_data
//	recommendations.recommendation_terms.<term>.recommendation_engines.cost.config
//	recommendations.monitoring_end_time
//	recommendations.current
type DetailResponse struct {
	ID                      string                        `json:"id"`
	ClusterAlias            string                        `json:"cluster_alias"`
	ClusterUUID             string                        `json:"cluster_uuid"`
	Container               string                        `json:"container"`
	Project                 string                        `json:"project"`
	Workload                string                        `json:"workload"`
	WorkloadType            string                        `json:"workload_type"`
	SourceID                string                        `json:"source_id"`
	LastReported            string                        `json:"last_reported"`
	AnalyticsIncomplete     bool                          `json:"analytics_incomplete,omitempty"`
	AnalyticsIncompleteAt   *string                       `json:"analytics_incomplete_at,omitempty"`
	IngestHooksFailed       bool                          `json:"ingest_hooks_failed,omitempty"`
	IngestHooksFailedAt     *string                       `json:"ingest_hooks_failed_at,omitempty"`
	Currency                string                        `json:"currency,omitempty"`
	IdleState               string                        `json:"idle_state"`
	IdleSince               *string                       `json:"idle_since,omitempty"`
	IdleDurationDays        *int                          `json:"idle_duration_days,omitempty"`
	PeakCPUMillicores       *int64                        `json:"peak_cpu_millicores,omitempty"`
	PeakMemoryBytes         *int64                        `json:"peak_memory_bytes,omitempty"`
	EstimatedMonthlyWaste   *money.MoneyAmount          `json:"estimated_monthly_waste,omitempty"`
	IdleRecommendation      *IdleRecommendation           `json:"idle_recommendation,omitempty"`
	Recommendations         DetailRecommendations         `json:"recommendations"`
	GPU                     map[string]*GPURecommendation `json:"gpu,omitempty"`
}

// ReplicaInfo conveys how many pod replicas back a workload's container.
type ReplicaInfo struct {
	Min       int    `json:"min"`
	Max       int    `json:"max"`
	Avg       int    `json:"avg"`
	Desired   int    `json:"desired,omitempty"`
	Available int    `json:"available,omitempty"`
	Source    string `json:"source,omitempty"`
}

// DetailRecommendations wraps the term-level data with monitoring_end_time
// and current resource config. Notifications live on each engine only.
type DetailRecommendations struct {
	Current                 *DetailResourceConfig  `json:"current,omitempty"`
	Replicas                *ReplicaInfo           `json:"replicas,omitempty"`
	EstimatedMonthlySavings *money.MoneyAmount     `json:"estimated_monthly_savings,omitempty"`
	CPUSavings              *money.MoneyAmount     `json:"cpu_savings,omitempty"`
	MemorySavings           *money.MoneyAmount     `json:"memory_savings,omitempty"`
	MonitoringEndTime       string                 `json:"monitoring_end_time"`
	RecommendationTerms     map[string]DetailTerm  `json:"recommendation_terms"`
}

// DetailTerm holds plots and engine recommendations for a single term.
type DetailTerm struct {
	DurationInHours       float64         `json:"duration_in_hours"`
	Plots                 *NativePlot     `json:"plots,omitempty"`
	BusinessHoursPlots    *NativePlot     `json:"business_hours_plots,omitempty"`
	RecommendationEngines *DetailEngines  `json:"recommendation_engines,omitempty"`
}

// DetailEngines groups cost and performance engine recommendations in the
// Kruize-compatible nested shape (config/variation/notifications).
type DetailEngines struct {
	Cost        *DetailEngine `json:"cost,omitempty"`
	Performance *DetailEngine `json:"performance,omitempty"`
}

// BusinessHoursDetail is the Kruize-compatible nested business_hours block on an engine.
type BusinessHoursDetail struct {
	Requests *DetailResourcePair `json:"requests,omitempty"`
	Limits   *DetailResourcePair `json:"limits,omitempty"`
	Reason   string              `json:"reason,omitempty"`
}

// DetailEngine is the Kruize-compatible engine recommendation shape.
// The UI reads config.requests.cpu.amount, variation.requests.cpu.amount, etc.
type DetailEngine struct {
	Config        *DetailResourceConfig                      `json:"config,omitempty"`
	BusinessHours *BusinessHoursDetail                       `json:"business_hours,omitempty"`
	Variation     *DetailResourceConfig                      `json:"variation,omitempty"`
	Notifications map[string]notifications.NotificationEntry `json:"notifications,omitempty"`
	Explanation   *ContainerExplanationAPI                   `json:"explanation,omitempty"`
}

// DetailResourceConfig holds CPU and memory values for requests and limits.
// Used for current config, recommended config, and variation.
type DetailResourceConfig struct {
	Limits   *DetailResourcePair `json:"limits,omitempty"`
	Requests *DetailResourcePair `json:"requests,omitempty"`
}

// DetailResourcePair holds CPU and memory resource values.
type DetailResourcePair struct {
	CPU    *DetailResourceValue `json:"cpu,omitempty"`
	Memory *DetailResourceValue `json:"memory,omitempty"`
}

// DetailResourceValue is a single resource value with amount and format.
type DetailResourceValue struct {
	Amount float64 `json:"amount"`
	Format string  `json:"format"`
}

var termDurationHours = map[string]float64{
	"short_term":  24,
	"medium_term": 7 * 24,
	"long_term":   15 * 24,
}

// BuildDetailResponse maps a NativeContainerResult into the Kruize-compatible
// DetailResponse structure, embedding boxplot data, monitoring_end_time,
// current resource config, and per-engine notifications.
func BuildDetailResponse(
	native *NativeContainerResult,
	plots map[string]*NativePlot,
	bhPlots map[string]*NativePlot,
	monitoringEndTime time.Time,
	opts ListResponseOptions,
) *DetailResponse {
	var replicas *ReplicaInfo
	if native.Replicas != nil {
		replicas = native.Replicas
	}
	terms := make(map[string]DetailTerm)
	var current *DetailResourceConfig

	for termKey, termRec := range native.Recommendations {
		costEngine := toDetailEngine(termRec.Cost)
		perfEngine := toDetailEngine(termRec.Performance)

		dt := DetailTerm{
			DurationInHours: termDurationHours[termKey],
			RecommendationEngines: &DetailEngines{
				Cost:        costEngine,
				Performance: perfEngine,
			},
		}
		if p, ok := plots[termKey]; ok {
			dt.Plots = p
		}
		if bhPlots != nil {
			if bp, ok := bhPlots[termKey]; ok {
				dt.BusinessHoursPlots = bp
			}
		}
		terms[termKey] = dt

		// Extract current resource config from the first engine that has it.
		if current == nil {
			current = extractCurrent(termRec.Cost)
		}
		if current == nil {
			current = extractCurrent(termRec.Performance)
		}
	}

	var metStr string
	if !monitoringEndTime.IsZero() && monitoringEndTime.Year() > 1 {
		metStr = monitoringEndTime.UTC().Format(time.RFC3339)
	}

	recs := DetailRecommendations{
		Current:                 current,
		Replicas:                replicas,
		EstimatedMonthlySavings: native.EstimatedMonthlySavings,
		MonitoringEndTime:       metStr,
		RecommendationTerms:     filterDetailRecommendationTerms(terms, opts),
	}

	resp := &DetailResponse{
		ID:                    native.ID,
		ClusterAlias:          native.ClusterAlias,
		ClusterUUID:           native.ClusterUUID,
		Container:             native.Container,
		Project:               native.Project,
		Workload:              native.Workload,
		WorkloadType:          native.WorkloadType,
		SourceID:              native.SourceID,
		LastReported:          native.LastReported,
		AnalyticsIncomplete:   native.AnalyticsIncomplete,
		AnalyticsIncompleteAt: native.AnalyticsIncompleteAt,
		IngestHooksFailed:     native.IngestHooksFailed,
		IngestHooksFailedAt:   native.IngestHooksFailedAt,
		Currency:              native.Currency,
		IdleState:             native.IdleState,
		IdleSince:             native.IdleSince,
		IdleDurationDays:      native.IdleDurationDays,
		PeakCPUMillicores:     native.PeakCPUMillicores,
		PeakMemoryBytes:       native.PeakMemoryBytes,
		EstimatedMonthlyWaste: native.EstimatedMonthlyWaste,
		IdleRecommendation:    native.IdleRecommendation,
		Recommendations:       recs,
		GPU:                   filterGPUByTerm(native.GPU, opts),
	}
	if resp.IdleState == "" {
		resp.IdleState = "active"
	}
	return resp
}

// toDetailEngine transforms a flat EngineRecommendation into the Kruize-
// compatible nested shape with config, variation, and notifications.
func toDetailEngine(eng *EngineRecommendation) *DetailEngine {
	if eng == nil {
		return nil
	}

	config := &DetailResourceConfig{
		Limits: &DetailResourcePair{
			CPU:    mcToCores(eng.CPULimitMillicores),
			Memory: kibToMiB(eng.MemLimitKiB),
		},
		Requests: &DetailResourcePair{
			CPU:    mcToCores(eng.CPURequestMillicores),
			Memory: kibToMiB(eng.MemRequestKiB),
		},
	}

	var variation *DetailResourceConfig
	hasRequestVar := eng.VariationCPURequestPct != nil || eng.VariationMemRequestPct != nil
	hasLimitVar := eng.VariationCPULimitPct != nil || eng.VariationMemLimitPct != nil
	if hasRequestVar || hasLimitVar {
		variation = &DetailResourceConfig{}
		if hasLimitVar {
			variation.Limits = &DetailResourcePair{
				CPU:    pctToValue(eng.VariationCPULimitPct),
				Memory: pctToValue(eng.VariationMemLimitPct),
			}
		}
		if hasRequestVar {
			variation.Requests = &DetailResourcePair{
				CPU:    pctToValue(eng.VariationCPURequestPct),
				Memory: pctToValue(eng.VariationMemRequestPct),
			}
		}
	}

	notifs := eng.Notifications
	if notifs == nil && len(eng.NotificationCodes) > 0 {
		notifs = notifications.MapToKruizeFormat(eng.NotificationCodes)
	}

	de := &DetailEngine{
		Config:        config,
		Variation:     variation,
		Notifications: notifs,
		Explanation:   eng.Explanation,
	}
	if eng.BusinessHours != nil {
		de.BusinessHours = businessHoursToDetail(eng.BusinessHours)
	}
	return de
}

func businessHoursToDetail(bh *BusinessHoursRecommendation) *BusinessHoursDetail {
	if bh == nil {
		return nil
	}
	if bh.Reason != "" {
		return &BusinessHoursDetail{Reason: bh.Reason}
	}
	detail := &BusinessHoursDetail{
		Requests: &DetailResourcePair{
			CPU:    mcToCores(bh.CPURequestMillicores),
			Memory: kibToMiB(bh.MemRequestKiB),
		},
	}
	if bh.CPULimitMillicores != nil || bh.MemLimitKiB != nil {
		detail.Limits = &DetailResourcePair{
			CPU:    mcToCores(bh.CPULimitMillicores),
			Memory: kibToMiB(bh.MemLimitKiB),
		}
	}
	return detail
}

// extractCurrent builds a DetailResourceConfig from an engine's current_* fields.
func extractCurrent(eng *EngineRecommendation) *DetailResourceConfig {
	if eng == nil {
		return nil
	}
	if eng.CurrentCPURequestMC == nil && eng.CurrentCPULimitMC == nil &&
		eng.CurrentMemRequestKiB == nil && eng.CurrentMemLimitKiB == nil {
		return nil
	}
	return &DetailResourceConfig{
		Limits: &DetailResourcePair{
			CPU:    mcToCores(eng.CurrentCPULimitMC),
			Memory: kibToMiB(eng.CurrentMemLimitKiB),
		},
		Requests: &DetailResourcePair{
			CPU:    mcToCores(eng.CurrentCPURequestMC),
			Memory: kibToMiB(eng.CurrentMemRequestKiB),
		},
	}
}

func mcToCores(mc *int64) *DetailResourceValue {
	if mc == nil {
		return nil
	}
	return &DetailResourceValue{Amount: float64(*mc) / 1000.0, Format: "cores"}
}

func kibToMiB(kib *int64) *DetailResourceValue {
	if kib == nil {
		return nil
	}
	return &DetailResourceValue{Amount: float64(*kib / 1024), Format: "MiB"}
}

func pctToValue(pct *int32) *DetailResourceValue {
	if pct == nil {
		return nil
	}
	return &DetailResourceValue{Amount: float64(*pct), Format: "percent"}
}

// NamespaceDetailResponse is the Kruize-compatible response for namespace
// recommendations. It mirrors DetailResponse but without container-specific
// fields (Container, Workload, WorkloadType, GPU).
type NamespaceDetailResponse struct {
	ID                    string                `json:"id"`
	ClusterAlias          string                `json:"cluster_alias"`
	ClusterUUID           string                `json:"cluster_uuid"`
	Project               string                `json:"project"`
	SourceID              string                `json:"source_id"`
	LastReported          string                `json:"last_reported"`
	IdleState             string                `json:"idle_state"`
	IdleSince             *string               `json:"idle_since,omitempty"`
	IdleDurationDays      *int                  `json:"idle_duration_days,omitempty"`
	EstimatedMonthlyWaste *money.MoneyAmount    `json:"estimated_monthly_waste,omitempty"`
	Recommendations       DetailRecommendations `json:"recommendations"`
}

// BuildNamespaceDetailResponse converts a NativeNamespaceResult (with flat
// EngineRecommendation fields like cpu_request_millicores) into the nested
// Kruize-compatible format (requests.cpu.amount / requests.cpu.format).
func BuildNamespaceDetailResponse(
	native *NativeNamespaceResult,
	plots map[string]*NativePlot,
	bhPlots map[string]*NativePlot,
	monitoringEndTime time.Time,
	opts ListResponseOptions,
) *NamespaceDetailResponse {
	terms := make(map[string]DetailTerm)
	var current *DetailResourceConfig

	for termKey, termVal := range native.Recommendations {
		if termKey == "monitoring_end_time" {
			continue
		}
		termRec, ok := termVal.(TermRecommendation)
		if !ok {
			continue
		}

		costEngine := toDetailEngine(termRec.Cost)
		perfEngine := toDetailEngine(termRec.Performance)

		dt := DetailTerm{
			DurationInHours: termDurationHours[termKey],
			RecommendationEngines: &DetailEngines{
				Cost:        costEngine,
				Performance: perfEngine,
			},
		}
		if p, ok := plots[termKey]; ok {
			dt.Plots = p
		}
		if bhPlots != nil {
			if bp, ok := bhPlots[termKey]; ok {
				dt.BusinessHoursPlots = bp
			}
		}
		terms[termKey] = dt

		if current == nil {
			current = extractCurrent(termRec.Cost)
		}
		if current == nil {
			current = extractCurrent(termRec.Performance)
		}
	}

	var metStr string
	if !monitoringEndTime.IsZero() && monitoringEndTime.Year() > 1 {
		metStr = monitoringEndTime.UTC().Format(time.RFC3339)
	}

	recs := DetailRecommendations{
		Current:                 current,
		EstimatedMonthlySavings: extractMoneyAmount(native.Recommendations, "estimated_monthly_savings"),
		CPUSavings:              extractMoneyAmount(native.Recommendations, "cpu_savings"),
		MemorySavings:           extractMoneyAmount(native.Recommendations, "memory_savings"),
		MonitoringEndTime:       metStr,
		RecommendationTerms:     filterDetailRecommendationTerms(terms, opts),
	}

	idleState := native.IdleState
	if idleState == "" {
		idleState = "active"
	}

	return &NamespaceDetailResponse{
		ID:                    native.ID,
		ClusterAlias:          native.ClusterAlias,
		ClusterUUID:           native.ClusterUUID,
		Project:               native.Project,
		SourceID:              native.SourceID,
		LastReported:          native.LastReported,
		IdleState:             idleState,
		IdleSince:             native.IdleSince,
		IdleDurationDays:      native.IdleDurationDays,
		EstimatedMonthlyWaste: native.EstimatedMonthlyWaste,
		Recommendations:       recs,
	}
}

func filterDetailRecommendationTerms(terms map[string]DetailTerm, opts ListResponseOptions) map[string]DetailTerm {
	if len(terms) == 0 {
		return nil
	}
	if opts.TermFilter == "" && opts.EngineFilter == "" {
		return terms
	}

	if opts.TermFilter != "" {
		termRec, ok := terms[opts.TermFilter]
		if !ok {
			return nil
		}
		filtered := filterDetailTermEngines(termRec, opts.EngineFilter)
		if filtered.RecommendationEngines == nil ||
			(filtered.RecommendationEngines.Cost == nil && filtered.RecommendationEngines.Performance == nil) {
			return nil
		}
		return map[string]DetailTerm{opts.TermFilter: filtered}
	}

	if opts.EngineFilter != "" {
		result := make(map[string]DetailTerm, len(terms))
		for termKey, termRec := range terms {
			filtered := filterDetailTermEngines(termRec, opts.EngineFilter)
			if filtered.RecommendationEngines != nil &&
				(filtered.RecommendationEngines.Cost != nil || filtered.RecommendationEngines.Performance != nil) {
				result[termKey] = filtered
			}
		}
		if len(result) == 0 {
			return nil
		}
		return result
	}

	return terms
}

func filterDetailTermEngines(dt DetailTerm, engineFilter string) DetailTerm {
	if dt.RecommendationEngines == nil {
		return DetailTerm{DurationInHours: dt.DurationInHours, Plots: dt.Plots, BusinessHoursPlots: dt.BusinessHoursPlots}
	}
	engines := &DetailEngines{}
	if engineFilter == "" || engineFilter == "cost" {
		engines.Cost = dt.RecommendationEngines.Cost
	}
	if engineFilter == "" || engineFilter == "performance" {
		engines.Performance = dt.RecommendationEngines.Performance
	}
	if engines.Cost == nil && engines.Performance == nil {
		return DetailTerm{DurationInHours: dt.DurationInHours, Plots: dt.Plots, BusinessHoursPlots: dt.BusinessHoursPlots}
	}
	return DetailTerm{
		DurationInHours:       dt.DurationInHours,
		Plots:                 dt.Plots,
		BusinessHoursPlots:    dt.BusinessHoursPlots,
		RecommendationEngines: engines,
	}
}

func filterGPUByTerm(gpu map[string]*GPURecommendation, opts ListResponseOptions) map[string]*GPURecommendation {
	if gpu == nil || opts.TermFilter == "" {
		return gpu
	}
	if rec, ok := gpu[opts.TermFilter]; ok {
		return map[string]*GPURecommendation{opts.TermFilter: rec}
	}
	return nil
}
