package model

import (
	"fmt"
	"strings"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"gorm.io/gorm"
)

// nativeContainerPageSortExpr returns the ORDER BY expression for container list pagination.
func nativeContainerPageSortExpr(orderByDBCol string) (sortExpr string, rsFilter string) {
	return nativeContainerSortExpr(orderByDBCol)
}

// nativeContainerDistinctOnOrder is the ORDER BY required by PostgreSQL DISTINCT ON for container pages.
func nativeContainerDistinctOnOrder(sortExpr, orderHow string) string {
	return fmt.Sprintf(
		"rs.cluster_uuid, rs.namespace, rs.workload, rs.workload_type, rs.container_name, %s %s NULLS LAST, rs.term ASC, rs.engine ASC",
		sortExpr, orderHow,
	)
}

// remapSortExprToOrgKeys translates rs.* sort columns to org_container_keys (ock) aliases.
func remapSortExprToOrgKeys(sortExpr string) string {
	s := sortExpr
	s = strings.ReplaceAll(s, "rs.namespace", "ock.namespace")
	s = strings.ReplaceAll(s, "rs.workload", "ock.workload")
	s = strings.ReplaceAll(s, "rs.workload_type", "ock.workload_type")
	s = strings.ReplaceAll(s, "rs.container_name", "ock.container_name")
	return s
}

// nativeContainerKeysDistinctOnOrder is DISTINCT ON order when paging ock joined to rs for rs-only sorts.
func nativeContainerKeysDistinctOnOrder(sortExpr, orderHow string) string {
	return fmt.Sprintf(
		"ock.cluster_uuid, ock.namespace, ock.workload, ock.workload_type, ock.container_name, %s %s NULLS LAST, rs.term ASC, rs.engine ASC",
		sortExpr, orderHow,
	)
}

// nativeContainerKeysPageOrder orders org_container_keys page selection (must match keyset seek).
func nativeContainerKeysPageOrder(sortExpr, orderHow string) string {
	return fmt.Sprintf(
		"%s %s NULLS LAST, ock.cluster_uuid, ock.namespace, ock.workload, ock.workload_type, ock.container_name",
		sortExpr, orderHow,
	)
}

// isNumericPageSort returns true when the sort expression targets a numeric DB
// column whose text representation does not sort lexicographically the same as
// the numeric value (e.g. "9" > "10" in text but 9 < 10 as integers).
func isNumericPageSort(sortExpr string) bool {
	numericSuffixes := []string{
		"_cents", "_millicores", "_kib", "_bytes", "_days", "_pct",
	}
	for _, s := range numericSuffixes {
		if strings.HasSuffix(sortExpr, s) {
			return true
		}
	}
	return false
}

// pageSortCol returns the SQL expression for ordering by ros_container_page_sort,
// applying a numeric cast when the underlying column is numeric.
func pageSortCol(alias string, numericSort bool) string {
	col := alias + ".ros_container_page_sort"
	if numericSort {
		return fmt.Sprintf("(%s)::bigint", col)
	}
	return col
}

// nativeContainerPageOrder orders the paginated container key subquery (must match keyset seek).
func nativeContainerPageOrder(pageAlias, orderHow string, numericSort bool) string {
	return fmt.Sprintf(
		"%s %s NULLS LAST, %s.cluster_uuid, %s.namespace, %s.workload, %s.workload_type, %s.container_name",
		pageSortCol(pageAlias, numericSort), orderHow, pageAlias, pageAlias, pageAlias, pageAlias, pageAlias,
	)
}

// nativeContainerDetailOrder preserves page order when expanding term/engine rows for assembly.
func nativeContainerDetailOrder(orderHow string, numericSort bool) string {
	return fmt.Sprintf(
		"%s %s NULLS LAST, page.cluster_uuid, page.namespace, page.workload, page.workload_type, page.container_name, rs.term, rs.engine",
		pageSortCol("page", numericSort), orderHow,
	)
}

// nativeContainerSeekAfter returns a WHERE clause for keyset pagination after the cursor row.
func nativeContainerSeekAfter(sortExpr, orderHow string, sortValue interface{}, clusterUUID, namespace, workload, workloadType, container string) (string, []interface{}) {
	tie := "(rs.namespace, rs.workload, rs.workload_type, rs.container_name, rs.cluster_uuid)"
	tieArgs := []interface{}{namespace, workload, workloadType, container, clusterUUID}
	if sortValue == nil {
		return fmt.Sprintf(
			"((%s) IS NULL AND %s > (?, ?, ?, ?, ?))",
			sortExpr, tie,
		), tieArgs
	}
	if orderHow == listoptions.OrderDesc {
		return fmt.Sprintf(
			"((%s) < ? OR (%s) IS NULL OR ((%s) IS NOT DISTINCT FROM ? AND %s > (?, ?, ?, ?, ?)))",
			sortExpr, sortExpr, sortExpr, tie,
		), append([]interface{}{sortValue, sortValue}, tieArgs...)
	}
	return fmt.Sprintf(
		"((%s) > ? OR (%s) IS NULL OR ((%s) IS NOT DISTINCT FROM ? AND %s > (?, ?, ?, ?, ?)))",
		sortExpr, sortExpr, sortExpr, tie,
	), append([]interface{}{sortValue, sortValue}, tieArgs...)
}

// nativeContainerSeekAfterLegacy supports cursors encoded before sort-key tie-breaking.
func nativeContainerSeekAfterLegacy(namespace, workload, workloadType, container string) (string, []interface{}) {
	return "(rs.namespace, rs.workload, rs.workload_type, rs.container_name) > (?, ?, ?, ?)",
		[]interface{}{namespace, workload, workloadType, container}
}

func applyNativeContainerPageSeek(query *gorm.DB, opts listoptions.ListOptions, sortExpr string) *gorm.DB {
	if !opts.HasCursor {
		return query
	}
	if opts.AfterContainerSortPresent {
		clause, args := nativeContainerSeekAfter(
			sortExpr, opts.OrderHow, opts.AfterContainerSortValue,
			opts.AfterContainerClusterUUID, opts.AfterNamespace, opts.AfterWorkload, opts.AfterWorkloadType, opts.AfterContainer,
		)
		return query.Where(clause, args...)
	}
	clause, args := nativeContainerSeekAfterLegacy(opts.AfterNamespace, opts.AfterWorkload, opts.AfterWorkloadType, opts.AfterContainer)
	return query.Where(clause, args...)
}

// nativeContainerKeysSeekAfter returns keyset seek predicates for org_container_keys pagination.
func nativeContainerKeysSeekAfter(sortExpr, orderHow string, sortValue interface{}, clusterUUID, namespace, workload, workloadType, container string) (string, []interface{}) {
	tie := "(ock.namespace, ock.workload, ock.workload_type, ock.container_name, ock.cluster_uuid)"
	tieArgs := []interface{}{namespace, workload, workloadType, container, clusterUUID}
	if sortValue == nil {
		return fmt.Sprintf(
			"((%s) IS NULL AND %s > (?, ?, ?, ?, ?))",
			sortExpr, tie,
		), tieArgs
	}
	if orderHow == listoptions.OrderDesc {
		return fmt.Sprintf(
			"((%s) < ? OR (%s) IS NULL OR ((%s) IS NOT DISTINCT FROM ? AND %s > (?, ?, ?, ?, ?)))",
			sortExpr, sortExpr, sortExpr, tie,
		), append([]interface{}{sortValue, sortValue}, tieArgs...)
	}
	return fmt.Sprintf(
		"((%s) > ? OR (%s) IS NULL OR ((%s) IS NOT DISTINCT FROM ? AND %s > (?, ?, ?, ?, ?)))",
		sortExpr, sortExpr, sortExpr, tie,
	), append([]interface{}{sortValue, sortValue}, tieArgs...)
}

func nativeContainerKeysSeekAfterLegacy(namespace, workload, workloadType, container string) (string, []interface{}) {
	return "(ock.namespace, ock.workload, ock.workload_type, ock.container_name) > (?, ?, ?, ?)",
		[]interface{}{namespace, workload, workloadType, container}
}

func applyNativeContainerKeysPageSeek(query *gorm.DB, opts listoptions.ListOptions, sortExpr string) *gorm.DB {
	if !opts.HasCursor {
		return query
	}
	if opts.AfterContainerSortPresent {
		clause, args := nativeContainerKeysSeekAfter(
			sortExpr, opts.OrderHow, opts.AfterContainerSortValue,
			opts.AfterContainerClusterUUID, opts.AfterNamespace, opts.AfterWorkload, opts.AfterWorkloadType, opts.AfterContainer,
		)
		return query.Where(clause, args...)
	}
	clause, args := nativeContainerKeysSeekAfterLegacy(opts.AfterNamespace, opts.AfterWorkload, opts.AfterWorkloadType, opts.AfterContainer)
	return query.Where(clause, args...)
}

// ContainerPaginationAnchor holds the sort position of the last row on a container list page.
type ContainerPaginationAnchor struct {
	SortValue     interface{}
	ClusterUUID   string
	Namespace     string
	Workload      string
	WorkloadType  string
	ContainerName string
}

// nativeContainerParseSortText converts a text sort key from the page subquery into a SQL bind value.
func nativeContainerParseSortText(sortExpr string, text *string) interface{} {
	return nativeNSParseSortText(sortExpr, text)
}
