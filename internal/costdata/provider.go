package costdata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"

	"github.com/redhatinsights/ros-ocp-backend/internal/cache"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/httpclient"
)

const (
	defaultCostDataCacheTTL    = 5 * time.Minute
	defaultCostCacheMaxEntries = 1000
)

// ClusterCostData holds the cost model rates and namespace-level cost/usage
// aggregates returned by the Koku effective-rates endpoint.
type ClusterCostData struct {
	ClusterID        string                    `json:"cluster_id"`
	ProviderUUID     string                    `json:"provider_uuid"`
	DistributionType string                    `json:"distribution_type"`
	MarkupPct        float64                   `json:"markup_pct"`
	Currency         string                    `json:"currency"`
	ConfiguredRates  map[string]RatePair       `json:"configured_rates"`
	Namespaces       map[string]NamespaceCosts `json:"namespace_aggregates"`
}

// RatePair holds the infrastructure and supplementary rate for a metric.
type RatePair struct {
	Infrastructure float64 `json:"infrastructure"`
	Supplementary  float64 `json:"supplementary"`
}

// NamespaceCosts holds the cost/usage aggregates for a single namespace.
type NamespaceCosts struct {
	CostModelCPUCost float64 `json:"cost_model_cpu_cost"`
	CostModelMemCost float64 `json:"cost_model_memory_cost"`
	InfraCost        float64 `json:"infrastructure_cost"`
	DistributedCost  float64 `json:"distributed_cost"`
	CPUUsageHours    float64 `json:"cpu_usage_hours"`
	CPURequestHours  float64 `json:"cpu_request_hours"`
	MemUsageHours    float64 `json:"mem_usage_hours"`
	MemRequestHours  float64 `json:"mem_request_hours"`
}

// CostDataProvider is the interface for fetching cost data from Koku.
type CostDataProvider interface {
	GetEffectiveRates(ctx context.Context, orgID, clusterID string,
		start, end time.Time) (*ClusterCostData, error)

	// GetUserCurrency returns the user's preferred display currency for the
	// given org_id. Returns DefaultCurrency ("USD") on error or when unset.
	GetUserCurrency(ctx context.Context, orgID string) (string, error)

	// GetExchangeRate returns the exchange rate for converting from one
	// currency to another. Returns 1.0 when the pair is unavailable or an
	// error occurs (fallback: no conversion).
	GetExchangeRate(ctx context.Context, orgID, from, to string) (float64, error)
}

var (
	costDataCacheMu         sync.RWMutex
	costDataCache           *expirable.LRU[string, *ClusterCostData]
	costDataCacheMaxEntries int
)

func costCacheKey(orgID, clusterID string) string {
	return orgID + "\x00" + clusterID
}

func costCacheTTL() time.Duration {
	return defaultCostDataCacheTTL
}

func currentCostCache() *expirable.LRU[string, *ClusterCostData] {
	costDataCacheMu.RLock()
	c := costDataCache
	costDataCacheMu.RUnlock()
	if c != nil {
		return c
	}
	return initCostCache()
}

func initCostCache() *expirable.LRU[string, *ClusterCostData] {
	costDataCacheMu.Lock()
	defer costDataCacheMu.Unlock()
	if costDataCache != nil {
		return costDataCache
	}
	costDataCacheMaxEntries = defaultCostCacheMaxEntries
	if cfg := config.GetConfig(); cfg != nil && cfg.CostCacheMaxEntries > 0 {
		costDataCacheMaxEntries = cfg.CostCacheMaxEntries
	}
	costDataCache = expirable.NewLRU[string, *ClusterCostData](
		costDataCacheMaxEntries,
		func(_ string, _ *ClusterCostData) {
			costCacheRemovals.Inc()
		},
		costCacheTTL(),
	)
	costCacheSize.Set(0)
	return costDataCache
}

// InvalidateCostDataCache clears cached effective rates for an org/cluster pair.
// Pass empty clusterID to invalidate all clusters for the org.
func InvalidateCostDataCache(orgID, clusterID string) {
	c := currentCostCache()
	if clusterID == "" {
		cache.RemoveByPrefix(c, orgID+"\x00")
		costCacheSize.Set(float64(c.Len()))
		return
	}
	c.Remove(costCacheKey(orgID, clusterID))
	costCacheSize.Set(float64(c.Len()))
}

// HTTPCostDataProvider fetches cost data from the Koku masu API over HTTP.
type HTTPCostDataProvider struct {
	BaseURL    string
	HTTPClient *http.Client
}

// NewHTTPCostDataProvider creates a new HTTP-based cost data provider with a shared transport.
func NewHTTPCostDataProvider(baseURL string, timeout time.Duration) *HTTPCostDataProvider {
	return &HTTPCostDataProvider{
		BaseURL:    baseURL,
		HTTPClient: httpclient.NewClient(timeout),
	}
}

func (p *HTTPCostDataProvider) GetEffectiveRates(
	ctx context.Context,
	orgID, clusterID string,
	start, end time.Time,
) (*ClusterCostData, error) {
	key := costCacheKey(orgID, clusterID)
	c := currentCostCache()
	if data, ok := c.Get(key); ok {
		return data, nil
	}

	data, err := p.fetchEffectiveRates(ctx, orgID, clusterID, start, end)
	if err != nil {
		return nil, err
	}

	c.Add(key, data)
	costCacheSize.Set(float64(c.Len()))
	return data, nil
}

func (p *HTTPCostDataProvider) fetchEffectiveRates(
	ctx context.Context,
	orgID, clusterID string,
	start, end time.Time,
) (*ClusterCostData, error) {
	params := url.Values{}
	params.Set("org_id", orgID)
	params.Set("cluster_id", clusterID)
	params.Set("start_date", start.UTC().Format("2006-01-02"))
	params.Set("end_date", end.UTC().Format("2006-01-02"))

	reqURL := fmt.Sprintf("%s/api/cost-management/v1/effective_rates/?%s", p.BaseURL, params.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	resp, err := p.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request to Koku: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Koku effective-rates returned %d: %s", resp.StatusCode, string(body))
	}

	var data ClusterCostData
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &data, nil
}

// ---------------------------------------------------------------------------
// User-currency cache
// ---------------------------------------------------------------------------

const (
	defaultUserCurrencyCacheTTL        = 1 * time.Hour
	defaultUserCurrencyCacheMaxEntries = 1000
)

var (
	userCurrencyCacheMu sync.RWMutex
	userCurrencyCache   *expirable.LRU[string, string]
)

func userCurrencyCacheTTL() time.Duration {
	if cfg := config.GetConfig(); cfg != nil && cfg.UserCurrencyCacheTTLSecs > 0 {
		return time.Duration(cfg.UserCurrencyCacheTTLSecs) * time.Second
	}
	return defaultUserCurrencyCacheTTL
}

func currentUserCurrencyCache() *expirable.LRU[string, string] {
	userCurrencyCacheMu.RLock()
	c := userCurrencyCache
	userCurrencyCacheMu.RUnlock()
	if c != nil {
		return c
	}
	return initUserCurrencyCache()
}

func initUserCurrencyCache() *expirable.LRU[string, string] {
	userCurrencyCacheMu.Lock()
	defer userCurrencyCacheMu.Unlock()
	if userCurrencyCache != nil {
		return userCurrencyCache
	}
	maxEntries := defaultUserCurrencyCacheMaxEntries
	if cfg := config.GetConfig(); cfg != nil && cfg.UserCurrencyCacheMaxEntries > 0 {
		maxEntries = cfg.UserCurrencyCacheMaxEntries
	}
	userCurrencyCache = expirable.NewLRU[string, string](
		maxEntries,
		func(_ string, _ string) { userCurrencyCacheRemovals.Inc() },
		userCurrencyCacheTTL(),
	)
	userCurrencyCacheSize.Set(0)
	return userCurrencyCache
}

// ---------------------------------------------------------------------------
// Exchange-rate cache
// ---------------------------------------------------------------------------

const (
	defaultExchangeRateCacheTTL        = 1 * time.Hour
	defaultExchangeRateCacheMaxEntries = 2000
)

var (
	exchangeRateCacheMu sync.RWMutex
	exchangeRateCache   *expirable.LRU[string, float64]
)

func exchangeRateCacheKey(orgID, from, to string) string {
	return orgID + "\x00" + from + "\x00" + to
}

func exchangeRateCacheTTL() time.Duration {
	if cfg := config.GetConfig(); cfg != nil && cfg.ExchangeRateCacheTTLSecs > 0 {
		return time.Duration(cfg.ExchangeRateCacheTTLSecs) * time.Second
	}
	return defaultExchangeRateCacheTTL
}

func currentExchangeRateCache() *expirable.LRU[string, float64] {
	exchangeRateCacheMu.RLock()
	c := exchangeRateCache
	exchangeRateCacheMu.RUnlock()
	if c != nil {
		return c
	}
	return initExchangeRateCache()
}

func initExchangeRateCache() *expirable.LRU[string, float64] {
	exchangeRateCacheMu.Lock()
	defer exchangeRateCacheMu.Unlock()
	if exchangeRateCache != nil {
		return exchangeRateCache
	}
	maxEntries := defaultExchangeRateCacheMaxEntries
	if cfg := config.GetConfig(); cfg != nil && cfg.ExchangeRateCacheMaxEntries > 0 {
		maxEntries = cfg.ExchangeRateCacheMaxEntries
	}
	exchangeRateCache = expirable.NewLRU[string, float64](
		maxEntries,
		func(_ string, _ float64) { exchangeRateCacheRemovals.Inc() },
		exchangeRateCacheTTL(),
	)
	exchangeRateCacheSize.Set(0)
	return exchangeRateCache
}

// ---------------------------------------------------------------------------
// HTTPCostDataProvider: GetUserCurrency & GetExchangeRate
// ---------------------------------------------------------------------------

func (p *HTTPCostDataProvider) GetUserCurrency(
	ctx context.Context, orgID string,
) (string, error) {
	c := currentUserCurrencyCache()
	if currency, ok := c.Get(orgID); ok {
		return currency, nil
	}

	currency, err := p.fetchUserCurrency(ctx, orgID)
	if err != nil {
		return DefaultCurrency, err
	}
	c.Add(orgID, currency)
	userCurrencyCacheSize.Set(float64(c.Len()))
	return currency, nil
}

type userCurrencyResponse struct {
	Currency string `json:"currency"`
}

func (p *HTTPCostDataProvider) fetchUserCurrency(
	ctx context.Context, orgID string,
) (string, error) {
	params := url.Values{}
	params.Set("org_id", orgID)
	reqURL := fmt.Sprintf("%s/api/cost-management/v1/user_currency/?%s", p.BaseURL, params.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return DefaultCurrency, fmt.Errorf("build user-currency request: %w", err)
	}

	resp, err := p.HTTPClient.Do(req)
	if err != nil {
		return DefaultCurrency, fmt.Errorf("HTTP user-currency request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return DefaultCurrency, fmt.Errorf("Koku user-currency returned %d: %s", resp.StatusCode, string(body))
	}

	var data userCurrencyResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return DefaultCurrency, fmt.Errorf("decode user-currency response: %w", err)
	}
	if data.Currency == "" {
		return DefaultCurrency, nil
	}
	return data.Currency, nil
}

func (p *HTTPCostDataProvider) GetExchangeRate(
	ctx context.Context, orgID, from, to string,
) (float64, error) {
	if from == to {
		return 1.0, nil
	}

	key := exchangeRateCacheKey(orgID, from, to)
	c := currentExchangeRateCache()
	if rate, ok := c.Get(key); ok {
		return rate, nil
	}

	rate, err := p.fetchExchangeRate(ctx, orgID, from, to)
	if err != nil {
		return 1.0, err
	}
	c.Add(key, rate)
	exchangeRateCacheSize.Set(float64(c.Len()))
	return rate, nil
}

type exchangeRateResponse struct {
	FromCurrency string  `json:"from_currency"`
	ToCurrency   string  `json:"to_currency"`
	Rate         *string `json:"rate"` // nullable JSON string
}

func (p *HTTPCostDataProvider) fetchExchangeRate(
	ctx context.Context, orgID, from, to string,
) (float64, error) {
	schema := "org" + orgID
	params := url.Values{}
	params.Set("schema", schema)
	params.Set("from", from)
	params.Set("to", to)
	reqURL := fmt.Sprintf("%s/api/cost-management/v1/exchange_rate/?%s", p.BaseURL, params.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return 1.0, fmt.Errorf("build exchange-rate request: %w", err)
	}

	resp, err := p.HTTPClient.Do(req)
	if err != nil {
		return 1.0, fmt.Errorf("HTTP exchange-rate request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 1.0, fmt.Errorf("Koku exchange-rate returned %d: %s", resp.StatusCode, string(body))
	}

	var data exchangeRateResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return 1.0, fmt.Errorf("decode exchange-rate response: %w", err)
	}

	if data.Rate == nil {
		log.Printf("WARN: exchange rate unavailable for %s->%s (org=%s), returning 1.0", from, to, orgID)
		return 1.0, nil
	}

	rate, err := strconv.ParseFloat(*data.Rate, 64)
	if err != nil {
		return 1.0, fmt.Errorf("parse exchange rate %q: %w", *data.Rate, err)
	}
	return rate, nil
}

// NilCostDataProvider returns zero-value cost data. Used when no Koku URL is configured.
type NilCostDataProvider struct{}

func (n *NilCostDataProvider) GetEffectiveRates(
	ctx context.Context,
	orgID, clusterID string,
	start, end time.Time,
) (*ClusterCostData, error) {
	return &ClusterCostData{
		ClusterID:       clusterID,
		Currency:        DefaultCurrency,
		ConfiguredRates: map[string]RatePair{},
		Namespaces:      map[string]NamespaceCosts{},
	}, nil
}

func (n *NilCostDataProvider) GetUserCurrency(
	ctx context.Context, orgID string,
) (string, error) {
	return DefaultCurrency, nil
}

func (n *NilCostDataProvider) GetExchangeRate(
	ctx context.Context, orgID, from, to string,
) (float64, error) {
	return 1.0, nil
}

// SetCostDataCacheTTLForTest recreates the cache with the given TTL, preserving
// the current max entries setting (tests only).
func SetCostDataCacheTTLForTest(ttl time.Duration) func() {
	costDataCacheMu.Lock()
	prev := costDataCache
	maxEntries := costDataCacheMaxEntries
	if maxEntries <= 0 {
		maxEntries = defaultCostCacheMaxEntries
	}
	costDataCache = expirable.NewLRU[string, *ClusterCostData](
		maxEntries,
		func(_ string, _ *ClusterCostData) {
			costCacheRemovals.Inc()
		},
		ttl,
	)
	costDataCacheMu.Unlock()
	costCacheSize.Set(0)
	return func() {
		costDataCacheMu.Lock()
		costDataCache = prev
		costDataCacheMu.Unlock()
		if prev != nil {
			costCacheSize.Set(float64(prev.Len()))
		} else {
			costCacheSize.Set(0)
		}
	}
}

// ResetCostDataCacheForTest replaces the cache with a fresh instance (tests only).
func ResetCostDataCacheForTest(maxEntries int) func() {
	costDataCacheMu.Lock()
	prev := costDataCache
	costDataCacheMaxEntries = maxEntries
	costDataCache = expirable.NewLRU[string, *ClusterCostData](
		maxEntries,
		func(_ string, _ *ClusterCostData) {
			costCacheRemovals.Inc()
		},
		costCacheTTL(),
	)
	costDataCacheMu.Unlock()
	costCacheSize.Set(0)
	return func() {
		costDataCacheMu.Lock()
		costDataCache = prev
		costDataCacheMu.Unlock()
		if prev != nil {
			costCacheSize.Set(float64(prev.Len()))
		} else {
			costCacheSize.Set(0)
		}
	}
}

// ClearCostDataCacheForTest removes all cached entries (tests only).
func ClearCostDataCacheForTest() {
	currentCostCache().Purge()
	costCacheSize.Set(0)
}
