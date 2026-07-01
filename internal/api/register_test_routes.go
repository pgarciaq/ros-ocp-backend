package api

import (
	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
	"github.com/redhatinsights/ros-ocp-backend/internal/reship"
)

// RegisterTestInternalRoutes mounts internal API routes (tags sync/status) for contract tests.
func RegisterTestInternalRoutes(e *echo.Echo) {
	internal := e.Group("/api/cost-management/v1/internal")
	internal.POST("/tags/sync", PostTagsSync)
	internal.GET("/tags/status", GetTagsStatus)
	internal.POST("/recalculate-savings", PostRecalculateSavings)
	internal.POST("/backfill-gpu-timeslicing", PostBackfillGPUTimeslicing)
}

// RegisterTestReferenceRoutes mounts reference endpoints that do not require identity middleware.
func RegisterTestReferenceRoutes(e *echo.Echo) {
	e.GET("/api/cost-management/v1/recommendations/openshift/notification-codes", GetNotificationCodes)
}

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
		RegisterThresholdSettingsRoutes(v1)
		v1.GET("/recommendations/openshift/settings/idle-detection", GetIdleDetectionSettings)
		v1.PUT("/recommendations/openshift/settings/idle-detection", PutIdleDetectionSettings)
		v1.DELETE("/recommendations/openshift/settings/idle-detection", DeleteIdleDetectionSettings)
		if pluginRecommendationRoutesActive("quota") {
			v1.GET("/recommendations/openshift/settings/quota", GetQuotaSettings)
			v1.PUT("/recommendations/openshift/settings/quota", PutQuotaSettings)
			v1.DELETE("/recommendations/openshift/settings/quota", DeleteQuotaSettings)
		}
		if pluginRecommendationRoutesActive("cluster-quota") {
			v1.GET("/recommendations/openshift/settings/cluster-quota", GetClusterQuotaSettings)
			v1.PUT("/recommendations/openshift/settings/cluster-quota", PutClusterQuotaSettings)
			v1.DELETE("/recommendations/openshift/settings/cluster-quota", DeleteClusterQuotaSettings)
		}
		if pluginRecommendationRoutesActive("vm") {
			v1.GET("/recommendations/openshift/settings/vm", GetVMSettings)
			v1.PUT("/recommendations/openshift/settings/vm", PutVMSettings)
			v1.DELETE("/recommendations/openshift/settings/vm", DeleteVMSettings)
			v1.GET("/recommendations/openshift/settings/vm/terms", GetVMTermSettings)
			v1.PUT("/recommendations/openshift/settings/vm/terms", PutVMTermSettings)
			v1.DELETE("/recommendations/openshift/settings/vm/terms", DeleteVMTermSettings)
		}
		v1.GET("/recommendations/openshift/settings/capabilities", GetCapabilities)
		v1.GET("/recommendations/openshift/history", GetRecommendationHistory)
		v1.GET("/recommendations/openshift/quality", GetRecommendationQuality)
		v1.GET("/recommendations/openshift/quality/containers", GetRecommendationQuality)
		v1.GET("/recommendations/openshift/quality/pvcs", GetPVCRecommendationQuality)
		v1.GET("/recommendations/openshift/quality/vms", GetVMRecommendationQuality)
		v1.GET("/recommendations/openshift/quality/gpu", GetGPUMIGRecommendationQuality)
		v1.GET("/recommendations/openshift/quality/snapshots", GetSnapshotRecommendationQuality)
		v1.GET("/recommendations/openshift/fleet-summary", GetFleetSummary)
		v1.GET("/recommendations/openshift/savings-summary", GetFleetSavingsSummary)
		v1.GET("/recommendations/openshift/workload-types", GetWorkloadTypes)
	}

	if nativeRecommendationRoutes && businessHoursRoutesActive() {
		bhHandler := NewBusinessHoursSettingsHandler(bhTrigger)
		RegisterBusinessHoursRoutes(v1, bhHandler)
	}

	for _, ap := range plugin.APIProviders() {
		ap.RegisterRoutes(v1)
	}
	registerDisabledPluginRouteGuards(v1)

	if nativeRecommendationRoutes && config.VisualInsightsEnabled() {
		v1.GET("/recommendations/openshift/containers/:recommendation-id/oom-timeline", GetOOMTimeline)
		v1.GET("/recommendations/openshift/fleet-heatmap", GetFleetHeatmap)
		if pluginRecommendationRoutesActive("quota") {
			v1.GET("/recommendations/openshift/quota/:quota-id/trend", GetQuotaTrend)
		}
	}

	if nativeRecommendationRoutes {
		v1.GET("/recommendations/openshift/:recommendation-id", GetRecommendationSetWithFallback)
	} else {
		v1.GET("/recommendations/openshift", GetRecommendationSetList)
		v1.GET("/recommendations/openshift/:recommendation-id", GetRecommendationSet)
	}
}
