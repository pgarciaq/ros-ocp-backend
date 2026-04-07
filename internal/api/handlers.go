package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/platform-go-middlewares/identity"
	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
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
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
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
			recommendationSet.Recommendations)
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
		return c.JSON(http.StatusBadRequest, echo.Map{
			"status":  "error",
			"message": listOptionsErr.Error(),
		})
	}

	queryParams, paramErr := MapNamespaceQueryParameters(c)
	if paramErr != nil {
		log.Error(paramErr.Error())
		var pe *ParamError
		if errors.As(paramErr, &pe) && pe.UserErr {
			return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": paramErr.Error()})
		}
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "unable to parse query parameters"})
	}

	unitChoices, setk8sUnits, err := ParseUnitParams(c, "cores", "bytes")
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
	}

	NamespaceRecommendationSet := model.NamespaceRecommendationSet{}
	namespaceRecommendationSets, count, queryErr := NamespaceRecommendationSet.GetNamespaceRecommendationSets(
		OrgID, apiListOptions, queryParams, user_permissions,
	)

	if queryErr != nil {
		log.Errorf("unable to fetch records from database; %v", queryErr)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch records from database",
		})
	}

	for i := range namespaceRecommendationSets {
		namespaceRecommendationSets[i].RecommendationsJSON = UpdateRecommendationJSON(
			handlerName,
			namespaceRecommendationSets[i].ID,
			namespaceRecommendationSets[i].ClusterUUID,
			unitChoices,
			setk8sUnits,
			namespaceRecommendationSets[i].Recommendations,
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
		return c.JSON(http.StatusNotAcceptable, map[string]string{
			"message": "CSV format is not supported. Please use application/json.",
		})
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
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "bad recommendation-id for project"})
	}

	unitChoices, setk8sUnits, unitParseErr := ParseUnitParams(c, "cores", "MiB")
	if unitParseErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": unitParseErr.Error()})
	}

	recommendationSetVar := model.NamespaceRecommendationSet{}
	nsRecommendationSet, getNSRecordErr := recommendationSetVar.GetNamespaceRecommendationSetByID(
		OrgID,
		RecommendationUUID.String(),
		user_permissions,
	)

	if getNSRecordErr != nil {
		log.Errorf("unable to fetch project recommendation %s; error %v", RecommendationIDStr, getNSRecordErr.Error())
		return c.JSON(http.StatusNotFound, echo.Map{"status": "error", "message": "unable to fetch project recommendation"})
	}

	if len(nsRecommendationSet.Recommendations) != 0 {
		nsRecommendationSet.RecommendationsJSON = UpdateRecommendationJSON(
			handlerName,
			nsRecommendationSet.ID,
			nsRecommendationSet.ClusterUUID,
			unitChoices,
			setk8sUnits,
			nsRecommendationSet.Recommendations)
		return c.JSON(http.StatusOK, nsRecommendationSet)
	} else {
		return c.JSON(http.StatusNotFound, echo.Map{"status": "not_found", "message": "project recommendation not found"})
	}
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
		for i, v := range results {
			interfaceSlice[i] = v
		}
		response := CollectionResponse(interfaceSlice, c.Request(), count, apiListOptions.Limit, apiListOptions.Offset)
		return c.JSON(http.StatusOK, response)
	}
}

func GetAppStatus(c echo.Context) error {
	status := map[string]string{
		"api-server": "working",
	}
	return c.JSON(http.StatusOK, status)
}
