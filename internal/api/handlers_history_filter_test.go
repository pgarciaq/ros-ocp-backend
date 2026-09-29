package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

func TestMapHistoryQueryParameters_RejectsExcessiveFilters(t *testing.T) {
	config.ResetForTest()
	t.Setenv("MAXIMUM_COUNT_PER_QUERY_PARAM", "2")
	t.Cleanup(func() {
		config.ResetForTest()
		_ = config.GetConfig()
	})
	_ = config.GetConfig()

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/?filter[project]=one&filter[project]=two&filter[project]=three", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	_, err := MapHistoryQueryParameters(c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too many project parameters")
}

// TestMapHistoryQueryParameters_HostedClusterID maps the HC filter to the
// frozen-ID column with the same cardinality cap as sibling filters (#638).
func TestMapHistoryQueryParameters_HostedClusterID(t *testing.T) {
	config.ResetForTest()
	t.Cleanup(func() {
		config.ResetForTest()
		_ = config.GetConfig()
	})
	_ = config.GetConfig()

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/?filter[hosted_cluster_id]=aaa-111", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	params, err := MapHistoryQueryParameters(c)
	require.NoError(t, err)
	assert.Equal(t, []string{"aaa-111"}, params["h.hosted_cluster_id IN ?"])
}
