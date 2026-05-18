package node

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
)

func TestNodePlugin_traitAssertions(t *testing.T) {
	t.Parallel()

	var (
		_ plugin.Plugin     = (*NodePlugin)(nil)
		_ plugin.IngestHook = (*NodePlugin)(nil)
	)
}

func TestNodePlugin_hookAfterTypes(t *testing.T) {
	t.Parallel()

	p := &NodePlugin{}
	assert.Equal(t, []string{"container"}, p.HookAfterCSVTypes())
}
