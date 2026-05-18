package node

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
)

func TestNodePlugin_traitAssertions(t *testing.T) {
	t.Parallel()

	var (
		_ plugin.Plugin            = (*NodePlugin)(nil)
		_ plugin.IngestHook        = (*NodePlugin)(nil)
		_ plugin.APIProvider       = (*NodePlugin)(nil)
		_ plugin.RetentionProvider = (*NodePlugin)(nil)
	)
}

func TestNodePlugin_hookAfterTypes(t *testing.T) {
	t.Parallel()

	p := &NodePlugin{}
	assert.Equal(t, []string{"container"}, p.HookAfterCSVTypes())
	assert.Equal(t, []string{"daily_node_digests", "node_recommendations"}, p.RetentionTables())
}
