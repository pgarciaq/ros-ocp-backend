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
)

const defaultCostDataCacheTTL = 5 * time.Minute

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

type costCacheEntry struct {
	data      *ClusterCostData
	expiresAt time.Time
}

var (
	sharedTransport     *http.Transport
	sharedTransportOnce sync.Once
	costDataCache       sync.Map // key: orgID+"\x00"+clusterID -> costCacheEntry
	costDataCacheTTL    = defaultCostDataCacheTTL
)

func sharedHTTPTransport() *http.Transport {
	sharedTransportOnce.Do(func() {
		sharedTransport = &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		}
	})
	return sharedTransport
}

func costCacheKey(orgID, clusterID string) string {
	return orgID + "\x00" + clusterID
}

// InvalidateCostDataCache clears cached effective rates for an org/cluster pair.
// Pass empty clusterID to invalidate all clusters for the org.
func InvalidateCostDataCache(orgID, clusterID string) {
	if clusterID == "" {
		prefix := orgID + "\x00"
		costDataCache.Range(func(k, _ any) bool {
			if key, ok := k.(string); ok && len(key) >= len(prefix) && key[:len(prefix)] == prefix {
				costDataCache.Delete(k)
			}
			return true
		})
		return
	}
	costDataCache.Delete(costCacheKey(orgID, clusterID))
}

// HTTPCostDataProvider fetches cost data from the Koku masu API over HTTP.
type HTTPCostDataProvider struct {
	BaseURL    string
	HTTPClient *http.Client
}

// NewHTTPCostDataProvider creates a new HTTP-based cost data provider with a shared transport.
func NewHTTPCostDataProvider(baseURL string, timeout time.Duration) *HTTPCostDataProvider {
	return &HTTPCostDataProvider{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout:   timeout,
			Transport: sharedHTTPTransport(),
		},
	}
}

func (p *HTTPCostDataProvider) GetEffectiveRates(
	ctx context.Context,
	orgID, clusterID string,
	start, end time.Time,
) (*ClusterCostData, error) {
	key := costCacheKey(orgID, clusterID)
	if v, ok := costDataCache.Load(key); ok {
		entry := v.(costCacheEntry)
		if time.Now().Before(entry.expiresAt) {
			return entry.data, nil
		}
		costDataCache.Delete(key)
	}

	data, err := p.fetchEffectiveRates(ctx, orgID, clusterID, start, end)
	if err != nil {
		return nil, err
	}

	costDataCache.Store(key, costCacheEntry{
		data:      data,
		expiresAt: time.Now().Add(costDataCacheTTL),
	})
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

// SetCostDataCacheTTLForTest overrides the TTL used by HTTPCostDataProvider (tests only).
func SetCostDataCacheTTLForTest(ttl time.Duration) func() {
	prev := costDataCacheTTL
	costDataCacheTTL = ttl
	return func() { costDataCacheTTL = prev }
}

// ClearCostDataCacheForTest removes all cached entries (tests only).
func ClearCostDataCacheForTest() {
	costDataCache.Range(func(k, _ any) bool {
		costDataCache.Delete(k)
		return true
	})
}
