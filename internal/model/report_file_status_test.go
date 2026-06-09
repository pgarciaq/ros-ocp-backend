package model

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestReportFileStatusLifecycle(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	manifestID := "manifest-test-uuid"
	clusterID := "cluster-uuid"
	orgID := "1234567"
	filename := "ocp_ros_usage.csv"

	require.NoError(t, EnsureReportFileExpectations(ctx, pool, manifestID, clusterID, orgID, []string{filename}, func(string) string {
		return "container"
	}))

	status, err := GetReportFileStatus(ctx, pool, manifestID, filename)
	require.NoError(t, err)
	assert.Equal(t, ReportFilePending, status)

	require.NoError(t, MarkReportFileProcessing(ctx, pool, manifestID, clusterID, orgID, filename, "container"))
	status, err = GetReportFileStatus(ctx, pool, manifestID, filename)
	require.NoError(t, err)
	assert.Equal(t, ReportFileProcessing, status)

	require.NoError(t, MarkReportFileDone(ctx, pool, manifestID, filename))
	complete, err := IsManifestIngestionComplete(ctx, pool, manifestID)
	require.NoError(t, err)
	assert.True(t, complete)

	types, err := CompletedReportTypes(ctx, pool, manifestID)
	require.NoError(t, err)
	assert.Equal(t, []string{"container"}, types)
}

func TestReportFileStatusFailedBlocksCompletion(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	manifestID := "manifest-failed-uuid"
	require.NoError(t, EnsureReportFileExpectations(ctx, pool, manifestID, "cluster", "org", []string{"a.csv", "b.csv"}, func(string) string {
		return "container"
	}))
	require.NoError(t, MarkReportFileDone(ctx, pool, manifestID, "a.csv"))
	require.NoError(t, MarkReportFileFailed(ctx, pool, manifestID, "b.csv", "fetch error"))

	complete, err := IsManifestIngestionComplete(ctx, pool, manifestID)
	require.NoError(t, err)
	assert.False(t, complete)
}
