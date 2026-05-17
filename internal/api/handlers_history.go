package api

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
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

// MapHistoryQueryParameters parses query params for the history endpoint.
// Uses the same date/filter pattern as MapNativeQueryParameters but with
// recommendation_history column aliases.
func MapHistoryQueryParameters(c echo.Context) (map[string]interface{}, error) {
	queryParams := make(map[string]interface{})

	now := time.Now().UTC()
	firstOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	startDateStr := c.QueryParam("start_date")
	if startDateStr == "" {
		queryParams["h.recorded_at >= ?"] = firstOfMonth
	} else {
		t, err := time.Parse(timeLayout, startDateStr)
		if err != nil {
			return queryParams, fmt.Errorf("invalid start_date: %w", err)
		}
		queryParams["h.recorded_at >= ?"] = t
	}

	endDateStr := c.QueryParam("end_date")
	if endDateStr == "" {
		queryParams["h.recorded_at < ?"] = now.Add(time.Second)
	} else {
		t, err := time.Parse(timeLayout, endDateStr)
		if err != nil {
			return queryParams, fmt.Errorf("invalid end_date: %w", err)
		}
		queryParams["h.recorded_at < ?"] = t.Add(24 * time.Hour)
	}

	if clusters := c.QueryParams()["cluster"]; len(clusters) > 0 {
		queryParams["c.cluster_alias IN ?"] = clusters
	}
	if projects := c.QueryParams()["project"]; len(projects) > 0 {
		queryParams["h.namespace IN ?"] = projects
	}
	if workloads := c.QueryParams()["workload"]; len(workloads) > 0 {
		queryParams["h.workload IN ?"] = workloads
	}
	if containers := c.QueryParams()["container"]; len(containers) > 0 {
		queryParams["h.container_name IN ?"] = containers
	}
	if terms := c.QueryParams()["term"]; len(terms) > 0 {
		queryParams["h.term IN ?"] = terms
	}
	if engines := c.QueryParams()["engine"]; len(engines) > 0 {
		queryParams["h.engine IN ?"] = engines
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

	opts, err := listoptions.ListAPIOptions(c, defaultHistoryOrderBy, HistoryAllowedOrderBy)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
	}

	queryParams, err := MapHistoryQueryParameters(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
	}

	rows, count, queryErr := model.GetRecommendationHistory(orgID, opts, queryParams, userPerms)
	if queryErr != nil {
		log.Errorf("unable to fetch recommendation history; %v", queryErr)
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
		interfaceSlice := make([]any, len(rows))
		for i := range rows {
			interfaceSlice[i] = rows[i]
		}
		response := CollectionResponse(interfaceSlice, c.Request(), count, opts.Limit, opts.Offset)
		return c.JSON(http.StatusOK, response)
	}
}

var historyCSVHeader = []string{
	"recorded_at", "cluster_uuid", "cluster_alias",
	"namespace", "workload", "container_name",
	"term", "engine",
	"rec_cpu_request_millicores", "rec_cpu_limit_millicores",
	"rec_memory_request_kib", "rec_memory_limit_kib",
	"confidence_level", "estimated_monthly_savings_usd",
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
			optFloat32Str(r.EstimatedSavingsUSD),
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

func optFloat32Str(v *float32) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(float64(*v), 'f', 2, 32)
}
