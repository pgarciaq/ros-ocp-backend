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
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

var log *logrus.Entry = logging.GetLogger()
var cfg *config.Config = config.GetConfig()

// StartAPIServer runs the REST API and Prometheus metrics listener until ctx is cancelled,
// then shuts both down gracefully.
func StartAPIServer(ctx context.Context) {
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

	app.Use(middleware.RequestLogger())
	app.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowMethods: []string{http.MethodGet, http.MethodPut, http.MethodDelete},
	}))

	app.GET("/status", GetAppStatus)
	app.GET("/readyz", GetReadyz)
	app.File("/api/cost-management/v1/recommendations/openshift/openapi.json", "openapi.json")

	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	if cfg.RBACEnabled {
		v1.Use(ros_middleware.Rbac)
	}

	// Container recommendations — native engine with Kruize fallback, or legacy-only.
	if cfg.UseNativeEngine {
		// Static /gpu path must register before /:recommendation-id so "gpu" is not captured as an ID.
		v1.GET("/recommendations/openshift/gpu", GetGPUSummary)
		v1.GET("/recommendations/openshift", GetRecommendationSetListWithFallback)
		v1.GET("/recommendations/openshift/:recommendation-id", GetRecommendationSetWithFallback)
	} else {
		v1.GET("/recommendations/openshift", GetRecommendationSetList)
		v1.GET("/recommendations/openshift/:recommendation-id", GetRecommendationSet)
	}

	// Project/Namespace — native engine with Kruize fallback, or legacy-only.
	// Canonical paths (consistent with nodes, pvcs, snapshots pattern):
	//   GET /recommendations/openshift/namespaces
	//   GET /recommendations/openshift/namespaces/:recommendation-id
	// Legacy paths (preserved for backward compatibility with IQE, OpenAPI spec):
	//   GET /openshift/namespace/recommendations
	//   GET /recommendations/openshift/namespace/:recommendation-id
	if cfg.UseNativeEngine {
		v1.GET("/recommendations/openshift/namespaces", GetNamespaceRecommendationSetListWithFallback)
		v1.GET("/recommendations/openshift/namespaces/:recommendation-id", GetNamespaceRecommendationSetWithFallback)
		v1.GET("/openshift/namespace/recommendations", GetNamespaceRecommendationSetListWithFallback)
		v1.GET("/recommendations/openshift/namespace/:recommendation-id", GetNamespaceRecommendationSetWithFallback)
	} else {
		v1.GET("/recommendations/openshift/namespaces", GetNamespaceRecommendationSetList)
		v1.GET("/recommendations/openshift/namespaces/:recommendation-id", GetNamespaceRecommendationSet)
		v1.GET("/openshift/namespace/recommendations", GetNamespaceRecommendationSetList)
		v1.GET("/recommendations/openshift/namespace/:recommendation-id", GetNamespaceRecommendationSet)
	}

	// Custom recommendation term settings (native engine only).
	if cfg.UseNativeEngine {
		v1.GET("/recommendations/openshift/settings/terms", GetTermSettings)
		v1.PUT("/recommendations/openshift/settings/terms", PutTermSettings)
		v1.DELETE("/recommendations/openshift/settings/terms", DeleteTermSettings)
	}

	// Historical tracking and quality metrics (native engine only).
	if cfg.UseNativeEngine {
		v1.GET("/recommendations/openshift/history", GetRecommendationHistory)
		v1.GET("/recommendations/openshift/quality", GetRecommendationQuality)
	}

	// Node-level GPU time-slicing and MIG-focused listings (native engine only).
	if cfg.UseNativeEngine {
		v1.GET("/recommendations/openshift/gpu/timeslicing", GetNodeRecommendations)
		v1.GET("/recommendations/openshift/gpu/mig", GetGPUMIGRecommendations)
		v1.GET("/recommendations/openshift/nodes", GetNodeUtilizationRecs)
		v1.GET("/recommendations/openshift/nodes/utilization", GetNodeUtilizationRecsLegacyPath)
	}

	// Fleet-level summary (native engine only).
	if cfg.UseNativeEngine {
		v1.GET("/recommendations/openshift/fleet-summary", GetFleetSummary)
	}

	// PVC right-sizing recommendations (native engine only).
	if cfg.UseNativeEngine {
		v1.GET("/recommendations/openshift/pvcs", GetPVCRecommendations)
	}

	// Snapshot staleness recommendations (native engine only).
	if cfg.UseNativeEngine {
		v1.GET("/recommendations/openshift/snapshots", GetSnapshotRecommendations)
		v1.GET("/recommendations/openshift/settings/snapshot", GetSnapshotSettings)
		v1.PUT("/recommendations/openshift/settings/snapshot", PutSnapshotSettings)
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
