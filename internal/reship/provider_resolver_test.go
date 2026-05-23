package reship

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

type staticProviderResolver struct {
	providerUUID uuid.UUID
	err          error
}

func (s staticProviderResolver) ResolveProviderUUID(
	_ context.Context,
	_ string,
	_ uuid.UUID,
) (uuid.UUID, error) {
	if s.err != nil {
		return uuid.Nil, s.err
	}
	return s.providerUUID, nil
}

func testMasuServer(reshipHandler http.HandlerFunc) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/effective_rates/") {
			w.Header().Set("Content-Type", "application/json")
			clusterID := r.URL.Query().Get("cluster_id")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"cluster_id":          clusterID,
				"provider_uuid":       testutil.TestProviderUUID,
				"configured_rates":    map[string]interface{}{},
				"namespace_aggregates": map[string]interface{}{},
			})
			return
		}
		reshipHandler(w, r)
	}))
}

func TestHTTPEffectiveRatesResolver_ResolveProviderUUID(t *testing.T) {
	clusterID := testutil.TestClusterUUID
	masu := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/cost-management/v1/effective_rates/", r.URL.Path)
		assert.Equal(t, clusterID, r.URL.Query().Get("cluster_id"))
		assert.Equal(t, "1234567", r.URL.Query().Get("org_id"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"cluster_id":    clusterID,
			"provider_uuid": testutil.TestProviderUUID,
		})
	}))
	defer masu.Close()

	resolver := NewHTTPEffectiveRatesResolver(masu.URL)
	providerUUID, err := resolver.ResolveProviderUUID(
		context.Background(),
		"1234567",
		uuid.MustParse(clusterID),
	)
	require.NoError(t, err)
	assert.Equal(t, testutil.TestProviderUUID, providerUUID.String())
}

func TestPostReship_UsesResolvedProviderUUID(t *testing.T) {
	var capturedURL string
	masu := testMasuServer(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		w.WriteHeader(http.StatusOK)
	})
	defer masu.Close()

	client := NewHTTPClient(masu.URL, nil)
	_, err := client.PostReship(
		context.Background(),
		"1234567",
		uuid.MustParse(testutil.TestClusterUUID),
	)
	require.NoError(t, err)
	assert.Contains(t, capturedURL, "provider_uuid="+testutil.TestProviderUUID)
	assert.NotContains(t, capturedURL, "provider_uuid="+testutil.TestClusterUUID)
}

func TestPostReship_ResolverFailure(t *testing.T) {
	client := NewHTTPClient("http://127.0.0.1:1", &http.Client{})
	client.resolver = staticProviderResolver{
		err: fmt.Errorf("provider lookup failed"),
	}
	_, err := client.PostReship(
		context.Background(),
		"1234567",
		uuid.MustParse(testutil.TestClusterUUID),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider lookup failed")
}
