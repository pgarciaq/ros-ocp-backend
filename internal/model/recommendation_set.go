package model

import (
	"encoding/json"
	"errors"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
	kruizeplugin "github.com/redhatinsights/ros-ocp-backend/internal/plugins/kruize"
	"github.com/redhatinsights/ros-ocp-backend/internal/rbac"
)

type RecommendationSet struct {
	// Composite primary key columns (migration 000028). Legacy Kruize responses are stored
	// as one JSON blob per container using term=short, engine=cost.
	OrgID         string `gorm:"column:org_id;primaryKey"`
	ClusterUUID   string `gorm:"column:cluster_uuid;primaryKey"`
	Namespace     string `gorm:"column:namespace;primaryKey"`
	Workload      string `gorm:"column:workload;primaryKey"`
	ContainerName string `gorm:"column:container_name;primaryKey"`
	Term          string `gorm:"column:term;primaryKey"`
	Engine        string `gorm:"column:engine;primaryKey"`

	WorkloadID   uint   `gorm:"column:workload_id"`
	WorkloadType string `gorm:"column:workload_type"`

	CPURequestCurrent    *float64 `gorm:"column:cpu_request_current;type:numeric(10,4)"`
	MemoryRequestCurrent *float64 `gorm:"column:memory_request_current;type:numeric(20,4)"`

	// Variation fields: percent of current CPU/memory request (aligned with API response).
	CPUVariationShortCostPct            *float64 `gorm:"column:cpu_variation_short_cost_pct;type:numeric(10,4)"`
	CPUVariationShortPerformancePct     *float64 `gorm:"column:cpu_variation_short_performance_pct;type:numeric(10,4)"`
	CPUVariationMediumCostPct           *float64 `gorm:"column:cpu_variation_medium_cost_pct;type:numeric(10,4)"`
	CPUVariationMediumPerformancePct    *float64 `gorm:"column:cpu_variation_medium_performance_pct;type:numeric(10,4)"`
	CPUVariationLongCostPct             *float64 `gorm:"column:cpu_variation_long_cost_pct;type:numeric(10,4)"`
	CPUVariationLongPerformancePct      *float64 `gorm:"column:cpu_variation_long_performance_pct;type:numeric(10,4)"`
	MemoryVariationShortCostPct         *float64 `gorm:"column:memory_variation_short_cost_pct;type:numeric(10,4)"`
	MemoryVariationShortPerformancePct  *float64 `gorm:"column:memory_variation_short_performance_pct;type:numeric(10,4)"`
	MemoryVariationMediumCostPct        *float64 `gorm:"column:memory_variation_medium_cost_pct;type:numeric(10,4)"`
	MemoryVariationMediumPerformancePct *float64 `gorm:"column:memory_variation_medium_performance_pct;type:numeric(10,4)"`
	MemoryVariationLongCostPct          *float64 `gorm:"column:memory_variation_long_cost_pct;type:numeric(10,4)"`
	MemoryVariationLongPerformancePct   *float64 `gorm:"column:memory_variation_long_performance_pct;type:numeric(10,4)"`

	MonitoringStartTime    time.Time `gorm:"type:timestamp"`
	MonitoringEndTime      time.Time `gorm:"type:timestamp"`
	Recommendations        datatypes.JSON
	UpdatedAt              time.Time `gorm:"type:timestamp"`
	MonitoringStartTimeStr string    `gorm:"-"`
	MonitoringEndTimeStr   string    `gorm:"-"`
	UpdatedAtStr           string    `gorm:"-"`
}

type RecommendationSetResult struct {
	/*
		Intended to be an API-ready struct.
		Updated recommendation data is saved to RecommendationsJSON before the API response is sent.
	*/
	ClusterAlias        string                 `json:"cluster_alias"`
	ClusterUUID         string                 `json:"cluster_uuid"`
	Container           string                 `json:"container"`
	ID                  string                 `json:"id"`
	LastReported        string                 `json:"last_reported"`
	Project             string                 `json:"project"`
	Recommendations     datatypes.JSON         `json:"-"`
	RecommendationsJSON map[string]interface{} `gorm:"-" json:"recommendations"`
	SourceID            string                 `json:"source_id"`
	Workload            string                 `json:"workload"`
	WorkloadType        string                 `json:"workload_type"`
	AnalyticsIncomplete bool                   `json:"analytics_incomplete,omitempty"`
	AnalyticsIncompleteAt *string              `json:"analytics_incomplete_at,omitempty"`
	// Embedded stored variation percentages (scanned from SELECT, excluded from JSON output).
	StoredVariationPcts `gorm:"embedded"`
	// Embedded typed sibling-row inputs for read-time synthesis (#599 option 2).
	SynthDBRow `gorm:"embedded"`
}

func (r *RecommendationSet) AfterFind(tx *gorm.DB) error {
	r.MonitoringEndTimeStr = r.MonitoringEndTime.Format(time.RFC3339)
	return nil
}

func GetFirstRecommendationSetsByWorkloadID(workload_id uint) (RecommendationSet, error) {
	recommendationSets := RecommendationSet{}
	db := database.GetDB()
	query := db.Where("workload_id = ?", workload_id).First(&recommendationSets)
	if query.Error != nil && errors.Is(query.Error, gorm.ErrRecordNotFound) {
		return recommendationSets, nil
	}
	return recommendationSets, query.Error
}

func (r *RecommendationSet) GetRecommendationSets(orgID string, opts listoptions.ListOptions, queryParams map[string]interface{}, user_permissions map[string][]string) ([]RecommendationSetResult, int, error) {
	var recommendationSets []RecommendationSetResult
	var count int64 = 0
	query := getRecommendationQuery(orgID)

	if err := rbac.AddRBACFilter(
		query,
		user_permissions,
		rbac.ResourceContainer,
	); err != nil {
		return recommendationSets, int(count), err
	}

	for key, values := range queryParams {
		switch v := values.(type) {
		case []string:
			// Convert []string to []interface{} for unpacking multiple values
			args := make([]interface{}, len(v))
			for i, s := range v {
				args[i] = s
			}
			query = query.Where(key, args...)
		default:
			query = query.Where(key, v)
		}
	}

	// #607 collapse, two-phase paging over short/cost representative rows
	// (locked: same primary rec as detail, CSV, and pin). Containers
	// lacking a short/cost row (partial writes only — the pipeline emits
	// all six) are skipped and counted on
	// rosocp_compat_collapse_skipped_containers_total.
	var allCount int64 = 0
	if err := query.Session(&gorm.Session{}).Distinct("recommendation_sets.container_id").Count(&allCount).Error; err != nil {
		return recommendationSets, 0, err
	}
	pinned := query.Session(&gorm.Session{}).Where(
		"recommendation_sets.term IN ? AND recommendation_sets.engine = ?",
		[]string{"short", "short_term"}, "cost",
	)
	if err := pinned.Session(&gorm.Session{}).Distinct("recommendation_sets.container_id").Count(&count).Error; err != nil {
		return recommendationSets, 0, err
	}
	metrics.IncCompatCollapseSkipped("container", int(allCount)-int(count))

	// OrderBy/OrderHow come from ListAPIOptions (allowlisted); secondary sort for stable ordering.
	limit := opts.Limit
	if opts.Format == "csv" {
		/*
		 each collapsed row carries all short, medium, long term recommendations
		 each such term recommendation has two types, cost and performance
		 total number of CSV rows would be RecordLimitCSV * 3 * 2
		*/
		limit = config.GetConfig().RecordLimitCSV
	}
	var keys []RecommendationSetResult
	err := pinned.Session(&gorm.Session{}).
		Order(listoptions.SQLOrderByFragment(opts.OrderBy, opts.OrderHow)).
		Order("recommendation_sets.container_id ASC").
		Offset(opts.Offset).
		Limit(limit).
		Scan(&keys).Error
	if err != nil {
		return recommendationSets, 0, err
	}
	if len(keys) == 0 {
		return recommendationSets, int(count), nil
	}

	ids := make([]string, 0, len(keys))
	for _, k := range keys {
		ids = append(ids, k.ID)
	}
	siblings, err := r.GetRecommendationSiblingRowsByContainers(orgID, ids, user_permissions)
	if err != nil {
		return recommendationSets, 0, err
	}
	byContainer := make(map[string][]kruizeplugin.SynthDBRow, len(keys))
	for i := range siblings {
		s := &siblings[i]
		byContainer[s.ID] = append(byContainer[s.ID], s.SynthDBRow)
	}
	for i := range keys {
		blob := kruizeplugin.SynthesizeKruizeJSON(kruizeplugin.SynthInputsFromRows(byContainer[keys[i].ID]))
		if len(blob) == 0 {
			continue
		}
		raw, merr := json.Marshal(blob)
		if merr != nil {
			continue
		}
		keys[i].Recommendations = datatypes.JSON(raw)
	}

	return keys, int(count), nil
}

func (r *RecommendationSet) GetRecommendationSetByID(orgID string, recommendationID string, user_permissions map[string][]string) (RecommendationSetResult, error) {
	var recommendationSet RecommendationSetResult

	query := getRecommendationQuery(orgID)
	query = query.Where("recommendation_sets.container_id = ?", recommendationID)
	// Legacy contract: one row per container (short-term, cost engine).
	// container_id excludes term/engine, so pin the legacy variant
	// deterministically; both term vocabs ('short' kruize, 'short_term'
	// native) sort short-first. (#596)
	query = query.Where("recommendation_sets.engine = ?", "cost")
	query = query.Order(`CASE recommendation_sets.term
		WHEN 'short' THEN 0 WHEN 'short_term' THEN 1
		WHEN 'medium' THEN 2 WHEN 'medium_term' THEN 3
		WHEN 'long' THEN 4 WHEN 'long_term' THEN 5
		ELSE 6 END`)

	if err := rbac.AddRBACFilter(
		query,
		user_permissions,
		rbac.ResourceContainer,
	); err != nil {
		return recommendationSet, err
	}

	err := query.Take(&recommendationSet).Error
	return recommendationSet, err
}

// GetRecommendationSiblingRows fetches all term/engine rows for one
// container (#599 Phase 3a: detail synthesis input). Siblings share the
// container_id by construction; same org scoping + RBAC as the detail row.
func (r *RecommendationSet) GetRecommendationSiblingRows(orgID string, recommendationID string, user_permissions map[string][]string) ([]RecommendationSetResult, error) {
	var rows []RecommendationSetResult

	query := getRecommendationQuery(orgID)
	query = query.Where("recommendation_sets.container_id = ?", recommendationID)

	if err := rbac.AddRBACFilter(
		query,
		user_permissions,
		rbac.ResourceContainer,
	); err != nil {
		return rows, err
	}

	err := query.Order("recommendation_sets.term ASC, recommendation_sets.engine ASC").Scan(&rows).Error
	return rows, err
}

// GetRecommendationSiblingRowsByContainers fetches all term/engine rows
// for a page of containers in one query (#607: no N+1). Same org scoping
// and RBAC as the detail row; sibling rows share namespace/cluster with
// their container, so scoping evaluates identically.
func (r *RecommendationSet) GetRecommendationSiblingRowsByContainers(orgID string, recommendationIDs []string, user_permissions map[string][]string) ([]RecommendationSetResult, error) {
	var rows []RecommendationSetResult
	if len(recommendationIDs) == 0 {
		return rows, nil
	}

	query := getRecommendationQuery(orgID)
	query = query.Where("recommendation_sets.container_id IN ?", recommendationIDs)

	if err := rbac.AddRBACFilter(
		query,
		user_permissions,
		rbac.ResourceContainer,
	); err != nil {
		return rows, err
	}

	err := query.Order("recommendation_sets.term ASC, recommendation_sets.engine ASC").Scan(&rows).Error
	return rows, err
}

func (r *RecommendationSet) CreateRecommendationSet(tx *gorm.DB) error {
	result := tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "org_id"},
			{Name: "cluster_uuid"},
			{Name: "namespace"},
			{Name: "workload"},
			{Name: "container_name"},
			{Name: "term"},
			{Name: "engine"},
		},
		DoUpdates: clause.AssignmentColumns([]string{
			"workload_id",
			"workload_type",
			"monitoring_start_time",
			"monitoring_end_time",
			"recommendations",
			"updated_at",
			"cpu_request_current",
			"memory_request_current",
			"cpu_variation_short_cost_pct",
			"cpu_variation_short_performance_pct",
			"cpu_variation_medium_cost_pct",
			"cpu_variation_medium_performance_pct",
			"cpu_variation_long_cost_pct",
			"cpu_variation_long_performance_pct",
			"memory_variation_short_cost_pct",
			"memory_variation_short_performance_pct",
			"memory_variation_medium_cost_pct",
			"memory_variation_medium_performance_pct",
			"memory_variation_long_cost_pct",
			"memory_variation_long_performance_pct",
		}),
	}).Create(r)

	if result.Error != nil {
		dbError.Inc()
		return result.Error
	}

	return nil
}
