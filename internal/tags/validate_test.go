package tags_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/tags"
)

func TestValidateSyncRequest(t *testing.T) {
	valid := tags.SyncRequest{
		OrgID:    "1234567",
		SyncedAt: time.Now().UTC().Format(time.RFC3339),
		TagKeys: []tags.TagKeyCatalog{
			{Key: "environment", Values: []string{"prod"}},
		},
		NamespaceTags: []tags.NamespaceTags{
			{
				ClusterUUID: "550e8400-e29b-41d4-a716-446655440000",
				Namespace:   "payments",
				Tags:        map[string]string{"environment": "prod"},
			},
		},
	}

	t.Run("valid request", func(t *testing.T) {
		require.NoError(t, tags.ValidateSyncRequest(valid))
	})

	t.Run("missing org_id", func(t *testing.T) {
		req := valid
		req.OrgID = "   "
		err := tags.ValidateSyncRequest(req)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "org_id is required")
	})

	t.Run("invalid synced_at", func(t *testing.T) {
		req := valid
		req.SyncedAt = "not-a-timestamp"
		err := tags.ValidateSyncRequest(req)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid synced_at")
	})

	t.Run("empty synced_at allowed", func(t *testing.T) {
		req := valid
		req.SyncedAt = ""
		require.NoError(t, tags.ValidateSyncRequest(req))
	})

	t.Run("null namespace tags allowed", func(t *testing.T) {
		req := valid
		req.NamespaceTags = nil
		require.NoError(t, tags.ValidateSyncRequest(req))
	})

	t.Run("oversized tag value rejected", func(t *testing.T) {
		req := valid
		req.NamespaceTags = []tags.NamespaceTags{
			{
				ClusterUUID: "550e8400-e29b-41d4-a716-446655440000",
				Namespace:   "payments",
				Tags:        map[string]string{"environment": strings.Repeat("x", 1025)},
			},
		}
		err := tags.ValidateSyncRequest(req)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tag value exceeds maximum")
	})
}
