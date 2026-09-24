package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/platform-go-middlewares/identity"
	"github.com/stretchr/testify/require"

	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// TestNamespaceCompatDetail_SynthesizesContent is the #599 Phase 5 RED
// contract for the pure-compat namespace detail path: a native namespace
// (blob-empty typed rows) must serve a synthesized legacy blob with 200.
// Pre-fix the handler's 404-on-empty fires for any row whose stored
// recommendations blob is empty — which all native rows are — so the
// Kruize-mode detail returns not_found instead of content.
func TestNamespaceCompatDetail_SynthesizesContent(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	seedCompatNamespaceDetail(t, pool)
	database.DB = testutil.OpenTestGORM(pool)

	nsID := model.NativeNamespaceID(testutil.TestClusterUUID, "ns-detail")
	c, rec := newCompatNamespaceDetailContext(nsID)

	err := GetNamespaceRecommendationSet(c)
	require.NoError(t, err, "handler must not return a Go error")
	require.Equal(t, http.StatusOK, rec.Code, "native namespace detail must synthesize instead of 404-on-empty")

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assertDetailSynthesizedName(t, body)
}

// seedCompatNamespaceDetail inserts a cluster and one full native namespace
// sextet (short/medium/long × cost/performance) with an empty stored blob.
func seedCompatNamespaceDetail(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	orgID := testutil.TestOrgID
	clusterUUID := testutil.TestClusterUUID

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, org_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, $2, 'compat-detail-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, orgID, clusterUUID)
	require.NoError(t, err)

	type rowSpec struct {
		term, engine string
		recCPU       int64
		recMem       int64
	}
	specs := []rowSpec{
		{"short", "cost", 500, 524288},
		{"short", "performance", 2000, 2097152},
		{"medium", "cost", 400, 419430},
		{"medium", "performance", 1500, 1572864},
		{"long", "cost", 300, 314572},
		{"long", "performance", 1200, 1258291},
	}
	start := map[string]string{
		"short":  "2024-01-14T00:00:00Z",
		"medium": "2024-01-08T00:00:00Z",
		"long":   "2023-12-31T00:00:00Z",
	}
	const end = "2024-01-15T00:00:00Z"
	for _, s := range specs {
		nsID := model.NativeNamespaceID(clusterUUID, "ns-detail")
		codes := "{}"
		if s.term == "short" && s.engine == "cost" {
			codes = "{1,77}"
		}
		_, err := pool.Exec(ctx, `
			INSERT INTO namespace_recommendation_sets (
				org_id, cluster_uuid, namespace_name, namespace_id, term, engine,
				rec_cpu_request_millicores, rec_cpu_limit_millicores,
				rec_memory_request_kib, rec_memory_limit_kib,
				current_cpu_request_millicores, current_cpu_limit_millicores,
				current_memory_request_kib, current_memory_limit_kib,
				monitoring_start_time, monitoring_end_time, notification_codes,
				confidence_level, updated_at
			) VALUES ($1, $2::uuid, 'ns-detail', $3::uuid, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
				$14::timestamptz, $15::timestamptz, $16, $17, now())`,
			orgID, clusterUUID, nsID, s.term, s.engine,
			s.recCPU, s.recCPU+1000,
			s.recMem, s.recMem+524288,
			1000, 2000,
			1048576, 2097152,
			start[s.term], end, codes,
			0.92,
		)
		require.NoError(t, err)
	}
}

func newCompatNamespaceDetailContext(nsID string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/namespace/"+nsID, nil)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("Identity", identity.XRHID{
		Identity: identity.Identity{OrgID: testutil.TestOrgID},
	})
	c.Set("user.permissions", map[string][]string{"*": {}})
	c.SetParamNames("recommendation-id")
	c.SetParamValues(nsID)
	return c, rec
}

func assertDetailSynthesizedName(t *testing.T, body map[string]interface{}) {
	t.Helper()
	if body["id"] != model.NativeNamespaceID(testutil.TestClusterUUID, "ns-detail") {
		t.Fatalf("expected id to be namespace_id, got %v", body["id"])
	}
	recs, ok := body["recommendations"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected a synthesized recommendations object, got %v", body["recommendations"])
	}
	if _, ok := recs["monitoring_end_time"]; !ok {
		t.Fatalf("expected monitoring_end_time in synthesized blob: %v", recs)
	}
	terms, ok := recs["recommendation_terms"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected recommendation_terms in synthesized blob: %v", recs)
	}
	for _, key := range []string{"short_term", "medium_term", "long_term"} {
		if _, ok := terms[key]; !ok {
			t.Fatalf("expected %s term in synthesized blob: %v", key, terms)
		}
	}
}
