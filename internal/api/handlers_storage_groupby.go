package api

import (
	"fmt"

	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
)

// parseStorageListGroupBy resolves group_by[cluster] vs group_by[project] for PVC and
// snapshot list endpoints. group_by[namespace] is accepted as an alias for project.
func parseStorageListGroupBy(c echo.Context) (groupByCluster, groupByProject bool, err error) {
	groupByCluster = queryparams.GroupByField(c, "cluster")
	groupByProject = queryparams.GroupByField(c, "project") || queryparams.GroupByField(c, "namespace")
	if groupByCluster && groupByProject {
		return false, false, fmt.Errorf("group_by[cluster] and group_by[project] cannot be used together")
	}
	return groupByCluster, groupByProject, nil
}
