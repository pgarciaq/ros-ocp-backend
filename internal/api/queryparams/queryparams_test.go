package queryparams

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newEchoContext(rawQuery string) echo.Context {
	e := echo.New()
	target := "/"
	if rawQuery != "" {
		target = "/?" + rawQuery
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	return e.NewContext(req, rec)
}

func TestSplitCommaValues(t *testing.T) {
	assert.Nil(t, SplitCommaValues(nil))
	assert.Equal(t, []string{"a", "b", "c"}, SplitCommaValues([]string{"a,b", "c"}))
}

func TestIncludeValues(t *testing.T) {
	t.Run("flat keys are merged", func(t *testing.T) {
		c := newEchoContext("project=alpha&project=beta")
		assert.Equal(t, []string{"alpha", "beta"}, IncludeValues(c, "project"))
	})

	t.Run("flat namespace alias for project", func(t *testing.T) {
		c := newEchoContext("namespace=payments")
		assert.Equal(t, []string{"payments"}, IncludeValues(c, "project"))
	})

	t.Run("bracket filter namespace alias for project", func(t *testing.T) {
		c := newEchoContext("filter%5Bnamespace%5D=payments")
		assert.Equal(t, []string{"payments"}, IncludeValues(c, "project"))
	})

	t.Run("bracket filter project preferred over namespace alias", func(t *testing.T) {
		c := newEchoContext("filter%5Bproject%5D=alpha&filter%5Bnamespace%5D=beta")
		assert.Equal(t, []string{"alpha", "beta"}, IncludeValues(c, "project"))
		assert.Equal(t, "alpha", FirstFilter(c, "project"))
	})

	t.Run("koku bracket comma separated", func(t *testing.T) {
		c := newEchoContext("filter%5Bproject%5D=payments,frontend")
		assert.Equal(t, []string{"payments", "frontend"}, IncludeValues(c, "project"))
	})

	t.Run("repeated bracket keys", func(t *testing.T) {
		c := newEchoContext("filter%5Bproject%5D=alpha&filter%5Bproject%5D=beta")
		assert.Equal(t, []string{"alpha", "beta"}, IncludeValues(c, "project"))
	})
}

func TestExcludeAndExactValues(t *testing.T) {
	c := newEchoContext("exclude%5Bproject%5D=kube-system&filter%5Bexact%3Acontainer%5D=web")
	assert.Equal(t, []string{"kube-system"}, ExcludeValues(c, "project"))
	assert.Equal(t, []string{"web"}, ExactValues(c, "container"))
}

func TestGroupByTagKey(t *testing.T) {
	c := newEchoContext("group_by%5Btag%3Aenvironment%5D=*")
	assert.Equal(t, "environment", GroupByTagKey(c))

	c = newEchoContext("group_by=tag:team")
	assert.Equal(t, "team", GroupByTagKey(c))

	c = newEchoContext("engine=cost")
	assert.Empty(t, GroupByTagKey(c))
}

func TestFirstFilter(t *testing.T) {
	t.Run("koku cluster filter", func(t *testing.T) {
		c := newEchoContext("filter%5Bcluster%5D=550e8400-e29b-41d4-a716-446655440000")
		assert.Equal(t, "550e8400-e29b-41d4-a716-446655440000", FirstFilter(c, "cluster"))
	})

	t.Run("flat cluster_uuid alias", func(t *testing.T) {
		c := newEchoContext("cluster_uuid=550e8400-e29b-41d4-a716-446655440000")
		assert.Equal(t, "550e8400-e29b-41d4-a716-446655440000", FirstFilter(c, "cluster"))
	})

	t.Run("guest_agent_detected bracket filter", func(t *testing.T) {
		c := newEchoContext("filter%5Bguest_agent_detected%5D=true")
		assert.Equal(t, "true", FirstFilter(c, "guest_agent_detected"))
	})
}

func TestParseOrderBy(t *testing.T) {
	allowed := map[string]string{
		"project":       "ns.namespace",
		"last_reported": "clusters.last_reported_at",
	}

	t.Run("koku bracket syntax", func(t *testing.T) {
		c := newEchoContext("order_by%5Bproject%5D=asc")
		col, dir, err := ParseOrderBy(c, allowed, "last_reported", "desc")
		require.NoError(t, err)
		assert.Equal(t, "ns.namespace", col)
		assert.Equal(t, "asc", dir)
	})

	t.Run("legacy flat order_by", func(t *testing.T) {
		c := newEchoContext("order_by=project&order_how=asc")
		col, dir, err := ParseOrderBy(c, allowed, "last_reported", "desc")
		require.NoError(t, err)
		assert.Equal(t, "ns.namespace", col)
		assert.Equal(t, "asc", dir)
	})

	t.Run("default when unset", func(t *testing.T) {
		c := newEchoContext("")
		col, dir, err := ParseOrderBy(c, allowed, "last_reported", "desc")
		require.NoError(t, err)
		assert.Equal(t, "clusters.last_reported_at", col)
		assert.Equal(t, "desc", dir)
	})

	t.Run("invalid bracket field", func(t *testing.T) {
		c := newEchoContext("order_by%5Bunknown%5D=desc")
		_, _, err := ParseOrderBy(c, allowed, "last_reported", "desc")
		require.Error(t, err)
	})
}

func TestNormalizeRecommendationTermFilter(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", "", false},
		{"short_term", "short", false},
		{"medium_term", "medium", false},
		{"long_term", "long", false},
		{"short", "short", false},
		{"medium", "medium", false},
		{"long", "long", false},
		{"SHORT_TERM", "short", false},
		{"invalid", "", true},
	}
	for _, tc := range tests {
		got, err := NormalizeRecommendationTermFilter(tc.in)
		if tc.wantErr {
			require.Error(t, err, "input %q", tc.in)
			continue
		}
		require.NoError(t, err, "input %q", tc.in)
		assert.Equal(t, tc.want, got, "input %q", tc.in)
	}
}

func TestParseOrderByAPIKey(t *testing.T) {
	allowed := map[string]string{"node": "f.node", "estimated_monthly_savings_usd": "sort_savings"}
	c := newEchoContext("order_by%5Bestimated_monthly_savings_usd%5D=desc")
	field, dir, err := ParseOrderByAPIKey(c, allowed, "estimated_monthly_savings_usd", "desc")
	require.NoError(t, err)
	assert.Equal(t, "estimated_monthly_savings_usd", field)
	assert.Equal(t, "desc", dir)
}
