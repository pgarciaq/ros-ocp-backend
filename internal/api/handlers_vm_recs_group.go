package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
)

type vmGroupedRow struct {
	ClusterUUID             string             `json:"cluster_uuid,omitempty"`
	Namespace               string             `json:"namespace,omitempty"`
	Count                   int                `json:"count"`
	EstimatedMonthlySavings *money.MoneyAmount `json:"estimated_monthly_savings,omitempty"`
}

type vmGroupedResponse struct {
	Meta  Metadata       `json:"meta"`
	Links Links          `json:"links"`
	Data  []vmGroupedRow `json:"data"`
}

func parseVMListGroupBy(c echo.Context) (field string, err error) {
	groupByCluster := queryparams.GroupByField(c, "cluster")
	groupByNamespace := queryparams.GroupByField(c, "project") || queryparams.GroupByField(c, "namespace")

	if groupByCluster && groupByNamespace {
		return "", fmt.Errorf("group_by[cluster] and group_by[project] cannot be used together")
	}
	if groupByCluster {
		return "cluster", nil
	}
	if groupByNamespace {
		return "namespace", nil
	}
	return "", nil
}

func getVMRecsGrouped(
	c echo.Context,
	ctx context.Context,
	pool *pgxpool.Pool,
	hlog *logrus.Entry,
	orgID string,
	allowedClusters []string,
	limit, offset int,
	groupByField string,
) error {
	var groupCol string
	switch groupByField {
	case "cluster":
		groupCol = "v.cluster_uuid::text"
	case "namespace":
		groupCol = "v.namespace"
	default:
		return c.JSON(http.StatusBadRequest, echo.Map{
			"status":  "error",
			"message": "unsupported group_by field",
		})
	}

	baseWhere := " WHERE v.org_id = $1 AND v.cluster_uuid::text = ANY($2::text[])"
	baseArgs := []any{orgID, allowedClusters}
	argIdx := 3

	countQuery := `SELECT COUNT(DISTINCT ` + groupCol + `) FROM vm_recommendations v` + baseWhere
	var total int
	if err := pool.QueryRow(ctx, countQuery, baseArgs...).Scan(&total); err != nil {
		hlog.Errorf("VM group count failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to count VM recommendation groups",
		})
	}

	innerQuery := `
		SELECT ` + groupCol + ` AS group_key,
			COUNT(*) AS row_count,
			COALESCE(SUM(v.estimated_savings_cents), 0) AS savings_cents
		FROM vm_recommendations v` + baseWhere + `
		GROUP BY ` + groupCol

	query := `SELECT group_key, row_count, savings_cents
		FROM (` + innerQuery + `) vm_groups
		ORDER BY group_key ASC`

	pageLimit := limit + 1
	query += ` LIMIT $` + strconv.Itoa(argIdx)
	pageArgs := append(append([]any{}, baseArgs...), pageLimit)
	argIdx++

	query += ` OFFSET $` + strconv.Itoa(argIdx)
	pageArgs = append(pageArgs, offset)

	rows, err := pool.Query(ctx, query, pageArgs...)
	if err != nil {
		hlog.Errorf("VM group query failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch VM recommendation groups",
		})
	}
	defer rows.Close()

	currency := resolveListCurrencyFromRequest(c, orgID)

	var data []vmGroupedRow
	for rows.Next() {
		var groupKey string
		var count int
		var savingsCents int64
		if err := rows.Scan(&groupKey, &count, &savingsCents); err != nil {
			return c.JSON(http.StatusServiceUnavailable, echo.Map{
				"status":  "error",
				"message": "unable to read VM recommendation groups",
			})
		}
		item := vmGroupedRow{Count: count}
		switch groupByField {
		case "cluster":
			item.ClusterUUID = groupKey
		case "namespace":
			item.Namespace = groupKey
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
		data = []vmGroupedRow{}
	}

	setRecommendationNoStore(c)
	return c.JSON(http.StatusOK, vmGroupedResponse{
		Meta: Metadata{
			Count:    total,
			Limit:    limit,
			Offset:   offset,
			HasNext:  hasNext,
			Currency: currency,
		},
		Links: buildLinks(c.Request(), total, limit, offset),
		Data:  data,
	})
}
