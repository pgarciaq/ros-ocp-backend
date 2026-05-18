package kruize

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
)

func TestKruizePlugin_traitAssertions(t *testing.T) {
	t.Parallel()

	var _ plugin.Plugin = (*KruizePlugin)(nil)
}

func TestKruizePlugin_name(t *testing.T) {
	t.Parallel()

	p := &KruizePlugin{}
	assert.Equal(t, "kruize", p.Name())
}

func TestKruizePlugin_enabled_defaultFalse(t *testing.T) {
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "")

	p := &KruizePlugin{}
	assert.False(t, p.Enabled())
}

func TestKruizePlugin_enabled_whenAllowlisted(t *testing.T) {
	t.Setenv("ROS_ENABLED_PLUGINS", "kruize")
	t.Setenv("ROS_DISABLED_PLUGINS", "")

	p := &KruizePlugin{}
	assert.True(t, p.Enabled())
}
