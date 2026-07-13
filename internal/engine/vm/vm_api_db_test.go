package vm

import (
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildVMRecWhere_IsNetworkBound(t *testing.T) {
	isBound := true
	where, args := buildVMRecWhere("org-1", VMRecommendationFilters{IsNetworkBound: &isBound})
	assert.Contains(t, where, "is_network_bound = $2")
	require.Len(t, args, 2)
	assert.Equal(t, "org-1", args[0])
	assert.Equal(t, true, args[1])
}

func TestBuildVMRecWhere_GuestOS(t *testing.T) {
	where, args := buildVMRecWhere("org-1", VMRecommendationFilters{GuestOS: "linux,windows"})
	assert.Contains(t, where, "guest_os ILIKE")
	assert.Contains(t, where, " OR ")
	require.Len(t, args, 3)
	assert.Equal(t, "%linux%", args[1])
	assert.Equal(t, "%windows%", args[2])
}

func TestBuildVMRecWhere_TagFilter(t *testing.T) {
	config.ResetTagsForTest()
	t.Setenv("ROS_TAGS_ENABLED", "true")
	t.Setenv("ROS_TAGS_SOURCE", "api")

	where, args := buildVMRecWhere("org-1", VMRecommendationFilters{
		TagFilters: []model.TagFilter{{Key: "environment", Values: []string{"production"}}},
	})
	assert.Contains(t, where, "EXISTS")
	assert.Contains(t, where, "org_container_keys")
	require.GreaterOrEqual(t, len(args), 2)
}
