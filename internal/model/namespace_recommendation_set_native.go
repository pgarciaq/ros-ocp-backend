package model

import (
	"fmt"
	"strings"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/notifications"
	"github.com/redhatinsights/ros-ocp-backend/internal/utils"
	"gorm.io/gorm"
)

// remapNativeNSOrderBy translates legacy full-table-name column references
// (from NsAllowedOrderBy) to the alias-based column references used in the
// native namespace query (ns. / c. prefixes).
func remapNativeNSOrderBy(col string) string {
	col = strings.Replace(col, "namespace_recommendation_sets.", "ns.", 1)
	col = strings.Replace(col, "clusters.", "c.", 1)
	return col
}

// NativeNamespaceRow represents a single row from namespace_recommendation_sets
// using the native relational columns (one row per namespace+term+engine).
type NativeNamespaceRow struct {
	OrgID         string `gorm:"column:org_id"`
	ClusterUUID   string `gorm:"column:cluster_uuid"`
	NamespaceName string `gorm:"column:namespace_name"`
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

	VariationCPURequestPct *int32        `gorm:"column:variation_cpu_request_pct"`
	VariationCPULimitPct   *int32        `gorm:"column:variation_cpu_limit_pct"`
	VariationMemRequestPct *int32        `gorm:"column:variation_memory_request_pct"`
	VariationMemLimitPct   *int32        `gorm:"column:variation_memory_limit_pct"`
	ConfidenceLevel        *float32      `gorm:"column:confidence_level"`
	NotificationCodes      SmallintArray `gorm:"column:notification_codes;type:smallint[]"`
	Stale                  bool          `gorm:"column:stale"`
	IdleState              string        `gorm:"column:idle_state"`

	MonitoringEndTime *time.Time `gorm:"column:monitoring_end_time"`
	UpdatedAt         time.Time  `gorm:"column:updated_at"`
	SourceID          string     `gorm:"column:source_id"`
	ClusterAlias      string     `gorm:"column:cluster_alias"`
	LastReported      time.Time  `gorm:"column:last_reported_at"`

	// PageSortText is the list sort key as text from the pagination subquery (not an API field).
	PageSortText *string `gorm:"column:ros_ns_page_sort"`
}

func (NativeNamespaceRow) TableName() string {
	return "namespace_recommendation_sets"
}

// NativeNamespaceResult is the API-ready format for a single namespace,
// with all recommendation variants nested by term. The Recommendations map
// uses `any` values because it contains both TermRecommendation objects
// (keyed by "short_term", "medium_term", "long_term") and string metadata
// ("monitoring_end_time") to match the legacy response format.
type NativeNamespaceResult struct {
	ID              string         `json:"id"`
	ClusterAlias    string         `json:"cluster_alias"`
	ClusterUUID     string         `json:"cluster_uuid"`
	Project         string         `json:"project"`
	SourceID        string         `json:"source_id"`
	LastReported    string         `json:"last_reported"`
	IdleState       string         `json:"idle_state"`
	Recommendations map[string]any `json:"recommendations"`

	// PaginationSort is the list order-by value for this namespace (not serialized).
	PaginationSort interface{} `json:"-"`
}

const nativeNSSelect = `ns.org_id, ns.cluster_uuid, ns.namespace_name, ns.term, ns.engine,
	ns.rec_cpu_request_millicores, ns.rec_cpu_limit_millicores,
	ns.rec_memory_request_kib, ns.rec_memory_limit_kib,
	ns.current_cpu_request_millicores, ns.current_cpu_limit_millicores,
	ns.current_memory_request_kib, ns.current_memory_limit_kib,
	ns.variation_cpu_request_pct, ns.variation_cpu_limit_pct,
	ns.variation_memory_request_pct, ns.variation_memory_limit_pct,
	ns.notification_codes, ns.confidence_level, ns.stale, ns.idle_state,
	ns.monitoring_end_time, ns.updated_at,
	c.source_id, c.cluster_alias, c.last_reported_at`

// GetNativeNamespaceRecommendations queries the native relational columns from
// namespace_recommendation_sets.
func GetNativeNamespaceRecommendations(orgID string, opts listoptions.ListOptions, queryParams map[string]interface{}, userPerms map[string][]string) (NativeNamespaceListPage, error) {
	db := database.GetDB()

	query := db.Table("namespace_recommendation_sets ns").
		Select(nativeNSSelect).
		Joins(`JOIN clusters c ON c.cluster_uuid = ns.cluster_uuid`).
		Where("ns.org_id = ?", orgID).
		Where("ns.term IS NOT NULL").
		Where("ns.schedule_type = 'all_hours'")

	query = applyNativeNamespaceRBAC(query, userPerms)
	query = applyNSQueryParams(query, queryParams)
	if tagFilters := TagFiltersFromParams(queryParams); len(tagFilters) > 0 {
		query = ApplyTagFiltersToClusterNamespace(query, orgID, tagFilters, "ns.cluster_uuid", "ns.namespace_name")
	}

	limit := opts.Limit
	if opts.Format == "csv" {
		limit = config.GetConfig().RecordLimitCSV
	}
	pageLimit := limit + 1

	sortExpr := nativeNSPageSortExpr(opts.OrderBy)
	orderHow := opts.OrderHow
	if orderHow == "" {
		orderHow = listoptions.OrderDesc
	}

	distinctNS := db.Table("namespace_recommendation_sets ns").
		Select(fmt.Sprintf(
			"DISTINCT ON (ns.cluster_uuid, ns.namespace_name) ns.cluster_uuid, ns.namespace_name, (%s)::text AS ros_ns_page_sort",
			sortExpr,
		)).
		Joins(`JOIN clusters c ON c.cluster_uuid = ns.cluster_uuid`).
		Where("ns.org_id = ?", orgID).
		Where("ns.term IS NOT NULL").
		Where("ns.schedule_type = 'all_hours'")
	distinctNS = applyNativeNamespaceRBAC(distinctNS, userPerms)
	distinctNS = applyNSQueryParams(distinctNS, queryParams)
	if tagFilters := TagFiltersFromParams(queryParams); len(tagFilters) > 0 {
		distinctNS = ApplyTagFiltersToClusterNamespace(distinctNS, orgID, tagFilters, "ns.cluster_uuid", "ns.namespace_name")
	}
	distinctNS = distinctNS.Order(nativeNSDistinctOnOrder(sortExpr, orderHow))
	distinctNS = applyNativeNSPageSeek(distinctNS, opts, sortExpr)

	countDistinct := db.Table("(?) AS dn", distinctNS).
		Select("dn.cluster_uuid, dn.namespace_name")

	pageSubquery := db.Table("(?) AS page", distinctNS).
		Select("page.cluster_uuid, page.namespace_name, page.ros_ns_page_sort").
		Order(nativeNSPageOrder("page", orderHow))
	if !opts.HasCursor {
		pageSubquery = pageSubquery.Offset(opts.Offset)
	}
	pageSubquery = pageSubquery.Limit(pageLimit)

	var rows []NativeNamespaceRow
	t0 := time.Now().UTC()

	err := query.
		Joins(`JOIN (?) page ON page.cluster_uuid = ns.cluster_uuid AND page.namespace_name = ns.namespace_name`, pageSubquery).
		Select(nativeNSSelect + ", page.ros_ns_page_sort").
		Order(nativeNSDetailOrder(orderHow)).
		Find(&rows).Error
	if err != nil {
		return NativeNamespaceListPage{}, err
	}

	results := assembleNativeNamespaceResults(rows, sortExpr)

	hasNext := len(results) > limit
	var lastAnchor *NamespacePaginationAnchor
	if hasNext {
		last := results[limit-1]
		lastAnchor = &NamespacePaginationAnchor{
			SortValue:   last.PaginationSort,
			ClusterUUID: last.ClusterUUID,
			Namespace:   last.Project,
		}
		results = results[:limit]
	}

	totalNamespaces, err := resolveOrgNamespaceCount(orgID, db, countDistinct)
	if err != nil {
		return NativeNamespaceListPage{}, err
	}
	log.Infof("native namespace list query: %dms (%d namespaces)", time.Since(t0).Milliseconds(), totalNamespaces)

	return NativeNamespaceListPage{
		Results:    results,
		Count:      int(totalNamespaces),
		HasNext:    hasNext,
		LastAnchor: lastAnchor,
	}, nil
}

func resolveOrgNamespaceCount(orgID string, db *gorm.DB, filteredDistinct *gorm.DB) (int64, error) {
	if filteredDistinct != nil {
		var total int64
		if err := db.Table("(?) AS dn", filteredDistinct).Count(&total).Error; err != nil {
			return 0, err
		}
		return total, nil
	}

	if count, ok, err := GetOrgNamespaceCount(orgID); err != nil {
		return 0, err
	} else if ok {
		return count, nil
	}

	var total int64
	if err := db.Table("namespace_recommendation_sets ns").
		Joins(`JOIN clusters c ON c.cluster_uuid = ns.cluster_uuid`).
		Where("ns.org_id = ?", orgID).
		Where("ns.term IS NOT NULL").
		Where("ns.schedule_type = 'all_hours'").
		Distinct("ns.cluster_uuid", "ns.namespace_name").
		Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

// GetNativeNamespaceRecommendationByID fetches a single namespace's
// recommendations by its deterministic UUID.
func GetNativeNamespaceRecommendationByID(orgID, id string, userPerms map[string][]string) (*NativeNamespaceResult, error) {
	db := database.GetDB()

	query := nativeNamespaceDetailQuery(db, orgID, id, userPerms)

	var rows []NativeNamespaceRow
	if err := query.Order("ns.term, ns.engine").Find(&rows).Error; err != nil {
		return nil, err
	}

	if len(rows) > 0 {
		results := assembleNativeNamespaceResults(rows, "")
		if len(results) > 0 {
			return &results[0], nil
		}
	}

	// Fallback: scan namespace keys and match by UUID v5.
	return getNativeNamespaceByIDFallback(db, orgID, id, userPerms)
}

// nativeNamespaceDetailQuery builds the primary detail lookup for a namespace recommendation.
// orgID is required: recommendation IDs are deterministic UUID v5 values that do not encode tenant scope.
func nativeNamespaceDetailQuery(db *gorm.DB, orgID, id string, userPerms map[string][]string) *gorm.DB {
	query := db.Table("namespace_recommendation_sets ns").
		Select(nativeNSSelect).
		Joins(`JOIN clusters c ON c.cluster_uuid = ns.cluster_uuid`).
		Where("ns.org_id = ?", orgID).
		Where("ns.namespace_id = ?", id).
		Where("ns.term IS NOT NULL").
		Where("ns.schedule_type = 'all_hours'").
		Where("ns.stale = false")
	return applyNativeNamespaceRBAC(query, userPerms)
}

func getNativeNamespaceByIDFallback(db *gorm.DB, orgID, id string, userPerms map[string][]string) (*NativeNamespaceResult, error) {
	log.Warnf("namespace_id miss for %s in org %s — using fallback scan", id, orgID)

	type nsKey struct {
		ClusterUUID   string `gorm:"column:cluster_uuid"`
		NamespaceName string `gorm:"column:namespace_name"`
	}

	keysQuery := db.Table("namespace_recommendation_sets ns").
		Select("DISTINCT ns.cluster_uuid, ns.namespace_name").
		Joins(`JOIN clusters c ON c.cluster_uuid = ns.cluster_uuid`).
		Where("ns.org_id = ?", orgID).
		Where("ns.term IS NOT NULL").
		Where("ns.schedule_type = 'all_hours'").
		Where("ns.stale = false").
		Limit(500)
	keysQuery = applyNativeNamespaceRBAC(keysQuery, userPerms)

	var keys []nsKey
	if err := keysQuery.Find(&keys).Error; err != nil {
		return nil, err
	}

	var matched *nsKey
	for i := range keys {
		if NativeNamespaceID(keys[i].ClusterUUID, keys[i].NamespaceName) == id {
			matched = &keys[i]
			break
		}
	}
	if matched == nil {
		return nil, nil
	}

	var rows []NativeNamespaceRow
	err := db.Table("namespace_recommendation_sets ns").
		Select(nativeNSSelect).
		Joins(`JOIN clusters c ON c.cluster_uuid = ns.cluster_uuid`).
		Where("ns.org_id = ?", orgID).
		Where("ns.cluster_uuid = ?", matched.ClusterUUID).
		Where("ns.namespace_name = ?", matched.NamespaceName).
		Where("ns.term IS NOT NULL").
		Where("ns.schedule_type = 'all_hours'").
		Where("ns.stale = false").
		Order("ns.term, ns.engine").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}

	results := assembleNativeNamespaceResults(rows, "")
	if len(results) == 0 {
		return nil, nil
	}
	return &results[0], nil
}

// assembleNativeNamespaceResults groups flat rows into nested NativeNamespaceResult structs.
func assembleNativeNamespaceResults(rows []NativeNamespaceRow, sortExpr string) []NativeNamespaceResult {
	type nsKey struct {
		ClusterUUID   string
		NamespaceName string
	}

	orderKeys := []nsKey{}
	grouped := map[nsKey][]NativeNamespaceRow{}

	for _, r := range rows {
		key := nsKey{r.ClusterUUID, r.NamespaceName}
		if _, exists := grouped[key]; !exists {
			orderKeys = append(orderKeys, key)
		}
		grouped[key] = append(grouped[key], r)
	}

	var results []NativeNamespaceResult
	for _, key := range orderKeys {
		rowGroup := grouped[key]
		first := rowGroup[0]

		idleState := first.IdleState
		if idleState == "" {
			idleState = "active"
		}
		result := NativeNamespaceResult{
			ID:              NativeNamespaceID(first.ClusterUUID, first.NamespaceName),
			ClusterAlias:    first.ClusterAlias,
			ClusterUUID:     first.ClusterUUID,
			Project:         first.NamespaceName,
			SourceID:        first.SourceID,
			LastReported:    first.LastReported.Format(time.RFC3339),
			IdleState:       idleState,
			Recommendations: make(map[string]any),
			PaginationSort:  nativeNSParseSortText(sortExpr, first.PageSortText),
		}

		if first.MonitoringEndTime != nil {
			result.Recommendations["monitoring_end_time"] = first.MonitoringEndTime.Format(time.RFC3339)
		}

		terms := map[string]TermRecommendation{}
		for _, r := range rowGroup {
			termKey := r.Term + "_term"
			term := terms[termKey]

			codes := r.NotificationCodes
			if codes == nil {
				codes = SmallintArray{}
			}
			notifMap := notifications.MapToKruizeFormat(r.NotificationCodes)

			eng := &EngineRecommendation{
				CPURequestMillicores:   r.RecCPURequestMC,
				CPULimitMillicores:     r.RecCPULimitMC,
				MemRequestKiB:          r.RecMemRequestKiB,
				MemLimitKiB:            r.RecMemLimitKiB,
				CurrentCPURequestMC:    r.CurrentCPURequestMC,
				CurrentCPULimitMC:      r.CurrentCPULimitMC,
				CurrentMemRequestKiB:   r.CurrentMemRequestKiB,
				CurrentMemLimitKiB:     r.CurrentMemLimitKiB,
				VariationCPURequestPct: r.VariationCPURequestPct,
				VariationCPULimitPct:   r.VariationCPULimitPct,
				VariationMemRequestPct: r.VariationMemRequestPct,
				VariationMemLimitPct:   r.VariationMemLimitPct,
				ConfidenceLevel:        r.ConfidenceLevel,
				NotificationCodes:      codes,
				Notifications:          notifMap,
			}

			switch r.Engine {
			case "cost":
				term.Cost = eng
			case "performance":
				term.Performance = eng
			}

			terms[termKey] = term
		}

		for k, v := range terms {
			result.Recommendations[k] = v
		}

		results = append(results, result)
	}

	return results
}

// applyNativeNamespaceRBAC adds RBAC-based WHERE clauses for namespace queries.
func applyNativeNamespaceRBAC(query *gorm.DB, userPerms map[string][]string) *gorm.DB {
	cfg := config.GetConfig()
	if !cfg.RBACEnabled {
		return query
	}
	if _, ok := userPerms["*"]; ok {
		return query
	}

	clusterPerms, hasCluster := userPerms["openshift.cluster"]
	projectPerms, hasProject := userPerms["openshift.project"]
	clusterAll := hasCluster && utils.StringInSlice("*", clusterPerms)
	projectAll := hasProject && utils.StringInSlice("*", projectPerms)

	if hasCluster && !clusterAll {
		query = query.Where("c.cluster_uuid IN (?)", clusterPerms)
	}
	if hasProject && !projectAll {
		query = query.Where("ns.namespace_name IN (?)", projectPerms)
	}
	return query
}

// applyNSQueryParams adds dynamic WHERE clauses from parsed namespace query parameters.
func applyNSQueryParams(query *gorm.DB, queryParams map[string]interface{}) *gorm.DB {
	for key, values := range queryParams {
		if key == TagFiltersQueryKey {
			continue
		}
		if !isAllowedNativeNamespaceQueryKey(key) {
			log.Warnf("applyNSQueryParams: skipping unknown query key %q", key)
			continue
		}
		query = query.Where(key, values)
	}
	return query
}
