// Package debug provides runtime profiling helpers.
//
// pprof endpoints are registered on the internal metrics/health port (not the
// API port) and are gated by ROS_ENABLE_PPROF=true. The Cmdline handler is
// intentionally excluded — it leaks the full process argument list, which may
// contain secrets passed via flags.
package debug

import (
	"net/http"
	"net/http/pprof"

	"github.com/labstack/echo/v4"
)

// RegisterEchoPprof adds pprof handlers to an Echo instance.
// Used by the API server's internal metrics listener.
func RegisterEchoPprof(e *echo.Echo) {
	e.GET("/debug/pprof/", echo.WrapHandler(http.HandlerFunc(pprof.Index)))
	e.GET("/debug/pprof/profile", echo.WrapHandler(http.HandlerFunc(pprof.Profile)))
	e.GET("/debug/pprof/symbol", echo.WrapHandler(http.HandlerFunc(pprof.Symbol)))
	e.GET("/debug/pprof/trace", echo.WrapHandler(http.HandlerFunc(pprof.Trace)))
	e.GET("/debug/pprof/:name", echo.WrapHandler(http.HandlerFunc(pprof.Index)))
}

// RegisterMuxPprof adds pprof handlers to a standard ServeMux.
// Used by the processor and poller metrics listener.
//
// No explicit /:name route is needed — ServeMux treats the trailing-slash
// pattern "/debug/pprof/" as a prefix match, so pprof.Index handles
// /debug/pprof/heap, /debug/pprof/goroutine, etc. automatically.
func RegisterMuxPprof(mux *http.ServeMux) {
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
}
