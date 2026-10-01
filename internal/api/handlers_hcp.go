package api

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
)

// MapHCPQueryParameters parses list filters for the dedicated HCP surface.
// It mirrors MapQueryParameters (same dates, same cluster/project/workload
// filters) plus the hosted_cluster_id filter. The HCP namespace scoping
// itself is forced server-side, never caller-selectable.
func MapHCPQueryParameters(c echo.Context) (map[string]interface{}, error) {
	queryParams, err := MapQueryParameters(c)
	if err != nil {
		return queryParams, err
	}
	if err := applyParamFilter(c, queryParams, "hosted_cluster_id", "recommendation_sets.hosted_cluster_id", model.ClusterMaxLen, true, SkipSanitizationForContainer); err != nil {
		return queryParams, err
	}
	return queryParams, nil
}

// GetHCPRecommendationSetList handles GET /recommendations/openshift/hcp:
// HCP-namespaced container rows (associated + incomplete:true unassociated).
// RBAC scoping in the model runs before the hosted filter by construction.
func GetHCPRecommendationSetList(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	OrgID := xrhid.Identity.OrgID
	user_permissions := get_user_permissions(c)
	handlerName := "hcprecommendationset-list"
	hlog := requestLogger(c, OrgID)

	apiListOptions, err := listoptions.ListAPIOptions(c, listoptions.DefaultContainerRecsDBColumn, listoptions.ContainerAllowedOrderBy)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"status":  "error",
			"message": err.Error(),
		})
	}

	queryParams, err := MapHCPQueryParameters(c)
	if err != nil {
		return apiErrResponse(c, err, http.StatusBadRequest, err.Error())
	}

	if queryparams.GroupByField(c, "hosted_cluster_id") {
		return getHCPGroupedRecommendations(c, OrgID, user_permissions, apiListOptions, queryParams)
	}

	unitChoices, setk8sUnits, unitParseErr := ParseUnitParams(c, "cores", "bytes")
	if unitParseErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": unitParseErr.Error()})
	}

	recommendationSets, count, queryErr := model.GetHCPRecommendationSets(OrgID, apiListOptions, queryParams, user_permissions)
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
		results := CollectionResponse(recommendationSets, c.Request(), count, apiListOptions.Limit, apiListOptions.Offset)
		setRecommendationNoStore(c)
		return c.JSON(http.StatusOK, results)
	case listoptions.ResponseFormatCSV:
		filename := "hcp-recommendations-" + time.Now().Format("20060102")
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
					_ = pipeWriter.Close()
				}
			}()
			generationErr = GenerateAndStreamCSV(reqCtx, pipeWriter, recommendationSets)
		}()
		return c.Stream(http.StatusOK, "text/csv", pipeReader)
	}
	return nil
}

// getHCPGroupedRecommendations serves group_by[hosted_cluster_id]: per-hosted
// counts plus summed savings rendered in display currency (fleet-summary
// pattern: stored cents converted once, stored-currency fallback).
func getHCPGroupedRecommendations(c echo.Context, orgID string, userPermissions map[string][]string, apiListOptions listoptions.ListOptions, queryParams map[string]interface{}) error {
	hlog := requestLogger(c, orgID)
	groups, count, queryErr := model.GetHCPGroupedRecommendations(orgID, apiListOptions, queryParams, userPermissions)
	if queryErr != nil {
		hlog.Errorf("unable to fetch records from database: %v", queryErr)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch records from database",
		})
	}
	ctx := c.Request().Context()
	displayCurrency, _, rate := resolveDisplayCurrency(ctx, orgID, "")
	for i := range groups {
		converted := int64(math.Round(float64(groups[i].SavingsCents) * rate))
		groups[i].EstimatedSavings = money.FormatCentsToAmountPtr(&converted, displayCurrency)
	}
	results := CollectionResponse(groups, c.Request(), count, apiListOptions.Limit, apiListOptions.Offset)
	results.Meta.Currency = displayCurrency
	setRecommendationNoStore(c)
	return c.JSON(http.StatusOK, results)
}

// GetHCPRecommendationSet handles GET /recommendations/openshift/hcp/:recommendation-id.
// IDs are the unchanged management identities; off-scope (non-HCP) IDs 404.
func GetHCPRecommendationSet(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	OrgID := xrhid.Identity.OrgID
	user_permissions := get_user_permissions(c)
	handlerName := "hcprecommendationset"
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
	recommendationSet, fetchErr := recommendationSetVar.GetRecommendationSetByID(OrgID, RecommendationUUID.String(), user_permissions)
	if fetchErr != nil {
		hlog.WithField("recommendation_id", RecommendationIDStr).Errorf("unable to fetch recommendation: %v", fetchErr)
		return c.JSON(http.StatusNotFound, echo.Map{"status": "not_found", "message": "unable to fetch recommendation"})
	}
	scoped, scopeErr := model.HCPRowScoped(OrgID, recommendationSet.ClusterUUID, recommendationSet.Project)
	if scopeErr != nil {
		hlog.WithField("recommendation_id", RecommendationIDStr).Warnf("hcp scope check failed, serving without scope gate: %v", scopeErr)
	} else if !scoped {
		return c.JSON(http.StatusNotFound, echo.Map{"status": "not_found", "message": "unable to fetch recommendation"})
	}
	if recommendationSet.HostedClusterID == "" {
		recommendationSet.Incomplete = true
	}

	if siblings, sibErr := recommendationSetVar.GetRecommendationSiblingRows(OrgID, RecommendationUUID.String(), user_permissions); sibErr != nil {
		hlog.WithField("recommendation_id", RecommendationIDStr).Warnf("sibling fetch failed, serving without synthesis: %v", sibErr)
	} else {
		populateDetailRecommendations(&recommendationSet, siblings)
	}
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
}
