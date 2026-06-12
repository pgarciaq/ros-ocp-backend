package api

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/sirupsen/logrus"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/tags"
)

var internalEndpointCallsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "rosocp_internal_endpoint_calls_total",
		Help: "Internal platform endpoint invocations for cross-tenant audit and anomaly detection",
	},
	[]string{"endpoint", "sa_name"},
)

// Internal endpoints authenticate platform service accounts via TokenReview but do not
// bind org_id to the caller's tenant scope. This is intentional: Koku/Masu invoke ROS
// on behalf of arbitrary tenants using cluster-internal credentials restricted by
// NetworkPolicy and ROS_TAGS_ALLOWED_SERVICE_ACCOUNTS. Structured audit logging,
// rosocp_internal_endpoint_calls_total, and optional ROS_INTERNAL_ALLOWED_ORGS provide
// defense-in-depth.

func authenticateInternalCaller(c echo.Context) (saName string, httpErr *echo.HTTPError) {
	if !config.InternalTagsAuthRequired() {
		return "auth-disabled", nil
	}
	bearerToken := tags.BearerTokenFromHeader(c.Request().Header.Get(echo.HeaderAuthorization))
	sa, err := tags.AuthenticateInternalCaller(c.Request().Context(), bearerToken)
	if err != nil {
		return "", echo.NewHTTPError(http.StatusUnauthorized, "invalid or missing service account token")
	}
	return sa, nil
}

func validateInternalOrgTarget(orgID string) *echo.HTTPError {
	allowed := strings.TrimSpace(config.GetConfig().InternalAllowedOrgs)
	if allowed == "" {
		return nil
	}
	orgID = strings.TrimSpace(orgID)
	for _, candidate := range strings.Split(allowed, ",") {
		if strings.TrimSpace(candidate) == orgID {
			return nil
		}
	}
	return echo.NewHTTPError(http.StatusForbidden, "org_id is not permitted for internal endpoints")
}

func auditInternalEndpoint(c echo.Context, endpoint, orgID, saName, action string) {
	log := requestLogger(c, orgID).WithFields(logrus.Fields{
		"internal_endpoint": endpoint,
		"target_org_id":     orgID,
		"caller_sa":         saName,
		"action":            action,
	})
	log.Info("internal endpoint call")
	internalEndpointCallsTotal.WithLabelValues(endpoint, saName).Inc()
}
