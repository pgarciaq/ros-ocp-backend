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
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
)

func getSnapshotRecommendationsGrouped(
	c echo.Context,
	ctx context.Context,
	pool *pgxpool.Pool,
	hlog *logrus.Entry,
	orgID, filterSQL string,
	args []any,
	argIdx, limit, offset int,
	groupByCluster bool,
	clusterFilter string,
	responseFormat string,
	cursor SnapshotCursor,
	hasCursor bool,
) error {
	groupCol := "namespace"
	if groupByCluster {
		groupCol = "cluster_uuid::text"
	}

	countQuery := `SELECT COUNT(DISTINCT ` + groupCol + `) FROM snapshot_recommendation_sets WHERE org_id = $1` + filterSQL
	var total int
	if err := pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		hlog.Errorf("snapshot recommendation group count failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to count snapshot recommendation groups",
		})
	}

	innerQuery := `
		SELECT ` + groupCol + ` AS group_key,
			COUNT(*) AS row_count,
			COALESCE(SUM(estimated_cost_cents), 0) AS cost_cents,
			COALESCE(SUM(restore_size_bytes), 0) AS restore_size_bytes
		FROM snapshot_recommendation_sets
		WHERE org_id = $1` + filterSQL + `
		GROUP BY ` + groupCol

	query := `SELECT group_key, row_count, cost_cents, restore_size_bytes
		FROM (` + innerQuery + `) snapshot_groups`

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
		hlog.Errorf("snapshot recommendation group query failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch snapshot recommendation groups",
		})
	}
	defer rows.Close()

	storedCurrency := fetchClusterCurrency(ctx, orgID, clusterFilter)
	userCurrency := resolveUserCurrency(ctx, orgID)
	rate := fetchExchangeRate(ctx, orgID, storedCurrency, userCurrency)
	displayCurrency := userCurrency
	if rate == 1.0 && storedCurrency != userCurrency {
		displayCurrency = storedCurrency
	}

	var data []SnapshotRecommendationResponse
	for rows.Next() {
		var groupKey string
		var count int
		var costCents, restoreSizeBytes int64
		if err := rows.Scan(&groupKey, &count, &costCents, &restoreSizeBytes); err != nil {
			return c.JSON(http.StatusServiceUnavailable, echo.Map{
				"status":  "error",
				"message": "unable to read snapshot recommendation groups",
			})
		}
		item := SnapshotRecommendationResponse{
			Count:            count,
			RestoreSizeBytes: restoreSizeBytes,
		}
		if groupByCluster {
			item.ClusterUUID = groupKey
		} else {
			item.Namespace = groupKey
		}
		if costCents > 0 {
			item.EstimatedMonthlyCost = money.FormatCentsToAmountPtr(&costCents, storedCurrency)
		}
		data = append(data, item)
	}

	for i := range data {
		convertAndPatchAmount(data[i].EstimatedMonthlyCost, rate, displayCurrency)
	}

	hasNext := false
	var nextCursor string
	if limit > 0 && len(data) > limit {
		hasNext = true
		last := data[limit-1]
		if groupByCluster {
			nextCursor = snapshotGroupNextCursor(last.ClusterUUID)
		} else {
			nextCursor = snapshotGroupNextCursor(last.Namespace)
		}
		data = data[:limit]
	}

	resp := SnapshotRecommendationListResponse{}
	resp.Meta.Count = total
	resp.Meta.Limit = limit
	resp.Meta.Offset = offset
	resp.Meta.HasNext = hasNext
	resp.Meta.NextCursor = nextCursor
	resp.Meta.Currency = displayCurrency
	resp.Links = buildLinks(c.Request(), total, limit, offset)
	applyKeysetNextLink(&resp.Links, c.Request(), limit, hasNext, nextCursor)
	resp.Data = data
	if resp.Data == nil {
		resp.Data = []SnapshotRecommendationResponse{}
	}

	if responseFormat == listoptions.ResponseFormatCSV {
		return streamCSV(c, csvFilename("snapshot-recommendations"), func(ctx context.Context, w io.Writer) error {
			return generateSnapshotRecCSV(ctx, w, resp.Data)
		})
	}
	return c.JSON(http.StatusOK, resp)
}
