package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

func GetRecommendationSetList(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	OrgID := xrhid.Identity.OrgID
	user_permissions := get_user_permissions(c)
	handlerName := "recommendationset-list"
	hlog := requestLogger(c, OrgID)

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
		hlog.Errorf("unable to fetch records from database: %v", queryErr)
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
		setRecommendationNoStore(c)
		return c.JSON(http.StatusOK, results)
	case listoptions.ResponseFormatCSV:
		filename := "recommendations-" + time.Now().Format("20060102")
		c.Response().Header().Set(echo.HeaderContentType, "text/csv")
		c.Response().Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s.csv", filename))
		pipeReader, pipeWriter := io.Pipe()
		reqCtx := c.Request().Context()

		go func() {
			var generationErr error
			defer func() {
				if r := recover(); r != nil {
					generationErr = fmt.Errorf("panic in CSV generation goroutine: %v", r)
				}
				if generationErr != nil {
					_ = pipeWriter.CloseWithError(generationErr)
					hlog.Errorf("error during CSV generation: %v", generationErr)
				} else {
					_ = pipeWriter.Close() // graceful closure
				}
			}()
			generationErr = GenerateAndStreamCSV(reqCtx, pipeWriter, recommendationSets)
		}()
		return c.Stream(http.StatusOK, "text/csv", pipeReader)
	}
	return nil
}

func GetRecommendationSet(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	OrgID := xrhid.Identity.OrgID
	user_permissions := get_user_permissions(c)
	handlerName := "recommendationset"
	hlog := requestLogger(c, OrgID)

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
		hlog.WithField("recommendation_id", RecommendationIDStr).Errorf("unable to fetch recommendation: %v", error)
		return c.JSON(http.StatusNotFound, echo.Map{"status": "not_found", "message": "unable to fetch recommendation"})
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
		setRecommendationNoStore(c)
		return c.JSON(http.StatusOK, recommendationSet)
	} else {
		return c.JSON(http.StatusNotFound, echo.Map{"status": "not_found", "message": "recommendation not found"})
	}
}

func GetNamespaceRecommendationSetList(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	OrgID := xrhid.Identity.OrgID
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
		setRecommendationNoStore(c)
		return c.JSON(http.StatusOK, results)
	case listoptions.ResponseFormatCSV:
		// TODO: Add CSV support when export feature is enabled
		csvErr := errors.New("CSV format is not supported. Please use application/json")
		return apiErrResponse(c, csvErr, http.StatusNotAcceptable, csvErr.Error())
	}
	return nil

}

func GetNamespaceRecommendationSet(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	OrgID := xrhid.Identity.OrgID
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

	if len(nsRecommendationSet.Recommendations) == 0 {
		return c.JSON(http.StatusNotFound, echo.Map{"status": "not_found", "message": "recommendation not found"})
	}
	nsRecommendationSet.RecommendationsJSON = UpdateRecommendationJSON(
		handlerName,
		nsRecommendationSet.ID,
		nsRecommendationSet.ClusterUUID,
		unitChoices,
		setk8sUnits,
		nsRecommendationSet.Recommendations,
		&nsRecommendationSet.StoredVariationPcts,
	)
	setRecommendationNoStore(c)
	return c.JSON(http.StatusOK, nsRecommendationSet)
}

// parseGPUFilters extracts GPU-specific query parameters that require post-query
// filtering. Note: has_gpu is now pushed to SQL via MapNativeQueryParameters for
// correct pagination; only gpu_model and gpu_classification remain as post-filters.
// parseGPUFilters is retained for backward compatibility but all GPU filters
// (has_gpu, gpu_model, gpu_classification) are now pushed to SQL in
// MapNativeQueryParameters. This function is a no-op.
func parseGPUFilters(_ echo.Context) (gpuModels, gpuClassifications []string) {
	return nil, nil
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

	var filterErrs []error
	workloadTypeVals := slices.Concat(
		c.QueryParams()["workload_type"],
		c.QueryParams()["filter[exact:workload_type]"],
		c.QueryParams()["exclude[workload_type]"],
	)
	if err := validateWorkloadTypeValues(workloadTypeVals); err != nil {
		filterErrs = append(filterErrs, err)
	}
	if err := applyNativeParamFilter(c, queryParams, "cluster", "", model.ClusterMaxLen, true); err != nil {
		filterErrs = append(filterErrs, err)
	}
	if err := applyNativeParamFilter(c, queryParams, "project", "rs.namespace", model.NamespaceMaxLen, false); err != nil {
		filterErrs = append(filterErrs, err)
	}
	if err := applyNativeParamFilter(c, queryParams, "workload", "rs.workload", model.ClusterMaxLen, true); err != nil {
		filterErrs = append(filterErrs, err)
	}
	if err := applyNativeParamFilter(c, queryParams, "workload_type", "rs.workload_type", model.NamespaceMaxLen, false); err != nil {
		filterErrs = append(filterErrs, err)
	}
	if err := applyNativeParamFilter(c, queryParams, "container", "rs.container_name", model.NamespaceMaxLen, false); err != nil {
		filterErrs = append(filterErrs, err)
	}
	if len(filterErrs) > 0 {
		return queryParams, errors.Join(filterErrs...)
	}

	if config.TagsFeatureEnabled() {
		tagFilters, err := parseTagFiltersFromRequest(c)
		if err != nil {
			return queryParams, err
		}
		if len(tagFilters) > 0 {
			queryParams[model.TagFiltersQueryKey] = tagFilters
		}
	}

	// Stale filter: by default, exclude stale. If ?stale=true, include all.
	// If ?stale=false (explicit), exclude stale. If ?stale=only, show only stale.
	staleParam := c.QueryParam("stale")
	switch staleParam {
	case "true":
		// No filter — return both stale and non-stale
	case "only":
		queryParams["rs.stale = ?"] = true
	default:
		// "false" or unset: exclude stale (backward compatible)
		queryParams["rs.stale = ?"] = false
	}

	// GPU presence filter: pushed to SQL for correct pagination.
	if v := c.QueryParam("has_gpu"); v != "" {
		queryParams["rs.has_gpu = ?"] = (v == "true" || v == "1")
	}

	// GPU model filter: substring match pushed to SQL via ILIKE.
	if models := c.QueryParams()["gpu_model"]; len(models) > 0 {
		clauses := make([]string, 0, len(models))
		vals := make([]string, 0, len(models))
		for _, m := range models {
			if m != "" {
				clauses = append(clauses, "rs.gpu_model_name ILIKE ?")
				vals = append(vals, "%"+m+"%")
			}
		}
		if len(clauses) > 0 {
			key := strings.Join(clauses, " OR ")
			queryParams[key] = vals
		}
	}

	// GPU classification filter: exact match pushed to SQL.
	if classes := c.QueryParams()["gpu_classification"]; len(classes) > 0 {
		var vals []string
		for _, cl := range classes {
			if cl != "" {
				vals = append(vals, cl)
			}
		}
		if len(vals) > 0 {
			queryParams["rs.gpu_classification IN ?"] = vals
		}
	}

	return queryParams, nil
}

// GetNativeRecommendationSetList serves recommendations from the native Go engine
// using relational columns instead of JSONB.
func GetNativeRecommendationSetList(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	OrgID := xrhid.Identity.OrgID
	userPerms := get_user_permissions(c)
	hlog := requestLogger(c, OrgID)

	apiListOptions, err := listoptions.ListAPIOptions(c, listoptions.DefaultContainerRecsDBColumn, listoptions.ContainerAllowedOrderBy)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
	}
	if err := applyContainerListCursor(c, &apiListOptions); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
	}

	queryParams, err := MapNativeQueryParameters(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
	}

	page, queryErr := model.GetNativeRecommendations(OrgID, apiListOptions, queryParams, userPerms)
	if queryErr != nil {
		hlog.Errorf("unable to fetch native recommendations: %v", queryErr)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch records from database",
		})
	}

	results := page.Results
	count := page.Count

	EnrichNativeContainerResults(c.Request().Context(), &NativeContainerEnrichmentInput{OrgID: OrgID, Results: results})

	gpuModels, gpuClassifications := parseGPUFilters(c)
	results, count = filterGPUResults(results, count, gpuModels, gpuClassifications)
	page.Results = results
	page.Count = count

	switch apiListOptions.Format {
	case listoptions.ResponseFormatCSV:
		filename := "recommendations-" + time.Now().Format("20060102")
		c.Response().Header().Set(echo.HeaderContentType, "text/csv")
		c.Response().Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s.csv", filename))
		pipeReader, pipeWriter := io.Pipe()
		reqCtx := c.Request().Context()
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
			genErr = GenerateNativeCSV(reqCtx, pipeWriter, results)
		}()
		return c.Stream(http.StatusOK, "text/csv", pipeReader)
	default:
		interfaceSlice := make([]any, len(results))
		for i := range results {
			interfaceSlice[i] = model.BuildDetailResponse(&results[i], nil, time.Time{})
		}
		response := buildContainerListMeta(c, page, apiListOptions)
		response.Data = interfaceSlice
		setRecommendationNoStore(c)
		return c.JSON(http.StatusOK, response)
	}
}

// GetNativeRecommendationSet returns a single container's native recommendations by deterministic UUID.
// The response is wrapped in the Kruize-compatible shape including boxplots and monitoring_end_time.
func GetNativeRecommendationSet(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	OrgID := xrhid.Identity.OrgID
	userPerms := get_user_permissions(c)
	hlog := requestLogger(c, OrgID)

	idStr := c.Param("recommendation-id")
	if _, err := uuid.Parse(idStr); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "bad recommendation_id"})
	}

	result, err := model.GetNativeRecommendationByID(OrgID, idStr, userPerms)
	if err != nil {
		hlog.WithField("recommendation_id", idStr).Errorf("unable to fetch native recommendation: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch recommendation",
		})
	}
	if result == nil {
		return c.JSON(http.StatusNotFound, echo.Map{"status": "not_found", "message": "recommendation not found"})
	}

	detail := enrichNativeDetail(c.Request().Context(), OrgID, result)
	setRecommendationNoStore(c)
	return c.JSON(http.StatusOK, detail)
}

// GetRecommendationSetListWithFallback tries the native engine first. If it
// returns zero results, falls back to the legacy Kruize JSONB path. This
// enables zero-downtime migration: containers not yet reprocessed by the
// native engine are still served from the old data.
func GetRecommendationSetListWithFallback(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	OrgID := xrhid.Identity.OrgID
	userPerms := get_user_permissions(c)
	hlog := requestLogger(c, OrgID)

	apiListOptions, err := listoptions.ListAPIOptions(c, listoptions.DefaultContainerRecsDBColumn, listoptions.ContainerAllowedOrderBy)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
	}
	if err := applyContainerListCursor(c, &apiListOptions); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
	}

	queryParams, err := MapNativeQueryParameters(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
	}

	page, queryErr := model.GetNativeRecommendations(OrgID, apiListOptions, queryParams, userPerms)
	if queryErr != nil {
		hlog.Errorf("unable to fetch native recommendations: %v", queryErr)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch records from database",
		})
	}

	results := page.Results
	EnrichNativeContainerResults(c.Request().Context(), &NativeContainerEnrichmentInput{OrgID: OrgID, Results: results})

	gpuModels, gpuClassifications := parseGPUFilters(c)
	results, count := filterGPUResults(results, page.Count, gpuModels, gpuClassifications)
	page.Results = results
	page.Count = count

	return serveNativeList(c, page, apiListOptions)
}

// GetRecommendationSetWithFallback tries the native detail lookup first.
// If not found, falls back to the legacy Kruize JSONB lookup. Native uses a
// deterministic UUID v5 (from composite key), while legacy uses the DB row's
// random UUID — both are valid UUIDs from different namespaces so there is no
// collision risk.
func GetRecommendationSetWithFallback(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	OrgID := xrhid.Identity.OrgID
	userPerms := get_user_permissions(c)
	hlog := requestLogger(c, OrgID)

	idStr := c.Param("recommendation-id")
	if _, err := uuid.Parse(idStr); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "bad recommendation_id"})
	}

	result, err := model.GetNativeRecommendationByID(OrgID, idStr, userPerms)
	if err != nil {
		hlog.WithField("recommendation_id", idStr).Errorf("unable to fetch native recommendation: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch recommendation",
		})
	}
	if result != nil {
		detail := enrichNativeDetail(c.Request().Context(), OrgID, result)
		setRecommendationNoStore(c)
		return c.JSON(http.StatusOK, detail)
	}

	return serveLegacyDetail(c, OrgID, idStr, userPerms)
}

func serveNativeList(c echo.Context, page model.NativeListPage, opts listoptions.ListOptions) error {
	results := page.Results
	switch opts.Format {
	case listoptions.ResponseFormatCSV:
		filename := "recommendations-" + time.Now().Format("20060102")
		c.Response().Header().Set(echo.HeaderContentType, "text/csv")
		c.Response().Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s.csv", filename))
		pipeReader, pipeWriter := io.Pipe()
		reqCtx := c.Request().Context()
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
			genErr = GenerateNativeCSV(reqCtx, pipeWriter, results)
		}()
		return c.Stream(http.StatusOK, "text/csv", pipeReader)
	default:
		interfaceSlice := make([]any, len(results))
		for i := range results {
			interfaceSlice[i] = model.BuildDetailResponse(&results[i], nil, results[i].MonitoringEndTime)
		}
		response := buildContainerListMeta(c, page, opts)
		response.Data = interfaceSlice
		setRecommendationNoStore(c)
		return c.JSON(http.StatusOK, response)
	}
}

func serveLegacyList(c echo.Context, orgID string, opts listoptions.ListOptions, userPerms map[string][]string) error {
	hlog := requestLogger(c, orgID)
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
		hlog.Errorf("unable to fetch legacy records from database: %v", queryErr)
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
			&recSets[i].StoredVariationPcts,
		)
	}

	switch opts.Format {
	case listoptions.ResponseFormatCSV:
		filename := "recommendations-" + time.Now().Format("20060102")
		c.Response().Header().Set(echo.HeaderContentType, "text/csv")
		c.Response().Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s.csv", filename))
		pipeReader, pipeWriter := io.Pipe()
		reqCtx := c.Request().Context()
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
			genErr = GenerateAndStreamCSV(reqCtx, pipeWriter, recSets)
		}()
		return c.Stream(http.StatusOK, "text/csv", pipeReader)
	default:
		interfaceSlice := make([]any, len(recSets))
		for i, v := range recSets {
			interfaceSlice[i] = v
		}
		response := CollectionResponse(interfaceSlice, c.Request(), count, opts.Limit, opts.Offset)
		setRecommendationNoStore(c)
		return c.JSON(http.StatusOK, response)
	}
}

func serveLegacyDetail(c echo.Context, orgID, idStr string, userPerms map[string][]string) error {
	hlog := requestLogger(c, orgID)
	unitChoices, setk8sUnits, unitParseErr := ParseUnitParams(c, "cores", "MiB")
	if unitParseErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": unitParseErr.Error()})
	}

	recSetVar := model.RecommendationSet{}
	recSet, err := recSetVar.GetRecommendationSetByID(orgID, idStr, userPerms)
	if err != nil {
		hlog.WithField("recommendation_id", idStr).Errorf("legacy fallback: unable to fetch recommendation: %v", err)
		return c.JSON(http.StatusNotFound, echo.Map{"status": "not_found", "message": "unable to fetch recommendation"})
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
		&recSet.StoredVariationPcts,
	)
	setRecommendationNoStore(c)
	return c.JSON(http.StatusOK, recSet)
}

// GetNamespaceRecommendationSetListWithFallback tries the native namespace
// engine first. If it returns zero results, falls back to the legacy Kruize
// JSONB path.
func GetNamespaceRecommendationSetListWithFallback(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	OrgID := xrhid.Identity.OrgID
	userPerms := get_user_permissions(c)
	hlog := requestLogger(c, OrgID)

	apiListOptions, err := listoptions.ListAPIOptions(c, listoptions.DefaultNsRecsDBColumn, listoptions.NsAllowedOrderBy)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
	}
	if err := applyNamespaceListCursor(c, &apiListOptions); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
	}

	queryParams, err := MapNativeNamespaceQueryParameters(c)
	if err != nil {
		hlog.Error(err.Error())
		var pe *ParamError
		if errors.As(err, &pe) && pe.UserErr {
			return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
		}
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "unable to parse query parameters"})
	}

	page, queryErr := model.GetNativeNamespaceRecommendations(OrgID, apiListOptions, queryParams, userPerms)
	if queryErr != nil {
		hlog.Errorf("unable to fetch native namespace recommendations: %v", queryErr)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch records from database",
		})
	}

	if page.Count > 0 {
		EnrichNativeNamespaceResults(c.Request().Context(), OrgID, page.Results)
		return serveNativeNamespaceList(c, page, apiListOptions)
	}

	hlog.Info("native namespace engine returned 0 results, falling back to Kruize path")
	return GetNamespaceRecommendationSetList(c)
}

func serveNativeNamespaceList(c echo.Context, page model.NativeNamespaceListPage, opts listoptions.ListOptions) error {
	results := page.Results
	switch opts.Format {
	case listoptions.ResponseFormatCSV:
		filename := "namespace-recommendations-" + time.Now().Format("20060102")
		c.Response().Header().Set(echo.HeaderContentType, "text/csv")
		c.Response().Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s.csv", filename))
		pipeReader, pipeWriter := io.Pipe()
		reqCtx := c.Request().Context()
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
			genErr = GenerateNativeNamespaceCSV(reqCtx, pipeWriter, results)
		}()
		return c.Stream(http.StatusOK, "text/csv", pipeReader)
	default:
		interfaceSlice := make([]any, len(results))
		for i := range results {
			interfaceSlice[i] = model.BuildNamespaceDetailResponse(&results[i], nil, time.Time{})
		}
		response := buildNamespaceListMeta(c, page, opts)
		response.Data = interfaceSlice
		setRecommendationNoStore(c)
		return c.JSON(http.StatusOK, response)
	}
}

// GetNamespaceRecommendationSetWithFallback tries the native namespace detail
// lookup first. If not found, falls back to the legacy Kruize JSONB lookup.
func GetNamespaceRecommendationSetWithFallback(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	OrgID := xrhid.Identity.OrgID
	userPerms := get_user_permissions(c)
	hlog := requestLogger(c, OrgID)

	idStr := c.Param("recommendation-id")
	if _, err := uuid.Parse(idStr); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "bad recommendation-id"})
	}

	result, err := model.GetNativeNamespaceRecommendationByID(OrgID, idStr, userPerms)
	if err != nil {
		hlog.WithField("recommendation_id", idStr).Errorf("unable to fetch native namespace recommendation: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch recommendation",
		})
	}
	if result != nil {
		enriched := enrichNativeNamespaceDetail(c.Request().Context(), OrgID, result)
		setRecommendationNoStore(c)
		return c.JSON(http.StatusOK, enriched)
	}

	log.Infof("native namespace detail miss for %s, falling back to Kruize path", idStr)
	return GetNamespaceRecommendationSet(c)
}

// enrichNativeDetail fetches boxplots and monitoring_end_time for a native
// recommendation and wraps it in the Kruize-compatible DetailResponse shape.
func enrichNativeDetail(ctx context.Context, orgID string, result *model.NativeContainerResult) *model.DetailResponse {
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
		termNames := make([]string, 0, len(result.Recommendations))
		for termKey := range result.Recommendations {
			termNames = append(termNames, termKey)
		}
		batchPlots, err := model.AssembleAllTermBoxplots(ctx, pool, key, termNames, orgID)
		if err != nil {
			log.Warnf("boxplot assembly failed for container %s/%s: %v", key.Namespace, key.ContainerName, err)
		} else {
			plots = batchPlots
		}
		met, _ = model.MonitoringEndTime(ctx, pool, key)
	}

	singleSlice := []model.NativeContainerResult{*result}
	EnrichNativeContainerResults(ctx, &NativeContainerEnrichmentInput{OrgID: orgID, Results: singleSlice})
	*result = singleSlice[0]

	return model.BuildDetailResponse(result, plots, met)
}

// enrichNativeNamespaceDetail fetches boxplots and monitoring_end_time for a
// native namespace recommendation and returns it in the Kruize-compatible
// NamespaceDetailResponse shape.
func enrichNativeNamespaceDetail(ctx context.Context, orgID string, result *model.NativeNamespaceResult) *model.NamespaceDetailResponse {
	pool := db.GetPool()

	plots := map[string]*model.NativePlot{}
	var met time.Time

	if pool != nil {
		key := model.NamespaceKey{
			OrgID:       orgID,
			ClusterUUID: result.ClusterUUID,
			Namespace:   result.Project,
		}

		termNames := make([]string, 0, len(result.Recommendations))
		for termKey := range result.Recommendations {
			if termKey == "monitoring_end_time" {
				continue
			}
			termNames = append(termNames, termKey)
		}
		batchPlots, err := model.AssembleAllTermNamespaceBoxplots(ctx, pool, key, termNames, orgID)
		if err != nil {
			log.Warnf("namespace boxplot assembly failed for %s/%s: %v", key.ClusterUUID, key.Namespace, err)
		} else {
			plots = batchPlots
		}

		met, _ = model.NamespaceMonitoringEndTime(ctx, pool, key)
	}

	singleSlice := []model.NativeNamespaceResult{*result}
	EnrichNativeNamespaceResults(ctx, orgID, singleSlice)
	*result = singleSlice[0]

	return model.BuildNamespaceDetailResponse(result, plots, met)
}

func GetAppStatus(c echo.Context) error {
	status := map[string]string{
		"api-server": "working",
	}
	return c.JSON(http.StatusOK, status)
}

// GetReadyz reports readiness by pinging the primary DB pool.
func GetReadyz(c echo.Context) error {
	ctx, cancel := context.WithTimeout(c.Request().Context(), 2*time.Second)
	defer cancel()
	var pool db.ReadyzDB
	if db.ReadyzPoolProvider != nil {
		pool = db.ReadyzPoolProvider()
	} else {
		pool = db.GetPool()
	}
	if pool == nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status": "error",
			"checks": echo.Map{"database": "pool_uninitialized"},
		})
	}
	if err := pool.Ping(ctx); err != nil {
		log.Warnf("readyz: database ping failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status": "error",
			"checks": echo.Map{"database": "unavailable"},
		})
	}
	return c.JSON(http.StatusOK, echo.Map{
		"status": "ok",
		"checks": echo.Map{"database": "ok"},
	})
}
