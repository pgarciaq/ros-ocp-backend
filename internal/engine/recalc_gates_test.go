package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

// Recalculation respects plugin enablement (#591): a disabled plugin's
// recalc returns nil before touching the DB (nil pool safe by construction).
// Nil pool + disabled plugin must never reach a query.
func TestDefaultRecalculateCluster_DisabledPluginSkips(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "pvc,node,snapshot")

	ctx := context.Background()
	for _, recType := range []string{"pvc", "node", "snapshot"} {
		require.NoError(t, defaultRecalculateCluster(ctx, nil, "org-gate-test",
			"00000000-0000-0000-0000-000000000001", recType),
			"disabled %s plugin recalc must skip without error (and without DB)", recType)
	}
}
