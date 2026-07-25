package api

import (
	"context"
	"io"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"
	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
)

func getPVCRecommendationsGrouped(
	c echo.Context,
	ctx context.Context,
	pool *pgxpool.Pool,
	hlog *logrus.Entry,
	orgID, filterSQL string,
	args []any,
	argIdx, limit, offset int,
	groupByCluster bool,
	clusterFilter, termFilter string,
	responseFormat string,
	cursor PVCCursor,
	hasCursor bool,
) error {
	groupCol := "namespace"
	if groupByCluster {
		groupCol = "cluster_uuid::text"
	}

	countQuery := `SELECT COUNT(DISTINCT ` + groupCol + `) FROM pvc_recommendation_sets WHERE org_id = $1` + filterSQL
	var total int
	if err := pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		hlog.Errorf("PVC recommendation group count failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to count PVC recommendation groups",
		})
	}

	innerQuery := `
		SELECT ` + groupCol + ` AS group_key,
			COUNT(*) AS row_count,
			COALESCE(SUM(estimated_savings_cents), 0) AS savings_cents,
			COALESCE(SUM(capacity_bytes), 0) AS capacity_bytes
		FROM pvc_recommendation_sets
		WHERE org_id = $1` + filterSQL + `
		GROUP BY ` + groupCol

	query := `SELECT group_key, row_count, savings_cents, capacity_bytes
		FROM (` + innerQuery + `) pvc_groups`

	if hasCursor && cursor.GroupKey != "" {
		query += ` WHERE group_key > $` + strconv.Itoa(argIdx)
		args = append(args, cursor.GroupKey)
		argIdx++
	}

	query += ` ORDER BY group_key ASC`

	pageLimit := limit
	if pageLimit > 0 {
		pageLimit++
	}
	query += ` LIMIT $` + strconv.Itoa(argIdx)
	pageArgs := append(args, pageLimit)
	argIdx++

	if !hasCursor {
		query += ` OFFSET $` + strconv.Itoa(argIdx)
		pageArgs = append(pageArgs, offset)
	}

	rows, err := pool.Query(ctx, query, pageArgs...)
	if err != nil {
		hlog.Errorf("PVC recommendation group query failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch PVC recommendation groups",
		})
	}
	defer rows.Close()

	currency := fetchClusterCurrency(ctx, orgID, clusterFilter)

	var data []PVCRecommendationResponse
	for rows.Next() {
		var groupKey string
		var count int
		var savingsCents, capacityBytes int64
		if err := rows.Scan(&groupKey, &count, &savingsCents, &capacityBytes); err != nil {
			return c.JSON(http.StatusServiceUnavailable, echo.Map{
				"status":  "error",
				"message": "unable to read PVC recommendation groups",
			})
		}
		item := PVCRecommendationResponse{
			Count:         count,
			CapacityBytes: capacityBytes,
		}
		if groupByCluster {
			item.ClusterUUID = groupKey
		} else {
			item.Namespace = groupKey
		}
		if savingsCents > 0 {
			item.EstimatedMonthlySavings = money.FormatCentsToAmountPtr(&savingsCents, currency)
		}
		data = append(data, item)
	}

	hasNext := false
	var nextCursor string
	if limit > 0 && len(data) > limit {
		hasNext = true
		last := data[limit-1]
		if groupByCluster {
			nextCursor = pvcGroupNextCursor(last.ClusterUUID)
		} else {
			nextCursor = pvcGroupNextCursor(last.Namespace)
		}
		data = data[:limit]
	}

	pvcTerms, pvcTermErr := engine.LoadTermConfigCached(ctx, pool, orgID, "pvc")
	if pvcTermErr != nil {
		pvcTerms = engine.DefaultTermsForPlugin("pvc")
	}

	resp := PVCRecommendationListResponse{}
	resp.Meta.Count = total
	resp.Meta.Limit = limit
	resp.Meta.Offset = offset
	resp.Meta.HasNext = hasNext
	resp.Meta.NextCursor = nextCursor
	resp.Meta.Currency = currency
	resp.Meta.MinDataDays = engine.MinDataDaysForTerm(pvcTerms, termFilter)
	resp.Links = buildLinks(c.Request(), total, limit, offset)
	applyKeysetNextLink(&resp.Links, c.Request(), limit, hasNext, nextCursor)
	resp.Data = data
	if resp.Data == nil {
		resp.Data = []PVCRecommendationResponse{}
	}

	if responseFormat == listoptions.ResponseFormatCSV {
		return streamCSV(c, csvFilename("pvc-recommendations"), func(ctx context.Context, w io.Writer) error {
			return generatePVCRecCSV(ctx, w, resp.Data)
		})
	}
	return c.JSON(http.StatusOK, resp)
}
