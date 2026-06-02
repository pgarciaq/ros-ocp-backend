package api

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/money"
)

// FleetTagSavingsRow is one tag value group in savings-summary group_by[tag:key] responses.
type FleetTagSavingsRow struct {
	TagValue                *string             `json:"tag_value"`
	EstimatedMonthlySavings money.SavingsObject `json:"estimated_monthly_savings"`
}

// FleetSavingsByTagMeta is metadata for tag-grouped savings summary.
type FleetSavingsByTagMeta struct {
	Count int `json:"count"`
}

// FleetSavingsByTagResponse is returned when group_by[tag:key] is requested on savings-summary.
type FleetSavingsByTagResponse struct {
	Meta FleetSavingsByTagMeta `json:"meta"`
	Data []FleetTagSavingsRow  `json:"data"`
}

// fleetSavingsByTagQuery holds parameters for tag-grouped fleet savings SQL.
type fleetSavingsByTagQuery struct {
	OrgID           string
	ClusterUUIDs    []string
	NamespaceFilter string
	EngineProfile   string
	TermProfile     string
	TagKey          string
}

func queryFleetSavingsByTag(
	ctx context.Context,
	pool *pgxpool.Pool,
	q fleetSavingsByTagQuery,
) (FleetSavingsByTagResponse, error) {
	resp := FleetSavingsByTagResponse{Data: []FleetTagSavingsRow{}}

	clusterFilter, args, engineParam, termParam := savingsSummaryQueryArgsForColumn(q.OrgID, q.ClusterUUIDs, q.EngineProfile, q.TermProfile, "ock.cluster_uuid")
	engineRef := fmt.Sprintf("$%d", engineParam)
	termRef := fmt.Sprintf("$%d", termParam)
	argIdx := len(args) + 1

	namespaceFilter := ""
	if q.NamespaceFilter != "" {
		namespaceFilter = fmt.Sprintf(" AND ock.namespace = $%d", argIdx)
		args = append(args, q.NamespaceFilter)
		argIdx++
	}

	tagKeyParam := argIdx
	args = append(args, q.TagKey)

	rows, err := pool.Query(ctx, `
		SELECT ock.resolved_tags->>$`+fmt.Sprintf("%d", tagKeyParam)+` AS tag_value,
		       COALESCE(SUM(rs.estimated_monthly_savings_usd), 0)::float / 100.0 AS savings_usd
		FROM org_container_keys ock
		INNER JOIN recommendation_sets rs
			ON rs.org_id = ock.org_id
			AND rs.cluster_uuid = ock.cluster_uuid
			AND rs.namespace = ock.namespace
			AND rs.workload = ock.workload
			AND rs.container_name = ock.container_name
			AND rs.term = `+termRef+`
			AND rs.engine = `+engineRef+`
			AND rs.stale = false
		WHERE ock.org_id = $1`+clusterFilter+namespaceFilter+`
		GROUP BY 1
		ORDER BY savings_usd DESC NULLS LAST`,
		args...,
	)
	if err != nil {
		return resp, err
	}
	defer rows.Close()

	currency := resolveFleetCurrency(ctx, q.OrgID, q.ClusterUUIDs)
	for rows.Next() {
		var tagValue sql.NullString
		var savingsUSD float64
		if err := rows.Scan(&tagValue, &savingsUSD); err != nil {
			return resp, err
		}
		row := FleetTagSavingsRow{
			EstimatedMonthlySavings: money.FormatUSDToSavings(roundUSD(savingsUSD), currency),
		}
		if tagValue.Valid {
			v := tagValue.String
			row.TagValue = &v
		}
		resp.Data = append(resp.Data, row)
	}
	if err := rows.Err(); err != nil {
		return resp, err
	}
	if resp.Data == nil {
		resp.Data = []FleetTagSavingsRow{}
	}
	resp.Meta.Count = len(resp.Data)
	return resp, nil
}
