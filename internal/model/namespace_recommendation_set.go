package model

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
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

type NamespaceRecommendationSet struct {
	ID                   string `gorm:"primaryKey;not null;autoIncrement"`
	OrgID                string `gorm:"type:text;not null"`
	WorkloadID           uint
	Workload             Workload `gorm:"foreignKey:WorkloadID"`
	NamespaceName        string
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
	CreatedAt              time.Time `gorm:"type:timestamp with time zone;not null;default:now();<-:create"`
	UpdatedAt              time.Time `gorm:"type:timestamp"`
	MonitoringStartTimeStr string    `gorm:"-"`
	MonitoringEndTimeStr   string    `gorm:"-"`
	UpdatedAtStr           string    `gorm:"-"`
}

type NamespaceRecommendationSetResult struct {
	ClusterAlias        string         `json:"cluster_alias"`
	ClusterUUID         string         `json:"cluster_uuid"`
	ID                  string         `json:"id"`
	LastReported        string         `json:"last_reported"`
	Project             string         `json:"project"`
	Recommendations     datatypes.JSON `json:"-"`
	RecommendationsJSON map[string]any `gorm:"-" json:"recommendations"`
	SourceID            string         `json:"source_id"`
	// Embedded stored variation percentages (scanned from SELECT, excluded from JSON output).
	StoredVariationPcts `gorm:"embedded"`
	// Embedded typed sibling-row inputs for read-time synthesis (#599 Phase 5).
	SynthDBRow `gorm:"embedded"`
	// ConfidenceLevel is the native per-row engine confidence, selected for
	// the result. The synthesized legacy blob omits it (the shared
	// synthesizer has no confidence path — same divergence as pods_count).
	ConfidenceLevel *float64 `gorm:"column:confidence_level" json:"-"`
}

func (r *NamespaceRecommendationSet) AfterFind(tx *gorm.DB) error {
	r.MonitoringEndTimeStr = r.MonitoringEndTime.Format(time.RFC3339)
	return nil
}

func (r *NamespaceRecommendationSet) GetNamespaceRecommendationSets(orgID string, opts listoptions.ListOptions, queryParams map[string]interface{}, user_permissions map[string][]string) ([]NamespaceRecommendationSetResult, int, error) {
	var recommendationSets []NamespaceRecommendationSetResult
	var count int64 = 0
	query := getNamespaceRecommendationQuery(orgID)

	if err := rbac.AddRBACFilter(
		query,
		user_permissions,
		rbac.ResourceProject,
	); err != nil {
		return recommendationSets, int(count), err
	}

	tagFilters := TagFiltersFromParams(queryParams)
	if len(tagFilters) > 0 {
		query = ApplyTagFiltersToClusterNamespace(
			query, orgID, tagFilters, "clusters.cluster_uuid", "namespace_recommendation_sets.namespace_name",
		)
	}
	delete(queryParams, TagFiltersQueryKey)

	for key, values := range queryParams {
		switch v := values.(type) {
		case []string:
			args := make([]any, len(v))
			for i, s := range v {
				args[i] = s
			}
			query = query.Where(key, args...)
		default:
			query = query.Where(key, v)
		}
	}

	// #607 collapse, two-phase paging over short/cost representative rows
	// (#599 Phase 5: same primary rec as detail and pin). Namespaces
	// lacking a short/cost row (partial writes only) are skipped and
	// counted on rosocp_compat_collapse_skipped_total{entity="namespace"}.
	var allCount int64 = 0
	if err := query.Session(&gorm.Session{}).
		// GORM's Count only builds COUNT(DISTINCT(x)) for single-column
		// selects; a composite key needs the explicit row constructor.
		Select("COUNT(DISTINCT (namespace_recommendation_sets.cluster_uuid, namespace_recommendation_sets.namespace_name))").
		Row().Scan(&allCount); err != nil {
		return recommendationSets, 0, err
	}
	pinned := query.Session(&gorm.Session{}).Where(
		"namespace_recommendation_sets.term IN ? AND namespace_recommendation_sets.engine = ?",
		[]string{"short", "short_term"}, "cost",
	)
	if err := pinned.Session(&gorm.Session{}).
		Select("COUNT(DISTINCT (namespace_recommendation_sets.cluster_uuid, namespace_recommendation_sets.namespace_name))").
		Row().Scan(&count); err != nil {
		return recommendationSets, 0, err
	}
	metrics.IncCompatCollapseSkipped("namespace", int(allCount)-int(count))

	// OrderBy/OrderHow come from ListAPIOptions (allowlisted); secondary sort for stable ordering.
	limit := opts.Limit
	if opts.Format == "csv" {
		limit = config.GetConfig().RecordLimitCSV
	}
	var keys []NamespaceRecommendationSetResult
	err := pinned.Session(&gorm.Session{}).
		Order(listoptions.SQLOrderByFragment(opts.OrderBy, opts.OrderHow)).
		// #608: tiebreak must match the native keyset order
		// (cluster_uuid, namespace_name) so compat and native serve the same
		// elements under tied sort keys (last_reported_at ties across a
		// cluster). id ASC is kept only as a final determinism tiebreak for
		// the degenerate case of two pinned rows sharing a namespace pair.
		Order("namespace_recommendation_sets.cluster_uuid ASC, namespace_recommendation_sets.namespace_name ASC, namespace_recommendation_sets.id ASC").
		Offset(opts.Offset).
		Limit(limit).
		Scan(&keys).Error
	if err != nil {
		return recommendationSets, 0, err
	}
	if len(keys) == 0 {
		return recommendationSets, int(count), nil
	}

	// One batch fetches every term/engine sibling row for the page (#599
	// Phase 5: no N+1), then each namespace's blob is synthesized from its
	// six typed rows exactly like the container collapse (#607).
	pairs := make([][]interface{}, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, []interface{}{k.ClusterUUID, k.Project})
	}
	siblings, err := r.GetNamespaceRecommendationSiblingRowsByNamespaces(orgID, pairs, user_permissions)
	if err != nil {
		return recommendationSets, 0, err
	}
	byNamespace := make(map[string][]kruizeplugin.SynthDBRow, len(keys))
	for i := range siblings {
		s := &siblings[i]
		key := s.ClusterUUID + "\x00" + s.Project
		byNamespace[key] = append(byNamespace[key], s.SynthDBRow)
	}
	for i := range keys {
		blob := kruizeplugin.SynthesizeKruizeJSON(kruizeplugin.SynthInputsFromRows(
			byNamespace[keys[i].ClusterUUID+"\x00"+keys[i].Project]))
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

// GetNamespaceRecommendationSiblingRowsByNamespaces fetches all term/engine
// rows for a page of (cluster_uuid, namespace_name) keys in one query
// (#599 Phase 5: no N+1). Same org scoping and RBAC as the collapsed list;
// siblings share the key with their namespace, so scoping evaluates
// identically.
func (r *NamespaceRecommendationSet) GetNamespaceRecommendationSiblingRowsByNamespaces(orgID string, pairs [][]interface{}, user_permissions map[string][]string) ([]NamespaceRecommendationSetResult, error) {
	var rows []NamespaceRecommendationSetResult
	if len(pairs) == 0 {
		return rows, nil
	}

	query := getNamespaceRecommendationQuery(orgID)
	query = query.Where("(namespace_recommendation_sets.cluster_uuid, namespace_recommendation_sets.namespace_name) IN ?", pairs)

	if err := rbac.AddRBACFilter(
		query,
		user_permissions,
		rbac.ResourceProject,
	); err != nil {
		return rows, err
	}

	err := query.Order("namespace_recommendation_sets.term ASC, namespace_recommendation_sets.engine ASC").Scan(&rows).Error
	return rows, err
}

// pinNamespaceRecommendationRow pins the legacy contract row for a
// namespace detail: cost engine, deterministic term preference (both term
// vocabs — 'short' native, 'short_term' legacy — sort short-first).
// Mirrors the container pin. (#599 Phase 5)
func pinNamespaceRecommendationRow(query *gorm.DB) *gorm.DB {
	query = query.Where("namespace_recommendation_sets.engine = ?", "cost")
	return query.Order(`CASE namespace_recommendation_sets.term
		WHEN 'short' THEN 0 WHEN 'short_term' THEN 1
		WHEN 'medium' THEN 2 WHEN 'medium_term' THEN 3
		WHEN 'long' THEN 4 WHEN 'long_term' THEN 5
		ELSE 6 END`)
}

func (r *NamespaceRecommendationSet) GetNamespaceRecommendationSetByID(orgID string, recommendationID string, user_permissions map[string][]string) (NamespaceRecommendationSetResult, error) {
	var nsRecommendationSet NamespaceRecommendationSetResult

	// namespace_id-first resolution (#599 Phase 5): native rows serve
	// namespace_id as the public id; rows without namespace_id (legacy
	// writes) resolve by primary key. The uuid parse guard keeps the
	// indexed namespace_id comparison safe (a non-uuid predicate would
	// raise a uuid cast error).
	if _, perr := uuid.Parse(recommendationID); perr == nil {
		query := pinNamespaceRecommendationRow(getNamespaceRecommendationQuery(orgID).
			Where("namespace_recommendation_sets.namespace_id = ?", recommendationID))
		if err := rbac.AddRBACFilter(
			query,
			user_permissions,
			rbac.ResourceProject,
		); err != nil {
			return nsRecommendationSet, err
		}
		if err := query.Take(&nsRecommendationSet).Error; err == nil || !errors.Is(err, gorm.ErrRecordNotFound) {
			return nsRecommendationSet, err
		}
	}

	// PK fallback.
	legacy := pinNamespaceRecommendationRow(getNamespaceRecommendationQuery(orgID).
		Where("namespace_recommendation_sets.id::text = ?", recommendationID))
	if err := rbac.AddRBACFilter(
		legacy,
		user_permissions,
		rbac.ResourceProject,
	); err != nil {
		return nsRecommendationSet, err
	}
	err := legacy.Take(&nsRecommendationSet).Error
	return nsRecommendationSet, err
}

// GetNamespaceRecommendationSiblingRows fetches all term/engine rows for
// one namespace (#599 Phase 5: detail synthesis input). Siblings share the
// (cluster_uuid, namespace_name) key; same org scoping + RBAC as the detail
// row. Mirrors GetRecommendationSiblingRows.
func (r *NamespaceRecommendationSet) GetNamespaceRecommendationSiblingRows(orgID string, clusterUUID string, namespaceName string, user_permissions map[string][]string) ([]NamespaceRecommendationSetResult, error) {
	var rows []NamespaceRecommendationSetResult

	query := getNamespaceRecommendationQuery(orgID)
	query = query.Where("namespace_recommendation_sets.cluster_uuid = ?", clusterUUID).
		Where("namespace_recommendation_sets.namespace_name = ?", namespaceName)

	if err := rbac.AddRBACFilter(
		query,
		user_permissions,
		rbac.ResourceProject,
	); err != nil {
		return rows, err
	}

	err := query.Order("namespace_recommendation_sets.term ASC, namespace_recommendation_sets.engine ASC").Scan(&rows).Error
	return rows, err
}

func (r *NamespaceRecommendationSet) CreateNamespaceRecommendationSet(tx *gorm.DB) error {
	result := tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "workload_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
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

func GetFirstNamespaceRecommendationSetsByWorkloadID(workload_id uint) (NamespaceRecommendationSet, error) {
	namespaceRecommendationSets := NamespaceRecommendationSet{}
	db := database.GetDB()
	query := db.Where("workload_id = ?", workload_id).First(&namespaceRecommendationSets)
	if query.Error != nil && errors.Is(query.Error, gorm.ErrRecordNotFound) {
		return namespaceRecommendationSets, nil
	}
	return namespaceRecommendationSets, query.Error
}
