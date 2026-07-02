package model

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/tags"
	"github.com/redhatinsights/ros-ocp-backend/internal/utils"
	"gorm.io/gorm"
)

// nativeNSKeysFilterAtoms maps namespace_recommendation_sets (ns) / clusters (c)
// filter clauses to their org_namespace_keys (onk) equivalents.
var nativeNSKeysFilterAtoms = map[string]string{
	"c.cluster_uuid = ?":                    "onk.cluster_uuid = ?",
	"c.cluster_uuid != ?":                   "onk.cluster_uuid != ?",
	"c.cluster_alias ILIKE ? ESCAPE '\\'":   "c.cluster_alias ILIKE ? ESCAPE '\\'",
	"c.cluster_alias != ?":                  "c.cluster_alias != ?",
	"ns.namespace_name ILIKE ? ESCAPE '\\'": "onk.namespace_name ILIKE ? ESCAPE '\\'",
	"ns.namespace_name = ?":                 "onk.namespace_name = ?",
	"ns.namespace_name != ?":                "onk.namespace_name != ?",
}

// nativeNSDetailOnlyQueryKeys are filters that can only apply on the detail
// (namespace_recommendation_sets) leg, not on org_namespace_keys.
var nativeNSDetailOnlyQueryKeys = map[string]struct{}{
	"ns.monitoring_end_time >= ?": {},
	"ns.monitoring_end_time < ?":  {},
	"ns.stale = ?":                {},
	"ns.engine IN ?":              {},
	"ns.term IN ?":                {},
	"ns.idle_state IN ?":          {},
}

// usesOrgNamespaceKeys returns true when the query should route through
// the org_namespace_keys path.
func usesOrgNamespaceKeys(queryParams map[string]interface{}) bool {
	stale, ok := queryParams["ns.stale = ?"]
	if !ok {
		return false
	}
	b, ok := stale.(bool)
	return ok && !b
}

// splitNativeNSListQueryParams divides query parameters into those that can
// filter org_namespace_keys (keysParams) and those that must filter the detail
// namespace_recommendation_sets rows (detailParams).
func splitNativeNSListQueryParams(queryParams map[string]interface{}) (keysParams, detailParams map[string]interface{}) {
	keysParams = make(map[string]interface{})
	detailParams = make(map[string]interface{})
	for key, values := range queryParams {
		if key == TagFiltersQueryKey {
			continue
		}
		if _, ok := nativeNSDetailOnlyQueryKeys[key]; ok {
			detailParams[key] = values
			continue
		}
		if isCompositeOfAtoms(key, nativeNSDetailOnlyQueryKeys, []string{" OR ", " AND "}) {
			detailParams[key] = values
			continue
		}
		if isAllowedNativeNamespaceQueryKey(key) {
			keysParams[key] = values
			if _, isAtom := nativeNSFilterAtoms[key]; isAtom ||
				isCompositeOfAtoms(key, nativeNSFilterAtoms, []string{" OR ", " AND "}) {
				detailParams[key] = values
			}
		}
	}
	return keysParams, detailParams
}

func remapNativeNSKeysQueryKey(key string) (string, bool) {
	if mapped, ok := nativeNSKeysFilterAtoms[key]; ok {
		return mapped, true
	}
	return remapCompositeNativeNSKeysQueryKey(key)
}

func remapCompositeNativeNSKeysQueryKey(key string) (string, bool) {
	k := strings.TrimSpace(key)
	if k == "" {
		return "", false
	}
	for len(k) >= 2 && k[0] == '(' && k[len(k)-1] == ')' {
		k = strings.TrimSpace(k[1 : len(k)-1])
	}
	for _, sep := range []string{" OR ", " AND "} {
		chunks := splitAtTopLevelSep(k, sep)
		if len(chunks) > 1 {
			remapped := make([]string, 0, len(chunks))
			for _, chunk := range chunks {
				mappedChunk, ok := remapNativeNSKeysQueryKey(chunk)
				if !ok {
					return "", false
				}
				remapped = append(remapped, mappedChunk)
			}
			return strings.Join(remapped, sep), true
		}
	}
	mapped, ok := nativeNSKeysFilterAtoms[k]
	return mapped, ok
}

// ApplyQueryParamsToNSKeys adds org_namespace_keys WHERE clauses from parsed query parameters.
func ApplyQueryParamsToNSKeys(query *gorm.DB, queryParams map[string]interface{}) *gorm.DB {
	for key, values := range queryParams {
		mappedKey, ok := remapNativeNSKeysQueryKey(key)
		if !ok {
			log.Warnf("ApplyQueryParamsToNSKeys: skipping unknown query key %q", key)
			continue
		}
		query = query.Where(mappedKey, values)
	}
	return query
}

// ApplyTagFiltersToNSKeys adds tag predicates on org_namespace_keys.
func ApplyTagFiltersToNSKeys(query *gorm.DB, orgID string, filters []TagFilter) *gorm.DB {
	if config.TagsSource() == "api" {
		return applyAPITagFiltersToNSKeys(query, filters)
	}
	return applyDBTagFiltersToNSKeys(query, orgID, filters)
}

func applyAPITagFiltersToNSKeys(query *gorm.DB, filters []TagFilter) *gorm.DB {
	for _, f := range filters {
		if f.Key == "" || len(f.Values) == 0 {
			continue
		}
		if len(f.Values) == 1 && f.Values[0] == "*" {
			query = query.Where("onk.resolved_tags ? ?", f.Key)
			continue
		}
		if len(f.Values) == 1 {
			payload, err := json.Marshal(map[string]string{f.Key: f.Values[0]})
			if err != nil {
				log.Warnf("ApplyTagFiltersToNSKeys: skipping filter %q: %v", f.Key, err)
				continue
			}
			query = query.Where("onk.resolved_tags @> ?", string(payload))
			continue
		}
		query = query.Where("onk.resolved_tags->>? IN ?", f.Key, f.Values)
	}
	return query
}

func applyDBTagFiltersToNSKeys(query *gorm.DB, orgID string, filters []TagFilter) *gorm.DB {
	schema, err := tags.TenantSchema(orgID)
	if err != nil {
		log.Warnf("ApplyTagFiltersToNSKeys: invalid org_id %q: %v", orgID, err)
		return query
	}
	tagValuesTable := pgx.Identifier{schema, "reporting_ocptags_values"}.Sanitize()

	for _, f := range filters {
		if f.Key == "" || len(f.Values) == 0 {
			continue
		}
		var matchClause string
		args := []interface{}{f.Key}
		if len(f.Values) == 1 && f.Values[0] == "*" {
			matchClause = "tv.key = ?"
		} else {
			matchClause = "tv.key = ? AND tv.value IN ?"
			args = append(args, f.Values)
		}
		existsSQL := fmt.Sprintf(`EXISTS (
			SELECT 1
			FROM %s tv,
			     unnest(tv.cluster_ids, tv.namespaces) AS t(cluster_id, namespace)
			WHERE %s
			  AND t.cluster_id = onk.cluster_uuid::text
			  AND t.namespace = onk.namespace_name
		)`, tagValuesTable, matchClause)
		query = query.Where(existsSQL, args...)
	}
	return query
}

// nativeNSSortUsesOrgKeysOnly reports whether sortExpr only references onk/c columns.
func nativeNSSortUsesOrgKeysOnly(sortExpr string) bool {
	return !strings.Contains(sortExpr, "ns.")
}

// remapNSSortExprToOrgKeys translates ns.* sort columns to org_namespace_keys (onk) aliases.
func remapNSSortExprToOrgKeys(sortExpr string) string {
	s := sortExpr
	s = strings.ReplaceAll(s, "ns.namespace_name", "onk.namespace_name")
	s = strings.ReplaceAll(s, "ns.cluster_uuid", "onk.cluster_uuid")
	return s
}

// remapNSSortExprToNRS translates ns.* sort columns to the nrs alias used
// when the keys path joins namespace_recommendation_sets.
func remapNSSortExprToNRS(sortExpr string) string {
	return strings.ReplaceAll(sortExpr, "ns.", "nrs.")
}

// applyNativeNamespaceKeysRBAC adds RBAC-based WHERE clauses for namespace keys queries.
func applyNativeNamespaceKeysRBAC(query *gorm.DB, userPerms map[string][]string) *gorm.DB {
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
		query = query.Where("onk.namespace_name IN (?)", projectPerms)
	}
	return query
}

const nativeNSKeysNRSJoin = `JOIN namespace_recommendation_sets nrs ON nrs.org_id = onk.org_id
		AND nrs.cluster_uuid = onk.cluster_uuid
		AND nrs.namespace_name = onk.namespace_name
		AND nrs.term IS NOT NULL
		AND nrs.schedule_type = 'all_hours'`

// getNativeNamespaceRecommendationsFromOrgKeys uses org_namespace_keys for count
// and page selection, then joins namespace_recommendation_sets for full detail.
func getNativeNamespaceRecommendationsFromOrgKeys(
	gdb *gorm.DB,
	orgID string,
	opts listoptions.ListOptions,
	queryParams map[string]interface{},
	userPerms map[string][]string,
) (NativeNamespaceListPage, error) {
	db := gdb
	keysParams, detailParams := splitNativeNSListQueryParams(queryParams)

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

	// 1. Count query on org_namespace_keys
	countQuery := db.Table("org_namespace_keys onk").
		Select("onk.cluster_uuid, onk.namespace_name").
		Joins("JOIN clusters c ON c.cluster_uuid = onk.cluster_uuid").
		Where("onk.org_id = ?", orgID)
	countQuery = applyNativeNamespaceKeysRBAC(countQuery, userPerms)
	countQuery = ApplyQueryParamsToNSKeys(countQuery, keysParams)
	if tagFilters := TagFiltersFromParams(queryParams); len(tagFilters) > 0 {
		countQuery = ApplyTagFiltersToNSKeys(countQuery, orgID, tagFilters)
	}

	// 2. Page keys query
	pageKeys := db.Table("org_namespace_keys onk").
		Joins("JOIN clusters c ON c.cluster_uuid = onk.cluster_uuid").
		Where("onk.org_id = ?", orgID)
	pageKeys = applyNativeNamespaceKeysRBAC(pageKeys, userPerms)
	pageKeys = ApplyQueryParamsToNSKeys(pageKeys, keysParams)
	if tagFilters := TagFiltersFromParams(queryParams); len(tagFilters) > 0 {
		pageKeys = ApplyTagFiltersToNSKeys(pageKeys, orgID, tagFilters)
	}

	keysOnlySort := nativeNSSortUsesOrgKeysOnly(sortExpr)
	pageSortExpr := sortExpr
	if keysOnlySort {
		pageSortExpr = remapNSSortExprToOrgKeys(sortExpr)
	} else {
		pageKeys = pageKeys.Joins(nativeNSKeysNRSJoin)
		pageSortExpr = remapNSSortExprToNRS(sortExpr)
	}

	selectPrefix := ""
	if !keysOnlySort {
		selectPrefix = "DISTINCT ON (onk.cluster_uuid, onk.namespace_name) "
	}
	pageKeys = pageKeys.Select(fmt.Sprintf(
		"%sonk.cluster_uuid, onk.namespace_name, (%s)::text AS ros_ns_page_sort",
		selectPrefix, pageSortExpr,
	))

	if keysOnlySort {
		pageKeys = pageKeys.Order(nativeNSKeysPageOrder("onk", pageSortExpr, orderHow))
		pageKeys = applyNativeNSKeysPageSeek(pageKeys, opts, pageSortExpr, orderHow)
	} else {
		pageKeys = pageKeys.Order(nativeNSKeysDistinctOnOrder(pageSortExpr, orderHow))
	}

	pageSubquery := db.Table("(?) AS page", pageKeys).
		Select("page.cluster_uuid, page.namespace_name, page.ros_ns_page_sort").
		Order(nativeNSKeysSubqueryPageOrder(orderHow))
	pageSubquery = applyNativeNSPageSeekOnPageKeys(pageSubquery, opts, orderHow)
	if !opts.HasCursor {
		pageSubquery = pageSubquery.Offset(opts.Offset)
	}
	pageSubquery = pageSubquery.Limit(pageLimit)

	// 3. Detail query
	var rows []NativeNamespaceRow
	t0 := time.Now().UTC()

	detailQuery := db.Table("namespace_recommendation_sets ns").
		Select(nativeNSSelect + ", page.ros_ns_page_sort").
		Joins(`JOIN clusters c ON c.cluster_uuid = ns.cluster_uuid`).
		Joins(`JOIN (?) page ON page.cluster_uuid = ns.cluster_uuid AND page.namespace_name = ns.namespace_name`, pageSubquery).
		Where("ns.org_id = ?", orgID).
		Where("ns.term IS NOT NULL").
		Where("ns.schedule_type = 'all_hours'")
	detailQuery = applyNSQueryParams(detailQuery, detailParams)
	err := detailQuery.Order(nativeNSKeysDetailOrder(orderHow)).Find(&rows).Error
	if err != nil {
		return NativeNamespaceListPage{}, err
	}

	results := assembleNativeNamespaceResults(rows, sortExpr, false)

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

	totalNamespaces, err := resolveOrgNamespaceCount(orgID, db, countQuery)
	if err != nil {
		return NativeNamespaceListPage{}, err
	}
	log.Infof("native namespace list query (keys path): %dms (%d namespaces)", time.Since(t0).Milliseconds(), totalNamespaces)

	return NativeNamespaceListPage{
		Results:    results,
		Count:      int(totalNamespaces),
		HasNext:    hasNext,
		LastAnchor: lastAnchor,
	}, nil
}

// nativeNSKeysDetailOrder preserves page order for the keys path detail query.
// Uses ros_ns_page_sort (text cast) instead of ros_ns_page_sort_raw (typed) because
// the keys path page subquery only selects ros_ns_page_sort.
func nativeNSKeysDetailOrder(orderHow string) string {
	nulls := "NULLS FIRST"
	if orderHow == listoptions.OrderDesc {
		nulls = "NULLS LAST"
	}
	return fmt.Sprintf("page.ros_ns_page_sort %s %s, page.cluster_uuid, page.namespace_name, ns.term, ns.engine", orderHow, nulls)
}

// nativeNSKeysPageOrder orders org_namespace_keys rows for direct pagination (identity sorts).
func nativeNSKeysPageOrder(alias, sortExpr, orderHow string) string {
	nulls := "NULLS FIRST"
	if orderHow == listoptions.OrderDesc {
		nulls = "NULLS LAST"
	}
	return fmt.Sprintf("%s %s %s, %s.cluster_uuid, %s.namespace_name",
		sortExpr, orderHow, nulls, alias, alias)
}

// nativeNSKeysDistinctOnOrder is the ORDER BY required by DISTINCT ON when the
// sort expression requires a JOIN to namespace_recommendation_sets.
func nativeNSKeysDistinctOnOrder(sortExpr, orderHow string) string {
	nulls := "NULLS FIRST"
	if orderHow == listoptions.OrderDesc {
		nulls = "NULLS LAST"
	}
	return fmt.Sprintf(
		"onk.cluster_uuid, onk.namespace_name, %s %s %s, nrs.term ASC, nrs.engine ASC",
		sortExpr, orderHow, nulls,
	)
}

// nativeNSKeysSubqueryPageOrder orders the page subquery after DISTINCT ON deduplication.
func nativeNSKeysSubqueryPageOrder(orderHow string) string {
	nulls := "NULLS FIRST"
	if orderHow == listoptions.OrderDesc {
		nulls = "NULLS LAST"
	}
	return fmt.Sprintf(
		"page.ros_ns_page_sort %s %s, page.cluster_uuid, page.namespace_name",
		orderHow, nulls,
	)
}

// applyNativeNSKeysPageSeek applies cursor-based seek on org_namespace_keys for identity sorts.
func applyNativeNSKeysPageSeek(query *gorm.DB, opts listoptions.ListOptions, sortExpr, orderHow string) *gorm.DB {
	if !opts.HasCursor {
		return query
	}
	if opts.AfterNSSortPresent {
		tie := "(onk.cluster_uuid, onk.namespace_name)"
		sortValue := opts.AfterNSSortValue
		clusterUUID := opts.AfterNSClusterUUID
		namespace := opts.AfterNamespaceName

		var clause string
		var args []interface{}
		if orderHow == listoptions.OrderDesc {
			if sortValue == nil {
				clause = fmt.Sprintf("((%s) IS NULL AND %s > (?, ?))", sortExpr, tie)
				args = []interface{}{clusterUUID, namespace}
			} else {
				clause = fmt.Sprintf(
					"((%s) < ? OR (%s) IS NULL OR ((%s) IS NOT DISTINCT FROM ? AND %s > (?, ?)))",
					sortExpr, sortExpr, sortExpr, tie,
				)
				args = []interface{}{sortValue, sortValue, clusterUUID, namespace}
			}
		} else {
			clause = fmt.Sprintf(
				"((%s) > ? OR ((%s) IS NOT DISTINCT FROM ? AND %s > (?, ?)))",
				sortExpr, sortExpr, tie,
			)
			args = []interface{}{sortValue, sortValue, clusterUUID, namespace}
		}
		return query.Where(clause, args...)
	}
	return query.Where("(onk.namespace_name, onk.cluster_uuid) > (?, ?)", opts.AfterNamespaceName, opts.AfterNSClusterUUID)
}

// applyNativeNSPageSeekOnPageKeys applies cursor-based seek on the page subquery for the keys path.
func applyNativeNSPageSeekOnPageKeys(query *gorm.DB, opts listoptions.ListOptions, orderHow string) *gorm.DB {
	if !opts.HasCursor {
		return query
	}
	if opts.AfterNSSortPresent {
		sortCol := "page.ros_ns_page_sort"
		tie := "(page.cluster_uuid, page.namespace_name)"
		sortValue := opts.AfterNSSortValue
		clusterUUID := opts.AfterNSClusterUUID
		namespace := opts.AfterNamespaceName

		var clause string
		var args []interface{}
		if orderHow == listoptions.OrderDesc {
			if sortValue == nil {
				clause = fmt.Sprintf("((%s) IS NULL AND %s > (?, ?))", sortCol, tie)
				args = []interface{}{clusterUUID, namespace}
			} else {
				clause = fmt.Sprintf(
					"((%s) < ? OR (%s) IS NULL OR ((%s) IS NOT DISTINCT FROM ? AND %s > (?, ?)))",
					sortCol, sortCol, sortCol, tie,
				)
				args = []interface{}{sortValue, sortValue, clusterUUID, namespace}
			}
		} else {
			clause = fmt.Sprintf(
				"((%s) > ? OR ((%s) IS NOT DISTINCT FROM ? AND %s > (?, ?)))",
				sortCol, sortCol, tie,
			)
			args = []interface{}{sortValue, sortValue, clusterUUID, namespace}
		}
		return query.Where(clause, args...)
	}
	return query.Where("(page.namespace_name, page.cluster_uuid) > (?, ?)", opts.AfterNamespaceName, opts.AfterNSClusterUUID)
}
