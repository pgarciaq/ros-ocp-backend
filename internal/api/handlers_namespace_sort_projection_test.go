package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	ros_middleware "github.com/redhatinsights/ros-ocp-backend/internal/api/middleware"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// #923: the namespace list's sort key must come from the same row the
// display shows — the term/engine-ASC-first row among the detail-filtered
// rows (the projected row when filter[term]/filter[engine] are set). The
// pre-fix sort key was the max across ALL term/engine rows, so the served
// order did not match the served savings column.

func TestNamespaceSavingsSortMatchesDisplayedValues(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.DB = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'sort-div-cluster', 'src-sd', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	// Two namespaces whose per-term savings disagree:
	//   ns-sort-high-max:  long/cost=50, medium/cost=60000, short/cost=100
	//   ns-sort-stable:    long/cost=30000, medium/cost=30000, short/cost=50000
	// Pre-fix (max-across-terms key), ns-sort-high-max sorts FIRST on every
	// request while displaying the SMALLEST savings — the served column is
	// out of order. Post-fix (display-row key), the order matches the column.
	rows := []struct {
		namespace string
		term      string
		savings   int64
	}{
		{"ns-sort-high-max", "long", 50},
		{"ns-sort-high-max", "medium", 60000},
		{"ns-sort-high-max", "short", 100},
		{"ns-sort-stable", "long", 30000},
		{"ns-sort-stable", "medium", 30000},
		{"ns-sort-stable", "short", 50000},
	}
	for _, r := range rows {
		_, err := pool.Exec(ctx, `
			INSERT INTO namespace_recommendation_sets (
				org_id, cluster_uuid, namespace_name, term, engine, stale,
				rec_cpu_request_millicores, rec_cpu_limit_millicores,
				rec_memory_request_kib, rec_memory_limit_kib,
				current_cpu_request_millicores, current_cpu_limit_millicores,
				current_memory_request_kib, current_memory_limit_kib,
				variation_cpu_request_pct, variation_cpu_limit_pct,
				variation_memory_request_pct, variation_memory_limit_pct,
				confidence_level, notification_codes, monitoring_start_time, monitoring_end_time, updated_at,
				estimated_savings_cents
			) VALUES (
				$1, $2, $3, $4, 'cost', false,
				4000, 8000, 8388608, 16777216,
				5000, 10000, 10485760, 20971520,
				-10, -10, -10, -10,
				0.9, '{}', NOW() - interval '8 days', NOW() - interval '1 day', NOW(),
				$5
			)`,
			testutil.TestOrgID, testutil.TestClusterUUID, r.namespace, r.term, r.savings,
		)
		require.NoError(t, err)
	}
	require.NoError(t, model.RefreshOrgNamespaceKeys(ctx, pool, testutil.TestOrgID))

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/namespaces", api.GetNamespaceRecommendationSetListWithFallback)
	identityHeader := makeIdentityHeader(testutil.TestOrgID)
	base := "/api/cost-management/v1/recommendations/openshift/namespaces"

	// (query suffix, expected first displayed savings in dollars, label)
	cases := []struct {
		query       string
		wantFirst   float64
		description string
	}{
		{"?limit=10&order_by=estimated_monthly_savings&order_how=desc", 300.0, "unfiltered: display row is long/cost"},
		{"?limit=10&order_by=estimated_monthly_savings&order_how=desc&filter%5Bterm%5D=short", 500.0, "term-projected: display row is short/cost"},
		{"?limit=10&order_by=estimated_monthly_savings&order_how=desc&filter%5Bstale%5D=true", 300.0, "stale-inclusive (DISTINCT path): display row is long/cost"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, base+tc.query, nil)
			req.Header.Set("X-Rh-Identity", identityHeader)
			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, req)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

			var resp struct {
				Data []struct {
					Project         string `json:"project"`
					Recommendations struct {
						EstimatedMonthlySavings *struct {
							Value string `json:"value"`
						} `json:"estimated_monthly_savings"`
					} `json:"recommendations"`
				} `json:"data"`
				Meta struct {
					Count int `json:"count"`
				} `json:"meta"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
			require.Equal(t, 2, resp.Meta.Count, "both seeded namespaces must be listed")

			var vals []float64
			for _, row := range resp.Data {
				require.NotNil(t, row.Recommendations.EstimatedMonthlySavings,
					"row %s carries no estimated_monthly_savings", row.Project)
				v, parseErr := strconv.ParseFloat(row.Recommendations.EstimatedMonthlySavings.Value, 64)
				require.NoError(t, parseErr, "savings value is not numeric: %q", row.Recommendations.EstimatedMonthlySavings.Value)
				vals = append(vals, v)
			}

			assert.Equal(t, tc.wantFirst, vals[0],
				"the first row must be the biggest DISPLAYED savings (%.2f), got %.2f — "+
					"the sort key must come from the row the response shows (#923)",
				tc.wantFirst, vals[0])
			assert.GreaterOrEqual(t, vals[0], vals[1],
				"served savings order must be non-increasing: %v (#923)", vals)
		})
	}
}
