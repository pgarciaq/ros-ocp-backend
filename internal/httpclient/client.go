// Package httpclient provides a shared HTTP transport and client singletons
// for outbound calls (RBAC, cost data, reship, CSV downloads).
package httpclient

import (
	"net/http"
	"sync"
	"time"
)

const (
	defaultMaxIdleConns        = 100
	defaultMaxIdleConnsPerHost = 10
	defaultIdleConnTimeout     = 90 * time.Second
)

var (
	transportOnce sync.Once
	transport     *http.Transport
)

// SharedTransport returns a process-wide http.Transport with connection pooling
// and keep-alive tuned for repeated calls to the same hosts.
func SharedTransport() *http.Transport {
	transportOnce.Do(func() {
		transport = &http.Transport{
			MaxIdleConns:        defaultMaxIdleConns,
			MaxIdleConnsPerHost: defaultMaxIdleConnsPerHost,
			IdleConnTimeout:     defaultIdleConnTimeout,
		}
	})
	return transport
}

// NewClient returns an http.Client that reuses SharedTransport with the given timeout.
func NewClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: SharedTransport(),
	}
}
