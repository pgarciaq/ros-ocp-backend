package tags_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/tags"
)

func TestValidateTagAuthConfig_DevTokenForbiddenInProduction(t *testing.T) {
	config.ResetTagsForTest()
	t.Setenv("DEVELOPMENT", "false")
	t.Setenv("ROS_TAGS_DEV_TOKEN", "secret")
	_ = config.GetConfig()

	err := tags.ValidateTagAuthConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ROS_TAGS_DEV_TOKEN")
}

func TestValidateTagAuthConfig_EmptyAllowlistForbiddenForAPISource(t *testing.T) {
	config.ResetTagsForTest()
	t.Setenv("DEVELOPMENT", "false")
	t.Setenv("ROS_TAGS_SOURCE", "api")
	t.Setenv("ROS_TAGS_ALLOWED_SERVICE_ACCOUNTS", "")
	_ = config.GetConfig()

	err := tags.ValidateTagAuthConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ROS_TAGS_ALLOWED_SERVICE_ACCOUNTS")
}

func TestValidateTagAuthConfig_AllowsDevModeWarnings(t *testing.T) {
	config.ResetTagsForTest()
	t.Setenv("DEVELOPMENT", "true")
	t.Setenv("ROS_TAGS_DEV_TOKEN", "dev-only")
	t.Setenv("ROS_TAGS_SOURCE", "api")
	t.Setenv("ROS_TAGS_ALLOWED_SERVICE_ACCOUNTS", "")
	_ = config.GetConfig()

	err := tags.ValidateTagAuthConfig()
	require.NoError(t, err)
}
