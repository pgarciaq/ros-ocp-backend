package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func defaultSnapshotSettings() SnapshotSettings {
	return SnapshotSettingsDefaults
}

func TestClassifySnapshot_Active(t *testing.T) {
	snap := snapshotInventoryRow{
		Namespace:         "production",
		SnapshotName:      "recent-snap",
		SourcePVCName:     "my-pvc",
		CreationTimestamp: time.Now().UTC().Add(-24 * time.Hour), // 1 day old
		SourcePVCExists:   true,
		RestoredPVCCount:  0,
		Labels:            map[string]string{},
	}
	settings := defaultSnapshotSettings()
	groups := map[string]*pvcGroup{}
	inventory := []snapshotInventoryRow{snap}

	classification, codes := classifySnapshot(snap, 0, 1, "", settings, groups, inventory)
	assert.Equal(t, "active", classification)
	assert.Nil(t, codes)
}

func TestClassifySnapshot_ActiveDueToRestores(t *testing.T) {
	snap := snapshotInventoryRow{
		Namespace:         "production",
		SnapshotName:      "old-but-used-snap",
		SourcePVCName:     "my-pvc",
		CreationTimestamp: time.Now().UTC().Add(-120 * 24 * time.Hour), // 120 days old
		SourcePVCExists:   true,
		RestoredPVCCount:  2,
		Labels:            map[string]string{},
	}
	settings := defaultSnapshotSettings()
	groups := map[string]*pvcGroup{}
	inventory := []snapshotInventoryRow{snap}

	classification, codes := classifySnapshot(snap, 0, 120, "", settings, groups, inventory)
	// It has restores, so it should NOT be stale (never_restored requires restored_pvc_count == 0)
	// But it IS older than 90 days... except restored_pvc_count > 0 means stale won't fire
	assert.Equal(t, "active", classification)
	assert.Nil(t, codes)
}

func TestClassifySnapshot_Orphaned(t *testing.T) {
	snap := snapshotInventoryRow{
		Namespace:         "production",
		SnapshotName:      "orphaned-snap",
		SourcePVCName:     "deleted-pvc",
		CreationTimestamp: time.Now().UTC().Add(-30 * 24 * time.Hour), // 30 days old
		SourcePVCExists:   false,
		RestoredPVCCount:  0,
		Labels:            map[string]string{},
	}
	settings := defaultSnapshotSettings()
	groups := map[string]*pvcGroup{}
	inventory := []snapshotInventoryRow{snap}

	classification, codes := classifySnapshot(snap, 0, 30, "", settings, groups, inventory)
	assert.Equal(t, "orphaned", classification)
	assert.Contains(t, codes, NotifSnapshotOrphaned)
}

func TestClassifySnapshot_OrphanedNotYoung(t *testing.T) {
	// Orphan detection requires age > orphan_age_days (7)
	snap := snapshotInventoryRow{
		Namespace:         "production",
		SnapshotName:      "young-orphan",
		SourcePVCName:     "deleted-pvc",
		CreationTimestamp: time.Now().UTC().Add(-3 * 24 * time.Hour), // 3 days old
		SourcePVCExists:   false,
		RestoredPVCCount:  0,
		Labels:            map[string]string{},
	}
	settings := defaultSnapshotSettings()
	groups := map[string]*pvcGroup{}
	inventory := []snapshotInventoryRow{snap}

	classification, _ := classifySnapshot(snap, 0, 3, "", settings, groups, inventory)
	// Too young to be orphaned — falls through to active
	assert.Equal(t, "active", classification)
}

func TestClassifySnapshot_Managed(t *testing.T) {
	snap := snapshotInventoryRow{
		Namespace:         "production",
		SnapshotName:      "velero-daily-snap",
		SourcePVCName:     "my-pvc",
		CreationTimestamp: time.Now().UTC().Add(-100 * 24 * time.Hour),
		SourcePVCExists:   true,
		RestoredPVCCount:  0,
		Labels:            map[string]string{"velero.io/backup-name": "daily-schedule"},
	}
	settings := defaultSnapshotSettings()
	groups := map[string]*pvcGroup{}
	inventory := []snapshotInventoryRow{snap}

	classification, codes := classifySnapshot(snap, 0, 100, "Velero", settings, groups, inventory)
	assert.Equal(t, "managed", classification)
	assert.Contains(t, codes, NotifSnapshotManaged)
}

func TestClassifySnapshot_Stale(t *testing.T) {
	snap := snapshotInventoryRow{
		Namespace:         "production",
		SnapshotName:      "very-old-snap",
		SourcePVCName:     "my-pvc",
		CreationTimestamp: time.Now().UTC().Add(-120 * 24 * time.Hour),
		SourcePVCExists:   true,
		RestoredPVCCount:  0,
		Labels:            map[string]string{},
	}
	settings := defaultSnapshotSettings()
	groups := map[string]*pvcGroup{}
	inventory := []snapshotInventoryRow{snap}

	classification, codes := classifySnapshot(snap, 0, 120, "", settings, groups, inventory)
	assert.Equal(t, "stale", classification)
	assert.Contains(t, codes, NotifSnapshotStale)
}

func TestClassifySnapshot_NeverRestored(t *testing.T) {
	snap := snapshotInventoryRow{
		Namespace:         "production",
		SnapshotName:      "forgotten-snap",
		SourcePVCName:     "my-pvc",
		CreationTimestamp: time.Now().UTC().Add(-45 * 24 * time.Hour), // 45 days
		SourcePVCExists:   true,
		RestoredPVCCount:  0,
		Labels:            map[string]string{},
	}
	settings := defaultSnapshotSettings()
	groups := map[string]*pvcGroup{}
	inventory := []snapshotInventoryRow{snap}

	classification, codes := classifySnapshot(snap, 0, 45, "", settings, groups, inventory)
	assert.Equal(t, "never_restored", classification)
	assert.Contains(t, codes, NotifSnapshotNeverUsed)
}

func TestClassifySnapshot_Redundant(t *testing.T) {
	now := time.Now().UTC()
	settings := defaultSnapshotSettings()
	settings.RedundantThreshold = 2

	// Create 4 snapshots for the same PVC — the oldest should be redundant
	inventory := []snapshotInventoryRow{
		{Namespace: "ns", SnapshotName: "snap-newest", SourcePVCName: "pvc1", CreationTimestamp: now.Add(-1 * 24 * time.Hour), SourcePVCExists: true, Labels: map[string]string{}},
		{Namespace: "ns", SnapshotName: "snap-recent", SourcePVCName: "pvc1", CreationTimestamp: now.Add(-30 * 24 * time.Hour), SourcePVCExists: true, Labels: map[string]string{}},
		{Namespace: "ns", SnapshotName: "snap-old", SourcePVCName: "pvc1", CreationTimestamp: now.Add(-95 * 24 * time.Hour), SourcePVCExists: true, Labels: map[string]string{}},
		{Namespace: "ns", SnapshotName: "snap-very-old", SourcePVCName: "pvc1", CreationTimestamp: now.Add(-150 * 24 * time.Hour), SourcePVCExists: true, Labels: map[string]string{}},
	}

	groups := map[string]*pvcGroup{
		"ns/pvc1": {snapshots: []int{0, 1, 2, 3}},
	}

	// snap-very-old (idx 3): 150 days > 90 stale_days, not among 2 most recent, 4 > threshold(2)
	classification, codes := classifySnapshot(inventory[3], 3, 150, "", settings, groups, inventory)
	assert.Equal(t, "redundant", classification)
	assert.Contains(t, codes, NotifSnapshotRedundant)

	// snap-old (idx 2): 95 days > 90 stale_days, not among 2 most recent, 4 > threshold(2)
	classification2, _ := classifySnapshot(inventory[2], 2, 95, "", settings, groups, inventory)
	assert.Equal(t, "redundant", classification2)

	// snap-recent (idx 1): 30 days < 90 stale_days → falls through to never_restored
	classification3, _ := classifySnapshot(inventory[1], 1, 30, "", settings, groups, inventory)
	assert.NotEqual(t, "redundant", classification3)
}

func TestClassifySnapshot_EmptySourcePVC_SkipsOrphanAndRedundant(t *testing.T) {
	snap := snapshotInventoryRow{
		Namespace:         "production",
		SnapshotName:      "pre-provisioned",
		SourcePVCName:     "", // empty
		CreationTimestamp: time.Now().UTC().Add(-100 * 24 * time.Hour),
		SourcePVCExists:   false, // even though false, empty source PVC skips orphan check
		RestoredPVCCount:  0,
		Labels:            map[string]string{},
	}
	settings := defaultSnapshotSettings()
	groups := map[string]*pvcGroup{}
	inventory := []snapshotInventoryRow{snap}

	// With empty source_pvc_name, orphan check is skipped (requires non-empty source)
	// Falls through to stale (100 > 90)
	classification, codes := classifySnapshot(snap, 0, 100, "", settings, groups, inventory)
	assert.Equal(t, "stale", classification)
	assert.Contains(t, codes, NotifSnapshotStale)
}

func TestDetectManagedTool(t *testing.T) {
	tests := []struct {
		labels   map[string]string
		expected string
	}{
		{map[string]string{"velero.io/backup-name": "daily"}, "Velero"},
		{map[string]string{"k10.kasten.io/backup-id": "abc"}, "Kasten K10"},
		{map[string]string{"backup.openshift.io/foo": "bar"}, "OpenShift Backup"},
		{map[string]string{"triliovault.trilio.io/bkp": "x"}, "Trilio"},
		{map[string]string{"stash.appscode.com/something": "y"}, "Stash/KubeStash"},
		{map[string]string{"app": "myapp"}, ""},
		{map[string]string{}, ""},
	}

	for _, tc := range tests {
		result := detectManagedTool(tc.labels)
		assert.Equal(t, tc.expected, result)
	}
}

func TestIsAmongNewest(t *testing.T) {
	now := time.Now().UTC()
	inventory := []snapshotInventoryRow{
		{CreationTimestamp: now.Add(-1 * 24 * time.Hour)},   // idx 0: newest
		{CreationTimestamp: now.Add(-5 * 24 * time.Hour)},   // idx 1: second
		{CreationTimestamp: now.Add(-10 * 24 * time.Hour)},  // idx 2: third
		{CreationTimestamp: now.Add(-100 * 24 * time.Hour)}, // idx 3: oldest
	}

	groupIdxs := []int{0, 1, 2, 3}

	assert.True(t, isAmongNewest(0, groupIdxs, 2, inventory))  // newest — yes
	assert.True(t, isAmongNewest(1, groupIdxs, 2, inventory))  // second — yes
	assert.False(t, isAmongNewest(2, groupIdxs, 2, inventory)) // third — no
	assert.False(t, isAmongNewest(3, groupIdxs, 2, inventory)) // oldest — no
}
