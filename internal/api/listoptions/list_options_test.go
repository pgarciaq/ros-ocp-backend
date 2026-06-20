package listoptions

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

func TestSQLOrderByFragment(t *testing.T) {
	tests := []struct {
		name     string
		column   string
		orderHow string
		want     string
	}{
		{
			name:     "container column desc appends NULLS LAST",
			column:   ContainerAllowedOrderBy["container"],
			orderHow: OrderDesc,
			want:     "recommendation_sets.container_name desc NULLS LAST",
		},
		{
			name:     "container column asc appends NULLS LAST",
			column:   ContainerAllowedOrderBy["container"],
			orderHow: OrderAsc,
			want:     "recommendation_sets.container_name asc NULLS LAST",
		},
		{
			name:     "container cpu_request_current desc appends NULLS LAST",
			column:   ContainerAllowedOrderBy["cpu_request_current"],
			orderHow: OrderDesc,
			want:     "recommendation_sets.cpu_request_current desc NULLS LAST",
		},
		{
			name:     "container cpu variation desc appends NULLS LAST",
			column:   ContainerAllowedOrderBy["cpu_variation_short_cost"],
			orderHow: OrderDesc,
			want:     "recommendation_sets.cpu_variation_short_cost_pct desc NULLS LAST",
		},
		{
			name:     "container cpu variation asc appends NULLS LAST",
			column:   ContainerAllowedOrderBy["cpu_variation_short_cost"],
			orderHow: OrderAsc,
			want:     "recommendation_sets.cpu_variation_short_cost_pct asc NULLS LAST",
		},
		{
			name:     "container memory variation desc appends NULLS LAST",
			column:   ContainerAllowedOrderBy["memory_variation_long_performance"],
			orderHow: OrderDesc,
			want:     "recommendation_sets.memory_variation_long_performance_pct desc NULLS LAST",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SQLOrderByFragment(tt.column, tt.orderHow)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestListAPIOptions_LimitValidation(t *testing.T) {
	e := echo.New()

	tests := []struct {
		name      string
		rawQuery  string
		wantLimit int
		wantErr   bool
	}{
		{name: "negative limit rejected", rawQuery: "limit=-1", wantErr: true},
		{name: "invalid limit string rejected", rawQuery: "limit=abc", wantErr: true},
		{name: "zero limit uses default", rawQuery: "limit=0", wantLimit: DefaultLimit},
		{name: "empty limit uses default", rawQuery: "", wantLimit: DefaultLimit},
		{name: "limit above max clamped", rawQuery: "limit=500000", wantLimit: MaxLimit},
		{name: "limit at max unchanged", rawQuery: "limit=1000", wantLimit: MaxLimit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := "/"
			if tt.rawQuery != "" {
				target = "/?" + tt.rawQuery
			}
			req := httptest.NewRequest(http.MethodGet, target, nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			opts, err := ListAPIOptions(c, DefaultContainerRecsDBColumn, ContainerAllowedOrderBy)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantLimit, opts.Limit)
		})
	}
}

func TestListAPIOptions_KokuOrderBySyntax(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/?order_by%5Bcontainer%5D=asc", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	opts, err := ListAPIOptions(c, DefaultContainerRecsDBColumn, ContainerAllowedOrderBy)
	require.NoError(t, err)
	assert.Equal(t, ContainerAllowedOrderBy["container"], opts.OrderBy)
	assert.Equal(t, OrderAsc, opts.OrderHow)
}

func TestListAPIOptions_OffsetValidation(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_API_MAX_OFFSET", "10000")
	_ = config.GetConfig()

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/?offset=10001", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	_, err := ListAPIOptions(c, DefaultContainerRecsDBColumn, ContainerAllowedOrderBy)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "offset exceeds maximum")
}

func TestParseOffset_WithinLimit(t *testing.T) {
	offset, err := parseOffset("5000", 10000)
	require.NoError(t, err)
	assert.Equal(t, 5000, offset)
}
