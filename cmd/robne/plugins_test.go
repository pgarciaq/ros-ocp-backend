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

// Round-9 B1: hcp must prune when no capacity rows are loaded — without
// this case, every default run (allShippedPlugins includes hcp) would
// emit v12 with no capacity sibling.
func TestPruneMissingFilePlugins_HCP(t *testing.T) {
	loadedWithCapacity := csv.LoadResult{
		Rows:            []csv.Row{{Namespace: "app"}},
		HCPCapacityRows: []csv.HCPCapacityRow{{CurrentHCCount: 3}},
	}
	loadedWithout := csv.LoadResult{Rows: []csv.Row{{Namespace: "app"}}}

	assert.Equal(t, []string{"container", "hcp"}, pruneMissingFilePlugins([]string{"container", "hcp"}, loadedWithCapacity))
	assert.Equal(t, []string{"container"}, pruneMissingFilePlugins([]string{"container", "hcp"}, loadedWithout))
	assert.Equal(t, []string{"hcp"}, pruneMissingFilePlugins([]string{"hcp"}, loadedWithCapacity))
}
