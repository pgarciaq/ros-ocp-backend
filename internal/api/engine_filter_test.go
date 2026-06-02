package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveQualityEngineFilter_DefaultCost(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/quality", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	engine, err := resolveQualityEngineFilter(c)
	require.NoError(t, err)
	assert.Equal(t, "cost", engine)
}

func TestResolveQualityEngineFilter_Performance(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/quality?filter%5Bengine%5D=performance", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	engine, err := resolveQualityEngineFilter(c)
	require.NoError(t, err)
	assert.Equal(t, "performance", engine)
}

func TestCollectEngineFilterValues_Invalid(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/?filter%5Bengine%5D=invalid", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	_, err := collectEngineFilterValues(c)
	require.Error(t, err)
}

func TestApplyNativeEngineQueryFilter(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/?filter%5Bengine%5D=cost", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	params := map[string]interface{}{}
	err := applyNativeEngineQueryFilter(c, params, "rs.engine")
	require.NoError(t, err)
	assert.Equal(t, []string{"cost"}, params["rs.engine IN ?"])
}
