package model

import (
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/money"
)

// ListResponse is the slim Kruize-compatible list item. It preserves the JSON
// fields the list UI reads (current config, short_term cost variation,
// notification_codes) while omitting plots, business_hours, duration_in_hours,
// and per-engine notification maps.
type ListResponse struct {
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
	Tags                    map[string]string             `json:"tags,omitempty"`
	Recommendations         ListRecommendations           `json:"recommendations"`
	GPU                     map[string]*GPURecommendation `json:"gpu,omitempty"`
}

// ListRecommendations wraps list-level recommendation data.
type ListRecommendations struct {
	Current                 *DetailResourceConfig `json:"current,omitempty"`
	Replicas                *ReplicaInfo        `json:"replicas,omitempty"`
	EstimatedMonthlySavings *money.MoneyAmount  `json:"estimated_monthly_savings,omitempty"`
	CPUSavings              *money.MoneyAmount  `json:"cpu_savings,omitempty"`
	MemorySavings           *money.MoneyAmount  `json:"memory_savings,omitempty"`
	MonitoringEndTime       string              `json:"monitoring_end_time"`
	NotificationCodes       []int16             `json:"notification_codes,omitempty"`
	RecommendationTerms     map[string]ListTerm `json:"recommendation_terms"`
}

// ListTerm holds engine recommendations for a single term in list responses.
type ListTerm struct {
	RecommendationEngines *ListEngines `json:"recommendation_engines,omitempty"`
}

// ListEngines groups cost and performance engine recommendations for list views.
type ListEngines struct {
	Cost        *DetailEngine `json:"cost,omitempty"`
	Performance *DetailEngine `json:"performance,omitempty"`
}

// ListResponseOptions narrows list payload assembly to match request filters.
type ListResponseOptions struct {
	TermFilter   string // API term key, e.g. short_term
	EngineFilter string // cost or performance
}

// BuildListResponse maps a NativeContainerResult into a slim list DTO. When the
// native payload is unfiltered it includes short_term cost only in
// recommendation_terms; term/engine query filters narrow the included terms.
func BuildListResponse(native *NativeContainerResult, monitoringEndTime time.Time, opts ListResponseOptions) *ListResponse {
	var replicas *ReplicaInfo
	if native.Replicas != nil {
		replicas = native.Replicas
	}

	var current *DetailResourceConfig
	for _, termRec := range native.Recommendations {
		if current == nil {
			current = extractCurrent(termRec.Cost)
		}
		if current == nil {
			current = extractCurrent(termRec.Performance)
		}
		if current != nil {
			break
		}
	}

	met := monitoringEndTime
	if met.IsZero() {
		met = native.MonitoringEndTime
	}
	var metStr string
	if !met.IsZero() && met.Year() > 1 {
		metStr = met.UTC().Format(time.RFC3339)
	}

	recs := ListRecommendations{
		Current:                 current,
		Replicas:                replicas,
		EstimatedMonthlySavings: native.EstimatedMonthlySavings,
		CPUSavings:              native.CPUSavings,
		MemorySavings:           native.MemorySavings,
		MonitoringEndTime:       metStr,
		RecommendationTerms:     buildListRecommendationTerms(native.Recommendations, opts),
	}
	if codes := aggregateNotificationCodes(native.Recommendations); len(codes) > 0 {
		recs.NotificationCodes = codes
	}

	resp := &ListResponse{
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
		Tags:                  native.Tags,
		Recommendations:       recs,
		GPU:                   native.GPU,
	}
	if resp.IdleState == "" {
		resp.IdleState = "active"
	}
	return resp
}

func buildListRecommendationTerms(terms map[string]TermRecommendation, opts ListResponseOptions) map[string]ListTerm {
	if len(terms) == 0 {
		return nil
	}

	if opts.TermFilter != "" {
		termRec, ok := terms[opts.TermFilter]
		if !ok {
			return nil
		}
		return map[string]ListTerm{
			opts.TermFilter: buildListTerm(termRec, opts.EngineFilter),
		}
	}

	if opts.EngineFilter != "" {
		result := make(map[string]ListTerm, len(terms))
		for termKey, termRec := range terms {
			result[termKey] = buildListTerm(termRec, opts.EngineFilter)
		}
		return result
	}

	if len(terms) == 1 {
		result := make(map[string]ListTerm, 1)
		for termKey, termRec := range terms {
			result[termKey] = buildListTerm(termRec, "")
		}
		return result
	}

	if single := detectSingleEngineFilter(terms); single != "" {
		result := make(map[string]ListTerm, len(terms))
		for termKey, termRec := range terms {
			result[termKey] = buildListTerm(termRec, single)
		}
		return result
	}

	termRec, ok := terms["short_term"]
	if !ok {
		return nil
	}
	return map[string]ListTerm{
		"short_term": buildListTerm(termRec, "cost"),
	}
}

func buildListTerm(termRec TermRecommendation, engineFilter string) ListTerm {
	engines := &ListEngines{}
	if engineFilter == "" || engineFilter == "cost" {
		if eng := toListDetailEngine(termRec.Cost); eng != nil {
			engines.Cost = eng
		}
	}
	if engineFilter == "" || engineFilter == "performance" {
		if eng := toListDetailEngine(termRec.Performance); eng != nil {
			engines.Performance = eng
		}
	}
	if engines.Cost == nil && engines.Performance == nil {
		return ListTerm{}
	}
	return ListTerm{RecommendationEngines: engines}
}

func toListDetailEngine(eng *EngineRecommendation) *DetailEngine {
	de := toDetailEngine(eng)
	if de == nil {
		return nil
	}
	de.Notifications = nil
	de.BusinessHours = nil
	return de
}

func detectSingleEngineFilter(terms map[string]TermRecommendation) string {
	hasCost := false
	hasPerformance := false
	for _, termRec := range terms {
		if termRec.Cost != nil {
			hasCost = true
		}
		if termRec.Performance != nil {
			hasPerformance = true
		}
	}
	switch {
	case hasCost && !hasPerformance:
		return "cost"
	case hasPerformance && !hasCost:
		return "performance"
	default:
		return ""
	}
}

// NamespaceListResponse is the slim Kruize-compatible namespace list item. It
// preserves the JSON fields the projects table reads while omitting plots,
// duration_in_hours, business_hours, and per-engine notification maps.
type NamespaceListResponse struct {
	ID              string                       `json:"id"`
	ClusterAlias    string                       `json:"cluster_alias"`
	ClusterUUID     string                       `json:"cluster_uuid"`
	Project         string                       `json:"project"`
	SourceID        string                       `json:"source_id"`
	LastReported    string                       `json:"last_reported"`
	IdleState       string                       `json:"idle_state,omitempty"`
	Recommendations NamespaceListRecommendations `json:"recommendations"`
}

// NamespaceListRecommendations wraps namespace list-level recommendation data.
type NamespaceListRecommendations struct {
	Current             *DetailResourceConfig `json:"current,omitempty"`
	MonitoringEndTime   string                `json:"monitoring_end_time"`
	NotificationCodes   []int16               `json:"notification_codes,omitempty"`
	RecommendationTerms map[string]ListTerm   `json:"recommendation_terms"`
}

// BuildNamespaceListResponse maps a NativeNamespaceResult into a slim list DTO.
// When the native payload is unfiltered it includes short_term cost only in
// recommendation_terms; term/engine query filters narrow the included terms.
func BuildNamespaceListResponse(native *NativeNamespaceResult, opts ListResponseOptions) *NamespaceListResponse {
	terms := namespaceTermsFromNative(native)

	var current *DetailResourceConfig
	for _, termRec := range terms {
		if current == nil {
			current = extractCurrent(termRec.Cost)
		}
		if current == nil {
			current = extractCurrent(termRec.Performance)
		}
		if current != nil {
			break
		}
	}

	recs := NamespaceListRecommendations{
		Current:             current,
		MonitoringEndTime:   namespaceMonitoringEndTime(native),
		RecommendationTerms: buildListRecommendationTerms(terms, opts),
	}
	if codes := aggregateNotificationCodes(terms); len(codes) > 0 {
		recs.NotificationCodes = codes
	}

	resp := &NamespaceListResponse{
		ID:              native.ID,
		ClusterAlias:    native.ClusterAlias,
		ClusterUUID:     native.ClusterUUID,
		Project:         native.Project,
		SourceID:        native.SourceID,
		LastReported:    native.LastReported,
		IdleState:       native.IdleState,
		Recommendations: recs,
	}
	if resp.IdleState == "" {
		resp.IdleState = "active"
	}
	return resp
}

func namespaceTermsFromNative(native *NativeNamespaceResult) map[string]TermRecommendation {
	if native == nil || len(native.Recommendations) == 0 {
		return nil
	}
	terms := make(map[string]TermRecommendation)
	for termKey, termVal := range native.Recommendations {
		if termKey == "monitoring_end_time" {
			continue
		}
		termRec, ok := termVal.(TermRecommendation)
		if !ok {
			continue
		}
		terms[termKey] = termRec
	}
	return terms
}

func namespaceMonitoringEndTime(native *NativeNamespaceResult) string {
	if native == nil {
		return ""
	}
	if v, ok := native.Recommendations["monitoring_end_time"].(string); ok {
		return v
	}
	return ""
}

func aggregateNotificationCodes(terms map[string]TermRecommendation) []int16 {
	seen := make(map[int16]struct{})
	var codes []int16
	for _, termRec := range terms {
		for _, eng := range []*EngineRecommendation{termRec.Cost, termRec.Performance} {
			if eng == nil {
				continue
			}
			for _, code := range eng.NotificationCodes {
				if _, ok := seen[code]; ok {
					continue
				}
				seen[code] = struct{}{}
				codes = append(codes, code)
			}
		}
	}
	return codes
}
