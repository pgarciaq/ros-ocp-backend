package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newGroupByContext(query string) echo.Context {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/?"+query, nil)
	rec := httptest.NewRecorder()
	return e.NewContext(req, rec)
}

func TestParseGPUMIGListGroupBy(t *testing.T) {
	t.Run("no group_by returns empty", func(t *testing.T) {
		c := newGroupByContext("limit=10")
		cluster, project, err := parseGPUMIGListGroupBy(c)
		require.NoError(t, err)
		assert.False(t, cluster)
		assert.False(t, project)
	})

	t.Run("group_by[cluster] returns cluster=true", func(t *testing.T) {
		c := newGroupByContext("group_by%5Bcluster%5D=*")
		cluster, project, err := parseGPUMIGListGroupBy(c)
		require.NoError(t, err)
		assert.True(t, cluster)
		assert.False(t, project)
	})

	t.Run("group_by[project] returns project=true", func(t *testing.T) {
		c := newGroupByContext("group_by%5Bproject%5D=*")
		cluster, project, err := parseGPUMIGListGroupBy(c)
		require.NoError(t, err)
		assert.False(t, cluster)
		assert.True(t, project)
	})

	t.Run("group_by[namespace] maps to project", func(t *testing.T) {
		c := newGroupByContext("group_by%5Bnamespace%5D=*")
		cluster, project, err := parseGPUMIGListGroupBy(c)
		require.NoError(t, err)
		assert.False(t, cluster)
		assert.True(t, project)
	})

	t.Run("cluster and project together returns error", func(t *testing.T) {
		c := newGroupByContext("group_by%5Bcluster%5D=*&group_by%5Bproject%5D=*")
		_, _, err := parseGPUMIGListGroupBy(c)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot be used together")
	})
}

func TestParseVMListGroupBy(t *testing.T) {
	t.Run("no group_by returns empty", func(t *testing.T) {
		c := newGroupByContext("limit=10")
		field, err := parseVMListGroupBy(c)
		require.NoError(t, err)
		assert.Empty(t, field)
	})

	t.Run("group_by[cluster] returns cluster", func(t *testing.T) {
		c := newGroupByContext("group_by%5Bcluster%5D=*")
		field, err := parseVMListGroupBy(c)
		require.NoError(t, err)
		assert.Equal(t, "cluster", field)
	})

	t.Run("group_by[project] returns namespace", func(t *testing.T) {
		c := newGroupByContext("group_by%5Bproject%5D=*")
		field, err := parseVMListGroupBy(c)
		require.NoError(t, err)
		assert.Equal(t, "namespace", field)
	})

	t.Run("group_by[namespace] maps to namespace", func(t *testing.T) {
		c := newGroupByContext("group_by%5Bnamespace%5D=*")
		field, err := parseVMListGroupBy(c)
		require.NoError(t, err)
		assert.Equal(t, "namespace", field)
	})

	t.Run("cluster and project together returns error", func(t *testing.T) {
		c := newGroupByContext("group_by%5Bcluster%5D=*&group_by%5Bproject%5D=*")
		_, err := parseVMListGroupBy(c)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot be used together")
	})
}
