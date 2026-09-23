package rbac

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

func withRBAC(t *testing.T, fn func()) {
	t.Helper()
	cfg := config.GetConfig()
	orig := cfg.RBACEnabled
	cfg.RBACEnabled = true
	t.Cleanup(func() { cfg.RBACEnabled = orig })
	fn()
}

func TestAddRBACFilter_UnsupportedResourceType(t *testing.T) {
	withRBAC(t, func() {
		err := AddRBACFilter(nil, map[string][]string{}, "unknown")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported resource type")
	})
}

func TestAddRBACFilter_NodeResourceTypeAccepted(t *testing.T) {
	withRBAC(t, func() {
		err := AddRBACFilter(nil, map[string][]string{"*": {"*"}}, ResourceNode)
		require.NoError(t, err)
	})
}

func TestAddRBACFilter_DisabledDoesNothing(t *testing.T) {
	cfg := config.GetConfig()
	orig := cfg.RBACEnabled
	cfg.RBACEnabled = false
	defer func() { cfg.RBACEnabled = orig }()

	err := AddRBACFilter(nil, map[string][]string{}, ResourceNode)
	require.NoError(t, err)
}

func TestAddRBACFilter_GlobalWildcardAllowsAll(t *testing.T) {
	withRBAC(t, func() {
		perms := map[string][]string{"*": {"*"}}
		err := AddRBACFilter(nil, perms, ResourceContainer)
		require.NoError(t, err)
		err = AddRBACFilter(nil, perms, ResourceProject)
		require.NoError(t, err)
		err = AddRBACFilter(nil, perms, ResourceNode)
		require.NoError(t, err)
	})
}

// TestAddRBACFilter_ContainerUsesDenormalizedColumns pins #600: container
// RBAC predicates must hit recommendation_sets (the compat query's
// workloads/clusters JOINs are gone). Project/node keep clusters.* —
// their queries still join clusters.
func TestAddRBACFilter_ContainerUsesDenormalizedColumns(t *testing.T) {
	withRBAC(t, func() {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{DryRun: true})
		require.NoError(t, err)
		perms := map[string][]string{
			"openshift.cluster": {"11111111-1111-1111-1111-111111111111"},
			"openshift.project": {"test-ns"},
		}
		query := db.Table("recommendation_sets")
		require.NoError(t, AddRBACFilter(query, perms, ResourceContainer))
		sql := query.ToSQL(func(tx *gorm.DB) *gorm.DB { return tx.Find(nil) })
		assert.Contains(t, sql, "recommendation_sets.cluster_uuid")
		assert.Contains(t, sql, "recommendation_sets.namespace")
		assert.NotContains(t, sql, "workloads.")
		assert.NotContains(t, sql, "clusters.")
	})
}

// TestAddRBACFilter_NonContainerKeepsClusterJoin pins the #600 scope
// boundary: project and node predicates still use clusters.* (their
// queries join clusters — untouched).
func TestAddRBACFilter_NonContainerKeepsClusterJoin(t *testing.T) {
	withRBAC(t, func() {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{DryRun: true})
		require.NoError(t, err)
		perms := map[string][]string{"openshift.cluster": {"11111111-1111-1111-1111-111111111111"}}
		query := db.Table("namespace_recommendation_sets")
		require.NoError(t, AddRBACFilter(query, perms, ResourceProject))
		sql := query.ToSQL(func(tx *gorm.DB) *gorm.DB { return tx.Find(nil) })
		assert.Contains(t, sql, "clusters.cluster_uuid")
	})
}
