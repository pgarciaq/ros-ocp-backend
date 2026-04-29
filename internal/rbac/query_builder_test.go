package rbac

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
