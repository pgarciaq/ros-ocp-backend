package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMachinesetNameFilterClause(t *testing.T) {
	t.Parallel()
	clause, arg, next, err := machinesetNameFilterClause("", 3)
	require.NoError(t, err)
	assert.Empty(t, clause)
	assert.Nil(t, arg)
	assert.Equal(t, 3, next)

	clause, arg, next, err = machinesetNameFilterClause("worker-a", 5)
	require.NoError(t, err)
	assert.Contains(t, clause, "= $5")
	assert.Equal(t, "worker-a", arg)
	assert.Equal(t, 6, next)

	clause, arg, next, err = machinesetNameFilterClause("worker-*", 2)
	require.NoError(t, err)
	assert.Contains(t, clause, "ILIKE")
	assert.Equal(t, "worker-%", arg)
	assert.Equal(t, 3, next)
}

func TestResolveMachineSetTerm(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/machinesets?term=short", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	term, err := resolveMachineSetTerm(c)
	require.NoError(t, err)
	assert.Equal(t, "short", term)

	req = httptest.NewRequest(http.MethodGet, "/machinesets?filter[term]=long", nil)
	c = e.NewContext(req, rec)
	term, err = resolveMachineSetTerm(c)
	require.NoError(t, err)
	assert.Equal(t, "long", term)

	req = httptest.NewRequest(http.MethodGet, "/machinesets", nil)
	c = e.NewContext(req, rec)
	term, err = resolveMachineSetTerm(c)
	require.NoError(t, err)
	assert.Equal(t, "medium", term)

	req = httptest.NewRequest(http.MethodGet, "/machinesets?term=invalid", nil)
	c = e.NewContext(req, rec)
	_, err = resolveMachineSetTerm(c)
	require.Error(t, err)
}
