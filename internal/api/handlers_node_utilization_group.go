package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"

	"github.com/redhatinsights/ros-ocp-backend/internal/money"
)

type nodeUtilGroupedRow struct {
	ClusterUUID             string             `json:"cluster_uuid"`
	Count                   int                `json:"count"`
	EstimatedMonthlySavings *money.MoneyAmount `json:"estimated_monthly_savings,omitempty"`
}

type nodeUtilGroupedResponse struct {
	Meta  nodeUtilGroupedMeta   `json:"meta"`
	Links Links                 `json:"links"`
	Data  []nodeUtilGroupedRow  `json:"data"`
}

type nodeUtilGroupedMeta struct {
	Count             int    `json:"count"`
	Limit             int    `json:"limit"`
	Offset            int    `json:"offset"`
	HasNext           bool   `json:"has_next"`
	Currency          string `json:"currency"`
	DataDaysAvailable int    `json:"data_days_available"`
	MinDataDays       int    `json:"min_data_days"`
}

func getNodeUtilizationRecsGrouped(
	c echo.Context,
	ctx context.Context,
	pool *pgxpool.Pool,
	hlog *logrus.Entry,
	orgID, baseFrom string,
	args []any,
	argIdx, limit, offset int,
	clusterFilter string,
	dataDaysAvailable int,
	minDataDays int,
) error {
	groupCol := "nr.cluster_uuid::text"

	countQuery := `SELECT COUNT(DISTINCT ` + groupCol + `)` + baseFrom
	var total int
	if err := pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		hlog.Errorf("node utilization group count failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to count node recommendation groups",
		})
	}

	innerQuery := `
		SELECT ` + groupCol + ` AS group_key,
			COUNT(DISTINCT nr.node) AS row_count,
			COALESCE(SUM(nr.estimated_savings_cents), 0) AS savings_cents` + baseFrom + `
		GROUP BY ` + groupCol

	query := `SELECT group_key, row_count, savings_cents
		FROM (` + innerQuery + `) node_groups
		ORDER BY group_key ASC`

	pageLimit := limit
	if pageLimit > 0 {
		pageLimit++
	}
	query += ` LIMIT $` + strconv.Itoa(argIdx)
	pageArgs := append(append([]any{}, args...), pageLimit)
	argIdx++

	query += ` OFFSET $` + strconv.Itoa(argIdx)
	pageArgs = append(pageArgs, offset)

	rows, err := pool.Query(ctx, query, pageArgs...)
	if err != nil {
		hlog.Errorf("node utilization group query failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch node recommendation groups",
		})
	}
	defer rows.Close()

	currency := fetchClusterCurrency(ctx, orgID, clusterFilter)

	var data []nodeUtilGroupedRow
	for rows.Next() {
		var groupKey string
		var count int
		var savingsCents int64
		if err := rows.Scan(&groupKey, &count, &savingsCents); err != nil {
			return c.JSON(http.StatusServiceUnavailable, echo.Map{
				"status":  "error",
				"message": "unable to read node recommendation groups",
			})
		}
		item := nodeUtilGroupedRow{
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
		data = []nodeUtilGroupedRow{}
	}

	setRecommendationNoStore(c)
	return c.JSON(http.StatusOK, nodeUtilGroupedResponse{
		Meta: nodeUtilGroupedMeta{
			Count:             total,
			Limit:             limit,
			Offset:            offset,
			HasNext:           hasNext,
			Currency:          currency,
			DataDaysAvailable: dataDaysAvailable,
			MinDataDays:       minDataDays,
		},
		Links: buildLinks(c.Request(), total, limit, offset),
		Data:  data,
	})
}
