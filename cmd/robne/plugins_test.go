package main

import (
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/librobne/csv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyFilePlugins_ImplicitPrunesMissing(t *testing.T) {
	loaded := csv.LoadResult{
		Rows:  []csv.Row{{}},
		Files: []string{"ocp_ros_usage.csv"},
	}
	got, err := applyFilePlugins(allShippedPlugins(), false, loaded)
	require.NoError(t, err)
	assert.Equal(t, []string{"container", "node"}, got)
}

func TestApplyFilePlugins_ExplicitMissingSnapshotErrors(t *testing.T) {
	loaded := csv.LoadResult{
		Rows:  []csv.Row{{}},
		Files: []string{"ocp_ros_usage.csv"},
	}
	_, err := applyFilePlugins([]string{"snapshot"}, true, loaded)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "snapshot")
}

func TestApplyFilePlugins_ImplicitKeepsClassifiedSnapshot(t *testing.T) {
	loaded := csv.LoadResult{Files: []string{"ocp_snapshot_inventory.csv"}}
	got, err := applyFilePlugins(allShippedPlugins(), false, loaded)
	require.NoError(t, err)
	assert.Equal(t, []string{"snapshot"}, got)
}
