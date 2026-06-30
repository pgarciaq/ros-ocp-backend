package costdata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
