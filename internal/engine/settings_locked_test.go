package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

func TestIsSettingsLocked_GlobalOff(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_SETTINGS_LOCKED", "false")
	_ = config.GetConfig()
	assert.False(t, IsSettingsLocked("container"))
	assert.False(t, IsSettingsLocked("vm"))
}

func TestIsSettingsLocked_GlobalOn(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_SETTINGS_LOCKED", "true")
	_ = config.GetConfig()
	assert.True(t, IsSettingsLocked("container"))
	assert.True(t, IsSettingsLocked("vm"))
}

func TestIsSettingsLocked_PerFeatureOptOut(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_SETTINGS_LOCKED", "true")
	t.Setenv("ROS_SETTINGS_LOCKED_VM", "false")
	_ = config.GetConfig()
	assert.True(t, IsSettingsLocked("container"))
	assert.False(t, IsSettingsLocked("vm"))
}

func TestLockedFieldsForAPI_GlobalLock(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_SETTINGS_LOCKED", "true")
	_ = config.GetConfig()
	locked := LockedFieldsForAPI("container", []string{"min_margin"})
	assert.Equal(t, []string{AllFieldsLockedMarker}, locked)
}

func TestShouldSkipTermTenantOverrides(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_SETTINGS_LOCKED", "true")
	t.Setenv("ROS_SETTINGS_LOCKED_TERMS", "false")
	_ = config.GetConfig()
	assert.False(t, ShouldSkipTermTenantOverrides("container"))

	config.ResetForTest()
	t.Setenv("ROS_SETTINGS_LOCKED", "true")
	t.Setenv("ROS_SETTINGS_LOCKED_TERMS", "true")
	_ = config.GetConfig()
	assert.True(t, ShouldSkipTermTenantOverrides("container"))
}
