package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestFilterPaths_RemovesDisabledPluginPaths(t *testing.T) {
	t.Setenv("ROS_ENABLED_PLUGINS", "container,namespace")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	config.ResetForTest()
	_ = config.GetConfig()

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
	config.ResetForTest()
	_ = config.GetConfig()

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
	config.ResetForTest()
	_ = config.GetConfig()

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

func TestOpenAPI_BusinessHoursPathsFiltered(t *testing.T) {
	t.Setenv("ROS_BUSINESS_HOURS_ENABLED", "false")
	t.Setenv("ROS_ENABLED_PLUGINS", "container")
	config.ResetForTest()
	_ = config.GetConfig()

	spec := map[string]interface{}{
		"paths": map[string]interface{}{
			"/recommendations/openshift/settings/business-hours": map[string]interface{}{
				"get": map[string]interface{}{
					"x-plugin-required": "business-hours",
				},
				"put": map[string]interface{}{
					"x-plugin-required": "business-hours",
				},
			},
			"/recommendations/openshift": map[string]interface{}{
				"get": map[string]interface{}{
					"x-plugin-required": "container",
				},
			},
		},
	}

	result := filterSpecByPlugins(spec)
	paths := result["paths"].(map[string]interface{})

	assert.NotContains(t, paths, "/recommendations/openshift/settings/business-hours")
	assert.Contains(t, paths, "/recommendations/openshift")
}

func TestOpenAPI_BusinessHoursPathsIncludedWhenEnabled(t *testing.T) {
	t.Setenv("ROS_BUSINESS_HOURS_ENABLED", "true")
	t.Setenv("ROS_ENABLED_PLUGINS", "container")
	config.ResetForTest()
	_ = config.GetConfig()

	spec := map[string]interface{}{
		"paths": map[string]interface{}{
			"/recommendations/openshift/settings/business-hours": map[string]interface{}{
				"get": map[string]interface{}{
					"x-plugin-required": "business-hours",
				},
			},
		},
	}

	result := filterSpecByPlugins(spec)
	paths := result["paths"].(map[string]interface{})
	assert.Contains(t, paths, "/recommendations/openshift/settings/business-hours")
}

// Cold-start edge (#536): a corrupt openapi.json must surface as an error,
// not a half-populated spec. The cached loader cannot be re-driven with bad
// input (sync.Once), hence the pure parse function.
func TestParseOpenAPISpec_CorruptJSONReturnsError(t *testing.T) {
	spec, err := parseOpenAPISpec([]byte(`{"openapi": "3.0.0", "paths": {`))
	assert.Error(t, err)
	assert.Nil(t, spec)
}

func TestParseOpenAPISpec_ValidJSONDecodes(t *testing.T) {
	spec, err := parseOpenAPISpec([]byte(`{"openapi": "3.0.0", "paths": {"/x": {}}}`))
	assert.NoError(t, err)
	assert.Equal(t, "3.0.0", spec["openapi"])
	assert.Contains(t, spec, "paths")
}

// poisonOpenAPISpecLoader swaps the file reader and resets the Once cache so
// a test drives loadOpenAPISpec with controlled bytes. Restores everything:
// a poisoned cache would pollute the whole package.
func poisonOpenAPISpecLoader(t *testing.T, read func(string) ([]byte, error)) {
	t.Helper()
	origRead := openapiReadFile
	origSpec, origErr := openapiSpec, openapiErr
	openapiReadFile = read
	openapiOnce = sync.Once{}
	openapiSpec, openapiErr = nil, nil
	t.Cleanup(func() {
		openapiReadFile = origRead
		openapiSpec, openapiErr = origSpec, origErr
		openapiOnce = sync.Once{}
	})
}

// Unreadable openapi.json at boot must surface as an error, not an empty spec (#564).
func TestLoadOpenAPISpec_ReadErrorSurfaces(t *testing.T) {
	poisonOpenAPISpecLoader(t, func(string) ([]byte, error) {
		return nil, errors.New("permission denied")
	})
	spec, err := loadOpenAPISpec()
	require.Error(t, err)
	assert.Nil(t, spec)
}

// Corrupt bytes via the loader take the same parse path as the pure
// function, proving the wired path — not just the extracted helper (#564).
func TestLoadOpenAPISpec_CorruptBytesSurfaceParseError(t *testing.T) {
	poisonOpenAPISpecLoader(t, func(string) ([]byte, error) {
		return []byte(`{"openapi": "3.0.0", "paths": {`), nil
	})
	spec, err := loadOpenAPISpec()
	require.Error(t, err)
	assert.Nil(t, spec)
}

// A poisoned loader must 500 through the handler, not serve an empty 200 (#564).
func TestServeFilteredOpenAPI_LoaderErrorIs500(t *testing.T) {
	poisonOpenAPISpecLoader(t, func(string) ([]byte, error) {
		return nil, errors.New("disk gone")
	})
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/openapi.json", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	require.NoError(t, ServeFilteredOpenAPI(c))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "failed to load")
}
