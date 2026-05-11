package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestLatestReplicaCounts(t *testing.T) {
	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	t.Run("empty rows returns zeros", func(t *testing.T) {
		desired, available := latestReplicaCounts(nil)
		assert.Equal(t, int64(0), desired)
		assert.Equal(t, int64(0), available)
	})

	t.Run("all zeros returns zeros", func(t *testing.T) {
		rows := []DigestRow{
			{BucketDate: base, DesiredReplicas: 0, AvailableReplicas: 0},
		}
		desired, available := latestReplicaCounts(rows)
		assert.Equal(t, int64(0), desired)
		assert.Equal(t, int64(0), available)
	})

	t.Run("single row with replicas", func(t *testing.T) {
		rows := []DigestRow{
			{BucketDate: base, DesiredReplicas: 3, AvailableReplicas: 3},
		}
		desired, available := latestReplicaCounts(rows)
		assert.Equal(t, int64(3), desired)
		assert.Equal(t, int64(3), available)
	})

	t.Run("takes latest day with replicas", func(t *testing.T) {
		rows := []DigestRow{
			{BucketDate: base, DesiredReplicas: 2, AvailableReplicas: 2},
			{BucketDate: base.AddDate(0, 0, 1), DesiredReplicas: 5, AvailableReplicas: 4},
			{BucketDate: base.AddDate(0, 0, 2), DesiredReplicas: 3, AvailableReplicas: 3},
		}
		desired, available := latestReplicaCounts(rows)
		assert.Equal(t, int64(3), desired)
		assert.Equal(t, int64(3), available)
	})

	t.Run("skips rows with zero desired replicas", func(t *testing.T) {
		rows := []DigestRow{
			{BucketDate: base, DesiredReplicas: 4, AvailableReplicas: 4},
			{BucketDate: base.AddDate(0, 0, 1), DesiredReplicas: 0, AvailableReplicas: 0},
		}
		desired, available := latestReplicaCounts(rows)
		assert.Equal(t, int64(4), desired)
		assert.Equal(t, int64(4), available)
	})
}
