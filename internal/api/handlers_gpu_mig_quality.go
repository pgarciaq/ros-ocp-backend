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

var GPUMIGQualityAllowedOrderBy = listoptions.OrderByMap{
	"measured_at":        "q.measured_at",
	"cluster":            "c.cluster_alias",
	"project":            "q.namespace",
	"workload":           "q.workload",
	"container_name":     "q.container_name",
	"stability":          "q.stability_pct",
	"adoption":           "q.adoption_detected",
	"contention_days":    "q.contention_days",
	"recommendation_age": "q.recommendation_age_hours",
}

const defaultGPUMIGQualityOrderBy = "q.measured_at"

// MapGPUMIGQualityQueryParameters parses query params for the GPU MIG quality endpoint.
func MapGPUMIGQualityQueryParameters(c echo.Context) (map[string]interface{}, error) {
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
	if containers := queryparams.IncludeValues(c, "container_name"); len(containers) > 0 {
		queryParams["q.container_name IN ?"] = containers
	}

	return queryParams, nil
}

// GetGPUMIGRecommendationQuality handles GET /recommendations/openshift/quality/gpu.
func GetGPUMIGRecommendationQuality(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	userPerms := get_user_permissions(c)
	hlog := requestLogger(c, orgID)

	opts, err := listoptions.ListAPIOptions(c, defaultGPUMIGQualityOrderBy, GPUMIGQualityAllowedOrderBy)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
	}

	qp, err := MapGPUMIGQualityQueryParameters(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
	}

	engineFilter, err := resolveQualityEngineFilter(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
	}
	qp["q.engine = ?"] = engineFilter

	rows, count, queryErr := model.GetGPUMIGRecommendationQuality(orgID, opts, qp, userPerms)
	if queryErr != nil {
		hlog.Errorf("unable to fetch GPU MIG recommendation quality: %v", queryErr)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch records from database",
		})
	}

	c.Response().Header().Set("Cache-Control", "private, max-age=300")

	switch opts.Format {
	case listoptions.ResponseFormatCSV:
		filename := "gpu-mig-recommendation-quality-" + time.Now().Format("20060102")
		c.Response().Header().Set(echo.HeaderContentType, "text/csv")
		c.Response().Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s.csv", filename))
		pipeReader, pipeWriter := io.Pipe()
		reqCtx := c.Request().Context()
		go func() {
			var genErr error
			defer func() {
				if r := recover(); r != nil {
					genErr = fmt.Errorf("panic in GPU MIG quality CSV generation: %v", r)
				}
				if genErr != nil {
					_ = pipeWriter.CloseWithError(genErr)
				} else {
					_ = pipeWriter.Close()
				}
			}()
			genErr = generateGPUMIGQualityCSV(reqCtx, pipeWriter, rows)
		}()
		return c.Stream(http.StatusOK, "text/csv", pipeReader)
	default:
		response := CollectionResponse(rows, c.Request(), count, opts.Limit, opts.Offset)
		response.Meta.Currency = resolveListCurrencyFromRequest(c, orgID)
		return c.JSON(http.StatusOK, response)
	}
}

var gpuMIGQualityCSVHeader = []string{
	"measured_at", "cluster_uuid", "cluster_alias",
	"namespace", "workload", "container_name", "engine",
	"stability_pct", "adoption_detected",
	"contention_days", "recommendation_age_hours",
}

func generateGPUMIGQualityCSV(_ context.Context, w io.Writer, rows []model.GPUMIGQualityRow) error {
	writer := csv.NewWriter(w)
	if err := writer.Write(gpuMIGQualityCSVHeader); err != nil {
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
			r.Engine,
			optFloat32Str(r.StabilityPct),
			strconv.FormatBool(r.AdoptionDetected),
			optInt64Str(r.ContentionDays),
			optInt64Str(r.RecommendationAgeHrs),
		}
		if err := writer.Write(record); err != nil {
			return fmt.Errorf("unable to write row: %w", err)
		}
	}
	writer.Flush()
	return writer.Error()
}
