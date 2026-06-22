package queryparams

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveVariationOrderByKey_Defaults(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/?order_by=cpu_variation", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	resolved, ok := ResolveVariationOrderByKey(c, "cpu_variation")
	require.True(t, ok)
	assert.Equal(t, "cpu_variation_short_cost", resolved)
}

func TestResolveVariationOrderByKey_WithFilters(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/?order_by=memory_variation&filter[term]=medium_term&filter[engine]=performance", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	resolved, ok := ResolveVariationOrderByKey(c, "memory_variation")
	require.True(t, ok)
	assert.Equal(t, "memory_variation_medium_performance", resolved)
}

func TestParseOrderBy_VariationAlias(t *testing.T) {
	allowed := map[string]string{
		"cpu_variation_short_cost": "rs.cpu_variation_short_cost_pct",
	}
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/?order_by=cpu_variation&filter[term]=short_term&filter[engine]=cost", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	col, dir, err := ParseOrderBy(c, allowed, "", "desc")
	require.NoError(t, err)
	assert.Equal(t, "rs.cpu_variation_short_cost_pct", col)
	assert.Equal(t, "desc", dir)
}

func TestHasExplicitTermAndEngineFilters(t *testing.T) {
	e := echo.New()

	reqBoth := httptest.NewRequest(http.MethodGet, "/?filter[term]=short_term&filter[engine]=cost", nil)
	cBoth := e.NewContext(reqBoth, httptest.NewRecorder())
	assert.True(t, HasExplicitTermAndEngineFilters(cBoth))

	reqTermOnly := httptest.NewRequest(http.MethodGet, "/?filter[term]=short_term", nil)
	cTermOnly := e.NewContext(reqTermOnly, httptest.NewRecorder())
	assert.False(t, HasExplicitTermAndEngineFilters(cTermOnly))
}
