package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFilterPaths_RemovesDisabledPluginPaths(t *testing.T) {
	t.Setenv("ROS_ENABLED_PLUGINS", "container,namespace")
	t.Setenv("ROS_DISABLED_PLUGINS", "")

	spec := map[string]interface{}{
		"info": map[string]interface{}{"title": "test"},
		"paths": map[string]interface{}{
			"/recommendations/openshift": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":           "container recs",
					"x-plugin-required": "container",
				},
			},
			"/recommendations/openshift/gpu": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":           "gpu recs",
					"x-plugin-required": "gpu",
				},
			},
			"/recommendations/openshift/nodes": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":           "node recs",
					"x-plugin-required": "node",
				},
			},
			"/status": map[string]interface{}{
				"get": map[string]interface{}{
					"summary": "health check",
				},
			},
		},
	}

	result := filterSpecByPlugins(spec)
	paths := result["paths"].(map[string]interface{})

	assert.Contains(t, paths, "/recommendations/openshift", "container plugin is enabled")
	assert.NotContains(t, paths, "/recommendations/openshift/gpu", "gpu plugin is not in ROS_ENABLED_PLUGINS")
	assert.NotContains(t, paths, "/recommendations/openshift/nodes", "node plugin is not in ROS_ENABLED_PLUGINS")
	assert.Contains(t, paths, "/status", "paths without x-plugin-required are always included")
}

func TestFilterPaths_AllPluginsEnabled(t *testing.T) {
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "")

	spec := map[string]interface{}{
		"paths": map[string]interface{}{
			"/recommendations/openshift/gpu": map[string]interface{}{
				"get": map[string]interface{}{
					"x-plugin-required": "gpu",
				},
			},
			"/recommendations/openshift/nodes": map[string]interface{}{
				"get": map[string]interface{}{
					"x-plugin-required": "node",
				},
			},
		},
	}

	result := filterSpecByPlugins(spec)
	paths := result["paths"].(map[string]interface{})

	assert.Contains(t, paths, "/recommendations/openshift/gpu")
	assert.Contains(t, paths, "/recommendations/openshift/nodes")
}

func TestFilterPaths_NoAnnotation_AlwaysIncluded(t *testing.T) {
	t.Setenv("ROS_ENABLED_PLUGINS", "container")
	t.Setenv("ROS_DISABLED_PLUGINS", "")

	spec := map[string]interface{}{
		"paths": map[string]interface{}{
			"/status": map[string]interface{}{
				"get": map[string]interface{}{
					"summary": "no plugin annotation",
				},
			},
		},
	}

	result := filterSpecByPlugins(spec)
	paths := result["paths"].(map[string]interface{})
	assert.Contains(t, paths, "/status")
}
