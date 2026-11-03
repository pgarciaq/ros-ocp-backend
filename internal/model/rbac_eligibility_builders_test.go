package model

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	"gorm.io/gorm"
)

// #753: builder-level units — settings-only permissions must produce a
// deny-all clause in every query builder whose fall-through treated absent
// OpenShift grants as unrestricted. Each assertion names the pre-fix
// behavior it rules out.

func setupBuilderTest(t *testing.T) {
	t.Helper()
	t.Setenv("RBAC_ENABLE", "true")
	config.ResetForTest()
	t.Cleanup(config.ResetForTest) // rebuild config from restored env — no RBACEnabled leak into later tests
	pool := testutil.SetupTestDB(t)
	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() {
		database.DB = nil
		database.Pool = nil
	})
}

func settingsOnlyPerms() map[string][]string {
	return map[string][]string{"settings.read": {"*"}}
}

func TestApplyNativeRBAC_Eligibility(t *testing.T) {
	setupBuilderTest(t)

	sql := database.GetDB().Table("recommendation_sets").ToSQL(func(tx *gorm.DB) *gorm.DB {
		return ApplyNativeRBAC(tx, settingsOnlyPerms()).Find(&[]RecommendationSet{})
	})
	assert.Contains(t, sql, "1 = 0",
		"settings-only must produce a deny-all clause (pre-fix: no WHERE added for absent grants)")

	sql = database.GetDB().Table("recommendation_sets").ToSQL(func(tx *gorm.DB) *gorm.DB {
		return ApplyNativeRBAC(tx, map[string][]string{"*": {}}).Find(&[]RecommendationSet{})
	})
	assert.NotContains(t, sql, "1 = 0", "wildcard stays unrestricted")
}

func TestApplyNativeNamespaceKeysRBAC_Eligibility(t *testing.T) {
	setupBuilderTest(t)

	sql := database.GetDB().Table("openshift_namespace_keys").ToSQL(func(tx *gorm.DB) *gorm.DB {
		return applyNativeNamespaceKeysRBAC(tx, settingsOnlyPerms()).Find(&[]RecommendationSet{})
	})
	assert.Contains(t, sql, "1 = 0",
		"settings-only must produce a deny-all clause (pre-fix: no WHERE added for absent grants)")

	sql = database.GetDB().Table("openshift_namespace_keys").ToSQL(func(tx *gorm.DB) *gorm.DB {
		return applyNativeNamespaceKeysRBAC(tx, map[string][]string{"openshift.cluster": {"11111111-1111-1111-1111-111111111111"}}).Find(&[]RecommendationSet{})
	})
	require.NotContains(t, sql, "1 = 0", "cluster-granted user is scoped, not denied")
}

func TestApplyNativeNamespaceRBAC_Eligibility(t *testing.T) {
	setupBuilderTest(t)

	sql := database.GetDB().Table("namespace_recommendation_sets").ToSQL(func(tx *gorm.DB) *gorm.DB {
		return applyNativeNamespaceRBAC(tx, settingsOnlyPerms()).Find(&[]RecommendationSet{})
	})
	assert.Contains(t, sql, "1 = 0",
		"settings-only must produce a deny-all clause (pre-fix: no WHERE added for absent grants)")
}

// TestApplyNativeRBAC_RowCounts (#753 review Important 6): row-count proof on
// the container path, including the empirical empty-IN check — an explicitly
// empty cluster grant must deny (GORM renders an empty IN as no matches).
func TestApplyNativeRBAC_RowCounts(t *testing.T) {
	setupBuilderTest(t)
	pool := database.GetPool()
	ctx := context.Background()
	const testOrg = "org-builder-rowcounts"
	const clusterA = "11111111-1111-1111-1111-111111111111"
	const clusterB = "22222222-2222-2222-2222-222222222222"
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (90, $1) ON CONFLICT DO NOTHING`, testOrg)
	require.NoError(t, err)
	for _, cu := range []string{clusterA, clusterB} {
		_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, org_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
			VALUES (90, $2, $1, 'builder-rowcounts', 'src-brc', now()) ON CONFLICT DO NOTHING`, cu, testOrg)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `INSERT INTO recommendation_sets (
				org_id, cluster_uuid, namespace, workload, workload_type, container_name,
				term, engine, stale, notification_codes, updated_at)
			VALUES ($1, $2, 'ns-brc', 'wl-brc', 'Deployment', 'ctr-brc', 'short', 'cost', false, '{}', now())`,
			testOrg, cu)
		require.NoError(t, err)
	}

	base := func() *gorm.DB {
		return database.GetDB().Table("recommendation_sets rs").
			Joins("LEFT JOIN clusters c ON rs.cluster_uuid = c.cluster_uuid AND c.org_id = ?", testOrg).
			Where("rs.org_id = ?", testOrg)
	}
	countFor := func(perms map[string][]string) int64 {
		var n int64
		require.NoError(t, ApplyNativeRBAC(base(), perms).Count(&n).Error)
		return n
	}

	assert.Equal(t, int64(0), countFor(settingsOnlyPerms()),
		"settings-only returns zero rows against seeded data")
	assert.Equal(t, int64(0), countFor(map[string][]string{"openshift.cluster": {}}),
		"explicitly empty cluster grant denies (empirical empty-IN check)")
	assert.Equal(t, int64(2), countFor(map[string][]string{"*": {}}),
		"wildcard returns both clusters' rows")
	assert.Equal(t, int64(1), countFor(map[string][]string{"openshift.cluster": {clusterA}}),
		"cluster grant scopes to that cluster's rows")
}
