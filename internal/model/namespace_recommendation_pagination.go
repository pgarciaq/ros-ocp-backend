package model

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"gorm.io/gorm"
)

// nativeNSPageSortExpr returns the ORDER BY expression for namespace list pagination.
func nativeNSPageSortExpr(orderByDBCol string) string {
	if orderByDBCol == "" {
		orderByDBCol = listoptions.DefaultNsRecsDBColumn
	}
	return remapNativeNSOrderBy(orderByDBCol)
}

// nativeNSDistinctOnOrder is the ORDER BY required by PostgreSQL DISTINCT ON for namespace pages.
func nativeNSDistinctOnOrder(sortExpr, orderHow string) string {
	nulls := "NULLS FIRST"
	if orderHow == listoptions.OrderDesc {
		nulls = "NULLS LAST"
	}
	return fmt.Sprintf(
		"ns.cluster_uuid, ns.namespace_name, %s %s %s, ns.term ASC, ns.engine ASC",
		sortExpr, orderHow, nulls,
	)
}

// nativeNSPageOrder orders the paginated namespace key subquery (must match keyset seek).
func nativeNSPageOrder(pageAlias, orderHow string) string {
	nulls := "NULLS FIRST"
	if orderHow == listoptions.OrderDesc {
		nulls = "NULLS LAST"
	}
	return fmt.Sprintf(
		"%s.ros_ns_page_sort_raw %s %s, %s.cluster_uuid, %s.namespace_name",
		pageAlias, orderHow, nulls, pageAlias, pageAlias,
	)
}

// nativeNSDetailOrder preserves page order when expanding term/engine rows for assembly.
func nativeNSDetailOrder(orderHow string) string {
	nulls := "NULLS FIRST"
	if orderHow == listoptions.OrderDesc {
		nulls = "NULLS LAST"
	}
	return fmt.Sprintf("page.ros_ns_page_sort_raw %s %s, page.cluster_uuid, page.namespace_name, ns.term, ns.engine", orderHow, nulls)
}

// nativeNSSeekAfter returns a WHERE clause for keyset pagination after the cursor row.
// sortExpr must be a trusted SQL fragment (from remapNativeNSOrderBy / allowlist).
//
// When ORDER BY is DESC NULLS LAST, rows with NULL sort values come after all
// non-NULL rows.  The seek condition must reflect this: if the cursor's sort
// value is NULL we only need to advance by the tie-breaker; if it is non-NULL
// we must also accept rows whose sort value IS NULL (they sort later).
func nativeNSSeekAfter(sortExpr, orderHow string, sortValue interface{}, clusterUUID, namespace string) (string, []interface{}) {
	tie := "(ns.cluster_uuid, ns.namespace_name)"
	if orderHow == listoptions.OrderDesc {
		if sortValue == nil {
			// Cursor is in the NULL region; advance by tie-breaker only.
			return fmt.Sprintf(
				"((%s) IS NULL AND %s > (?, ?))",
				sortExpr, tie,
			), []interface{}{clusterUUID, namespace}
		}
		// Non-NULL cursor: accept rows with a smaller sort value, rows with
		// NULL sort value (they sort after non-NULLs in DESC NULLS LAST),
		// or same sort value with a later tie-breaker.
		return fmt.Sprintf(
			"((%s) < ? OR (%s) IS NULL OR ((%s) IS NOT DISTINCT FROM ? AND %s > (?, ?)))",
			sortExpr, sortExpr, sortExpr, tie,
		), []interface{}{sortValue, sortValue, clusterUUID, namespace}
	}
	return fmt.Sprintf(
		"((%s) > ? OR ((%s) IS NOT DISTINCT FROM ? AND %s > (?, ?)))",
		sortExpr, sortExpr, tie,
	), []interface{}{sortValue, sortValue, clusterUUID, namespace}
}

// nativeNSSeekAfterLegacy supports cursors encoded before sort-key tie-breaking.
func nativeNSSeekAfterLegacy(clusterUUID, namespace string) (string, []interface{}) {
	return "(ns.namespace_name, ns.cluster_uuid) > (?, ?)", []interface{}{namespace, clusterUUID}
}

func applyNativeNSPageSeek(query *gorm.DB, opts listoptions.ListOptions, sortExpr string) *gorm.DB {
	if !opts.HasCursor {
		return query
	}
	if opts.AfterNSSortPresent {
		clause, args := nativeNSSeekAfter(
			sortExpr, opts.OrderHow, opts.AfterNSSortValue,
			opts.AfterNSClusterUUID, opts.AfterNamespaceName,
		)
		return query.Where(clause, args...)
	}
	clause, args := nativeNSSeekAfterLegacy(opts.AfterNSClusterUUID, opts.AfterNamespaceName)
	return query.Where(clause, args...)
}

// applyNativeNSPageSeekOnPage applies cursor-based seek to the page subquery
// (AFTER DISTINCT ON deduplication). Uses page.ros_ns_page_sort_raw for the
// sort comparison and page.cluster_uuid/namespace_name for tie-breakers.
func applyNativeNSPageSeekOnPage(query *gorm.DB, opts listoptions.ListOptions) *gorm.DB {
	if !opts.HasCursor {
		return query
	}
	if opts.AfterNSSortPresent {
		sortCol := "page.ros_ns_page_sort_raw"
		tie := "(page.cluster_uuid, page.namespace_name)"
		orderHow := opts.OrderHow
		sortValue := opts.AfterNSSortValue
		clusterUUID := opts.AfterNSClusterUUID
		namespace := opts.AfterNamespaceName

		var clause string
		var args []interface{}
		if orderHow == listoptions.OrderDesc {
			if sortValue == nil {
				clause = fmt.Sprintf(
					"((%s) IS NULL AND %s > (?, ?))",
					sortCol, tie,
				)
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

// NamespacePaginationAnchor holds the sort position of the last row on a namespace list page.
type NamespacePaginationAnchor struct {
	SortValue   interface{}
	ClusterUUID string
	Namespace   string
}

// PaginationSortValueJSON encodes a sort key for opaque list cursors.
func PaginationSortValueJSON(v interface{}) json.RawMessage {
	if v == nil {
		return nil
	}
	switch x := v.(type) {
	case time.Time:
		b, _ := json.Marshal(x.UTC().Format(time.RFC3339Nano))
		return b
	case *time.Time:
		if x == nil {
			return nil
		}
		b, _ := json.Marshal(x.UTC().Format(time.RFC3339Nano))
		return b
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return nil
		}
		return b
	}
}

// nativeNSParseSortText converts a text sort key from the page subquery into a SQL bind value.
func nativeNSParseSortText(sortExpr string, text *string) interface{} {
	if text == nil || *text == "" {
		return nil
	}
	raw := *text
	if strings.Contains(sortExpr, "last_reported") {
		if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			return t
		}
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			return t
		}
	}
	if strings.Contains(sortExpr, "_pct") {
		if i, err := strconv.ParseInt(raw, 10, 32); err == nil {
			return int32(i)
		}
		if f, err := strconv.ParseFloat(raw, 64); err == nil {
			return f
		}
	}
	if strings.Contains(sortExpr, "estimated_savings") || strings.Contains(sortExpr, "estimated_waste") {
		if i, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return i
		}
	}
	if isNumericPageSort(sortExpr) {
		if i, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return i
		}
	}
	return raw
}

// DecodePaginationSortValue decodes a cursor sort key for SQL binding.
func DecodePaginationSortValue(raw json.RawMessage) (interface{}, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t, nil
		}
		return s, nil
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return f, nil
	}
	var i int64
	if err := json.Unmarshal(raw, &i); err == nil {
		return i, nil
	}
	return nil, fmt.Errorf("unsupported cursor sort value")
}
