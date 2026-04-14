package model

import "time"

// DetailResponse is the strongly-typed Kruize-compatible response for the
// native detail endpoint. The JSON shape matches what koku-ui expects:
//
//	recommendations.recommendation_terms.<term>.plots.plots_data
//	recommendations.monitoring_end_time
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
}

// DetailRecommendations wraps the term-level data with monitoring_end_time.
type DetailRecommendations struct {
	MonitoringEndTime    string                `json:"monitoring_end_time"`
	RecommendationTerms  map[string]DetailTerm `json:"recommendation_terms"`
}

// DetailTerm holds plots and engine recommendations for a single term.
type DetailTerm struct {
	DurationInHours       float64        `json:"duration_in_hours"`
	Plots                 *NativePlot    `json:"plots,omitempty"`
	RecommendationEngines *DetailEngines `json:"recommendation_engines,omitempty"`
}

// DetailEngines groups cost and performance engine recommendations.
type DetailEngines struct {
	Cost        *EngineRecommendation `json:"cost,omitempty"`
	Performance *EngineRecommendation `json:"performance,omitempty"`
}

var termDurationHours = map[string]float64{
	"short_term":  24,
	"medium_term": 7 * 24,
	"long_term":   15 * 24,
}

// BuildDetailResponse maps a NativeContainerResult into the Kruize-compatible
// DetailResponse structure, embedding boxplot data and monitoring_end_time.
func BuildDetailResponse(
	native *NativeContainerResult,
	plots map[string]*NativePlot,
	monitoringEndTime time.Time,
) *DetailResponse {
	terms := make(map[string]DetailTerm)

	for termKey, termRec := range native.Recommendations {
		dt := DetailTerm{
			DurationInHours: termDurationHours[termKey],
			RecommendationEngines: &DetailEngines{
				Cost:        termRec.Cost,
				Performance: termRec.Performance,
			},
		}
		if p, ok := plots[termKey]; ok {
			dt.Plots = p
		}
		terms[termKey] = dt
	}

	var metStr string
	if !monitoringEndTime.IsZero() && monitoringEndTime.Year() > 1 {
		metStr = monitoringEndTime.UTC().Format(time.RFC3339)
	}

	return &DetailResponse{
		ID:           native.ID,
		ClusterAlias: native.ClusterAlias,
		ClusterUUID:  native.ClusterUUID,
		Container:    native.Container,
		Project:      native.Project,
		Workload:     native.Workload,
		WorkloadType: native.WorkloadType,
		SourceID:     native.SourceID,
		LastReported: native.LastReported,
		Recommendations: DetailRecommendations{
			MonitoringEndTime:   metStr,
			RecommendationTerms: terms,
		},
	}
}
