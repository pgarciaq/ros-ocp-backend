package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

func TestLogSettingsLockedStartup_EmitsWarning(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_SETTINGS_LOCKED", "true")
	_ = config.GetConfig()

	var msgs []string
	LogSettingsLockedStartup(func(format string, args ...any) {
		msgs = append(msgs, strings.TrimSpace(fmt.Sprintf(format, args...)))
	})

	require.Len(t, msgs, 1)
	assert.Contains(t, msgs[0], "ROS_SETTINGS_LOCKED=true")
}

func TestLogSettingsLockedStartup_OptOutListsUnlockedFeatures(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_SETTINGS_LOCKED", "true")
	t.Setenv("ROS_SETTINGS_LOCKED_VM", "false")
	_ = config.GetConfig()

	var msgs []string
	LogSettingsLockedStartup(func(format string, args ...any) {
		msgs = append(msgs, fmt.Sprintf(format, args...))
	})

	require.Len(t, msgs, 2)
	assert.Contains(t, msgs[1], "vm")
}

func TestLogSettingsLockedStartup_NoOpWhenUnlocked(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_SETTINGS_LOCKED", "false")
	_ = config.GetConfig()

	var msgs []string
	LogSettingsLockedStartup(func(format string, args ...any) {
		msgs = append(msgs, format)
	})
	assert.Empty(t, msgs)
}
