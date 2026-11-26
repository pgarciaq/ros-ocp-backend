package costdata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/sirupsen/logrus"

	"github.com/redhatinsights/ros-ocp-backend/internal/cache"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/httpclient"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

const (
	defaultCostDataCacheTTL    = 5 * time.Minute
	defaultCostCacheMaxEntries = 1000
)

// MoneyScaleMicroCents is the wire scale this build requires: every money field
// of the effective-rates payload is an integer number of micro-cents
// (1 USD = 100_000_000), hours are integer milli-hours, and markup is integer
// basis points (#461). Koku echoes the scale it used so a Masu build that
// predates the contract is a named error here rather than a silent 1e8
// mispricing.
const MoneyScaleMicroCents = "microcents"

// ClusterCostData holds the cost model rates and namespace-level cost/usage
// aggregates returned by the Koku effective-rates endpoint.
//
// All money is integer micro-cents, all hours are integer milli-hours and
// MarkupPct is integer basis points — the `money_scale=microcents` contract.
type ClusterCostData struct {
	ClusterID        string                    `json:"cluster_id"`
	ProviderUUID     string                    `json:"provider_uuid"`
	DistributionType string                    `json:"distribution_type"`
	MoneyScale       string                    `json:"money_scale"`
	MarkupPct        int64                     `json:"markup_pct"`
	Currency         string                    `json:"currency"`
	ConfiguredRates  map[string]RatePair       `json:"configured_rates"`
	Namespaces       map[string]NamespaceCosts `json:"namespace_aggregates"`

	// TierBGPUMonthlyRateMicroCents is not on the wire: it is the cluster
	// Tier-B GPU rate derived from observed spend (#942) — Σ namespace
	// cost_model_gpu_cost ÷ cluster GPU-count-months. GPUMonthlyRate
	// prefers the configured gpu_cost_per_month and falls back to this.
	TierBGPUMonthlyRateMicroCents int64 `json:"-"`
}

// RatePair holds the infrastructure and supplementary rate for a metric.
// Values are micro-cents per the metric's own unit (core-hour, GiB-hour,
// GiB-month, month).
type RatePair struct {
	Infrastructure int64 `json:"infrastructure"`
	Supplementary  int64 `json:"supplementary"`
}

// NamespaceCosts holds the cost/usage aggregates for a single namespace.
// Money fields are micro-cents; hour fields are milli-hours.
//
// #905/#942: cost_model_gpu_cost is decoded for the Tier-B GPU rate ONLY —
// it never enters the CPU/memory numerators (the cost model's own attribution
// semantics: directly-attributable resource costs stay attributed; only
// unattributable overhead is distributed). On OCP-on-cloud clusters, cloud
// GPU instance spend already reaches savings via InfraCost (the back-populate
// writes the cloud's amortized instance cost there) — the GPU rate's
// numerator is cost_model_gpu_cost alone, so GPU money is counted once.
// See #905's decision record and #942 (the Tier-B GPU rate).
type NamespaceCosts struct {
	CostModelCPUCost int64 `json:"cost_model_cpu_cost"`
	CostModelMemCost int64 `json:"cost_model_memory_cost"`
	// CostModelGPUCost is the namespace's explicit GPU charges (#942: the
	// Tier-B GPU rate numerator; never a CPU/memory term — see above).
	CostModelGPUCost int64 `json:"cost_model_gpu_cost"`
	InfraCost        int64 `json:"infrastructure_cost"`
	DistributedCost  int64 `json:"distributed_cost"`
	CPUUsageHours    int64 `json:"cpu_usage_hours"`
	CPURequestHours  int64 `json:"cpu_request_hours"`
	MemUsageHours    int64 `json:"mem_usage_hours"`
	MemRequestHours  int64 `json:"mem_request_hours"`
}

// DeriveTierBGPURate computes the cluster Tier-B GPU rate (#942):
// (Σ namespace cost_model_gpu_cost) ÷ gpuCountMonths, deposited on the
// card's TierBGPUMonthlyRateMicroCents. gpuCountMonths is the cluster's
// GPU inventory over the window in GPU-months (Σ node_gpu_count across the
// node digests ÷ 30), derived by the caller from ROS's own data — windows
// align by construction because ROS fetches effective_rates with the same
// range. The numerator is cost_model_gpu_cost ONLY: infrastructure spend
// (which on OCP-on-cloud carries the cloud's GPU instance cost) never
// enters — GPU money is counted once (#905's no-double-count constraint).
// No rate when there is no spend or no inventory (current behavior: zero
// GPU savings). A configured gpu_cost_per_month always wins (Tier A) —
// GPUMonthlyRate reads this field only as the fallback.
func (cd *ClusterCostData) DeriveTierBGPURate(gpuCountMonths float64) {
	if cd == nil {
		return
	}
	var spend int64
	for _, ns := range cd.Namespaces {
		spend += ns.CostModelGPUCost
	}
	if spend <= 0 || gpuCountMonths <= 0 {
		return
	}
	cd.TierBGPUMonthlyRateMicroCents = int64(float64(spend) / gpuCountMonths)
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
	// #461: the payload's only reader wants integers. A Masu that predates the
	// parameter answers with float dollars and echoes "dollars"; the check
	// below turns that into an error instead of a silent 1e8 mispricing.
	params.Set("money_scale", MoneyScaleMicroCents)

	reqURL := fmt.Sprintf("%s/api/cost-management/v1/effective_rates/?%s", p.BaseURL, params.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	resp, err := p.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request to Koku: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, fmt.Errorf("koku effective-rates returned %d: %s", resp.StatusCode, string(body))
	}

	var data ClusterCostData
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		// #461: a float in a money or hours field is the signature of a Masu
		// build that predates the integer contract. Name that here, otherwise
		// the operator only sees a generic unmarshal failure.
		return nil, fmt.Errorf(
			"decode response: %w (a float money/hours field means the Koku/Masu build "+
				"predates #461; this build only reads money_scale=%q)", err, MoneyScaleMicroCents)
	}

	// #461: refuse a payload on any other scale. A Masu build that predates the
	// contract omits the field (or echoes "dollars") and its money fields are
	// floats that this build would read as micro-cents, inflating every applied
	// saving by 100_000_000x with no error anywhere. The caller turns this into
	// NotifNoCostData, so the failure is visible in the recommendation.
	if data.MoneyScale != MoneyScaleMicroCents {
		return nil, fmt.Errorf(
			"koku effective-rates answered money_scale=%q; this build requires %q "+
				"(the Koku/Masu build predates #461 or ignored the parameter)",
			data.MoneyScale, MoneyScaleMicroCents,
		)
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
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return DefaultCurrency, fmt.Errorf("koku user-currency returned %d: %s", resp.StatusCode, string(body))
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
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return 1.0, fmt.Errorf("koku exchange-rate returned %d: %s", resp.StatusCode, string(body))
	}

	var data exchangeRateResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return 1.0, fmt.Errorf("decode exchange-rate response: %w", err)
	}

	if data.Rate == nil {
		logging.GetLogger().WithFields(logrus.Fields{
			"org_id":        orgID,
			"from_currency": from,
			"to_currency":   to,
		}).Warn("exchange rate unavailable, defaulting to 1.0")
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

// ClearExchangeRateCacheForTest removes all cached rates (tests only).
// Clears run at test start for cross-iteration isolation (-count=N reuses
// the process); cross-test isolation comes from distinct orgs per test
// (see the invariant note in provider_currency_test.go), since a global
// purge cannot protect a fetch that races a sibling's populate (#617).
func ClearExchangeRateCacheForTest() {
	currentExchangeRateCache().Purge()
	exchangeRateCacheSize.Set(0)
}

// ClearUserCurrencyCacheForTest removes all cached currencies (tests only).
// Same two-level rationale as exchange rates (#617).
func ClearUserCurrencyCacheForTest() {
	currentUserCurrencyCache().Purge()
	userCurrencyCacheSize.Set(0)
}
