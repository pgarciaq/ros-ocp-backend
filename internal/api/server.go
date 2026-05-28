package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo-contrib/echoprometheus"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/sirupsen/logrus"

	ros_middleware "github.com/redhatinsights/ros-ocp-backend/internal/api/middleware"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
	"github.com/redhatinsights/ros-ocp-backend/internal/reship"
)

var log *logrus.Entry = logging.GetLogger()
var cfg *config.Config = config.GetConfig()

func registerRecommendationRoutes(v1 *echo.Group) {
	// Legacy container routes (retained for backward compatibility)
	v1.GET("/recommendations/openshift", GetRecommendationSetList)
	v1.GET("/recommendations/openshift/:recommendation-id", GetRecommendationSet)

	// New container routes
	v1.GET("/recommendations/openshift/container", GetRecommendationSetList)
	v1.GET("/recommendations/openshift/container/:recommendation-id", GetRecommendationSet)

	// Project/Namespace
	v1.GET("/recommendations/openshift/namespace", GetNamespaceRecommendationSetList)
	v1.GET("/recommendations/openshift/namespace/:recommendation-id", GetNamespaceRecommendationSet)
}

func StartAPIServer() {
const businessHoursPluginName = "business-hours"

// pluginRecommendationRoutesActive reports whether gpu/node/pvc/snapshot/quota HTTP routes are registered:
// those plugins omit routes entirely when Kruize owns recommendations or when the plugin is disabled.
// The business-hours feature uses x-plugin-required "business-hours" and ROS_BUSINESS_HOURS_ENABLED.
func pluginRecommendationRoutesActive(pluginName string) bool {
	if plugin.EnabledFor(plugin.KruizePluginName) {
		return false
	}
	if pluginName == businessHoursPluginName {
		return config.BusinessHoursFeatureEnabled()
	}
	return plugin.EnabledFor(pluginName)
}

// businessHoursRoutesActive reports whether business-hours settings routes should be registered.
func businessHoursRoutesActive() bool {
	return pluginRecommendationRoutesActive(businessHoursPluginName)
}

func disabledPluginRoute404(pluginName string) echo.HandlerFunc {
	return func(c echo.Context) error {
		return c.JSON(http.StatusNotFound, echo.Map{
			"status":  "not_found",
			"message": fmt.Sprintf("plugin '%s' is not enabled", pluginName),
		})
	}
}

// registerDisabledPluginRouteGuards installs 404 handlers for plugin URL prefixes when the plugin
// does not register real routes, so requests do not fall through to /recommendations/openshift/:recommendation-id
// (which treats e.g. "gpu" as a UUID and returns 400).
func registerDisabledPluginRouteGuards(v1 *echo.Group) {
	if !pluginRecommendationRoutesActive("gpu") {
		v1.GET("/recommendations/openshift/gpu", disabledPluginRoute404("gpu"))
		v1.GET("/recommendations/openshift/gpu/*", disabledPluginRoute404("gpu"))
	}
	if !pluginRecommendationRoutesActive("node") {
		v1.GET("/recommendations/openshift/nodes", disabledPluginRoute404("node"))
		v1.GET("/recommendations/openshift/nodes/*", disabledPluginRoute404("node"))
	}
	if !pluginRecommendationRoutesActive("pvc") {
		v1.GET("/recommendations/openshift/pvcs", disabledPluginRoute404("pvc"))
	}
	if !pluginRecommendationRoutesActive("snapshot") {
		v1.GET("/recommendations/openshift/snapshots", disabledPluginRoute404("snapshot"))
		v1.GET("/recommendations/openshift/settings/snapshot", disabledPluginRoute404("snapshot"))
		v1.PUT("/recommendations/openshift/settings/snapshot", disabledPluginRoute404("snapshot"))
	}
	if !pluginRecommendationRoutesActive("quota") {
		v1.GET("/recommendations/openshift/quota", disabledPluginRoute404("quota"))
		v1.GET("/recommendations/openshift/settings/quota", disabledPluginRoute404("quota"))
		v1.PUT("/recommendations/openshift/settings/quota", disabledPluginRoute404("quota"))
		v1.DELETE("/recommendations/openshift/settings/quota", disabledPluginRoute404("quota"))
	}
	registerBusinessHoursRouteGuards(v1)
}

// registerBusinessHoursRouteGuards installs 404 handlers for business-hours settings paths when
// ROS_BUSINESS_HOURS_ENABLED is false (or the native engine is off). Real handlers register in
// Phase 3 when the feature is enabled.
func registerBusinessHoursRouteGuards(v1 *echo.Group) {
	if businessHoursRoutesActive() {
		return
	}
	bh404 := disabledPluginRoute404(businessHoursPluginName)
	v1.GET("/recommendations/openshift/settings/business-hours", bh404)
	v1.PUT("/recommendations/openshift/settings/business-hours", bh404)
	v1.DELETE("/recommendations/openshift/settings/business-hours", bh404)
	v1.GET("/recommendations/openshift/settings/business-hours/clusters/:cluster_id", bh404)
	v1.PUT("/recommendations/openshift/settings/business-hours/clusters/:cluster_id", bh404)
	v1.DELETE("/recommendations/openshift/settings/business-hours/clusters/:cluster_id", bh404)
	v1.GET("/recommendations/openshift/settings/business-hours/clusters/:cluster_id/namespaces/:namespace", bh404)
	v1.PUT("/recommendations/openshift/settings/business-hours/clusters/:cluster_id/namespaces/:namespace", bh404)
	v1.DELETE("/recommendations/openshift/settings/business-hours/clusters/:cluster_id/namespaces/:namespace", bh404)
}

// StartAPIServer runs the REST API and Prometheus metrics listener until ctx is cancelled,
// then shuts both down gracefully.
func StartAPIServer(ctx context.Context) {
	// JSON encoding uses Echo's default encoding/json serializer. Benchmarks on
	// ~10–50KB list payloads showed <10% gain from jsoniter/sonic vs added
	// dependency and compatibility risk — see internal/api/json_bench_test.go.
	app := echo.New()
	app.Use(echoprometheus.NewMiddlewareWithConfig(echoprometheus.MiddlewareConfig{
		Subsystem: "rosocp",
		LabelFuncs: map[string]echoprometheus.LabelValueFunc{
			"url": func(c echo.Context, err error) string {
				return c.Path()
			},
		},
	}))

	metricsEcho := echo.New()
	metricsEcho.GET("/metrics", echoprometheus.NewHandler())
	metricsErrCh := make(chan error, 1)
	go func() {
		addr := fmt.Sprintf(":%s", cfg.PrometheusPort)
		if err := metricsEcho.Start(addr); err != nil && !errors.Is(err, http.ErrServerClosed) {
			metricsErrCh <- err
		}
	}()

	app.Pre(middleware.RemoveTrailingSlash())
	app.Use(middleware.GzipWithConfig(middleware.GzipConfig{
		Level:     5,
		MinLength: 1024,
	}))
	app.Use(middleware.RequestID())
	app.Use(middleware.RequestLogger())
	app.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowMethods: []string{http.MethodGet, http.MethodPut, http.MethodDelete},
	}))

	app.GET("/status", GetAppStatus)
	app.GET("/readyz", GetReadyz)
	app.GET("/api/cost-management/v1/recommendations/openshift/openapi.json", ServeFilteredOpenAPI)

	// Internal routes (no identity/RBAC middleware). Tag sync is gated by ROS_TAGS_ENABLED in handler.
	internal := app.Group("/api/cost-management/v1/internal")
	internal.Use(middleware.BodyLimit(cfg.TagsSyncBodyLimit()))
	internal.POST("/tags/sync", PostTagsSync)
	internal.GET("/tags/status", GetTagsStatus)

	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	if cfg.RBACEnabled {
		v1.Use(ros_middleware.Rbac)
	}

	registerRecommendationRoutes(v1)
	// Container
	nativeRecommendationRoutes := !plugin.EnabledFor(plugin.KruizePluginName)

	// Container recommendations (list + detail) stay in this package rather than a plugin APIProvider:
	// they pair native handlers with Kruize-specific fallbacks, and Echo requires registering this
	// parameterized `/.../:recommendation-id` route after static paths and plugin routes (see below).

	// Container recommendations — native engine with Kruize fallback, or legacy-only.
	if nativeRecommendationRoutes {
		v1.GET("/recommendations/openshift", GetRecommendationSetListWithFallback)
	} else {
		v1.GET("/recommendations/openshift", GetRecommendationSetList)
	}

	// Namespace routes are registered by namespace.APIProvider (see plugin.APIProviders).

	// Custom recommendation term settings (native engine only).
	if nativeRecommendationRoutes {
		v1.GET("/recommendations/openshift/settings/terms", GetTermSettings)
		v1.PUT("/recommendations/openshift/settings/terms", PutTermSettings)
		v1.DELETE("/recommendations/openshift/settings/terms", DeleteTermSettings)
		v1.GET("/recommendations/openshift/settings/thresholds", GetThresholdSettings)
		v1.PUT("/recommendations/openshift/settings/thresholds", PutThresholdSettings)
		v1.DELETE("/recommendations/openshift/settings/thresholds", DeleteThresholdSettings)
		v1.GET("/recommendations/openshift/settings/idle-detection", GetIdleDetectionSettings)
		v1.PUT("/recommendations/openshift/settings/idle-detection", PutIdleDetectionSettings)
		v1.DELETE("/recommendations/openshift/settings/idle-detection", DeleteIdleDetectionSettings)
		if pluginRecommendationRoutesActive("quota") {
			v1.GET("/recommendations/openshift/settings/quota", GetQuotaSettings)
			v1.PUT("/recommendations/openshift/settings/quota", PutQuotaSettings)
			v1.DELETE("/recommendations/openshift/settings/quota", DeleteQuotaSettings)
		}
		v1.GET("/recommendations/openshift/settings/capabilities", GetCapabilities)
	}

	if nativeRecommendationRoutes && businessHoursRoutesActive() {
		bhHandler := NewBusinessHoursSettingsHandler(reship.DefaultTriggerer())
		RegisterBusinessHoursRoutes(v1, bhHandler)
		reship.StartPoller(ctx, db.GetPool(), cfg)
	}

	// Historical tracking and quality metrics (native engine only).
	if nativeRecommendationRoutes {
		v1.GET("/recommendations/openshift/history", GetRecommendationHistory)
		v1.GET("/recommendations/openshift/quality", GetRecommendationQuality)
	}

	// GPU / node utilization routes are registered by gpu/node APIProvider plugins.

	// Fleet-level summary (native engine only).
	if nativeRecommendationRoutes {
		v1.GET("/recommendations/openshift/fleet-summary", GetFleetSummary)
		v1.GET("/recommendations/openshift/savings-summary", GetFleetSavingsSummary)
	}

	// Plugin-provided routes ([plugin.APIProviders] returns individually enabled plugins,
	// without Kruize mutual exclusivity, so namespace endpoints stay available in Kruize mode).
	for _, ap := range plugin.APIProviders() {
		ap.RegisterRoutes(v1)
	}

	registerDisabledPluginRouteGuards(v1)

	// Parameterized container recommendation detail — after static paths and plugin routes (ordering; see note above).
	if nativeRecommendationRoutes {
		v1.GET("/recommendations/openshift/:recommendation-id", GetRecommendationSetWithFallback)
	} else {
		v1.GET("/recommendations/openshift/:recommendation-id", GetRecommendationSet)
	}

	srv := &http.Server{
		Addr:              ":" + cfg.API_PORT,
		Handler:           app,
		ReadHeaderTimeout: time.Duration(cfg.ReadHeaderTimeout) * time.Second,
	}

	apiErrCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			apiErrCh <- err
		}
	}()

	select {
	case err := <-apiErrCh:
		log.Fatalf("api server failed to start: %v", err)
	case err := <-metricsErrCh:
		log.Fatalf("metrics server failed to start: %v", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := metricsEcho.Shutdown(shutdownCtx); err != nil {
		log.Warnf("metrics server shutdown: %v", err)
	}
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Warnf("api server shutdown: %v", err)
	}
}
