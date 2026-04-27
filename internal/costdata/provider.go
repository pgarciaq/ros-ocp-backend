package costdata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// ClusterCostData holds the cost model rates and namespace-level cost/usage
// aggregates returned by the Koku effective-rates endpoint.
type ClusterCostData struct {
	ClusterID        string                       `json:"cluster_id"`
	ProviderUUID     string                       `json:"provider_uuid"`
	DistributionType string                       `json:"distribution_type"`
	MarkupPct        float64                      `json:"markup_pct"`
	ConfiguredRates  map[string]RatePair          `json:"configured_rates"`
	Namespaces       map[string]NamespaceCosts    `json:"namespace_aggregates"`
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

// HTTPCostDataProvider fetches cost data from the Koku masu API over HTTP.
type HTTPCostDataProvider struct {
	BaseURL    string
	HTTPClient *http.Client
}

// NewHTTPCostDataProvider creates a new HTTP-based cost data provider.
// baseURL is the Koku masu API base URL, e.g. "http://cost-onprem-masu:5042".
func NewHTTPCostDataProvider(baseURL string, timeout time.Duration) *HTTPCostDataProvider {
	return &HTTPCostDataProvider{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (p *HTTPCostDataProvider) GetEffectiveRates(
	ctx context.Context,
	orgID, clusterID string,
	start, end time.Time,
) (*ClusterCostData, error) {
	params := url.Values{}
	params.Set("org_id", orgID)
	params.Set("cluster_id", clusterID)
	params.Set("start_date", start.Format("2006-01-02"))
	params.Set("end_date", end.Format("2006-01-02"))

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

// NilCostDataProvider always returns nil cost data. Used when no Koku URL is configured.
type NilCostDataProvider struct{}

func (n *NilCostDataProvider) GetEffectiveRates(
	ctx context.Context,
	orgID, clusterID string,
	start, end time.Time,
) (*ClusterCostData, error) {
	return nil, nil
}
