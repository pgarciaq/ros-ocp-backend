package reship

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
)

// HTTPClient calls Koku masu reship_ros.
type HTTPClient struct {
	baseURL    string
	httpClient *http.Client
	resolver   ProviderResolver
}

// ReshipResult holds masu response metadata for metrics and logging.
type ReshipResult struct {
	FilesProcessed int
	FilesTotal     int
}

// NewHTTPClient builds a client for the masu reship_ros endpoint.
// baseURL is the Koku masu host only (e.g. "http://cost-onprem-masu:5042"), matching HTTPCostDataProvider.
func NewHTTPClient(baseURL string, client *http.Client) *HTTPClient {
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	return &HTTPClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: client,
		resolver:   NewHTTPEffectiveRatesResolver(baseURL),
	}
}

// masuAPIV1Base returns the cost-management v1 API prefix for a masu host URL.
func masuAPIV1Base(host string) string {
	return strings.TrimRight(host, "/") + "/api/cost-management/v1"
}

// ReshipURL builds the masu reship_ros URL with query parameters.
func ReshipURL(baseURL, orgID string, providerUUID uuid.UUID, startDate, endDate string) string {
	params := url.Values{}
	params.Set("schema", tenantSchema(orgID))
	params.Set("provider_uuid", providerUUID.String())
	params.Set("start_date", startDate)
	params.Set("end_date", endDate)
	return fmt.Sprintf("%s/reship_ros/?%s", masuAPIV1Base(baseURL), params.Encode())
}

// PostReship issues POST reship_ros for one cluster over the configured date window.
func (c *HTTPClient) PostReship(ctx context.Context, orgID string, clusterUUID uuid.UUID) (ReshipResult, error) {
	providerUUID, err := c.resolver.ResolveProviderUUID(ctx, orgID, clusterUUID)
	if err != nil {
		return ReshipResult{}, err
	}

	start, end := dateRange()
	reqURL := ReshipURL(c.baseURL, orgID, providerUUID, start, end)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, nil)
	if err != nil {
		return ReshipResult{}, fmt.Errorf("building reship request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ReshipResult{}, fmt.Errorf("reship_ros request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ReshipResult{}, fmt.Errorf("reship_ros returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var result ReshipResult
	if len(body) > 0 {
		var parsed struct {
			FilesProcessed int `json:"files_processed"`
			FilesTotal     int `json:"files_total"`
		}
		if err := json.Unmarshal(body, &parsed); err == nil {
			result.FilesProcessed = parsed.FilesProcessed
			result.FilesTotal = parsed.FilesTotal
		}
	}
	return result, nil
}

func dateRange() (start, end string) {
	days := engine.PluginMaxWindowDays("container")
	now := time.Now().UTC()
	end = now.Format("2006-01-02")
	start = now.AddDate(0, 0, -days).Format("2006-01-02")
	return start, end
}

func tenantSchema(orgID string) string {
	if strings.HasPrefix(orgID, "org") {
		return orgID
	}
	return "org" + orgID
}
