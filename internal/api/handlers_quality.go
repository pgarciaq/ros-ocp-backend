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
	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

var QualityAllowedOrderBy = listoptions.OrderByMap{
	"measured_at":        "q.measured_at",
	"cluster":            "c.cluster_alias",
	"project":            "q.namespace",
	"workload":           "q.workload",
	"container":          "q.container_name",
	"stability":          "q.stability_pct",
	"adoption":           "q.adoption_detected",
	"oom_events":         "q.oom_events_after_rec",
	"recommendation_age": "q.recommendation_age_hours",
}

const defaultQualityOrderBy = "q.measured_at"

// MapQualityQueryParameters parses query params for the quality endpoint.
// Keys added below must stay in sync with internal/model/native_query_allowlist.go (nativeRecFixedQueryKeys / ApplyQueryParams).
func MapQualityQueryParameters(c echo.Context) (map[string]interface{}, error) {
	queryParams := make(map[string]interface{})

	now := time.Now().UTC()
	firstOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	startDateStr := c.QueryParam("start_date")
	if startDateStr == "" {
		queryParams["q.measured_at >= ?"] = firstOfMonth
	} else {
		t, err := time.Parse(timeLayout, startDateStr)
		if err != nil {
			return queryParams, fmt.Errorf("invalid start_date: %w", err)
		}
		queryParams["q.measured_at >= ?"] = t
	}

	endDateStr := c.QueryParam("end_date")
	if endDateStr == "" {
		queryParams["q.measured_at < ?"] = now.Add(time.Second)
	} else {
		t, err := time.Parse(timeLayout, endDateStr)
		if err != nil {
			return queryParams, fmt.Errorf("invalid end_date: %w", err)
		}
		queryParams["q.measured_at < ?"] = t.Add(24 * time.Hour)
	}

	if clusters := queryparams.IncludeValues(c, "cluster"); len(clusters) > 0 {
		queryParams["c.cluster_alias IN ?"] = clusters
	}
	if projects := queryparams.IncludeValues(c, "project"); len(projects) > 0 {
		queryParams["q.namespace IN ?"] = projects
	}
	if workloads := queryparams.IncludeValues(c, "workload"); len(workloads) > 0 {
		queryParams["q.workload IN ?"] = workloads
	}
	if containers := queryparams.IncludeValues(c, "container"); len(containers) > 0 {
		queryParams["q.container_name IN ?"] = containers
	}

	return queryParams, nil
}

// GetRecommendationQuality handles GET /recommendations/openshift/quality.
func GetRecommendationQuality(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	userPerms := get_user_permissions(c)
	hlog := requestLogger(c, orgID)

	opts, err := listoptions.ListAPIOptions(c, defaultQualityOrderBy, QualityAllowedOrderBy)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
	}

	queryParams, err := MapQualityQueryParameters(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
	}

	rows, count, queryErr := model.GetRecommendationQuality(orgID, opts, queryParams, userPerms)
	if queryErr != nil {
		hlog.Errorf("unable to fetch recommendation quality: %v", queryErr)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch records from database",
		})
	}

	c.Response().Header().Set("Cache-Control", "private, max-age=300")

	switch opts.Format {
	case listoptions.ResponseFormatCSV:
		filename := "recommendation-quality-" + time.Now().Format("20060102")
		c.Response().Header().Set(echo.HeaderContentType, "text/csv")
		c.Response().Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s.csv", filename))
		pipeReader, pipeWriter := io.Pipe()
		reqCtx := c.Request().Context()
		go func() {
			var genErr error
			defer func() {
				if r := recover(); r != nil {
					genErr = fmt.Errorf("panic in quality CSV generation: %v", r)
				}
				if genErr != nil {
					_ = pipeWriter.CloseWithError(genErr)
				} else {
					_ = pipeWriter.Close()
				}
			}()
			genErr = generateQualityCSV(reqCtx, pipeWriter, rows)
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

var qualityCSVHeader = []string{
	"measured_at", "cluster_uuid", "cluster_alias",
	"namespace", "workload", "container_name",
	"stability_pct", "adoption_detected",
	"oom_events_after_rec", "recommendation_age_hours",
}

func generateQualityCSV(ctx context.Context, w io.Writer, rows []model.QualityRow) error {
	writer := csv.NewWriter(w)
	if err := writer.Write(qualityCSVHeader); err != nil {
		return fmt.Errorf("unable to write header: %w", err)
	}
	for _, r := range rows {
		record := []string{
			r.MeasuredAt.Format(time.RFC3339),
			r.ClusterUUID,
			r.ClusterAlias,
			r.Namespace,
			r.Workload,
			r.ContainerName,
			optFloat32Str(r.StabilityPct),
			strconv.FormatBool(r.AdoptionDetected),
			optInt64Str(r.OOMEventsAfterRec),
			optInt64Str(r.RecommendationAgeHrs),
		}
		if err := writer.Write(record); err != nil {
			return fmt.Errorf("unable to write row: %w", err)
		}
	}
	writer.Flush()
	return writer.Error()
}
