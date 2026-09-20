package plugin

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// DAG validation (#588): enabled plugins with declared requirements must
// have those requirements enabled. Fail fast, never auto-drag.
func TestValidatePluginDAG_gpuWithoutContainerErrors(t *testing.T) {
	resetRegistry(t)
	t.Setenv(envEnabledPlugins, "gpu")
	t.Setenv(envDisabledPlugins, "")

	Register(&stubPlugin{name: "gpu", requires: []string{"container"}})
	Register(&stubPlugin{name: "container"})

	err := validatePluginDAG()
	require.Error(t, err, "gpu without container must fail, not degrade silently")
	assert.Contains(t, err.Error(), "gpu")
	assert.Contains(t, err.Error(), "container")
}

func TestValidatePluginDAG_fullSetPasses(t *testing.T) {
	resetRegistry(t)
	t.Setenv(envEnabledPlugins, "container,gpu,node,quota")
	t.Setenv(envDisabledPlugins, "")

	Register(&stubPlugin{name: "container"})
	Register(&stubPlugin{name: "gpu", requires: []string{"container"}})
	Register(&stubPlugin{name: "node", requires: []string{"container"}})
	Register(&stubPlugin{name: "quota", requires: []string{"container"}})

	require.NoError(t, validatePluginDAG())
}

func TestValidatePluginDAG_disabledPluginSkipped(t *testing.T) {
	resetRegistry(t)
	t.Setenv(envEnabledPlugins, "container")
	t.Setenv(envDisabledPlugins, "")

	// gpu declares the requirement but is not enabled: nothing to enforce.
	Register(&stubPlugin{name: "gpu", requires: []string{"container"}})
	Register(&stubPlugin{name: "container"})

	require.NoError(t, validatePluginDAG())
}

func TestValidatePluginDAG_unregisteredRequirementErrors(t *testing.T) {
	resetRegistry(t)
	t.Setenv(envEnabledPlugins, "gpu")
	t.Setenv(envDisabledPlugins, "")

	// container absent entirely: the requirement names it anyway.
	Register(&stubPlugin{name: "gpu", requires: []string{"container"}})

	err := validatePluginDAG()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "container")
}

func TestValidatePluginDAG_kruizeOnlySkips(t *testing.T) {
	resetRegistry(t)
	t.Setenv(envEnabledPlugins, "kruize")
	t.Setenv(envDisabledPlugins, "")

	Register(&stubPlugin{name: "kruize"})
	Register(&stubPlugin{name: "gpu", requires: []string{"container"}})

	// kruize mode runs no native plugins: DAG validation stays out of the way
	// (kruize exclusivity is enforced separately).
	require.NoError(t, validatePluginDAG())
}
