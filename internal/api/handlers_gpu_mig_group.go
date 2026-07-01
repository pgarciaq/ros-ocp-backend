package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
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

func getGPUMIGRecsGrouped(
	c echo.Context,
	ctx context.Context,
	pool *pgxpool.Pool,
	hlog *logrus.Entry,
	orgID string,
	clusterUUIDs []string,
	start, end time.Time,
	groupByCluster bool,
	limit, offset int,
	clusterFilter string,
) error {
	groupCol := "g.namespace"
	if groupByCluster {
		groupCol = "g.cluster_uuid::text"
	}

	baseFilter := ` AND g.interval_start >= $2::date AND g.interval_start <= $3::date
		AND g.cluster_uuid::text = ANY($4::text[])`
	baseArgs := []any{orgID, start.Format("2006-01-02"), end.Format("2006-01-02"), clusterUUIDs}
	argIdx := 5

	if clusterFilter != "" {
		baseFilter += " AND g.cluster_uuid = $" + strconv.Itoa(argIdx) + "::uuid"
		baseArgs = append(baseArgs, clusterFilter)
		argIdx++
	}

	countQuery := `SELECT COUNT(DISTINCT ` + groupCol + `)
		FROM gpu_container_digests g
		WHERE g.org_id = $1` + baseFilter
	var total int
	if err := pool.QueryRow(ctx, countQuery, baseArgs...).Scan(&total); err != nil {
		hlog.Errorf("GPU MIG group count failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to count GPU MIG recommendation groups",
		})
	}

	innerQuery := `
		SELECT ` + groupCol + ` AS group_key,
			COUNT(DISTINCT (g.cluster_uuid, g.namespace, g.workload, g.container_name, g.gpu_model_name)) AS row_count
		FROM gpu_container_digests g
		WHERE g.org_id = $1` + baseFilter + `
		GROUP BY ` + groupCol

	query := `SELECT group_key, row_count
		FROM (` + innerQuery + `) mig_groups
		ORDER BY group_key ASC`

	pageLimit := limit + 1
	query += ` LIMIT $` + strconv.Itoa(argIdx)
	pageArgs := append(append([]any{}, baseArgs...), pageLimit)
	argIdx++

	query += ` OFFSET $` + strconv.Itoa(argIdx)
	pageArgs = append(pageArgs, offset)

	rows, err := pool.Query(ctx, query, pageArgs...)
	if err != nil {
		hlog.Errorf("GPU MIG group query failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch GPU MIG recommendation groups",
		})
	}
	defer rows.Close()

	var data []gpuMIGGroupedRow
	for rows.Next() {
		var groupKey string
		var count int
		if err := rows.Scan(&groupKey, &count); err != nil {
			return c.JSON(http.StatusServiceUnavailable, echo.Map{
				"status":  "error",
				"message": "unable to read GPU MIG recommendation groups",
			})
		}
		item := gpuMIGGroupedRow{Count: count}
		if groupByCluster {
			item.ClusterUUID = groupKey
		} else {
			item.Namespace = groupKey
		}
		data = append(data, item)
	}

	hasNext := false
	if limit > 0 && len(data) > limit {
		hasNext = true
		data = data[:limit]
	}

	if data == nil {
		data = []gpuMIGGroupedRow{}
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
