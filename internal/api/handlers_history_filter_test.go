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
