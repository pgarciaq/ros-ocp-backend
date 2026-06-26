package api

import (
	"fmt"
	"strings"

	"github.com/labstack/echo/v4"

	kruizeplugin "github.com/redhatinsights/ros-ocp-backend/internal/plugins/kruize"
)

const timeLayout = "2006-01-02"

type ParamError struct {
	AppErr  error
	UserErr bool
}

func (e *ParamError) Error() string { return e.AppErr.Error() }
func (e *ParamError) Unwrap() error { return e.AppErr }

// namespaceAPIErrf constructs a ParamError. UserErr per error is not evaluated currently;
// apiErrResponse in utils.go is the single gate controlling user-facing error visibility.
func namespaceAPIErrf(userErr bool, format string, args ...any) *ParamError { //nolint:unparam
	return &ParamError{AppErr: fmt.Errorf(format, args...), UserErr: userErr}
}

// Filter modes for param-based query filters (cluster, project, etc.).
const (
	FilterModeInclude = "include"
	FilterModeExact   = "exact"
	FilterModeExclude = "exclude"
)

const (
	SkipSanitizationForContainer = true
	SkipSanitizationForNamespace = true
)

// EnableUserAPIErr controls whether ParamError.UserErr is set in namespaceAPIErrf.
// The constant is retained for the ParamError system; apiErrResponse itself now
// always returns the standard {"status":"error","message":"..."} shape.
const EnableUserAPIErr = false

// maxWorkloadTypeLen is the Kubernetes name convention limit.
const maxWorkloadTypeLen = 63

func validateWorkloadTypeValues(vals []string) error {
	for _, v := range vals {
		lower := strings.ToLower(v)
		if lower == "" || strings.TrimSpace(lower) == "" {
			return namespaceAPIErrf(EnableUserAPIErr, "workload_type must not be empty")
		}
		if lower == "<none>" {
			return namespaceAPIErrf(EnableUserAPIErr, "workload_type %q is a sentinel value and not allowed", v)
		}
		if len(lower) > maxWorkloadTypeLen {
			return namespaceAPIErrf(EnableUserAPIErr, "workload_type %q exceeds maximum length of %d characters", v, maxWorkloadTypeLen)
		}
		if strings.ContainsAny(lower, " \t\n\r") {
			return namespaceAPIErrf(EnableUserAPIErr, "workload_type %q must not contain whitespace", v)
		}
	}
	return nil
}

// FilterModeClause maps mode to SQL clause suffix, wrap for include, and join for multi-value params.
var FilterModeClause = map[string]struct {
	Suffix string
	Wrap   bool
	Join   string
}{
	FilterModeInclude: {" ILIKE ? ESCAPE '\\'", true, " OR "},
	FilterModeExact:   {" = ?", false, " OR "},
	FilterModeExclude: {" != ?", false, " AND "},
}

type Collection[T any] struct {
	Data  []T        `json:"data"`
	Meta  Metadata   `json:"meta"`
	Links Links      `json:"links"`
}

type Metadata struct {
	Count      int      `json:"count"`
	Limit      int      `json:"limit"`
	Offset     int      `json:"offset,omitempty"`
	HasNext    bool     `json:"has_next"`
	NextCursor string   `json:"next_cursor,omitempty"`
	Currency   string   `json:"currency"`
	Warnings   []string `json:"warnings,omitempty"`
}

type Links struct {
	First    string `json:"first"`
	Previous string `json:"previous,omitempty"`
	Next     string `json:"next,omitempty"`
	Last     string `json:"last"`
}

var NotificationsToShow = kruizeplugin.NotificationsToShow

var MemoryUnitk8s = kruizeplugin.MemoryUnitk8s

var CPUUnitk8s = kruizeplugin.CPUUnitk8s

var FlattenedCSVHeader = []string{
	"id",
	"cluster_uuid",
	"cluster_alias",
	"container",
	"project",
	"workload",
	"workload_type",
	"last_reported",
	"source_id",
	"current_cpu_limit_amount",
	"current_cpu_limit_format",
	"current_memory_limit_amount",
	"current_memory_limit_format",
	"current_cpu_request_amount",
	"current_cpu_request_format",
	"current_memory_request_amount",
	"current_memory_request_format",
	"monitoring_end_time",
	"recommendation_term",
	"duration_in_hours",
	"monitoring_start_time",
	"recommendation_type",
	"config_cpu_limit_amount",
	"config_cpu_limit_format",
	"config_memory_limit_amount",
	"config_memory_limit_format",
	"config_cpu_request_amount",
	"config_cpu_request_format",
	"config_memory_request_amount",
	"config_memory_request_format",
	"variation_cpu_limit_amount",
	"variation_cpu_limit_format",
	"variation_memory_limit_amount",
	"variation_memory_limit_format",
	"variation_cpu_request_amount",
	"variation_cpu_request_format",
	"variation_memory_request_amount",
	"variation_memory_request_format",
}

var NativeCSVHeader = []string{
	"cluster_uuid",
	"cluster_alias",
	"container",
	"project",
	"workload",
	"workload_type",
	"last_reported",
	"source_id",
	"pod_count_min",
	"pod_count_max",
	"pod_count_avg",
	"estimated_monthly_savings",
	"cpu_savings",
	"memory_savings",
	"estimated_monthly_waste",
	"estimated_monthly_waste_currency",
	"currency",
	"idle_state",
	"idle_since",
	"idle_duration_days",
	"peak_cpu_millicores",
	"peak_memory_bytes",
	"recommendation_term",
	"recommendation_engine",
	"rec_cpu_request_millicores",
	"rec_cpu_limit_millicores",
	"rec_memory_request_kib",
	"rec_memory_limit_kib",
	"current_cpu_request_millicores",
	"current_cpu_limit_millicores",
	"current_memory_request_kib",
	"current_memory_limit_kib",
	"variation_cpu_request_pct",
	"variation_cpu_limit_pct",
	"variation_memory_request_pct",
	"variation_memory_limit_pct",
	"confidence_level",
	"notification_codes",
}

var NativeNSCSVHeader = []string{
	"cluster_uuid",
	"cluster_alias",
	"project",
	"last_reported",
	"source_id",
	"idle_state",
	"idle_since",
	"idle_duration_days",
	"estimated_monthly_waste",
	"estimated_monthly_savings",
	"recommendation_term",
	"recommendation_engine",
	"rec_cpu_request_millicores",
	"rec_cpu_limit_millicores",
	"rec_memory_request_kib",
	"rec_memory_limit_kib",
	"current_cpu_request_millicores",
	"current_cpu_limit_millicores",
	"current_memory_request_kib",
	"current_memory_limit_kib",
	"variation_cpu_request_pct",
	"variation_cpu_limit_pct",
	"variation_memory_request_pct",
	"variation_memory_limit_pct",
	"confidence_level",
	"notification_codes",
}

// setRecommendationNoStore marks recommendation JSON as non-cacheable (volatile).
func setRecommendationNoStore(c echo.Context) {
	c.Response().Header().Set("Cache-Control", "no-store")
}
