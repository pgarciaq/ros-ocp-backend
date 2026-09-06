package middleware

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/types"
	"github.com/redhatinsights/ros-ocp-backend/internal/utils"
	"github.com/sirupsen/logrus"
)

var cfg *config.Config = config.GetConfig()
var log *logrus.Entry = logging.GetLogger()

// rbacErrorsTotal counts RBAC upstream failures by reason. Bounded reason set,
// never tenant data. Truncation (a known capacity cap, not an outage) is
// observed here too while still serving the collected ACLs.
var rbacErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "rosocp_rbac_errors_total",
	Help: "RBAC API failures while fetching user permissions, by reason",
}, []string{"reason"})

func Rbac(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		permissions, err := get_user_permissions_from_rbac(c.Request().Header.Get("X-Rh-Identity"))
		if err != nil {
			// Generic body: never leak RBAC topology (host, URL, upstream
			// status) to callers; details stay server-side (#532).
			return echo.NewHTTPError(http.StatusServiceUnavailable, "User authorization check unavailable")
		}
		if permissions != nil {
			c.Set("user.permissions", permissions)
		} else {
			return echo.NewHTTPError(http.StatusForbidden, "User is not authorized")
		}
		return next(c)
	}
}

func appendResourcePermissions(permissions map[string][]string, key string, defs []types.RbacResourceDefinitions) {
	if _, ok := permissions[key]; !ok {
		permissions[key] = []string{}
	}
	if len(defs) == 0 {
		permissions[key] = append(permissions[key], "*")
		return
	}
	for _, resourceDefinition := range defs {
		switch t := resourceDefinition.AttributeFilter.Value.(type) {
		case []interface{}:
			for _, v := range t {
				permissions[key] = append(permissions[key], fmt.Sprint(v))
			}
		case string:
			permissions[key] = append(permissions[key], t)
		}
	}
}

// aggregate_permissions loop over all the permissions/roles/alcs of the user returned
// from rbac and creates and return the map of permissions where key is
// resourceType (openshift.cluster, openshift.node, openshift.project) and the values are the
// slice of resources (cluster names, node names, project names).
//
// Sample output from the rbac - https://github.com/RedHatInsights/ros-ocp-backend/pull/24#issuecomment-1482708944
func aggregate_permissions(acls []types.RbacData) map[string][]string {
	permissions := map[string][]string{}
	for _, acl := range acls {
		parts := strings.SplitN(acl.Permission, ":", 3)
		if len(parts) < 2 {
			log.Warnf("skipping malformed RBAC permission (no colon): %q", acl.Permission)
			continue
		}
		resourceType := parts[1]
		if strings.Contains(resourceType, "openshift") {
			appendResourcePermissions(permissions, resourceType, acl.ResourceDefinitions)
		} else if resourceType == "settings" && len(parts) >= 3 {
			operation := parts[2]
			appendResourcePermissions(permissions, resourceType+"."+operation, acl.ResourceDefinitions)
		} else if resourceType == "*" {
			permissions["*"] = []string{}
		}
	}
	return permissions
}

func get_user_permissions_from_rbac(encodedIdentity string) (map[string][]string, error) {
	cfg := config.GetConfig()
	cacheEnabled := cfg.RBACCacheTTLSecs > 0
	cacheKey := rbacIdentityCacheKey(encodedIdentity)
	if cacheEnabled {
		if perms, ok := getCachedRBACPermissions(cacheKey); ok {
			return perms, nil
		}
	}

	url := fmt.Sprintf(
		"%s://%s:%s/api/rbac/v1/access/?application=cost-management&limit=100",
		cfg.RBACProtocol, cfg.RBACHost, cfg.RBACPort,
	)
	acls, err := request_user_access(url, encodedIdentity)
	if err != nil {
		return nil, err
	}
	if len(acls) > 0 {
		permissions := aggregate_permissions(acls)
		if len(permissions) > 0 {
			if cacheEnabled {
				storeCachedRBACPermissions(cacheKey, permissions)
			}
			return permissions, nil
		}
		return nil, nil
	}
	return nil, nil
}

const maxRBACPages = 50

// request_user_access pages through the RBAC access API. Fail-closed (#532):
// any transport, status, read, unmarshal, or link anomaly returns an error and
// discards partial ACLs — a partial set must never authorize. The two
// exceptions: RBAC 4xx is RBAC speaking authoritatively about the request, so
// it denies ((nil, nil), same as empty ACLs); hitting maxRBACPages with
// Links.Next still set is a known capacity cap, served observably with a
// metric + warn rather than denied.
func request_user_access(url, encodedIdentity string) ([]types.RbacData, error) {
	access := []types.RbacData{}
	currentURL := url

	for page := 0; page < maxRBACPages; page++ {
		req, err := http.NewRequest("GET", currentURL, nil)
		if err != nil {
			rbacErrorsTotal.WithLabelValues("request_error").Inc()
			log.Errorf("unable to create RBAC request: %v", err)
			return nil, fmt.Errorf("create RBAC request: %w", err)
		}
		req.Header.Set("x-rh-identity", encodedIdentity)
		res, err := utils.HTTPClient.Do(req)
		if err != nil {
			rbacErrorsTotal.WithLabelValues("request_error").Inc()
			log.Errorf("error calling RBAC API: %v", err)
			return nil, fmt.Errorf("call RBAC API: %w", err)
		}
		body, err := io.ReadAll(res.Body)
		if closeErr := res.Body.Close(); closeErr != nil {
			log.Warnf("RBAC response body close failed: %v", closeErr)
		}
		if err != nil {
			rbacErrorsTotal.WithLabelValues("read_error").Inc()
			log.Errorf("unable to read RBAC API response body: %v", err)
			return nil, fmt.Errorf("read RBAC API response body: %w", err)
		}
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			// 4xx is a denial, not an outage: same path as empty ACLs.
			if res.StatusCode >= 400 && res.StatusCode < 500 {
				log.Warnf("RBAC API denied request: %d", res.StatusCode)
				return nil, nil
			}
			rbacErrorsTotal.WithLabelValues("bad_status").Inc()
			log.Errorf("RBAC API returned non-2xx status: %d", res.StatusCode)
			return nil, fmt.Errorf("RBAC API returned status: %d", res.StatusCode)
		}
		response := types.RbacResponse{}
		if err := json.Unmarshal(body, &response); err != nil {
			rbacErrorsTotal.WithLabelValues("unmarshal_error").Inc()
			log.Errorf("unable to unmarshal response of RBAC API %v", err)
			return nil, fmt.Errorf("unmarshal RBAC API response: %w", err)
		}
		access = append(access, response.Data...)
		if response.Links.Next == "" {
			return access, nil
		}
		if !strings.HasPrefix(response.Links.Next, "/api/rbac/") {
			rbacErrorsTotal.WithLabelValues("bad_link").Inc()
			log.Errorf("RBAC pagination link has unexpected prefix: %q; stopping", response.Links.Next)
			return nil, fmt.Errorf("RBAC pagination link has unexpected prefix")
		}
		currentURL = fmt.Sprintf("%s://%s:%s%s", cfg.RBACProtocol, cfg.RBACHost, cfg.RBACPort, response.Links.Next)
	}
	rbacErrorsTotal.WithLabelValues("truncated").Inc()
	log.Warnf("RBAC pagination truncated at %d pages; serving partial ACL set", maxRBACPages)
	return access, nil
}
