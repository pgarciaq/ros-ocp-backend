package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTagsFeatureEnabled_DefaultTrue(t *testing.T) {
	ResetTagsForTest()
	t.Setenv("ROS_TAGS_ENABLED", "")
	cfg := GetConfig()
	assert.True(t, cfg.TagsEnabled)
	assert.True(t, TagsFeatureEnabled())
}

func TestTagsFeatureEnabled_ExplicitFalse(t *testing.T) {
	ResetTagsForTest()
	t.Setenv("ROS_TAGS_ENABLED", "false")
	cfg := GetConfig()
	assert.False(t, cfg.TagsEnabled)
	assert.False(t, TagsFeatureEnabled())
}

func TestTagsFeatureEnabled_Enabled(t *testing.T) {
	ResetTagsForTest()
	t.Setenv("ROS_TAGS_ENABLED", "true")
	t.Setenv("ROS_TAGS_SOURCE", "db")
	t.Setenv("ROS_TAGS_DEV_TOKEN", "test-token")
	t.Setenv("ROS_TAGS_ALLOWED_SERVICE_ACCOUNTS", "cost-onprem-koku-api")

	cfg := GetConfig()
	require.True(t, cfg.TagsEnabled)
	assert.Equal(t, "db", cfg.TagsSource)
	assert.Equal(t, "test-token", cfg.TagsDevToken)
	assert.Equal(t, "cost-onprem-koku-api", cfg.TagsAllowedServiceAccounts)
	assert.True(t, TagsFeatureEnabled())
	assert.False(t, TagsUsePushSync())
}

func TestDisableTagsFeature_RuntimeOverride(t *testing.T) {
	ResetTagsForTest()
	t.Setenv("ROS_TAGS_ENABLED", "true")
	require.True(t, TagsFeatureEnabled())
	DisableTagsFeature()
	assert.False(t, TagsFeatureEnabled())
}
