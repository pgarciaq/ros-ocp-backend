package api

import (
	"encoding/json"
	"net/http"
	"os"
	"sync"

	"github.com/labstack/echo/v4"
)

var (
	openapiOnce sync.Once
	openapiSpec map[string]interface{}
	openapiErr  error
)

func loadOpenAPISpec() (map[string]interface{}, error) {
	openapiOnce.Do(func() {
		data, err := os.ReadFile("openapi.json")
		if err != nil {
			openapiErr = err
			return
		}
		openapiErr = json.Unmarshal(data, &openapiSpec)
	})
	return openapiSpec, openapiErr
}

// ServeFilteredOpenAPI returns the OpenAPI spec with disabled plugin paths removed.
func ServeFilteredOpenAPI(c echo.Context) error {
	spec, err := loadOpenAPISpec()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to load openapi.json"})
	}

	filtered := filterSpecByPlugins(spec)
	return c.JSON(http.StatusOK, filtered)
}

func filterSpecByPlugins(spec map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(spec))
	for k, v := range spec {
		if k == "paths" {
			result[k] = filterPaths(v)
		} else {
			result[k] = v
		}
	}
	return result
}

func filterPaths(pathsRaw interface{}) interface{} {
	paths, ok := pathsRaw.(map[string]interface{})
	if !ok {
		return pathsRaw
	}

	filtered := make(map[string]interface{}, len(paths))
	for path, methods := range paths {
		if shouldIncludePath(methods) {
			filtered[path] = methods
		}
	}
	return filtered
}

// shouldIncludePath checks all HTTP methods in a path item; if ANY operation
// has x-plugin-required pointing to a disabled plugin, the entire path is excluded.
func shouldIncludePath(methodsRaw interface{}) bool {
	methods, ok := methodsRaw.(map[string]interface{})
	if !ok {
		return true
	}

	httpMethods := []string{"get", "post", "put", "patch", "delete", "head", "options"}
	for _, method := range httpMethods {
		op, ok := methods[method].(map[string]interface{})
		if !ok {
			continue
		}
		pluginName, ok := op["x-plugin-required"].(string)
		if !ok || pluginName == "" {
			continue
		}
		if !pluginRecommendationRoutesActive(pluginName) {
			return false
		}
	}
	return true
}
