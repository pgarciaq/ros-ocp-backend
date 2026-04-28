package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/platform-go-middlewares/identity"
	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

func GetRecommendationSetList(c echo.Context) error {
	XRHID := c.Get("Identity").(identity.XRHID)
	OrgID := XRHID.Identity.OrgID
	user_permissions := get_user_permissions(c)
	handlerName := "recommendationset-list"

	apiListOptions, err := listoptions.ListAPIOptions(c, listoptions.DefaultContainerRecsDBColumn, listoptions.ContainerAllowedOrderBy)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"status":  "error",
			"message": err.Error(),
		})
	}

	queryParams, err := MapQueryParameters(c)
	if err != nil {
		return apiErrResponse(c, err, http.StatusBadRequest, err.Error())
	}

	unitChoices, setk8sUnits, unitParseErr := ParseUnitParams(c, "cores", "bytes")
	if unitParseErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": unitParseErr.Error()})
	}

	recommendationSet := model.RecommendationSet{}
	recommendationSets, count, queryErr := recommendationSet.GetRecommendationSets(OrgID, apiListOptions, queryParams, user_permissions)
	if queryErr != nil {
		log.Errorf("unable to fetch records from database; %v", queryErr)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch records from database",
		})
	}

	for i := range recommendationSets {
		recommendationSets[i].RecommendationsJSON = UpdateRecommendationJSON(
			handlerName,
			recommendationSets[i].ID,
			recommendationSets[i].ClusterUUID,
			unitChoices,
			setk8sUnits,
			recommendationSets[i].Recommendations,
			&recommendationSets[i].StoredVariationPcts,
		)
	}

	switch apiListOptions.Format {
	case listoptions.ResponseFormatJSON:
		interfaceSlice := make([]any, len(recommendationSets))
		for i, v := range recommendationSets {
			interfaceSlice[i] = v
		}
		results := CollectionResponse(interfaceSlice, c.Request(), count, apiListOptions.Limit, apiListOptions.Offset)
		return c.JSON(http.StatusOK, results)
	case listoptions.ResponseFormatCSV:
		filename := "recommendations-" + time.Now().Format("20060102")
		c.Response().Header().Set(echo.HeaderContentType, "text/csv")
		c.Response().Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s.csv", filename))
		pipeReader, pipeWriter := io.Pipe()

		go func() {
			var generationErr error
			defer func() {
				if r := recover(); r != nil {
					generationErr = fmt.Errorf("panic in CSV generation goroutine: %v", r)
				}
				if generationErr != nil {
					_ = pipeWriter.CloseWithError(generationErr)
					log.Errorf("error during CSV generation (recovered or returned): %v", generationErr)
				} else {
					_ = pipeWriter.Close() // graceful closure
				}
			}()
			generationErr = GenerateAndStreamCSV(pipeWriter, recommendationSets)
		}()
		return c.Stream(http.StatusOK, "text/csv", pipeReader)
	}
	return nil
}

func GetRecommendationSet(c echo.Context) error {
	XRHID := c.Get("Identity").(identity.XRHID)
	OrgID := XRHID.Identity.OrgID
	user_permissions := get_user_permissions(c)
	handlerName := "recommendationset"

	RecommendationIDStr := c.Param("recommendation-id")
	RecommendationUUID, err := uuid.Parse(RecommendationIDStr)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "bad recommendation_id"})
	}

	unitChoices, setk8sUnits, unitParseErr := ParseUnitParams(c, "cores", "MiB")
	if unitParseErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": unitParseErr.Error()})
	}

	recommendationSetVar := model.RecommendationSet{}
	recommendationSet, error := recommendationSetVar.GetRecommendationSetByID(OrgID, RecommendationUUID.String(), user_permissions)

	if error != nil {
		log.Errorf("unable to fetch recommendation %s; error %v", RecommendationIDStr, error)
		return c.JSON(http.StatusNotFound, echo.Map{"status": "error", "message": "unable to fetch recommendation"})
	}

	if len(recommendationSet.Recommendations) != 0 {
		recommendationSet.RecommendationsJSON = UpdateRecommendationJSON(
			handlerName,
			recommendationSet.ID,
			recommendationSet.ClusterUUID,
			unitChoices,
			setk8sUnits,
			recommendationSet.Recommendations,
			&recommendationSet.StoredVariationPcts,
		)
		return c.JSON(http.StatusOK, recommendationSet)
	} else {
		return c.JSON(http.StatusNotFound, echo.Map{"status": "not_found", "message": "recommendation not found"})
	}
}

func GetNamespaceRecommendationSetList(c echo.Context) error {
	XRHID := c.Get("Identity").(identity.XRHID)
	OrgID := XRHID.Identity.OrgID
	user_permissions := get_user_permissions(c)
	handlerName := "namespace-recommendationset-list"

	apiListOptions, listOptionsErr := listoptions.ListAPIOptions(c, listoptions.DefaultNsRecsDBColumn, listoptions.NsAllowedOrderBy)
	if listOptionsErr != nil {
		return apiErrResponse(c, listOptionsErr, http.StatusBadRequest, listOptionsErr.Error())
	}

	queryParams, paramErr := MapNamespaceQueryParameters(c)
	if paramErr != nil {
		return apiErrResponse(c, paramErr, http.StatusBadRequest, paramErr.Error())
	}

	unitChoices, setk8sUnits, err := ParseUnitParams(c, "cores", "bytes")
	if err != nil {
		return apiErrResponse(c, err, http.StatusBadRequest, err.Error())
	}

	NamespaceRecommendationSet := model.NamespaceRecommendationSet{}
	namespaceRecommendationSets, count, queryErr := NamespaceRecommendationSet.GetNamespaceRecommendationSets(
		OrgID, apiListOptions, queryParams, user_permissions,
	)

	if queryErr != nil {
		return apiErrResponse(c, queryErr, http.StatusServiceUnavailable, "unable to fetch records from database")
	}

	for i := range namespaceRecommendationSets {
		namespaceRecommendationSets[i].RecommendationsJSON = UpdateRecommendationJSON(
			handlerName,
			namespaceRecommendationSets[i].ID,
			namespaceRecommendationSets[i].ClusterUUID,
			unitChoices,
			setk8sUnits,
			namespaceRecommendationSets[i].Recommendations,
			&namespaceRecommendationSets[i].StoredVariationPcts,
		)
	}

	switch apiListOptions.Format {
	case listoptions.ResponseFormatJSON:
		interfaceSlice := make([]any, len(namespaceRecommendationSets))
		for i, v := range namespaceRecommendationSets {
			interfaceSlice[i] = v
		}
		results := CollectionResponse(interfaceSlice, c.Request(), count, apiListOptions.Limit, apiListOptions.Offset)
		return c.JSON(http.StatusOK, results)
	case listoptions.ResponseFormatCSV:
		// TODO: Add CSV support when export feature is enabled
		csvErr := errors.New("CSV format is not supported. Please use application/json")
		return apiErrResponse(c, csvErr, http.StatusNotAcceptable, csvErr.Error())
	}
	return nil

}

func GetNamespaceRecommendationSet(c echo.Context) error {
	XRHID := c.Get("Identity").(identity.XRHID)
	OrgID := XRHID.Identity.OrgID
	user_permissions := get_user_permissions(c)
	handlerName := "namespace-recommendationset"

	RecommendationIDStr := c.Param("recommendation-id")
	RecommendationUUID, err := uuid.Parse(RecommendationIDStr)
	if err != nil {
		return apiErrResponse(c, err, http.StatusBadRequest, "bad recommendation-id for project")
	}

	unitChoices, setk8sUnits, unitParseErr := ParseUnitParams(c, "cores", "bytes")
	if unitParseErr != nil {
		return apiErrResponse(c, unitParseErr, http.StatusBadRequest, unitParseErr.Error())
	}

	recommendationSetVar := model.NamespaceRecommendationSet{}
	nsRecommendationSet, getNSRecordErr := recommendationSetVar.GetNamespaceRecommendationSetByID(
		OrgID,
		RecommendationUUID.String(),
		user_permissions,
	)

	if getNSRecordErr != nil {
		return apiErrResponse(c, getNSRecordErr, http.StatusNotFound, "unable to fetch project recommendation")
	}

	if len(nsRecommendationSet.Recommendations) != 0 {
		nsRecommendationSet.RecommendationsJSON = UpdateRecommendationJSON(
			handlerName,
			nsRecommendationSet.ID,
			nsRecommendationSet.ClusterUUID,
			unitChoices,
			setk8sUnits,
			nsRecommendationSet.Recommendations,
			&nsRecommendationSet.StoredVariationPcts,
		)
	}
	return c.JSON(http.StatusOK, nsRecommendationSet)
}

// parseGPUFilters extracts GPU-specific query parameters. These are applied
// post-enrichment since GPU data lives in a separate table.
func parseGPUFilters(c echo.Context) (hasGPU *bool, gpuModels, gpuClassifications []string) {
	if v := c.QueryParam("has_gpu"); v != "" {
		b := v == "true" || v == "1"
		hasGPU = &b
	}
	if models := c.QueryParams()["gpu_model"]; len(models) > 0 {
		gpuModels = models
	}
	if classes := c.QueryParams()["gpu_classification"]; len(classes) > 0 {
		gpuClassifications = classes
	}
	return
}

// MapNativeQueryParameters parses query params using the native schema's column names.
func MapNativeQueryParameters(c echo.Context) (map[string]interface{}, error) {
	queryParams := make(map[string]interface{})
	var startTimestamp, endTimestamp time.Time

	now := time.Now().UTC()
	firstOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	startDateStr := c.QueryParam("start_date")
	if startDateStr == "" {
		startTimestamp = firstOfMonth
	} else {
		var err error
		startTimestamp, err = time.Parse(timeLayout, startDateStr)
		if err != nil {
			return queryParams, err
		}
	}
	queryParams["rs.updated_at >= ?"] = startTimestamp

	endDateStr := c.QueryParam("end_date")
	if endDateStr == "" {
		endTimestamp = now.Add(time.Second)
	} else {
		var err error
		endTimestamp, err = time.Parse(timeLayout, endDateStr)
		if err != nil {
			return queryParams, err
		}
		endTimestamp = endTimestamp.Add(24 * time.Hour)
	}
	queryParams["rs.updated_at < ?"] = endTimestamp

	if clusters := c.QueryParams()["cluster"]; len(clusters) > 0 {
		queryParams["c.cluster_alias IN ?"] = clusters
	}
	if projects := c.QueryParams()["project"]; len(projects) > 0 {
		queryParams["rs.namespace IN ?"] = projects
	}
	if workloads := c.QueryParams()["workload"]; len(workloads) > 0 {
		queryParams["rs.workload IN ?"] = workloads
	}
	if workloadTypes := c.QueryParams()["workload_type"]; len(workloadTypes) > 0 {
		queryParams["rs.workload_type IN ?"] = workloadTypes
	}
	if containers := c.QueryParams()["container"]; len(containers) > 0 {
		queryParams["rs.container_name IN ?"] = containers
	}

	return queryParams, nil
}

// GetNativeRecommendationSetList serves recommendations from the native Go engine
// using relational columns instead of JSONB.
func GetNativeRecommendationSetList(c echo.Context) error {
	XRHID := c.Get("Identity").(identity.XRHID)
	OrgID := XRHID.Identity.OrgID
	userPerms := get_user_permissions(c)

	apiListOptions, err := listoptions.ListAPIOptions(c, listoptions.DefaultContainerRecsDBColumn, listoptions.ContainerAllowedOrderBy)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
	}

	queryParams, err := MapNativeQueryParameters(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
	}

	results, count, queryErr := model.GetNativeRecommendations(OrgID, apiListOptions, queryParams, userPerms)
	if queryErr != nil {
		log.Errorf("unable to fetch native recommendations; %v", queryErr)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch records from database",
		})
	}

	enrichWithGPU(results, OrgID)

	hasGPU, gpuModels, gpuClassifications := parseGPUFilters(c)
	results, count = filterGPUResults(results, hasGPU, gpuModels, gpuClassifications)

	switch apiListOptions.Format {
	case listoptions.ResponseFormatCSV:
		filename := "recommendations-" + time.Now().Format("20060102")
		c.Response().Header().Set(echo.HeaderContentType, "text/csv")
		c.Response().Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s.csv", filename))
		pipeReader, pipeWriter := io.Pipe()
		go func() {
			var genErr error
			defer func() {
				if r := recover(); r != nil {
					genErr = fmt.Errorf("panic in native CSV generation: %v", r)
				}
				if genErr != nil {
					_ = pipeWriter.CloseWithError(genErr)
				} else {
					_ = pipeWriter.Close()
				}
			}()
			genErr = GenerateNativeCSV(pipeWriter, results)
		}()
		return c.Stream(http.StatusOK, "text/csv", pipeReader)
	default:
		interfaceSlice := make([]any, len(results))
		for i := range results {
			interfaceSlice[i] = model.BuildDetailResponse(&results[i], nil, time.Time{})
		}
		response := CollectionResponse(interfaceSlice, c.Request(), count, apiListOptions.Limit, apiListOptions.Offset)
		return c.JSON(http.StatusOK, response)
	}
}

// GetNativeRecommendationSet returns a single container's native recommendations by deterministic UUID.
// The response is wrapped in the Kruize-compatible shape including boxplots and monitoring_end_time.
func GetNativeRecommendationSet(c echo.Context) error {
	XRHID := c.Get("Identity").(identity.XRHID)
	OrgID := XRHID.Identity.OrgID
	userPerms := get_user_permissions(c)

	idStr := c.Param("recommendation-id")
	if _, err := uuid.Parse(idStr); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "bad recommendation_id"})
	}

	result, err := model.GetNativeRecommendationByID(OrgID, idStr, userPerms)
	if err != nil {
		log.Errorf("unable to fetch native recommendation %s; %v", idStr, err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch recommendation",
		})
	}
	if result == nil {
		return c.JSON(http.StatusNotFound, echo.Map{"status": "not_found", "message": "recommendation not found"})
	}

	detail := enrichNativeDetail(OrgID, result)
	return c.JSON(http.StatusOK, detail)
}

// GetRecommendationSetListWithFallback tries the native engine first. If it
// returns zero results, falls back to the legacy Kruize JSONB path. This
// enables zero-downtime migration: containers not yet reprocessed by the
// native engine are still served from the old data.
func GetRecommendationSetListWithFallback(c echo.Context) error {
	XRHID := c.Get("Identity").(identity.XRHID)
	OrgID := XRHID.Identity.OrgID
	userPerms := get_user_permissions(c)

	apiListOptions, err := listoptions.ListAPIOptions(c, listoptions.DefaultContainerRecsDBColumn, listoptions.ContainerAllowedOrderBy)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
	}

	queryParams, err := MapNativeQueryParameters(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
	}

	results, _, queryErr := model.GetNativeRecommendations(OrgID, apiListOptions, queryParams, userPerms)
	if queryErr != nil {
		log.Errorf("unable to fetch native recommendations; %v", queryErr)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch records from database",
		})
	}

	enrichWithGPU(results, OrgID)

	hasGPU, gpuModels, gpuClassifications := parseGPUFilters(c)
	results, count := filterGPUResults(results, hasGPU, gpuModels, gpuClassifications)

	return serveNativeList(c, results, count, apiListOptions)
}

// GetRecommendationSetWithFallback tries the native detail lookup first.
// If not found, falls back to the legacy Kruize JSONB lookup. Native uses a
// deterministic UUID v5 (from composite key), while legacy uses the DB row's
// random UUID — both are valid UUIDs from different namespaces so there is no
// collision risk.
func GetRecommendationSetWithFallback(c echo.Context) error {
	XRHID := c.Get("Identity").(identity.XRHID)
	OrgID := XRHID.Identity.OrgID
	userPerms := get_user_permissions(c)

	idStr := c.Param("recommendation-id")
	if _, err := uuid.Parse(idStr); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "bad recommendation_id"})
	}

	result, err := model.GetNativeRecommendationByID(OrgID, idStr, userPerms)
	if err != nil {
		log.Errorf("unable to fetch native recommendation %s; %v", idStr, err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch recommendation",
		})
	}
	if result != nil {
		detail := enrichNativeDetail(OrgID, result)
		return c.JSON(http.StatusOK, detail)
	}

	return c.JSON(http.StatusNotFound, echo.Map{
		"status":  "error",
		"message": fmt.Sprintf("recommendation %s not found", idStr),
	})
}

func serveNativeList(c echo.Context, results []model.NativeContainerResult, count int, opts listoptions.ListOptions) error {
	switch opts.Format {
	case listoptions.ResponseFormatCSV:
		filename := "recommendations-" + time.Now().Format("20060102")
		c.Response().Header().Set(echo.HeaderContentType, "text/csv")
		c.Response().Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s.csv", filename))
		pipeReader, pipeWriter := io.Pipe()
		go func() {
			var genErr error
			defer func() {
				if r := recover(); r != nil {
					genErr = fmt.Errorf("panic in native CSV generation: %v", r)
				}
				if genErr != nil {
					_ = pipeWriter.CloseWithError(genErr)
				} else {
					_ = pipeWriter.Close()
				}
			}()
			genErr = GenerateNativeCSV(pipeWriter, results)
		}()
		return c.Stream(http.StatusOK, "text/csv", pipeReader)
	default:
		interfaceSlice := make([]any, len(results))
		for i := range results {
			interfaceSlice[i] = model.BuildDetailResponse(&results[i], nil, time.Time{})
		}
		response := CollectionResponse(interfaceSlice, c.Request(), count, opts.Limit, opts.Offset)
		return c.JSON(http.StatusOK, response)
	}
}

func serveLegacyList(c echo.Context, orgID string, opts listoptions.ListOptions, userPerms map[string][]string) error {
	queryParams, err := MapQueryParameters(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
	}

	unitChoices, setk8sUnits, unitParseErr := ParseUnitParams(c, "cores", "bytes")
	if unitParseErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": unitParseErr.Error()})
	}

	recSet := model.RecommendationSet{}
	recSets, count, queryErr := recSet.GetRecommendationSets(orgID, opts, queryParams, userPerms)
	if queryErr != nil {
		log.Errorf("unable to fetch legacy records from database; %v", queryErr)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch records from database",
		})
	}

	for i := range recSets {
		recSets[i].RecommendationsJSON = UpdateRecommendationJSON(
			"recommendationset-list",
			recSets[i].ID,
			recSets[i].ClusterUUID,
			unitChoices,
			setk8sUnits,
			recSets[i].Recommendations,
		)
	}

	switch opts.Format {
	case listoptions.ResponseFormatCSV:
		filename := "recommendations-" + time.Now().Format("20060102")
		c.Response().Header().Set(echo.HeaderContentType, "text/csv")
		c.Response().Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s.csv", filename))
		pipeReader, pipeWriter := io.Pipe()
		go func() {
			var genErr error
			defer func() {
				if r := recover(); r != nil {
					genErr = fmt.Errorf("panic in legacy CSV generation: %v", r)
				}
				if genErr != nil {
					_ = pipeWriter.CloseWithError(genErr)
				} else {
					_ = pipeWriter.Close()
				}
			}()
			genErr = GenerateAndStreamCSV(pipeWriter, recSets)
		}()
		return c.Stream(http.StatusOK, "text/csv", pipeReader)
	default:
		interfaceSlice := make([]any, len(recSets))
		for i, v := range recSets {
			interfaceSlice[i] = v
		}
		response := CollectionResponse(interfaceSlice, c.Request(), count, opts.Limit, opts.Offset)
		return c.JSON(http.StatusOK, response)
	}
}

func serveLegacyDetail(c echo.Context, orgID, idStr string, userPerms map[string][]string) error {
	unitChoices, setk8sUnits, unitParseErr := ParseUnitParams(c, "cores", "MiB")
	if unitParseErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": unitParseErr.Error()})
	}

	recSetVar := model.RecommendationSet{}
	recSet, err := recSetVar.GetRecommendationSetByID(orgID, idStr, userPerms)
	if err != nil {
		log.Errorf("legacy fallback: unable to fetch recommendation %s; %v", idStr, err)
		return c.JSON(http.StatusNotFound, echo.Map{"status": "error", "message": "unable to fetch recommendation"})
	}

	if len(recSet.Recommendations) == 0 {
		return c.JSON(http.StatusNotFound, echo.Map{"status": "not_found", "message": "recommendation not found"})
	}

	recSet.RecommendationsJSON = UpdateRecommendationJSON(
		"recommendationset",
		recSet.ID,
		recSet.ClusterUUID,
		unitChoices,
		setk8sUnits,
		recSet.Recommendations,
	)
	return c.JSON(http.StatusOK, recSet)
}

// GetNamespaceRecommendationSetListWithFallback tries the native namespace
// engine first. If it returns zero results, falls back to the legacy Kruize
// JSONB path.
func GetNamespaceRecommendationSetListWithFallback(c echo.Context) error {
	XRHID := c.Get("Identity").(identity.XRHID)
	OrgID := XRHID.Identity.OrgID
	userPerms := get_user_permissions(c)

	apiListOptions, err := listoptions.ListAPIOptions(c, listoptions.DefaultNsRecsDBColumn, listoptions.NsAllowedOrderBy)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
	}

	queryParams, err := MapNativeNamespaceQueryParameters(c)
	if err != nil {
		log.Error(err.Error())
		var pe *ParamError
		if errors.As(err, &pe) && pe.UserErr {
			return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
		}
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "unable to parse query parameters"})
	}

	results, count, queryErr := model.GetNativeNamespaceRecommendations(OrgID, apiListOptions, queryParams, userPerms)
	if queryErr != nil {
		log.Errorf("unable to fetch native namespace recommendations; %v", queryErr)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch records from database",
		})
	}

	if count > 0 {
		return serveNativeNamespaceList(c, results, int(count), apiListOptions)
	}

	log.Info("native namespace engine returned 0 results, falling back to Kruize path")
	return GetNamespaceRecommendationSetList(c)
}

func serveNativeNamespaceList(c echo.Context, results []model.NativeNamespaceResult, count int, opts listoptions.ListOptions) error {
	switch opts.Format {
	case listoptions.ResponseFormatCSV:
		filename := "namespace-recommendations-" + time.Now().Format("20060102")
		c.Response().Header().Set(echo.HeaderContentType, "text/csv")
		c.Response().Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s.csv", filename))
		pipeReader, pipeWriter := io.Pipe()
		go func() {
			var genErr error
			defer func() {
				if r := recover(); r != nil {
					genErr = fmt.Errorf("panic in native namespace CSV generation: %v", r)
				}
				if genErr != nil {
					_ = pipeWriter.CloseWithError(genErr)
				} else {
					_ = pipeWriter.Close()
				}
			}()
			genErr = GenerateNativeNamespaceCSV(pipeWriter, results)
		}()
		return c.Stream(http.StatusOK, "text/csv", pipeReader)
	default:
		interfaceSlice := make([]any, len(results))
		for i, v := range results {
			interfaceSlice[i] = v
		}
		response := CollectionResponse(interfaceSlice, c.Request(), count, opts.Limit, opts.Offset)
		return c.JSON(http.StatusOK, response)
	}
}

// GetNamespaceRecommendationSetWithFallback tries the native namespace detail
// lookup first. If not found, falls back to the legacy Kruize JSONB lookup.
func GetNamespaceRecommendationSetWithFallback(c echo.Context) error {
	XRHID := c.Get("Identity").(identity.XRHID)
	OrgID := XRHID.Identity.OrgID
	userPerms := get_user_permissions(c)

	idStr := c.Param("recommendation-id")
	if _, err := uuid.Parse(idStr); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "bad recommendation-id"})
	}

	result, err := model.GetNativeNamespaceRecommendationByID(OrgID, idStr, userPerms)
	if err != nil {
		log.Errorf("unable to fetch native namespace recommendation %s; %v", idStr, err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch recommendation",
		})
	}
	if result != nil {
		enriched := enrichNativeNamespaceDetail(OrgID, result)
		return c.JSON(http.StatusOK, enriched)
	}

	log.Infof("native namespace detail miss for %s, falling back to Kruize path", idStr)
	return GetNamespaceRecommendationSet(c)
}

// enrichNativeDetail fetches boxplots and monitoring_end_time for a native
// recommendation and wraps it in the Kruize-compatible DetailResponse shape.
func enrichNativeDetail(orgID string, result *model.NativeContainerResult) *model.DetailResponse {
	ctx := context.Background()
	pool := db.GetPool()

	key := model.ContainerKey{
		OrgID:         orgID,
		ClusterUUID:   result.ClusterUUID,
		Namespace:     result.Project,
		Workload:      result.Workload,
		ContainerName: result.Container,
	}

	plots := map[string]*model.NativePlot{}
	var met time.Time

	if pool != nil {
		for termKey := range result.Recommendations {
			p, err := model.AssembleBoxplots(ctx, pool, key, termKey, orgID)
			if err != nil {
				log.Warnf("boxplot assembly failed for container %s/%s term %s: %v", key.Namespace, key.ContainerName, termKey, err)
			}
			if p != nil {
				plots[termKey] = p
			}
		}
		met, _ = model.MonitoringEndTime(ctx, pool, key)
	}

	singleSlice := []model.NativeContainerResult{*result}
	enrichWithGPU(singleSlice, orgID)
	*result = singleSlice[0]

	return model.BuildDetailResponse(result, plots, met)
}

// enrichNativeNamespaceDetail fetches boxplots for a native namespace
// recommendation and embeds them into the response alongside monitoring_end_time.
func enrichNativeNamespaceDetail(orgID string, result *model.NativeNamespaceResult) *model.NativeNamespaceResult {
	ctx := context.Background()
	pool := db.GetPool()
	if pool == nil {
		return result
	}

	key := model.NamespaceKey{
		OrgID:       orgID,
		ClusterUUID: result.ClusterUUID,
		Namespace:   result.Project,
	}

	termPlots := map[string]*model.NativePlot{}
	for termKey := range result.Recommendations {
		if termKey == "monitoring_end_time" {
			continue
		}
		p, err := model.AssembleNamespaceBoxplots(ctx, pool, key, termKey, orgID)
		if err != nil {
			log.Warnf("namespace boxplot assembly failed for %s/%s term %s: %v", key.ClusterUUID, key.Namespace, termKey, err)
		}
		if p != nil {
			termPlots[termKey] = p
		}
	}

	met, _ := model.NamespaceMonitoringEndTime(ctx, pool, key)
	if !met.IsZero() && met.Year() > 1 {
		result.Recommendations["monitoring_end_time"] = met.UTC().Format(time.RFC3339)
	}

	for termKey, plot := range termPlots {
		termVal, ok := result.Recommendations[termKey]
		if !ok {
			continue
		}
		if termRec, ok := termVal.(model.TermRecommendation); ok {
			termRec.Plots = plot
			result.Recommendations[termKey] = termRec
		}
	}

	return result
}

func GetAppStatus(c echo.Context) error {
	status := map[string]string{
		"api-server": "working",
	}
	return c.JSON(http.StatusOK, status)
}
