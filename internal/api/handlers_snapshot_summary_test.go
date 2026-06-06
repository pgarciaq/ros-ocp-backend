package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSnapshotNamespaceFilterClause(t *testing.T) {
	t.Parallel()
	clause, arg, next, err := snapshotNamespaceFilterClause("", 3)
	require.NoError(t, err)
	assert.Empty(t, clause)
	assert.Nil(t, arg)
	assert.Equal(t, 3, next)

	clause, arg, next, err = snapshotNamespaceFilterClause("payments", 5)
	require.NoError(t, err)
	assert.Contains(t, clause, "= $5")
	assert.Equal(t, "payments", arg)
	assert.Equal(t, 6, next)

	clause, arg, next, err = snapshotNamespaceFilterClause("pay-*", 2)
	require.NoError(t, err)
	assert.Contains(t, clause, "ILIKE")
	assert.Equal(t, "pay-%", arg)
	assert.Equal(t, 3, next)
}

func TestResolveSnapshotSummaryGroupBy(t *testing.T) {
	t.Parallel()
	e := echo.New()

	req := httptest.NewRequest(http.MethodGet, "/snapshots/summary", nil)
	c := e.NewContext(req, httptest.NewRecorder())
	groupBy, err := resolveSnapshotSummaryGroupBy(c)
	require.NoError(t, err)
	assert.Equal(t, snapshotSummaryGroupProject, groupBy)

	req = httptest.NewRequest(http.MethodGet, "/snapshots/summary?group_by=cluster", nil)
	c = e.NewContext(req, httptest.NewRecorder())
	groupBy, err = resolveSnapshotSummaryGroupBy(c)
	require.NoError(t, err)
	assert.Equal(t, snapshotSummaryGroupCluster, groupBy)

	req = httptest.NewRequest(http.MethodGet, "/snapshots/summary?group_by%5Bproject%5D=*", nil)
	c = e.NewContext(req, httptest.NewRecorder())
	groupBy, err = resolveSnapshotSummaryGroupBy(c)
	require.NoError(t, err)
	assert.Equal(t, snapshotSummaryGroupProject, groupBy)

	req = httptest.NewRequest(http.MethodGet, "/snapshots/summary?group_by=namespace", nil)
	c = e.NewContext(req, httptest.NewRecorder())
	groupBy, err = resolveSnapshotSummaryGroupBy(c)
	require.NoError(t, err)
	assert.Equal(t, snapshotSummaryGroupProject, groupBy)

	req = httptest.NewRequest(http.MethodGet, "/snapshots/summary?group_by=invalid", nil)
	c = e.NewContext(req, httptest.NewRecorder())
	_, err = resolveSnapshotSummaryGroupBy(c)
	require.Error(t, err)
}

func TestSnapshotSummaryGroupBySQL(t *testing.T) {
	t.Parallel()
	groupSQL, selectNS := snapshotSummaryGroupBySQL(snapshotSummaryGroupProject)
	assert.Contains(t, groupSQL, "namespace")
	assert.Contains(t, selectNS, "namespace,")

	groupSQL, selectNS = snapshotSummaryGroupBySQL(snapshotSummaryGroupCluster)
	assert.Contains(t, groupSQL, "cluster_uuid")
	assert.Contains(t, selectNS, "'' AS namespace")
}

func TestBytesToGiB(t *testing.T) {
	t.Parallel()
	assert.Equal(t, float64(0), bytesToGiB(0))
	assert.InDelta(t, 1.0, bytesToGiB(snapshotSummaryGiBDivisor), 1e-9)
	assert.InDelta(t, 2.5, bytesToGiB(int64(2.5*float64(snapshotSummaryGiBDivisor))), 1e-6)
}
