package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// #753: helper-level units for the two unexported api-layer RBAC helpers
// whose fall-through granted unrestricted scope to absent-grant identities.

func TestGpuMIGEntryRBACVisible_Eligibility(t *testing.T) {
	t.Setenv("RBAC_ENABLE", "true")
	config.ResetForTest()
	t.Cleanup(config.ResetForTest) // rebuild config from restored env — no RBACEnabled leak into later tests

	// Settings-only: no data-read eligibility (pre-fix: !hasNode → true).
	assert.False(t, gpuMIGEntryRBACVisible("node-a", map[string][]string{"settings.read": {"*"}}),
		"settings-only must not see GPU MIG entries (pre-fix: absent node grant treated as unrestricted)")
	assert.False(t, gpuMIGEntryRBACVisible("node-a", map[string][]string{}),
		"empty permissions must not see GPU MIG entries")

	// Positive controls.
	assert.True(t, gpuMIGEntryRBACVisible("node-a", map[string][]string{"*": {}}),
		"global wildcard stays unrestricted")
	assert.True(t, gpuMIGEntryRBACVisible("node-a", map[string][]string{"openshift.cluster": {testutil.TestClusterUUID}}),
		"cluster-granted user with no node grant inherits the cluster scope (intentional inheritance)")
	assert.True(t, gpuMIGEntryRBACVisible("node-a", map[string][]string{"openshift.node": {"node-a"}}),
		"node-granted user sees their granted node")
	assert.False(t, gpuMIGEntryRBACVisible("node-b", map[string][]string{"openshift.node": {"node-a"}}),
		"node grant does not extend to other nodes")
}

func TestSnapshotRBACClusterFilter_Eligibility(t *testing.T) {
	t.Setenv("RBAC_ENABLE", "true")
	config.ResetForTest()
	t.Cleanup(config.ResetForTest) // rebuild config from restored env — no RBACEnabled leak into later tests

	// Settings-only: deny (pre-fix: !hasCluster → no restriction).
	_, args, _, deny := snapshotRBACClusterFilter(map[string][]string{"settings.write": {"*"}}, 1)
	assert.True(t, deny,
		"settings-only must be denied snapshot rows (pre-fix: absent cluster grant treated as unrestricted)")
	assert.Empty(t, args)

	// Positive controls.
	_, _, _, deny = snapshotRBACClusterFilter(map[string][]string{"*": {}}, 1)
	assert.False(t, deny, "global wildcard stays unrestricted")

	sql, args, _, deny := snapshotRBACClusterFilter(map[string][]string{"openshift.cluster": {testutil.TestClusterUUID}}, 1)
	require.False(t, deny, "cluster-scoped user is restricted, not denied")
	require.NotEmpty(t, sql, "cluster scope produces a filter clause")
	require.Len(t, args, 1)

	// Explicit-empty grant: deny (existing behavior — the :311 guard).
	_, _, _, deny = snapshotRBACClusterFilter(map[string][]string{"openshift.cluster": {}}, 1)
	assert.True(t, deny, "explicitly empty cluster grant denies")
}
