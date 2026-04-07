package model

import (
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/utils"
	"gorm.io/gorm"
)

// NativeRecommendationRow represents a single row from recommendation_sets
// using the new relational columns (one row per container+term+engine).
type NativeRecommendationRow struct {
	OrgID         string `gorm:"column:org_id"`
	ClusterUUID   string `gorm:"column:cluster_uuid"`
	Namespace     string `gorm:"column:namespace"`
	Workload      string `gorm:"column:workload"`
	WorkloadType  string `gorm:"column:workload_type"`
	ContainerName string `gorm:"column:container_name"`
	Term          string `gorm:"column:term"`
	Engine        string `gorm:"column:engine"`

	RecCPURequestMC  *int64 `gorm:"column:rec_cpu_request_millicores"`
	RecCPULimitMC    *int64 `gorm:"column:rec_cpu_limit_millicores"`
	RecMemRequestKiB *int64 `gorm:"column:rec_memory_request_kib"`
	RecMemLimitKiB   *int64 `gorm:"column:rec_memory_limit_kib"`

	CurrentCPURequestMC  *int64 `gorm:"column:current_cpu_request_millicores"`
	CurrentCPULimitMC    *int64 `gorm:"column:current_cpu_limit_millicores"`
	CurrentMemRequestKiB *int64 `gorm:"column:current_memory_request_kib"`
	CurrentMemLimitKiB   *int64 `gorm:"column:current_memory_limit_kib"`

	VariationCPURequestPct *float32 `gorm:"column:variation_cpu_request_pct"`
	VariationMemRequestPct *float32 `gorm:"column:variation_memory_request_pct"`
	ConfidenceLevel        *float32 `gorm:"column:confidence_level"`
	NotificationCodes      []int16  `gorm:"column:notification_codes;type:smallint[]"`
	Stale                  bool     `gorm:"column:stale"`

	UpdatedAt    time.Time `gorm:"column:updated_at"`
	SourceID     string    `gorm:"column:source_id"`
	ClusterAlias string    `gorm:"column:cluster_alias"`
	LastReported time.Time `gorm:"column:last_reported_at"`
}

func (NativeRecommendationRow) TableName() string {
	return "recommendation_sets"
}

// NativeContainerResult is the API-ready format for a single container,
// with all 6 recommendation variants nested.
type NativeContainerResult struct {
	ClusterAlias    string                        `json:"cluster_alias"`
	ClusterUUID     string                        `json:"cluster_uuid"`
	Container       string                        `json:"container"`
	Project         string                        `json:"project"`
	Workload        string                        `json:"workload"`
	WorkloadType    string                        `json:"workload_type"`
	SourceID        string                        `json:"source_id"`
	LastReported    string                        `json:"last_reported"`
	Recommendations map[string]TermRecommendation `json:"recommendations"`
}

// TermRecommendation holds cost and performance recommendations for a term.
type TermRecommendation struct {
	Cost        *EngineRecommendation `json:"cost,omitempty"`
	Performance *EngineRecommendation `json:"performance,omitempty"`
}

// EngineRecommendation holds the actual CPU/memory recommendation values.
type EngineRecommendation struct {
	CPURequestMillicores *int64   `json:"cpu_request_millicores,omitempty"`
	CPULimitMillicores   *int64   `json:"cpu_limit_millicores,omitempty"`
	MemRequestKiB        *int64   `json:"memory_request_kib,omitempty"`
	MemLimitKiB          *int64   `json:"memory_limit_kib,omitempty"`
	ConfidenceLevel      *float32 `json:"confidence_level,omitempty"`
}

// GetNativeRecommendations queries the native relational columns from recommendation_sets.
func GetNativeRecommendations(orgID string, opts listoptions.ListOptions, queryParams map[string]interface{}, userPerms map[string][]string) ([]NativeContainerResult, int, error) {
	db := database.GetDB()

	query := db.Table("recommendation_sets rs").
		Select(`rs.org_id, rs.cluster_uuid, rs.namespace, rs.workload, rs.workload_type,
			rs.container_name, rs.term, rs.engine,
			rs.rec_cpu_request_millicores, rs.rec_cpu_limit_millicores,
			rs.rec_memory_request_kib, rs.rec_memory_limit_kib,
			rs.confidence_level, rs.stale, rs.updated_at,
			c.source_id, c.cluster_alias, c.last_reported_at`).
		Joins(`JOIN clusters c ON c.cluster_uuid = rs.cluster_uuid`).
		Joins(`JOIN rh_accounts ra ON ra.id = c.tenant_id`).
		Where("ra.org_id = ?", orgID).
		Where("rs.stale = false")

	applyNativeRBAC(query, userPerms)

	for key, values := range queryParams {
		switch v := values.(type) {
		case []string:
			args := make([]interface{}, len(v))
			for i, s := range v {
				args[i] = s
			}
			query = query.Where(key, args...)
		default:
			query = query.Where(key, v)
		}
	}

	var rows []NativeRecommendationRow
	err := query.
		Order("rs.namespace, rs.workload, rs.container_name, rs.term, rs.engine").
		Offset(opts.Offset * 6).Limit(opts.Limit * 6).
		Find(&rows).Error
	if err != nil {
		return nil, 0, err
	}

	results := assembleNativeResults(rows)
	return results, len(results), nil
}

// applyNativeRBAC adds RBAC-based WHERE clauses using the native schema's column names.
func applyNativeRBAC(query *gorm.DB, userPerms map[string][]string) {
	cfg := config.GetConfig()
	if !cfg.RBACEnabled {
		return
	}
	if _, ok := userPerms["*"]; ok {
		return
	}

	clusterPerms, hasCluster := userPerms["openshift.cluster"]
	projectPerms, hasProject := userPerms["openshift.project"]
	clusterAll := hasCluster && utils.StringInSlice("*", clusterPerms)
	projectAll := hasProject && utils.StringInSlice("*", projectPerms)

	if hasCluster && !clusterAll {
		query.Where("c.cluster_uuid IN (?)", clusterPerms)
	}
	if hasProject && !projectAll {
		query.Where("rs.namespace IN (?)", projectPerms)
	}
}

// assembleNativeResults groups flat rows into nested NativeContainerResult structs.
func assembleNativeResults(rows []NativeRecommendationRow) []NativeContainerResult {
	type containerKey struct {
		ClusterUUID   string
		Namespace     string
		Workload      string
		ContainerName string
	}

	orderKeys := []containerKey{}
	grouped := map[containerKey][]NativeRecommendationRow{}

	for _, r := range rows {
		key := containerKey{r.ClusterUUID, r.Namespace, r.Workload, r.ContainerName}
		if _, exists := grouped[key]; !exists {
			orderKeys = append(orderKeys, key)
		}
		grouped[key] = append(grouped[key], r)
	}

	var results []NativeContainerResult
	for _, key := range orderKeys {
		rowGroup := grouped[key]
		first := rowGroup[0]

		result := NativeContainerResult{
			ClusterAlias:    first.ClusterAlias,
			ClusterUUID:     first.ClusterUUID,
			Container:       first.ContainerName,
			Project:         first.Namespace,
			Workload:        first.Workload,
			WorkloadType:    first.WorkloadType,
			SourceID:        first.SourceID,
			LastReported:    first.LastReported.Format(time.RFC3339),
			Recommendations: make(map[string]TermRecommendation),
		}

		for _, r := range rowGroup {
			termKey := r.Term + "_term"
			term, exists := result.Recommendations[termKey]
			if !exists {
				term = TermRecommendation{}
			}

			eng := &EngineRecommendation{
				CPURequestMillicores: r.RecCPURequestMC,
				CPULimitMillicores:   r.RecCPULimitMC,
				MemRequestKiB:        r.RecMemRequestKiB,
				MemLimitKiB:          r.RecMemLimitKiB,
				ConfidenceLevel:      r.ConfidenceLevel,
			}

			switch r.Engine {
			case "cost":
				term.Cost = eng
			case "performance":
				term.Performance = eng
			}

			result.Recommendations[termKey] = term
		}

		results = append(results, result)
	}

	return results
}
