package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDetectSnapshotAdoption(t *testing.T) {
	tests := []struct {
		name      string
		inventory map[string]bool
		oldRecs   []OldSnapshotRecommendation
		want      map[string]bool
	}{
		{
			"snapshot disappeared = adopted",
			map[string]bool{"snap-b": true},
			[]OldSnapshotRecommendation{
				{SnapshotName: "snap-a", RecommendationType: "orphaned", UpdatedAt: time.Now()},
				{SnapshotName: "snap-b", RecommendationType: "stale", UpdatedAt: time.Now()},
			},
			map[string]bool{"snap-a": true},
		},
		{
			"active recs are ignored",
			map[string]bool{},
			[]OldSnapshotRecommendation{
				{SnapshotName: "snap-a", RecommendationType: "active", UpdatedAt: time.Now()},
			},
			map[string]bool{},
		},
		{
			"still present = not adopted",
			map[string]bool{"snap-a": true, "snap-b": true},
			[]OldSnapshotRecommendation{
				{SnapshotName: "snap-a", RecommendationType: "orphaned", UpdatedAt: time.Now()},
				{SnapshotName: "snap-b", RecommendationType: "stale", UpdatedAt: time.Now()},
			},
			map[string]bool{},
		},
		{
			"empty inventory means all adopted",
			map[string]bool{},
			[]OldSnapshotRecommendation{
				{SnapshotName: "snap-a", RecommendationType: "orphaned", UpdatedAt: time.Now()},
				{SnapshotName: "snap-b", RecommendationType: "redundant", UpdatedAt: time.Now()},
			},
			map[string]bool{"snap-a": true, "snap-b": true},
		},
		{
			"empty old recs",
			map[string]bool{"snap-a": true},
			nil,
			map[string]bool{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectSnapshotAdoption(tt.inventory, tt.oldRecs)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildSnapshotQualityRows_Empty(t *testing.T) {
	rows := BuildSnapshotQualityRows("org1", "cluster-1", nil, nil)
	assert.Nil(t, rows)
}

func TestBuildSnapshotQualityRows_Basic(t *testing.T) {
	oldRecs := []OldSnapshotRecommendation{
		{SnapshotName: "snap-orphan", RecommendationType: "orphaned", UpdatedAt: time.Now().UTC().Add(-72 * time.Hour)},
		{SnapshotName: "snap-stale", RecommendationType: "stale", UpdatedAt: time.Now().UTC().Add(-24 * time.Hour)},
		{SnapshotName: "snap-active", RecommendationType: "active", UpdatedAt: time.Now().UTC()},
	}
	currentInventory := map[string]bool{
		"snap-stale": true,
	}

	rows := BuildSnapshotQualityRows("org1", "cluster-1", oldRecs, currentInventory)
	assert.Len(t, rows, 2)

	rowMap := make(map[string]SnapshotQualityRow)
	for _, r := range rows {
		rowMap[r.SnapshotName] = r
	}

	orphanRow := rowMap["snap-orphan"]
	assert.True(t, orphanRow.AdoptionDetected)
	assert.Greater(t, orphanRow.RecommendationAgeHrs, int64(0))

	staleRow := rowMap["snap-stale"]
	assert.False(t, staleRow.AdoptionDetected)
	assert.Greater(t, staleRow.RecommendationAgeHrs, int64(0))
}

func TestBuildSnapshotQualityRows_Deduplicates(t *testing.T) {
	oldRecs := []OldSnapshotRecommendation{
		{SnapshotName: "snap-a", RecommendationType: "orphaned", UpdatedAt: time.Now().UTC()},
		{SnapshotName: "snap-a", RecommendationType: "stale", UpdatedAt: time.Now().UTC()},
	}
	rows := BuildSnapshotQualityRows("org1", "cluster-1", oldRecs, map[string]bool{})
	assert.Len(t, rows, 1)
}
