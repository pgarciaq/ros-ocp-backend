package api

import (
	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
	"github.com/redhatinsights/ros-ocp-backend/internal/reship"
)

// RegisterV1RoutesForTest mounts the same v1 recommendation routes as StartAPIServer.
// Used by OpenAPI contract tests; bhTrigger may be nil (defaults to NoopTriggerer).
func RegisterV1RoutesForTest(v1 *echo.Group, bhTrigger reship.Triggerer) {
	if bhTrigger == nil {
		bhTrigger = reship.DefaultTriggerer()
	}

	nativeRecommendationRoutes := !plugin.EnabledFor(plugin.KruizePluginName)

	if nativeRecommendationRoutes {
		v1.GET("/recommendations/openshift", GetRecommendationSetListWithFallback)
		v1.GET("/recommendations/openshift/settings/terms", GetTermSettings)
		v1.PUT("/recommendations/openshift/settings/terms", PutTermSettings)
		v1.DELETE("/recommendations/openshift/settings/terms", DeleteTermSettings)
		v1.GET("/recommendations/openshift/settings/thresholds", GetThresholdSettings)
		v1.PUT("/recommendations/openshift/settings/thresholds", PutThresholdSettings)
		v1.DELETE("/recommendations/openshift/settings/thresholds", DeleteThresholdSettings)
		v1.GET("/recommendations/openshift/settings/capabilities", GetCapabilities)
		v1.GET("/recommendations/openshift/history", GetRecommendationHistory)
		v1.GET("/recommendations/openshift/quality", GetRecommendationQuality)
		v1.GET("/recommendations/openshift/fleet-summary", GetFleetSummary)
		v1.GET("/recommendations/openshift/savings-summary", GetFleetSavingsSummary)
	}

	if nativeRecommendationRoutes && businessHoursRoutesActive() {
		bhHandler := NewBusinessHoursSettingsHandler(bhTrigger)
		RegisterBusinessHoursRoutes(v1, bhHandler)
	}

	for _, ap := range plugin.APIProviders() {
		ap.RegisterRoutes(v1)
	}
	registerDisabledPluginRouteGuards(v1)

	if nativeRecommendationRoutes {
		v1.GET("/recommendations/openshift/:recommendation-id", GetRecommendationSetWithFallback)
	} else {
		v1.GET("/recommendations/openshift", GetRecommendationSetList)
		v1.GET("/recommendations/openshift/:recommendation-id", GetRecommendationSet)
	}
}
