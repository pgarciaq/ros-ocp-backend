package listoptions

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
)

const (
	DefaultLimit  = 100
	MaxLimit      = 1000
	DefaultOffset = 0

	OrderAsc           = "asc"
	OrderDesc          = "desc"
	ResponseFormatJSON = "json"
	ResponseFormatCSV  = "csv"

	// Default DB columns for OrderBy.
	DefaultContainerRecsDBColumn = "clusters.last_reported_at"
	DefaultNsRecsDBColumn        = "clusters.last_reported_at"
	DefaultNodeRecsOrderBy       = "node_name"
	DefaultGpuMigOrderBy         = "cluster_uuid"
)

type ListOptions struct {
	Limit    int
	Offset   int
	OrderBy  string
	OrderHow string
	Format   string

	// Keyset pagination (after query param). When HasCursor is true, Offset is ignored.
	HasCursor            bool
	AfterNamespace       string
	AfterWorkload        string
	AfterContainer       string
	AfterNSClusterUUID   string
	AfterNamespaceName   string
	AfterNSSortPresent bool        // true when cursor includes primary sort key (new cursors)
	AfterNSSortValue   interface{} // decoded sort key; only valid when AfterNSSortPresent

	AfterContainerClusterUUID   string
	AfterContainerSortPresent bool        // true when cursor includes primary sort key (new cursors)
	AfterContainerSortValue   interface{} // decoded sort key; only valid when AfterContainerSortPresent
}

// OrderByMap maps allowed JSON keys to DB columns.
type OrderByMap map[string]string

// SQLOrderByFragment returns the ORDER BY expression for list queries. For DESC it appends
// NULLS LAST so rows with NULL in nullable sort columns (e.g. variation *_pct) appear last.
func SQLOrderByFragment(orderByColumnSQL, orderHow string) string {
	s := orderByColumnSQL + " " + orderHow
	if orderHow == OrderDesc {
		s += " NULLS LAST"
	}
	return s
}

// API-specific maps and defaults.
var ContainerAllowedOrderBy = OrderByMap{
	"cluster":       "clusters.cluster_alias",
	"workload_type": "workloads.workload_type",
	"workload":      "workloads.workload_name",
	"project":       "workloads.namespace",
	"container":     "recommendation_sets.container_name",
	"last_reported": "clusters.last_reported_at",
	// Current request amounts
	"cpu_request_current":    "recommendation_sets.cpu_request_current",
	"memory_request_current": "recommendation_sets.memory_request_current",
	// Per-term, per-engine variation (values are percent of current request; DB columns use _pct suffix)
	"cpu_variation_short_cost":            "recommendation_sets.cpu_variation_short_cost_pct",
	"cpu_variation_short_performance":     "recommendation_sets.cpu_variation_short_performance_pct",
	"cpu_variation_medium_cost":           "recommendation_sets.cpu_variation_medium_cost_pct",
	"cpu_variation_medium_performance":    "recommendation_sets.cpu_variation_medium_performance_pct",
	"cpu_variation_long_cost":             "recommendation_sets.cpu_variation_long_cost_pct",
	"cpu_variation_long_performance":      "recommendation_sets.cpu_variation_long_performance_pct",
	"memory_variation_short_cost":         "recommendation_sets.memory_variation_short_cost_pct",
	"memory_variation_short_performance":  "recommendation_sets.memory_variation_short_performance_pct",
	"memory_variation_medium_cost":        "recommendation_sets.memory_variation_medium_cost_pct",
	"memory_variation_medium_performance": "recommendation_sets.memory_variation_medium_performance_pct",
	"memory_variation_long_cost":          "recommendation_sets.memory_variation_long_cost_pct",
	"memory_variation_long_performance":   "recommendation_sets.memory_variation_long_performance_pct",
	"idle_state":                        "recommendation_sets.idle_state",
	"idle_duration_days":                "recommendation_sets.idle_duration_days",
	"estimated_monthly_waste":           "recommendation_sets.estimated_waste_cents",
}

var NodeRecsAllowedOrderBy = OrderByMap{
	"node_name":            "node_name",
	"cluster_uuid":         "cluster_uuid",
	"gpu_model":            "gpu_model",
	"recommended_replicas": "recommended_replicas",
	"confidence":           "confidence",
	"total_node_savings_usd": "total_node_savings_usd",
}

// GpuMigAllowedOrderBy defines sort keys for GET .../gpu/mig (applied in memory after query).
var GpuMigAllowedOrderBy = OrderByMap{
	"cluster_uuid": "cluster_uuid",
	"namespace":    "namespace",
	"workload":     "workload",
	"container":    "container",
	"term":         "term",
	"gpu_model":    "gpu_model",
	"confidence":   "confidence",
}

var NsAllowedOrderBy = OrderByMap{
	"cluster":       "clusters.cluster_alias",
	"project":       "namespace_recommendation_sets.namespace_name",
	"last_reported": "clusters.last_reported_at",
	// Backward-compatible: keep current columns for order-by
	"cpu_request_current":    "namespace_recommendation_sets.cpu_request_current",
	"memory_request_current": "namespace_recommendation_sets.memory_request_current",
	// Per-term, per-engine variation (values are percent of current request; DB columns use _pct suffix)
	"cpu_variation_short_cost":            "namespace_recommendation_sets.cpu_variation_short_cost_pct",
	"cpu_variation_short_performance":     "namespace_recommendation_sets.cpu_variation_short_performance_pct",
	"cpu_variation_medium_cost":           "namespace_recommendation_sets.cpu_variation_medium_cost_pct",
	"cpu_variation_medium_performance":    "namespace_recommendation_sets.cpu_variation_medium_performance_pct",
	"cpu_variation_long_cost":             "namespace_recommendation_sets.cpu_variation_long_cost_pct",
	"cpu_variation_long_performance":      "namespace_recommendation_sets.cpu_variation_long_performance_pct",
	"memory_variation_short_cost":         "namespace_recommendation_sets.memory_variation_short_cost_pct",
	"memory_variation_short_performance":  "namespace_recommendation_sets.memory_variation_short_performance_pct",
	"memory_variation_medium_cost":        "namespace_recommendation_sets.memory_variation_medium_cost_pct",
	"memory_variation_medium_performance": "namespace_recommendation_sets.memory_variation_medium_performance_pct",
	"memory_variation_long_cost":          "namespace_recommendation_sets.memory_variation_long_cost_pct",
	"memory_variation_long_performance":   "namespace_recommendation_sets.memory_variation_long_performance_pct",
}

func parseOffset(val string) int {
	if val == "" {
		return DefaultOffset
	}
	if i, err := strconv.Atoi(val); err == nil && i >= 0 {
		return i
	}
	return DefaultOffset
}

func parseLimit(val string) (int, error) {
	if val == "" {
		return DefaultLimit, nil
	}
	i, err := strconv.Atoi(val)
	if err != nil {
		return 0, fmt.Errorf("invalid limit: %q", val)
	}
	if i < 0 {
		return 0, fmt.Errorf("limit cannot be negative")
	}
	if i == 0 {
		return DefaultLimit, nil
	}
	if i > MaxLimit {
		return MaxLimit, nil
	}
	return i, nil
}

func ListAPIOptions(c echo.Context, defaultDBColumn string, allowedOrderBy OrderByMap) (ListOptions, error) {

	limit, err := parseLimit(c.QueryParam("limit"))
	if err != nil {
		return ListOptions{}, err
	}
	offset := parseOffset(c.QueryParam("offset"))

	defaultAPIField := ""
	for apiField, dbCol := range allowedOrderBy {
		if dbCol == defaultDBColumn {
			defaultAPIField = apiField
			break
		}
	}
	orderByCol, orderHow, err := queryparams.ParseOrderBy(c, allowedOrderBy, defaultAPIField, OrderDesc)
	if err != nil {
		return ListOptions{}, err
	}
	orderBy := orderByCol
	if orderBy == "" {
		orderBy = defaultDBColumn
	}

	// Format handling
	acceptHeader := c.Request().Header.Get("Accept")
	formatParam := strings.ToLower(c.QueryParam("format"))

	format, err := resolveResponseFormat(acceptHeader, formatParam)
	if err != nil {
		return ListOptions{}, err
	}

	if offset < 0 {
		offset = DefaultOffset
	}

	return ListOptions{
		Limit:    limit,
		Offset:   offset,
		OrderBy:  orderBy,
		OrderHow: orderHow,
		Format:   format,
	}, nil
}

// ResolveResponseFormat selects JSON or CSV from the Accept header and format query parameter.
func ResolveResponseFormat(acceptHeaderVal string, formatQueryParamVal string) (string, error) {
	return resolveResponseFormat(acceptHeaderVal, strings.ToLower(strings.TrimSpace(formatQueryParamVal)))
}

func resolveResponseFormat(acceptHeaderVal string, formatQueryParamVal string) (string, error) {
	if acceptHeaderVal == "" && formatQueryParamVal == "" {
		return ResponseFormatJSON, nil // default format
	}

	switch acceptHeaderVal {
	case "text/csv":
		return ResponseFormatCSV, nil
	case "application/json":
		return ResponseFormatJSON, nil
	}

	switch formatQueryParamVal {
	case "", "json":
		return ResponseFormatJSON, nil
	case "csv":
		return ResponseFormatCSV, nil
	default:
		return "", fmt.Errorf("invalid value for format: %q", formatQueryParamVal)
	}

}
