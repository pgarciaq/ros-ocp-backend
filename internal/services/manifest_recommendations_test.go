package services

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	"github.com/redhatinsights/ros-ocp-backend/internal/types"
)

func TestRunManifestRecommendations_RunsVMWhenManifestComplete(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "")

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	manifestID := "manifest-vm-complete"
	orgID := testutil.TestOrgID
	clusterUUID := testutil.TestClusterUUID
	filename := "ocp_vm_usage.csv"

	require.NoError(t, model.EnsureReportFileExpectations(ctx, pool, manifestID, clusterUUID, orgID, []string{filename}, func(string) string {
		return string(types.PayloadTypeVM)
	}))
	require.NoError(t, model.MarkReportFileProcessing(ctx, pool, manifestID, clusterUUID, orgID, filename, string(types.PayloadTypeVM)))
	require.NoError(t, model.MarkReportFileDone(ctx, pool, manifestID, filename))

	var vmRuns atomic.Int32
	restoreVM := setRunVMRecommendationsHookForTest(func(context.Context, types.KafkaMsg) error {
		vmRuns.Add(1)
		return nil
	})
	t.Cleanup(restoreVM)

	msg := types.KafkaMsg{}
	msg.Metadata.Org_id = orgID
	msg.Metadata.Cluster_uuid = clusterUUID
	msg.Metadata.Manifest_id = manifestID
	require.NoError(t, runManifestRecommendations(ctx, pool, msg))
	assert.Equal(t, int32(1), vmRuns.Load())
}

func setRunVMRecommendationsHookForTest(hook func(context.Context, types.KafkaMsg) error) func() {
	prev := runVMRecommendationsHook
	runVMRecommendationsHook = hook
	return func() { runVMRecommendationsHook = prev }
}
