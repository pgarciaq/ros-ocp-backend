package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTagsFeatureEnabled_DefaultFalse(t *testing.T) {
	ResetTagsForTest()
	cfg := GetConfig()
	assert.False(t, cfg.TagsEnabled)
	assert.False(t, TagsFeatureEnabled())
}

func TestTagsFeatureEnabled_Enabled(t *testing.T) {
	ResetTagsForTest()
	t.Setenv("ROS_TAGS_ENABLED", "true")
	t.Setenv("ROS_TAGS_INTERNAL_TOKEN", "test-token")

	cfg := GetConfig()
	require.True(t, cfg.TagsEnabled)
	assert.Equal(t, "test-token", cfg.TagsInternalToken)
	assert.True(t, TagsFeatureEnabled())
}
