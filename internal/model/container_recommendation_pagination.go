package model

import (
	"fmt"

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
		"rs.cluster_uuid, rs.namespace, rs.workload, rs.workload_type, rs.container_name, %s %s, rs.term ASC, rs.engine ASC",
		sortExpr, orderHow,
	)
}

// nativeContainerPageOrder orders the paginated container key subquery (must match keyset seek).
func nativeContainerPageOrder(pageAlias, orderHow string) string {
	return fmt.Sprintf(
		"%s.ros_container_page_sort %s, %s.cluster_uuid, %s.namespace, %s.workload, %s.workload_type, %s.container_name",
		pageAlias, orderHow, pageAlias, pageAlias, pageAlias, pageAlias, pageAlias,
	)
}

// nativeContainerDetailOrder preserves page order when expanding term/engine rows for assembly.
func nativeContainerDetailOrder(orderHow string) string {
	return fmt.Sprintf(
		"page.ros_container_page_sort %s, page.cluster_uuid, page.namespace, page.workload, page.workload_type, page.container_name, rs.term, rs.engine",
		orderHow,
	)
}

// nativeContainerSeekAfter returns a WHERE clause for keyset pagination after the cursor row.
func nativeContainerSeekAfter(sortExpr, orderHow string, sortValue interface{}, clusterUUID, namespace, workload, workloadType, container string) (string, []interface{}) {
	tie := "(rs.namespace, rs.workload, rs.workload_type, rs.container_name, rs.cluster_uuid)"
	if orderHow == listoptions.OrderDesc {
		return fmt.Sprintf(
			"((%s) < ? OR ((%s) IS NOT DISTINCT FROM ? AND %s > (?, ?, ?, ?, ?)))",
			sortExpr, sortExpr, tie,
		), []interface{}{sortValue, sortValue, namespace, workload, workloadType, container, clusterUUID}
	}
	return fmt.Sprintf(
		"((%s) > ? OR ((%s) IS NOT DISTINCT FROM ? AND %s > (?, ?, ?, ?, ?)))",
		sortExpr, sortExpr, tie,
	), []interface{}{sortValue, sortValue, namespace, workload, workloadType, container, clusterUUID}
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
