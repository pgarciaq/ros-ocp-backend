package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
)

type gpuTSGroupedRow struct {
	ClusterUUID             string             `json:"cluster_uuid"`
	Count                   int                `json:"count"`
	EstimatedMonthlySavings *money.MoneyAmount `json:"estimated_monthly_savings,omitempty"`
}

type gpuTSGroupedResponse struct {
	Meta  model.NodeRecommendationMeta  `json:"meta"`
	Links model.NodeRecommendationLinks `json:"links"`
	Data  []gpuTSGroupedRow             `json:"data"`
}

func getGPUTSRecsGrouped(
	c echo.Context,
	ctx context.Context,
	pool *pgxpool.Pool,
	hlog *logrus.Entry,
	orgID string,
	userPerms map[string][]string,
	opts listoptions.ListOptions,
	clusterUUIDs []string,
	clusterFilter, nodeNameFilter, gpuModelFilter, termFilter string,
) error {
	filterSQL, args, argIdx, _, tagFilterErr := buildNodeGPUTimeslicingFilterSQL(
		c, orgID, clusterUUIDs, userPerms,
		clusterFilter, nodeNameFilter, gpuModelFilter, termFilter,
	)
	if tagFilterErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": tagFilterErr.Error()})
	}

	groupCol := "t.cluster_uuid::text"

	countQuery := `SELECT COUNT(DISTINCT ` + groupCol + `) FROM node_gpu_timeslicing_recommendations t WHERE t.org_id = $1` + filterSQL
	var total int
	if err := pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		hlog.Errorf("GPU TS group count failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to count GPU time-slicing recommendation groups",
		})
	}

	innerQuery := `
		SELECT ` + groupCol + ` AS group_key,
			COUNT(*) AS row_count,
			COALESCE(SUM(t.estimated_savings_cents), 0) AS savings_cents
		FROM node_gpu_timeslicing_recommendations t
		WHERE t.org_id = $1` + filterSQL + `
		GROUP BY ` + groupCol

	query := `SELECT group_key, row_count, savings_cents
		FROM (` + innerQuery + `) gpu_ts_groups
		ORDER BY group_key ASC`

	limit := opts.Limit
	if limit <= 0 {
		limit = listoptions.DefaultLimit
	}

	pageLimit := limit + 1
	query += ` LIMIT $` + strconv.Itoa(argIdx)
	pageArgs := append(append([]any{}, args...), pageLimit)
	argIdx++

	query += ` OFFSET $` + strconv.Itoa(argIdx)
	pageArgs = append(pageArgs, opts.Offset)

	rows, err := pool.Query(ctx, query, pageArgs...)
	if err != nil {
		hlog.Errorf("GPU TS group query failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch GPU time-slicing recommendation groups",
		})
	}
	defer rows.Close()

	currency := costdata.DefaultCurrency
	if clusterFilter != "" {
		if cur := fetchClusterCurrency(ctx, orgID, clusterFilter); cur != "" {
			currency = cur
		}
	}

	var data []gpuTSGroupedRow
	for rows.Next() {
		var groupKey string
		var count int
		var savingsCents int64
		if err := rows.Scan(&groupKey, &count, &savingsCents); err != nil {
			return c.JSON(http.StatusServiceUnavailable, echo.Map{
				"status":  "error",
				"message": "unable to read GPU time-slicing recommendation groups",
			})
		}
		item := gpuTSGroupedRow{
			ClusterUUID: groupKey,
			Count:       count,
		}
		if savingsCents != 0 {
			item.EstimatedMonthlySavings = money.FormatCentsToAmountPtr(&savingsCents, currency)
		}
		data = append(data, item)
	}

	hasNext := false
	if limit > 0 && len(data) > limit {
		hasNext = true
		data = data[:limit]
	}

	if data == nil {
		data = []gpuTSGroupedRow{}
	}

	gpuTerms, gpuTermErr := engine.LoadTermConfigCached(ctx, pool, orgID, "gpu")
	if gpuTermErr != nil {
		gpuTerms = engine.DefaultTermsForPlugin("gpu")
	}

	setRecommendationNoStore(c)
	return c.JSON(http.StatusOK, gpuTSGroupedResponse{
		Meta: model.NodeRecommendationMeta{
			Count:       total,
			Limit:       limit,
			Offset:      opts.Offset,
			HasNext:     hasNext,
			Currency:    currency,
			MinDataDays: engine.MinDataDaysForTerm(gpuTerms, termFilter),
		},
		Links: buildNodeLinks(c.Request(), total, limit, opts.Offset),
		Data:  data,
	})
}
