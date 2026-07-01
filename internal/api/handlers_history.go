package api

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
)

var HistoryAllowedOrderBy = listoptions.OrderByMap{
	"recorded_at": "h.recorded_at",
	"cluster":     "c.cluster_alias",
	"project":     "h.namespace",
	"workload":    "h.workload",
	"container":   "h.container_name",
	"term":        "h.term",
	"engine":      "h.engine",
}

const defaultHistoryOrderBy = "h.recorded_at"

func checkHistoryFilterCardinality(param string, values []string) error {
	max := config.GetConfig().MaxCountPerQueryParam
	if max <= 0 {
		max = 5
	}
	if len(values) > max {
		return fmt.Errorf("too many %s parameters, a maximum of %d is allowed", param, max)
	}
	return nil
}

// MapHistoryQueryParameters parses query params for the history endpoint.
// Uses the same date/filter pattern as MapNativeQueryParameters but with
// recommendation_history column aliases.
// When start_date and end_date are both omitted, defaults to the last ROS_HISTORY_DEFAULT_DAYS
// (default 30) ending at the current UTC time.
// Keys added below must stay in sync with internal/model/native_query_allowlist.go (nativeRecFixedQueryKeys / ApplyQueryParams).
func MapHistoryQueryParameters(c echo.Context) (map[string]interface{}, error) {
	queryParams := make(map[string]interface{})

	now := time.Now().UTC()
	defaultDays := config.GetConfig().HistoryDefaultDays
	if defaultDays <= 0 {
		defaultDays = 30
	}
	defaultStart := now.AddDate(0, 0, -defaultDays)

	startDateStr := c.QueryParam("start_date")
	endDateStr := c.QueryParam("end_date")
	if startDateStr == "" && endDateStr == "" {
		queryParams["h.recorded_at >= ?"] = defaultStart
		queryParams["h.recorded_at < ?"] = now.Add(time.Second)
	} else {
		if startDateStr == "" {
			queryParams["h.recorded_at >= ?"] = defaultStart
		} else {
			t, err := time.Parse(timeLayout, startDateStr)
			if err != nil {
				return queryParams, fmt.Errorf("invalid start_date: %w", err)
			}
			queryParams["h.recorded_at >= ?"] = t
		}

		if endDateStr == "" {
			queryParams["h.recorded_at < ?"] = now.Add(time.Second)
		} else {
			t, err := time.Parse(timeLayout, endDateStr)
			if err != nil {
				return queryParams, fmt.Errorf("invalid end_date: %w", err)
			}
			queryParams["h.recorded_at < ?"] = t.Add(24 * time.Hour)
		}
	}

	if clusters := queryparams.IncludeValues(c, "cluster"); len(clusters) > 0 {
		if err := checkHistoryFilterCardinality("cluster", clusters); err != nil {
			return queryParams, err
		}
		queryParams["c.cluster_alias IN ?"] = clusters
	}
	if projects := queryparams.IncludeValues(c, "project"); len(projects) > 0 {
		if err := checkHistoryFilterCardinality("project", projects); err != nil {
			return queryParams, err
		}
		queryParams["h.namespace IN ?"] = projects
	}
	if workloads := queryparams.IncludeValues(c, "workload"); len(workloads) > 0 {
		if err := checkHistoryFilterCardinality("workload", workloads); err != nil {
			return queryParams, err
		}
		queryParams["h.workload IN ?"] = workloads
	}
	if containers := queryparams.IncludeValues(c, "container"); len(containers) > 0 {
		if err := checkHistoryFilterCardinality("container", containers); err != nil {
			return queryParams, err
		}
		queryParams["h.container_name IN ?"] = containers
	}
	if terms := queryparams.IncludeValues(c, "term"); len(terms) > 0 {
		if err := checkHistoryFilterCardinality("term", terms); err != nil {
			return queryParams, err
		}
		normalized := make([]string, 0, len(terms))
		for _, t := range terms {
			dbTerm, termErr := queryparams.NormalizeRecommendationTermFilter(t)
			if termErr != nil {
				return queryParams, termErr
			}
			if dbTerm != "" {
				normalized = append(normalized, dbTerm)
			}
		}
		if len(normalized) > 0 {
			queryParams["h.term IN ?"] = normalized
		}
	}
	if engines := queryparams.IncludeValues(c, "engine"); len(engines) > 0 {
		if err := checkHistoryFilterCardinality("engine", engines); err != nil {
			return queryParams, err
		}
		queryParams["h.engine IN ?"] = engines
	}
	if err := attachTagFiltersToQueryParams(c, queryParams); err != nil {
		return queryParams, err
	}

	return queryParams, nil
}

// GetRecommendationHistory handles GET /recommendations/openshift/history.
func GetRecommendationHistory(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	userPerms := get_user_permissions(c)
	hlog := requestLogger(c, orgID)

	opts, err := listoptions.ListAPIOptions(c, defaultHistoryOrderBy, HistoryAllowedOrderBy)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
	}

	queryParams, err := MapHistoryQueryParameters(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
	}

	if opts.Format == listoptions.ResponseFormatCSV {
		opts.Limit = config.GetConfig().RecordLimitCSV
		opts.Offset = 0
	}

	rows, count, queryErr := model.GetRecommendationHistory(orgID, opts, queryParams, userPerms)
	if queryErr != nil {
		hlog.Errorf("unable to fetch recommendation history: %v", queryErr)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch records from database",
		})
	}

	c.Response().Header().Set("Cache-Control", "private, max-age=300")

	switch opts.Format {
	case listoptions.ResponseFormatCSV:
		filename := "recommendation-history-" + time.Now().Format("20060102")
		c.Response().Header().Set(echo.HeaderContentType, "text/csv")
		c.Response().Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s.csv", filename))
		pipeReader, pipeWriter := io.Pipe()
		reqCtx := c.Request().Context()
		go func() {
			var genErr error
			defer func() {
				if r := recover(); r != nil {
					genErr = fmt.Errorf("panic in history CSV generation: %v", r)
				}
				if genErr != nil {
					_ = pipeWriter.CloseWithError(genErr)
				} else {
					_ = pipeWriter.Close()
				}
			}()
			genErr = generateHistoryCSV(reqCtx, pipeWriter, rows)
		}()
		return c.Stream(http.StatusOK, "text/csv", pipeReader)
	default:
		response := CollectionResponse(rows, c.Request(), count, opts.Limit, opts.Offset)
		response.Meta.Currency = resolveListCurrencyFromRequest(c, orgID)
		attachTagWarningsToCollection(response, c, orgID, len(rows))
		return c.JSON(http.StatusOK, response)
	}
}

var historyCSVHeader = []string{
	"recorded_at", "cluster_uuid", "cluster_alias",
	"namespace", "workload", "container_name",
	"term", "engine",
	"rec_cpu_request_millicores", "rec_cpu_limit_millicores",
	"rec_memory_request_kib", "rec_memory_limit_kib",
	"confidence_level", "estimated_savings_cents",
	"notification_codes",
	"expl_data_days", "expl_decay_half_life_hours",
	"expl_cpu_cost_pct_mc", "expl_cpu_perf_pct_mc",
	"expl_cpu_usage_p95_mc", "expl_cpu_usage_p50_mc", "expl_cpu_usage_mean_mc",
	"expl_cpu_adaptive_margin_bp", "expl_cpu_trend_slope",
	"expl_mem_cost_pct_kib", "expl_mem_perf_pct_kib",
	"expl_mem_usage_p95_kib", "expl_mem_usage_p50_kib", "expl_mem_usage_mean_kib",
	"expl_mem_adaptive_margin_bp", "expl_mem_trend_slope",
	"expl_oom_count_sum", "expl_oom_bump_applied", "expl_cpu_floor_applied", "expl_mem_floor_applied", "expl_is_idle",
}

func generateHistoryCSV(ctx context.Context, w io.Writer, rows []model.HistoryRow) error {
	writer := csv.NewWriter(w)
	if err := writer.Write(historyCSVHeader); err != nil {
		return fmt.Errorf("unable to write header: %w", err)
	}
	for _, r := range rows {
		record := []string{
			r.RecordedAt.Format(time.RFC3339),
			r.ClusterUUID,
			r.ClusterAlias,
			r.Namespace,
			r.Workload,
			r.ContainerName,
			r.Term,
			r.Engine,
			optInt64Str(r.RecCPURequestMC),
			optInt64Str(r.RecCPULimitMC),
			optInt64Str(r.RecMemRequestKiB),
			optInt64Str(r.RecMemLimitKiB),
			optFloat32Str(r.ConfidenceLevel),
			optCentsUSDStr(r.EstimatedSavingsCents),
			smallintArrayStr(r.NotificationCodes),
			optIntStr(r.ExplDataDays),
			optFloat64Str(r.ExplDecayHalfLifeHours),
			optInt64Str(r.ExplCPUCostPctMC),
			optInt64Str(r.ExplCPUPerfPctMC),
			optInt64Str(r.ExplCPUUsageP95MC),
			optInt64Str(r.ExplCPUUsageP50MC),
			optInt64Str(r.ExplCPUUsageMeanMC),
			optInt32Str(r.ExplCPUAdaptiveMarginBP),
			optFloat64Str(r.ExplCPUTrendSlope),
			optInt64Str(r.ExplMemCostPctKiB),
			optInt64Str(r.ExplMemPerfPctKiB),
			optInt64Str(r.ExplMemUsageP95KiB),
			optInt64Str(r.ExplMemUsageP50KiB),
			optInt64Str(r.ExplMemUsageMeanKiB),
			optInt32Str(r.ExplMemAdaptiveMarginBP),
			optFloat64Str(r.ExplMemTrendSlope),
			optInt64Str(r.ExplOOMCountSum),
			optBoolStr(r.ExplOOMBumpApplied),
			optBoolStr(r.ExplCPUFloorApplied),
			optBoolStr(r.ExplMemFloorApplied),
			optBoolStr(r.ExplIsIdle),
		}
		if err := writer.Write(record); err != nil {
			return fmt.Errorf("unable to write row: %w", err)
		}
	}
	writer.Flush()
	return writer.Error()
}

func optInt64Str(v *int64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatInt(*v, 10)
}

func optCentsUSDStr(cents *int64) string {
	if cents == nil {
		return ""
	}
	return strconv.FormatFloat(money.CentsToUSD(*cents), 'f', 2, 64)
}

func optFloat32Str(v *float32) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(float64(*v), 'f', 3, 32)
}

func smallintArrayStr(codes model.SmallintArray) string {
	if len(codes) == 0 {
		return ""
	}
	parts := make([]string, len(codes))
	for i, c := range codes {
		parts[i] = strconv.FormatInt(int64(c), 10)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func optIntStr(v *int) string {
	if v == nil {
		return ""
	}
	return strconv.Itoa(*v)
}

func optInt32Str(v *int32) string {
	if v == nil {
		return ""
	}
	return strconv.FormatInt(int64(*v), 10)
}

func optFloat64Str(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}

func optBoolStr(v *bool) string {
	if v == nil {
		return ""
	}
	if *v {
		return "true"
	}
	return "false"
}
