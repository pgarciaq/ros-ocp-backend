package model

import (
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/notifications"
)

// GPURecommendation holds GPU-specific recommendation data.
type GPURecommendation struct {
	CurrentGPUModel               string   `json:"current_gpu_model"`
	CurrentGPUProfile             *string  `json:"current_gpu_profile"`
	GPUClassification             string   `json:"gpu_classification,omitempty"`
	RecommendedGPUProfile         *string  `json:"recommended_gpu_profile,omitempty"`
	MemoryBoundDetected           bool     `json:"memory_bound_detected"`
	GPUConfidence                 float32  `json:"gpu_confidence"`
	TensorPipeActiveAvg           float32  `json:"tensor_pipe_active_avg"`
	DRAMActiveAvg                 float32  `json:"dram_active_avg"`
	SMActiveAvg                   float32  `json:"sm_active_avg"`
	FBUsageMaxMiB                 float32  `json:"fb_usage_max_mib"`
	EstimatedMonthlyGPUSavingsUSD         *float32 `json:"estimated_monthly_gpu_savings_usd,omitempty"`
	EstimatedMonthlyTimeslicingSavingsUSD *float32 `json:"estimated_monthly_timeslicing_savings_usd,omitempty"`
	Notifications                         []int16  `json:"notifications,omitempty"`
	TimeSlicingNode                       *string  `json:"time_slicing_node,omitempty"`
	TimeSlicingReplicas                   *int     `json:"time_slicing_replicas,omitempty"`
}

// DetailResponse is the strongly-typed Kruize-compatible response for the
// native detail endpoint. The JSON shape matches what koku-ui expects:
//
//	recommendations.recommendation_terms.<term>.plots.plots_data
//	recommendations.recommendation_terms.<term>.recommendation_engines.cost.config
//	recommendations.monitoring_end_time
//	recommendations.current
type DetailResponse struct {
	ID              string                `json:"id"`
	ClusterAlias    string                `json:"cluster_alias"`
	ClusterUUID     string                `json:"cluster_uuid"`
	Container       string                `json:"container"`
	Project         string                `json:"project"`
	Workload        string                `json:"workload"`
	WorkloadType    string                `json:"workload_type"`
	SourceID        string                `json:"source_id"`
	LastReported    string                `json:"last_reported"`
	Recommendations DetailRecommendations `json:"recommendations"`
	GPU             *GPURecommendation    `json:"gpu,omitempty"`
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

// DetailRecommendations wraps the term-level data with monitoring_end_time,
// current resource config, and top-level notifications.
type DetailRecommendations struct {
	Current                 *DetailResourceConfig                      `json:"current,omitempty"`
	Replicas                *ReplicaInfo                               `json:"replicas,omitempty"`
	EstimatedMonthlySavings *float32                                   `json:"estimated_monthly_savings_usd,omitempty"`
	MonitoringEndTime       string                                     `json:"monitoring_end_time"`
	Notifications           map[string]notifications.NotificationEntry `json:"notifications,omitempty"`
	RecommendationTerms     map[string]DetailTerm                      `json:"recommendation_terms"`
}

// DetailTerm holds plots and engine recommendations for a single term.
type DetailTerm struct {
	DurationInHours       float64                                    `json:"duration_in_hours"`
	Notifications         map[string]notifications.NotificationEntry `json:"notifications,omitempty"`
	Plots                 *NativePlot                                `json:"plots,omitempty"`
	RecommendationEngines *DetailEngines                             `json:"recommendation_engines,omitempty"`
}

// DetailEngines groups cost and performance engine recommendations in the
// Kruize-compatible nested shape (config/variation/notifications).
type DetailEngines struct {
	Cost        *DetailEngine `json:"cost,omitempty"`
	Performance *DetailEngine `json:"performance,omitempty"`
}

// DetailEngine is the Kruize-compatible engine recommendation shape.
// The UI reads config.requests.cpu.amount, variation.requests.cpu.amount, etc.
type DetailEngine struct {
	Config        *DetailResourceConfig                      `json:"config,omitempty"`
	Variation     *DetailResourceConfig                      `json:"variation,omitempty"`
	Notifications map[string]notifications.NotificationEntry `json:"notifications,omitempty"`
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
// current resource config, and aggregated notifications.
func BuildDetailResponse(
	native *NativeContainerResult,
	plots map[string]*NativePlot,
	monitoringEndTime time.Time,
) *DetailResponse {
	var replicas *ReplicaInfo
	if native.Replicas != nil {
		replicas = native.Replicas
	}
	terms := make(map[string]DetailTerm)
	allNotifications := map[string]notifications.NotificationEntry{}
	var current *DetailResourceConfig

	for termKey, termRec := range native.Recommendations {
		costEngine := toDetailEngine(termRec.Cost)
		perfEngine := toDetailEngine(termRec.Performance)

		// Aggregate term-level notifications from both engines.
		termNotifs := map[string]notifications.NotificationEntry{}
		mergeNotifications(termNotifs, costEngine)
		mergeNotifications(termNotifs, perfEngine)

		// Bubble up to top-level.
		for k, v := range termNotifs {
			allNotifications[k] = v
		}

		dt := DetailTerm{
			DurationInHours: termDurationHours[termKey],
			RecommendationEngines: &DetailEngines{
				Cost:        costEngine,
				Performance: perfEngine,
			},
		}
		if len(termNotifs) > 0 {
			dt.Notifications = termNotifs
		}
		if p, ok := plots[termKey]; ok {
			dt.Plots = p
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
		RecommendationTerms:     terms,
	}
	if len(allNotifications) > 0 {
		recs.Notifications = allNotifications
	}

	return &DetailResponse{
		ID:              native.ID,
		ClusterAlias:    native.ClusterAlias,
		ClusterUUID:     native.ClusterUUID,
		Container:       native.Container,
		Project:         native.Project,
		Workload:        native.Workload,
		WorkloadType:    native.WorkloadType,
		SourceID:        native.SourceID,
		LastReported:    native.LastReported,
		Recommendations: recs,
		GPU:             native.GPU,
	}
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

	return &DetailEngine{
		Config:        config,
		Variation:     variation,
		Notifications: eng.Notifications,
	}
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
	return &DetailResourceValue{Amount: float64(*pct), Format: "percentage"}
}

func mergeNotifications(dst map[string]notifications.NotificationEntry, eng *DetailEngine) {
	if eng == nil || eng.Notifications == nil {
		return
	}
	for k, v := range eng.Notifications {
		dst[k] = v
	}
}

// NamespaceDetailResponse is the Kruize-compatible response for namespace
// recommendations. It mirrors DetailResponse but without container-specific
// fields (Container, Workload, WorkloadType, GPU).
type NamespaceDetailResponse struct {
	ID              string               `json:"id"`
	ClusterAlias    string               `json:"cluster_alias"`
	ClusterUUID     string               `json:"cluster_uuid"`
	Project         string               `json:"project"`
	SourceID        string               `json:"source_id"`
	LastReported    string               `json:"last_reported"`
	Recommendations DetailRecommendations `json:"recommendations"`
}

// BuildNamespaceDetailResponse converts a NativeNamespaceResult (with flat
// EngineRecommendation fields like cpu_request_millicores) into the nested
// Kruize-compatible format (requests.cpu.amount / requests.cpu.format).
func BuildNamespaceDetailResponse(
	native *NativeNamespaceResult,
	plots map[string]*NativePlot,
	monitoringEndTime time.Time,
) *NamespaceDetailResponse {
	terms := make(map[string]DetailTerm)
	allNotifications := map[string]notifications.NotificationEntry{}
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

		termNotifs := map[string]notifications.NotificationEntry{}
		mergeNotifications(termNotifs, costEngine)
		mergeNotifications(termNotifs, perfEngine)

		for k, v := range termNotifs {
			allNotifications[k] = v
		}

		dt := DetailTerm{
			DurationInHours: termDurationHours[termKey],
			RecommendationEngines: &DetailEngines{
				Cost:        costEngine,
				Performance: perfEngine,
			},
		}
		if len(termNotifs) > 0 {
			dt.Notifications = termNotifs
		}
		if p, ok := plots[termKey]; ok {
			dt.Plots = p
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
		Current:             current,
		MonitoringEndTime:   metStr,
		RecommendationTerms: terms,
	}
	if len(allNotifications) > 0 {
		recs.Notifications = allNotifications
	}

	return &NamespaceDetailResponse{
		ID:              native.ID,
		ClusterAlias:    native.ClusterAlias,
		ClusterUUID:     native.ClusterUUID,
		Project:         native.Project,
		SourceID:        native.SourceID,
		LastReported:    native.LastReported,
		Recommendations: recs,
	}
}
