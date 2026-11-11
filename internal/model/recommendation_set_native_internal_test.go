package model

// Internal (package model) tests for the native container DISTINCT
// fallback path — unexported, so unreachable from the model_test external
// package. The path is currently dormant (usesOrgContainerKeys() is
// unconditionally true, ADR-0306), which is exactly why #840's review
// demanded direct coverage before it can ever be revived: it carries the
// same tenant-isolation obligation as the live keys path.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pgrec"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

// TestGetNativeRecommendationsDistinct_ScopesByOrgID is #840's regression
// for the DISTINCT fallback: org B's row for a cluster UUID co-registered
// by org A must not appear in org A's list. Seeded through the production
// writer (pgrec) — no engine dependency, so this internal test avoids the
// engine↔model import cycle.
func TestGetNativeRecommendationsDistinct_ScopesByOrgID(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.DB = nil })

	const clusterUUID = "99999999-9999-9999-9999-999999999999"
	const tenantA = 90
	const tenantB = 91
	orgA := testutil.TestOrgID
	orgB := testutil.TestOrgID + "-distinct-b"

	for _, seed := range []struct {
		tenant int
		org    string
		alias  string
	}{
		{tenantA, orgA, "a-cluster"},
		{tenantB, orgB, "b-cluster"},
	} {
		_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, seed.tenant, seed.org)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
			VALUES ($1, $2, $3, 'src-1', now()) ON CONFLICT DO NOTHING`, seed.tenant, clusterUUID, seed.alias)
		require.NoError(t, err)
	}

	base := types.ContainerRec{
		ClusterUUID:     clusterUUID,
		Namespace:       "demo-app",
		Workload:        "web",
		WorkloadType:    "Deployment",
		ContainerName:   "c0",
		RecCPURequestMC: 100,
	}
	recA := base
	recA.OrgID = orgA
	recA.Term = "short"
	recA.Engine = "cost"
	rogue := base
	rogue.OrgID = orgB
	rogue.Term = "rogue"
	rogue.Engine = "cost"
	rogue.RecCPURequestMC = 9000
	require.NoError(t, pgrec.WriteRecommendations(ctx, pool, []types.ContainerRec{recA, rogue}))

	page, err := getNativeRecommendationsDistinct(database.DB, orgA, listoptions.ListOptions{Limit: 10},
		map[string]interface{}{}, map[string][]string{"*": {}}, money.DefaultCurrency)
	require.NoError(t, err)
	require.NotEmpty(t, page.Results, "org A's own recommendation must be present (control)")
	for _, r := range page.Results {
		_, hasRogue := r.Recommendations["rogue_term"]
		assert.False(t, hasRogue,
			"org B's rogue_term variant must not appear in org A's DISTINCT-path list — the fallback expansion must scope rs by org_id (#840)")
		short, hasShort := r.Recommendations["short_term"]
		if assert.True(t, hasShort, "org A's own short-term variant must be present (control)") {
			assert.NotNil(t, short.Cost)
		}
	}
}
