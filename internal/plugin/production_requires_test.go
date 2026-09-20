package plugin_test

import (
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugins/gpu"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugins/node"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugins/quota"
	"github.com/stretchr/testify/assert"
)

// Production plugins must declare the dependencies the docs promise (#588).
// The DAG unit tests use stubs; without this pin, deleting a Requires
// method would keep every suite green while real deployments lose the guard.
func TestProductionPluginRequires(t *testing.T) {
	cases := []struct {
		name string
		p    plugin.DependencyDeclarer
		want []string
	}{
		{"gpu requires container", &gpu.GPUPlugin{}, []string{"container"}},
		{"node requires container", &node.NodePlugin{}, []string{"container"}},
		{"quota requires container", &quota.QuotaPlugin{}, []string{"container"}},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, tc.p.Requires(), tc.name)
	}
}
