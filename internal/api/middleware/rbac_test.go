package middleware

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	promtest "github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/types"
)

func TestAggregatePermissions(t *testing.T) {
	tests := []struct {
		name string
		acls []types.RbacData
		want map[string][]string
		desc string
	}{
		{
			name: "empty acl list",
			acls: []types.RbacData{},
			want: map[string][]string{},
		},
		{
			name: "permission without colon does not panic",
			acls: []types.RbacData{
				{Permission: "no-colon-here"},
			},
			want: map[string][]string{},
		},
		{
			name: "empty permission string does not panic",
			acls: []types.RbacData{
				{Permission: ""},
			},
			want: map[string][]string{},
		},
		{
			name: "wildcard resource type",
			acls: []types.RbacData{
				{Permission: "cost-management:*:read"},
			},
			want: map[string][]string{"*": {}},
		},
		{
			name: "openshift.cluster with no resource definitions",
			acls: []types.RbacData{
				{Permission: "cost-management:openshift.cluster:read"},
			},
			want: map[string][]string{"openshift.cluster": {"*"}},
		},
		{
			name: "openshift.project with string resource definition",
			acls: []types.RbacData{
				{
					Permission: "cost-management:openshift.project:read",
					ResourceDefinitions: []types.RbacResourceDefinitions{
						{AttributeFilter: types.AttributeFilter{Value: "my-project"}},
					},
				},
			},
			want: map[string][]string{"openshift.project": {"my-project"}},
		},
		{
			name: "openshift.node with array resource definition",
			acls: []types.RbacData{
				{
					Permission: "cost-management:openshift.node:read",
					ResourceDefinitions: []types.RbacResourceDefinitions{
						{AttributeFilter: types.AttributeFilter{Value: []interface{}{"node-a", "node-b"}}},
					},
				},
			},
			want: map[string][]string{"openshift.node": {"node-a", "node-b"}},
		},
		{
			name: "non-openshift resource type is ignored",
			acls: []types.RbacData{
				{Permission: "cost-management:aws.account:read"},
			},
			want: map[string][]string{},
		},
		{
			name: "settings write permission",
			acls: []types.RbacData{
				{Permission: "cost-management:settings:write"},
			},
			want: map[string][]string{"settings.write": {"*"}},
		},
		{
			name: "settings read permission",
			acls: []types.RbacData{
				{Permission: "cost-management:settings:read"},
			},
			want: map[string][]string{"settings.read": {"*"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := aggregate_permissions(tt.acls)
			if len(got) != len(tt.want) {
				t.Errorf("aggregate_permissions() returned %d keys, want %d.\ngot:  %v\nwant: %v", len(got), len(tt.want), got, tt.want)
				return
			}
			for k, wantVals := range tt.want {
				gotVals, ok := got[k]
				if !ok {
					t.Errorf("missing key %q in result", k)
					continue
				}
				if len(gotVals) != len(wantVals) {
					t.Errorf("key %q: got %v, want %v", k, gotVals, wantVals)
					continue
				}
				wantSet := make(map[string]bool, len(wantVals))
				for _, v := range wantVals {
					wantSet[v] = true
				}
				for _, v := range gotVals {
					if !wantSet[v] {
						t.Errorf("key %q: unexpected value %q in result %v", k, v, gotVals)
					}
				}
			}
		})
	}
}

func TestRequestUserAccess_Non2xxStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal server error"))
	}))
	defer srv.Close()

	acls, _, err := request_user_access(srv.URL, "dummyIdentity")
	if err == nil {
		t.Errorf("expected error on 500 response, got nil")
	}
	if len(acls) != 0 {
		t.Errorf("expected empty acls on 500 response, got %d", len(acls))
	}
}

func TestRequestUserAccess_RBACDenialIsNotAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("unauthorized"))
	}))
	defer srv.Close()

	// RBAC 401 is a denial, not an outage: (nil, nil) so callers 403 (#532).
	acls, _, err := request_user_access(srv.URL, "dummyIdentity")
	if err != nil {
		t.Errorf("expected nil error on 401 denial, got %v", err)
	}
	if len(acls) != 0 {
		t.Errorf("expected empty acls on 401 denial, got %d", len(acls))
	}
}

func TestRequestUserAccess_ValidResponse(t *testing.T) {
	rbacResp := types.RbacResponse{
		Data: []types.RbacData{
			{Permission: "cost-management:openshift.cluster:read"},
		},
	}
	body, _ := json.Marshal(rbacResp)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	acls, _, err := request_user_access(srv.URL, "dummyIdentity")
	if err != nil {
		t.Fatalf("unexpected error on valid response: %v", err)
	}
	if len(acls) != 1 {
		t.Errorf("expected 1 acl, got %d", len(acls))
	}
}

func TestRequestUserAccess_ConnectionRefused(t *testing.T) {
	// Calling an unreachable URL should error, not panic
	acls, _, err := request_user_access("http://127.0.0.1:1/unreachable", "dummyIdentity")
	if err == nil {
		t.Errorf("expected error on connection failure, got nil")
	}
	if len(acls) != 0 {
		t.Errorf("expected empty acls on connection error, got %d", len(acls))
	}
}

func TestRequestUserAccess_GarbageJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{not json"))
	}))
	defer srv.Close()

	acls, _, err := request_user_access(srv.URL, "dummyIdentity")
	if err == nil {
		t.Errorf("expected error on garbage JSON, got nil")
	}
	if len(acls) != 0 {
		t.Errorf("expected empty acls on garbage JSON, got %d", len(acls))
	}
}

func TestRequestUserAccess_Pagination(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var resp types.RbacResponse
		resp.Data = []types.RbacData{
			{Permission: "cost-management:openshift.cluster:read"},
		}
		if callCount < 3 {
			resp.Links.Next = "/api/rbac/v1/access/?offset=100"
		}
		body, _ := json.Marshal(resp)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	cfg.RBACProtocol = "http"
	cfg.RBACHost = srv.Listener.Addr().(*net.TCPAddr).IP.String()
	cfg.RBACPort = fmt.Sprintf("%d", srv.Listener.Addr().(*net.TCPAddr).Port)

	acls, _, err := request_user_access(srv.URL, "dummyIdentity")
	if err != nil {
		t.Fatalf("unexpected error on paginated response: %v", err)
	}
	if len(acls) != 3 {
		t.Errorf("expected 3 acls from paginated response, got %d", len(acls))
	}
	if callCount != 3 {
		t.Errorf("expected 3 HTTP calls, got %d", callCount)
	}
}

func TestRequestUserAccess_PaginationCapsAt50(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		resp := types.RbacResponse{
			Data: []types.RbacData{
				{Permission: "cost-management:openshift.cluster:read"},
			},
		}
		resp.Links.Next = "/api/rbac/v1/access/?offset=100"
		body, _ := json.Marshal(resp)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	cfg.RBACProtocol = "http"
	cfg.RBACHost = srv.Listener.Addr().(*net.TCPAddr).IP.String()
	cfg.RBACPort = fmt.Sprintf("%d", srv.Listener.Addr().(*net.TCPAddr).Port)

	acls, _, err := request_user_access(srv.URL, "dummyIdentity")
	if err != nil {
		t.Fatalf("unexpected error on truncated pagination: %v", err)
	}
	if callCount != maxRBACPages {
		t.Errorf("expected pagination capped at %d pages, got %d", maxRBACPages, callCount)
	}
	if len(acls) != maxRBACPages {
		t.Errorf("expected %d acls, got %d", maxRBACPages, len(acls))
	}
}

func TestRequestUserAccess_PaginationStopsOnBadPrefix(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		resp := types.RbacResponse{
			Data: []types.RbacData{
				{Permission: "cost-management:openshift.cluster:read"},
			},
		}
		if callCount == 1 {
			resp.Links.Next = "/evil/redirect?target=http://attacker.com"
		}
		body, _ := json.Marshal(resp)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	cfg.RBACProtocol = "http"
	cfg.RBACHost = srv.Listener.Addr().(*net.TCPAddr).IP.String()
	cfg.RBACPort = fmt.Sprintf("%d", srv.Listener.Addr().(*net.TCPAddr).Port)

	acls, _, err := request_user_access(srv.URL, "dummyIdentity")
	if err == nil {
		t.Errorf("expected error on bad pagination prefix, got nil")
	}
	if callCount != 1 {
		t.Errorf("expected pagination to stop after 1 page due to bad prefix, got %d calls", callCount)
	}
	if len(acls) != 0 {
		t.Errorf("expected no acls on bad prefix (fail-closed), got %d", len(acls))
	}
}

func TestRequestUserAccess_MidPagination500DiscardsPartial(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount > 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("boom"))
			return
		}
		resp := types.RbacResponse{
			Data: []types.RbacData{
				{Permission: "cost-management:openshift.cluster:read"},
			},
		}
		resp.Links.Next = "/api/rbac/v1/access/?offset=100"
		body, _ := json.Marshal(resp)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	// Follow-up pages are built from the package cfg (as in the pagination
	// tests above), not srv.URL.
	cfg.RBACProtocol = "http"
	cfg.RBACHost = srv.Listener.Addr().(*net.TCPAddr).IP.String()
	cfg.RBACPort = fmt.Sprintf("%d", srv.Listener.Addr().(*net.TCPAddr).Port)

	// Fail-closed (#532): the first page's ACL must not authorize when the
	// second page fails.
	acls, _, err := request_user_access(srv.URL, "dummyIdentity")
	if err == nil {
		t.Errorf("expected error on mid-pagination 500, got nil")
	}
	if len(acls) != 0 {
		t.Errorf("expected no acls on mid-pagination 500 (partial discarded), got %d", len(acls))
	}
	if callCount != 2 {
		t.Errorf("expected 2 HTTP calls, got %d", callCount)
	}
}

func TestRequestUserAccess_MidStreamGarbageJSONDiscardsPartial(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount > 1 {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{not json"))
			return
		}
		resp := types.RbacResponse{
			Data: []types.RbacData{
				{Permission: "cost-management:openshift.cluster:read"},
			},
		}
		resp.Links.Next = "/api/rbac/v1/access/?offset=100"
		body, _ := json.Marshal(resp)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	// Follow-up pages are built from the package cfg (as in the pagination
	// tests above), not srv.URL.
	cfg.RBACProtocol = "http"
	cfg.RBACHost = srv.Listener.Addr().(*net.TCPAddr).IP.String()
	cfg.RBACPort = fmt.Sprintf("%d", srv.Listener.Addr().(*net.TCPAddr).Port)

	acls, _, err := request_user_access(srv.URL, "dummyIdentity")
	if err == nil {
		t.Errorf("expected error on mid-stream garbage JSON, got nil")
	}
	if len(acls) != 0 {
		t.Errorf("expected no acls on mid-stream garbage JSON (partial discarded), got %d", len(acls))
	}
	if callCount != 2 {
		t.Errorf("expected 2 HTTP calls, got %d", callCount)
	}
}

func TestRequestUserAccess_TruncationEmitsMetric(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := types.RbacResponse{
			Data: []types.RbacData{
				{Permission: "cost-management:openshift.cluster:read"},
			},
		}
		resp.Links.Next = "/api/rbac/v1/access/?offset=100"
		body, _ := json.Marshal(resp)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	cfg.RBACProtocol = "http"
	cfg.RBACHost = srv.Listener.Addr().(*net.TCPAddr).IP.String()
	cfg.RBACPort = fmt.Sprintf("%d", srv.Listener.Addr().(*net.TCPAddr).Port)

	before := promtest.ToFloat64(rbacErrorsTotal.WithLabelValues("truncated"))
	_, truncated, err := request_user_access(srv.URL, "dummyIdentity")
	if err != nil {
		t.Fatalf("truncation serves collected ACLs, unexpected error: %v", err)
	}
	if !truncated {
		t.Errorf("expected truncated=true after maxRBACPages with Links.Next set")
	}
	if got := promtest.ToFloat64(rbacErrorsTotal.WithLabelValues("truncated")) - before; got != 1 {
		t.Errorf("expected truncated counter +1, got %v", got)
	}
}

// withStubRBACConfig points both the per-call config read and the package cfg
// at srv, disables the permission cache, and restores everything afterwards.
func withStubRBACConfig(t *testing.T, srv *httptest.Server) {
	t.Helper()
	addr := srv.Listener.Addr().(*net.TCPAddr)
	host := addr.IP.String()
	port := fmt.Sprintf("%d", addr.Port)

	live := config.GetConfig()
	origHost, origPort, origProto, origTTL := live.RBACHost, live.RBACPort, live.RBACProtocol, live.RBACCacheTTLSecs
	live.RBACHost, live.RBACPort, live.RBACProtocol, live.RBACCacheTTLSecs = host, port, "http", 0
	origPHost, origPPort, origPProto := cfg.RBACHost, cfg.RBACPort, cfg.RBACProtocol
	cfg.RBACHost, cfg.RBACPort, cfg.RBACProtocol = host, port, "http"
	ClearRBACPermissionCacheForTest()
	t.Cleanup(func() {
		live.RBACHost, live.RBACPort, live.RBACProtocol, live.RBACCacheTTLSecs = origHost, origPort, origProto, origTTL
		cfg.RBACHost, cfg.RBACPort, cfg.RBACProtocol = origPHost, origPPort, origPProto
		ClearRBACPermissionCacheForTest()
	})
}

func validACLResponse() []byte {
	body, _ := json.Marshal(types.RbacResponse{
		Data: []types.RbacData{
			{Permission: "cost-management:openshift.cluster:read"},
		},
	})
	return body
}

func TestRbacMiddleware_MapsUpstreamErrorTo503(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("bad gateway"))
	}))
	defer srv.Close()
	withStubRBACConfig(t, srv)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Rh-Identity", "dGVzdA==")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	var nextCalled bool
	err := Rbac(func(c echo.Context) error { nextCalled = true; return nil })(c)
	he, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected *echo.HTTPError, got %T (%v)", err, err)
	}
	if he.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 on upstream error, got %d", he.Code)
	}
	if nextCalled {
		t.Errorf("next handler must not run on upstream error")
	}
}

func TestRbacMiddleware_MapsEmptyTo403(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data": []}`))
	}))
	defer srv.Close()
	withStubRBACConfig(t, srv)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Rh-Identity", "dGVzdA==")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	var nextCalled bool
	err := Rbac(func(c echo.Context) error { nextCalled = true; return nil })(c)
	he, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected *echo.HTTPError, got %T (%v)", err, err)
	}
	if he.Code != http.StatusForbidden {
		t.Errorf("expected 403 on empty ACLs, got %d", he.Code)
	}
	if nextCalled {
		t.Errorf("next handler must not run on empty ACLs")
	}
}

func TestRbacMiddleware_MapsRBACDenialTo403(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("unauthorized"))
	}))
	defer srv.Close()
	withStubRBACConfig(t, srv)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Rh-Identity", "dGVzdA==")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	var nextCalled bool
	err := Rbac(func(c echo.Context) error { nextCalled = true; return nil })(c)
	he, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected *echo.HTTPError, got %T (%v)", err, err)
	}
	if he.Code != http.StatusForbidden {
		t.Errorf("expected 403 on RBAC denial (not 503), got %d", he.Code)
	}
	if nextCalled {
		t.Errorf("next handler must not run on RBAC denial")
	}
}

func TestRbacMiddleware_PassesValidThrough(t *testing.T) {
	body := validACLResponse()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	withStubRBACConfig(t, srv)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Rh-Identity", "dGVzdA==")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	var nextCalled bool
	if err := Rbac(func(c echo.Context) error { nextCalled = true; return nil })(c); err != nil {
		t.Fatalf("unexpected error on valid ACLs: %v", err)
	}
	if !nextCalled {
		t.Errorf("next handler must run on valid ACLs")
	}
	perms, ok := c.Get("user.permissions").(map[string][]string)
	if !ok {
		t.Fatalf("expected user.permissions in context, got %T", c.Get("user.permissions"))
	}
	if _, ok := perms["openshift.cluster"]; !ok {
		t.Errorf("expected openshift.cluster permissions, got %v", perms)
	}
}

// withCacheEnabledRBACConfig mirrors withStubRBACConfig but keeps the
// permission cache enabled so cache store/skip behavior is observable.
func withCacheEnabledRBACConfig(t *testing.T, srv *httptest.Server) {
	t.Helper()
	addr := srv.Listener.Addr().(*net.TCPAddr)
	host := addr.IP.String()
	port := fmt.Sprintf("%d", addr.Port)

	live := config.GetConfig()
	origHost, origPort, origProto, origTTL := live.RBACHost, live.RBACPort, live.RBACProtocol, live.RBACCacheTTLSecs
	live.RBACHost, live.RBACPort, live.RBACProtocol, live.RBACCacheTTLSecs = host, port, "http", 60
	origPHost, origPPort, origPProto := cfg.RBACHost, cfg.RBACPort, cfg.RBACProtocol
	cfg.RBACHost, cfg.RBACPort, cfg.RBACProtocol = host, port, "http"
	ClearRBACPermissionCacheForTest()
	t.Cleanup(func() {
		live.RBACHost, live.RBACPort, live.RBACProtocol, live.RBACCacheTTLSecs = origHost, origPort, origProto, origTTL
		cfg.RBACHost, cfg.RBACPort, cfg.RBACProtocol = origPHost, origPPort, origPProto
		ClearRBACPermissionCacheForTest()
	})
}

func truncatingRBACServer(callCount *int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*callCount++
		resp := types.RbacResponse{
			Data: []types.RbacData{
				{Permission: "cost-management:openshift.cluster:read"},
			},
		}
		resp.Links.Next = "/api/rbac/v1/access/?offset=100"
		body, _ := json.Marshal(resp)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
}

func TestTruncatedPartialsAreServedButNeverCached(t *testing.T) {
	callCount := 0
	srv := truncatingRBACServer(&callCount)
	defer srv.Close()
	withCacheEnabledRBACConfig(t, srv)

	const identity = "dGVzdA=="
	key := rbacIdentityCacheKey(identity)

	// First call serves the partial set (fail-open capacity-cap exception).
	perms, err := get_user_permissions_from_rbac(identity)
	if err != nil {
		t.Fatalf("truncated partial must be served, got error: %v", err)
	}
	if _, ok := perms["openshift.cluster"]; !ok {
		t.Fatalf("expected openshift.cluster permissions, got %v", perms)
	}
	if callCount != maxRBACPages {
		t.Fatalf("expected %d upstream pages, got %d", maxRBACPages, callCount)
	}

	// The partial must not enter the cache: a bool-only assertion on
	// request_user_access would pass while the store still caches, so the
	// cache miss itself is the discriminating assertion.
	if _, ok := getCachedRBACPermissions(key); ok {
		t.Fatalf("truncated partial must not be cached")
	}

	// Second call re-pages upstream instead of serving the stale set.
	perms, err = get_user_permissions_from_rbac(identity)
	if err != nil {
		t.Fatalf("second truncated call must still serve, got error: %v", err)
	}
	if _, ok := perms["openshift.cluster"]; !ok {
		t.Fatalf("expected openshift.cluster permissions on retry, got %v", perms)
	}
	if callCount != 2*maxRBACPages {
		t.Errorf("expected upstream re-fetch (%d calls), got %d: truncated set was cached", 2*maxRBACPages, callCount)
	}
	if _, ok := getCachedRBACPermissions(key); ok {
		t.Errorf("truncated partial must not be cached after retry either")
	}
}

func TestCompleteACLsAreStillCached(t *testing.T) {
	callCount := 0
	body := validACLResponse()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	withCacheEnabledRBACConfig(t, srv)

	const identity = "dGVzdA=="
	key := rbacIdentityCacheKey(identity)

	first, err := get_user_permissions_from_rbac(identity)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := get_user_permissions_from_rbac(identity)
	if err != nil {
		t.Fatalf("unexpected error on second call: %v", err)
	}
	// Non-truncated behavior is unchanged: exactly one upstream fetch.
	if callCount != 1 {
		t.Errorf("expected 1 upstream call across 2 requests (cached), got %d", callCount)
	}
	if _, ok := getCachedRBACPermissions(key); !ok {
		t.Errorf("complete ACL set must be cached")
	}
	if len(first) != len(second) {
		t.Errorf("cached permissions must match served permissions")
	}
}

func TestRequestUserAccess_NonAuthoritative4xxAreErrors(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		wantErr    bool
		wantReason string
	}{
		// 429/408 must not masquerade as identity denials (#546).
		{name: "429 rate limited", status: http.StatusTooManyRequests, wantErr: true, wantReason: "rate_limited"},
		{name: "408 timeout", status: http.StatusRequestTimeout, wantErr: true, wantReason: "bad_status"},
		// 404 reports route drift against our fixed URL, not an identity verdict.
		{name: "404 route drift", status: http.StatusNotFound, wantErr: true, wantReason: "bad_status"},
		// 400 reports a malformed request we built ourselves.
		{name: "400 malformed", status: http.StatusBadRequest, wantErr: true, wantReason: "bad_status"},
		// Authoritative denials stay denials.
		{name: "401 denied", status: http.StatusUnauthorized, wantErr: false},
		{name: "403 denied", status: http.StatusForbidden, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(http.StatusText(tt.status)))
			}))
			defer srv.Close()

			cfg.RBACProtocol = "http"
			cfg.RBACHost = srv.Listener.Addr().(*net.TCPAddr).IP.String()
			cfg.RBACPort = fmt.Sprintf("%d", srv.Listener.Addr().(*net.TCPAddr).Port)

			var before float64
			if tt.wantReason != "" {
				before = promtest.ToFloat64(rbacErrorsTotal.WithLabelValues(tt.wantReason))
			}
			acls, truncated, err := request_user_access(srv.URL, "dummyIdentity")
			if tt.wantErr && err == nil {
				t.Errorf("expected error on %d, got nil (would have 403'd a capacity/outage signal)", tt.status)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("expected nil error on authoritative %d, got %v", tt.status, err)
			}
			if len(acls) != 0 {
				t.Errorf("expected no acls on %d, got %d", tt.status, len(acls))
			}
			if truncated {
				t.Errorf("truncated must be false on %d (only maxRBACPages sets it)", tt.status)
			}
			if tt.wantReason != "" {
				if got := promtest.ToFloat64(rbacErrorsTotal.WithLabelValues(tt.wantReason)) - before; got != 1 {
					t.Errorf("expected %q counter +1 on %d, got %v", tt.wantReason, tt.status, got)
				}
			}
		})
	}
}

func TestRbacMiddleware_MapsNonAuthoritative4xxTo503(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   int
	}{
		// Capacity/outage signals must stay retryable 503s, never identity-blaming 403s.
		{name: "429", status: http.StatusTooManyRequests, want: http.StatusServiceUnavailable},
		{name: "408", status: http.StatusRequestTimeout, want: http.StatusServiceUnavailable},
		{name: "404", status: http.StatusNotFound, want: http.StatusServiceUnavailable},
		{name: "400", status: http.StatusBadRequest, want: http.StatusServiceUnavailable},
		// Authoritative denials stay 403.
		{name: "403", status: http.StatusForbidden, want: http.StatusForbidden},
		{name: "401", status: http.StatusUnauthorized, want: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(http.StatusText(tt.status)))
			}))
			defer srv.Close()
			withStubRBACConfig(t, srv)

			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("X-Rh-Identity", "dGVzdA==")
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			var nextCalled bool
			err := Rbac(func(c echo.Context) error { nextCalled = true; return nil })(c)
			he, ok := err.(*echo.HTTPError)
			if !ok {
				t.Fatalf("expected *echo.HTTPError, got %T (%v)", err, err)
			}
			if he.Code != tt.want {
				t.Errorf("expected %d on RBAC %d, got %d", tt.want, tt.status, he.Code)
			}
			if nextCalled {
				t.Errorf("next handler must not run on RBAC %d", tt.status)
			}
		})
	}
}
