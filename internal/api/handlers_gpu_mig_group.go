package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

type gpuMIGGroupedRow struct {
	ClusterUUID string `json:"cluster_uuid,omitempty"`
	Namespace   string `json:"namespace,omitempty"`
	Count       int    `json:"count"`
}

type gpuMIGGroupedResponse struct {
	Meta  model.GPUMIGListMeta `json:"meta"`
	Links Links                `json:"links"`
	Data  []gpuMIGGroupedRow   `json:"data"`
}

func parseGPUMIGListGroupBy(c echo.Context) (groupByCluster, groupByProject bool, err error) {
	groupByCluster = queryparams.GroupByField(c, "cluster")
	groupByProject = queryparams.GroupByField(c, "project") || queryparams.GroupByField(c, "namespace")
	if groupByCluster && groupByProject {
		return false, false, fmt.Errorf("group_by[cluster] and group_by[project] cannot be used together")
	}
	return groupByCluster, groupByProject, nil
}

func getGPUMIGRecsGroupedSQL(
	c echo.Context,
	ctx context.Context,
	pool *pgxpool.Pool,
	hlog *logrus.Entry,
	orgID string,
	filters engine.GPUMIGListFilters,
	groupByCluster bool,
	limit, offset int,
) error {
	total, countErr := engine.CountGPUMIGGrouped(ctx, pool, orgID, filters, groupByCluster)
	if countErr != nil {
		hlog.Errorf("GPU MIG group count failed: %v", countErr)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to count GPU MIG recommendation groups",
		})
	}

	rows, listErr := engine.ListGPUMIGGrouped(ctx, pool, orgID, filters, groupByCluster, limit, offset)
	if listErr != nil {
		hlog.Errorf("GPU MIG group list failed: %v", listErr)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch GPU MIG recommendation groups",
		})
	}

	hasNext := false
	if limit > 0 && len(rows) > limit {
		hasNext = true
		rows = rows[:limit]
	}

	data := make([]gpuMIGGroupedRow, 0, len(rows))
	for _, r := range rows {
		item := gpuMIGGroupedRow{Count: r.Count}
		if groupByCluster {
			item.ClusterUUID = r.GroupKey
		} else {
			item.Namespace = r.GroupKey
		}
		data = append(data, item)
	}

	setRecommendationNoStore(c)
	return c.JSON(http.StatusOK, gpuMIGGroupedResponse{
		Meta: model.GPUMIGListMeta{
			Count:    total,
			Limit:    limit,
			Offset:   offset,
			HasNext:  hasNext,
			Currency: resolveListCurrencyFromRequest(c, orgID),
		},
		Links: buildLinks(c.Request(), total, limit, offset),
		Data:  data,
	})
}
