package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

func TestHasListProjectionParams(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		path string
		want bool
	}{
		{"no params", "/namespaces?limit=10", false},
		{"flat term", "/namespaces?term=short_term", true},
		{"filter term", "/namespaces?filter%5Bterm%5D=short_term", true},
		{"flat engine", "/namespaces?engine=cost", true},
		{"filter engine", "/namespaces?filter%5Bengine%5D=cost", true},
		{"both params", "/namespaces?term=short_term&engine=cost", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			assert.Equal(t, tc.want, hasListProjectionParams(c))
		})
	}
}
