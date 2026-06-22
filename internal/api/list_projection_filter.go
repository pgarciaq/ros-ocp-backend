package api

import (
	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

func filterContainerListByProjection(listData []*model.ListResponse) []*model.ListResponse {
	filtered := make([]*model.ListResponse, 0, len(listData))
	for _, item := range listData {
		if item == nil {
			continue
		}
		if model.RecommendationTermsHasData(item.Recommendations.RecommendationTerms) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func filterNamespaceListByProjection(listData []*model.NamespaceListResponse) []*model.NamespaceListResponse {
	filtered := make([]*model.NamespaceListResponse, 0, len(listData))
	for _, item := range listData {
		if item == nil {
			continue
		}
		if model.RecommendationTermsHasData(item.Recommendations.RecommendationTerms) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func filterNodeListByProjection(recs []model.NodeUtilizationRec) []model.NodeUtilizationRec {
	filtered := make([]model.NodeUtilizationRec, 0, len(recs))
	for _, rec := range recs {
		if model.NodeUtilRecommendationTermsHasData(rec.RecommendationTerms) {
			filtered = append(filtered, rec)
		}
	}
	return filtered
}

func shouldFilterListByProjection(c echo.Context) bool {
	return queryparams.HasExplicitTermAndEngineFilters(c)
}
