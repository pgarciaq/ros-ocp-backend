package snapshot

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
)

func defaultSnapshotSettings() SnapshotSettings {
	return SnapshotSettingsDefaults
}

func TestClassifySnapshotInventory_NoPool(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	inventory := []snapshotInventoryRow{
		{
			Namespace:         "production",
			SnapshotName:      "orphaned-snap",
			SourcePVCName:     "deleted-pvc",
			CreationTimestamp: now.Add(-30 * 24 * time.Hour),
			RestoreSizeBytes:  1024 * 1024 * 1024,
			SourcePVCExists:   false,
			RestoredPVCCount:  0,
			Labels:            map[string]string{},
		},
		{
			Namespace:         "production",
			SnapshotName:      "recent-snap",
			SourcePVCName:     "my-pvc",
			CreationTimestamp: now.Add(-24 * time.Hour),
			RestoreSizeBytes:  2 * 1024 * 1024 * 1024,
			SourcePVCExists:   true,
			RestoredPVCCount:  0,
			Labels:            map[string]string{},
		},
	}

	recs := ClassifySnapshotInventory(inventory, "org-1", "cluster-1", defaultSnapshotSettings(), now)
	require.Len(t, recs, 2)
	byName := map[string]SnapshotRec{}
	for _, r := range recs {
		assert.Equal(t, "org-1", r.OrgID)
		assert.Equal(t, "cluster-1", r.ClusterUUID)
		assert.NotNil(t, r.EstimatedCostCents)
		byName[r.SnapshotName] = r
	}
	assert.Equal(t, "orphaned", byName["orphaned-snap"].RecommendationType)
	assert.Contains(t, byName["orphaned-snap"].NotificationCodes, engine.NotifSnapshotOrphaned)
	assert.Equal(t, "active", byName["recent-snap"].RecommendationType)
}

func TestClassifySnapshotInventory_Empty(t *testing.T) {
	assert.Nil(t, ClassifySnapshotInventory(nil, "org", "cluster", defaultSnapshotSettings(), time.Now().UTC()))
}
