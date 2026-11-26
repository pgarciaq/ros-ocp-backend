package api

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
)

// FleetTagSavingsRow is one tag value group in savings-summary group_by[tag:key] responses.
type FleetTagSavingsRow struct {
	TagValue                *string           `json:"tag_value"`
	EstimatedMonthlySavings money.MoneyAmount `json:"estimated_monthly_savings"`
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
	q db.QueryRower,
	params fleetSavingsByTagQuery,
) (FleetSavingsByTagResponse, error) {
	resp := FleetSavingsByTagResponse{Data: []FleetTagSavingsRow{}}

	clusterFilter, args, engineParam, termParam, _ := savingsSummaryQueryArgsForColumn(params.OrgID, params.ClusterUUIDs, params.EngineProfile, params.TermProfile, "ock.cluster_uuid")
	// This query never references vmTerm: pop it through the checked helper
	// BEFORE numbering further placeholders, or the $N gap breaks PG typing
	// with an untypable phantom parameter (#540); a future helper
	// reorder/removal errors instead of silently dropping a real arg (#565).
	args, err := popVMTermArg(args, params.TermProfile)
	if err != nil {
		return resp, err
	}
	engineRef := fmt.Sprintf("$%d", engineParam)
	termRef := fmt.Sprintf("$%d", termParam)
	argIdx := len(args) + 1

	namespaceFilter := ""
	if params.NamespaceFilter != "" {
		namespaceFilter = fmt.Sprintf(" AND ock.namespace = $%d", argIdx)
		args = append(args, params.NamespaceFilter)
		argIdx++
	}

	tagKeyParam := argIdx
	args = append(args, params.TagKey)

	// #814: tag values are stored as sorted arrays ({"k": ["a","b"]}); the
	// pre-#814 scalar rows (and a missing key, which stays a NULL group) are
	// normalized to a one-element array, so every storage shape explodes to
	// one row per value and a multi-valued namespace contributes its savings
	// to each of its values' groups. The `::text` casts pin the `->` operator
	// (jsonb->int and jsonb->text are both candidates for a bare $n).
	//
	// Trust boundary: this explosion trusts the write path's uniqueness —
	// a hand-seeded duplicate value (["a","a"]) counts the container's
	// savings twice in that group. Unreachable through the sync (sorted-
	// unique normalization); the enrichment projection deliberately does
	// NOT trust write discipline (it re-sorts) — if a dedup is ever needed
	// here, wrap the lateral in a DISTINCT the same way.
	tagKeyRef := fmt.Sprintf("$%d::text", tagKeyParam)
	rows, err := q.Query(ctx, `
		SELECT tv.value AS tag_value,
		       COALESCE(SUM(rs.estimated_savings_cents), 0)::float / 100.0 AS savings_usd
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
		CROSS JOIN LATERAL jsonb_array_elements_text(
			CASE WHEN jsonb_typeof(ock.resolved_tags->`+tagKeyRef+`) = 'array'
				THEN ock.resolved_tags->`+tagKeyRef+`
				ELSE jsonb_build_array(ock.resolved_tags->`+tagKeyRef+`)
			END
		) AS tv(value)
		WHERE ock.org_id = $1`+clusterFilter+namespaceFilter+`
		GROUP BY 1
		ORDER BY savings_usd DESC NULLS LAST`,
		args...,
	)
	if err != nil {
		return resp, err
	}
	defer rows.Close()

	storedCurrency := resolveFleetCurrency(ctx, params.OrgID, params.ClusterUUIDs)
	for rows.Next() {
		var tagValue sql.NullString
		var savingsUSD float64
		if err := rows.Scan(&tagValue, &savingsUSD); err != nil {
			return resp, err
		}
		row := FleetTagSavingsRow{
			EstimatedMonthlySavings: money.FormatUSDToAmount(roundUSD(savingsUSD), storedCurrency),
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

	userCurrency := resolveUserCurrency(ctx, params.OrgID, "")
	rate := fetchExchangeRate(ctx, params.OrgID, storedCurrency, userCurrency)
	displayCurrency := userCurrency
	if rate == costdata.PPBIdentity && storedCurrency != userCurrency {
		displayCurrency = storedCurrency
	}
	for i := range resp.Data {
		convertAndPatchAmount(&resp.Data[i].EstimatedMonthlySavings, rate, displayCurrency)
	}

	if resp.Data == nil {
		resp.Data = []FleetTagSavingsRow{}
	}
	resp.Meta.Count = len(resp.Data)
	return resp, nil
}
