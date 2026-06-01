package api

import (
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

type pvcListFilters struct {
	termFilter       string
	clusterFilter    string
	namespaceFilter  string
	typeFilter       string
	storageclassFilter string
}

func parsePVCListFilters(c echo.Context) pvcListFilters {
	termFilter := queryparams.FirstFilter(c, "term")
	if termFilter == "" {
		termFilter = "medium"
	}
	return pvcListFilters{
		termFilter:         termFilter,
		clusterFilter:      queryparams.FirstFilter(c, "cluster"),
		namespaceFilter:    queryparams.FirstFilter(c, "project"),
		typeFilter:         queryparams.FirstFilter(c, "recommendation_type"),
		storageclassFilter: queryparams.FirstFilter(c, "storageclass"),
	}
}

// buildPVCRecommendationFilterSQL returns WHERE clause suffix (starts with " AND term = $2")
// and args beginning with orgID and termFilter.
func buildPVCRecommendationFilterSQL(c echo.Context, orgID string, f pvcListFilters) (filterSQL string, args []interface{}, argIdx int, tagErr error) {
	filterSQL = " AND term = $2"
	args = []interface{}{orgID, f.termFilter}
	argIdx = 3

	if f.clusterFilter != "" {
		filterSQL += ` AND cluster_uuid = $` + strconv.Itoa(argIdx)
		args = append(args, f.clusterFilter)
		argIdx++
	}
	if f.namespaceFilter != "" {
		filterSQL += ` AND namespace = $` + strconv.Itoa(argIdx)
		args = append(args, f.namespaceFilter)
		argIdx++
	}
	if f.typeFilter != "" {
		filterSQL += ` AND recommendation_type = $` + strconv.Itoa(argIdx)
		args = append(args, f.typeFilter)
		argIdx++
	}
	if f.storageclassFilter != "" {
		filterSQL += ` AND storageclass = $` + strconv.Itoa(argIdx)
		args = append(args, f.storageclassFilter)
		argIdx++
	}

	if config.TagsFeatureEnabled() {
		tagFilters, err := parseTagFiltersFromRequest(c)
		if err != nil {
			return "", nil, 0, err
		}
		if len(tagFilters) > 0 {
			tagClause, tagArgs, nextIdx := model.TagFilterExistsClause(
				orgID, "pvc_recommendation_sets.cluster_uuid", "pvc_recommendation_sets.namespace", tagFilters, argIdx)
			if tagClause != "" {
				filterSQL += " AND " + tagClause
				args = append(args, tagArgs...)
				argIdx = nextIdx
			}
		}
	}
	return filterSQL, args, argIdx, nil
}
