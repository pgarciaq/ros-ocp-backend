package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	promtest "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

func TestValidateInternalOrgTarget_AllowsWhenUnset(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_INTERNAL_ALLOWED_ORGS", "")

	err := validateInternalOrgTarget("1234567")
	assert.Nil(t, err)
}

func TestValidateInternalOrgTarget_RejectsDisallowedOrg(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_INTERNAL_ALLOWED_ORGS", "1234567,7654321")

	err := validateInternalOrgTarget("9999999")
	require.NotNil(t, err)
	assert.Equal(t, http.StatusForbidden, err.Code)
}

func TestAuditInternalEndpoint_IncrementsMetric(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/internal/tags/status?org_id=1234567", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	before := promtest.ToFloat64(internalEndpointCallsTotal.WithLabelValues(
		"GET /internal/tags/status", "1234567", "test-sa",
	))
	auditInternalEndpoint(c, "GET /internal/tags/status", "1234567", "test-sa", "read_tag_status")
	after := promtest.ToFloat64(internalEndpointCallsTotal.WithLabelValues(
		"GET /internal/tags/status", "1234567", "test-sa",
	))
	assert.InDelta(t, 1, after-before, 0)
}
