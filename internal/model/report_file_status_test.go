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

func TestClearSynthManifestStatusForCluster(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	orgID := "org-clear-synth"
	clusterA := "cluster-aaaa"
	clusterB := "cluster-bbbb"

	// Seed synth manifests for cluster A
	require.NoError(t, EnsureReportFileExpectations(ctx, pool,
		"synth-manifest-a1", clusterA, orgID, []string{"ros.csv"}, func(string) string { return "container" }))
	require.NoError(t, MarkReportFileDone(ctx, pool, "synth-manifest-a1", "ros.csv"))
	require.NoError(t, EnsureReportFileExpectations(ctx, pool,
		"synth-manifest-a2", clusterA, orgID, []string{"ros2.csv"}, func(string) string { return "container" }))
	require.NoError(t, MarkReportFileDone(ctx, pool, "synth-manifest-a2", "ros2.csv"))

	// Seed a non-synth manifest for cluster A (should not be deleted)
	require.NoError(t, EnsureReportFileExpectations(ctx, pool,
		"real-manifest-a1", clusterA, orgID, []string{"pod.csv"}, func(string) string { return "container" }))
	require.NoError(t, MarkReportFileDone(ctx, pool, "real-manifest-a1", "pod.csv"))

	// Seed synth manifest for cluster B (should not be deleted)
	require.NoError(t, EnsureReportFileExpectations(ctx, pool,
		"synth-manifest-b1", clusterB, orgID, []string{"ros.csv"}, func(string) string { return "container" }))
	require.NoError(t, MarkReportFileDone(ctx, pool, "synth-manifest-b1", "ros.csv"))

	// Clear synth manifests for cluster A only
	cleared, err := ClearSynthManifestStatusForCluster(ctx, pool, orgID, clusterA)
	require.NoError(t, err)
	assert.Equal(t, int64(2), cleared)

	// Synth manifests for cluster A should be gone
	status, err := GetReportFileStatus(ctx, pool, "synth-manifest-a1", "ros.csv")
	require.NoError(t, err)
	assert.Empty(t, status)

	status, err = GetReportFileStatus(ctx, pool, "synth-manifest-a2", "ros2.csv")
	require.NoError(t, err)
	assert.Empty(t, status)

	// Non-synth manifest for cluster A should remain
	status, err = GetReportFileStatus(ctx, pool, "real-manifest-a1", "pod.csv")
	require.NoError(t, err)
	assert.Equal(t, ReportFileDone, status)

	// Synth manifest for cluster B should remain
	status, err = GetReportFileStatus(ctx, pool, "synth-manifest-b1", "ros.csv")
	require.NoError(t, err)
	assert.Equal(t, ReportFileDone, status)
}
