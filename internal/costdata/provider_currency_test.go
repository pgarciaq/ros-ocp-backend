package costdata_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
)

// ---------------------------------------------------------------------------
// GetUserCurrency contract tests
// ---------------------------------------------------------------------------

func TestGetUserCurrency_Success(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/cost-management/v1/user_currency/", r.URL.Path)
		assert.Equal(t, "1234567", r.URL.Query().Get("org_id"))
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"currency":"EUR"}`))
	}))
	t.Cleanup(srv.Close)

	provider := costdata.NewHTTPCostDataProvider(srv.URL, 5*time.Second)
	currency, err := provider.GetUserCurrency(context.Background(), "1234567")
	require.NoError(t, err)
	assert.Equal(t, "EUR", currency)
}

func TestGetUserCurrency_EmptyCurrency_DefaultsUSD(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"currency":""}`))
	}))
	t.Cleanup(srv.Close)

	provider := costdata.NewHTTPCostDataProvider(srv.URL, 5*time.Second)
	currency, err := provider.GetUserCurrency(context.Background(), "1234567")
	require.NoError(t, err)
	assert.Equal(t, "USD", currency)
}

func TestGetUserCurrency_ServerError_DefaultsUSD(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	provider := costdata.NewHTTPCostDataProvider(srv.URL, 5*time.Second)
	currency, err := provider.GetUserCurrency(context.Background(), "1234567")
	require.Error(t, err)
	assert.Equal(t, "USD", currency)
}

func TestGetUserCurrency_Timeout_DefaultsUSD(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	provider := costdata.NewHTTPCostDataProvider(srv.URL, 50*time.Millisecond)
	currency, err := provider.GetUserCurrency(context.Background(), "1234567")
	require.Error(t, err)
	assert.Equal(t, "USD", currency)
}

func TestGetUserCurrency_MalformedJSON_DefaultsUSD(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`INVALID JSON`))
	}))
	t.Cleanup(srv.Close)

	provider := costdata.NewHTTPCostDataProvider(srv.URL, 5*time.Second)
	currency, err := provider.GetUserCurrency(context.Background(), "1234567")
	require.Error(t, err)
	assert.Equal(t, "USD", currency)
}

func TestGetUserCurrency_Caching(t *testing.T) {
	t.Parallel()
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"currency":"GBP"}`))
	}))
	t.Cleanup(srv.Close)

	provider := costdata.NewHTTPCostDataProvider(srv.URL, 5*time.Second)

	c1, err := provider.GetUserCurrency(context.Background(), "org1")
	require.NoError(t, err)
	assert.Equal(t, "GBP", c1)

	c2, err := provider.GetUserCurrency(context.Background(), "org1")
	require.NoError(t, err)
	assert.Equal(t, "GBP", c2)
	assert.Equal(t, 1, callCount, "second call should hit cache")
}

// ---------------------------------------------------------------------------
// GetExchangeRate contract tests
// ---------------------------------------------------------------------------

func TestGetExchangeRate_Success(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/cost-management/v1/exchange_rate/", r.URL.Path)
		assert.Equal(t, "org1234567", r.URL.Query().Get("schema"))
		assert.Equal(t, "USD", r.URL.Query().Get("from"))
		assert.Equal(t, "EUR", r.URL.Query().Get("to"))
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"from_currency":"USD","to_currency":"EUR","rate":"0.92"}`))
	}))
	t.Cleanup(srv.Close)

	provider := costdata.NewHTTPCostDataProvider(srv.URL, 5*time.Second)
	rate, err := provider.GetExchangeRate(context.Background(), "1234567", "USD", "EUR")
	require.NoError(t, err)
	assert.InDelta(t, 0.92, rate, 0.001)
}

func TestGetExchangeRate_SameCurrency_NoHTTP(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("should not make HTTP call when from==to")
	}))
	t.Cleanup(srv.Close)

	provider := costdata.NewHTTPCostDataProvider(srv.URL, 5*time.Second)
	rate, err := provider.GetExchangeRate(context.Background(), "1234567", "USD", "USD")
	require.NoError(t, err)
	assert.Equal(t, 1.0, rate)
}

func TestGetExchangeRate_NullRate_Returns1(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"from_currency":"USD","to_currency":"JPY","rate":null}`))
	}))
	t.Cleanup(srv.Close)

	provider := costdata.NewHTTPCostDataProvider(srv.URL, 5*time.Second)
	rate, err := provider.GetExchangeRate(context.Background(), "1234567", "USD", "JPY")
	require.NoError(t, err)
	assert.Equal(t, 1.0, rate)
}

func TestGetExchangeRate_ServerError_Returns1(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	provider := costdata.NewHTTPCostDataProvider(srv.URL, 5*time.Second)
	rate, err := provider.GetExchangeRate(context.Background(), "1234567", "USD", "EUR")
	require.Error(t, err)
	assert.Equal(t, 1.0, rate)
}

func TestGetExchangeRate_InvalidRateString_Returns1(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"from_currency":"USD","to_currency":"EUR","rate":"not-a-number"}`))
	}))
	t.Cleanup(srv.Close)

	provider := costdata.NewHTTPCostDataProvider(srv.URL, 5*time.Second)
	rate, err := provider.GetExchangeRate(context.Background(), "1234567", "USD", "EUR")
	require.Error(t, err)
	assert.Equal(t, 1.0, rate)
}

func TestGetExchangeRate_Caching(t *testing.T) {
	t.Parallel()
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"from_currency":"USD","to_currency":"EUR","rate":"0.92"}`))
	}))
	t.Cleanup(srv.Close)

	provider := costdata.NewHTTPCostDataProvider(srv.URL, 5*time.Second)

	r1, err := provider.GetExchangeRate(context.Background(), "org1", "USD", "EUR")
	require.NoError(t, err)
	assert.InDelta(t, 0.92, r1, 0.001)

	r2, err := provider.GetExchangeRate(context.Background(), "org1", "USD", "EUR")
	require.NoError(t, err)
	assert.InDelta(t, 0.92, r2, 0.001)
	assert.Equal(t, 1, callCount, "second call should hit cache")
}

// ---------------------------------------------------------------------------
// NilCostDataProvider fallback tests
// ---------------------------------------------------------------------------

func TestNilProvider_GetUserCurrency(t *testing.T) {
	t.Parallel()
	p := &costdata.NilCostDataProvider{}
	currency, err := p.GetUserCurrency(context.Background(), "1234567")
	require.NoError(t, err)
	assert.Equal(t, "USD", currency)
}

func TestNilProvider_GetExchangeRate(t *testing.T) {
	t.Parallel()
	p := &costdata.NilCostDataProvider{}
	rate, err := p.GetExchangeRate(context.Background(), "1234567", "USD", "EUR")
	require.NoError(t, err)
	assert.Equal(t, 1.0, rate)
}
